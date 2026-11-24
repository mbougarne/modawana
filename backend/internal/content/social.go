package content

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

// SocialRevision resolves a public link in the same transaction that will
// record a scheduled send or authorize a delivery. No draft files are read.
func (s *Service) SocialRevision(ctx context.Context, tx *sql.Tx, key, locale, revision string) (string, error) {
	visible, err := siteconfig.LocaleVisibleIn(ctx, tx, locale)
	if err != nil {
		return "", err
	}
	if !visible {
		return "", sql.ErrNoRows
	}
	v, err := loadVersion(ctx, tx, key, locale)
	if err != nil {
		return "", err
	}
	if v.Published == "" || v.Published != revision {
		return "", sql.ErrNoRows
	}
	decision, err := s.Access.ResolveIn(ctx, tx, access.Request{
		Principal: access.Principal{Audience: access.Anonymous}, Surface: access.Feed, ResourceID: v.ID, Locale: locale,
	})
	if err != nil {
		return "", err
	}
	if !decision.AllowsBody() || decision.RevisionID != revision {
		return "", sql.ErrNoRows
	}
	path, err := canonical(ctx, tx, key, locale, v.Kind, v.Slug)
	if err == nil && path == "" {
		err = sql.ErrNoRows
	}
	return path, err
}

type SocialCandidate struct {
	Key      string `json:"key"`
	Revision string `json:"revision_id"`
	Title    string `json:"title"`
}

type SocialCandidates struct {
	Items []SocialCandidate `json:"items"`
	Next  string            `json:"next,omitempty"`
}

// SocialCandidates exposes titles of exact published revisions to the composer.
func (s *Service) SocialCandidates(ctx context.Context, locale, cursor string) (SocialCandidates, error) {
	out := SocialCandidates{Items: []SocialCandidate{}}
	if !localeOK(locale) || cursor != "" && !keyPattern.MatchString(cursor) {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	visible, err := siteconfig.LocaleVisibleIn(ctx, tx, locale)
	if err != nil || !visible {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT i.key,r.id,r.title FROM content_versions v
		JOIN content_items i ON i.id=v.item_id JOIN revisions r ON r.id=v.published_revision_id
		WHERE v.locale=? AND i.key>? ORDER BY i.key LIMIT 51`, locale, cursor)
	if err != nil {
		return out, err
	}
	batch := []SocialCandidate{}
	for rows.Next() {
		var item SocialCandidate
		if err = rows.Scan(&item.Key, &item.Revision, &item.Title); err != nil {
			rows.Close()
			return out, err
		}
		batch = append(batch, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(batch) > 50 {
		batch = batch[:50]
		out.Next = batch[len(batch)-1].Key
	}
	for _, item := range batch {
		_, err := s.SocialRevision(ctx, tx, item.Key, locale, item.Revision)
		if err == nil {
			out.Items = append(out.Items, item)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
	}
	return out, nil
}
