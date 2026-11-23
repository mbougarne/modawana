package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/dashboard"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/outbox"
	"github.com/mbougarne/modawana/backend/internal/plugin"
)

var exampleWasmOnce sync.Once
var exampleWasm []byte
var exampleWasmErr error

func builtExampleWasm(t *testing.T) []byte {
	t.Helper()
	exampleWasmOnce.Do(func() {
		dir, err := os.MkdirTemp("", "plugin-wasm")
		if err != nil {
			exampleWasmErr = err
			return
		}
		defer os.RemoveAll(dir)
		out := filepath.Join(dir, "plugin.wasm")
		build := exec.Command("go", "build", "-o", out, "../plugin/testdata/example")
		build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
		if raw, err := build.CombinedOutput(); err != nil {
			exampleWasmErr = err
			t.Log(string(raw))
			return
		}
		exampleWasm, exampleWasmErr = os.ReadFile(out)
	})
	if exampleWasmErr != nil {
		t.Fatal(exampleWasmErr)
	}
	return exampleWasm
}

func TestPluginRoutesOwnerBoundaryAndLifecycle(t *testing.T) {
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	d, e := db.Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	pluginDir := filepath.Join(root, "plugins", "example")
	if e = os.MkdirAll(pluginDir, 0700); e != nil {
		t.Fatal(e)
	}
	manifest, e := os.ReadFile(filepath.Join("..", "plugin", "testdata", "example", "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(pluginDir, "manifest.json"), manifest, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(pluginDir, "plugin.wasm"), builtExampleWasm(t), 0600); e != nil {
		t.Fatal(e)
	}
	owner, _ := db.NewID()
	sid, _ := db.NewID()
	token := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(token))
	csrf := sha256.Sum256([]byte("csrf:" + token))
	now := time.Now()
	if _, e = d.Exec("INSERT INTO owner(id,login_email) VALUES(?,?)", owner, "private@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec(`INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at) VALUES(?,?,?,'owner','passkey',1,?,?,?)`, sid, owner, hex.EncodeToString(hash[:]), db.UTC(now), db.UTC(now.Add(time.Hour)), db.UTC(now)); e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(d, cfg)
	host := extension.NewHost(nil)
	runtime, e := plugin.NewRuntime(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	defer runtime.Close(context.Background())
	plugins := &plugin.Service{DB: d, Dir: filepath.Join(root, "plugins"), Host: host, Runtime: runtime}
	handler := New(Options{Config: cfg, Auth: a, Domain: &Domain{Auth: a, Extensions: host, Plugins: plugins, Deliveries: &outbox.Worker{DB: d}, Dashboard: &dashboard.Service{DB: d}}})
	request := func(method, path, body string, cookie, withCSRF bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://example.test"+path, bytes.NewBufferString(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: token})
		}
		if withCSRF {
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
			r.Header.Set("Origin", cfg.BaseURL)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	stale := func() {
		t.Helper()
		if _, e := d.Exec("UPDATE sessions SET authenticated_at=?", db.UTC(now.Add(-31*time.Minute))); e != nil {
			t.Fatal(e)
		}
	}
	fresh := func() {
		t.Helper()
		if _, e := d.Exec("UPDATE sessions SET authenticated_at=?", db.UTC(time.Now())); e != nil {
			t.Fatal(e)
		}
	}
	for _, path := range []string{"/api/v1/extension-contributions", "/api/v1/plugin-usage/example"} {
		if w := request("GET", path, "", false, false); w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
	if w := request("GET", "/api/v1/extension-contributions", "", true, false); w.Code != 200 ||
		w.Header().Get("Cache-Control") != "no-store" || strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Fatal("extension catalog", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/plugin-deliveries", "", false, false); w.Code != 401 {
		t.Fatal("deliveries without session", w.Code)
	}
	if w := request("GET", "/api/v1/plugin-deliveries", "", true, false); w.Code != 200 ||
		w.Header().Get("Cache-Control") != "no-store" || strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Fatal("owner deliveries", w.Code, w.Body.String())
	}
	const retryPath = "/api/v1/plugin-deliveries/00000000000000000000000000000001/retry"
	if w := request("POST", retryPath, "", true, false); w.Code != 403 {
		t.Fatal("retry without CSRF/origin", w.Code)
	}
	if w := request("POST", retryPath, "", true, true); w.Code != 409 {
		t.Fatal("retry for nonexistent job", w.Code)
	}
	const dashboardPath = "/api/v1/dashboard-layout"
	if w := request("GET", dashboardPath, "", false, false); w.Code != 401 {
		t.Fatal("dashboard layout without session", w.Code)
	}
	const layout = `{"scope":"personal","expected_version":0,"layout":{"home":"dashboard","appearance":{"theme":"slate"}}}`
	if w := request("PUT", dashboardPath, layout, true, false); w.Code != 403 {
		t.Fatal("dashboard layout without CSRF", w.Code)
	}
	if w := request("PUT", dashboardPath, layout, true, true); w.Code != 200 {
		t.Fatal("dashboard layout save", w.Code, w.Body.String())
	}
	if w := request("GET", dashboardPath, "", true, false); w.Code != 200 ||
		!strings.Contains(w.Body.String(), `"home":"dashboard"`) ||
		w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private dashboard layout", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/plugins", "", false, false); w.Code != 401 {
		t.Fatal("list without session", w.Code)
	}
	if w := request("GET", "/api/v1/plugin-configuration/example", "", false, false); w.Code != 401 {
		t.Fatal("plugin configuration without session", w.Code)
	}
	if w := request("PUT", "/api/v1/plugin-configuration/example/secrets/token", `{"expected_version":1,"value":"test"}`, true, false); w.Code != 403 {
		t.Fatal("secret mutation without origin and CSRF", w.Code)
	}
	w := request("GET", "/api/v1/plugins", "", true, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"available"`) {
		t.Fatal("list", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/plugins/example/install", `{"expected_version":0}`, true, false); w.Code != 403 {
		t.Fatal("install without CSRF/origin", w.Code)
	}
	stale()
	if w = request("POST", "/api/v1/plugins/example/install", `{"expected_version":0}`, true, true); w.Code != 403 {
		t.Fatal("install without recent reauthentication", w.Code, w.Body.String())
	}
	fresh()
	if w = request("POST", "/api/v1/plugins/example/install", `{"expected_version":0}`, true, true); w.Code != 200 {
		t.Fatal("install", w.Code, w.Body.String())
	}
	var record struct {
		Enabled    bool  `json:"enabled"`
		RowVersion int64 `json:"row_version"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &record); e != nil || record.Enabled {
		t.Fatal("install response", w.Body.String())
	}
	// Approved but not enabled: nothing executes, publicly or for the owner.
	if w = request("GET", "/plugins/example/hello", "", false, false); w.Code != 404 {
		t.Fatal("disabled public route served", w.Code)
	}
	stale()
	if w = request("POST", "/api/v1/plugins/example/enable", `{"expected_version":1}`, true, true); w.Code != 403 {
		t.Fatal("enable without recent reauthentication", w.Code)
	}
	fresh()
	if w = request("POST", "/api/v1/plugins/example/enable", `{"expected_version":9}`, true, true); w.Code != 409 {
		t.Fatal("enable with stale version", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/plugins/example/enable", `{"expected_version":1}`, true, true); w.Code != 200 {
		t.Fatal("enable", w.Code, w.Body.String())
	}
	w = request("GET", "/plugins/example/hello", "", false, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"plugin":"example"`) || !strings.Contains(w.Body.String(), `"owner":false`) {
		t.Fatal("public route", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatal("public route content type", contentType)
	}
	if w = request("POST", "/plugins/example/hello", "", false, false); w.Code != 405 {
		t.Fatal("public write", w.Code)
	}
	if w = request("GET", "/api/v1/plugins/example/info", "", false, false); w.Code != 401 {
		t.Fatal("owner route without session", w.Code)
	}
	w = request("GET", "/api/v1/plugins/example/info", "", true, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"owner":true`) {
		t.Fatal("owner route", w.Code, w.Body.String())
	}
	w = request("POST", "/api/v1/plugins/example/info", `{"note":"hi"}`, true, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"body_len":13`) {
		t.Fatal("owner write route", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/plugins/example/info", `{"note":"hi"}`, true, false); w.Code != 403 {
		t.Fatal("owner write route without CSRF", w.Code)
	}
	if w = request("GET", "/api/v1/plugins/example/absent", "", true, false); w.Code != 404 {
		t.Fatal("unregistered owner route", w.Code)
	}
	if w = request("POST", "/api/v1/plugins/example/disable", `{"expected_version":2}`, true, true); w.Code != 200 {
		t.Fatal("disable", w.Code, w.Body.String())
	}
	if w = request("GET", "/plugins/example/hello", "", false, false); w.Code != 404 {
		t.Fatal("disabled plugin served", w.Code)
	}
	if w = request("POST", "/api/v1/plugins/example/remove", `{"expected_version":3}`, true, true); w.Code != 200 {
		t.Fatal("remove", w.Code, w.Body.String())
	}
	w = request("GET", "/api/v1/plugins", "", true, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"available"`) {
		t.Fatal("list after removal", w.Code, w.Body.String())
	}
}
