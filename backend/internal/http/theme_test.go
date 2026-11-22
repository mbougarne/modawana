package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/storage"
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
	case "/en/articles/locked":
		// Articles cannot actually lock (only lessons receive PublicTeaser);
		// this pins the defensive guard in renderThemed.
		locked := article
		locked.Locked, locked.Markdown = true, ""
		page.URL = "/en/articles/locked"
		page.Content = &locked
		return page, nil
	case "/en/learn/path/locked":
		return content.PageRoute{Kind: "lesson", Locale: "en", Content: &content.PublicContent{Locked: true, Summary: "Safe teaser"}}, content.ErrLocked
	case "/en", "/en/about", "/en/writing", "/en/search", "/en/learn", "/en/docs":
		kind := strings.TrimPrefix(path, "/en/")
		if path == "/en" {
			kind = "home"
		}
		return content.PageRoute{Kind: kind, Locale: "en", URL: path, Alternatives: []content.Alternative{}}, nil
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
	for _, path := range []string{"/en/articles/plain", "/en", "/en/about", "/en/writing", "/en/search"} {
		if body := get(t, h, "GET", path).Body.String(); !strings.Contains(body, `id="root"`) {
			t.Fatal("BLOG_FORCE_SPA ignored on", path)
		}
	}
	// Kinds outside the allowlist keep today's shell exactly.
	h = themeServer(t, true, false)
	for _, path := range []string{"/en/learn", "/en/docs"} {
		if body := get(t, h, "GET", path).Body.String(); !strings.Contains(body, `id="root"`) || strings.Contains(body, `data-rendered`) {
			t.Fatal("non-allowlisted page changed", path)
		}
	}
	if w := get(t, h, "GET", "/en/articles/missing"); w.Code != 404 || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatal("404 changed", w.Code)
	}
	// A locked content projection keeps the SPA shell even if a future kind
	// in the allowlist could lock.
	if body := get(t, h, "GET", "/en/articles/locked").Body.String(); !strings.Contains(body, `id="root"`) || strings.Contains(body, `data-rendered`) {
		t.Fatal("locked content must keep the shell")
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

type discoveryFixture struct {
	themeFixture
}

func (discoveryFixture) ReaderContext(r *http.Request) context.Context {
	return access.WithPrincipal(r.Context(), access.Principal{Audience: access.Learner, ID: "reader"})
}

func (discoveryFixture) PublicDiscovery(ctx context.Context, filter content.Filter) (content.Page, error) {
	if access.FromContext(ctx).ID != "reader" || filter.Locale != "en" || filter.Limit != 20 {
		return content.Page{}, errThemeUnavailable
	}
	if filter.Kind != "article" || filter.Category != "code-notes" || filter.Tag != "golang" || filter.Query != "<script>" {
		return content.Page{}, content.ErrInvalid
	}
	return content.Page{Total: 2, Next: "page-two", Items: []content.ListEntry{
		{Kind: "article", Title: "Public entry", URL: "/en/articles/plain", Published: "2026-11-22", CategoryKey: "code", Tags: []string{"go"}},
		{Kind: "lesson", Title: "Locked entry", Summary: "Safe teaser", URL: "/en/learn/path/locked", Published: "2026-11-21", Locked: true},
	}}, nil
}

func TestThemedDiscoveryUsesPublicProjection(t *testing.T) {
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, Domain: discoveryFixture{}})
	query := "?kind=article&category=code-notes&tag=golang&q=%3Cscript%3E&cursor=page-one&previous="
	for _, path := range []string{"/en/writing", "/en/search"} {
		w := get(t, h, "GET", path+query)
		body := w.Body.String()
		for _, part := range []string{`data-rendered="server"`, `href="/en/articles/plain"`, "Safe teaser", "Results: 2", "cursor=page-two", "Previous", "&lt;script&gt;"} {
			if w.Code != 200 || !strings.Contains(body, part) {
				t.Fatalf("%s: missing %s in %s", path, part, body)
			}
		}
		for _, absent := range []string{`id="root"`, `href="/en/learn/path/locked"`, `<bdi><script>`} {
			if strings.Contains(body, absent) {
				t.Fatal("unexpected", absent)
			}
		}
	}
	body := get(t, h, "GET", "/en/search?kind=invalid").Body.String()
	if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `method="get"`) || strings.Contains(body, "Public entry") {
		t.Fatal("invalid filter did not render a usable form and error", body)
	}
}

