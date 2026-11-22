package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

type pageResolver interface {
	ResolvePage(context.Context, string) (content.PageRoute, error)
}

func (s *Server) readerContext(r *http.Request) context.Context {
	if reader, ok := s.options.Domain.(interface {
		ReaderContext(*http.Request) context.Context
	}); ok {
		return reader.ReaderContext(r)
	}
	return r.Context()
}

func (d *Domain) ResolvePage(ctx context.Context, path string) (content.PageRoute, error) {
	return d.Content.ResolvePage(ctx, path)
}

// PublicTaxonomy feeds the theme renderer's category sticker and tag links
// with the same published-only terms the public taxonomy endpoint serves.
func (d *Domain) PublicTaxonomy(ctx context.Context, locale string) ([]content.Term, error) {
	return d.Content.Taxonomy(ctx, locale, false)
}

type publicMetadata struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Social      socialMetadata `json:"social"`
}

func pageMetadata(page content.PageRoute, settings siteconfig.Public, locale string, status int) publicMetadata {
	out := publicMetadata{Title: page.Title, Description: page.Summary}
	if out.Title != "" {
		return out
	}
	key := "page." + page.Kind
	if status == http.StatusNotFound {
		key = "foundation.notFound"
	}
	out.Title = pageLabel(settings, key, locale)
	if out.Title == "" {
		out.Title = "Personal blog"
	}
	if page.Kind == "home" || page.Kind == "about" {
		if name := settings.Effective["name"].Text; name != "" {
			out.Title = name + " · " + out.Title
		}
		out.Description = settings.Effective["bio"].Text
	}
	return out
}

func (s *Server) publicPage(w http.ResponseWriter, r *http.Request, resolver pageResolver) {
	ctx := s.readerContext(r)
	page, err := resolver.ResolvePage(ctx, r.URL.Query().Get("path"))
	if errors.Is(err, content.ErrLocked) {
		w.Header().Set("X-Robots-Tag", "noindex")
		writeJSON(w, http.StatusForbidden, page)
		return
	}
	if err != nil {
		domainError(w, err)
		return
	}
	var settings siteconfig.Public
	if s.options.SiteConfig != nil {
		settings, err = s.options.SiteConfig.Public(ctx, page.Locale)
		if err != nil {
			writeError(w, Unavailable)
			return
		}
	}
	metadata := pageMetadata(page, settings, page.Locale, http.StatusOK)
	metadata.Social = s.renderedMeta(ctx, page, settings, s.options.Config.BaseURL, metadata.Title)
	writeJSON(w, http.StatusOK, struct {
		content.PageRoute
		Metadata publicMetadata `json:"metadata"`
	}{page, metadata})
}

func (d *Domain) PublicCollections(ctx context.Context, locale, kind, cursor string, limit int) (content.CollectionPage, error) {
	return d.Content.PublicCollections(ctx, locale, kind, cursor, limit)
}

func (d *Domain) pageRoutes(w http.ResponseWriter, r *http.Request) bool {
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
		out, e := d.PublicCollections(d.ReaderContext(r), q.Get("locale"), q.Get("kind"), q.Get("cursor"), limit)
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
	values, defaults := settings.Labels[key], siteconfig.Registry[key].Values
	local := values[locale]
	if len(local) == 0 {
		local = defaults[locale]
	}
	for _, raw := range []json.RawMessage{local, values["en"], defaults["en"]} {
		var text string
		if json.Unmarshal(raw, &text) == nil && text != "" {
			return text
		}
	}
	return ""
}
