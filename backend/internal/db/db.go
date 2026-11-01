// Package db owns SQLite connection policy and schema lifecycle. No content bodies live here.
package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mbougarne/modawana/backend/migrations"
	_ "modernc.org/sqlite"
)

const TimestampLayout = "2006-01-02T15:04:05.000Z"

func UTC(t time.Time) string { return t.UTC().Format(TimestampLayout) }
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Open expects the canonical external data root validated by config.Load.
// Existing data must be backed up before starting a binary with pending migrations.
func Open(ctx context.Context, dataRoot string) (*sql.DB, error) {
	if !filepath.IsAbs(dataRoot) {
		return nil, errors.New("database root must be absolute")
	}
	if err := os.MkdirAll(dataRoot, 0700); err != nil {
		return nil, errors.New("database directory unavailable")
	}
	info, err := os.Lstat(dataRoot)
	if err != nil || !info.IsDir() || info.Mode()&0077 != 0 {
		return nil, errors.New("database directory must be private and not a symlink")
	}
	path := filepath.Join(dataRoot, "blog.sqlite")
	// Pre-create a private regular file; SQLite sidecars inherit the main file's mode.
	for _, name := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		info, e := os.Lstat(name)
		if e == nil && (!info.Mode().IsRegular() || info.Mode()&0077 != 0) {
			return nil, errors.New("unsafe database file")
		}
		if e != nil && !os.IsNotExist(e) {
			return nil, errors.New("database file unavailable")
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.New("database file unavailable")
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "trusted_schema(0)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	database, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("database open failed")
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	if err = database.PingContext(ctx); err == nil {
		err = migrate(ctx, database, migrations.Files)
	}
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

var ErrVersionConflict = errors.New("version conflict")

type VersionedTable string

const (
	ContentVersions        VersionedTable = "content_versions"
	Collections            VersionedTable = "collections"
	ConfigurationDocuments VersionedTable = "config_documents"
	PluginInstalls         VersionedTable = "plugin_installs"
)

// BumpVersion must share the transaction that changes the document/pointers.
// The table identifier is code-owned; user input is always bound as a parameter.
func BumpVersion(ctx context.Context, tx *sql.Tx, table VersionedTable, id string, expected int64) error {
	switch table {
	case ContentVersions, Collections, ConfigurationDocuments, PluginInstalls:
	default:
		return errors.New("invalid versioned table")
	}
	if expected < 1 || expected == int64(^uint64(0)>>1) {
		return ErrVersionConflict
	}
	result, err := tx.ExecContext(ctx, "UPDATE "+string(table)+" SET version=version+1 WHERE id=? AND version=?", id, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrVersionConflict
	}
	return nil
}
