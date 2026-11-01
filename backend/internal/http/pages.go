package httpserver

import (
	"context"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/access"
	"net/http"
	"strconv"

	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func (d *Domain) ResolvePage(ctx context.Context, path string) (content.PageRoute, error) {
	return d.Content.ResolvePage(ctx, path)
}

// PublicTaxonomy feeds the theme renderer's category sticker and tag links
// with the same published-only terms the public taxonomy endpoint serves.
func (d *Domain) PublicTaxonomy(ctx context.Context, locale string) ([]content.Term, error) {
	return d.Content.Taxonomy(ctx, locale, false)
}
func (d *Domain) pageRoutes(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/v1/public/page" && r.Method == "GET" {
		out, e := d.ResolvePage(access.WithPrincipal(r.Context(), d.readerPrincipal(r)), r.URL.Query().Get("path"))
		if errors.Is(e, content.ErrLocked) {
			w.Header().Set("X-Robots-Tag", "noindex")
			writeJSON(w, 403, out)
		} else if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if r.URL.Path == "/api/v1/public/collections" && r.Method == "GET" {
		q := r.URL.Query()
		limit := 20
		if q.Has("limit") {
			var e error
			limit, e = strconv.Atoi(q.Get("limit"))
			if e != nil {
				writeError(w, InvalidFields)
				return true
			}
		}
		out, e := d.Content.PublicCollections(d.ReaderContext(r), q.Get("locale"), q.Get("kind"), q.Get("cursor"), limit)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	return false
}

func pageLabel(settings siteconfig.Public, key, locale string) string {
	values := settings.Labels[key]
	for _, raw := range []json.RawMessage{values[locale], values["en"], siteconfig.Registry[key].Values[locale], siteconfig.Registry[key].Values["en"]} {
		var text string
		if json.Unmarshal(raw, &text) == nil && text != "" {
			return text
		}
	}
	return ""
}
