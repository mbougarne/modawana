package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
const readerScript = "/assets/reader-learning.0123456789abcdef0123.js"

// buildThemeFixture extends the shell fixture with the reader islands.
func buildThemeFixture(t *testing.T, withIsland bool) string {
	t.Helper()
	root := buildFixture(t)
	if !withIsland {
		return root
	}
	for _, script := range []string{katexScript, readerScript} {
		if err := os.WriteFile(filepath.Join(root, script), []byte("island"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := Manifest{
		Version: 1, Scripts: []string{scriptName}, Styles: []string{styleName},
		Assets:  []string{scriptName, styleName, katexScript, readerScript},
		Islands: map[string][]string{"katex": {katexScript}, "reader-learning": {readerScript}},
	}
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

func themeServer(t *testing.T, withIsland bool) http.Handler {
	t.Helper()
	assets, err := LoadAssets(buildThemeFixture(t, withIsland))
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{Assets: assets, Domain: themeFixture{}, Config: config.Config{BaseURL: "https://canonical.example"}})
}

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestThemedArticleReplacesShell(t *testing.T) {
	h := themeServer(t, true)
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
	h := themeServer(t, true)
	response := get(t, h, "GET", "/en/articles/math")
	math := response.Body.String()
	if !strings.Contains(math, `<script defer src="`+katexScript+`" nonce="`+response.Header().Get("X-Request-ID")+`"></script>`) {
		t.Fatal("math page without katex island", math)
	}
	plain := get(t, h, "GET", "/en/articles/plain").Body.String()
	if strings.Contains(plain, "<script defer") {
		t.Fatal("math-free page ships rendering JavaScript", plain)
	}
}

func TestPublicMenuRevisions(t *testing.T) {
	ctx := t.Context()
	content, owner := newThemeContent(t)
	cfg := &siteconfig.Service{DB: content.DB, Files: content.Files}
	assets, err := LoadAssets(buildThemeFixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, SiteConfig: cfg, Domain: &Domain{Content: content, SiteConfig: cfg}})
	page := func(locale string) string {
		t.Helper()
		w := get(t, h, http.MethodGet, "/"+locale)
		if w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	ordered := func(html string, snippets ...string) {
		t.Helper()
		for _, snippet := range snippets {
			after := strings.Index(html, snippet)
			if after < 0 {
				t.Fatalf("missing %q in order in %s", snippet, html)
			}
			html = html[after+len(snippet):]
		}
	}
	legacy := page("en")
	ordered(legacy, `class="vb-nav public-nav"`, `href="/en/writing"`, `href="/en/learn"`, `href="/en/docs"`, `href="/en/about"`, `id="navigation-menu"`, `href="/en"`)
	initial, err := cfg.Get(ctx, "site")
	if err != nil {
		t.Fatal(err)
	}
	var settings siteconfig.Site
	if err := json.Unmarshal(initial.Snapshot, &settings); err != nil {
		t.Fatal(err)
	}
	settings.HeaderMenu = &[]siteconfig.MenuItem{
		{ID: "external", Target: "custom", Label: siteconfig.Localized{"en": "Research & notes", "ar": "ملاحظات البحث"}, URL: "https://example.org/notes"},
		{ID: "home", Target: "home"},
	}
	settings.FooterMenu = &[]siteconfig.MenuItem{
		{ID: "contact", Target: "custom", Label: siteconfig.Localized{"en": "Contact"}, URL: "/en/contact"},
		{ID: "about", Target: "about"},
	}
	input := func(version int64) siteconfig.Input {
		t.Helper()
		snapshot, err := json.Marshal(settings)
		if err != nil {
			t.Fatal(err)
		}
		return siteconfig.Input{ExpectedVersion: &version, Snapshot: snapshot}
	}
	draft := input(initial.Version)
	preview, err := cfg.Preview(ctx, "site", draft)
	if err != nil || len(preview.Required) != 1 || preview.Required[0].Path != "header_menu.external.label" {
		t.Fatal(preview, err)
	}
	if _, err := cfg.Apply(ctx, owner, "site", draft); !errors.Is(err, siteconfig.ErrReview) {
		t.Fatalf("expected a translation review, got %v", err)
	}
	if strings.Contains(page("ar"), "example.org/notes") {
		t.Fatal("preview exposed unapplied links")
	}
	draft.Reviews = []siteconfig.Review{preview.Required[0].Review}
	saved, err := cfg.Apply(ctx, owner, "site", draft)
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "ar", "fr"} {
		body := page(locale)
		link := `href="https://example.org/notes" lang="en" dir="auto">Research &amp; notes</a>`
		if locale == "ar" {
			link = `href="https://example.org/notes" lang="ar" dir="auto">ملاحظات البحث</a>`
		}
		home := `href="/` + locale + `" aria-current="page"`
		ordered(body, `class="vb-nav public-nav"`, link, home, `id="navigation-menu"`, link, home,
			`<footer class="vb-footer">`, `href="/en/contact" lang="en" dir="auto">Contact</a>`, `href="/`+locale+`/about"`)
		footer := strings.SplitN(body, `<footer class="vb-footer">`, 2)[1]
		if strings.Contains(footer, `href="/`+locale+`/writing"`) || strings.Contains(footer, "example.org/notes") {
			t.Fatal("header or default menu leaked into customized footer", locale)
		}
	}
	settings.HeaderMenu = &[]siteconfig.MenuItem{}
	settings.FooterMenu = nil
	if _, err := cfg.Apply(ctx, owner, "site", input(saved.Version)); err != nil {
		t.Fatal(err)
	}
	body := page("en")
	if strings.Contains(body, `class="vb-nav public-nav"`) || strings.Contains(body, `id="navigation-menu"`) {
		t.Fatal("an explicitly empty header must stay empty")
	}
	ordered(body, `<footer class="vb-footer">`, `href="/en/writing"`, `href="/en/learn"`, `href="/en/docs"`)
}

func TestThemedErrorsAndMissingIsland(t *testing.T) {
	h := themeServer(t, false)
	if w := get(t, h, http.MethodGet, "/en/articles/math"); w.Code != 200 ||
		!strings.Contains(w.Body.String(), "Euler:") || strings.Contains(w.Body.String(), katexScript) ||
		strings.Contains(w.Body.String(), scriptName) {
		t.Fatal(w.Code, w.Body.String())
	}
	for path, status := range map[string]int{
		"/en/learn": 503, "/en/docs": 503, "/en/articles/locked": 503, "/ar/articles/missing": 404,
	} {
		w := get(t, h, http.MethodGet, path)
		body := w.Body.String()
		if w.Code != status || w.Header().Get("X-Robots-Tag") != "noindex" ||
			!strings.Contains(body, `data-rendered="server"`) || strings.Contains(body, `id="root"`) ||
			strings.Contains(body, `rel="canonical"`) {
			t.Fatal(path, w.Code, body)
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

func newThemeContent(t *testing.T) (*content.Service, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	files, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	owner, err := db.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO owner(id) VALUES(?)", owner); err != nil {
		t.Fatal(err)
	}
	c := &content.Service{DB: database, Files: files, Access: &access.Live{DB: database}}
	return c, owner
}

type themePreviewOwner struct{}

func (themePreviewOwner) Handle(http.ResponseWriter, *http.Request) bool { return false }
func (themePreviewOwner) Authorized(r *http.Request) bool {
	return r.Header.Get("X-Test-Owner") == "yes"
}

func TestPublicThemeSelection(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	cfg := &siteconfig.Service{DB: c.DB, Files: c.Files}
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{
		Assets: assets, Auth: themePreviewOwner{}, SiteConfig: cfg, Domain: &Domain{Content: c, SiteConfig: cfg},
		Config: config.Config{BaseURL: "https://canonical.example"},
	})
	request := func(path, choice string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Accept", "text/html")
		if authenticated {
			r.Header.Set("X-Test-Owner", "yes")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if choice != "" {
			body := w.Body.String()
			if w.Code != 200 || !strings.Contains(body, `data-public-theme="`+choice+`"`) ||
				strings.Contains(body, `id="root"`) || strings.Contains(body, scriptName) {
				t.Fatal(path, choice, w.Code, body)
			}
			if choice != "classic" && (!strings.Contains(body, `<style id="builtin-theme" nonce="`+w.Header().Get("X-Request-ID")+`">`) ||
				!strings.Contains(body, `[data-public-theme='`+choice+`']`)) {
				t.Fatal("missing theme stylesheet", choice)
			}
		}
		return w
	}
	request("/en", "classic", false)
	const preview = "/ar/admin/settings/theme-preview?theme=folio"
	if denied := request(preview, "", false); denied.Code != http.StatusFound || strings.Contains(denied.Body.String(), "data-public-theme") {
		t.Fatal("unexpected anonymous preview", denied.Code, denied.Body.String())
	}
	if w := request(preview, "folio", true); w.Header().Get("X-Robots-Tag") != "noindex" ||
		w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), `rel="canonical"`) ||
		!strings.Contains(w.Body.String(), `dir="rtl"`) {
		t.Fatal("unexpected preview headers or metadata", w.Header(), w.Body.String())
	}
	request("/en?theme=folio", "classic", false)
	doc, err := cfg.Get(ctx, "site")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(doc.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	input := func() siteconfig.Input {
		t.Helper()
		data, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return siteconfig.Input{ExpectedVersion: &doc.Version, Snapshot: data}
	}
	apply := func() {
		t.Helper()
		revision, err := cfg.Apply(ctx, owner, "site", input())
		if err != nil {
			t.Fatal(err)
		}
		doc = revision
	}
	delete(snapshot, "public_theme")
	apply()
	if !strings.Contains(string(doc.Snapshot), `"public_theme":"classic"`) {
		t.Fatal("older site settings must normalize to Classic")
	}
	classic := doc.RevisionID
	for _, choice := range []string{"folio", "studio"} {
		snapshot["public_theme"] = choice
		apply()
		request("/fr", choice, false)
	}
	snapshot["public_theme"] = "unknown"
	if _, err := cfg.Apply(ctx, owner, "site", input()); !errors.Is(err, siteconfig.ErrInvalid) {
		t.Fatal("accepted an unknown theme", err)
	}
	request("/en", "studio", false)
	restored, err := cfg.Restore(ctx, "site", classic, doc.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Apply(ctx, owner, "site", siteconfig.Input{ExpectedVersion: &doc.Version, Snapshot: restored.Snapshot}); err != nil {
		t.Fatal(err)
	}
	request("/en", "classic", false)
}

func TestThemedProfileUsesAppliedSettingsAndPublishedRevisions(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	cfg := &siteconfig.Service{DB: c.DB, Files: c.Files}
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
