package operations

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/plugin"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"github.com/mbougarne/modawana/backend/internal/theme"
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
func readRevisionArtifact(files *storage.Store, f storage.File) ([]byte, error) {
	body, err := files.Read(f)
	if err != nil {
		return nil, err
	}
	if f.HasPageBlocks() && content.ValidatePageBlocks(body) != nil {
		return nil, storage.ErrIntegrity
	}
	return body, nil
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
func Backup(ctx context.Context, d *sql.DB, files *storage.Store, dataRoot string, out io.Writer) error {
	return files.Exclusive(ctx, func() error {
		root, e := os.MkdirTemp("", "blog-backup-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(root)
		if e = db.Snapshot(ctx, d, filepath.Join(root, "blog.sqlite")); e != nil {
			return e
		}
		if e = stripExtensionSecrets(ctx, root); e != nil {
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
		if e != nil {
			snapshot.Close()
			return e
		}
		approved, e := theme.Approvals(ctx, snapshot)
		if e != nil {
			snapshot.Close()
			return e
		}
		plugins, e := plugin.ArchiveRecords(ctx, snapshot)
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
				b, e := readRevisionArtifact(files, f)
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
		for _, a := range approved {
			pack, err := theme.ReadVerifiedPack(dataRoot, a.ID, a.Checksum)
			if err != nil {
				continue // Keep the approval and restore to the built-in fallback.
			}
			key := theme.PackPath(a.ID)
			p := filepath.Join(root, key)
			if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
				return e
			}
			if e = os.WriteFile(p, pack.Raw, 0600); e != nil {
				return e
			}
			paths = append(paths, key)
		}
		for _, record := range plugins {
			files, err := record.CopyVerifiedFiles(dataRoot, root)
			if err != nil {
				return err
			}
			paths = append(paths, files...)
		}
		return bundle.Write(out, "backup", root, paths)
	})
}
func openSnapshot(root string) (*sql.DB, error) {
	return openSnapshotMode(root, "ro")
}

func stripExtensionSecrets(ctx context.Context, root string) error {
	database, err := openSnapshotMode(root, "rw")
	if err != nil {
		return err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	// Rebuild pages as well as deleting rows, so old deleted ciphertext is
	// removed even when the extension credentials are already empty.
	for _, statement := range []string{"PRAGMA secure_delete=ON", "DELETE FROM plugin_secrets", "UPDATE editorial_settings SET enabled=0,credential=NULL", "VACUUM"} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func openSnapshotMode(root, mode string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: filepath.Join(root, "blog.sqlite")}
	q := url.Values{"mode": {mode}, "_pragma": {"trusted_schema(0)", "foreign_keys(1)"}}
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
	if e != nil {
		snapshot.Close()
		return e
	}
	approved, e := theme.Approvals(ctx, snapshot)
	if e != nil {
		snapshot.Close()
		return e
	}
	plugins, e := plugin.ArchiveRecords(ctx, snapshot)
	snapshot.Close()
	if e != nil {
		return e
	}
	filesByPath := map[string]plugin.File{}
	for _, record := range plugins {
		for _, file := range record.Files {
			filesByPath[record.Path(file)] = file
		}
	}
	included := map[string]bool{}
	themeFiles := map[string]theme.Approval{}
	for _, a := range approved {
		themeFiles[theme.PackPath(a.ID)] = a
	}
	expected := map[string]bool{"blog.sqlite": true}
	for _, f := range refs {
		expected[f.Key] = true
	}
	for _, f := range manifest.Files {
		if expected[f.Path] {
			delete(expected, f.Path)
			continue
		}
		if file, ok := filesByPath[f.Path]; ok {
			if f.SHA256 != file.SHA256 || f.Size != file.Size {
				return bundle.ErrInvalid
			}
			included[f.Path] = true
			continue
		}
		a, ok := themeFiles[f.Path]
		if !ok || a.Checksum != f.SHA256 {
			return bundle.ErrInvalid
		}
		if _, e = theme.ReadVerifiedPack(root, a.ID, a.Checksum); e != nil {
			return bundle.ErrInvalid
		}
	}
	if len(expected) != 0 {
		return bundle.ErrInvalid
	}
	var disabled []string
	for _, record := range plugins {
		count := 0
		for _, file := range record.Files {
			if included[record.Path(file)] {
				count++
			}
		}
		if count == 0 {
			disabled = append(disabled, record.Slug)
			continue
		}
		directory, err := plugin.ReadDirectory(filepath.Join(root, "plugins"), record.Slug)
		if err != nil || !plugin.SameFiles(directory.Files, record.Files) {
			return bundle.ErrInvalid
		}
	}
	fs, e := storage.Open(root)
	if e != nil {
		return e
	}
	for _, f := range refs {
		if strings.HasPrefix(f.Key, "content/") {
			_, e = readRevisionArtifact(fs, f)
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
	for _, q := range []string{"UPDATE editorial_settings SET enabled=0,credential=NULL", "DELETE FROM social_oauth_flows", "DELETE FROM social_connections", "DELETE FROM plugin_secrets", "DELETE FROM oauth_clients", "DELETE FROM auth_flows", "DELETE FROM staff_flows", "DELETE FROM staff_sessions", "UPDATE staff_members SET revoked_at=coalesce(revoked_at,strftime('%Y-%m-%dT%H:%M:%fZ','now'))", "UPDATE staff_invites SET revoked_at=coalesce(revoked_at,strftime('%Y-%m-%dT%H:%M:%fZ','now')) WHERE consumed_at IS NULL", "DELETE FROM auth_grants", "DELETE FROM sessions", "DELETE FROM recovery_codes", "DELETE FROM passkeys", "DELETE FROM api_tokens", "DELETE FROM auth_throttles", "DELETE FROM idempotency_records", "DELETE FROM learning_identities", "UPDATE owner SET password_hash=NULL,totp_cipher=NULL,totp_step=-1,auth_version=auth_version+1"} {
		if _, e = tx.ExecContext(ctx, q); e != nil {
			tx.Rollback()
			d.Close()
			return e
		}
	}
	for _, slug := range disabled {
		if _, e = tx.ExecContext(ctx, "UPDATE plugin_installs SET enabled=0,version=version+1 WHERE slug=? AND enabled=1", slug); e != nil {
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
