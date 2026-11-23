package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestLocalizedFrontPageKeepsCanonicalAndNestedMenus(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	settings := &siteconfig.Service{DB: c.DB, Files: c.Files}
	assets, err := LoadAssets(buildThemeFixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	server := New(Options{Assets: assets, SiteConfig: settings, Config: config.Config{BaseURL: "https://example.test"},
		Domain: &Domain{Content: c, SiteConfig: settings}})
	zero := int64(0)
	const key = "local-front"
	saved, err := c.Save(ctx, owner, key, "ar", "", content.DraftInput{
		ExpectedVersion: &zero, Kind: "page", SourceLocale: "ar", Slug: "welcome",
		Title: "مرحباً", Markdown: "محتوى الصفحة الرئيسية",
	})
	if err != nil {
		t.Fatal(err)
	}
	live, err := c.ChangePublication(ctx, owner, key, "ar", "", "publish",
		content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := settings.Get(ctx, "site")
	if err != nil {
		t.Fatal(err)
	}
	var site siteconfig.Site
	if err := json.Unmarshal(doc.Snapshot, &site); err != nil {
		t.Fatal(err)
	}
	site.FrontPages = siteconfig.Localized{"ar": key}
	menu := []siteconfig.MenuItem{{ID: "parent", Target: "docs", Children: []siteconfig.MenuItem{
		{ID: "child", Target: "page", PageKey: key},
	}}}
	site.HeaderMenu, site.FooterMenu = &menu, &menu
	raw, err := json.Marshal(site)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Apply(ctx, owner, "site", siteconfig.Input{Snapshot: raw, ExpectedVersion: &doc.Version}); err != nil {
		t.Fatal(err)
	}
	w := get(t, server, http.MethodGet, "/ar")
	for _, want := range []string{`lang="ar" dir="rtl"`, `rel="canonical" href="https://example.test/ar"`, `class="public-nav-children"`, "محتوى الصفحة الرئيسية", `href="/ar" aria-current="page" lang="ar"`} {
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Fatal(w.Code, want, w.Body.String())
		}
	}
	if w := get(t, server, http.MethodGet, "/ar/pages/welcome"); w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/ar" {
		t.Fatal(w.Code, w.Header())
	}
	if w := get(t, server, http.MethodGet, "/en"); w.Code != 200 || strings.Contains(w.Body.String(), "محتوى الصفحة الرئيسية") {
		t.Fatal("Arabic page leaked into English home", w.Code)
	}
	search, err := c.Discover(ctx, content.Filter{Locale: "ar", Kind: "page"}, access.Principal{})
	if err != nil || len(search.Items) != 1 || search.Items[0].URL != "/ar" {
		t.Fatal("discovery URL differs from canonical", search, err)
	}
	if w := get(t, server, http.MethodGet, "/sitemap.xml"); w.Code != 200 ||
		!strings.Contains(w.Body.String(), "<loc>https://example.test/ar</loc>") || strings.Contains(w.Body.String(), "/ar/pages/welcome") {
		t.Fatal("sitemap canonical mismatch", w.Code, w.Body.String())
	}
	if _, err := c.ChangePublication(ctx, owner, key, "ar", "", "unpublish",
		content.Action{ExpectedVersion: &live.Version, RevisionID: live.RevisionID}); err != nil {
		t.Fatal(err)
	}
	w = get(t, server, http.MethodGet, "/ar")
	if w.Code != 200 || strings.Contains(w.Body.String(), "محتوى الصفحة الرئيسية") ||
		strings.Contains(w.Body.String(), `class="public-nav-children"`) {
		t.Fatal("unpublished front page still present", w.Code)
	}
}
