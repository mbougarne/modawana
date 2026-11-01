package content

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
)

type LessonState struct {
	Open         bool   `json:"open"`
	Plan         string `json:"plan"`
	Epoch        int64  `json:"epoch"`
	Collection   string `json:"-"`
	Prerequisite string `json:"-"`
}

func lessonState(ctx context.Context, tx *sql.Tx, item string, p access.Principal) (LessonState, error) {
	var out LessonState
	var policy, position int
	e := tx.QueryRowContext(ctx, `SELECT st.collection_id,p.id,st.epoch,p.gate_policy_version,pm.position FROM collection_items m JOIN collection_plan_state st ON st.collection_id=m.collection_id JOIN collection_plans p ON p.id=st.active_plan_id JOIN collection_plan_members pm ON pm.plan_id=p.id AND pm.item_id=m.item_id WHERE m.item_id=?`, item).Scan(&out.Collection, &out.Plan, &out.Epoch, &policy, &position)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil || policy > 1 {
		return out, e
	}
	identity := ""
	if p.Audience == access.Learner {
		identity = p.ID
	}
	// Every preceding gate counts, even when its source is no longer published.
	e = tx.QueryRowContext(ctx, `SELECT g.item_id FROM collection_plan_gates g JOIN collection_plan_members m ON m.plan_id=g.plan_id AND m.item_id=g.item_id WHERE g.plan_id=? AND m.position<? AND NOT EXISTS(SELECT 1 FROM learning_completion c JOIN learning_identities i ON i.id=c.identity_id WHERE c.identity_id=? AND c.item_id=g.item_id AND c.collection_id=? AND c.epoch=? AND c.method='quiz_pass' AND i.expires_at>strftime('%Y-%m-%dT%H:%M:%fZ','now')) ORDER BY m.position LIMIT 1`, out.Plan, position, identity, out.Collection, out.Epoch).Scan(&out.Prerequisite)
	if errors.Is(e, sql.ErrNoRows) {
		out.Open = true
		return out, nil
	}
	return out, e
}

func validateGateOrder(ctx context.Context, tx *sql.Tx, collection, plan string) error {
	var n int
	// Existing gated targets may not cross their prerequisite in an ordinary reorder.
	// Explicitly remove the gate first if changing this policy is intentional.
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM collection_plan_state st JOIN collection_plan_gates old ON old.plan_id=st.active_plan_id JOIN collection_plan_members source ON source.plan_id=old.plan_id AND source.item_id=old.item_id JOIN collection_plan_members target ON target.plan_id=old.plan_id AND target.position>source.position JOIN collection_plan_gates retained ON retained.plan_id=? AND retained.item_id=old.item_id LEFT JOIN collection_plan_members ns ON ns.plan_id=retained.plan_id AND ns.item_id=source.item_id LEFT JOIN collection_plan_members nt ON nt.plan_id=retained.plan_id AND nt.item_id=target.item_id WHERE st.collection_id=? AND (ns.item_id IS NULL OR nt.item_id IS NULL OR ns.position>=nt.position)`, plan, collection).Scan(&n)
	if e != nil {
		return e
	}
	if n != 0 {
		return ErrInvalid
	}
	return nil
}
func validateGateAssessments(ctx context.Context, tx *sql.Tx, plan string) error {
	var n int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM collection_plan_gates g JOIN content_items i ON i.id=g.item_id JOIN content_versions v ON v.item_id=i.id AND v.locale=i.source_locale WHERE g.plan_id=? AND v.published_revision_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM quiz_bindings b JOIN assessment_reviews r ON r.assessment_id=b.assessment_id WHERE b.revision_id=v.published_revision_id AND b.assessment_id=g.assessment_id)`, plan).Scan(&n)
	if e != nil {
		return e
	}
	if n != 0 {
		return learning.ErrReview
	}
	// New/replaced gates require the reviewed, currently published source assessment.
	// Retained gates stay effective during temporary unpublication.
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM collection_plan_gates g JOIN collection_plans p ON p.id=g.plan_id JOIN collection_plan_state st ON st.collection_id=p.collection_id WHERE g.plan_id=? AND NOT EXISTS(SELECT 1 FROM collection_plan_gates old WHERE old.plan_id=st.active_plan_id AND old.item_id=g.item_id AND old.assessment_id=g.assessment_id) AND NOT EXISTS(SELECT 1 FROM content_items i JOIN content_versions v ON v.item_id=i.id AND v.locale=i.source_locale JOIN quiz_bindings b ON b.revision_id=v.published_revision_id JOIN assessment_reviews r ON r.assessment_id=b.assessment_id WHERE i.id=g.item_id AND b.assessment_id=g.assessment_id)`, plan).Scan(&n)
	if e != nil {
		return e
	}
	if n != 0 {
		return learning.ErrReview
	}
	return nil
}

// ResetPathProgress is separate from normal publication and plan edits.
// Its HTTP caller requires a freshly authenticated owner session.
func (s *Service) ResetPathProgress(ctx context.Context, key string, expected int64, authorize func(*sql.Tx) error) (int64, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if authorize == nil {
		return 0, ErrInvalid
	}
	if e = authorize(tx); e != nil {
		return 0, e
	}
	var id string
	var version, epoch int64
	e = tx.QueryRowContext(ctx, `SELECT c.id,c.version,st.epoch FROM collections c JOIN collection_plan_state st ON st.collection_id=c.id WHERE c.key=? AND c.kind='learning'`, key).Scan(&id, &version, &epoch)
	if e != nil {
		return 0, e
	}
	if version != expected {
		return 0, db.ErrVersionConflict
	}
	if _, e = tx.ExecContext(ctx, "UPDATE collection_plan_state SET epoch=epoch+1 WHERE collection_id=?", id); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE collections SET version=version+1 WHERE id=?", id); e != nil {
		return 0, e
	}
	// Prior epochs cannot grant access; remove old achievements to keep storage bounded.
	if _, e = tx.ExecContext(ctx, "DELETE FROM learning_completion WHERE collection_id=?", id); e != nil {
		return 0, e
	}
	return epoch + 1, tx.Commit()
}
