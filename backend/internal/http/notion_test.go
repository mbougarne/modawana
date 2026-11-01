package httpserver

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

func TestNotionImportHTTPAuthorizationAndDrafts(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	database, e := db.Open(context.Background(), root)
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
	session, _ := db.NewID()
	token := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(token))
	csrf := sha256.Sum256([]byte("csrf:" + token))
	now := time.Now()
	if _, e = database.Exec("INSERT INTO owner(id) VALUES(?)", owner); e != nil {
		t.Fatal(e)
	}
	if _, e = database.Exec(`INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at) VALUES(?,?,?,'owner','passkey',1,?,?,?)`, session, owner, hex.EncodeToString(hash[:]), db.UTC(now), db.UTC(now.Add(time.Hour)), db.UTC(now)); e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(database, cfg)
	handler := New(Options{Config: cfg, Auth: a, Domain: &Domain{Auth: a, Content: &content.Service{DB: database, Files: files}}})
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	w, _ := z.Create("article.md")
	w.Write([]byte("# An article\n\nA draft imported through the real HTTP handler."))
	z.Close()
	for _, tc := range []struct {
		path                 string
		cookie, csrf, origin bool
		want                 int
	}{
		{"preview?locale=en&kind=article", false, false, false, 401},
		{"import?locale=en&kind=article", false, false, false, 401},
		{"preview?locale=en&kind=article", true, false, true, 403},
		{"import?locale=en&kind=article", true, true, false, 403},
		{"preview?locale=en&kind=article&kind=learning", true, true, true, 422},
		{"preview?locale=de&kind=article", true, true, true, 422},
		{"preview?locale=en&kind=article", true, true, true, 200},
		{"import?locale=en&kind=article", true, true, true, 200},
	} {
		r := httptest.NewRequest("POST", "https://example.test/api/v1/portability/notion/"+tc.path, bytes.NewReader(archive.Bytes()))
		r.Header.Set("Content-Type", "application/zip")
		if tc.cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: token})
		}
		if tc.csrf {
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
		}
		if tc.origin {
			r.Header.Set("Origin", cfg.BaseURL)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		if out.Code != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.path, out.Code, tc.want)
		}
	}
	var drafts, published int
	database.QueryRow("SELECT count(*),count(published_revision_id) FROM content_versions").Scan(&drafts, &published)
	if drafts != 1 || published != 0 {
		t.Fatal("HTTP import did not remain draft-only")
	}
	if _, e = database.Exec("UPDATE sessions SET authenticated_at=?", db.UTC(now.Add(-31*time.Minute))); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "https://example.test/api/v1/portability/notion/import?locale=en&kind=article", bytes.NewReader(archive.Bytes()))
	r.Header.Set("Origin", cfg.BaseURL)
	r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
	r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: token})
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal("stale authentication imported content", out.Code)
	}
}
