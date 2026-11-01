package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBulkPublicationHTTPBoundary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, e := db.Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	f, e := storage.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	owner, _ := db.NewID()
	session, _ := db.NewID()
	d.Exec("INSERT INTO owner(id) VALUES(?)", owner)
	raw := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(raw))
	csrf := sha256.Sum256([]byte("csrf:" + raw))
	now := time.Now()
	_, e = d.Exec(`INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at) VALUES(?,?,?,'owner','passkey',1,?,?,?)`, session, owner, hex.EncodeToString(sum[:]), db.UTC(now), db.UTC(now.Add(time.Hour)), db.UTC(now))
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(d, cfg)
	contents := &content.Service{DB: d, Files: f, Access: &access.Live{DB: d}}
	domain := &Domain{Auth: a, Content: contents}
	h := New(Options{Config: cfg, Auth: a, Domain: domain})

	for _, tc := range []struct {
		cookie, csrf, origin bool
		want                 int
	}{{false, false, false, 401}, {true, false, true, 403}, {true, true, false, 403}, {true, true, true, 200}} {
		r := httptest.NewRequest("PUT", "https://example.test/api/v1/collections/simple/workspace?locale=en", strings.NewReader(`{"expected_version":0,"kind":"learning","slug":"simple","title":"Simple","visible":true,"members":[]}`))
		r.Header.Set("Content-Type", "application/json")
		if tc.cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		}
		if tc.csrf {
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
		}
		if tc.origin {
			r.Header.Set("Origin", cfg.BaseURL)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal("workspace owner boundary", w.Code, w.Body.String())
		}
	}

	{
		r := httptest.NewRequest("PUT", "https://example.test/api/v1/collections/bad/workspace?locale=en", strings.NewReader(`{"expected_version":0,"kind":"learning","slug":"bad slug","title":"Bad URL","visible":true,"members":[]}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", cfg.BaseURL)
		r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
		r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "collection_slug") || strings.Contains(w.Body.String(), "quiz") {
			t.Fatal("imprecise collection validation", w.Code, w.Body.String())
		}
	}

	version := int64(0)
	saved, err := contents.Save(ctx, owner, "bulk-http", "en", "", content.DraftInput{ExpectedVersion: &version, Kind: "article", SourceLocale: "en", Slug: "bulk-http", Title: "Bulk HTTP", Markdown: "Private draft"})
	if err != nil {
		t.Fatal(err)
	}
	version = saved.Version
	payload, _ := json.Marshal(content.BulkPublication{Operation: "publish", Items: []content.BulkPublicationItem{{Key: "bulk-http", Locale: "en", ExpectedVersion: &version, RevisionID: saved.RevisionID}}})
	for _, tc := range []struct {
		cookie, csrf, origin bool
		want                 int
	}{{false, false, false, 401}, {true, false, true, 403}, {true, true, false, 403}, {true, true, true, 200}} {
		r := httptest.NewRequest("POST", "https://example.test/api/v1/content/bulk-publication", strings.NewReader(string(payload)))
		r.Header.Set("Content-Type", "application/json")
		if tc.cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		}
		if tc.csrf {
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
		}
		if tc.origin {
			r.Header.Set("Origin", cfg.BaseURL)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	pub, err := contents.Public(ctx, "en", "bulk-http", access.Principal{})
	if err != nil || pub.Markdown != "Private draft" {
		t.Fatal(pub, err)
	}
	// Type conversion uses the same owner/Origin/CSRF boundary, including live items.
	zero := int64(0)
	collection, err := contents.SaveCollection(ctx, "conversion-path", "en", content.CollectionInput{ExpectedVersion: &zero, Kind: "learning", Slug: "conversion-path", Title: "Path", Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := contents.Draft(ctx, "bulk-http", "en")
	if err != nil {
		t.Fatal(err)
	}
	conversion, _ := json.Marshal(content.TypeChange{ExpectedVersion: &draft.Version, FromKind: "article", Kind: "lesson", CollectionKey: "conversion-path", ExpectedCollectionVersion: &collection.Version})
	for _, tc := range []struct {
		cookie, csrf, origin bool
		want                 int
	}{{false, false, false, 401}, {true, false, true, 403}, {true, true, false, 403}, {true, true, true, 200}} {
		r := httptest.NewRequest("POST", "https://example.test/api/v1/content/bulk-http/en/type", strings.NewReader(string(conversion)))
		r.Header.Set("Content-Type", "application/json")
		if tc.cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		}
		if tc.csrf {
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
		}
		if tc.origin {
			r.Header.Set("Origin", cfg.BaseURL)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal("conversion boundary", w.Code, w.Body.String())
		}
	}

	for _, authenticated := range []bool{false, true} {
		r := httptest.NewRequest("GET", "https://example.test/api/v1/owner/references?locale=en&kind=learning&q=conversion", nil)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if !authenticated && w.Code != 401 {
			t.Fatal("private references exposed")
		}
		if authenticated {
			if w.Header().Get("X-Reference-Search-Version") != "2" {
				t.Fatal("missing reference capability marker")
			}
			var result content.ReferencePage
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].Key != "conversion-path" {
				t.Fatal("reference search failed", w.Code, w.Body.String())
			}
		}
	}
	current, err := contents.Draft(ctx, "bulk-http", "en")
	if err != nil {
		t.Fatal(err)
	}
	update, _ := json.Marshal(content.DraftInput{ExpectedVersion: &current.Version, Kind: "article", SourceLocale: "en", Slug: current.Revision.Slug, Title: "Edited and retyped", Markdown: "Updated through normal Save"})
	for _, tc := range []struct {
		cookie, csrf, origin bool
		want                 int
	}{{false, false, false, 401}, {true, false, true, 403}, {true, true, false, 403}, {true, true, true, 200}} {
		r := httptest.NewRequest("PUT", "https://example.test/api/v1/content/bulk-http/en", strings.NewReader(string(update)))
		r.Header.Set("Content-Type", "application/json")
		if tc.cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		}
		if tc.csrf {
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
		}
		if tc.origin {
			r.Header.Set("Origin", cfg.BaseURL)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal("normal Save type boundary", w.Code, w.Body.String())
		}
	}
	edited, err := contents.Draft(ctx, "bulk-http", "en")
	if err != nil || edited.Kind != "article" || edited.Revision.Title != "Edited and retyped" || edited.PublishedID != current.PublishedID {
		t.Fatal("normal Save lost publication or changes", err)
	}

}
