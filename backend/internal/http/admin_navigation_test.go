package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestExpiredOwnerSessionRedirectsHTMLButNeverAPIs(t *testing.T) {
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "private-data"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	owner, _ := db.NewID()
	session, _ := db.NewID()
	raw := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(raw))
	now := time.Now()
	if _, err = database.Exec("INSERT INTO owner(id) VALUES(?)", owner); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at) VALUES(?,?,?,'owner','passkey',1,?,?,?)`, session, owner, hex.EncodeToString(hash[:]), db.UTC(now), db.UTC(now.Add(time.Minute)), db.UTC(now)); err != nil {
		t.Fatal(err)
	}
	authentication := auth.New(database, config.Config{BaseURL: "https://example.test"})
	authentication.Now = func() time.Time { return now }
	assets, err := LoadAssets(buildFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Auth: authentication, Assets: assets})
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://example.test"+path, nil)
		r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("/en/admin/transfer"); w.Code != 200 || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatalf("valid owner page: %d %s", w.Code, w.Body.String())
	}
	now = now.Add(2 * time.Minute)
	for _, locale := range []string{"en", "ar", "fr"} {
		if w := request("/" + locale + "/admin/security"); w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "/"+locale+"/login?") {
			t.Fatalf("expired page %s: %d %s", locale, w.Code, w.Body.String())
		}
		if w := request("/" + locale + "/login"); w.Code != 200 {
			t.Fatalf("public login %s: %d", locale, w.Code)
		}
	}
	if w := request("/api/v1/security/factors"); w.Code != 401 || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("expired API: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminBrowserNavigationRequiresLoginWithoutServingPrivateShell(t *testing.T) {
	h, _ := testServer(t, nil)
	for _, locale := range []string{"en", "ar", "fr"} {
		for _, path := range []string{"", "/security", "/posts?q=one%26two", "/editor/private-key/ar", "/preview/private-key/fr", "/transfer", "/plugins"} {
			for _, method := range []string{"GET", "HEAD"} {
				target := "/" + locale + "/admin" + path
				r := httptest.NewRequest(method, target, nil)
				r.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
				r.Header.Set("Cookie", "__Host-owner=expired-session")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				location, err := url.Parse(w.Header().Get("Location"))
				if err != nil || w.Code != 302 || location.IsAbs() || location.Host != "" || location.Path != "/"+locale+"/login" || location.Query().Get("return") != target {
					t.Fatalf("%s %s: %d %q", method, target, w.Code, w.Header().Get("Location"))
				}
				if strings.Contains(w.Body.String(), `id="root"`) || strings.Contains(w.Body.String(), "unauthenticated") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("private shell, raw JSON or cacheable redirect")
				}
				if method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD response has body")
				}
			}
		}
	}
}

func TestAdminNonBrowserRequestsKeepAuthenticationJSON(t *testing.T) {
	h, _ := testServer(t, nil)
	for _, accept := range []string{"", "*/*", "application/json", "text/html;q=0,application/json", "text/html;q=invalid", "text/html;q=NaN"} {
		r := httptest.NewRequest("GET", "/ar/admin/settings", nil)
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 || !strings.Contains(w.Body.String(), `"code":"unauthenticated"`) || w.Header().Get("Location") != "" {
			t.Fatalf("Accept %q: %d %s", accept, w.Code, w.Body.String())
		}
	}
}

func TestAdminLoginAliasesAndUnsafePaths(t *testing.T) {
	h, _ := testServer(t, nil)
	for path, expected := range map[string]string{
		"/admin/login": "/en/login", "/en/admin/login": "/en/login", "/ar/admin/login": "/ar/login", "/fr/admin/login": "/fr/login",
	} {
		r := httptest.NewRequest("GET", path+"?return=https://evil.example", nil)
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 302 || w.Header().Get("Location") != expected {
			t.Fatalf("%s: %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
	for _, path := range []string{"/en/admin/../login", "/en/admin//evil.example", "/en/admin/%2f%2fevil.example", "/en/admin/%5cevil.example", "/api/v1/missing"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 404 || w.Header().Get("Location") != "" {
			t.Fatalf("unsafe/API path %s: %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
}
