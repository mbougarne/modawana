package content

import (
	"context"
	"strings"
)

type OwnerEntry struct {
	DraftRevisionID     string `json:"draft_revision_id"`
	PublishedRevisionID string `json:"published_revision_id"`
	Key                 string `json:"key"`
	Kind                string `json:"kind"`
	SourceLocale        string `json:"source_locale"`
	Locale              string `json:"locale"`
	Title               string `json:"title"`
	Slug                string `json:"slug"`
	Status              string `json:"status"`
	Version             int64  `json:"version"`
}

func (s *Service) OwnerList(ctx context.Context, locale, kind, status, search, cursor string) ([]OwnerEntry, string, error) {
	out := []OwnerEntry{}
	if !localeOK(locale) || (kind != "" && kind != "article" && kind != "lesson" && kind != "doc") || (status != "" && status != "draft" && status != "published" && status != "changed") || !strOK(search, 200, true) || (cursor != "" && !keyPattern.MatchString(cursor)) {
		return nil, "", ErrInvalid
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT i.key,i.kind,i.source_locale,v.locale,r.title,v.slug,CASE WHEN v.published_revision_id IS NULL THEN 'draft' WHEN v.published_revision_id=v.draft_revision_id THEN 'published' ELSE 'changed' END AS state,v.version,v.draft_revision_id,coalesce(v.published_revision_id,'') FROM content_items i JOIN content_versions v ON v.item_id=i.id JOIN revisions r ON r.id=v.draft_revision_id WHERE v.locale=? AND i.key>? AND (?='' OR i.kind=?) AND (?='' OR state=?) AND (instr(lower(r.title),lower(?))>0 OR instr(lower(i.key),lower(?))>0) ORDER BY i.key LIMIT 51`, locale, cursor, kind, kind, status, status, strings.TrimSpace(search), strings.TrimSpace(search))
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	for rows.Next() {
		var item OwnerEntry
		if e = rows.Scan(&item.Key, &item.Kind, &item.SourceLocale, &item.Locale, &item.Title, &item.Slug, &item.Status, &item.Version, &item.DraftRevisionID, &item.PublishedRevisionID); e != nil {
			return nil, "", e
		}
		out = append(out, item)
	}
	next := ""
	if len(out) > 50 {
		out = out[:50]
		next = out[49].Key
	}
	return out, next, rows.Err()
}
