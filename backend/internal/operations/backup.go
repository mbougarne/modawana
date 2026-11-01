package operations

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func References(ctx context.Context, d *sql.DB) ([]storage.File, error) {
	rows, e := d.QueryContext(ctx, "SELECT markdown_path,sha256,byte_size FROM revisions UNION ALL SELECT storage_key,sha256,byte_size FROM media")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []storage.File{}
	for rows.Next() {
		var f storage.File
		if e = rows.Scan(&f.Key, &f.SHA256, &f.Size); e != nil {
			return nil, e
		}
		out = append(out, f)
		if len(out) > bundle.MaxFiles-1 {
			return nil, bundle.ErrInvalid
		}
	}
	return out, rows.Err()
}
func copyFile(source, destination string) error {
	r, e := os.Open(source)
	if e != nil {
		return e
	}
	defer r.Close()
	if e = os.MkdirAll(filepath.Dir(destination), 0700); e != nil {
		return e
	}
	w, e := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer w.Close()
	if _, e = io.Copy(w, r); e != nil {
		return e
	}
	return w.Sync()
}
func syncDir(p string) error {
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func Backup(ctx context.Context, d *sql.DB, files *storage.Store, out io.Writer) error {
	return files.Exclusive(ctx, func() error {
		root, e := os.MkdirTemp("", "blog-backup-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(root)
		if e = db.Snapshot(ctx, d, filepath.Join(root, "blog.sqlite")); e != nil {
			return e
		}
		snapshot, e := openSnapshot(root)
		if e != nil {
			return e
		}
		refs, e := References(ctx, snapshot)
		if e == nil {
			e = db.VerifySchema(ctx, snapshot)
		}
		snapshot.Close()
		if e != nil {
			return e
		}
		paths := []string{"blog.sqlite"}
		for _, f := range refs {
			p := filepath.Join(root, f.Key)
			if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
				return e
			}
			if strings.HasPrefix(f.Key, "content/") {
				b, e := files.Read(f)
				if e != nil {
					return e
				}
				if e = os.WriteFile(p, b, 0600); e != nil {
					return e
				}
			} else {
				r, e := files.OpenUpload(f)
				if e != nil {
					return e
				}
				w, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if e != nil {
					r.Close()
					return e
				}
				_, e = io.Copy(w, r)
				r.Close()
				w.Close()
				if e != nil {
					return e
				}
			}
			paths = append(paths, f.Key)
		}
		return bundle.Write(out, "backup", root, paths)
	})
}
func openSnapshot(root string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: filepath.Join(root, "blog.sqlite")}
	q := url.Values{"mode": {"ro"}, "_pragma": {"trusted_schema(0)", "foreign_keys(1)"}}
	u.RawQuery = q.Encode()
	return sql.Open("sqlite", u.String())
}

// Restore only targets a new directory. It never replaces a running installation.
func Restore(ctx context.Context, archive, target string) error {
	if !filepath.IsAbs(target) {
		return errors.New("restore target must be absolute")
	}
	if _, e := os.Lstat(target); !os.IsNotExist(e) {
		return errors.New("restore target must not exist")
	}
	root, manifest, e := bundle.Read(archive, "backup")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	snapshot, e := openSnapshot(root)
	if e != nil {
		return e
	}
	e = db.VerifySchema(ctx, snapshot)
	if e != nil {
		snapshot.Close()
		return e
	}
	refs, e := References(ctx, snapshot)
	snapshot.Close()
	if e != nil {
		return e
	}
	expected := map[string]bool{"blog.sqlite": true}
	for _, f := range refs {
		expected[f.Key] = true
	}
	if len(expected) != len(manifest.Files) {
		return bundle.ErrInvalid
	}
	for _, f := range manifest.Files {
		if !expected[f.Path] {
			return bundle.ErrInvalid
		}
	}
	fs, e := storage.Open(root)
	if e != nil {
		return e
	}
	for _, f := range refs {
		if strings.HasPrefix(f.Key, "content/") {
			_, e = fs.Read(f)
		} else {
			var r *os.File
			r, e = fs.OpenUpload(f)
			if e == nil {
				r.Close()
			}
		}
		if e != nil {
			fs.Close()
			return e
		}
	}
	fs.Close()
	// Work beside the target, then publish with a same-filesystem rename.
	stage, e := os.MkdirTemp(filepath.Dir(target), ".blog-restore-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	for _, f := range manifest.Files {
		if e = copyFile(filepath.Join(root, f.Path), filepath.Join(stage, f.Path)); e != nil {
			return e
		}
	}
	d, e := db.Open(ctx, stage)
	if e != nil {
		return e
	}
	tx, e := d.BeginTx(ctx, nil)
	if e != nil {
		d.Close()
		return e
	}
	for _, q := range []string{"DELETE FROM oauth_clients", "DELETE FROM auth_flows", "DELETE FROM auth_grants", "DELETE FROM sessions", "DELETE FROM recovery_codes", "DELETE FROM passkeys", "DELETE FROM api_tokens", "DELETE FROM auth_throttles", "DELETE FROM idempotency_records", "DELETE FROM learning_identities", "UPDATE owner SET password_hash=NULL,totp_cipher=NULL,totp_step=-1,auth_version=auth_version+1"} {
		if _, e = tx.ExecContext(ctx, q); e != nil {
			tx.Rollback()
			d.Close()
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		d.Close()
		return e
	}
	if _, e = d.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); e != nil {
		d.Close()
		return e
	}
	if e = d.Close(); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(stage, "restore-required"), []byte("Local owner re-enrollment and tested factor required before resume.\n"), 0600); e != nil {
		return e
	}
	if e = syncDir(stage); e != nil {
		return e
	}
	if _, e = os.Lstat(target); !os.IsNotExist(e) {
		return errors.New("restore target appeared during validation")
	}
	if e = os.Rename(stage, target); e != nil {
		return e
	}
	return syncDir(filepath.Dir(target))
}
func Maintenance(root string) bool {
	_, e := os.Lstat(filepath.Join(root, "restore-required"))
	return !os.IsNotExist(e)
}
func Resume(ctx context.Context, d *sql.DB, root string) error {
	var n int
	if e := d.QueryRowContext(ctx, "SELECT count(*) FROM passkeys WHERE revoked_at IS NULL AND tested_at IS NOT NULL AND recovery_pending=0").Scan(&n); e != nil {
		return e
	}
	if n < 1 {
		return errors.New("enroll and test a new passkey through local recovery before resuming")
	}
	if e := os.Remove(filepath.Join(root, "restore-required")); e != nil {
		return e
	}
	return syncDir(root)
}
