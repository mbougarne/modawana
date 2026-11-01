package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

// Storage quotas: fixed per plugin in stage 1, enforced inside the write
// transaction so a plugin cannot grow SQLite unbounded. Manifest-declared
// collection bounds arrive with plugin migrations in stage 2.
const (
	maxKVValue   = 64 << 10
	maxKVKeys    = 1024
	maxKVBytes   = 1 << 20
	maxDocBody   = 256 << 10
	maxDocs      = 4096
	maxDocsBytes = 4 << 20
)

// ErrNotFound reports a missing key or document.
var ErrNotFound = errors.New("plugin storage: not found")

// ErrStorageInvalid reports a malformed key, collection, or value.
var ErrStorageInvalid = errors.New("plugin storage: invalid input")

// ErrQuota reports a write that would exceed the plugin's storage quota.
var ErrQuota = errors.New("plugin storage: quota exceeded")

var kvKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
var collectionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// storage is the namespaced persistence surface behind the storage host
// functions. Every row is keyed by the plugin's install ID: one plugin can
// never read or write another plugin's namespace.
//
// The quota writes read current usage and then upsert inside one transaction.
// That pre-check is sound only because the connection policy (db.Open's
// _txlock=immediate) makes every transaction a write transaction from BEGIN:
// concurrent writers serialize at BeginTx, so two invocations can never both
// pass the usage check on stale reads and overshoot a quota.
type storage struct {
	db *sql.DB
}

func (s storage) kvGet(ctx context.Context, pluginID, key string) ([]byte, error) {
	if !kvKeyPattern.MatchString(key) {
		return nil, ErrStorageInvalid
	}
	var value []byte
	err := s.db.QueryRowContext(ctx, "SELECT value FROM plugin_kv WHERE plugin_id=? AND key=?", pluginID, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return value, err
}

func (s storage) kvSet(ctx context.Context, pluginID, key string, value []byte) error {
	if !kvKeyPattern.MatchString(key) || len(value) > maxKVValue {
		return ErrStorageInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var keys int
	var used int64
	err = tx.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(length(key)+length(value)),0) FROM plugin_kv WHERE plugin_id=? AND key!=?", pluginID, key).Scan(&keys, &used)
	if err != nil {
		return err
	}
	if keys+1 > maxKVKeys || used+int64(len(key)+len(value)) > maxKVBytes {
		return ErrQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plugin_kv(plugin_id,key,value,updated_at) VALUES(?,?,?,?)
 ON CONFLICT(plugin_id,key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, pluginID, key, value, db.UTC(time.Now()))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s storage) kvDelete(ctx context.Context, pluginID, key string) error {
	if !kvKeyPattern.MatchString(key) {
		return ErrStorageInvalid
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM plugin_kv WHERE plugin_id=? AND key=?", pluginID, key)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s storage) docGet(ctx context.Context, pluginID, collection, id string) ([]byte, error) {
	if !collectionPattern.MatchString(collection) || !kvKeyPattern.MatchString(id) {
		return nil, ErrStorageInvalid
	}
	var body string
	err := s.db.QueryRowContext(ctx, "SELECT body FROM plugin_documents WHERE plugin_id=? AND collection=? AND doc_id=?", pluginID, collection, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return []byte(body), err
}

func (s storage) docPut(ctx context.Context, pluginID, collection, id string, body []byte) error {
	if !collectionPattern.MatchString(collection) || !kvKeyPattern.MatchString(id) || len(body) > maxDocBody || !json.Valid(body) {
		return ErrStorageInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var docs int
	var used int64
	err = tx.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(length(body)),0) FROM plugin_documents WHERE plugin_id=? AND NOT (collection=? AND doc_id=?)", pluginID, collection, id).Scan(&docs, &used)
	if err != nil {
		return err
	}
	if docs+1 > maxDocs || used+int64(len(body)) > maxDocsBytes {
		return ErrQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plugin_documents(plugin_id,collection,doc_id,body,updated_at) VALUES(?,?,?,?,?)
 ON CONFLICT(plugin_id,collection,doc_id) DO UPDATE SET body=excluded.body, updated_at=excluded.updated_at`, pluginID, collection, id, string(body), db.UTC(time.Now()))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s storage) docDelete(ctx context.Context, pluginID, collection, id string) error {
	if !collectionPattern.MatchString(collection) || !kvKeyPattern.MatchString(id) {
		return ErrStorageInvalid
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM plugin_documents WHERE plugin_id=? AND collection=? AND doc_id=?", pluginID, collection, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
