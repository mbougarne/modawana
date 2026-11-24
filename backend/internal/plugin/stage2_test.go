package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/publichttp"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/db"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestStageTwoLifecycle(t *testing.T) {
	s, host := fixture(t)
	ctx := t.Context()
	s.Runtime.SetKeySource(func() ([]byte, error) { return bytes.Repeat([]byte{7}, 32), nil })
	fetches := 0
	s.Runtime.fetchClient = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		fetches++
		if req.URL.Host != "api.example.com" || req.Method != http.MethodPost {
			t.Fatal(req)
		}
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader("accepted"))}, nil
	})}
	step := Migration{Name: "initialize", Checksum: strings.Repeat("a", 64)}
	m := Manifest{
		SchemaVersion: 1, HostAPIVersion: 2, Slug: "stage-two", Name: "Stage two fixture", Module: "plugin.wasm", Version: "1",
		Capabilities: []string{"storage", "settings", "secrets", "jobs", "http_fetch", "routes"},
		Settings:     []SettingField{{Name: "message", Type: "string", Label: map[string]string{"en": "Message"}, Default: json.RawMessage(`"default"`)}},
		Secrets:      []NamedSecret{{Name: "token", Label: map[string]string{"en": "Token"}}},
		Jobs:         []ManifestJob{{Name: "sync", EverySeconds: 60}},
		HTTPSHosts:   []string{"api.example.com"},
		Migrations:   []Migration{step},
		Routes:       []ManifestRoute{{Method: "POST", Path: "/owner"}, {Method: "GET", Path: "/public", Public: true}},
	}
	dir := filepath.Join(s.Dir, m.Slug)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, m.Module), "./testdata/stage2")
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	stage := func() Status {
		t.Helper()
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		items, err := s.List(ctx)
		if err != nil || len(items) != 1 || items[0].CandidateChecksum == "" {
			t.Fatal(items, err)
		}
		return items[0]
	}
	first := stage()
	if _, err := s.ApproveCandidate(ctx, m.Slug, "", 0, pass); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("accepted unbound approval", err)
	}
	record, err := s.ApproveCandidate(ctx, m.Slug, first.CandidateChecksum, 0, pass)
	if err != nil || record.Enabled {
		t.Fatal(record, err)
	}
	if err := s.SaveSettings(ctx, m.Slug, SettingsInput{ExpectedVersion: record.RowVersion, Values: map[string]json.RawMessage{"message": json.RawMessage("123")}}); !errors.Is(err, ErrStorageInvalid) {
		t.Fatal("accepted wrong settings type", err)
	}
	save := func(expected int64, value string) {
		t.Helper()
		if err := s.SaveSettings(ctx, m.Slug, SettingsInput{
			ExpectedVersion: expected, Values: map[string]json.RawMessage{"message": json.RawMessage(value)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	save(record.RowVersion, `"first value"`)
	save(record.RowVersion+1, `"second value"`)
	if err := s.SaveSettings(ctx, m.Slug, SettingsInput{ExpectedVersion: record.RowVersion + 2, RestoreVersion: record.RowVersion + 1}); err != nil {
		t.Fatal(err)
	}
	secret := "test-token"
	if err := s.ChangeSecret(ctx, m.Slug, "token", &secret, record.RowVersion+3, pass); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Configuration(ctx, m.Slug)
	if err != nil || string(cfg.Values["message"]) != `"first value"` || !cfg.Secrets["token"] || len(cfg.History) != 3 {
		t.Fatal(cfg, err)
	}
	var cipher []byte
	if err := s.DB.QueryRowContext(ctx, "SELECT ciphertext FROM plugin_secrets").Scan(&cipher); err != nil || bytes.Contains(cipher, []byte(secret)) {
		t.Fatal("secret was not encrypted", err)
	}
	if _, err := s.Runtime.secretBox(record.ID, "other", cipher, false); err == nil {
		t.Fatal("ciphertext accepted against another secret name")
	}
	record, err = s.SetEnabled(ctx, m.Slug, true, cfg.Version, pass)
	if err != nil {
		t.Fatal(err)
	}
	p := s.loaded[m.Slug]
	job := host.Jobs()[0]
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"initialize": "migration ran", "job": `"first value"`} {
		if got, err := s.Runtime.storage.kvGet(ctx, record.ID, key); err != nil || string(got) != value {
			t.Fatal(key, string(got), err)
		}
	}
	for _, call := range []routeEnvelope{{Method: "GET"}, {Method: "GET", Owner: true}, {Method: "POST", Owner: true}} {
		result, err := s.Runtime.InvokeRoute(ctx, p, call)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]int
		if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
			t.Fatal(err)
		}
		if call.Method == "POST" {
			if body["secret_size"] != len(secret) || body["response_size"] <= 0 || fetches != 1 {
				t.Fatal(body, fetches)
			}
		} else if body["secret_size"] != codeDenied || body["response_size"] != codeDenied || fetches != 0 {
			t.Fatal("public route reached a private capability", body, fetches)
		}
	}
	m.Migrations = append(m.Migrations, Migration{Name: "fail", Checksum: strings.Repeat("b", 64)})
	m.Version = "2"
	update := stage()
	if _, err := s.ApproveCandidate(ctx, m.Slug, update.CandidateChecksum, record.RowVersion, pass); !errors.Is(err, ErrMigration) {
		t.Fatal("failing migration was accepted", err)
	}
	if _, err := s.Runtime.storage.kvGet(ctx, record.ID, "fail"); !errors.Is(err, ErrNotFound) || !p.enabled.Load() {
		t.Fatal("failed upgrade changed storage or running plugin", err)
	}
	m.Migrations = nil
	update = stage()
	if _, err := s.ApproveCandidate(ctx, m.Slug, update.CandidateChecksum, record.RowVersion, pass); !errors.Is(err, ErrMigration) {
		t.Fatal("migration downgrade was accepted", err)
	}
	if _, err := s.SetEnabled(ctx, m.Slug, false, record.RowVersion, pass); err != nil {
		t.Fatal(err)
	}
	if len(host.Jobs()) != 0 || job.Run(ctx) == nil {
		t.Fatal("disabled job remained callable")
	}
}

func TestPluginFetchDestinationPolicy(t *testing.T) {
	for _, value := range []string{
		"127.0.0.1", "10.0.0.4", "169.254.0.1", "100.64.0.2", "::1", "::ffff:127.0.0.1", "fc00::2", "64:ff9b::808:808",
	} {
		if publichttp.IsPublicAddress(netip.MustParseAddr(value)) {
			t.Fatal("non-public address accepted", value)
		}
	}
	if !publichttp.IsPublicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
	for _, raw := range []string{
		`{"method":"GET","url":"http://api.example.com/"}`,
		`{"method":"GET","url":"https://api.example.com:444/"}`,
		`{"method":"GET","url":"https://sub.api.example.com/"}`,
		`{"method":"GET","url":"https://api.example.com/","headers":{"Host":"other.example"}}`,
	} {
		if _, err := pluginHTTPRequest(t.Context(), []string{"api.example.com"}, []byte(raw)); err == nil {
			t.Fatal("request accepted outside capability", raw)
		}
	}
}
