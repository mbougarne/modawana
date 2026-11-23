package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/outbox"
)

func (d *Domain) deliveryRoutes(w http.ResponseWriter, r *http.Request) bool {
	const path = "/api/v1/plugin-deliveries"
	p := r.URL.Path
	if p != path && !strings.HasPrefix(p, path+"/") {
		return false
	}
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return true
	}
	if p == path {
		if r.Method != http.MethodGet {
			writeError(w, MethodNotAllowed)
			return true
		}
		result, err := d.Deliveries.List(r.Context(), r.URL.Query().Get("cursor"))
		switch {
		case errors.Is(err, outbox.ErrInvalid):
			writeError(w, InvalidFields)
		case err != nil:
			domainError(w, err)
		default:
			writeJSON(w, http.StatusOK, result)
		}
		return true
	}
	parts := strings.Split(strings.TrimPrefix(p, path+"/"), "/")
	switch {
	case len(parts) != 2 || parts[1] != "retry":
		writeError(w, NotFound)
	case r.Method != http.MethodPost:
		writeError(w, MethodNotAllowed)
	default:
		err := d.Deliveries.Retry(r.Context(), parts[0])
		switch {
		case errors.Is(err, outbox.ErrInvalid):
			writeError(w, InvalidFields)
		case err != nil:
			domainError(w, err)
		default:
			writeJSON(w, http.StatusOK, map[string]bool{"queued": true})
		}
	}
	return true
}
