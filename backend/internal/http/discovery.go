package httpserver

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/content"
)

func (d *Domain) discoveryRoutes(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if (p == "/api/v1/public/content" || p == "/api/v1/public/search") && r.Method == "GET" {
		q := r.URL.Query()
		limit := 0
		var e error
		if q.Has("limit") {
			limit, e = strconv.Atoi(q.Get("limit"))
			if e != nil || limit < 1 {
				writeError(w, InvalidFields)
				return true
			}
		}
		out, e := d.Content.Discover(r.Context(), content.Filter{Locale: q.Get("locale"), Kind: q.Get("kind"), Category: q.Get("category"), Tag: q.Get("tag"), Query: q.Get("q"), Cursor: q.Get("cursor"), Limit: limit}, d.readerPrincipal(r))
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if p == "/api/v1/public/taxonomy" && r.Method == "GET" {
		out, e := d.Content.Taxonomy(r.Context(), r.URL.Query().Get("locale"), false)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": out})
		}
		return true
	}
	if p != "/api/v1/taxonomy" && !strings.HasPrefix(p, "/api/v1/taxonomy/") {
		return false
	}
	if _, e := d.Auth.OwnerRequest(r); e != nil {
		domainError(w, e)
		return true
	}
	if p == "/api/v1/taxonomy" && r.Method == "GET" {
		out, e := d.Content.Taxonomy(r.Context(), r.URL.Query().Get("locale"), true)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": out})
		}
		return true
	}
	parts := strings.Split(strings.TrimPrefix(p, "/api/v1/taxonomy/"), "/")
	if len(parts) == 3 && r.Method == "PUT" {
		var in content.TaxonomyInput
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		out, e := d.Content.SaveTerm(r.Context(), parts[0], parts[1], parts[2], in)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	writeError(w, MethodNotAllowed)
	return true
}
