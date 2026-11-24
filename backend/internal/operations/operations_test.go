package operations

import (
	"bytes"
	"context"
	"database/sql"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/plugin"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"github.com/mbougarne/modawana/backend/internal/theme"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupRestoreDisablesSnapshotCredentialsAndKeepsFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, e := db.Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	files, e := storage.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer files.Close()
	owner, _ := db.NewID()
	key, _ := db.NewID()
	token, _ := db.NewID()
	now := db.UTC(time.Now())
	for _, q := range []struct {
		sql  string
		args []any
	}{{"INSERT INTO owner(id,password_hash,login_email) VALUES(?,?,'owner@example.test')", []any{owner, "private"}}, {"INSERT INTO passkeys(id,owner_id,credential,label,created_at,tested_at) VALUES(?,?,'{}','Snapshot key',?,?)", []any{key, owner, now, now}}, {"INSERT INTO api_tokens(id,owner_id,token_hash,label,expires_at) VALUES(?,?,?,'Snapshot token',?)", []any{token, owner, strings.Repeat("a", 64), db.UTC(time.Now().Add(time.Hour))}}} {
		if _, e = d.Exec(q.sql, q.args...); e != nil {
			t.Fatal(e)
		}
	}

	for _, query := range []string{
		"INSERT INTO oauth_clients VALUES('oauth-client','https://chatgpt.com/connector_platform_oauth_redirect','2099-01-01T00:00:00.000Z')",
		"INSERT INTO oauth_requests(id,client_id,state,challenge,expires_at) VALUES('pending','oauth-client','state','challenge','2099-01-01T00:00:00.000Z')",
		"INSERT INTO oauth_grants(id,code_hash,client_id,token_id,auth_version,access_hash,access_expires,refresh_hash,expires_at) SELECT 'grant','code','oauth-client',id,1,'access','2099-01-01T00:00:00.000Z','refresh','2099-01-01T00:00:00.000Z' FROM api_tokens LIMIT 1",
		"INSERT INTO oauth_refresh_used VALUES('used','grant')",
	} {
		if _, e = d.Exec(query); e != nil {
			t.Fatal(e)
		}
	}
	zero := int64(0)
	cs := &content.Service{DB: d, Files: files}
	rev, e := cs.Save(ctx, owner, "kept", "en", "", content.DraftInput{ExpectedVersion: &zero, Kind: "article", SourceLocale: "en", Slug: "kept", Title: "Kept", Markdown: "Immutable backup body"})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = learning.NewIdentity(d, 180, 10).Create(ctx, "reader")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec("UPDATE store_checkout_settings SET enabled=1"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec("UPDATE editorial_settings SET enabled=1,credential=x'cabbed'"); e != nil {
		t.Fatal(e)
	}
	archive := filepath.Join(t.TempDir(), "backup.zip")
	f, _ := os.Create(archive)
	if e = Backup(ctx, d, files, root, f); e != nil {
		t.Fatal(e)
	}
	f.Close()
	d.Exec("UPDATE passkeys SET revoked_at=? WHERE id=?", now, key)
	d.Exec("UPDATE api_tokens SET revoked_at=? WHERE id=?", now, token)
	target := filepath.Join(t.TempDir(), "restored")
	if e = Restore(ctx, archive, target); e != nil {
		t.Fatal(e)
	}
	if !Maintenance(target) {
		t.Fatal("restore reopened access")
	}
	restored, e := db.Open(ctx, target)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	rf, _ := storage.Open(target)
	defer rf.Close()
	for _, table := range []string{"oauth_clients", "oauth_requests", "oauth_grants", "oauth_refresh_used", "passkeys", "api_tokens", "sessions", "auth_flows", "auth_grants", "recovery_codes", "learning_identities"} {
		var n int
		if e = restored.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal(table, n, e)
		}
	}
	var hasSecret bool
	if e = restored.QueryRow("SELECT password_hash IS NOT NULL OR totp_cipher IS NOT NULL FROM owner").Scan(&hasSecret); e != nil || hasSecret {
		t.Fatal("restored password/TOTP", e)
	}
	var checkoutEnabled, paymentReview bool
	if e = restored.QueryRow("SELECT enabled,payment_review_required FROM store_checkout_settings WHERE id=1").Scan(&checkoutEnabled, &paymentReview); e != nil || checkoutEnabled || !paymentReview {
		t.Fatal("restored checkout should wait for provider reconciliation", checkoutEnabled, paymentReview, e)
	}
	if e = restored.QueryRow("SELECT enabled OR credential IS NOT NULL FROM editorial_settings").Scan(&hasSecret); e != nil || hasSecret {
		t.Fatal("restored editorial credential", e)
	}
	authentication := auth.New(restored, config.Config{})
	grant, e := authentication.Grant(ctx, true)
	if e != nil {
		t.Fatal(e)
	}
	flow, e := authentication.Start(ctx, "restore-test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = authentication.Redeem(ctx, flow, grant, "owner@example.test", ""); e != nil {
		t.Fatal("restored owner cannot redeem local recovery", e)
	}
	got, e := (&content.Service{DB: restored, Files: rf}).Draft(ctx, "kept", "en")
	if e != nil || got.Revision.ID != rev.RevisionID || got.Revision.Markdown != "Immutable backup body" {
		t.Fatal(got, e)
	}
	if e = Resume(ctx, restored, target); e == nil {
		t.Fatal("resumed without tested factor")
	}
	if _, e = restored.Exec("INSERT INTO passkeys(id,owner_id,credential,label,created_at,tested_at) VALUES(?,?,'{}','New tested factor',?,?)", key, owner, now, now); e != nil {
		t.Fatal(e)
	}
	if e = Resume(ctx, restored, target); e != nil || Maintenance(target) {
		t.Fatal(e)
	}
	if e = Restore(ctx, archive, target); e == nil {
		t.Fatal("overwrote live data")
	}
	// Removing a referenced file and rebuilding valid archive checksums still fails.
	extracted, m, e := bundle.Read(archive, "backup")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(extracted)
	paths := []string{}
	for _, entry := range m.Files {
		if entry.Path == "blog.sqlite" {
			paths = append(paths, entry.Path)
		}
	}
	bad := filepath.Join(t.TempDir(), "missing.zip")
	f, _ = os.Create(bad)
	bundle.Write(f, "backup", extracted, paths)
	f.Close()
	missingTarget := filepath.Join(t.TempDir(), "missing")
	if e = Restore(ctx, bad, missingTarget); e == nil {
		t.Fatal("missing body accepted")
	}
	if _, e = os.Stat(missingTarget); !os.IsNotExist(e) {
		t.Fatal("partial restore published")
	}
}
func TestBackupSnapshotUnderConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, e := db.Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	files, _ := storage.Open(root)
	defer files.Close()
	owner, _ := db.NewID()
	d.Exec("INSERT INTO owner(id) VALUES(?)", owner)
	cs := &content.Service{DB: d, Files: files}
	done := make(chan error, 1)
	go func() {
		var version int64
		for i := 0; i < 8; i++ {
			r, e := cs.Save(ctx, owner, "concurrent", "en", "", content.DraftInput{ExpectedVersion: &version, Kind: "article", SourceLocale: "en", Slug: "concurrent", Title: "Concurrent", Markdown: strings.Repeat("body ", i+1)})
			if e != nil {
				done <- e
				return
			}
			version = r.Version
		}
		done <- nil
	}()
	var out bytes.Buffer
	if e = Backup(ctx, d, files, root, &out); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	archive := filepath.Join(t.TempDir(), "backup.zip")
	os.WriteFile(archive, out.Bytes(), 0600)
	if e = Restore(ctx, archive, filepath.Join(t.TempDir(), "restored")); e != nil {
		t.Fatal(e)
	}
}
func TestMaintenanceKeepsReferencesAndDeletesOnlyOldOrphans(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, _ := db.Open(ctx, root)
	defer d.Close()
	files, _ := storage.Open(root)
	defer files.Close()
	owner, _ := db.NewID()
	d.Exec("INSERT INTO owner(id) VALUES(?)", owner)
	zero := int64(0)
	cs := &content.Service{DB: d, Files: files}
	if _, e := cs.Save(ctx, owner, "kept", "en", "", content.DraftInput{ExpectedVersion: &zero, Kind: "article", SourceLocale: "en", Slug: "kept", Title: "Kept", Markdown: "keep"}); e != nil {
		t.Fatal(e)
	}
	var orphan storage.File
	if e := files.Exclusive(ctx, func() error { var e error; orphan, _, e = files.WriteLocked(owner, "en", []byte("orphan")); return e }); e != nil {
		t.Fatal(e)
	}
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(filepath.Join(root, orphan.Key), old, old)
	result, e := Cleanup(ctx, d, files)
	if e != nil || result.Files != 1 {
		t.Fatal(result, e)
	}
	if _, e = cs.Draft(ctx, "kept", "en"); e != nil {
		t.Fatal(e)
	}
}

