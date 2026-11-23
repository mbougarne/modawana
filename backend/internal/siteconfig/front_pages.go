package siteconfig

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mbougarne/modawana/backend/internal/config"
)

// FrontPage resolves an applied selection only while its exact language has a
// published standalone page. Routing and canonical generation share it.
func FrontPage(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, locale string) (key, slug string, err error) {
	err = q.QueryRowContext(ctx, `SELECT i.key,v.slug FROM content_items i
		JOIN content_versions v ON v.item_id=i.id
		JOIN revisions rev ON rev.id=v.published_revision_id AND rev.version_id=v.id
		WHERE i.kind='page' AND v.locale=? AND i.key=(
			SELECT json_extract(r.snapshot_json,?) FROM config_documents d
			JOIN config_revisions r ON r.id=d.applied_revision_id WHERE d.kind='site'
		)`, locale, "$.front_pages."+locale).Scan(&key, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return key, slug, err
}

func validateFrontPageReferences(ctx context.Context, q query, site Site) error {
	var issues []FieldIssue
	for _, locale := range config.SupportedLocales() {
		if key := site.FrontPages[locale]; key != "" {
			var one int
			err := q.QueryRowContext(ctx, `SELECT 1 FROM content_items i JOIN content_versions v ON v.item_id=i.id
				WHERE i.kind='page' AND i.key=? AND v.locale=?`, key, locale).Scan(&one)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				issues = append(issues, FieldIssue{"front_pages." + locale, "menu_page"})
			case err != nil:
				return err
			}
		}
	}
	if len(issues) != 0 {
		return &ValidationError{Fields: issues}
	}
	return nil
}
