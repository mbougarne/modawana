package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const scriptName = "/assets/app.0123456789abcdef0123.js"
const styleName = "/assets/app.0123456789abcdef0123.css"

func buildFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{scriptName, styleName} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("test asset"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := Manifest{1, []string{scriptName}, []string{styleName}, []string{scriptName, styleName}}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}
func testServer(t *testing.T, ready func(context.Context) error) (http.Handler, *bytes.Buffer) {
	t.Helper()
	a, e := LoadAssets(buildFixture(t))
	if e != nil {
		t.Fatal(e)
	}
	logs := &bytes.Buffer{}
	return New(Options{Assets: a, Ready: ready, Logger: slog.New(slog.NewJSONHandler(logs, nil))}), logs
}
func TestSameOriginAndMissingRoutes(t *testing.T) {
	h, _ := testServer(t, func(context.Context) error { return nil })
	for _, tc := range []struct {
		path     string
		status   int
		contains string
	}{
		{"/en", 200, `lang="en"`}, {"/ar", 200, `dir="rtl"`}, {"/fr", 200, `lang="fr"`}, {"/healthz", 200, `"status":"ok"`}, {"/readyz", 200, `"status":"ok"`}, {"/api/v1/public/runtime", 200, `"api_version":"v1"`}, {scriptName, 200, "test asset"}, {"/api/v1/missing", 404, `"code":"not_found"`}, {"/api", 404, `"code":"not_found"`}, {"/media/private", 404, `"code":"not_found"`}, {"/assets/missing.js", 404, `"code":"not_found"`}, {"/assets/../manifest.json", 404, `"code":"not_found"`}, {"/assets/%2e%2e/secret", 404, `"code":"not_found"`}, {"/unknown", 404, `id="root"`}} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(w.Header().Get("X-Request-ID")) {
				t.Fatal("missing request ID")
			}
			if strings.HasPrefix(tc.path, "/api") || strings.HasPrefix(tc.path, "/media") {
				if strings.Contains(w.Body.String(), "<html") {
					t.Fatal("SPA fallback")
				}
			}
		})
	}
}
func TestHeadersNoSecretsAndReadiness(t *testing.T) {
	h, logs := testServer(t, func(context.Context) error { return errors.New("private sqlite path") })
	for _, path := range []string{"/readyz?token=SECRET", "/en?secret=SECRET"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer SECRET")
		r.Header.Set("Cookie", "session=SECRET")
		r.Header.Set("X-Request-ID", "SECRET")
		r.Header.Set("X-Forwarded-Host", "evil.example")
		r.Header.Set("X-Forwarded-Proto", "http")
		h.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "private sqlite") || strings.Contains(w.Body.String(), "evil.example") {
			t.Fatal("leak")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache")
		}
		if strings.HasPrefix(path, "/readyz") && w.Code != 503 {
			t.Fatal(w.Code)
		}
		if strings.HasPrefix(path, "/en") {
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+w.Header().Get("X-Request-ID")+"'") {
				t.Fatal("missing CSP nonce")
			}
		}
	}
	if strings.Contains(logs.String(), "SECRET") || strings.Contains(logs.String(), "sqlite") {
		t.Fatal(logs.String())
	}
}
func TestMethodAndUnavailable(t *testing.T) {
	h, _ := testServer(t, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/public/runtime", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestJSONBoundaries(t *testing.T) {
	for _, tc := range []struct {
		body, contentType string
		status            int
	}{
		{`{"name":"ok"}`, "application/json", 200}, {`{"name":`, "application/json", 400}, {`{"name":"ok","extra":1}`, "application/json", 400}, {`{"name":"ok"} {}`, "application/json", 400}, {`null`, "application/json", 400}, {`[]`, "application/json", 400}, {``, "application/json", 400}, {`{}`, "text/plain", 415}, {`{"name":"` + strings.Repeat("a", 100) + `"}`, "application/json", 413}} {
		t.Run(tc.body, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			r.ContentLength = -1
			var dst struct {
				Name string `json:"name"`
			}
			ok := DecodeJSON(w, r, &dst, 64)
			if ok {
				w.WriteHeader(200)
			}
			if w.Code != tc.status {
				t.Fatalf("got %d body %s", w.Code, w.Body)
			}
		})
	}
}
func TestAssetsFailClosed(t *testing.T) {
	for _, kind := range []string{"traversal", "symlink", "unknown", "missing"} {
		t.Run(kind, func(t *testing.T) {
			root := buildFixture(t)
			switch kind {
			case "traversal":
				_ = os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"version":1,"scripts":["/assets/../secret.js"],"styles":[],"assets":["/assets/../secret.js"]}`), 0600)
			case "unknown":
				_ = os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"version":2,"scripts":[],"styles":[],"assets":[]}`), 0600)
			case "missing":
				_ = os.Remove(filepath.Join(root, scriptName))
			case "symlink":
				_ = os.Remove(filepath.Join(root, scriptName))
				secret := filepath.Join(t.TempDir(), "secret")
				_ = os.WriteFile(secret, []byte("SECRET"), 0600)
				if err := os.Symlink(secret, filepath.Join(root, scriptName)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := LoadAssets(root); err == nil {
				t.Fatal("accepted invalid assets")
			}
		})
	}
}
func TestAssetSnapshotAndHead(t *testing.T) {
	root := buildFixture(t)
	a, err := LoadAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, scriptName), []byte("replacement"), 0600)
	h := New(Options{Assets: a})
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, scriptName, nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
			t.Fatal(w.Code)
		}
		if method == "GET" && w.Body.String() != "test asset" {
			t.Fatal("snapshot changed")
		}
		if method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
	}
}

func TestOpenAPIErrorCodesMatchImplementation(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/api/v1.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Properties map[string]struct {
						Enum []string `json:"enum"`
					} `json:"properties"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	codes := spec.Components.Schemas["ErrorEnvelope"].Properties["error"].Properties["code"].Enum
	if len(codes) != len(statuses) {
		t.Fatal("schema code count differs")
	}
	for _, code := range codes {
		if _, ok := statuses[ErrorCode(code)]; !ok {
			t.Fatal("schema drift", code)
		}
	}
}
