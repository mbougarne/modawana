package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/analytics"
	"github.com/mbougarne/modawana/backend/internal/forms"
)

func formError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, forms.ErrInvalid):
		writeError(w, InvalidFields)
	case errors.Is(err, forms.ErrConflict):
		writeError(w, IdempotencyConflict)
	case errors.Is(err, forms.ErrLimited):
		w.Header().Set("Retry-After", "900")
		writeError(w, RateLimited)
	default:
		domainError(w, err)
	}
}

func (d *Domain) leadSubmission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, MethodNotAllowed)
		return
	}
	if d.Auth == nil || d.Content == nil || r.URL.RawQuery != "" ||
		len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != d.Auth.Config.BaseURL {
		writeError(w, Forbidden)
		return
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/x-www-form-urlencoded" {
		writeError(w, UnsupportedMediaType)
		return
	}
	ip := d.Auth.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	if err := d.Forms.Allow(r.Context(), ip); err != nil {
		formError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 24<<10)
	defer r.Body.Close()
	if err := r.ParseForm(); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeError(w, TooLarge)
		} else {
			writeError(w, BadRequest)
		}
		return
	}
	fields := map[string]string{}
	for key, values := range r.PostForm {
		if len(values) != 1 {
			writeError(w, InvalidFields)
			return
		}
		if field, ok := strings.CutPrefix(key, "f_"); ok {
			fields[field] = values[0]
			continue
		}
		switch key {
		case "form", "version", "locale", "page", "revision", "block", "consent", "website", "retry":
		default:
			writeError(w, InvalidFields)
			return
		}
	}
	values := r.PostForm
	if values.Get("website") != "" {
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
		return
	}
	version, err := strconv.ParseInt(values.Get("version"), 10, 64)
	if err != nil || (values.Get("consent") != "" && values.Get("consent") != "yes") {
		writeError(w, InvalidFields)
		return
	}
	in := forms.Submission{Reference: forms.Reference{Key: values.Get("form"), Version: version},
		Locale: values.Get("locale"), Page: values.Get("page"), Revision: values.Get("revision"),
		Block: values.Get("block"), Values: fields, Consent: values.Get("consent") == "yes"}
	check := d.Content.CheckPublishedForm
	var analyticsPath string
	privacy := requestPrivacy(r)
	if d.Analytics != nil && d.Analytics.Settings().Allows(privacy) {
		check = func(ctx context.Context, tx *sql.Tx, in forms.Submission) error {
			if err := d.Content.CheckPublishedForm(ctx, tx, in); err != nil {
				return err
			}
			analyticsPath, _ = d.Content.AnalyticsFormPath(ctx, tx, in)
			return nil
		}
	}
	created, err := d.Forms.Submit(r.Context(), values.Get("retry"), in, check)
	if err != nil {
		formError(w, err)
		return
	}
	if created && analyticsPath != "" {
		d.Analytics.Record(analytics.LeadSubmitted, analyticsPath, privacy)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (d *Domain) formRoutes(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path == "/api/v1/public/lead-submit" {
		d.leadSubmission(w, r)
		return true
	}
	if path != "/api/v1/owner/forms" && path != "/api/v1/owner/leads" &&
		path != "/api/v1/owner/leads/export" && !strings.HasPrefix(path, "/api/v1/owner/leads/") {
		return false
	}
	if d.Auth == nil {
		writeError(w, Unauthenticated)
		return true
	}
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return true
	}
	ctx := r.Context()
	switch {
	case path == "/api/v1/owner/forms" && r.Method == http.MethodGet:
		items, err := d.Forms.List(ctx)
		if err != nil {
			formError(w, err)
		} else {
			writeJSON(w, http.StatusOK, items)
		}
	case path == "/api/v1/owner/forms" && r.Method == http.MethodPut:
		var in forms.Form
		if DecodeJSON(w, r, &in, 32<<10) {
			out, err := d.Forms.Save(ctx, in)
			if err != nil {
				formError(w, err)
			} else {
				writeJSON(w, http.StatusOK, out)
			}
		}
	case path == "/api/v1/owner/leads" && r.Method == http.MethodGet:
		out, err := d.Forms.Inbox(ctx, r.URL.Query().Get("cursor"))
		if err != nil {
			formError(w, err)
		} else {
			writeJSON(w, http.StatusOK, out)
		}
	case path == "/api/v1/owner/leads/export" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="modawana-leads.csv"`)
		if err := d.Forms.Export(ctx, w); err != nil {
			// A partial streamed export cannot be replaced with a JSON response.
			if response, ok := w.(*response); ok && response.status == 0 {
				formError(w, err)
			}
		}
	case strings.HasPrefix(path, "/api/v1/owner/leads/") && r.Method == http.MethodPatch:
		var in struct {
			Status string   `json:"status"`
			Tags   []string `json:"tags"`
		}
		if DecodeJSON(w, r, &in, 8<<10) {
			if err := d.Forms.Update(ctx, strings.TrimPrefix(path, "/api/v1/owner/leads/"), in.Status, in.Tags); err != nil {
				formError(w, err)
			} else {
				writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
			}
		}
	case strings.HasPrefix(path, "/api/v1/owner/leads/") && r.Method == http.MethodDelete:
		if err := d.Forms.Delete(ctx, strings.TrimPrefix(path, "/api/v1/owner/leads/")); err != nil {
			formError(w, err)
		} else {
			writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
		}
	default:
		writeError(w, MethodNotAllowed)
	}
	return true
}
