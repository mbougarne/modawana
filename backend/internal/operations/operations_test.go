package operations

import (
	"bytes"
	"context"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/storage"
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
	archive := filepath.Join(t.TempDir(), "backup.zip")
	f, _ := os.Create(archive)
	if e = Backup(ctx, d, files, f); e != nil {
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
	if e = Backup(ctx, d, files, &out); e != nil {
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
