package publisher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/extension"
)

func TestPublisherExtensionsRequireOptIn(t *testing.T) {
	root := t.TempDir()
	schedulePath := filepath.Join(root, "schedule.json")
	environment := map[string]string{}
	getenv := func(key string) string { return environment[key] }
	const origin = "https://example.test"
	if extensions, err := Extensions(origin, getenv, nil); err != nil || len(extensions) != 0 {
		t.Fatal("extensions enabled by default", extensions, err)
	}
	environment["BLOG_LESSON_SCHEDULE_FILE"] = schedulePath
	if err := saveRunnerJSON(schedulePath, Schedule{}); err != nil {
		t.Fatal(err)
	}
	if extensions, err := Extensions(origin, getenv, nil); err != nil || len(extensions) != 0 {
		t.Fatal("disabled schedule required credentials", extensions, err)
	}
	if err := saveRunnerJSON(schedulePath, scheduleFixture()); err != nil {
		t.Fatal(err)
	}
	environment["BLOG_LESSON_STATE_DIR"] = filepath.Join(root, "state")
	environment["BLOG_PUBLISHER_TOKEN"] = "publisher-token"
	environment["NOTION_API_KEY"] = "example-token"
	if _, err := Extensions(origin, getenv, nil); err == nil {
		t.Fatal("enabled scheduler accepted missing generator credentials")
	}
	environment["OPENAI_API_KEY"] = "example-generation-key"
	builtins, err := Extensions(origin, getenv, nil)
	if err != nil || len(builtins) != 1 || builtins[0].Slug() != "lesson-scheduler" {
		t.Fatal(builtins, err)
	}
	environment["BLOG_NOTION_DELIVERY_ENABLED"] = "true"
	builtins, err = Extensions(origin, getenv, nil)
	if err != nil || len(builtins) != 2 || builtins[0].Slug() != "notion-delivery" {
		t.Fatal(builtins, err)
	}
}

func TestNotionExtensionRetriesOnlyTheUnfinishedDestination(t *testing.T) {
	writes := 0
	transport := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writes++
		_ = json.NewEncoder(w).Encode(content.Result{RevisionID: strings.Repeat("a", 32), Version: 1})
	}))
	client, err := New(transport.URL, "local-test-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.HTTP.Transport = transport
	notion := &fakeNotion{fail: true}
	host := extension.NewHost(nil)
	if err := host.Install(&notionExtension{client: client, notion: notion, dir: filepath.Join(t.TempDir(), "state")}); err != nil {
		t.Fatal(err)
	}
	route := host.Routes()[0]
	if route.Public || route.Method != http.MethodPost || route.Path != "/deliver" {
		t.Fatal("delivery route is not a private write", route)
	}
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/notion-delivery/deliver", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		route.Handler.ServeHTTP(w, req)
		return w
	}
	if w := post(`{"unexpected":true}`); w.Code != http.StatusBadRequest {
		t.Fatal("invalid request accepted", w.Code)
	}
	const input = `{"key":"lesson","locale":"en","source_revision":"source","notion_parent":"cccccccccccccccccccccccccccccccc","draft":{"expected_version":0}}`
	if w := post(input); w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "destination unavailable") {
		t.Fatal("partial delivery did not report the failed destination", w.Code, w.Body.String())
	}
	notion.fail = false
	for range 2 {
		if w := post(input); w.Code != http.StatusOK || strings.Count(w.Body.String(), `"state":"done"`) != 2 {
			t.Fatal("exact retry did not finish both destinations", w.Code, w.Body.String())
		}
	}
	if writes != 1 || notion.calls != 2 {
		t.Fatal("successful destination repeated", writes, notion.calls)
	}
}
