package content

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/access"
)

// PageRoute is the public contract shared by HTML delivery and SPA navigation.
// It never accepts an owner principal: owning a session cannot turn a public URL into a preview.
type PageRoute struct {
	Kind         string            `json:"kind"`
	Locale       string            `json:"locale"`
	URL          string            `json:"url"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Alternatives []Alternative     `json:"alternatives"`
	Content      *PublicContent    `json:"content,omitempty"`
	Collection   *PublicCollection `json:"collection,omitempty"`
}

func collectionPath(locale, kind, slug string) string {
	section := "docs"
	if kind == "learning" {
		section = "learn"
	}
	return "/" + locale + "/" + section + "/" + url.PathEscape(slug)
}

// Canonical paths use published locale slugs and active membership only.
func canonical(ctx context.Context, q query, key, locale, kind, slug string) (string, error) {
	if kind == "article" {
		return "/" + locale + "/articles/" + url.PathEscape(slug), nil
	}
	var collectionSlug, collectionKind string
	e := q.QueryRowContext(ctx, `SELECT l.slug,c.kind FROM content_items i JOIN collection_plan_members m ON m.item_id=i.id JOIN collection_plan_state st ON st.active_plan_id=m.plan_id JOIN collections c ON c.id=st.collection_id JOIN collection_locales l ON l.collection_id=c.id WHERE i.key=? AND l.locale=? AND l.visible=1`, key, locale).Scan(&collectionSlug, &collectionKind)
	if errors.Is(e, sql.ErrNoRows) {
		// Documents can be published before their guide plan is activated.
		if kind == "doc" {
			return "/" + locale + "/doc/" + url.PathEscape(slug), nil
		}
		return "", nil
	}
	if e != nil {
		return "", e
	}
	return collectionPath(locale, collectionKind, collectionSlug) + "/" + url.PathEscape(slug), nil
}

func (s *Service) ResolvePage(ctx context.Context, path string) (PageRoute, error) {
	out := PageRoute{Alternatives: []Alternative{}}
	if path == "/" {
		path = "/en"
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 || !localeOK(parts[0]) {
		return out, sql.ErrNoRows
	}
	out.Locale = parts[0]
	out.URL = path
	if len(parts) == 1 || len(parts) == 2 && (parts[1] == "about" || parts[1] == "writing" || parts[1] == "search" || parts[1] == "learn" || parts[1] == "docs") {
		out.Kind = "home"
		suffix := ""
		if len(parts) == 2 {
			out.Kind = parts[1]
			suffix = "/" + parts[1]
		}
		for _, l := range []string{"en", "ar", "fr"} {
			out.Alternatives = append(out.Alternatives, Alternative{Locale: l, URL: "/" + l + suffix})
		}
		return out, nil
	}
	if len(parts) < 3 || len(parts) > 4 {
		return out, sql.ErrNoRows
	}
	if parts[1] == "learn" || parts[1] == "docs" {
		c, e := s.PublicCollection(ctx, out.Locale, parts[2], access.FromContext(ctx))
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) && parts[1] == "docs" && len(parts) == 4 {
				legacy, err := s.docCollectionPath(ctx, out.Locale, parts[2], parts[3])
				if err != nil {
					return out, err
				}
				if legacy {
					return s.ResolvePage(ctx, "/"+out.Locale+"/doc/"+url.PathEscape(parts[3]))
				}
			}
			return out, e
		}
		if collectionPath(out.Locale, c.Kind, c.Slug) != "/"+out.Locale+"/"+parts[1]+"/"+url.PathEscape(parts[2]) {
			return out, sql.ErrNoRows
		}
		out.Collection = &c
		if len(parts) == 3 {
			out.Kind = "collection"
			out.Title = c.Title
			out.Summary = c.Summary
			// Only locales with a visible, nonempty active public outline are alternatives.
			rows, e := s.DB.QueryContext(ctx, `SELECT l.locale,l.slug FROM collection_locales l JOIN collections c ON c.id=l.collection_id WHERE c.key=? AND l.visible=1 ORDER BY l.locale`, c.Key)
			if e != nil {
				return out, e
			}
			candidates := []Alternative{}
			for rows.Next() {
				var a Alternative
				if e = rows.Scan(&a.Locale, &a.Slug); e != nil {
					rows.Close()
					return out, e
				}
				candidates = append(candidates, a)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return out, e
			}
			for _, a := range candidates {
				alt, e := s.PublicCollection(ctx, a.Locale, a.Slug, access.FromContext(ctx))
				if errors.Is(e, sql.ErrNoRows) {
					continue
				}
				if e != nil {
					return out, e
				}
				a.URL = collectionPath(a.Locale, alt.Kind, a.Slug)
				out.Alternatives = append(out.Alternatives, a)
			}
			out.URL = collectionPath(out.Locale, c.Kind, c.Slug)
			return out, nil
		}
	} else if len(parts) != 3 || (parts[1] != "articles" && parts[1] != "doc") {
		return out, sql.ErrNoRows
	}
	slug := parts[len(parts)-1]
	c, e := s.Public(ctx, out.Locale, slug, access.FromContext(ctx))
	if e != nil && !errors.Is(e, ErrLocked) {
		return out, e
	}
	canonicalURL, ce := canonical(ctx, s.DB, c.Key, c.Locale, c.Kind, c.Slug)
	if ce != nil {
		return out, ce
	}
	decoded, ce := url.PathUnescape(canonicalURL)
	if ce != nil || canonicalURL == "" {
		return out, sql.ErrNoRows
	}
	if decoded != path {
		legacyDoc := c.Kind == "doc" && len(parts) == 3 && parts[1] == "doc"
		if c.Kind == "doc" && len(parts) == 4 && parts[1] == "docs" {
			legacyDoc, ce = s.docCollectionPath(ctx, out.Locale, parts[2], parts[3])
			if ce != nil {
				return out, ce
			}
		}
		var alias int
		if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM content_route_aliases a JOIN content_items i ON i.id=a.item_id WHERE a.path=? AND i.key=? AND a.locale=?", path, c.Key, c.Locale).Scan(&alias); err != nil {
			return out, err
		}
		if !legacyDoc && alias != 1 {
			return out, sql.ErrNoRows
		}
	}
	out.URL = canonicalURL
	out.Kind = c.Kind
	out.Title = c.Title
	out.Summary = c.Summary
	out.Content = &c
	out.Alternatives = c.Alternatives
	return out, e
}

// A published document remains independently readable when its guide's visible
// locale or active plan changes. Never apply this fallback to gated lessons.
func (s *Service) docCollectionPath(ctx context.Context, locale, collectionSlug, slug string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM content_items i JOIN content_versions v ON v.item_id=i.id JOIN collection_items m ON m.item_id=i.id JOIN collections c ON c.id=m.collection_id JOIN collection_locales l ON l.collection_id=c.id AND l.locale=v.locale WHERE i.kind='doc' AND c.kind='docs' AND v.locale=? AND v.slug=? AND v.published_revision_id IS NOT NULL AND l.slug=?`, locale, slug, collectionSlug).Scan(&count)
	return count == 1, err
}

