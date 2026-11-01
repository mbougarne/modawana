package content

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/learning"

	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

type Revision struct {
	Quiz          *learning.Definition `json:"quiz,omitempty"`
	ID            string               `json:"revision_id"`
	Title         string               `json:"title"`
	Summary       string               `json:"summary"`
	Markdown      string               `json:"markdown"`
	Slug          string               `json:"slug"`
	SourceID      string               `json:"source_revision_id"`
	CategoryKey   string               `json:"category_key"`
	Tags          []string             `json:"tag_keys"`
	CoverID       string               `json:"cover_media_id"`
	Created       string               `json:"created_at"`
	Media         []media.Record       `json:"media"`
	Presentations []Presentation       `json:"media_presentations"`
}
type Draft struct {
	CollectionKey string   `json:"collection_key"`
	Position      int      `json:"position"`
	SlugLocked    bool     `json:"slug_locked"`
	Key           string   `json:"key"`
	Kind          string   `json:"kind"`
	SourceLocale  string   `json:"source_locale"`
	Locale        string   `json:"locale"`
	Version       int64    `json:"version"`
	PublishedID   string   `json:"published_revision_id"`
	Reviewed      bool     `json:"reviewed"`
	Stale         bool     `json:"translation_stale"`
	Revision      Revision `json:"revision"`
}

func (s *Service) revision(ctx context.Context, q query, id, versionID string) (Revision, error) {
	out := Revision{Tags: []string{}, Media: []media.Record{}, Presentations: []Presentation{}}
	var file storage.File
	e := q.QueryRowContext(ctx, `SELECT r.id,r.title,r.summary,CASE WHEN r.slug='' THEN v.slug ELSE r.slug END,coalesce(r.source_revision_id,''),coalesce(c.key,''),coalesce(r.cover_media_id,''),r.created_at,r.markdown_path,r.sha256,r.byte_size FROM revisions r JOIN content_versions v ON v.id=r.version_id LEFT JOIN categories c ON c.id=r.category_id WHERE r.id=? AND r.version_id=?`, id, versionID).Scan(&out.ID, &out.Title, &out.Summary, &out.Slug, &out.SourceID, &out.CategoryKey, &out.CoverID, &out.Created, &file.Key, &file.SHA256, &file.Size)
	if e != nil {
		return out, e
	}
	b, e := s.Files.Read(file)
	if e != nil {
		return out, e
	}
	out.Markdown = string(b)
	rows, e := q.QueryContext(ctx, "SELECT t.key FROM revision_tags rt JOIN tags t ON t.id=rt.tag_id WHERE rt.revision_id=? ORDER BY t.key", id)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			rows.Close()
			return out, e
		}
		out.Tags = append(out.Tags, key)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = q.QueryContext(ctx, "SELECT media_id FROM revision_media WHERE revision_id=? ORDER BY media_id", id)
	if e != nil {
		return out, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, id := range ids {
		m, e := media.Get(ctx, q, id)
		if e != nil {
			return out, e
		}
		out.Media = append(out.Media, m)
	}
	rows, e = q.QueryContext(ctx, "SELECT media_id,coalesce(poster_media_id,''),coalesce(caption_track_media_id,''),caption,transcript FROM revision_media_presentations WHERE revision_id=? ORDER BY media_id", id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var p Presentation
		if e = rows.Scan(&p.MediaID, &p.PosterID, &p.TrackID, &p.Caption, &p.Transcript); e != nil {
			return out, e
		}
		out.Presentations = append(out.Presentations, p)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	rows.Close()
	out.Quiz, e = learning.Read(ctx, q, id)
	return out, e
}
func (s *Service) Draft(ctx context.Context, key, locale string) (Draft, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Draft{}, e
	}
	defer tx.Rollback()
	if e = access.CheckMutation(ctx, tx, ""); e != nil {
		return Draft{}, e
	}
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return Draft{}, e
	}
	rev, e := s.revision(ctx, tx, v.Draft, v.ID)
	if e != nil {
		return Draft{}, e
	}
	out := Draft{SlugLocked: v.First != "", Key: key, Kind: v.Kind, SourceLocale: v.Source, Locale: locale, Version: v.Number, PublishedID: v.Published, Revision: rev}
	if e = tx.QueryRowContext(ctx, `SELECT c.key,m.position FROM collection_items m JOIN collections c ON c.id=m.collection_id WHERE m.item_id=?`, v.Item).Scan(&out.CollectionKey, &out.Position); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return Draft{}, e
	}
	if v.Source != locale {
		var current string
		_ = tx.QueryRowContext(ctx, "SELECT coalesce(published_revision_id,'') FROM content_versions WHERE item_id=? AND locale=?", v.Item, v.Source).Scan(&current)
		out.Stale = current == "" || current != rev.SourceID
		var n int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM translation_reviews WHERE revision_id=? AND source_revision_id=?", rev.ID, rev.SourceID).Scan(&n); e != nil {
			return out, e
		}
		out.Reviewed = n == 1
	}
	return out, nil
}

