package content

import (
	"context"
	"database/sql"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
)

type QuizGateInput struct {
	ExpectedVersion *int64 `json:"expected_version"`
	PlanID          string `json:"plan_id"`
	AssessmentID    string `json:"assessment_id"`
	Enabled         bool   `json:"enabled"`
}

// SaveQuizGate clones the draft plan. Public gated activation is implemented in 031.
func (s *Service) SaveQuizGate(ctx context.Context, owner, key, locale string, in QuizGateInput) (CollectionResult, error) {
	var out CollectionResult
	if in.ExpectedVersion == nil {
		return out, ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return out, e
	}
	if v.Kind != "lesson" {
		return out, ErrInvalid
	}
	var plan, kind string
	e = tx.QueryRowContext(ctx, `SELECT c.id,c.kind,c.version,coalesce(st.draft_plan_id,'') FROM collection_items m JOIN collections c ON c.id=m.collection_id JOIN collection_plan_state st ON st.collection_id=c.id WHERE m.item_id=?`, v.Item).Scan(&out.ID, &kind, &out.Version, &plan)
	if e != nil {
		return out, e
	}
	if kind != "learning" || plan == "" {
		return out, ErrInvalid
	}
	if plan != in.PlanID || out.Version != *in.ExpectedVersion {
		return out, db.ErrVersionConflict
	}
	if in.Enabled {
		var count int
		e = tx.QueryRowContext(ctx, `SELECT count(*) FROM collection_plan_members pm JOIN quiz_bindings b ON b.revision_id=? JOIN assessment_reviews ar ON ar.assessment_id=b.assessment_id WHERE pm.plan_id=? AND pm.item_id=? AND b.assessment_id=?`, v.Draft, plan, v.Item, in.AssessmentID).Scan(&count)
		if e != nil {
			return out, e
		}
		if count != 1 {
			return out, learning.ErrReview
		}
	}
	var remaining int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM collection_plan_gates WHERE plan_id=? AND item_id!=?", plan, v.Item).Scan(&remaining); e != nil {
		return out, e
	}
	policy := 0
	if in.Enabled || remaining > 0 {
		policy = 1
	}
	out.PlanID, e = db.NewID()
	if e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plans(id,collection_id,created_by,gate_policy_version) VALUES(?,?,?,?)", out.PlanID, out.ID, owner, policy); e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plan_members SELECT ?,item_id,position,announcements_json FROM collection_plan_members WHERE plan_id=?", out.PlanID, plan); e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plan_gates SELECT ?,item_id,assessment_id FROM collection_plan_gates WHERE plan_id=? AND item_id!=?", out.PlanID, plan, v.Item); e != nil {
		return out, e
	}
	if in.Enabled {
		if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plan_gates VALUES(?,?,?)", out.PlanID, v.Item, in.AssessmentID); e != nil {
			return out, e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE collection_plan_state SET draft_plan_id=? WHERE collection_id=?", out.PlanID, out.ID); e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE collections SET version=version+1 WHERE id=?", out.ID); e != nil {
		return out, e
	}
	out.Version++
	return out, tx.Commit()
}

type QuizPath struct {
	Key     string `json:"key"`
	Version int64  `json:"version"`
	PlanID  string `json:"plan_id"`
	Enabled bool   `json:"enabled"`
}

func (s *Service) QuizPath(ctx context.Context, key, locale string) (QuizPath, error) {
	var out QuizPath
	v, e := loadVersion(ctx, s.DB, key, locale)
	if e != nil {
		return out, e
	}
	if v.Kind != "lesson" {
		return out, sql.ErrNoRows
	}
	e = s.DB.QueryRowContext(ctx, `SELECT c.key,c.version,coalesce(st.draft_plan_id,''),EXISTS(SELECT 1 FROM collection_plan_gates g WHERE g.plan_id=st.draft_plan_id AND g.item_id=?) FROM collection_items m JOIN collections c ON c.id=m.collection_id JOIN collection_plan_state st ON st.collection_id=c.id WHERE m.item_id=? AND c.kind='learning'`, v.Item, v.Item).Scan(&out.Key, &out.Version, &out.PlanID, &out.Enabled)
	return out, e
}
