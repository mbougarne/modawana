package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"time"
)

type ReaderLearning struct {
	Version       learning.Version     `json:"version"`
	Quiz          *learning.PublicQuiz `json:"quiz,omitempty"`
	Completed     bool                 `json:"completed"`
	CanMarkRead   bool                 `json:"can_mark_read"`
	Unavailable   bool                 `json:"unavailable"`
	Alternatives  []Alternative        `json:"alternatives"`
	Prerequisites []Alternative        `json:"prerequisites"`
}

func (s *Service) readerLearning(ctx context.Context, tx *sql.Tx, v version, p access.Principal) (ReaderLearning, error) {
	out := ReaderLearning{Version: learning.Version{Content: v.Published}, Alternatives: []Alternative{}, Prerequisites: []Alternative{}}
	state := LessonState{Open: true, Epoch: 1}
	var e error
	if v.Kind == "lesson" {
		state, e = lessonState(ctx, tx, v.Item, p)
		if e != nil {
			return out, e
		}
		out.Version.Path = state.Plan
		out.Version.Epoch = state.Epoch
	}
	if !state.Open {
		out.Unavailable = true
		if state.Prerequisite != "" {
			out.Prerequisites, e = alternatives(ctx, tx, state.Prerequisite)
			out.Unavailable = len(out.Prerequisites) == 0
		}
		return out, e
	}
	d, e := learning.Read(ctx, tx, v.Published)
	if e != nil {
		return out, e
	}
	if d != nil {
		out.Version.Quiz = d.AssessmentID
		// A stale translation remains readable, but cannot award a new pass.
		var current string
		e = tx.QueryRowContext(ctx, `SELECT coalesce(b.assessment_id,'') FROM content_versions v LEFT JOIN quiz_bindings b ON b.revision_id=v.published_revision_id WHERE v.item_id=? AND v.locale=?`, v.Item, v.Source).Scan(&current)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		if current != d.AssessmentID {
			out.Unavailable = true
			out.Alternatives, e = alternatives(ctx, tx, v.Item)
			if e != nil {
				return out, e
			}
		} else {
			q := learning.Public(*d, v.Published, v.Locale)
			q.Version = out.Version
			if state.Plan != "" {
				var n int
				e = tx.QueryRowContext(ctx, "SELECT count(*) FROM collection_plan_gates WHERE plan_id=? AND item_id=? AND assessment_id=?", state.Plan, v.Item, d.AssessmentID).Scan(&n)
				if e != nil {
					return out, e
				}
				q.UnlocksNext = n == 1
			}
			out.Quiz = &q
		}
	} else {
		out.CanMarkRead = v.Kind == "lesson"
	}
	if p.Audience == access.Learner {
		var n int
		e = tx.QueryRowContext(ctx, `SELECT count(*) FROM learning_completion c JOIN learning_identities i ON i.id=c.identity_id WHERE c.identity_id=? AND c.item_id=? AND c.epoch=? AND i.expires_at>?`, p.ID, v.Item, state.Epoch, db.UTC(time.Now())).Scan(&n)
		if e != nil {
			return out, e
		}
		out.Completed = n == 1
	}
	return out, nil
}

func (s *Service) ReaderQuiz(ctx context.Context, key, locale string, p access.Principal) (ReaderLearning, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return ReaderLearning{}, e
	}
	defer tx.Rollback()
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return ReaderLearning{}, e
	}
	if v.Published == "" {
		return ReaderLearning{}, sql.ErrNoRows
	}
	decision, e := s.Access.ResolveIn(ctx, tx, access.Request{Principal: p, Surface: access.Content, ResourceID: v.ID})
	if e != nil {
		return ReaderLearning{}, e
	}
	if decision.Visibility == access.Hidden {
		return ReaderLearning{}, sql.ErrNoRows
	}
	out, e := s.readerLearning(ctx, tx, v, p)
	if e != nil {
		return out, e
	}
	if !decision.AllowsBody() {
		return out, ErrLocked
	}
	return out, nil
}

