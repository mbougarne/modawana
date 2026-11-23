package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestStandalonePagesPublishTheirExactTemplate(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	settings := &siteconfig.Service{DB: c.DB, Files: c.Files}
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, Auth: themePreviewOwner{}, SiteConfig: settings,
		Domain: &Domain{Content: c, SiteConfig: settings}, Config: config.Config{BaseURL: "https://example.test"}})
	request := func(path string, status int) string {
		t.Helper()
		w := get(t, h, http.MethodGet, path)
		if w.Code != status {
			t.Fatalf("%s: status %d, want %d: %s", path, w.Code, status, w.Body.String())
		}
		return w.Body.String()
	}
	zero := int64(0)
	in := content.DraftInput{ExpectedVersion: &zero, Kind: "page", PageTemplate: "landing", SourceLocale: "en",
		Slug: "contact", Title: "Contact page", Summary: "Get in touch", Markdown: "Published page body"}
	saved, err := c.Save(ctx, owner, "contact", "en", "page-save-0001", in)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/en/admin/pages", nil)
	r.Header.Set("X-Test-Owner", "yes")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatal("owner Pages route unavailable", w.Code)
	}
	items, _, err := c.OwnerList(ctx, "en", "page", "draft", "", "")
	if err != nil || len(items) != 1 || items[0].Key != "contact" {
		t.Fatal(items, err)
	}
	request("/en/pages/contact", 404)
	request("/api/v1/public/content/en/contact", 404)
	live, err := c.ChangePublication(ctx, owner, "contact", "en", "", "publish",
		content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	const publicPath = "/en/pages/contact"
	html := request(publicPath, 200)
	for _, want := range []string{`data-page-template="landing"`, "Published page body", `rel="canonical" href="https://example.test/en/pages/contact"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("public page missing %q", want)
		}
	}
	request("/en/articles/contact", 404)
	var route content.PageRoute
	if err := json.Unmarshal([]byte(request("/api/v1/public/page?path="+publicPath, 200)), &route); err != nil || route.Kind != "page" || route.Content == nil || route.Content.PageTemplate != "landing" {
		t.Fatal(route, err)
	}
	if !strings.Contains(request("/en/search?q=contact", 200), publicPath) || !strings.Contains(request("/sitemap.xml", 200), publicPath) {
		t.Fatal("published page missing from search or sitemap")
	}
	if strings.Contains(request("/en/writing", 200), publicPath) || strings.Contains(request("/en/feed.xml", 200), publicPath) {
		t.Fatal("standalone page appeared in the writing stream")
	}
	menuDoc, err := settings.Get(ctx, "site")
	if err != nil {
		t.Fatal(err)
	}
	var site siteconfig.Site
	if err := json.Unmarshal(menuDoc.Snapshot, &site); err != nil {
		t.Fatal(err)
	}
	site.HeaderMenu = &[]siteconfig.MenuItem{{ID: "contact", Target: "custom", Label: siteconfig.Localized{"en": "Contact"}, URL: publicPath}}
	snapshot, err := json.Marshal(site)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Apply(ctx, owner, "site", siteconfig.Input{ExpectedVersion: &menuDoc.Version, Snapshot: snapshot}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request("/en", 200), `href="/en/pages/contact"`) {
		t.Fatal("published page could not be linked from the menu")
	}
	in.ExpectedVersion = &live.Version
	in.PageTemplate, in.Markdown = "standard", "Unpublished replacement"
	draft, err := c.Save(ctx, owner, "contact", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	html = request(publicPath, 200)
	if !strings.Contains(html, `data-page-template="landing"`) || strings.Contains(html, in.Markdown) {
		t.Fatal("draft changes reached the public page")
	}
	current, err := c.ChangePublication(ctx, owner, "contact", "en", "", "publish",
		content.Action{ExpectedVersion: &draft.Version, RevisionID: draft.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	if html := request(publicPath, 200); !strings.Contains(html, `data-page-template="standard"`) || !strings.Contains(html, in.Markdown) {
		t.Fatal("newly published template did not render")
	}
	restored, err := c.Restore(ctx, owner, "contact", "en", "", content.Action{ExpectedVersion: &current.Version, RevisionID: saved.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	working, err := c.Draft(ctx, "contact", "en")
	if err != nil || working.Revision.ID != restored.RevisionID || working.Revision.PageTemplate != "landing" || working.PublishedID != draft.RevisionID {
		t.Fatal("history did not restore the template as a draft", working, err)
	}
	translated := content.DraftInput{ExpectedVersion: &zero, Kind: "page", SourceLocale: "en", SourceRevisionID: current.RevisionID,
		Slug: "contact-fr", Title: "Contact en français", Markdown: "Nous contacter"}
	fr, err := c.Save(ctx, owner, "contact", "fr", "", translated)
	if err != nil {
		t.Fatal(err)
	}
	action := content.Action{ExpectedVersion: &fr.Version, RevisionID: fr.RevisionID, SourceRevisionID: current.RevisionID}
	if _, err := c.ChangePublication(ctx, owner, "contact", "fr", "", "publish", action); !errors.Is(err, content.ErrReview) {
		t.Fatal("translation published without review", err)
	}
	reviewed, err := c.ChangePublication(ctx, owner, "contact", "fr", "", "review", action)
	if err != nil {
		t.Fatal(err)
	}
	action.ExpectedVersion = &reviewed.Version
	if _, err := c.ChangePublication(ctx, owner, "contact", "fr", "", "publish", action); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request(publicPath, 200), `hreflang="fr" href="https://example.test/fr/pages/contact-fr"`) ||
		!strings.Contains(request("/fr/pages/contact-fr", 200), `data-page-template="standard"`) {
		t.Fatal("published translation missing or default template unavailable")
	}
}
