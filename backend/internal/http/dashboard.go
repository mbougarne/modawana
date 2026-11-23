package httpserver

import (
	"errors"
	"net/http"

	"github.com/mbougarne/modawana/backend/internal/dashboard"
)

func (d *Domain) dashboardRoutes(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/v1/dashboard-layout" {
		return false
	}
	owner, err := d.Auth.OwnerRequest(r)
	if err != nil {
		domainError(w, err)
		return true
	}
	switch r.Method {
	case http.MethodGet:
		out, err := d.Dashboard.Get(r.Context(), owner, dashboard.Choices(d.extensions()))
		if err != nil {
			domainError(w, err)
		} else {
			writeJSON(w, http.StatusOK, out)
		}
	case http.MethodPut:
		var input dashboard.Input
		if !DecodeJSON(w, r, &input, 64<<10) {
			return true
		}
		out, err := d.Dashboard.Save(r.Context(), owner, input)
		switch {
		case errors.Is(err, dashboard.ErrInvalid):
			writeError(w, InvalidFields)
		case err != nil:
			domainError(w, err)
		default:
			writeJSON(w, http.StatusOK, out)
		}
	default:
		writeError(w, MethodNotAllowed)
	}
	return true
}