type CollectionPage struct {
	Items []PublicCollection `json:"items"`
	Next  string             `json:"next_cursor"`
}

func (s *Service) PublicCollections(ctx context.Context, locale, kind, cursor string, limit int) (CollectionPage, error) {
	out := CollectionPage{Items: []PublicCollection{}}
	if !localeOK(locale) || (kind != "learning" && kind != "docs") || limit < 1 || limit > 20 || (cursor != "" && !keyPattern.MatchString(cursor)) {
		return out, ErrInvalid
	}
	// Empty plans are not public collections. The key cursor permits bounded pages.
	rows, e := s.DB.QueryContext(ctx, `SELECT c.key,l.slug FROM collections c JOIN collection_locales l ON l.collection_id=c.id JOIN collection_plan_state st ON st.collection_id=c.id WHERE l.locale=? AND l.visible=1 AND c.kind=? AND c.key>? AND EXISTS(SELECT 1 FROM collection_plan_members m LEFT JOIN content_versions v ON v.item_id=m.item_id AND v.locale=l.locale AND v.published_revision_id IS NOT NULL WHERE m.plan_id=st.active_plan_id AND (v.id IS NOT NULL OR coalesce(json_extract(m.announcements_json,'$.'||l.locale),'')<>'')) ORDER BY c.key LIMIT ?`, locale, kind, cursor, limit+1)
	if e != nil {
		return out, e
	}
	type candidate struct{ key, slug string }
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.key, &c.slug); e != nil {
			rows.Close()
			return out, e
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
		out.Next = candidates[len(candidates)-1].key
	}
	for _, c := range candidates {
		v, e := s.PublicCollection(ctx, locale, c.slug, access.FromContext(ctx))
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}

// A conservative estimate from whitespace-separated words in the saved Markdown.
// Zero on older immutable revisions means unavailable, never an invented duration.
func readingMinutes(markdown string) int {
	words := len(strings.Fields(markdown))
	if words == 0 {
		return 0
	}
	return (words + 199) / 200
}
