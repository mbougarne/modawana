package content

import (
	"context"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

// RemoveCollection retains revisions and immutable plans for audit and learning history.
// Lessons become unpublished standalone articles so detachment never bypasses a gate.
func (s *Service) RemoveCollection(ctx context.Context, owner, key string, version int64) error {
	return s.Files.Exclusive(ctx, func() error {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = access.CheckMutation(ctx, tx, ""); err != nil {
			return err
		}
		var id string
		var current int64
		if err = tx.QueryRowContext(ctx, "SELECT id,version FROM collections WHERE key=? AND removed=0", key).Scan(&id, &current); err != nil {
			return err
		}
		if current != version {
			return db.ErrVersionConflict
		}
		statements := []string{
			"UPDATE collection_plan_state SET active_plan_id=NULL,draft_plan_id=NULL WHERE collection_id=?",
			"INSERT INTO content_type_conversion_guard SELECT i.id,i.kind,'article' FROM content_items i JOIN collection_items m ON m.item_id=i.id WHERE m.collection_id=? AND i.kind IN ('lesson','doc')",

			"UPDATE content_versions SET published_revision_id=NULL,version=version+1 WHERE item_id IN (SELECT i.id FROM content_items i JOIN collection_items m ON m.item_id=i.id WHERE m.collection_id=? AND i.kind='lesson')",
			"DELETE FROM collection_items WHERE collection_id=?",
			"UPDATE content_items SET kind='article' WHERE kind='lesson' AND id IN (SELECT item_id FROM content_type_conversion_guard) AND EXISTS(SELECT 1 FROM collections WHERE id=?)",
			"UPDATE collection_locales SET visible=0 WHERE collection_id=?",
			"UPDATE collection_plan_state SET active_plan_id=NULL WHERE collection_id=?",
			"UPDATE collections SET removed=1,version=version+1 WHERE id=?",
		}
		for _, statement := range statements {
			if _, err = tx.ExecContext(ctx, statement, id); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO content_type_events(id,item_id,from_kind,to_kind,created_by) SELECT lower(hex(randomblob(16))),item_id,from_kind,to_kind,? FROM content_type_conversion_guard WHERE from_kind='lesson'", owner); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM content_type_conversion_guard"); err != nil {
			return err
		}
		return tx.Commit()
	})
}
