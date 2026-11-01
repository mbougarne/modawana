package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/media"
)

type CollectionInput struct {
	ExpectedVersion *int64 `json:"expected_version"`
	Kind            string `json:"kind"`
	Slug            string `json:"slug"`
	Title           string `json:"title"`
	Summary         string `json:"summary"`
	Visible         bool   `json:"visible"`
}
type CollectionResult struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	PlanID  string `json:"plan_id,omitempty"`
}

func (s *Service) SaveCollection(ctx context.Context, key, locale string, in CollectionInput) (CollectionResult, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return CollectionResult{}, err
	}
	defer tx.Rollback()
	out, err := s.saveCollectionIn(ctx, tx, key, locale, in)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s *Service) saveCollectionIn(ctx context.Context, tx *sql.Tx, key, locale string, in CollectionInput) (CollectionResult, error) {
	var out CollectionResult
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 0 {
		return out, typeIssue("expected_version", "collection_version")
	}
	if !keyPattern.MatchString(key) {
		return out, typeIssue("key", "collection_key")
	}
	if !localeOK(locale) {
		return out, typeIssue("locale", "collection_locale")
	}
	if in.Kind != "learning" && in.Kind != "docs" {
		return out, typeIssue("kind", "collection_kind")
	}
	if !slugPattern.MatchString(in.Slug) {
		return out, typeIssue("slug", "collection_slug")
	}
	if !strOK(in.Title, 200, false) {
		return out, typeIssue("title", "collection_title")
	}
	if !strOK(in.Summary, 500, true) {
		return out, typeIssue("summary", "collection_summary")
	}
	var kind string
	e := tx.QueryRowContext(ctx, "SELECT id,kind,version FROM collections WHERE key=? AND removed=0", key).Scan(&out.ID, &kind, &out.Version)
	if errors.Is(e, sql.ErrNoRows) {
		if *in.ExpectedVersion != 0 {
			return out, db.ErrVersionConflict
		}
		out.ID, e = db.NewID()
		if e != nil {
			return out, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO collections(id,key,kind) VALUES(?,?,?)", out.ID, key, in.Kind); e != nil {
			return out, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plan_state(collection_id) VALUES(?)", out.ID); e != nil {
			return out, e
		}
		out.Version = 1
	} else if e == nil {
		if kind != in.Kind {
			return out, ErrInvalid
		}
		if out.Version != *in.ExpectedVersion {
			return out, db.ErrVersionConflict
		}
		_, e = tx.ExecContext(ctx, "UPDATE collections SET version=version+1 WHERE id=?", out.ID)
		out.Version++
	}
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO collection_locales(collection_id,locale,slug,title,summary,visible) VALUES(?,?,?,?,?,?) ON CONFLICT(collection_id,locale) DO UPDATE SET slug=excluded.slug,title=excluded.title,summary=excluded.summary,visible=excluded.visible`, out.ID, locale, in.Slug, in.Title, in.Summary, in.Visible)
	if e != nil {
		if strings.Contains(e.Error(), "collection_locales.locale, collection_locales.slug") {
			return out, typeIssue("slug", "collection_slug_taken")
		}
		return out, constraint(e)
	}
	return out, nil
}

// AttachMember is used by draft saves and plan creation. Once assigned, moves
// between collections are unsupported; a future route-migration design is needed.
func (s *Service) AttachMember(ctx context.Context, tx *sql.Tx, item, kind, key string, position int) error {
	if !keyPattern.MatchString(key) || position < 0 || position > 500 {
		return ErrInvalid
	}
	var collection, collectionKind string
	e := tx.QueryRowContext(ctx, "SELECT id,kind FROM collections WHERE key=?", key).Scan(&collection, &collectionKind)
	if e != nil {
		return ErrInvalid
	}
	if kind != "lesson" && kind != "doc" || kind == "lesson" && collectionKind != "learning" || kind == "doc" && collectionKind != "docs" {
		return ErrInvalid
	}
	var existing string
	var oldPosition int
	e = tx.QueryRowContext(ctx, "SELECT collection_id,position FROM collection_items WHERE item_id=?", item).Scan(&existing, &oldPosition)
	if e == nil {
		if existing != collection || (position != 0 && oldPosition != position) {
			return ErrInvalid
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if position == 0 {
		if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(position),0)+1 FROM collection_items WHERE collection_id=?", collection).Scan(&position); e != nil {
			return e
		}
		if position > 500 {
			return ErrInvalid
		}
	}
	var count int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM collection_items WHERE collection_id=?", collection).Scan(&count); e != nil {
		return e
	}
	if count >= 500 {
		return ErrInvalid
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO collection_items(collection_id,item_id,position) VALUES(?,?,?)", collection, item, position)
	if e != nil {
		return constraint(e)
	}
	_, e = tx.ExecContext(ctx, "UPDATE collections SET version=version+1 WHERE id=?", collection)
	return e
}

type PlanMember struct {
	Key           string            `json:"key"`
	Announcements map[string]string `json:"announcements"`
}
type PlanInput struct {
	ExpectedVersion *int64       `json:"expected_version"`
	Members         []PlanMember `json:"members"`
}
type ApplyPlanInput struct {
	ExpectedVersion *int64 `json:"expected_version"`
	PlanID          string `json:"plan_id"`
}

func (s *Service) SavePlan(ctx context.Context, owner, key string, in PlanInput) (CollectionResult, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return CollectionResult{}, err
	}
	defer tx.Rollback()
	out, err := s.savePlanIn(ctx, tx, owner, key, in)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s *Service) savePlanIn(ctx context.Context, tx *sql.Tx, owner, key string, in PlanInput) (CollectionResult, error) {
	var out CollectionResult
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 1 || len(in.Members) > 500 {
		return out, ErrInvalid
	}
	seen := map[string]bool{}
	for _, m := range in.Members {
		if !keyPattern.MatchString(m.Key) || seen[m.Key] || len(m.Announcements) > 3 {
			return out, ErrInvalid
		}
		seen[m.Key] = true
		for locale, title := range m.Announcements {
			if !localeOK(locale) || !strOK(title, 200, false) {
				return out, ErrInvalid
			}
		}
	}
	var kind string
	e := tx.QueryRowContext(ctx, "SELECT id,kind,version FROM collections WHERE key=? AND removed=0", key).Scan(&out.ID, &kind, &out.Version)
	if e != nil {
		return out, e
	}
	if out.Version != *in.ExpectedVersion {
		return out, db.ErrVersionConflict
	}
	var existingPolicy int
	if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(p.gate_policy_version),0) FROM collection_plan_state st JOIN collection_plans p ON p.id=st.active_plan_id OR p.id=st.draft_plan_id WHERE st.collection_id=?", out.ID).Scan(&existingPolicy); e != nil {
		return out, e
	}
	if existingPolicy > 1 {
		return out, ErrInvalid
	}
	// Once public, removing membership would break its route even after unpublish.
	rows, e := tx.QueryContext(ctx, "SELECT i.key FROM collection_items m JOIN content_items i ON i.id=m.item_id WHERE m.collection_id=? AND EXISTS(SELECT 1 FROM content_versions v WHERE v.item_id=i.id AND v.first_published_at IS NOT NULL)", out.ID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var k string
		if e = rows.Scan(&k); e != nil {
			rows.Close()
			return out, e
		}
		if !seen[k] {
			rows.Close()
			return out, typeIssue("members", "collection_published_member")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out.PlanID, e = db.NewID()
	if e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plans(id,collection_id,created_by,gate_policy_version) VALUES(?,?,?,?)", out.PlanID, out.ID, owner, existingPolicy); e != nil {
		return out, e
	}
	for index, m := range in.Members {
		var item, itemKind, existing string
		e = tx.QueryRowContext(ctx, "SELECT id,kind FROM content_items WHERE key=?", m.Key).Scan(&item, &itemKind)
		if e != nil {
			return out, ErrInvalid
		}
		if itemKind == "article" || itemKind == "lesson" && kind != "learning" || itemKind == "doc" && kind != "docs" {
			return out, ErrInvalid
		}
		e = tx.QueryRowContext(ctx, "SELECT collection_id FROM collection_items WHERE item_id=?", item).Scan(&existing)
		if e == nil && existing != out.ID {
			return out, typeIssue("members", "collection_foreign_member")
		}
		if errors.Is(e, sql.ErrNoRows) {
			var next int
			if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(position),0)+1 FROM collection_items WHERE collection_id=?", out.ID).Scan(&next); e != nil {
				return out, e
			}
			if e = s.AttachMember(ctx, tx, item, itemKind, key, next); e != nil {
				return out, e
			}
		} else if e != nil {
			return out, e
		}
		announcements := m.Announcements
		if announcements == nil {
			announcements = map[string]string{}
		}
		b, _ := json.Marshal(announcements)
		if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plan_members VALUES(?,?,?,?)", out.PlanID, item, index+1, string(b)); e != nil {
			return out, e
		}
	}
	// Preserve gates on ordinary plan edits. Removing a gate is a separate explicit action.
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plan_gates SELECT ?,g.item_id,g.assessment_id FROM collection_plan_gates g JOIN collection_plan_state st ON st.draft_plan_id=g.plan_id WHERE st.collection_id=?", out.PlanID, out.ID); e != nil {
		return out, ErrInvalid
	}
	if e = validateGateOrder(ctx, tx, out.ID, out.PlanID); e != nil {
		if errors.Is(e, ErrInvalid) {
			return out, typeIssue("members", "collection_gate_order")
		}
		return out, e
	}
	_, e = tx.ExecContext(ctx, "UPDATE collection_plan_state SET draft_plan_id=? WHERE collection_id=?", out.PlanID, out.ID)
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, "UPDATE collections SET version=version+1 WHERE id=?", out.ID)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT version FROM collections WHERE id=?", out.ID).Scan(&out.Version); e != nil {
		return out, e
	}
	return out, nil
}
func (s *Service) ApplyPlan(ctx context.Context, key string, in ApplyPlanInput) (CollectionResult, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return CollectionResult{}, e
	}
	defer tx.Rollback()
	out, e := s.applyPlanIn(ctx, tx, key, in)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Service) applyPlanIn(ctx context.Context, tx *sql.Tx, key string, in ApplyPlanInput) (CollectionResult, error) {
	var out CollectionResult
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 1 || !media.ID.MatchString(in.PlanID) {
		return out, ErrInvalid
	}
	var draft string
	var policy int
	e := tx.QueryRowContext(ctx, `SELECT c.id,c.version,coalesce(st.draft_plan_id,''),p.gate_policy_version FROM collections c JOIN collection_plan_state st ON st.collection_id=c.id JOIN collection_plans p ON p.id=st.draft_plan_id WHERE c.key=?`, key).Scan(&out.ID, &out.Version, &draft, &policy)
	if e != nil {
		return out, e
	}
	if out.Version != *in.ExpectedVersion || draft != in.PlanID {
		return out, db.ErrVersionConflict
	}
	// Gates are introduced later; this implementation must not silently activate or
	// reorder a plan from a newer gated schema it cannot validate.
	var activePolicy int
	if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(p.gate_policy_version),0) FROM collection_plan_state st JOIN collection_plans p ON p.id=st.active_plan_id WHERE st.collection_id=?", out.ID).Scan(&activePolicy); e != nil {
		return out, e
	}
	if policy > 1 || activePolicy > 1 {
		return out, ErrInvalid
	}
	if e = validateGateOrder(ctx, tx, out.ID, in.PlanID); e != nil {
		if errors.Is(e, ErrInvalid) {
			return out, typeIssue("members", "collection_gate_order")
		}
		return out, e
	}
	if e = validateGateAssessments(ctx, tx, in.PlanID); e != nil {
		if errors.Is(e, learning.ErrReview) {
			return out, &TypeChangeError{Path: "members", Code: "collection_gate_review", Cause: e}
		}
		return out, e
	}
	// Reorder the membership projection atomically without transient unique collisions.
	rows, e := tx.QueryContext(ctx, "SELECT m.item_id FROM collection_items m LEFT JOIN collection_plan_members pm ON pm.item_id=m.item_id AND pm.plan_id=? WHERE m.collection_id=? ORDER BY CASE WHEN pm.position IS NULL THEN 1 ELSE 0 END,pm.position,m.position", in.PlanID, out.ID)
	if e != nil {
		return out, e
	}
	ordered := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ordered = append(ordered, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE collection_items SET position=position+1000 WHERE collection_id=?", out.ID); e != nil {
		return out, e
	}
	for i, id := range ordered {
		if _, e = tx.ExecContext(ctx, "UPDATE collection_items SET position=? WHERE collection_id=? AND item_id=?", i+1, out.ID, id); e != nil {
			return out, e
		}
	}
	_, e = tx.ExecContext(ctx, "UPDATE collection_plan_state SET active_plan_id=? WHERE collection_id=?", in.PlanID, out.ID)
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, "UPDATE collections SET version=version+1 WHERE id=?", out.ID)
	if e != nil {
		return out, e
	}
	out.Version++
	out.PlanID = in.PlanID
	return out, nil
}

func (s *Service) LessonAccess(ctx context.Context, tx *sql.Tx, item string, p access.Principal) (bool, error) {
	state, e := lessonState(ctx, tx, item, p)
	return state.Open, e
}
func (s *Service) CheckPublicationPlan(ctx context.Context, tx *sql.Tx, item, revision string, publish bool) error {
	var kind string
	if e := tx.QueryRowContext(ctx, "SELECT kind FROM content_items WHERE id=?", item).Scan(&kind); e != nil {
		return e
	}
	if kind == "article" {
		return nil
	}
	var n int
	if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM collection_items WHERE item_id=?", item).Scan(&n); e != nil {
		return e
	}
	if n != 1 {
		return ErrInvalid
	}
	var policy int
	e := tx.QueryRowContext(ctx, `SELECT p.gate_policy_version FROM collection_items m JOIN collection_plan_state st ON st.collection_id=m.collection_id JOIN collection_plans p ON p.id=st.active_plan_id WHERE m.item_id=?`, item).Scan(&policy)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if policy > 1 {
		return ErrInvalid
	}
	if publish {
		var count int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM collection_plan_gates g JOIN collection_plan_state st ON st.active_plan_id=g.plan_id WHERE g.item_id=? AND NOT EXISTS(SELECT 1 FROM quiz_bindings b WHERE b.revision_id=? AND b.assessment_id=g.assessment_id)", item, revision).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return learning.ErrReview
		}
	}
	return nil
}
