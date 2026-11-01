package content

import (
	"context"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
)

const MaxBulkPublication = 500

type BulkPublicationItem struct {
	Key             string `json:"key"`
	Locale          string `json:"locale"`
	ExpectedVersion *int64 `json:"expected_version"`
	RevisionID      string `json:"revision_id"`
}
type BulkPublication struct {
	Operation string                `json:"operation"`
	Items     []BulkPublicationItem `json:"items"`
}
type BulkPublicationResult struct {
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

// ChangePublicationBulk changes only publication pointers, under the same checks
// as the editor. Any failure rolls back the whole selection; files are untouched.
func (s *Service) ChangePublicationBulk(ctx context.Context, owner string, in BulkPublication) (BulkPublicationResult, error) {
	var out BulkPublicationResult
	if (in.Operation != "publish" && in.Operation != "unpublish") || len(in.Items) == 0 || len(in.Items) > MaxBulkPublication {
		return out, ErrInvalid
	}
	seen := map[string]bool{}
	for _, item := range in.Items {
		id := item.Key + "/" + item.Locale
		if !keyPattern.MatchString(item.Key) || !localeOK(item.Locale) || seen[id] || item.ExpectedVersion == nil || *item.ExpectedVersion < 1 || !media.ID.MatchString(item.RevisionID) {
			return out, ErrInvalid
		}
		seen[id] = true
	}
	err := s.Files.Exclusive(ctx, func() error {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = access.CheckMutation(ctx, tx, ""); err != nil {
			return err
		}
		for _, item := range in.Items {
			v, err := loadVersion(ctx, tx, item.Key, item.Locale)
			if err != nil {
				return err
			}
			if v.Number != *item.ExpectedVersion {
				return db.ErrVersionConflict
			}
			// No-op selections still bind to an exact observed revision/version.
			if in.Operation == "unpublish" && v.Published == "" {
				if item.RevisionID != v.Draft {
					return db.ErrVersionConflict
				}
				out.Unchanged++
				continue
			}
			if in.Operation == "publish" && v.Published == v.Draft && item.RevisionID == v.Draft {
				out.Unchanged++
				continue
			}
			if _, err = s.changePublicationIn(ctx, tx, owner, item.Key, item.Locale, in.Operation, Action{ExpectedVersion: item.ExpectedVersion, RevisionID: item.RevisionID}); err != nil {
				return err
			}
			out.Changed++
		}
		return tx.Commit()
	})
	if err != nil {
		return BulkPublicationResult{}, err
	}
	return out, nil
}
