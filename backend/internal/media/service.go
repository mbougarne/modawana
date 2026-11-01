package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var ID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Record struct {
	RetainedReferences int          `json:"retained_references"`
	PublicReuse        bool         `json:"public_reuse"`
	ID                 string       `json:"id"`
	URL                string       `json:"url"`
	Kind               string       `json:"kind"`
	MIME               string       `json:"mime_type"`
	Size               int64        `json:"byte_size"`
	Filename           string       `json:"filename"`
	Purpose            string       `json:"purpose"`
	Width              int          `json:"width"`
	Height             int          `json:"height"`
	Created            string       `json:"created_at"`
	File               storage.File `json:"-"`
}
type Service struct {
	DB     *sql.DB
	Files  *storage.Store
	Access access.Service
	slots  chan struct{}
}

func New(d *sql.DB, f *storage.Store, a access.Service) *Service {
	return &Service{DB: d, Files: f, Access: a, slots: make(chan struct{}, 1)}
}

// A single bounded validator avoids concurrent decoded-image memory amplification.
func (s *Service) Upload(ctx context.Context, owner, name, declared, purpose string, r io.Reader) (Record, error) {
	return s.UploadWithKey(ctx, owner, name, declared, purpose, r, "")
}
func (s *Service) UploadWithKey(ctx context.Context, owner, name, declared, purpose string, r io.Reader, retryKey string) (Record, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return Record{}, ctx.Err()
	}
	b, e := io.ReadAll(io.LimitReader(r, storage.MaxUpload+1))
	if e != nil {
		return Record{}, e
	}
	if len(b) > storage.MaxUpload {
		return Record{}, ErrTooLarge
	}
	if e = ctx.Err(); e != nil {
		return Record{}, e
	}
	f, e := Validate(b, declared, purpose)
	if e != nil {
		return Record{}, e
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if len(name) > 255 || strings.ContainsAny(name, "\x00\r\n") || name == "." {
		return Record{}, ErrInvalid
	}
	var out Record
	byteHash := sha256.Sum256(b)
	request := requestDigest(struct {
		Bytes                   string
		Name, Declared, Purpose string
	}{hex.EncodeToString(byteHash[:]), name, declared, purpose})
	e = s.Files.Exclusive(ctx, func() error {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = access.CheckMutation(ctx, tx, ""); e != nil {
			return e
		}
		prior, ok, e := uploadRetry(ctx, tx, access.Actor(ctx, owner), retryKey, request)
		if e != nil || ok {
			out = prior
			return e
		}
		file, id, e := s.Files.WriteUploadLocked(ctx, b)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO media(id,storage_key,mime_type,byte_size,sha256,original_filename,created_by,kind,purpose,width,height) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, file.Key, f.MIME, file.Size, file.SHA256, name, owner, f.Kind, purpose, f.Width, f.Height)
		if e != nil {
			return e
		}
		if e = access.CheckMutation(ctx, tx, id); e != nil {
			return e
		}
		if e = rememberUpload(ctx, tx, access.Actor(ctx, owner), retryKey, request, id); e != nil {
			return e
		}
		out, e = Get(ctx, tx, id)
		if e != nil {
			return e
		}
		return tx.Commit()
	})
	return out, e
}

type Querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Get(ctx context.Context, q Querier, id string) (Record, error) {
	var out Record
	if !ID.MatchString(id) {
		return out, sql.ErrNoRows
	}
	e := q.QueryRowContext(ctx, `SELECT id,storage_key,sha256,byte_size,mime_type,original_filename,kind,purpose,width,height,created_at FROM media WHERE id=?`, id).Scan(&out.ID, &out.File.Key, &out.File.SHA256, &out.Size, &out.MIME, &out.Filename, &out.Kind, &out.Purpose, &out.Width, &out.Height, &out.Created)
	out.File.Size = out.Size
	out.URL = "/media/" + out.ID
	return out, e
}
func (s *Service) Get(ctx context.Context, id string) (Record, error) { return Get(ctx, s.DB, id) }
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.Files.Exclusive(ctx, func() error {
		m, e := s.Get(ctx, id)
		if e != nil {
			return e
		}
		_, e = s.DB.ExecContext(ctx, "DELETE FROM media WHERE id=?", id)
		if e != nil {
			var constraint *sqlite.Error
			if errors.As(e, &constraint) && constraint.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
				return ErrReferenced
			}
			return e
		}
		return s.Files.RemoveUploadLocked(m.File.Key)
	})
}
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, id string, p access.Principal) error {
	if r.Method != "GET" && r.Method != "HEAD" {
		return ErrInvalid
	}
	d, e := s.Access.Resolve(r.Context(), access.Request{Principal: p, Surface: access.Media, ResourceID: id, Method: r.Method})
	if e != nil {
		return e
	}
	if !d.AllowsBody() {
		return sql.ErrNoRows
	}
	m, e := s.Get(r.Context(), id)
	if e != nil {
		return e
	}
	f, e := s.Files.OpenUpload(m.File)
	if e != nil {
		return e
	}
	defer f.Close()
	// One range is sufficient for seeking and avoids multi-range amplification.
	if len(r.Header.Get("Range")) > 128 || strings.Contains(r.Header.Get("Range"), ",") {
		w.WriteHeader(416)
		return nil
	}
	w.Header().Set("Content-Type", m.MIME)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if m.Kind == "pdf" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": m.Filename}))
	}
	http.ServeContent(w, r, "", time.Time{}, f)
	return nil
}
func (s *Service) List(ctx context.Context, kind, cursor string) ([]Record, string, error) {
	out := []Record{}
	if cursor != "" && !ID.MatchString(cursor) {
		return nil, "", ErrInvalid
	}
	if kind != "" && kind != "image" && kind != "video" && kind != "pdf" && kind != "captions" {
		return nil, "", ErrInvalid
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT id FROM media WHERE id>? AND (?='' OR kind=?) ORDER BY id LIMIT 51`, cursor, kind, kind)
	if e != nil {
		return nil, "", e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, "", e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, "", e
	}
	next := ""
	if len(ids) > 50 {
		ids = ids[:50]
		next = ids[49]
	}
	for _, id := range ids {
		m, e := s.Get(ctx, id)
		if e != nil {
			return nil, "", e
		}
		if e = s.DB.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM revision_media WHERE media_id=?)+(SELECT count(*) FROM config_revision_media WHERE media_id=?)", id, id).Scan(&m.RetainedReferences); e != nil {
			return nil, "", e
		}
		decision, e := s.Access.Resolve(ctx, access.Request{Surface: access.Media, ResourceID: id, Method: "GET"})
		if e != nil {
			return nil, "", e
		}
		m.PublicReuse = decision.AllowsBody()
		out = append(out, m)
	}
	return out, next, nil
}
