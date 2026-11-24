package content

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

// PublicRevisionPath checks an exact published revision and anonymous access
// in the caller's transaction. No draft files are read.
func (s *Service) PublicRevisionPath(ctx context.Context, tx *sql.Tx, key, locale, revision string) (string, error) {
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

// SocialCover resolves the same immutable JPEG the public media route can serve.
func (s *Service) SocialCover(ctx context.Context, tx *sql.Tx, revision string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT m.id FROM revisions r JOIN media m ON m.id=r.cover_media_id
		WHERE r.id=? AND m.mime_type='image/jpeg' AND m.byte_size<=8388608
		AND m.width BETWEEN 320 AND 1440 AND m.height>0 AND m.width*100>=m.height*80 AND m.width*100<=m.height*191`, revision).Scan(&id)
	if err != nil {
		return "", err
	}
	if !media.ID.MatchString(id) {
		return "", sql.ErrNoRows
	}
	decision, err := s.Access.ResolveIn(ctx, tx, access.Request{Principal: access.Principal{Audience: access.Anonymous},
		Surface: access.Media, ResourceID: id, Method: "GET"})
	if err != nil {
		return "", err
	}
	if !decision.AllowsBody() {
		return "", sql.ErrNoRows
	}
	return "/media/" + id, nil
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
		_, err := s.PublicRevisionPath(ctx, tx, item.Key, locale, item.Revision)
		if err == nil {
			out.Items = append(out.Items, item)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
	}
	return out, nil
}
