package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/content"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type fakeNotion struct {
	calls int
	fail  bool
}

func (f *fakeNotion) Deliver(context.Context, Delivery, bool) (Destination, error) {
	f.calls++
	if f.fail {
		return Destination{State: "failed"}, errors.New("destination unavailable")
	}
	return Destination{State: "done", ID: strings.Repeat("b", 32), URL: "https://www.notion.so/" + strings.Repeat("b", 32)}, nil
}
func TestDeliveryIndependentOutcomesAndDuplicateRetry(t *testing.T) {
	writes := 0
	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writes++
		if r.Method != "PUT" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("bad request")
		}
		json.NewEncoder(w).Encode(content.Result{RevisionID: strings.Repeat("a", 32), Version: 1})
	}))
	defer server.Close()
	client, e := New(server.URL, "test-token", nil)
	if e != nil {
		t.Fatal(e)
	}
	client.HTTP.Transport = server
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	zero := int64(0)
	in := Delivery{Key: "lesson-1", Locale: "en", SourceRevision: "source-1", NotionParent: strings.Repeat("c", 32), Draft: content.DraftInput{ExpectedVersion: &zero, Kind: "lesson", CollectionKey: "course", SourceLocale: "en", Title: "Lesson", Markdown: "Body"}}
	notion := &fakeNotion{fail: true}
	state, e := Deliver(context.Background(), client, notion, dir, in)
	if e == nil || state.Blog.State != "done" || state.Notion.State != "failed" {
		t.Fatal(state, e)
	}
	notion.fail = false
	state, e = Deliver(context.Background(), client, notion, dir, in)
	if e != nil || state.Notion.State != "done" || writes != 1 {
		t.Fatal(state, e, writes)
	}
	if _, e = Deliver(context.Background(), client, notion, dir, in); e != nil || writes != 1 || notion.calls != 2 {
		t.Fatal("duplicate delivery", e, writes, notion.calls)
	}
	in.Draft.Markdown = "Manual source change"
	if _, e = Deliver(context.Background(), client, notion, dir, in); e == nil {
		t.Fatal("changed source silently overwrote separate destination")
	}
}
func TestNotionUncertainCreateNeverDuplicates(t *testing.T) {
	creates := 0
	found := false
	zero := int64(0)
	in := Delivery{Key: "lesson", Locale: "en", SourceRevision: "source", NotionParent: strings.Repeat("c", 32), Draft: content.DraftInput{ExpectedVersion: &zero, Title: "Title", Markdown: "Body"}}
	marker := "Delivery key: lesson/en; source revision: source; payload: " + fingerprint(in.Draft)
	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			creates++
			w.WriteHeader(504)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/markdown") {
			json.NewEncoder(w).Encode(map[string]any{"markdown": marker + "\n\nBody", "truncated": false})
			return
		}
		rows := []any{}
		if found {
			rows = append(rows, map[string]any{"id": strings.Repeat("b", 32), "type": "child_page", "child_page": map[string]string{"title": "Title [lesson/en]"}})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": rows, "has_more": false})
	}))
	defer server.Close()
	n := NewNotion("test-token")
	n.Base = server.URL
	n.HTTP.Transport = server
	state, e := n.Deliver(context.Background(), in, false)
	if e == nil || state.State != "uncertain" || creates != 1 {
		t.Fatal(state, e)
	}
	if _, e = n.Deliver(context.Background(), in, true); e == nil || creates != 1 {
		t.Fatal("uncertain create duplicated", e, creates)
	}
	n.Token = ""
	state, e = n.Deliver(context.Background(), in, true)
	if e == nil || state.State != "uncertain" {
		t.Fatal("lost uncertainty when reconciliation was unavailable", state, e)
	}
	n.Token = "test-token"
	found = true
	state, e = n.Deliver(context.Background(), in, true)
	if e != nil || state.State != "done" || creates != 1 {
		t.Fatal(state, e, creates)
	}
}
func TestClientRefusesCredentialRedirect(t *testing.T) {
	leaked := false
	target := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer target.Close()
	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	c, e := New(server.URL, "private-token", nil)
	if e != nil {
		t.Fatal(e)
	}
	c.HTTP.Transport = server
	if _, e = c.Do(context.Background(), "PUT", "/api/v1/publisher/content/x/en", map[string]string{}, "retry"); e == nil || leaked || strings.Contains(e.Error(), "private-token") {
		t.Fatal("credential redirect", e, leaked)
	}
}

// In-memory HTTP transport keeps unit tests independent of listen permissions.
type testServer struct {
	URL     string
	handler http.Handler
}

func newTestServer(handler http.Handler) *testServer {
	return &testServer{URL: "http://127.0.0.1:34567", handler: handler}
}
func (s *testServer) Close() {}
func (s *testServer) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
