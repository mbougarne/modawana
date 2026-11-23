package httpserver

import (
	"context"

	"github.com/mbougarne/modawana/backend/internal/access"
	"net/http"
	"strconv"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/content"
)

func (d *Domain) PublicDiscovery(ctx context.Context, filter content.Filter) (content.Page, error) {
	return d.Content.Discover(ctx, filter, access.FromContext(ctx))
}

func (d *Domain) discoveryRoutes(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	aboutDocuments := p == "/api/v1/owner/about-documents"
	if (p == "/api/v1/public/content" || p == "/api/v1/public/search" || aboutDocuments) && r.Method == "GET" {
		q := r.URL.Query()
		locale, kind := q.Get("locale"), q.Get("kind")
		if aboutDocuments {
			if d.Auth == nil {
				writeError(w, Unauthenticated)
				return true
			}
			if _, err := d.Auth.OwnerRequest(r); err != nil {
				domainError(w, err)
				return true
			}
			kind = "doc"
		} else if hiddenLocale(r.Context(), locale) {
			writeError(w, NotFound)
			return true
		}
		limit := 0
		var e error
		if q.Has("limit") {
			limit, e = strconv.Atoi(q.Get("limit"))
			if e != nil || limit < 1 {
				writeError(w, InvalidFields)
				return true
			}
		}
		out, e := d.PublicDiscovery(d.ReaderContext(r), content.Filter{Locale: locale, Kind: kind, Category: q.Get("category"), Tag: q.Get("tag"), Query: q.Get("q"), Cursor: q.Get("cursor"), Limit: limit})
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if p == "/api/v1/public/taxonomy" && r.Method == "GET" {
		locale := r.URL.Query().Get("locale")
		if hiddenLocale(r.Context(), locale) {
			writeError(w, NotFound)
			return true
		}
		out, e := d.Content.Taxonomy(r.Context(), locale, false)
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
