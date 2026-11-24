package httpserver

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/analytics"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

const analyticsAPI = "/api/v1/owner/analytics"
const analyticsChoice = "/api/v1/public/analytics-choice"
const analyticsCookie = "__Host-analytics-choice"

func requestPrivacy(r *http.Request) analytics.Privacy {
	out := analytics.Privacy{}
	if cookie, err := r.Cookie(analyticsCookie); err == nil && (cookie.Value == analytics.Allow || cookie.Value == analytics.Decline) {
		out.Choice = cookie.Value
	}
	out.Disallowed = slices.Contains(r.Header.Values("Sec-GPC"), "1") || slices.Contains(r.Header.Values("DNT"), "1") ||
		strings.Contains(strings.ToLower(r.Header.Get("Sec-Purpose")), "prefetch") ||
		strings.Contains(strings.ToLower(r.Header.Get("Purpose")), "prefetch")
	for _, name := range []string{"__Host-owner", "__Host-staff"} {
		if _, err := r.Cookie(name); err == nil {
			out.Disallowed = true
		}
	}
	return out
}

func (d *Domain) analyticsRoutes(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if (path != analyticsAPI && path != analyticsChoice) || !d.extensions().Installed(analytics.Slug) {
		return false
	}
	if d.Auth == nil {
		writeError(w, Unauthenticated)
		return true
	}
	if path == analyticsChoice {
		d.saveAnalyticsChoice(w, r)
		return true
	}
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return true
	}
	switch r.Method {
	case http.MethodGet:
		out, err := d.Analytics.Report(r.Context())
		if err != nil {
			domainError(w, err)
		} else {
			writeJSON(w, http.StatusOK, out)
		}
	case http.MethodPut:
		var in analytics.Settings
		if DecodeJSON(w, r, &in, 48<<10) {
			out, err := d.Analytics.Update(r.Context(), in)
			switch {
			case errors.Is(err, analytics.ErrInvalid):
				writeError(w, InvalidFields)
			case err != nil:
				domainError(w, err)
			default:
				writeJSON(w, http.StatusOK, out)
			}
		}
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, MethodNotAllowed)
	}
	return true
}

func (d *Domain) saveAnalyticsChoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, MethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != d.Auth.Config.BaseURL ||
		r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, Forbidden)
		return
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/x-www-form-urlencoded" {
		writeError(w, UnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	if err := r.ParseForm(); err != nil {
		writeError(w, BadRequest)
		return
	}
	if len(r.PostForm) != 2 || len(r.PostForm["return"]) != 1 || len(r.PostForm["choice"]) != 1 {
		writeError(w, InvalidFields)
		return
	}
	path, ok := analytics.Path(r.PostForm.Get("return"))
	choice := r.PostForm.Get("choice")
	if !ok || (choice != analytics.Allow && choice != analytics.Decline) {
		writeError(w, InvalidFields)
		return
	}
	if d.Analytics.Settings().Enabled {
		const age = 180 * 24 * time.Hour
		http.SetCookie(w, &http.Cookie{Name: analyticsCookie, Value: choice, Path: "/", MaxAge: int(age / time.Second),
			HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

func publicAnalyticsPage(page content.PageRoute) bool {
	return page.URL != "" && (page.Content == nil || !page.Content.Locked && (page.Content.SEO == nil || !page.Content.SEO.NoIndex))
}

func (s *Server) analyticsPage(ctx context.Context, r *http.Request, page content.PageRoute, status int, public bool) string {
	if s.options.Analytics == nil || !public || status != http.StatusOK || r.Method != http.MethodGet ||
		!publicAnalyticsPage(page) || requestPrivacy(r).Disallowed {
		return ""
	}
	path, ok := analytics.Path(page.URL)
	if !ok {
		return ""
	}
	settings := s.options.Analytics.Settings()
	if !settings.Enabled || settings.Excluded(path) {
		return ""
	}
	// The server may have resolved a gated lesson for a known learner.
	if access.FromContext(ctx).Audience != access.Anonymous {
		resolver, ok := s.options.Domain.(pageResolver)
		if !ok {
			return ""
		}
		decoded, err := url.PathUnescape(path)
		if err != nil {
			return ""
		}
		anon, err := resolver.ResolvePage(access.WithPrincipal(ctx, access.Principal{}), decoded)
		if err != nil || !publicAnalyticsPage(anon) || anon.URL != page.URL {
			return ""
		}
	}
	return path
}

func analyticsFooter(r *http.Request, path string, settings siteconfig.Public, language string, mode string) *theme.AnalyticsChoice {
	if path == "" {
		return nil
	}
	label := func(key string) string { return pageLabel(settings, key, language) }
	view := &theme.AnalyticsChoice{Action: analyticsChoice, ReturnPath: path, Label: label("analytics.privacy")}
	switch requestPrivacy(r).Choice {
	case analytics.Allow:
		view.Decline = label("analytics.decline")
	case analytics.Decline:
		view.Allow = label("analytics.allow")
	default:
		view.Allow, view.Decline = label("analytics.allow"), label("analytics.decline")
		view.Note = label("analytics.reader.optIn")
		if mode == analytics.UnlessDeclined {
			view.Note = label("analytics.reader.unlessDeclined")
		}
	}
	return view
}
