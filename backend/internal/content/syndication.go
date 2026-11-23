package content

import (
	"context"
	"database/sql"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/access"
)

// SyndicatedPage contains only published metadata for an anonymous reader.
type SyndicatedPage struct {
	ID, URL, CollectionURL, Title, Summary, Published string
}

type syndicationCandidate struct {
	SyndicatedPage
	key, kind, locale, slug string
}

const syndicationBatch = 128

// SyndicatedPages selects the newest publicly readable pages. An empty locale
// includes all languages for the sitemap. Publication, routing, and anonymous
// access use the same snapshot. No revision file or learner state is loaded.
func (s *Service) SyndicatedPages(ctx context.Context, locale string, limit int, includePages bool) ([]SyndicatedPage, error) {
	if (locale != "" && !localeOK(locale)) || limit < 1 {
		return nil, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	pages := []SyndicatedPage{}
	var after syndicationCandidate
	for len(pages) < limit {
		batch, err := syndicationCandidates(ctx, tx, locale, after, includePages)
		if err != nil {
			return nil, err
		}
		for _, row := range batch {
			decision, err := s.Access.ResolveIn(ctx, tx, access.Request{
				Principal: access.Principal{Audience: access.Anonymous},
				Surface:   access.Feed, ResourceID: row.ID, Locale: row.locale,
			})
			if err != nil {
				return nil, err
			}
			if !decision.AllowsBody() {
				continue
			}
			row.URL, err = canonical(ctx, tx, row.key, row.locale, row.kind, row.slug)
			if err != nil {
				return nil, err
			}
			if row.URL == "" {
				continue
			}
			if row.kind == "lesson" || strings.HasPrefix(row.URL, "/"+row.locale+"/docs/") {
				row.CollectionURL = row.URL[:strings.LastIndexByte(row.URL, '/')]
			}
			pages = append(pages, row.SyndicatedPage)
			if len(pages) == limit {
				return pages, nil
			}
		}
		if len(batch) < syndicationBatch {
			break
		}
		after = batch[len(batch)-1]
	}
	return pages, nil
}

func syndicationCandidates(ctx context.Context, tx *sql.Tx, locale string, after syndicationCandidate, includePages bool) ([]syndicationCandidate, error) {
	statement := `SELECT v.id,i.key,i.kind,v.locale,v.slug,r.title,r.summary,v.last_published_at
		FROM content_versions v JOIN content_items i ON i.id=v.item_id JOIN revisions r ON r.id=v.published_revision_id
		WHERE v.published_revision_id IS NOT NULL`
	var args []any
	if !includePages {
		statement += " AND i.kind!='page'"
	}
	if locale != "" {
		statement += " AND v.locale=?"
		args = append(args, locale)
	}
	if after.ID != "" {
		statement += " AND (v.last_published_at<? OR (v.last_published_at=? AND v.id>?))"
		args = append(args, after.Published, after.Published, after.ID)
	}
	statement += " ORDER BY v.last_published_at DESC,v.id LIMIT ?"
	rows, err := tx.QueryContext(ctx, statement, append(args, syndicationBatch)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batch := make([]syndicationCandidate, 0, syndicationBatch)
	for rows.Next() {
		var row syndicationCandidate
		if err := rows.Scan(&row.ID, &row.key, &row.kind, &row.locale, &row.slug, &row.Title, &row.Summary, &row.Published); err != nil {
			return nil, err
		}
		batch = append(batch, row)
	}
	return batch, rows.Err()
}
