package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
)

type pageFixture struct{}

func (pageFixture) Handle(http.ResponseWriter, *http.Request) bool { return false }
func (pageFixture) ResolvePage(_ context.Context, path string) (content.PageRoute, error) {
	p := content.PageRoute{Kind: "article", Locale: "ar", URL: "/ar/articles/%D9%85%D9%82%D8%AF%D9%85%D8%A9", Title: `A <script>alert(1)</script> & "quote"`, Summary: `"><script>bad</script>`, Alternatives: []content.Alternative{{Locale: "ar", URL: "/ar/articles/%D9%85%D9%82%D8%AF%D9%85%D8%A9"}}}
	switch path {
	case "/ar/articles/مقدمة":
		return p, nil
	case "/ar/learn/path/locked":
		p.Kind = "lesson"
		return p, content.ErrLocked
	case "/ar/articles/corrupt":
		return p, errors.New("PRIVATE FILE PATH")
	default:
		return content.PageRoute{}, sql.ErrNoRows
	}
}
func TestPublicHTMLMetadataStatusAndEncodedRoutes(t *testing.T) {
	assets, e := LoadAssets(buildFixture(t))
	if e != nil {
		t.Fatal(e)
	}
	h := New(Options{Assets: assets, Domain: pageFixture{}, Config: config.Config{BaseURL: "https://canonical.example"}})
	for _, tc := range []struct {
		path   string
		status int
	}{{"/ar/articles/%D9%85%D9%82%D8%AF%D9%85%D8%A9", 200}, {"/ar/learn/path/locked", 403}, {"/ar/articles/missing", 404}, {"/ar/articles/corrupt", 503}, {"/ar/articles%2fhidden", 404}, {"/ar/%2e%2e/secret", 404}} {
		for _, method := range []string{"GET", "HEAD"} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, tc.path, nil)
			r.Header.Set("Host", "evil.example")
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%s %s: %d %s", method, tc.path, w.Code, w.Body.String())
			}
			body := w.Body.String()
			if strings.Contains(body, "<script>alert") || strings.Contains(body, "<script>bad") || strings.Contains(body, "PRIVATE FILE PATH") || strings.Contains(body, "evil.example") {
				t.Fatal("unescaped or private metadata", body)
			}
			if tc.status == 200 && method == "GET" {
				for _, part := range []string{`lang="ar" dir="rtl"`, `<title>A &lt;script&gt;`, `rel="canonical" href="https://canonical.example/ar/articles/`, `hreflang="ar"`, `property="og:description"`} {
					if !strings.Contains(body, part) {
						t.Fatal("missing "+part, body)
					}
				}
				if strings.Contains(body, `hreflang="fr"`) {
					t.Fatal("unpublished alternate")
				}
			}
			if tc.status == 403 || tc.status == 404 && strings.Contains(body, "<html") {
				if w.Header().Get("X-Robots-Tag") != "noindex" {
					t.Fatal("missing noindex")
				}
			}
			if method == "HEAD" && (tc.status == 200 || tc.status == 403 || strings.Contains(body, "<html")) && body != "" {
				t.Fatal("HEAD body")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("public page caching")
			}
		}
	}
}
