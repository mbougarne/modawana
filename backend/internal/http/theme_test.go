package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
)

const katexScript = "/assets/katex.0123456789abcdef0123.js"

// buildThemeFixture extends the shell fixture with a katex island entry.
func buildThemeFixture(t *testing.T, withIsland bool) string {
	t.Helper()
	root := buildFixture(t)
	if !withIsland {
		return root
	}
	if err := os.WriteFile(filepath.Join(root, katexScript), []byte("island"), 0600); err != nil {
		t.Fatal(err)
	}
	m := Manifest{Version: 1, Scripts: []string{scriptName}, Styles: []string{styleName}, Assets: []string{scriptName, styleName, katexScript}, Islands: map[string][]string{"katex": {katexScript}}}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

type themeFixture struct{}

func (themeFixture) Handle(http.ResponseWriter, *http.Request) bool { return false }
func (themeFixture) PublicTaxonomy(context.Context, string) ([]content.Term, error) {
	return []content.Term{
		{Kind: "category", Key: "code", Label: "Code notes", Slug: "code-notes"},
		{Kind: "tag", Key: "go", Label: "Go", Slug: "golang"},
	}, nil
}
func (themeFixture) ResolvePage(_ context.Context, path string) (content.PageRoute, error) {
	article := content.PublicContent{
		ReadingMinutes: 3,
		Key:            "plain",
		Kind:           "article",
		Locale:         "en",
		Slug:           "plain",
		Title:          "Server rendered",
		Summary:        "A public summary",
		Published:      "2026-10-29T10:00:00.000Z",
		Markdown:       "# Heading\n\nServer body paragraph with `code`.",
		CategoryKey:    "code",
		Tags:           []string{"go"},
	}
	page := content.PageRoute{Kind: "article", Locale: "en", URL: "/en/articles/plain", Title: article.Title, Summary: article.Summary, Alternatives: []content.Alternative{{Locale: "ar", URL: "/ar/articles/%D9%85%D9%82%D8%AF%D9%85%D8%A9"}, {Locale: "en", URL: "/en/articles/plain"}}}
	switch path {
	case "/en/articles/plain":
		page.Content = &article
		return page, nil
	case "/en/articles/math":
		math := article
		math.Slug, math.Key = "math", "math"
		math.Markdown = "Euler: $e^{i\\pi}+1=0$"
		page.URL = "/en/articles/math"
		page.Content = &math
		return page, nil
	case "/en", "/en/about":
		return content.PageRoute{Kind: "home", Locale: "en", URL: "/en", Alternatives: []content.Alternative{}}, nil
	default:
		return content.PageRoute{}, sql.ErrNoRows
	}
}

func themeServer(t *testing.T, withIsland bool, force bool) http.Handler {
	t.Helper()
	assets, err := LoadAssets(buildThemeFixture(t, withIsland))
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{Assets: assets, Domain: themeFixture{}, Config: config.Config{BaseURL: "https://canonical.example", ForceSPA: force}})
}

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestThemedArticleReplacesShell(t *testing.T) {
	h := themeServer(t, true, false)
	w := get(t, h, "GET", "/en/articles/plain")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, part := range []string{
		`data-rendered="server"`,
		`<article class="vb-prose" lang="en">`,
		`Server body paragraph with <code dir="ltr">code</code>.`,
		`Code notes · Article`,
		`href="/en/writing?category=code-notes"`,
		`<bdi>Go</bdi>`,
		`<time datetime="2026-10-29T10:00:00.000Z">29 Oct 2026</time>`,
		`About 3 min read`,
		`rel="canonical" href="https://canonical.example/en/articles/plain"`,
		`hreflang="ar"`,
		`<meta property="og:title" content="Server rendered">`,
		`aria-current="page">Writing</a>`,
	} {
		if !strings.Contains(body, part) {
			t.Fatal("missing", part, body)
		}
	}
	for _, absent := range []string{`id="root"`, scriptName, "JavaScript is required"} {
		if strings.Contains(body, absent) {
			t.Fatal("unexpected", absent)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("caching changed")
	}
	if w.Header().Get("X-Robots-Tag") != "" {
		t.Fatal("public article must stay indexable")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+w.Header().Get("X-Request-ID")+"'") {
		t.Fatal("missing CSP nonce")
	}
	if head := get(t, h, "HEAD", "/en/articles/plain"); head.Code != 200 || head.Body.Len() != 0 {
		t.Fatal("HEAD body", head.Code)
	}
}

func TestThemedMathIncludesIslandOnly(t *testing.T) {
	h := themeServer(t, true, false)
	math := get(t, h, "GET", "/en/articles/math").Body.String()
	if !strings.Contains(math, `<script defer src="`+katexScript+`"></script>`) {
		t.Fatal("math page without katex island", math)
	}
	plain := get(t, h, "GET", "/en/articles/plain").Body.String()
	if strings.Contains(plain, "<script defer") {
		t.Fatal("math-free page ships rendering JavaScript", plain)
	}
}

func TestThemedFallbacks(t *testing.T) {
	// A frontend build without the katex island keeps math articles on the SPA.
	h := themeServer(t, false, false)
	if body := get(t, h, "GET", "/en/articles/math").Body.String(); !strings.Contains(body, `id="root"`) {
		t.Fatal("expected SPA shell for math without island")
	}
	if body := get(t, h, "GET", "/en/articles/plain").Body.String(); !strings.Contains(body, `data-rendered="server"`) {
		t.Fatal("math-free article must still render server-side")
	}
	// The operator override forces the SPA shell for every page.
	h = themeServer(t, true, true)
	if body := get(t, h, "GET", "/en/articles/plain").Body.String(); !strings.Contains(body, `id="root"`) {
		t.Fatal("BLOG_FORCE_SPA ignored")
	}
	// Kinds outside the allowlist keep today's shell exactly.
	h = themeServer(t, true, false)
	for _, path := range []string{"/en", "/en/about"} {
		if body := get(t, h, "GET", path).Body.String(); !strings.Contains(body, `id="root"`) || strings.Contains(body, `data-rendered`) {
			t.Fatal("non-allowlisted page changed", path)
		}
	}
	if w := get(t, h, "GET", "/en/articles/missing"); w.Code != 404 || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatal("404 changed", w.Code)
	}
}

var headTag = regexp.MustCompile(`<title>.*?</title>|<meta [^>]*>|<link [^>]*>`)

// The themed head must carry exactly the metadata the shell path carries;
// only the boot blob and script tags may differ.
func TestThemedMetadataParityWithShell(t *testing.T) {
	themedResponse := get(t, themeServer(t, true, false), "GET", "/en/articles/plain")
	shellResponse := get(t, themeServer(t, true, true), "GET", "/en/articles/plain")
	themed := strings.ReplaceAll(themedResponse.Body.String(), themedResponse.Header().Get("X-Request-ID"), "NONCE")
	shell := strings.ReplaceAll(shellResponse.Body.String(), shellResponse.Header().Get("X-Request-ID"), "NONCE")
	extract := func(body string) []string {
		tags := headTag.FindAllString(body[:strings.Index(body, "</head>")], -1)
		sort.Strings(tags)
		return tags
	}
	a, b := extract(themed), extract(shell)
	if len(a) != len(b) {
		t.Fatal("head tag count differs", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("metadata drift", a[i], b[i])
		}
	}
}
