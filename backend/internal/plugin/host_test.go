package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
)

var exampleModule []byte

// TestMain compiles the checked-in example plugin once for every test; the
// binary itself is never committed.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "plugin-wasm")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "plugin.wasm")
	build := exec.Command("go", "build", "-o", out, "./testdata/example")
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	if raw, err := build.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, "example plugin build failed:", string(raw))
		os.Exit(1)
	}
	if exampleModule, err = os.ReadFile(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func pass(*sql.Tx) error { return nil }

func fixture(t *testing.T) (*Service, *extension.Host) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	runtime, err := NewRuntime(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close(ctx) })
	host := extension.NewHost(nil)
	return &Service{DB: database, Dir: filepath.Join(root, "plugins"), Host: host, Runtime: runtime}, host
}

func writePlugin(t *testing.T, s *Service, slug, manifest string) {
	t.Helper()
	dir := filepath.Join(s.Dir, slug)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.wasm"), exampleModule, 0600); err != nil {
		t.Fatal(err)
	}
}

func exampleManifest(slug string) string {
	raw, err := os.ReadFile(filepath.Join("testdata", "example", "manifest.json"))
	if err != nil {
		panic(err)
	}
	return strings.ReplaceAll(string(raw), `"example"`, `"`+slug+`"`)
}

