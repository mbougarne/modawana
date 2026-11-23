package siteconfig

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
)

type MenuPage struct {
	Title string
	Slug  string
}

type menuPageRef struct {
	path string
	key  string
}

func pageMenuRefs(site Site) []menuPageRef {
	var refs []menuPageRef
	for _, menu := range []struct {
		path  string
		items *[]MenuItem
	}{{"header_menu", site.HeaderMenu}, {"footer_menu", site.FooterMenu}} {
		if menu.items == nil {
			continue
		}
		for i, item := range *menu.items {
			if item.Target == "page" {
				refs = append(refs, menuPageRef{path: menu.path + "." + strconv.Itoa(i) + ".page_key", key: item.PageKey})
			}
		}
	}
	return refs
}

func validateMenuPageReferences(ctx context.Context, q query, site Site) error {
	checked := map[string]bool{}
	var issues []FieldIssue
	for _, ref := range pageMenuRefs(site) {
		found, ok := checked[ref.key]
		if !ok {
			var one int
			err := q.QueryRowContext(ctx, `SELECT 1 FROM content_items WHERE key=? AND kind='page'`, ref.key).Scan(&one)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			found = err == nil
			checked[ref.key] = found
		}
		if !found {
			issues = append(issues, FieldIssue{ref.path, "menu_page"})
		}
	}
	if len(issues) != 0 {
		return &ValidationError{Fields: issues}
	}
	return nil
}

// Published destinations come from the same read transaction as the menus.
// Project only links that resolve in the requested public language.
func projectMenuPages(ctx context.Context, q query, site *Site, locale string) (map[string]MenuPage, error) {
	refs := pageMenuRefs(*site)
	if len(refs) == 0 {
		return nil, nil
	}
	pages := map[string]MenuPage{}
	if slices.Contains(site.visibleLocales(), locale) {
		args := []any{locale}
		seen := map[string]bool{}
		for _, ref := range refs {
			if !seen[ref.key] {
				seen[ref.key] = true
				args = append(args, ref.key)
			}
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)-1), ",")
		rows, err := q.QueryContext(ctx, `SELECT i.key, v.slug, r.title
			FROM content_items i
			JOIN content_versions v ON v.item_id=i.id AND v.locale=?
			JOIN revisions r ON r.id=v.published_revision_id AND r.version_id=v.id
			WHERE i.kind='page' AND i.key IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			var page MenuPage
			if err := rows.Scan(&key, &page.Slug, &page.Title); err != nil {
				return nil, err
			}
			pages[key] = page
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	for _, menu := range []*[]MenuItem{site.HeaderMenu, site.FooterMenu} {
		if menu != nil {
			*menu = slices.DeleteFunc(*menu, func(item MenuItem) bool {
				_, published := pages[item.PageKey]
				return item.Target == "page" && !published
			})
		}
	}
	return pages, nil
}
