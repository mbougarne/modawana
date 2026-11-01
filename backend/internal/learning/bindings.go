package learning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/db"
)

type Query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Bind executes in the same transaction as its immutable content revision.
func Bind(ctx context.Context, tx *sql.Tx, item, revision, locale, source, sourceRevision, kind string, d *Definition) error {
	if d == nil {
		return nil
	}
	if kind == "doc" {
		return ErrInvalid
	}
	c, w, e := split(*d)
	if e != nil {
		return e
	}
	raw := encode(c)
	id := d.AssessmentID
	if id != "" {
		var existing, owner string
		if e = tx.QueryRowContext(ctx, "SELECT item_id,definition_json FROM assessments WHERE id=?", id).Scan(&owner, &existing); e != nil || owner != item || existing != raw {
			return ErrInvalid
		}
	}
	if locale != source {
		var canonicalID string
		if id == "" {
			return ErrInvalid
		}
		if e = tx.QueryRowContext(ctx, "SELECT assessment_id FROM quiz_bindings WHERE revision_id=?", sourceRevision).Scan(&canonicalID); e != nil || canonicalID != id {
			return ErrInvalid
		}
	}
	if id == "" {
		e = tx.QueryRowContext(ctx, "SELECT id FROM assessments WHERE item_id=? AND canonical_hash=?", item, digest([]byte(raw))).Scan(&id)
		if errors.Is(e, sql.ErrNoRows) {
			id, e = db.NewID()
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO assessments(id,item_id,canonical_hash,definition_json) VALUES(?,?,?,?)", id, item, digest([]byte(raw)), raw)
		}
		if e != nil {
			return e
		}
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO quiz_bindings(revision_id,assessment_id,locale,wording_json) VALUES(?,?,?,?)", revision, id, locale, encode(w))
	return e
}
func Read(ctx context.Context, q Query, revision string) (*Definition, error) {
	var d Definition
	var raw, localized string
	e := q.QueryRowContext(ctx, "SELECT a.id,a.definition_json,b.wording_json FROM quiz_bindings b JOIN assessments a ON a.id=b.assessment_id WHERE b.revision_id=?", revision).Scan(&d.AssessmentID, &raw, &localized)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var c canonical
	var w wording
	if e = json.Unmarshal([]byte(raw), &c); e != nil {
		return nil, e
	}
	if e = json.Unmarshal([]byte(localized), &w); e != nil {
		return nil, e
	}
	if len(w.Questions) != len(c.Questions) || len(w.Explanations) != len(c.Questions) {
		return nil, ErrInvalid
	}
	d.PassMark = c.PassMark
	d.ShowExplanations = c.ShowExplanations
	for i, q := range w.Questions {
		d.Questions = append(d.Questions, PrivateQuestion{q, c.Questions[i].Correct, w.Explanations[i]})
	}
	return &d, nil
}

// ReviewPublication requires an exact localized binding and current source assessment.
func ReviewPublication(ctx context.Context, tx *sql.Tx, owner, item, revision, locale, source, op string) error {
	if op == "unpublish" {
		return nil
	}
	d, e := Read(ctx, tx, revision)
	if e != nil || d == nil {
		return e
	}
	if locale == source {
		return nil
	}
	var current string
	e = tx.QueryRowContext(ctx, `SELECT b.assessment_id FROM content_versions v JOIN quiz_bindings b ON b.revision_id=v.published_revision_id WHERE v.item_id=? AND v.locale=?`, item, source).Scan(&current)
	if e != nil || current != d.AssessmentID {
		return ErrReview
	}
	if op == "review" {
		_, e = tx.ExecContext(ctx, "INSERT INTO quiz_translation_reviews(revision_id,assessment_id,reviewed_by) VALUES(?,?,?) ON CONFLICT DO NOTHING", revision, d.AssessmentID, owner)
		return e
	}
	var n int
	e = tx.QueryRowContext(ctx, "SELECT count(*) FROM quiz_translation_reviews WHERE revision_id=? AND assessment_id=?", revision, d.AssessmentID).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrReview
	}
	return nil
}
func ReviewAssessment(ctx context.Context, tx *sql.Tx, owner, item, assessment string) error {
	var n int
	e := tx.QueryRowContext(ctx, "SELECT count(*) FROM assessments WHERE id=? AND item_id=?", assessment, item).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrInvalid
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO assessment_reviews(assessment_id,reviewed_by) VALUES(?,?) ON CONFLICT DO NOTHING", assessment, owner)
	return e
}
