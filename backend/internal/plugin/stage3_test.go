package plugin

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/extension"
)

func installStageThreeFixture(t *testing.T, service *Service, manifest Manifest, module []byte) Install {
	t.Helper()
	dir := filepath.Join(service.Dir, manifest.Slug)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.Module), module, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	items, err := service.List(ctx)
	if err != nil || len(items) != 1 || items[0].CandidateChecksum == "" {
		t.Fatal(items, err)
	}
	record, err := service.ApproveCandidate(ctx, manifest.Slug, items[0].CandidateChecksum, 0, pass)
	if err != nil {
		t.Fatal(err)
	}
	record, err = service.SetEnabled(ctx, manifest.Slug, true, record.RowVersion, pass)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func buildWASIFixture(t *testing.T, source string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.wasm")
	build := exec.Command("go", "build", "-o", path, source)
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	return fixtureFile(t, path)
}

func fixtureFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTypeScriptMCPPlugin(t *testing.T) {
	path := os.Getenv("MODAWANA_TYPESCRIPT_PLUGIN_WASM")
	if path == "" {
		t.Skip("build the TypeScript example and set MODAWANA_TYPESCRIPT_PLUGIN_WASM")
	}
	const slug = "typescript-example"
	manifest, err := ParseManifest(fixtureFile(t, filepath.Join(filepath.Dir(path), "manifest.json")), slug)
	if err != nil {
		t.Fatal(err)
	}
	service, host := fixture(t)
	record := installStageThreeFixture(t, service, manifest, fixtureFile(t, path))
	tool, ok := host.MCPTool(slug + "__echo")
	if !ok || !tool.RequiresPluginScope || !tool.ReadOnly {
		t.Fatal("enabled tool was not registered with its scope")
	}
	for _, invalid := range []string{`{}`, `{"message":7}`, `{"message":null}`, `{"message":"valid","extra":true}`, `{"message":"` + strings.Repeat("x", toolStringLimit+1) + `"}`} {
		if _, err := tool.Call(t.Context(), json.RawMessage(invalid)); err == nil {
			t.Fatal("invalid tool arguments accepted")
		}
	}
	out, err := tool.Call(t.Context(), json.RawMessage(`{"message":"مرحبا"}`))
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err != nil || json.Unmarshal(out, &result) != nil || len(result.Content) != 1 ||
		result.Content[0].Text != `{"api":3,"kind":"mcp","tool":"echo","arguments":{"message":"مرحبا"}}` {
		t.Fatal(string(out), err)
	}
	if _, err := service.SetEnabled(t.Context(), slug, false, record.RowVersion, pass); err != nil {
		t.Fatal(err)
	}
	if _, ok := host.MCPTool(slug + "__echo"); ok {
		t.Fatal("disabled tool remains discoverable")
	}
	if out, err := tool.Call(t.Context(), json.RawMessage(`{"message":"disabled"}`)); err != nil || !strings.Contains(string(out), `"isError":true`) {
		t.Fatal("disabled tool still runs", string(out), err)
	}
}

func TestPluginMCPInvocationCannotWriteStorage(t *testing.T) {
	service, host := fixture(t)
	manifest := Manifest{
		SchemaVersion: 1, HostAPIVersion: 3, Slug: "tool-fixture", Name: "Tool fixture", Module: "plugin.wasm", Version: "1",
		Capabilities: []string{"storage", "secrets", "http_fetch", "mcp_tools"},
		Secrets:      []NamedSecret{{Name: "token", Label: map[string]string{"en": "Token"}}},
		HTTPSHosts:   []string{"api.example.com"},
		MCPTools:     []ManifestTool{{Name: "check", Description: "Check read-only storage."}},
	}
	oldManifest := manifest
	oldManifest.HostAPIVersion = 2
	if err := oldManifest.validate(manifest.Slug); !errors.Is(err, ErrManifest) {
		t.Fatal("API 2 manifest accepted MCP tools", err)
	}
	record := installStageThreeFixture(t, service, manifest, buildWASIFixture(t, "./testdata/stage2"))
	tool, ok := host.MCPTool("tool-fixture__check")
	if !ok {
		t.Fatal("tool registration failed")
	}
	out, err := tool.Call(t.Context(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(string(out), `"isError":true`) {
		t.Fatal("read-only violation was not rejected", string(out), err)
	}
	if _, err := service.Runtime.storage.kvGet(t.Context(), record.ID, "mcp"); !errors.Is(err, ErrNotFound) {
		t.Fatal("tool changed storage", err)
	}
}

func TestSocialCardsWASMParity(t *testing.T) {
	const slug = "social-cards-wasm"
	manifest, err := ParseManifest(fixtureFile(t, "../../cmd/plugin-social-cards/manifest.json"), slug)
	if err != nil {
		t.Fatal(err)
	}
	service, host := fixture(t)
	installStageThreeFixture(t, service, manifest, buildWASIFixture(t, "../../cmd/plugin-social-cards"))
	payload := extension.RenderedMetaPayload{
		Origin: "https://example.test", Title: "المقال", Kind: "article",
		Site:    extension.RenderedMetaSite{Wordmark: "مدونة", BannerEnabled: true, BannerID: "banner", BannerAlt: "الصورة"},
		Content: &extension.RenderedMetaContent{CoverID: "cover", Media: []extension.RenderedMetaMedia{{ID: "cover", Kind: "image", MIME: "image/png", Width: 1200, Height: 630}}},
	}
	compare := func() {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		results := host.DispatchHook(t.Context(), extension.HookRenderedMeta, raw)
		var got extension.RenderedMetaResult
		if len(results) != 1 || json.Unmarshal(results[0], &got) != nil || got != extension.SelectRenderedMeta(payload) {
			t.Fatal("native and WebAssembly metadata differ", results, got)
		}
	}
	compare()
	payload.Kind = "lesson"
	payload.Content = &extension.RenderedMetaContent{Locked: true}
	compare()
	payload.Content = nil
	payload.Site.BannerEnabled = false
	compare()
}
