package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mbougarne/modawana/backend/migrations"
)

func privateDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}
func openTest(t *testing.T) *sql.DB {
	t.Helper()
	database, err := Open(context.Background(), privateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}
func id(t *testing.T) string {
	t.Helper()
	s, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func exec(t *testing.T, d interface {
	Exec(string, ...any) (sql.Result, error)
}, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}
func reject(t *testing.T, d interface {
	Exec(string, ...any) (sql.Result, error)
}, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err == nil {
		t.Fatalf("accepted invalid statement: %s", q)
	}
}
func owner(t *testing.T, d *sql.DB) string {
	t.Helper()
	var existing string
	if d.QueryRow("SELECT id FROM owner").Scan(&existing) == nil {
		return existing
	}
	o := id(t)
	exec(t, d, "INSERT INTO owner(id) VALUES(?)", o)
	return o
}
func item(t *testing.T, d *sql.DB, kind, locale string) (string, string, string) {
	t.Helper()
	o := owner(t, d)
	i, v, r := id(t), id(t), id(t)
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec(t, tx, "INSERT INTO content_items(id,key,kind,source_locale) VALUES(?,?,?,?)", i, "key-"+i, kind, locale)
	exec(t, tx, "INSERT INTO content_versions(id,item_id,locale,slug,draft_revision_id) VALUES(?,?,?,?,?)", v, i, locale, "slug-"+v, r)
	exec(t, tx, "INSERT INTO revisions(id,version_id,title,markdown_path,sha256,byte_size,created_by) VALUES(?,?,?,?,?,?,?)", r, v, "Title", "content/"+i+"/"+locale+"/"+r+".md", strings.Repeat("a", 64), 12, o)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return i, v, r
}
func media(t *testing.T, d *sql.DB) string {
	t.Helper()
	m := id(t)
	exec(t, d, "INSERT INTO media(id,storage_key,mime_type,byte_size,sha256,original_filename,created_by) VALUES(?,?,?,?,?,?,?)", m, "uploads/"+m+".png", "image/png", 20, strings.Repeat("b", 64), "photo.png", owner(t, d))
	return m
}
func TestFreshRestartAndConnectionPolicy(t *testing.T) {
	root := privateDir(t)
	d, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, r := item(t, d, "article", "en")
	var conns []*sql.Conn
	for i := 0; i < 4; i++ {
		c, e := d.Conn(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		conns = append(conns, c)
		for _, p := range []struct {
			name string
			want int
		}{{"foreign_keys", 1}, {"busy_timeout", 5000}, {"synchronous", 2}, {"trusted_schema", 0}} {
			var got int
			if e = c.QueryRowContext(context.Background(), "PRAGMA "+p.name).Scan(&got); e != nil || got != p.want {
				t.Fatalf("%s=%d: %v", p.name, got, e)
			}
		}
		var mode string
		if e = c.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&mode); e != nil || mode != "wal" {
			t.Fatal(mode, e)
		}
	}
	for _, c := range conns {
		c.Close()
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var count int
	var timestamp string
	if err = d.QueryRow("SELECT created_at FROM revisions WHERE id=?", r).Scan(&timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err = time.Parse(TimestampLayout, timestamp); err != nil {
		t.Fatal(timestamp)
	}
	if err = d.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != len(migrationTestSource(t)) {
		t.Fatal(count, err)
	}
	for _, table := range []string{"site_settings", "profile_locales", "quizzes"} {
		if err = d.QueryRow("SELECT count(*) FROM sqlite_master WHERE name=?", table).Scan(&count); err != nil || count != 0 {
			t.Fatal("unexpected table", table)
		}
	}
}
func TestContentConstraintsAndPointerOwnership(t *testing.T) {
	d := openTest(t)
	i, v, r := item(t, d, "article", "en")
	_, v2, r2 := item(t, d, "article", "ar")
	reject(t, d, "INSERT INTO content_items(id,key,kind,source_locale) SELECT ?,key,kind,source_locale FROM content_items WHERE id=?", id(t), i)
	reject(t, d, "UPDATE content_items SET key='changed' WHERE id=?", i)
	reject(t, d, "UPDATE content_versions SET locale='fr' WHERE id=?", v)
	reject(t, d, "UPDATE content_versions SET draft_revision_id=? WHERE id=?", r2, v)
	now := UTC(time.Now())
	reject(t, d, "UPDATE content_versions SET published_revision_id=?,first_published_at=?,last_published_at=? WHERE id=?", r2, now, now, v)
	reject(t, d, "UPDATE content_versions SET version=0 WHERE id=?", v)
	exec(t, d, "UPDATE content_versions SET published_revision_id=?,first_published_at=?,last_published_at=? WHERE id=?", r, now, now, v)
	reject(t, d, "UPDATE content_versions SET slug='changed' WHERE id=?", v)
	reject(t, d, "UPDATE revisions SET title='rewrite history' WHERE id=?", r)
	reject(t, d, "DELETE FROM revisions WHERE id=?", r)
	reject(t, d, "INSERT INTO revisions(id,version_id,title,markdown_path,sha256,byte_size,created_by) VALUES(?,?,?,?,?,?,?)", id(t), v2, "Bad path", "../private.md", strings.Repeat("a", 64), 1, owner(t, d))
	reject(t, d, "UPDATE content_items SET updated_at='2026-10-24T10:00:00+03:00' WHERE id=?", i)
}
func TestLocaleAndSlugUniqueness(t *testing.T) {
	d := openTest(t)
	i, v, r := item(t, d, "article", "en")
	var slug string
	_ = d.QueryRow("SELECT slug FROM content_versions WHERE id=?", v).Scan(&slug)
	tx, _ := d.Begin()
	defer tx.Rollback()
	reject(t, tx, "INSERT INTO content_versions(id,item_id,locale,slug,draft_revision_id) VALUES(?,?,?,?,?)", id(t), i, "en", "other", r)
	i2 := id(t)
	exec(t, tx, "INSERT INTO content_items(id,key,kind,source_locale) VALUES(?,?,?,?)", i2, "k-"+i2, "doc", "fr")
	reject(t, tx, "INSERT INTO content_versions(id,item_id,locale,slug,draft_revision_id) VALUES(?,?,?,?,?)", id(t), i2, "en", slug, r)
	reject(t, tx, "INSERT INTO content_versions(id,item_id,locale,slug,draft_revision_id) VALUES(?,?,?,?,?)", id(t), i2, "de", "other", r)
}
func TestMediaAndConfigurationReferences(t *testing.T) {
	d := openTest(t)
	_, _, r := item(t, d, "article", "en")
	m := media(t, d)
	exec(t, d, "INSERT INTO revision_media VALUES(?,?)", r, m)
	reject(t, d, "DELETE FROM media WHERE id=?", m)
	reject(t, d, "INSERT INTO revision_media VALUES(?,?)", r, id(t))
	doc1, doc2, rev1, rev2 := id(t), id(t), id(t), id(t)
	exec(t, d, "INSERT INTO config_documents(id,kind) VALUES(?,?)", doc1, "site")
	exec(t, d, "INSERT INTO config_documents(id,kind) VALUES(?,?)", doc2, "profile")
	for _, pair := range [][2]string{{rev1, doc1}, {rev2, doc2}} {
		exec(t, d, "INSERT INTO config_revisions(id,document_id,snapshot_json,schema_version,colour_algorithm_version,created_by) VALUES(?,?,?,1,1,?)", pair[0], pair[1], "{}", owner(t, d))
	}
	reject(t, d, "UPDATE config_documents SET applied_revision_id=? WHERE id=?", rev2, doc1)
	exec(t, d, "UPDATE config_documents SET applied_revision_id=? WHERE id=?", rev1, doc1)
	reject(t, d, "UPDATE config_revisions SET snapshot_json='{}' WHERE id=?", rev1)
	reject(t, d, "INSERT INTO config_revisions(id,document_id,snapshot_json,schema_version,colour_algorithm_version,created_by) VALUES(?,?,?,1,1,?)", id(t), doc1, "[]", owner(t, d))
	exec(t, d, "INSERT INTO config_revision_media(revision_id,media_id,role) VALUES(?,?,?)", rev1, m, "logo")
	exec(t, d, "DELETE FROM revision_media WHERE revision_id=?", r)
	reject(t, d, "DELETE FROM media WHERE id=?", m)
}
func TestCollectionOrderMembershipAndKinds(t *testing.T) {
	d := openTest(t)
	i, v, r := item(t, d, "lesson", "en")
	i2, _, _ := item(t, d, "lesson", "en")
	doc, _, _ := item(t, d, "doc", "en")
	c, c2 := id(t), id(t)
	for _, n := range []string{c, c2} {
		exec(t, d, "INSERT INTO collections(id,key,kind) VALUES(?,?,'learning')", n, "k-"+n)
	}
	exec(t, d, "INSERT INTO collection_items VALUES(?,?,1)", c, i)
	reject(t, d, "UPDATE collections SET kind='docs' WHERE id=?", c)
	reject(t, d, "UPDATE content_items SET kind='doc' WHERE id=?", i)
	reject(t, d, "INSERT INTO collection_items VALUES(?,?,1)", c, i2)
	reject(t, d, "INSERT INTO collection_items VALUES(?,?,2)", c2, i)
	reject(t, d, "INSERT INTO collection_items VALUES(?,?,2)", c, doc)
	reject(t, d, "INSERT INTO collection_items VALUES(?,?,0)", c, i2)
	now := UTC(time.Now())
	exec(t, d, "UPDATE content_versions SET published_revision_id=?,first_published_at=?,last_published_at=? WHERE id=?", r, now, now, v)
	exec(t, d, "UPDATE collection_items SET position=3 WHERE item_id=?", i)
	reject(t, d, "UPDATE collection_items SET collection_id=? WHERE item_id=?", c2, i)
	reject(t, d, "DELETE FROM collection_items WHERE item_id=?", i)
}
func TestConcurrentVersionConflictIsAtomic(t *testing.T) {
	d := openTest(t)
	_, v, _ := item(t, d, "article", "en")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := d.BeginTx(ctx, nil)
			if err == nil {
				defer tx.Rollback()
				err = BumpVersion(ctx, tx, ContentVersions, v, 1)
				if err == nil {
					err = tx.Commit()
				}
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrVersionConflict):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	var version int
	_ = d.QueryRow("SELECT version FROM content_versions WHERE id=?", v).Scan(&version)
	if version != 2 {
		t.Fatal(version)
	}
}
func TestMigrationHistoryAndRollback(t *testing.T) {
	for _, mode := range []string{"checksum", "newer", "gap", "failed_sql"} {
		t.Run(mode, func(t *testing.T) {
			d := openTest(t)
			raw, err := migrations.Files.ReadFile("0001_core.sql")
			if err != nil {
				t.Fatal(err)
			}
			auth, _ := migrations.Files.ReadFile("0002_owner_security.sql")
			source := migrationTestSource(t)
			_ = auth
			switch mode {
			case "checksum":
				source["0001_core.sql"] = &fstest.MapFile{Data: append(raw, []byte("\n-- changed")...)}
			case "newer":
				exec(t, d, "INSERT INTO schema_migrations VALUES(?,?,?,?)", len(source)+1, fmt.Sprintf("%04d_future.sql", len(source)+1), strings.Repeat("a", 64), UTC(time.Now()))
			case "gap":
				source[fmt.Sprintf("%04d_gap.sql", len(source)+2)] = &fstest.MapFile{Data: []byte("SELECT 1;")}
			case "failed_sql":
				source[fmt.Sprintf("%04d_failed.sql", len(source)+1)] = &fstest.MapFile{Data: []byte("CREATE TABLE should_rollback(id INTEGER); INVALID SQL;")}
			}
			err = migrate(context.Background(), d, source)
			if err == nil {
				t.Fatal("accepted bad migration")
			}
			if mode != "failed_sql" && !errors.Is(err, ErrSchemaMismatch) {
				t.Fatal(err)
			}
			if mode == "failed_sql" {
				var count int
				_ = d.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='should_rollback'").Scan(&count)
				if count != 0 {
					t.Fatal("partial migration")
				}
				_ = d.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count)
				if count != len(source)-1 {
					t.Fatal("partial history")
				}
			}
		})
	}
}
func TestOpenRejectsUnsafeFiles(t *testing.T) {
	root := privateDir(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "blog.sqlite")); err != nil {
		t.Fatal(err)
	}
	if d, err := Open(context.Background(), root); err == nil {
		d.Close()
		t.Fatal("accepted symlink")
	}
	if d, err := Open(context.Background(), "relative"); err == nil {
		d.Close()
		t.Fatal("accepted relative root")
	}
}
func TestIDsUTCAndNoBodyColumns(t *testing.T) {
	a, b := id(t), id(t)
	if a == b || len(a) != 32 {
		t.Fatal("invalid generated IDs")
	}
	if got := UTC(time.Date(2026, 10, 24, 3, 0, 0, 123456789, time.FixedZone("offset", 3*3600))); got != "2026-10-24T00:00:00.123Z" {
		t.Fatal(got)
	}
	d := openTest(t)
	for _, table := range []string{"revisions", "media"} {
		rows, err := d.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var cid, notnull, pk int
			var name, kind string
			var def any
			if err = rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); err != nil {
				t.Fatal(err)
			}
			if kind == "BLOB" || name == "body" || name == "markdown" {
				t.Fatal("body in metadata schema")
			}
		}
		rows.Close()
	}
}