type HistoryEntry struct {
	ID       string `json:"revision_id"`
	Title    string `json:"title"`
	Created  string `json:"created_at"`
	SourceID string `json:"source_revision_id"`
}

func (s *Service) History(ctx context.Context, key, locale, cursor string) ([]HistoryEntry, string, error) {
	out := []HistoryEntry{}
	if cursor != "" && !media.ID.MatchString(cursor) {
		return nil, "", ErrInvalid
	}
	v, e := loadVersion(ctx, s.DB, key, locale)
	if e != nil {
		return nil, "", e
	}
	var date string
	if cursor != "" {
		if e = s.DB.QueryRowContext(ctx, "SELECT created_at FROM revisions WHERE id=? AND version_id=?", cursor, v.ID).Scan(&date); e != nil {
			return nil, "", ErrInvalid
		}
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT id,title,created_at,coalesce(source_revision_id,'') FROM revisions WHERE version_id=? AND (?='' OR created_at<? OR (created_at=? AND id<?)) ORDER BY created_at DESC,id DESC LIMIT 51", v.ID, cursor, date, date, cursor)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	for rows.Next() {
		var h HistoryEntry
		if e = rows.Scan(&h.ID, &h.Title, &h.Created, &h.SourceID); e != nil {
			return nil, "", e
		}
		out = append(out, h)
	}
	next := ""
	if len(out) > 50 {
		out = out[:50]
		next = out[49].ID
	}
	return out, next, rows.Err()
}

type Action struct {
	Plan             *ApplyPlanInput `json:"plan,omitempty"`
	ExpectedVersion  *int64          `json:"expected_version"`
	RevisionID       string          `json:"revision_id"`
	SourceRevisionID string          `json:"source_revision_id,omitempty"`
}

func (s *Service) Restore(ctx context.Context, owner, key, locale, retryKey string, a Action) (Result, error) {
	if a.ExpectedVersion == nil || *a.ExpectedVersion < 1 || !media.ID.MatchString(a.RevisionID) {
		return Result{}, ErrInvalid
	}
	v, e := loadVersion(ctx, s.DB, key, locale)
	if e != nil {
		return Result{}, e
	}
	r, e := s.revision(ctx, s.DB, a.RevisionID, v.ID)
	if e != nil {
		return Result{}, e
	}
	slug := r.Slug
	if v.First != "" {
		slug = v.Slug
	}
	in := DraftInput{Quiz: r.Quiz, ExpectedVersion: a.ExpectedVersion, Kind: v.Kind, SourceLocale: v.Source, Slug: slug, Title: r.Title, Summary: r.Summary, Markdown: r.Markdown, SourceRevisionID: r.SourceID, CategoryKey: r.CategoryKey, TagKeys: r.Tags, CoverMediaID: r.CoverID, Presentations: r.Presentations}
	// The exact historical revision participates in the operation key even if two
	// revisions have identical bytes and metadata.
	return s.save(ctx, owner, key, locale, retryKey, "restore:"+a.RevisionID, in)
}

type ItemSummary struct {
	Key          string `json:"key"`
	Kind         string `json:"kind"`
	SourceLocale string `json:"source_locale"`
}

func (s *Service) List(ctx context.Context, cursor string) ([]ItemSummary, string, error) {
	if cursor != "" && !keyPattern.MatchString(cursor) {
		return nil, "", ErrInvalid
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT key,kind,source_locale FROM content_items WHERE key>? ORDER BY key LIMIT 51", cursor)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	out := []ItemSummary{}
	for rows.Next() {
		var i ItemSummary
		if e = rows.Scan(&i.Key, &i.Kind, &i.SourceLocale); e != nil {
			return nil, "", e
		}
		out = append(out, i)
	}
	next := ""
	if len(out) > 50 {
		out = out[:50]
		next = out[49].Key
	}
	return out, next, rows.Err()
}

func (s *Service) Historical(ctx context.Context, key, locale, id string) (Revision, error) {
	v, e := loadVersion(ctx, s.DB, key, locale)
	if e != nil {
		return Revision{}, e
	}
	return s.revision(ctx, s.DB, id, v.ID)
}
