// Package content coordinates durable file writes with metadata transactions.
package content

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

type Revisions struct {
	DB    *sql.DB
	Files *storage.Store
	fault func(string) error
}

// SaveInput is internal, not an HTTP request. Authorization, media extraction and
// publication are added in ticket 012; no route exposes this persistence primitive.
type SaveInput struct {
	ItemID, VersionID, OwnerID, Locale, Slug, Title, Summary string
	Body                                                     []byte
	ExpectedVersion                                          int64
}
type Saved struct {
	RevisionID string
	Version    int64
}

func (s *Revisions) Save(ctx context.Context, in SaveInput) (Saved, error) {
	var out Saved
	if utf8.RuneCountInString(in.Title) < 1 || utf8.RuneCountInString(in.Title) > 200 || utf8.RuneCountInString(in.Summary) > 500 || in.ExpectedVersion < 0 {
		return out, errors.New("invalid revision")
	}
	e := s.Files.Exclusive(ctx, func() error {
		f, id, e := s.Files.WriteLocked(in.ItemID, in.Locale, in.Body)
		if e != nil {
			return e
		}
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if in.ExpectedVersion == 0 {
			_, e = tx.ExecContext(ctx, "INSERT INTO content_versions(id,item_id,locale,slug,draft_revision_id) VALUES(?,?,?,?,?)", in.VersionID, in.ItemID, in.Locale, in.Slug, id)
		} else {
			var item, locale string
			e = tx.QueryRowContext(ctx, "SELECT item_id,locale FROM content_versions WHERE id=?", in.VersionID).Scan(&item, &locale)
			if e == nil && (item != in.ItemID || locale != in.Locale) {
				e = errors.New("version ownership mismatch")
			}
			if e == nil {
				e = db.BumpVersion(ctx, tx, db.ContentVersions, in.VersionID, in.ExpectedVersion)
			}
			if e == nil {
				_, e = tx.ExecContext(ctx, "UPDATE content_versions SET draft_revision_id=? WHERE id=?", id, in.VersionID)
			}
		}
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO revisions(id,version_id,title,summary,markdown_path,sha256,byte_size,created_by,reading_minutes) VALUES(?,?,?,?,?,?,?,?,?)", id, in.VersionID, in.Title, in.Summary, f.Key, f.SHA256, f.Size, in.OwnerID, readingMinutes(string(in.Body)))
		if e != nil {
			return e
		}
		if s.fault != nil {
			if e = s.fault("before_commit"); e != nil {
				return e
			}
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		out = Saved{id, in.ExpectedVersion + 1}
		if s.fault != nil {
			return s.fault("after_commit")
		}
		return nil
	})
	return out, e
}
func (s *Revisions) Read(ctx context.Context, id string) ([]byte, error) {
	var f storage.File
	if e := s.DB.QueryRowContext(ctx, "SELECT markdown_path,sha256,byte_size FROM revisions WHERE id=?", id).Scan(&f.Key, &f.SHA256, &f.Size); e != nil {
		return nil, storage.ErrIntegrity
	}
	return s.Files.Read(f)
}