func TestThemedPageAPIMetadataUsesSameSocialHookAndOmitsLocked(t *testing.T) {
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	host := extension.NewHost(nil)
	h := New(Options{Assets: assets, Domain: themeFixture{}, Extensions: host, Config: config.Config{BaseURL: "https://canonical.example"}}).(*Server)
	h.extensions()
	if err = host.Install(hostileMeta{result: `{"image":"https://canonical.example/media/custom","alt":"Custom card"}`}); err != nil {
		t.Fatal(err)
	}
	const path = "/en/articles/plain"
	html := get(t, h, "GET", path).Body.String()
	jsonPage := get(t, h, "GET", "/api/v1/public/page?path="+url.QueryEscape(path))
	var out struct{ Metadata publicMetadata }
	if err := json.Unmarshal(jsonPage.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if jsonPage.Code != 200 || out.Metadata.Title != "Server rendered" || out.Metadata.Social.Alt != "Custom card" ||
		!strings.Contains(html, `property="og:image" content="`+out.Metadata.Social.Image+`"`) {
		t.Fatal("HTML and page API differ", out, jsonPage.Code, html)
	}
	locked := get(t, h, "GET", "/api/v1/public/page?path=/en/learn/path/locked")
	if locked.Code != 403 || locked.Header().Get("X-Robots-Tag") != "noindex" ||
		strings.Contains(locked.Body.String(), `"metadata"`) || !strings.Contains(locked.Body.String(), "Safe teaser") {
		t.Fatal("unexpected locked projection", locked.Code, locked.Body.String())
	}
}

func TestThemedLabelsRespectMissingAndExplicitEmptyOverrides(t *testing.T) {
	settings := siteconfig.Public{Labels: siteconfig.Labels{"page.about": {"en": json.RawMessage(`"About this blog"`)}}}
	if label := pageLabel(settings, "page.about", "fr"); label != "À propos" {
		t.Fatal("an English override replaced the shipped French", label)
	}
	settings.Labels["page.about"]["fr"] = json.RawMessage(`""`)
	if label := pageLabel(settings, "page.about", "fr"); label != "About this blog" {
		t.Fatal("an explicitly empty French override did not fall back", label)
	}
}

func TestThemedProfileUsesAppliedSettingsAndPublishedRevisions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	files, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	owner, err := db.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO owner(id) VALUES(?)", owner); err != nil {
		t.Fatal(err)
	}
	c := &content.Service{DB: database, Files: files, Access: &access.Live{DB: database}}
	cfg := &siteconfig.Service{DB: database, Files: files}
	save := func(key, kind, title, markdown string, publish bool) {
		t.Helper()
		zero := int64(0)
		draft, err := c.Save(ctx, owner, key, "en", "", content.DraftInput{
			ExpectedVersion: &zero, Kind: kind, SourceLocale: "en", Slug: key, Title: title, Markdown: markdown,
		})
		if err != nil {
			t.Fatal(err)
		}
		if publish {
			if _, err := c.ChangePublication(ctx, owner, key, "en", "", "publish", content.Action{
				ExpectedVersion: &draft.Version, RevisionID: draft.RevisionID,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	save("bio-document", "doc", "About document", "The public About narrative.", true)
	save("writing", "article", "Published writing", "Public article paragraph.", true)
	save("draft", "article", "Private working title", "Private draft body.", false)
	doc, err := cfg.Get(ctx, "profile")
	if err != nil {
		t.Fatal(err)
	}
	var profile siteconfig.Profile
	if err := json.Unmarshal(doc.Snapshot, &profile); err != nil {
		t.Fatal(err)
	}
	profile.Headline = siteconfig.Localized{"en": "A *publishing* site"}
	profile.AboutKey = "bio-document"
	profile.Bio = siteconfig.Localized{"en": "A **public** biography."}
	snapshot, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Apply(ctx, owner, "profile", siteconfig.Input{ExpectedVersion: &doc.Version, Snapshot: snapshot}); err != nil {
		t.Fatal(err)
	}
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, SiteConfig: cfg, Domain: &Domain{Content: c, SiteConfig: cfg}})
	for path, want := range map[string]string{
		"/en": "Published writing", "/en/about": "The public About narrative.",
		"/en/writing": "Published writing", "/en/search?q=Published&kind=article": "Published writing",
		"/ar": `<h1 lang="en" dir="ltr">A <em>publishing</em> site</h1>`,
	} {
		w := get(t, h, "GET", path)
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, `data-rendered="server"`) || !strings.Contains(body, want) || strings.Contains(body, "Private working title") {
			t.Fatalf("%s: unexpected response %d %s", path, w.Code, body)
		}
	}
	if w := get(t, h, "GET", "/ar/about"); strings.Contains(w.Body.String(), "The public About narrative.") {
		t.Fatal("About rendered an unpublished language")
	}
}
