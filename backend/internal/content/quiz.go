package content

import (
	"context"
	"database/sql"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
)

func (s *Service) PreviewQuiz(ctx context.Context, key, locale string, in learning.Submission) (learning.Result, error) {
	var out learning.Result
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return out, e
	}
	d, e := learning.Read(ctx, tx, v.Draft)
	if e != nil {
		return out, e
	}
	if d == nil {
		return out, sql.ErrNoRows
	}
	if in.Version.Content != v.Draft || in.Version.Quiz != d.AssessmentID {
		return out, db.ErrVersionConflict
	}
	return learning.Grade(*d, v.Draft, in.Answers)
}
func (s *Service) ReviewQuiz(ctx context.Context, owner, key, locale string, a Action) error {
	if a.ExpectedVersion == nil {
		return ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return e
	}
	if v.Draft != a.RevisionID || v.Number != *a.ExpectedVersion {
		return db.ErrVersionConflict
	}
	d, e := learning.Read(ctx, tx, v.Draft)
	if e != nil {
		return e
	}
	if d == nil {
		return ErrInvalid
	}
	if e = learning.ReviewAssessment(ctx, tx, owner, v.Item, d.AssessmentID); e != nil {
		return e
	}
	return tx.Commit()
}
