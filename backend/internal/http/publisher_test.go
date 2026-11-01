package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPublisherIsolationRetryAndManualConflicts(t *testing.T) {
	t.Run("REST", func(t *testing.T) { testPublisherIsolation(t, "") })
	t.Run("MCP-legacy", func(t *testing.T) { testPublisherIsolation(t, "2025-11-25") })
	t.Run("MCP-OAuth", func(t *testing.T) { testPublisherIsolation(t, "oauth") })
	t.Run("MCP-modern", func(t *testing.T) { testPublisherIsolation(t, mcpVersion) })
}

func testPublisherIsolation(t *testing.T, mcp string) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	database, e := db.Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	files, e := storage.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer files.Close()
	owner, _ := db.NewID()
	if _, e = database.Exec("INSERT INTO owner(id) VALUES(?)", owner); e != nil {
		t.Fatal(e)
	}
	policy := &access.Live{DB: database}
	contents := &content.Service{DB: database, Files: files, Access: policy}
	contents.Membership = contents.AttachMember
	contents.PublicationHook = contents.CheckPublicationPlan
	policy.Gates = contents.LessonAccess
	zero := int64(0)
	c, e := contents.SaveCollection(ctx, "allowed", "en", content.CollectionInput{ExpectedVersion: &zero, Kind: "learning", Slug: "allowed", Title: "Allowed", Visible: true})
	if e != nil {
		t.Fatal(e)
	}
	raw := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	h := sha256.Sum256([]byte(raw))
	token, _ := db.NewID()
	if _, e = database.Exec("INSERT INTO api_tokens(id,owner_id,token_hash,label,expires_at) VALUES(?,?,?,?,?)", token, owner, hex.EncodeToString(h[:]), "Test", db.UTC(time.Now().Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	for _, scope := range []string{"draft", "publish"} {
		database.Exec("INSERT INTO api_token_scopes VALUES(?,?)", token, scope)
	}
	database.Exec("INSERT INTO api_token_collections VALUES(?,?)", token, c.ID)

	if mcp == "oauth" {
		mcp = "2025-11-25"
		raw = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
		h = sha256.Sum256([]byte(raw))
		expiry := db.UTC(time.Now().Add(time.Hour))
		if _, e = database.Exec("INSERT INTO oauth_clients VALUES('test-client','https://chatgpt.com/connector_platform_oauth_redirect',?)", expiry); e != nil {
			t.Fatal(e)
		}
		if _, e = database.Exec("INSERT INTO oauth_grants(id,code_hash,client_id,token_id,auth_version,access_hash,access_expires,refresh_hash,expires_at) SELECT 'test-grant','test-code','test-client',?,auth_version,?,?,'test-refresh',? FROM owner WHERE id=?", token, hex.EncodeToString(h[:]), expiry, expiry, owner); e != nil {
			t.Fatal(e)
		}
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(database, cfg)
	domain := &Domain{Auth: a, Content: contents, Media: media.New(database, files, policy), Learning: learning.NewIdentity(database, 180, 100)}
	server := New(Options{Config: cfg, Auth: a, Domain: domain})
	if mcp != "" {
		probe := func(body string, headers map[string]string) *httptest.ResponseRecorder {
			r := httptest.NewRequest("POST", cfg.BaseURL+mcpPath, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			r.Header.Set("Authorization", "Bearer "+raw)
			for key, value := range headers {
				r.Header.Set(key, value)
			}
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			return w
		}
		legacy := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
		if w := probe(legacy, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "protocolVersion") {
			t.Fatal(w.Code, w.Body.String())
		}
		modern := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
		if w := probe(modern, map[string]string{"MCP-Protocol-Version": mcpVersion, "Mcp-Method": "server/discover"}); w.Code != 200 || !strings.Contains(w.Body.String(), "supportedVersions") {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, row := range []struct {
			body    string
			headers map[string]string
			status  int
		}{
			{legacy, map[string]string{"Origin": "https://attacker.test"}, 403},
			{legacy, map[string]string{"Authorization": "Bearer invalid"}, 401},
			{modern, map[string]string{"MCP-Protocol-Version": mcpVersion, "Mcp-Method": "tools/call"}, 400},
			{`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, map[string]string{"MCP-Protocol-Version": mcpVersion, "Mcp-Method": "tools/list"}, 400},
			{`[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, nil, 400},
			{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_draft","arguments":{"key":"../private","locale":"en"}}}`, nil, 400},
			{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_draft","arguments":{"key":"first","locale":"en","draft":{}}}}`, nil, 400},
			{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"upload_media","arguments":{"filename":"test.png","mime_type":"image/png","data_base64":"%%%","idempotency_key":"upload"}}}`, nil, 400},
		} {
			if w := probe(row.body, row.headers); w.Code != row.status {
				t.Fatal("MCP boundary", row.status, w.Code, w.Body.String())
			}
		}
		args := map[string]any{"filename": "captions.vtt", "mime_type": "text/vtt", "data_base64": base64.StdEncoding.EncodeToString([]byte("WEBVTT\n\n00:00.000 --> 00:01.000\nHello\n")), "idempotency_key": "mcp-upload"}
		request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "upload_media", "arguments": args}})
		if w := probe(string(request), nil); w.Code != 200 || !strings.Contains(w.Body.String(), `\"status\":403`) {
			t.Fatal("media scope bypass", w.Code, w.Body.String())
		}
		if _, e := database.Exec("INSERT INTO api_token_scopes VALUES(?,'media')", token); e != nil {
			t.Fatal(e)
		}
		first := probe(string(request), nil)
		if first.Code != 200 || !strings.Contains(first.Body.String(), `\"status\":201`) {
			t.Fatal("MCP upload failed", first.Code, first.Body.String())
		}
		if replay := probe(string(request), nil); replay.Body.String() != first.Body.String() {
			t.Fatal("MCP upload retry duplicated")
		}
		var storedID string
		if e := database.QueryRow("SELECT media_id FROM publisher_media WHERE token_id=?", token).Scan(&storedID); e != nil {
			t.Fatal(e)
		}
		public := httptest.NewRecorder()
		server.ServeHTTP(public, httptest.NewRequest("GET", cfg.BaseURL+"/media/"+storedID, nil))
		if public.Code < 400 {
			t.Fatal("draft MCP upload became public")
		}
	}
	call := func(method, path, key string, in any) *httptest.ResponseRecorder {
		t.Helper()
		if mcp != "" && strings.HasPrefix(path, "/api/v1/publisher/content/") && !strings.HasSuffix(path, "/quiz-review") {
			parts := strings.Split(strings.TrimPrefix(path, "/api/v1/publisher/content/"), "/")
			name := "get_draft"
			args := map[string]any{"key": parts[0], "locale": parts[1]}
			if method == "PUT" {
				name = "save_draft"
				args["draft"] = in
			}
			if method == "POST" {
				name = "publish_revision"
				args["publication"] = in
			}
			if method != "GET" {
				args["idempotency_key"] = key
			}
			params := map[string]any{"name": name, "arguments": args}
			if mcp == mcpVersion {
				params["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": mcpVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
			}
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params})
			r := httptest.NewRequest("POST", cfg.BaseURL+mcpPath, bytes.NewReader(b))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			r.Header.Set("Authorization", "Bearer "+raw)
			r.Header.Set("MCP-Protocol-Version", mcp)
			r.Header.Set("Mcp-Method", "tools/call")
			r.Header.Set("Mcp-Name", name)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if w.Code != 200 {
				return w
			}
			var rpc struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
			}
			if json.Unmarshal(w.Body.Bytes(), &rpc) != nil || len(rpc.Result.Content) != 1 {
				t.Fatal("invalid MCP result", w.Body.String())
			}
			var payload struct {
				Status int             `json:"status"`
				Data   json.RawMessage `json:"data"`
			}
			if json.Unmarshal([]byte(rpc.Result.Content[0].Text), &payload) != nil {
				t.Fatal("invalid publisher envelope")
			}
			out := httptest.NewRecorder()
			out.WriteHeader(payload.Status)
			out.Write(append(payload.Data, '\n'))
			return out
		}
		b, _ := json.Marshal(in)
		r := httptest.NewRequest(method, cfg.BaseURL+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+raw)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	in := content.DraftInput{ExpectedVersion: &zero, Kind: "lesson", SourceLocale: "en", Slug: "first", Title: "First", Markdown: "Draft", CollectionKey: "allowed", Position: 1}
	path := "/api/v1/publisher/content/first/en"
	w := call("PUT", path, "draft-one", in)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var revision content.Result
	json.Unmarshal(w.Body.Bytes(), &revision)
	replay := call("PUT", path, "draft-one", in)
	if replay.Code != 200 || replay.Body.String() != w.Body.String() {
		t.Fatal("retry changed result", replay.Body.String())
	}
	in.Title = "Changed"
	if w = call("PUT", path, "draft-one", in); w.Code != 409 {
		t.Fatal("changed replay", w.Code)
	}
	in.ExpectedVersion = &revision.Version
	manual, e := contents.Save(ctx, owner, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if w = call("PUT", path, "draft-two", in); w.Code != 409 {
		t.Fatal("manual edit overwritten", w.Code)
	}
	if w = call("POST", path+"/publish", "publish-one", content.Action{ExpectedVersion: &revision.Version, RevisionID: revision.RevisionID}); w.Code != 409 {
		t.Fatal("stale published", w.Code)
	}
	if w = call("POST", path+"/publish", "publish-two", content.Action{ExpectedVersion: &manual.Version, RevisionID: manual.RevisionID}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Publishing an owner-prepared quiz does not grant draft answer-key access.
	current, e := contents.Draft(ctx, "first", "en")
	if e != nil {
		t.Fatal(e)
	}
	in.ExpectedVersion = &current.Version
	in.Quiz = &learning.Definition{PassMark: 1, Questions: []learning.PrivateQuestion{{Question: learning.Question{ID: "q1", Type: "single", Text: "Question", Options: []learning.Option{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}}, Correct: []string{"a"}}}}
	quizDraft, e := contents.Save(ctx, owner, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if e = contents.ReviewQuiz(ctx, owner, "first", "en", content.Action{ExpectedVersion: &quizDraft.Version, RevisionID: quizDraft.RevisionID}); e != nil {
		t.Fatal(e)
	}
	if w = call("GET", path, "", nil); w.Code != 403 {
		t.Fatal("answer-key access granted without quiz scope", w.Code)
	}
	if _, e := database.Exec("INSERT INTO api_token_scopes VALUES(?,'quiz')", token); e != nil {
		t.Fatal(e)
	}
	if w = call("GET", path, "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"correct"`) {
		t.Fatal("scoped quiz readback failed", w.Code, w.Body.String())
	}
	database.Exec("DELETE FROM api_token_scopes WHERE token_id=? AND scope='quiz'", token)
	if w = call("PUT", path, "edit-quiz-denied", in); w.Code != 403 {
		t.Fatal("quiz editing granted without quiz scope", w.Code)
	}
	if w = call("POST", path+"/publish", "publish-reviewed-quiz", content.Action{ExpectedVersion: &quizDraft.Version, RevisionID: quizDraft.RevisionID}); w.Code != 200 {
		t.Fatal("independent publication capability", w.Code, w.Body.String())
	}
	in.ExpectedVersion = &zero
	in.Kind = "article"
	in.CollectionKey = ""
	in.Position = 0
	in.Slug = "outside"
	if w = call("PUT", "/api/v1/publisher/content/outside/en", "outside", in); w.Code != 403 {
		t.Fatal("outside scope", w.Code, w.Body.String())
	}
	for _, p := range []string{"/api/v1/security/publisher-tokens", "/api/v1/collections/allowed/progress-reset", "/api/v1/content/first/en/quiz-review", "/api/v1/config/site/apply", "/api/v1/publisher/content/first/en/quiz-review"} {
		w = call("POST", p, "escalate", map[string]any{})
		if w.Code < 400 {
			t.Fatal("privilege escalation", p, w.Code)
		}
	}
	if w = call("POST", path+"/publish", "plan", map[string]any{"expected_version": manual.Version, "revision_id": manual.RevisionID, "plan": map[string]any{"expected_version": 1, "plan_id": strings.Repeat("a", 32)}}); w.Code != 403 {
		t.Fatal("plan escalation", w.Code)
	}
	database.Exec("UPDATE api_tokens SET revoked_at=? WHERE id=?", db.UTC(time.Now()), token)
	if w = call("PUT", path, "draft-one", in); w.Code != 401 {
		t.Fatal("revoked replay", w.Code)
	}
}
