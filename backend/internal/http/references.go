package httpserver

import "net/http"

func (d *Domain) referenceRoutes(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/v1/owner/references" {
		return false
	}
	w.Header().Set("X-Reference-Search-Version", "2")
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return true
	}
	if r.Method != "GET" {
		writeError(w, MethodNotAllowed)
		return true
	}
	q := r.URL.Query()
	out, err := d.Content.ReferenceChoices(r.Context(), q.Get("locale"), q.Get("kind"), q.Get("q"), q.Get("collection_key"), q["exclude_key"]...)
	if err != nil {
		domainError(w, err)
	} else {
		writeJSON(w, 200, out)
	}
	return true
}