func TestBackupRestoresApprovedThemePacks(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const id = "pack-archive"
	const payload = `{"api_version":1,"name":"Archived theme","layout":"{{template \"header\" .}}{{template \"content\" .}}{{template \"footer\" .}}"}`
	writePack := func(id, raw string) {
		t.Helper()
		p := filepath.Join(root, theme.PackPath(id))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writePack(id, payload)
	writePack("pack-candidate", payload)
	packs := &theme.Service{DB: d, DataRoot: root}
	pack, err := theme.ReadPack(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := packs.Approve(ctx, id, pack.Checksum, "", func(*sql.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	backup := func() string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "snapshot.zip")
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := Backup(ctx, d, store, root, f); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	restore := func(archive string, available bool) {
		t.Helper()
		destination := filepath.Join(t.TempDir(), "restored")
		if err := Restore(ctx, archive, destination); err != nil {
			t.Fatal(err)
		}
		snapshot, err := db.Open(ctx, destination)
		if err != nil {
			t.Fatal(err)
		}
		defer snapshot.Close()
		restored := &theme.Service{DB: snapshot, DataRoot: destination}
		out, ok, err := restored.Appearance(ctx, snapshot, id)
		if err != nil || ok != available || available && out.ID != id || !available && out.ID != theme.Classic {
			t.Fatalf("unexpected restored theme %q, %v, %v", out.ID, ok, err)
		}
		if _, err := os.Stat(filepath.Join(destination, theme.PackPath("pack-candidate"))); !os.IsNotExist(err) {
			t.Fatal("unapproved file restored")
		}
	}
	archive := backup()
	restore(archive, true)
	writePack(id, strings.Replace(payload, "Archived theme", "Edited theme", 1))
	restore(backup(), false)
	extracted, m, err := bundle.Read(archive, "backup")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(extracted)
	if err := os.WriteFile(filepath.Join(extracted, theme.PackPath(id)), []byte(payload+" "), 0600); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.zip")
	f, err := os.Create(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(m.Files))
	for _, item := range m.Files {
		paths = append(paths, item.Path)
	}
	if err := bundle.Write(f, "backup", extracted, paths); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Restore(ctx, corrupt, filepath.Join(t.TempDir(), "corrupt")); err != bundle.ErrInvalid {
		t.Fatal("restore accepted a file with the wrong approval checksum", err)
	}
}

func TestBackupKeepsApprovedPluginFilesAndScrubsSecrets(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	plugins := &plugin.Service{DB: d, Dir: filepath.Join(root, "plugins")}
	const sentinel = "opaque-test-ciphertext-bytes"
	const removed = "previously-deleted-test-ciphertext"
	for _, slug := range []string{"retained", "missing"} {
		dir := filepath.Join(plugins.Dir, slug)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		manifest := `{"schema_version":1,"host_api_version":1,"slug":"` + slug + `","name":"Backup fixture","version":"1","module":"plugin.wasm","capabilities":["storage"]}`
		for name, payload := range map[string]string{"manifest.json": manifest, "plugin.wasm": "module fixture"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
		}
		record, err := plugins.Approve(ctx, slug, 0, func(*sql.Tx) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec("UPDATE plugin_installs SET enabled=1 WHERE id=?", record.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec("INSERT INTO plugin_secrets VALUES(?,?,?)", record.ID, "token", []byte(sentinel)); err != nil {
			t.Fatal(err)
		}
		if slug == "missing" {
			if _, err := d.Exec("INSERT INTO plugin_secrets VALUES(?,?,?)", record.ID, "removed", []byte(removed)); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Exec("DELETE FROM plugin_secrets WHERE plugin_id=? AND name='removed'", record.ID); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(dir, "plugin.wasm")); err != nil {
				t.Fatal(err)
			}
		}
	}
	archive := filepath.Join(t.TempDir(), "backup.zip")
	var data bytes.Buffer
	if err := Backup(ctx, d, store, root, &data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	extracted, _, err := bundle.Read(archive, "backup")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(extracted)
	file, err := os.ReadFile(filepath.Join(extracted, "blog.sqlite"))
	if err != nil || bytes.Contains(file, []byte(sentinel)) || bytes.Contains(file, []byte(removed)) {
		t.Fatal("plugin secret survived in the archive", err)
	}
	var existing int
	if err := d.QueryRow("SELECT count(*) FROM plugin_secrets").Scan(&existing); err != nil || existing != 2 {
		t.Fatal("backup changed the live secrets", existing, err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if err := Restore(ctx, archive, dest); err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	for _, slug := range []string{"retained", "missing"} {
		var enabled bool
		if err := restored.QueryRow("SELECT enabled FROM plugin_installs WHERE slug=?", slug).Scan(&enabled); err != nil || enabled != (slug == "retained") {
			t.Fatal(slug, enabled, err)
		}
	}
	if _, err := plugin.ReadDirectory(filepath.Join(dest, "plugins"), "retained"); err != nil {
		t.Fatal("approved files did not survive restore", err)
	}
}