func enableExample(t *testing.T, s *Service, slug string) Install {
	t.Helper()
	ctx := context.Background()
	record, err := s.Approve(ctx, slug, 0, pass)
	if err != nil {
		t.Fatal(err)
	}
	if record.Enabled {
		t.Fatal("approval must not enable")
	}
	record, err = s.SetEnabled(ctx, slug, true, record.RowVersion, pass)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func dispatchStorage(t *testing.T, host *extension.Host, request string) map[string]any {
	t.Helper()
	results := host.DispatchHook(context.Background(), "test.storage", []byte(request))
	if len(results) != 1 {
		t.Fatal("storage dispatch results", len(results))
	}
	var out map[string]any
	if err := json.Unmarshal(results[0], &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func code(t *testing.T, out map[string]any) int {
	t.Helper()
	value, ok := out["code"].(float64)
	if !ok {
		t.Fatal("missing code", out)
	}
	return int(value)
}

// builtinEcho mirrors the example plugin's test.echo result so dispatch parity
// between a built-in and a Wasm plugin is byte-exact.
type builtinEcho struct{}

func (builtinEcho) Slug() string { return "builtin-echo" }
func (builtinEcho) Register(r *extension.Registrar) error {
	return r.Hook("test.echo", 1, func(_ context.Context, hook string, payload []byte) ([]byte, error) {
		return json.Marshal(struct {
			Hook    string          `json:"hook"`
			Version int             `json:"version"`
			Echo    json.RawMessage `json:"echo"`
		}{hook, 1, payload})
	})
}

func TestHookDispatchParityWithBuiltin(t *testing.T) {
	s, host := fixture(t)
	if err := host.Install(builtinEcho{}); err != nil {
		t.Fatal(err)
	}
	writePlugin(t, s, "example", exampleManifest("example"))
	enableExample(t, s, "example")
	payload := []byte(`{"value":42}`)
	results := host.DispatchHook(context.Background(), "test.echo", payload)
	if len(results) != 2 {
		t.Fatal("expected builtin and plugin results, got", len(results))
	}
	if string(results[0]) != string(results[1]) {
		t.Fatalf("parity broken: builtin %s plugin %s", results[0], results[1])
	}
}

func TestSandboxDeniesFilesystemNetworkAndEnvironment(t *testing.T) {
	s, host := fixture(t)
	writePlugin(t, s, "example", exampleManifest("example"))
	enableExample(t, s, "example")
	results := host.DispatchHook(context.Background(), "test.sandbox", []byte(`{}`))
	if len(results) != 1 {
		t.Fatal("sandbox dispatch results", len(results))
	}
	var probe map[string]any
	if err := json.Unmarshal(results[0], &probe); err != nil {
		t.Fatal(err)
	}
	if probe["env_count"] != float64(0) {
		t.Fatal("environment leaked", probe)
	}
	for _, key := range []string{"file_error", "dir_error", "net_error", "write_error"} {
		if probe[key] != true {
			t.Fatal("sandbox probe succeeded:", key, probe)
		}
	}
}

func TestInvocationLimits(t *testing.T) {
	s, host := fixture(t)
	writePlugin(t, s, "example", exampleManifest("example"))
	enableExample(t, s, "example")
	s.mu.Lock()
	p := s.loaded["example"]
	s.mu.Unlock()
	ctx := context.Background()
	previous := invokeTimeout
	invokeTimeout = 300 * time.Millisecond
	defer func() { invokeTimeout = previous }()
	started := time.Now()
	if _, err := s.Runtime.InvokeHook(ctx, p, "test.spin", 1, []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Fatal("busy loop not interrupted:", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("interruption too slow")
	}
	invokeTimeout = previous
	if _, err := s.Runtime.InvokeHook(ctx, p, "test.memory", 1, []byte(`{}`)); err == nil {
		t.Fatal("memory growth beyond the limit succeeded")
	}
	if _, err := s.Runtime.InvokeHook(ctx, p, "test.big", 1, []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatal("oversized result accepted:", err)
	}
	// The host registry drops the failures: dispatch returns no results.
	if results := host.DispatchHook(ctx, "test.big", []byte(`{}`)); len(results) != 0 {
		t.Fatal("oversized result reached dispatch")
	}
}

func TestStorageNamespacingAndQuotas(t *testing.T) {
	s, host := fixture(t)
	writePlugin(t, s, "example", exampleManifest("example"))
	enableExample(t, s, "example")
	set := `{"op":"kv_set","key":"shared","value":"from-example"}`
	if got := code(t, dispatchStorage(t, host, set)); got != 0 {
		t.Fatal("kv_set", got)
	}
	// A second plugin running the same module reads the same key and sees its
	// own empty namespace, not the first plugin's value.
	writePlugin(t, s, "second", exampleManifest("second"))
	enableExample(t, s, "second")
	reads := host.DispatchHook(context.Background(), "test.storage", []byte(`{"op":"kv_get","key":"shared"}`))
	if len(reads) != 2 {
		t.Fatal("expected both plugins to run", len(reads))
	}
	var first, second map[string]any
	if json.Unmarshal(reads[0], &first) != nil || json.Unmarshal(reads[1], &second) != nil {
		t.Fatal("unreadable results")
	}
	if first["value"] != "from-example" || second["code"] != float64(codeNotFound) {
		t.Fatal("namespacing", first, second)
	}
	s.unload("second")
	if got := code(t, dispatchStorage(t, host, `{"op":"kv_set","key":"big","value_size":65537}`)); got != codeInvalid {
		t.Fatal("oversized value accepted", got)
	}
	if out := dispatchStorage(t, host, `{"op":"kv_fill","fill":32,"value_size":60000}`); code(t, out) != codeQuota {
		t.Fatal("quota not enforced", out)
	}
	if got := code(t, dispatchStorage(t, host, `{"op":"kv_get","key":"absent"}`)); got != codeNotFound {
		t.Fatal("missing key", got)
	}
	if got := code(t, dispatchStorage(t, host, `{"op":"doc_put","collection":"notes","id":"n1","body":"{\"a\":1}"}`)); got != 0 {
		t.Fatal("doc_put", got)
	}
	if out := dispatchStorage(t, host, `{"op":"doc_get","collection":"notes","id":"n1"}`); out["value"] != `{"a":1}` {
		t.Fatal("doc_get", out)
	}
	if got := code(t, dispatchStorage(t, host, `{"op":"doc_put","collection":"notes","id":"n2","body":"not json"}`)); got != codeInvalid {
		t.Fatal("invalid document accepted", got)
	}
	if got := code(t, dispatchStorage(t, host, `{"op":"doc_delete","collection":"notes","id":"n1"}`)); got != 0 {
		t.Fatal("doc_delete", got)
	}
	if got := code(t, dispatchStorage(t, host, `{"op":"doc_get","collection":"notes","id":"n1"}`)); got != codeNotFound {
		t.Fatal("document survived delete", got)
	}
}

func TestUndeclaredHostFunctionUseIsRefused(t *testing.T) {
	s, _ := fixture(t)
	// The module imports the storage host functions; a manifest that does not
	// declare the storage capability must refuse to load it at all.
	manifest := strings.Replace(exampleManifest("example"), `["hooks", "routes", "storage"]`, `["hooks", "routes"]`, 1)
	writePlugin(t, s, "example", manifest)
	record, err := s.Approve(context.Background(), "example", 0, pass)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SetEnabled(context.Background(), "example", true, record.RowVersion, pass)
	if err == nil || !errors.Is(err, ErrInvoke) || !strings.Contains(err.Error(), "undeclared") {
		t.Fatal("undeclared host-function import loaded:", err)
	}
	record, err = s.install(context.Background(), "example")
	if err != nil || record.Enabled {
		t.Fatal("failed enable must not persist the flag", record.Enabled, err)
	}
}

func TestDisabledPluginNeverRuns(t *testing.T) {
	s, host := fixture(t)
	writePlugin(t, s, "example", exampleManifest("example"))
	record := enableExample(t, s, "example")
	ctx := context.Background()
	if results := host.DispatchHook(ctx, "test.echo", []byte(`{}`)); len(results) != 1 {
		t.Fatal("enabled plugin did not run")
	}
	record, err := s.SetEnabled(ctx, "example", false, record.RowVersion, pass)
	if err != nil {
		t.Fatal(err)
	}
	if results := host.DispatchHook(ctx, "test.echo", []byte(`{}`)); len(results) != 0 {
		t.Fatal("disabled plugin ran")
	}
	if len(host.Routes()) != 0 {
		t.Fatal("disabled plugin kept routes")
	}
	if _, err = s.SetEnabled(ctx, "example", true, record.RowVersion, pass); err != nil {
		t.Fatal(err)
	}
	if results := host.DispatchHook(ctx, "test.echo", []byte(`{}`)); len(results) != 1 {
		t.Fatal("re-enabled plugin did not run")
	}
}

func TestChecksumDriftRefusesLoadAndReportsChanged(t *testing.T) {
	s, _ := fixture(t)
	writePlugin(t, s, "example", exampleManifest("example"))
	record := enableExample(t, s, "example")
	ctx := context.Background()
	record, err := s.SetEnabled(ctx, "example", false, record.RowVersion, pass)
	if err != nil {
		t.Fatal(err)
	}
	modified := append(append([]byte{}, exampleModule...), 0)
	if err = os.WriteFile(filepath.Join(s.Dir, "example", "plugin.wasm"), modified, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetEnabled(ctx, "example", true, record.RowVersion, pass); !errors.Is(err, ErrChecksum) {
		t.Fatal("drifted files loaded:", err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].State != "changed" {
		t.Fatal("drift not reported", list)
	}
	// Startup takes the same path: an enabled record with drifted files is
	// skipped, never loaded.
	if _, err = s.DB.Exec("UPDATE plugin_installs SET enabled=1"); err != nil {
		t.Fatal(err)
	}
	if err = s.LoadEnabled(ctx); err != nil {
		t.Fatal(err)
	}
	if s.running("example") {
		t.Fatal("drifted plugin running after startup load")
	}
}

func TestRemoveDeletesNamespaceAndVersionGuards(t *testing.T) {
	s, host := fixture(t)
	writePlugin(t, s, "example", exampleManifest("example"))
	record := enableExample(t, s, "example")
	if got := code(t, dispatchStorage(t, host, `{"op":"kv_set","key":"k","value":"v"}`)); got != 0 {
		t.Fatal("kv_set", got)
	}
	ctx := context.Background()
	if err := s.Remove(ctx, "example", record.RowVersion-1, pass); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("stale remove accepted:", err)
	}
	if err := s.Remove(ctx, "example", record.RowVersion, pass); err != nil {
		t.Fatal(err)
	}
	if results := host.DispatchHook(ctx, "test.echo", []byte(`{}`)); len(results) != 0 {
		t.Fatal("removed plugin ran")
	}
	var rows int
	if err := s.DB.QueryRow("SELECT count(*) FROM plugin_kv").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("storage survived removal", rows, err)
	}
	if err := s.Remove(ctx, "example", 1, pass); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("second remove:", err)
	}
}

func TestApproveRecordsChecksumsAndRequiresReapprovalVersion(t *testing.T) {
	s, _ := fixture(t)
	writePlugin(t, s, "example", exampleManifest("example"))
	ctx := context.Background()
	record, err := s.Approve(ctx, "example", 0, pass)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Files) != 2 {
		t.Fatal("expected manifest and module checksums", record.Files)
	}
	for _, file := range record.Files {
		if len(file.SHA256) != 64 || file.Size == 0 {
			t.Fatal("checksum not recorded", file)
		}
	}
	if _, err = s.Approve(ctx, "example", 0, pass); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("re-approval without the expected version:", err)
	}
	if _, err = s.Approve(ctx, "example", record.RowVersion, pass); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("reauthentication required")
	if _, err = s.Approve(ctx, "example", 2, func(*sql.Tx) error { return refused }); !errors.Is(err, refused) {
		t.Fatal("approval without fresh reauthentication:", err)
	}
}
