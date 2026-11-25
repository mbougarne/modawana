package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/operations"
	"github.com/mbougarne/modawana/backend/internal/plugin"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

func TestCatalogApprovalAndRecovery(t *testing.T) {
	ctx := context.Background()
	root, inputs := filepath.Join(t.TempDir(), "data"), t.TempDir()
	database, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	runtime, err := plugin.NewRuntime(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(ctx)
	host := extension.NewHost(nil)
	plugins := &plugin.Service{DB: database, Dir: filepath.Join(root, "plugins"), Host: host, Runtime: runtime}
	themes := &theme.Service{DB: database, DataRoot: root}
	installer := Installer{Root: root, Plugins: plugins, Themes: themes}
	files, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()

	wasm := filepath.Join(inputs, "example.wasm")
	build := exec.Command("go", "build", "-trimpath", "-o", wasm, "../plugin/testdata/example")
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile example: %s: %v", out, err)
	}
	module, err := os.ReadFile(wasm)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	req := Request{Index: filepath.Join(inputs, "index.json"), Signature: filepath.Join(inputs, "signature.json"),
		Keyring: filepath.Join(inputs, "keys.json"), Bundle: filepath.Join(inputs, "bundle.zip"),
		Kind: "plugin", ID: "example", Version: "1"}
	trusted := keyring{Keys: map[string]string{"test": base64.StdEncoding.EncodeToString(pub)}}
	writeJSON(t, req.Keyring, trusted)
	signBundle := func(payload []byte, api int, expires time.Time) {
		t.Helper()
		if err := os.WriteFile(req.Bundle, payload, 0600); err != nil {
			t.Fatal(err)
		}
		raw := writeJSON(t, req.Index, index{Schema: 1, ExpiresAt: expires.Format(time.RFC3339), Entries: []Entry{{
			Kind: req.Kind, ID: req.ID, Version: req.Version, APIVersion: api, Bytes: int64(len(payload)), SHA256: digest(payload),
		}}})
		writeJSON(t, req.Signature, signature{KeyID: "test", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw))})
	}

	first := plugin.Manifest{SchemaVersion: 1, HostAPIVersion: 1, Slug: "example", Name: "Example", Version: "1",
		Module: "plugin.wasm", Capabilities: []string{"storage", "routes"}, Routes: []plugin.ManifestRoute{{Method: "GET", Path: "/hello", Public: true}}}
	firstZIP := pluginZIP(t, first, module)
	signBundle(firstZIP, 1, now.Add(time.Hour))
	review, err := installer.Run(ctx, req, "", now)
	if err != nil || review.Plugin.Candidate.Version != "1" || review.Plugin.Enabled {
		t.Fatal(review, err)
	}
	if _, err := installer.Run(ctx, req, strings.Repeat("a", 64), now); err == nil {
		t.Fatal("unreviewed install succeeded")
	}
	if _, err := installer.Run(ctx, req, review.ReviewSHA256, now); err != nil {
		t.Fatal(err)
	}
	pass := func(tx *sql.Tx) error { return nil }
	record, err := plugins.SetEnabled(ctx, "example", true, 1, pass)
	if err != nil || !host.Installed("example") {
		t.Fatal(record, err)
	}
	// A real offline install uses a fresh host with no serving process.
	offline := &plugin.Service{DB: database, Dir: plugins.Dir, Host: extension.NewHost(nil), Runtime: runtime}
	installer.Plugins = offline

	req.Version = "2"
	next := first
	next.HostAPIVersion, next.Version = 2, req.Version
	next.Migrations = []plugin.Migration{{Name: "fail", Checksum: strings.Repeat("a", 64)}}
	nextZIP := pluginZIP(t, next, module)
	signBundle(nextZIP, 2, now.Add(time.Hour))
	review, err = installer.Run(ctx, req, "", now)
	if err != nil || !review.Plugin.Enabled || len(review.Plugin.Candidate.Migrations) != 1 || len(review.Plugin.Current.Migrations) != 0 {
		t.Fatal(review, err)
	}
	if _, err := installer.Run(ctx, req, review.ReviewSHA256, now); err == nil {
		t.Fatal("failing migration accepted")
	}

	signBundle(nextZIP, plugin.HostAPIVersion+1, now.Add(time.Hour))
	if _, err := installer.Run(ctx, req, "", now); err == nil {
		t.Fatal("unsupported host API accepted")
	}
	signBundle(nextZIP, 2, now.Add(-time.Hour))
	if _, err := installer.Run(ctx, req, "", now); err == nil {
		t.Fatal("expired catalog accepted")
	}
	signBundle(nextZIP, 2, now.Add(time.Hour))
	trusted.Revoked = []string{"test"}
	writeJSON(t, req.Keyring, trusted)
	if _, err := installer.Run(ctx, req, "", now); err == nil {
		t.Fatal("revoked key accepted")
	}
	trusted.Revoked = nil
	writeJSON(t, req.Keyring, trusted)
	if err := os.WriteFile(req.Bundle, append(nextZIP, 0), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Run(ctx, req, "", now); err == nil {
		t.Fatal("tampered package accepted")
	}
	signBundle(nextZIP, 2, now.Add(time.Hour))
	raw, err := os.ReadFile(req.Index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(req.Index, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Run(ctx, req, "", now); err == nil {
		t.Fatal("tampered catalog accepted")
	}
	if err := offline.LoadEnabled(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := offline.List(ctx)
	if err != nil || len(status) != 1 || status[0].Version != "1" || !status[0].Running {
		t.Fatal(status, err)
	}
	if _, err := database.Exec("INSERT INTO plugin_kv(plugin_id,key,value,updated_at) VALUES(?,?,?,?)", record.ID, "example", []byte("retained"), db.UTC(now)); err != nil {
		t.Fatal(err)
	}
	req.Version = "3"
	next.Version, next.Migrations = req.Version, nil
	signBundle(pluginZIP(t, next, module), 2, now.Add(time.Hour))
	offline = &plugin.Service{DB: database, Dir: plugins.Dir, Host: extension.NewHost(nil), Runtime: runtime}
	installer.Plugins = offline
	review, err = installer.Run(ctx, req, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Run(ctx, req, review.ReviewSHA256, now); err != nil {
		t.Fatal(err)
	}
	if err := offline.LoadEnabled(ctx); err != nil || !offline.Host.Installed("example") {
		t.Fatal("upgraded plugin failed to load", err)
	}
	if _, err := offline.SetEnabled(ctx, "example", false, record.RowVersion+1, pass); err != nil || offline.Host.Installed("example") {
		t.Fatal("disable failed", err)
	}

	req.Kind, req.ID, req.Version = "theme", "pack-example", "1"
	themeBytes := []byte(`{"api_version":1,"name":"Pack","layout":"{{template \"header\" .}}{{template \"content\" .}}{{template \"footer\" .}}","css":""}`)
	signBundle(themeBytes, 1, now.Add(time.Hour))
	review, err = installer.Run(ctx, req, "", now)
	if err != nil || review.CandidateTheme != "Pack" {
		t.Fatal(review, err)
	}
	if _, err := installer.Run(ctx, req, review.ReviewSHA256, now); err != nil {
		t.Fatal(err)
	}
	if _, err := themes.ApprovedPack(ctx, database, "pack-example"); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(inputs, "backup.zip")
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	err = operations.Backup(ctx, database, files, root, out)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if err := operations.Restore(ctx, archive, restored); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(ctx, restored)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restoredPlugins := &plugin.Service{DB: reopened, Dir: filepath.Join(restored, "plugins")}
	status, err = restoredPlugins.List(ctx)
	if err != nil || len(status) != 1 || status[0].Version != "3" || status[0].Enabled || status[0].State != "installed" {
		t.Fatal(status, err)
	}
	var preserved string
	if err := reopened.QueryRow("SELECT value FROM plugin_kv WHERE plugin_id=? AND key='example'", record.ID).Scan(&preserved); err != nil || preserved != "retained" {
		t.Fatal(preserved, err)
	}
	if _, err := (&theme.Service{DB: reopened, DataRoot: restored}).ApprovedPack(ctx, reopened, "pack-example"); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, file string, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func pluginZIP(t *testing.T, manifest plugin.Manifest, wasm []byte) []byte {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, file := range []struct {
		name string
		data []byte
	}{{"manifest.json", raw}, {"plugin.wasm", wasm}} {
		entry, err := archive.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
