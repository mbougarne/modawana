package content

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mbougarne/modawana/backend/internal/forms"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

func (s *Service) CheckPublishedForm(ctx context.Context, tx *sql.Tx, in forms.Submission) error {
	visible, err := siteconfig.LocaleVisibleIn(ctx, tx, in.Locale)
	if err != nil {
		return err
	}
	if !visible || !keyPattern.MatchString(in.Page) || !blockID.MatchString(in.Block) {
		return sql.ErrNoRows
	}
	var file storage.File
	err = tx.QueryRowContext(ctx, `SELECT r.markdown_path,r.sha256,r.byte_size FROM content_items i JOIN content_versions v
		ON v.item_id=i.id JOIN revisions r ON r.id=v.published_revision_id
		WHERE i.key=? AND i.kind='page' AND v.locale=? AND r.id=?`, in.Page, in.Locale, in.Revision).Scan(&file.Key, &file.SHA256, &file.Size)
	if err != nil {
		return err
	}
	if !file.HasPageBlocks() {
		return sql.ErrNoRows
	}
	raw, err := s.Files.Read(file)
	if err != nil {
		return err
	}
	_, refs, err := parsePageBlocks(raw, nil)
	if err != nil {
		return storage.ErrIntegrity
	}
	if refs.forms[in.Block] != in.Reference {
		return sql.ErrNoRows
	}
	return nil
}

// AnalyticsFormPath is used only for the accepted submission's public route.
func (s *Service) AnalyticsFormPath(ctx context.Context, tx *sql.Tx, in forms.Submission) (string, error) {
	var unlisted bool
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(json_extract(seo,'$.no_index'),0) FROM revisions WHERE id=?`, in.Revision).Scan(&unlisted); err != nil {
		return "", err
	}
	if unlisted {
		return "", sql.ErrNoRows
	}
	return s.PublicRevisionPath(ctx, tx, in.Page, in.Locale, in.Revision)
}

func validateFormPublication(ctx context.Context, tx *sql.Tx, refs blockReferences) error {
	for _, form := range refs.forms {
		if _, err := forms.Visible(ctx, tx, form); err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, forms.ErrInvalid) {
				return typeIssue("page_blocks", "form_stale")
			}
			return err
		}
	}
	return nil
}
