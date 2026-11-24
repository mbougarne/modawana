package httpserver

import (
	"errors"
	"net/http"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/editorial"
)

const editorialAPI = "/api/v1/owner/editorial"
const editorialContext = editorialAPI + "/context"
const editorialSuggestion = editorialAPI + "/suggest"

func (d *Domain) editorialRoutes(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if (path != editorialAPI && path != editorialContext && path != editorialSuggestion) ||
		!d.extensions().Installed(editorial.Slug) {
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
	case path == editorialAPI && r.Method == http.MethodGet:
		out, err := d.Editorial.Settings(ctx)
		if err != nil {
			editorialError(w, err)
		} else {
			writeJSON(w, http.StatusOK, out)
		}
	case path == editorialAPI && r.Method == http.MethodPut:
		var in editorial.Update
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		check, err := d.Auth.FreshOwnerCheck(r)
		if err != nil {
			domainError(w, err)
			return true
		}
		out, err := d.Editorial.Update(ctx, in, check)
		if err != nil {
			editorialError(w, err)
		} else {
			writeJSON(w, http.StatusOK, out)
		}
	case path == editorialContext && r.Method == http.MethodGet && d.Content != nil:
		q := r.URL.Query()
		source, err := d.Content.EditorialContext(ctx, q.Get("key"), q.Get("locale"), "", true, true)
		if err != nil {
			editorialError(w, err)
		} else {
			writeJSON(w, http.StatusOK, source)
		}
	case path == editorialSuggestion && r.Method == http.MethodPost:
		var in editorial.SourceRequest
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		out, err := d.Editorial.Suggest(ctx, in)
		if err != nil {
			editorialError(w, err)
		} else {
			writeJSON(w, http.StatusOK, out)
		}
	default:
		writeError(w, MethodNotAllowed)
	}
	return true
}

func editorialError(w http.ResponseWriter, err error) {
	var status int
	var code, message string
	switch {
	case errors.Is(err, editorial.ErrInvalid), errors.Is(err, content.ErrInvalid):
		writeError(w, InvalidFields)
		return
	case errors.Is(err, editorial.ErrChanged):
		writeError(w, RevisionConflict)
		return
	case errors.Is(err, editorial.ErrDisabled):
		status, code, message = http.StatusServiceUnavailable, "editorial_disabled", "Editorial generation is unavailable."
	case errors.Is(err, editorial.ErrKey):
		status, code, message = http.StatusServiceUnavailable, "editorial_key", "The editorial credential is unavailable."
	case errors.Is(err, editorial.ErrQuota):
		status, code, message = http.StatusTooManyRequests, "editorial_quota", "The daily editorial quota has been used."
	case errors.Is(err, editorial.ErrProviderLimit):
		status, code, message = http.StatusTooManyRequests, "editorial_provider_limit", "The provider refused the request because of its quota or rate limits."
	case errors.Is(err, editorial.ErrBusy):
		status, code, message = http.StatusTooManyRequests, "editorial_busy", "Editorial generation is busy."
	case errors.Is(err, editorial.ErrTimeout):
		status, code, message = http.StatusGatewayTimeout, "editorial_timeout", "The editorial provider timed out."
	case errors.Is(err, editorial.ErrProvider):
		status, code, message = http.StatusBadGateway, "editorial_provider", "The editorial provider rejected or failed the request."
	case errors.Is(err, editorial.ErrOutput):
		status, code, message = http.StatusBadGateway, "editorial_output", "The editorial provider returned an unusable suggestion."
	default:
		domainError(w, err)
		return
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID")}})
}