// SubmitQuiz owns the single atomic snapshot for access, versions, grading,
// attempt replay and durable achievement. No caller-supplied success is trusted.
func (s *Service) SubmitQuiz(ctx context.Context, key, locale string, p access.Principal, in learning.Submission) (learning.Result, error) {
	var out learning.Result
	if p.Audience != access.Learner {
		return out, learning.ErrIdentity
	}
	if !retryPattern.MatchString(in.IdempotencyKey) {
		return out, ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM learning_identities WHERE id=? AND expires_at>?", p.ID, db.UTC(time.Now())).Scan(&n); e != nil {
		return out, e
	}
	if n != 1 {
		return out, learning.ErrIdentity
	}
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return out, e
	}
	if v.Published == "" {
		return out, sql.ErrNoRows
	}
	decision, e := s.Access.ResolveIn(ctx, tx, access.Request{Principal: p, Surface: access.Content, ResourceID: v.ID})
	if e != nil {
		return out, e
	}
	if decision.Visibility == access.Hidden {
		return out, sql.ErrNoRows
	}
	if !decision.AllowsBody() {
		return out, ErrLocked
	}
	requestHash := hash(struct {
		Key, Locale string
		Submission  learning.Submission
	}{key, locale, in})
	retryHash := hash(in.IdempotencyKey)
	var previous, stored string
	e = tx.QueryRowContext(ctx, "SELECT request_hash,feedback_json FROM learning_attempts WHERE identity_id=? AND idempotency_hash=?", p.ID, retryHash).Scan(&previous, &stored)
	if e == nil {
		if previous != requestHash {
			return out, ErrIdempotency
		}
		e = json.Unmarshal([]byte(stored), &out)
		return out, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	state, e := s.readerLearning(ctx, tx, v, p)
	if e != nil {
		return out, e
	}
	if state.Quiz == nil || state.Version != in.Version {
		return out, db.ErrVersionConflict
	}
	// The retained ledger also enforces the per-identity ceiling across restarts.
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM learning_attempts WHERE identity_id=? AND created_at>?", p.ID, db.UTC(time.Now().Add(-time.Minute))).Scan(&n); e != nil {
		return out, e
	}
	if n >= 10 {
		return out, learning.ErrLimited
	}
	definition, e := learning.Read(ctx, tx, v.Published)
	if e != nil {
		return out, e
	}
	out, e = learning.Grade(*definition, v.Published, in.Answers)
	if e != nil {
		return out, e
	}
	out.Version = state.Version
	epoch := state.Version.Epoch
	if epoch == 0 {
		epoch = 1
	}
	var collection string
	if v.Kind == "lesson" {
		if e = tx.QueryRowContext(ctx, "SELECT collection_id FROM collection_items WHERE item_id=?", v.Item).Scan(&collection); e != nil {
			return out, e
		}
	}
	now := db.UTC(time.Now())
	if out.Passed {
		if _, e = tx.ExecContext(ctx, `INSERT INTO learning_completion(identity_id,item_id,collection_id,epoch,method,assessment_id,revision_id,created_at) VALUES(?,?,?,?,'quiz_pass',?,?,?) ON CONFLICT(identity_id,item_id,epoch) DO UPDATE SET method='quiz_pass',assessment_id=excluded.assessment_id,revision_id=excluded.revision_id WHERE learning_completion.method='read'`, p.ID, v.Item, nullable(collection), epoch, definition.AssessmentID, v.Published, now); e != nil {
			return out, e
		}
	}
	out.Completed = out.Passed || state.Completed
	if v.Kind == "lesson" {
		out.UnlockedHref, e = s.nextLesson(ctx, tx, v.Item, locale, p)
		if e != nil {
			return out, e
		}
	}
	id, e := db.NewID()
	if e != nil {
		return out, e
	}
	b, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO learning_attempts(id,identity_id,assessment_id,revision_id,path_plan_id,epoch,score,total,passed,feedback_json,idempotency_hash,request_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, p.ID, definition.AssessmentID, v.Published, nullable(state.Version.Path), epoch, out.Score, out.Total, out.Passed, string(b), retryHash, requestHash, now); e != nil {
		return out, e
	}
	return out, tx.Commit()
}

func (s *Service) nextLesson(ctx context.Context, tx *sql.Tx, item, locale string, p access.Principal) (string, error) {
	var id, key, kind, slug string
	e := tx.QueryRowContext(ctx, `SELECT v.id,i.key,i.kind,v.slug FROM collection_items m JOIN collection_plan_state st ON st.collection_id=m.collection_id JOIN collection_plan_members current ON current.plan_id=st.active_plan_id AND current.item_id=m.item_id JOIN collection_plan_members next ON next.plan_id=current.plan_id AND next.position=current.position+1 JOIN content_items i ON i.id=next.item_id JOIN content_versions v ON v.item_id=i.id AND v.locale=? AND v.published_revision_id IS NOT NULL WHERE m.item_id=?`, locale, item).Scan(&id, &key, &kind, &slug)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	d, e := s.Access.ResolveIn(ctx, tx, access.Request{Principal: p, Surface: access.Content, ResourceID: id})
	if e != nil || !d.AllowsBody() {
		return "", e
	}
	return canonical(ctx, tx, key, locale, kind, slug)
}

func (s *Service) MarkRead(ctx context.Context, key, locale string, p access.Principal, expected learning.Version) (ReaderLearning, error) {
	var out ReaderLearning
	if p.Audience != access.Learner {
		return out, learning.ErrIdentity
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM learning_identities WHERE id=? AND expires_at>?", p.ID, db.UTC(time.Now())).Scan(&n); e != nil {
		return out, e
	}
	if n != 1 {
		return out, learning.ErrIdentity
	}
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return out, e
	}
	if v.Published == "" {
		return out, sql.ErrNoRows
	}
	decision, e := s.Access.ResolveIn(ctx, tx, access.Request{Principal: p, Surface: access.Content, ResourceID: v.ID})
	if e != nil {
		return out, e
	}
	if decision.Visibility == access.Hidden {
		return out, sql.ErrNoRows
	}
	state, e := lessonState(ctx, tx, v.Item, p)
	if e != nil {
		return out, e
	}
	if !state.Open {
		return out, ErrLocked
	}
	out, e = s.readerLearning(ctx, tx, v, p)
	if e != nil {
		return out, e
	}
	if out.Version != expected {
		return out, db.ErrVersionConflict
	}
	if !out.CanMarkRead || out.Quiz != nil {
		return out, ErrInvalid
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO learning_completion(identity_id,item_id,collection_id,epoch,method,revision_id,created_at) VALUES(?,?,?,?,'read',?,?) ON CONFLICT DO NOTHING`, p.ID, v.Item, state.Collection, state.Epoch, v.Published, db.UTC(time.Now()))
	if e != nil {
		return out, e
	}
	out.Completed = true
	return out, tx.Commit()
}