func TestMigrationAppendAndDirtyInitialization(t *testing.T) {
	d := openTest(t)
	raw, _ := migrations.Files.ReadFile("0001_core.sql")
	auth, _ := migrations.Files.ReadFile("0002_owner_security.sql")
	source := migrationTestSource(t)
	_ = raw
	_ = auth
	source[fmt.Sprintf("%04d_test.sql", len(source)+1)] = &fstest.MapFile{Data: []byte("CREATE TABLE extension_test(id INTEGER PRIMARY KEY) STRICT;")}
	if err := migrate(context.Background(), d, source); err != nil {
		t.Fatal(err)
	}
	if err := migrate(context.Background(), d, source); err != nil {
		t.Fatal("rerun", err)
	}
	if err := migrate(context.Background(), d, migrations.Files); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatal("downgrade not rejected", err)
	}
	// A pre-existing unversioned database is never silently adopted as a fresh installation.
	dirty, err := sql.Open("sqlite", "file:"+filepath.Join(privateDir(t), "dirty.sqlite")+"?_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer dirty.Close()
	exec(t, dirty, "CREATE TABLE legacy(id INTEGER)")
	if err = migrate(context.Background(), dirty, migrations.Files); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatal(err)
	}
	var count int
	_ = dirty.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='schema_migrations'").Scan(&count)
	if count != 0 {
		t.Fatal("failed initialization left ledger")
	}
}
func TestSessionRecoveryCannotBecomeFullOwnerAndHashesAreUnique(t *testing.T) {
	d := openTest(t)
	o := owner(t, d)
	now := UTC(time.Now())
	expiry := UTC(time.Now().Add(time.Hour))
	hash := strings.Repeat("f", 64)
	query := "INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at) VALUES(?,?,?,?,?,1,?,?)"
	reject(t, d, query, id(t), o, hash, "owner", "recovery", now, expiry)
	reject(t, d, query, id(t), o, hash, "owner", "password", now, expiry)
	exec(t, d, query, id(t), o, hash, "restricted_enrollment", "recovery", now, expiry)
	reject(t, d, query, id(t), o, hash, "owner", "passkey", now, expiry)
	reject(t, d, query, id(t), o, "RAW_SECRET", "owner", "passkey", now, expiry)
}

func TestMetadataLimitsMatchApprovedDefaults(t *testing.T) {
	d := openTest(t)
	_, v, _ := item(t, d, "article", "en")
	o := owner(t, d)
	query := "INSERT INTO revisions(id,version_id,title,summary,markdown_path,sha256,byte_size,created_by) VALUES(?,?,?,?,?,?,?,?)"
	for _, pair := range [][2]string{{strings.Repeat("a", 201), "summary"}, {"title", strings.Repeat("a", 501)}} {
		r := id(t)
		reject(t, d, query, r, v, pair[0], pair[1], "content/"+r+".md", strings.Repeat("a", 64), 1, o)
	}
}

func migrationTestSource(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	entries, e := migrations.Files.ReadDir(".")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			b, e := migrations.Files.ReadFile(entry.Name())
			if e != nil {
				t.Fatal(e)
			}
			out[entry.Name()] = &fstest.MapFile{Data: b}
		}
	}
	return out
}
