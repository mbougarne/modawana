package httpserver

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	locales "github.com/mbougarne/modawana/backend/internal/locale"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/social"
)

const socialAPI = "/api/v1/owner/social"
const socialCookie = "__Host-social-oauth"

func socialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, social.ErrImage):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{
			"code": "invalid_fields", "message": messages[InvalidFields], "request_id": w.Header().Get("X-Request-ID"),
			"fields": []siteconfig.FieldIssue{{Path: "image", Code: "cover_required"}},
		}})
	case errors.Is(err, social.ErrInvalid):
		writeError(w, InvalidFields)
	case errors.Is(err, social.ErrConflict):
		writeError(w, RevisionConflict)
	case errors.Is(err, social.ErrUnavailable):
		writeError(w, Unavailable)
	default:
		domainError(w, err)
	}
}

func (d *Domain) socialRoutes(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if (path != socialAPI && !strings.HasPrefix(path, socialAPI+"/")) || !d.extensions().Installed(social.Slug) {
		return false
	}
	if d.Auth == nil {
		writeError(w, Unauthenticated)
		return true
	}
	if path == social.CallbackPath && r.Method == http.MethodGet {
		d.socialCallback(w, r)
		return true
	}
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return true
	}
	ctx := r.Context()
	switch {
	case path == socialAPI+"/connection" && r.Method == http.MethodGet:
		settings, err := d.Social.Configuration(ctx)
		if err != nil {
			socialError(w, err)
		} else {
			writeJSON(w, http.StatusOK, settings)
		}
	case path == socialAPI && r.Method == http.MethodGet:
		history, err := d.Social.History(ctx, r.URL.Query().Get("cursor"))
		if err != nil {
			socialError(w, err)
		} else {
			writeJSON(w, http.StatusOK, history)
		}
	case path == socialAPI+"/content" && r.Method == http.MethodGet && d.Content != nil:
		q := r.URL.Query()
		items, err := d.Content.SocialCandidates(ctx, q.Get("locale"), q.Get("cursor"))
		if err != nil {
			socialError(w, err)
		} else {
			writeJSON(w, http.StatusOK, items)
		}
	case path == socialAPI+"/connect" && r.Method == http.MethodPost:
		d.socialConnect(w, r)
	case path == socialAPI+"/disconnect" && r.Method == http.MethodPost:
		var in struct {
			Provider string `json:"provider"`
		}
		if DecodeJSON(w, r, &in, 1024) {
			session, err := d.Auth.FreshOwnerBinding(r)
			if err == nil {
				err = d.Social.Disconnect(ctx, in.Provider, session, d.Auth.CheckFreshOwnerBinding)
			}
			socialSaved(w, err)
		}
	case path == socialAPI+"/preview" && r.Method == http.MethodPost:
		var in social.Draft
		if DecodeJSON(w, r, &in, 32<<10) {
			out, err := d.Social.Preview(ctx, in)
			if err != nil {
				socialError(w, err)
			} else {
				writeJSON(w, http.StatusOK, out)
			}
		}
	case path == socialAPI+"/schedule" && r.Method == http.MethodPost:
		var in social.Schedule
		if DecodeJSON(w, r, &in, 32<<10) {
			socialSaved(w, d.Social.Schedule(ctx, in))
		}
	case strings.HasPrefix(path, socialAPI+"/cancel/") && r.Method == http.MethodPost:
		socialSaved(w, d.Social.Cancel(ctx, strings.TrimPrefix(path, socialAPI+"/cancel/")))
	default:
		writeError(w, MethodNotAllowed)
	}
	return true
}

func socialSaved(w http.ResponseWriter, err error) {
	if err != nil {
		socialError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (d *Domain) socialConnect(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Locale   string `json:"locale"`
		Provider string `json:"provider"`
	}
	if !DecodeJSON(w, r, &in, 1024) {
		return
	}
	session, err := d.Auth.FreshOwnerBinding(r)
	if err != nil {
		domainError(w, err)
		return
	}
	out, err := d.Social.Start(r.Context(), in.Provider, in.Locale, session, d.Auth.CheckFreshOwnerBinding)
	if err != nil {
		socialError(w, err)
		return
	}
	setSocialCookie(w, out.BrowserProof, 600)
	writeJSON(w, http.StatusOK, map[string]string{"url": out.URL})
}

func setSocialCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: socialCookie, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (d *Domain) socialCallback(w http.ResponseWriter, r *http.Request) {
	// The Strict owner cookie is unavailable on this cross-site return. Use
	// the short-lived Lax proof plus state and the bound owner session.
	w.Header().Set("Referrer-Policy", "no-referrer")
	setSocialCookie(w, "", -1)
	language, result := "en", "failed"
	browser, err := r.Cookie(socialCookie)
	if err == nil && r.URL.Query().Get("error") == "" {
		lang, err := d.Social.Complete(r.Context(), r.URL.Query().Get("state"), browser.Value, r.URL.Query().Get("code"), d.Auth.CheckFreshOwnerBinding)
		if pageLocale(lang) {
			language = lang
		}
		if err == nil {
			result = "connected"
		}
	}
	nonce := RequestID(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("X-Robots-Tag", "noindex")
	view := struct{ Locale, Dir, Nonce, Path, Title, Continue string }{
		language, locales.Direction(language), nonce, "/" + language + "/admin/social?oauth=" + result,
		pageLabel(siteconfig.Public{}, "social.callbackTitle", language), pageLabel(siteconfig.Public{}, "social.continue", language),
	}
	_ = socialReturn.Execute(w, view)
}

var socialReturn = template.Must(template.New("social-return").Parse(`<!doctype html>
<html lang="{{.Locale}}" dir="{{.Dir}}"><head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body><p><a href="{{.Path}}">{{.Continue}}</a></p><script nonce="{{.Nonce}}">window.location.replace({{.Path}})</script></body></html>`))
