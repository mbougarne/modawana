package content

import (
	"context"
	"strings"
)

type ReferenceChoice struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Version int64  `json:"version"`
}
type ReferencePage struct {
	Items []ReferenceChoice `json:"items"`
	More  bool              `json:"more"`
}

func (s *Service) ReferenceChoices(ctx context.Context, locale, kind, search, collection string, exclude ...string) (ReferencePage, error) {
	out := ReferencePage{Items: []ReferenceChoice{}}
	if len(exclude) > 500 || !localeOK(locale) || !strOK(search, 200, true) || (collection != "" && !keyPattern.MatchString(collection)) {
		return out, ErrInvalid
	}
	search = strings.TrimSpace(search)
	query := `SELECT c.key,coalesce(l.title,''),c.kind,c.version FROM collections c LEFT JOIN collection_locales l ON l.collection_id=c.id AND l.locale=? WHERE c.kind=? AND instr(lower(c.key||' '||coalesce(l.title,'')),lower(?))>0 ORDER BY c.key LIMIT 6`
	args := []any{locale, kind, search}
	if kind == "lesson" || kind == "doc" {
		query = `SELECT i.key,r.title,i.kind,v.version FROM content_items i JOIN content_versions v ON v.item_id=i.id JOIN revisions r ON r.id=v.draft_revision_id WHERE v.locale=? AND i.kind=? AND instr(lower(i.key||' '||r.title),lower(?))>0 AND (?='' OR EXISTS(SELECT 1 FROM collection_items m JOIN collections c ON c.id=m.collection_id WHERE m.item_id=i.id AND c.key=?)) ORDER BY i.key LIMIT 6`
		args = append(args, collection, collection)
	} else if kind == "learning_item" || kind == "docs_item" {
		target := "lesson"
		if kind == "docs_item" {
			target = "doc"
		}
		query = "SELECT i.key,r.title,i.kind,v.version FROM content_items i JOIN content_versions v ON v.item_id=i.id JOIN revisions r ON r.id=v.draft_revision_id WHERE v.locale=? AND i.kind IN ('article',?) AND instr(lower(i.key||' '||r.title),lower(?))>0 AND NOT EXISTS(SELECT 1 FROM collection_items m JOIN collections c ON c.id=m.collection_id WHERE m.item_id=i.id AND c.key!=?) ORDER BY i.key LIMIT 6"
		args = []any{locale, target, search, collection}
	} else if kind != "learning" && kind != "docs" {
		return out, ErrInvalid
	}
	if len(exclude) > 0 && (kind == "learning_item" || kind == "docs_item") {
		for _, key := range exclude {
			if !keyPattern.MatchString(key) {
				return out, ErrInvalid
			}
			args = append(args, key)
		}
		query = strings.Replace(query, " ORDER BY i.key", " AND i.key NOT IN ("+strings.TrimSuffix(strings.Repeat("?,", len(exclude)), ",")+") ORDER BY i.key", 1)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ReferenceChoice
		if err = rows.Scan(&item.Key, &item.Title, &item.Kind, &item.Version); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if len(out.Items) > 5 {
		out.Items = out.Items[:5]
		out.More = true
	}
	return out, rows.Err()
}
