package db

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/migrations"
	"modernc.org/sqlite"
	"os"
)

// Snapshot uses SQLite's online backup API, including committed WAL pages.
// Callers hold the storage maintenance lock until referenced files are copied.
func Snapshot(ctx context.Context, source *sql.DB, destination string) error {
	f, e := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	conn, e := source.Conn(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	e = conn.Raw(func(raw any) error {
		b, e := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		}).NewBackup(destination)
		if e != nil {
			return e
		}
		for {
			if e = ctx.Err(); e != nil {
				break
			}
			var more bool
			more, e = b.Step(128)
			if e != nil || !more {
				break
			}
		}
		finish := b.Finish()
		if e != nil {
			return e
		}
		return finish
	})
	if e != nil {
		os.Remove(destination)
		return e
	}
	f, e = os.OpenFile(destination, os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// VerifySchema rejects missing/newer migrations without modifying the snapshot.
func VerifySchema(ctx context.Context, d *sql.DB) error {
	expected, e := readMigrations(migrations.Files)
	if e != nil {
		return e
	}
	rows, e := d.QueryContext(ctx, "SELECT version,name,checksum FROM schema_migrations ORDER BY version")
	if e != nil {
		return e
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var n int
		var name, sum string
		if e = rows.Scan(&n, &name, &sum); e != nil {
			return e
		}
		if i >= len(expected) || n != expected[i].number || name != expected[i].name || sum != expected[i].checksum {
			return ErrSchemaMismatch
		}
		i++
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if i != len(expected) {
		return ErrSchemaMismatch
	}
	rows.Close()
	var check string
	if e = d.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); e != nil || check != "ok" {
		return errors.New("snapshot integrity check failed")
	}
	rows, e = d.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("snapshot foreign key check failed")
	}
	if e = rows.Err(); e != nil {
		return e
	}
	rows.Close()
	return verifyObjects(ctx, d)
}

func schemaObjects(ctx context.Context, d *sql.DB) ([]string, error) {
	rows, e := d.QueryContext(ctx, "SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' AND name!='schema_migrations' ORDER BY type,name")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var kind, name, table, definition string
		if e = rows.Scan(&kind, &name, &table, &definition); e != nil {
			return nil, e
		}
		out = append(out, kind+"\x00"+name+"\x00"+table+"\x00"+definition)
	}
	return out, rows.Err()
}
func verifyObjects(ctx context.Context, d *sql.DB) error {
	expected, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		return e
	}
	defer expected.Close()
	expected.SetMaxOpenConns(1)
	list, e := readMigrations(migrations.Files)
	if e != nil {
		return e
	}
	for _, m := range list {
		if _, e = expected.ExecContext(ctx, m.sql); e != nil {
			return e
		}
	}
	want, e := schemaObjects(ctx, expected)
	if e != nil {
		return e
	}
	have, e := schemaObjects(ctx, d)
	if e != nil {
		return e
	}
	if len(want) != len(have) {
		return ErrSchemaMismatch
	}
	for i := range want {
		if want[i] != have[i] {
			return ErrSchemaMismatch
		}
	}
	return nil
}
