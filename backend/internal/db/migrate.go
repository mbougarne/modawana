package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrSchemaMismatch = errors.New("database schema does not match this binary; migration history or downgrade rejected")
var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

type migration struct {
	number              int
	name, checksum, sql string
}

func readMigrations(source fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			if strings.HasSuffix(entry.Name(), ".sql") {
				return nil, ErrSchemaMismatch
			}
			continue
		}
		n, _ := strconv.Atoi(match[1])
		raw, err := fs.ReadFile(source, entry.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		out = append(out, migration{n, entry.Name(), hex.EncodeToString(sum[:]), string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].number < out[j].number })
	if len(out) == 0 {
		return nil, ErrSchemaMismatch
	}
	for i, m := range out {
		if m.number != i+1 {
			return nil, ErrSchemaMismatch
		}
	}
	return out, nil
}
func migrate(ctx context.Context, database *sql.DB, source fs.FS) error {
	list, err := readMigrations(source)
	if err != nil {
		return err
	}
	// BEGIN IMMEDIATE (DSN policy) serializes startup and makes history + DDL one atomic change.
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
 version INTEGER PRIMARY KEY CHECK(version>0), name TEXT NOT NULL UNIQUE,
 checksum TEXT NOT NULL CHECK(length(checksum)=64), applied_at TEXT NOT NULL
 ) STRICT`)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT version,name,checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var n int
		var name, sum string
		if err = rows.Scan(&n, &name, &sum); err != nil {
			rows.Close()
			return err
		}
		if count >= len(list) || n != list[count].number || name != list[count].name || sum != list[count].checksum {
			rows.Close()
			return ErrSchemaMismatch
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count == 0 {
		var tables int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name!='schema_migrations'").Scan(&tables); err != nil {
			return err
		}
		if tables != 0 {
			return ErrSchemaMismatch
		}
	}
	for _, m := range list[count:] {
		if _, err = tx.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("migration %04d failed: %w", m.number, err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,name,checksum,applied_at) VALUES(?,?,?,?)", m.number, m.name, m.checksum, UTC(time.Now())); err != nil {
			return err
		}
	}
	// Refuse a successful startup if any relationship was corrupted by an external connection.
	rows, err = tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	bad := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if bad {
		return ErrSchemaMismatch
	}
	return tx.Commit()
}
