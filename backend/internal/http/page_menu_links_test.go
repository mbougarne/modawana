package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestPageMenuLinksFollowLocalizedPublication(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	settings := &siteconfig.Service{DB: c.DB, Files: c.Files}
	assets, err := LoadAssets(buildThemeFixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, SiteConfig: settings,
		Domain: &Domain{Content: c, SiteConfig: settings}, Config: config.Config{BaseURL: "https://example.test"}})
	request := func(path string, status int) string {
		t.Helper()
		w := get(t, h, http.MethodGet, path)
		if w.Code != status {
			t.Fatalf("%s: status %d, want %d: %s", path, w.Code, status, w.Body.String())
		}
		return w.Body.String()
	}
	const key = "contact-page"
	const englishPath = "/en/pages/contact"
	const frenchPath = "/fr/pages/nous-contacter"
	zero := int64(0)
	in := content.DraftInput{ExpectedVersion: &zero, Kind: "page", SourceLocale: "en",
		Slug: "contact", Title: "Contact & support", Markdown: "Contact details"}
	draft, err := c.Save(ctx, owner, key, "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	other := content.DraftInput{ExpectedVersion: &zero, Kind: "article", SourceLocale: "en",
		Slug: "contact-story", Title: "Contact story", Markdown: "A separate article"}
	if _, err := c.Save(ctx, owner, "contact-story", "en", "", other); err != nil {
		t.Fatal(err)
	}
	choices, err := c.ReferenceChoices(ctx, "fr", "page", "contact", "")
	if err != nil || len(choices.Items) != 1 || choices.Items[0].Key != key || choices.Items[0].Title != in.Title {
		t.Fatal("page search should find the source title while a translation is missing", choices, err)
	}

	doc, err := settings.Get(ctx, "site")
	if err != nil {
		t.Fatal(err)
	}
	var site siteconfig.Site
	if err := json.Unmarshal(doc.Snapshot, &site); err != nil {
		t.Fatal(err)
	}
	input := func() siteconfig.Input {
		t.Helper()
		snapshot, err := json.Marshal(site)
		if err != nil {
			t.Fatal(err)
		}
		return siteconfig.Input{ExpectedVersion: &doc.Version, Snapshot: snapshot}
	}
	site.HeaderMenu = &[]siteconfig.MenuItem{{ID: "contact", Target: "page", PageKey: "contact-story"}}
	_, err = settings.Preview(ctx, "site", input())
	var invalid *siteconfig.ValidationError
	if !errors.As(err, &invalid) || len(invalid.Fields) != 1 || invalid.Fields[0].Path != "header_menu.0.page_key" || invalid.Fields[0].Code != "menu_page" {
		t.Fatal("a non-page key should be rejected on its menu field", err)
	}
	(*site.HeaderMenu)[0].PageKey = key
	site.FooterMenu = &[]siteconfig.MenuItem{
		{ID: "feedback", Target: "custom", URL: "/feedback", Label: siteconfig.Localized{"en": "Feedback"}},
		{ID: "contact", Target: "page", PageKey: key},
	}
	doc, err = settings.Apply(ctx, owner, "site", input())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc.Snapshot), key) {
		t.Fatal("settings did not save the draft page reference")
	}

	publicSite := "/api/v1/public/site?locale="
	assertMissing := func(locale, path string) {
		t.Helper()
		html := request("/"+locale, 200)
		if strings.Contains(html, path) || strings.Contains(html, "class=\"vb-nav public-nav\"") ||
			!strings.Contains(html, `href="/feedback"`) {
			t.Fatal("unavailable page changed the configured menus", locale)
		}
		body := request(publicSite+locale, 200)
		if strings.Contains(body, key) || !strings.Contains(body, `"header_menu":[]`) {
			t.Fatal("public site did not preserve the empty header override", body)
		}
	}
	assertMissing("en", englishPath)

	publish := func(locale, operation string, action content.Action) content.Result {
		t.Helper()
		result, err := c.ChangePublication(ctx, owner, key, locale, "", operation, action)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	live := publish("en", "publish", content.Action{ExpectedVersion: &draft.Version, RevisionID: draft.RevisionID})
	englishLink := `href="/en/pages/contact" lang="en" dir="auto">Contact &amp; support</a>`
	if html := request("/en", 200); strings.Count(html, englishLink) != 3 {
		t.Fatal("expected the published title in the English header, mobile menu and footer")
	}
	currentLink := `href="/en/pages/contact" aria-current="page" lang="en" dir="auto"`
	if html := request(englishPath, 200); strings.Count(html, currentLink) != 2 {
		t.Fatal("the active page was not marked in both header menus")
	}
	if !strings.Contains(request(publicSite+"en", 200), key) {
		t.Fatal("published page reference missing from the public site API")
	}
	assertMissing("fr", englishPath)

	in.ExpectedVersion = &live.Version
	in.Title = "Updated contact title"
	in.BodyFormat, in.Markdown = content.BlockBodyFormat, ""
	in.PageBlocks = json.RawMessage(`{"version":1,"blocks":[{"id":"heading","type":"core.heading","version":1,"properties":{"level":2,"text":"Contact"}}]}`)
	update, err := c.Save(ctx, owner, key, "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	if html := request("/en", 200); !strings.Contains(html, englishLink) || strings.Contains(html, in.Title) {
		t.Fatal("the working title replaced the published menu label")
	}
	fr, err := c.Save(ctx, owner, key, "fr", "", content.DraftInput{ExpectedVersion: &zero, Kind: "page",
		SourceLocale: "en", SourceRevisionID: live.RevisionID, Slug: "nous-contacter", Title: "Nous contacter", Markdown: "Coordonnées"})
	if err != nil {
		t.Fatal(err)
	}
	choices, err = c.ReferenceChoices(ctx, "fr", "page", "nous contacter", "")
	if err != nil || len(choices.Items) != 1 || choices.Items[0].Key != key {
		t.Fatal("page search should use the localized title", choices, err)
	}
	assertMissing("fr", frenchPath)
	action := content.Action{ExpectedVersion: &fr.Version, RevisionID: fr.RevisionID, SourceRevisionID: live.RevisionID}
	review := publish("fr", "review", action)
	action.ExpectedVersion = &review.Version
	publish("fr", "publish", action)
	frenchLink := `href="/fr/pages/nous-contacter" lang="fr" dir="auto">Nous contacter</a>`
	if html := request("/fr", 200); strings.Count(html, frenchLink) != 3 || strings.Contains(html, englishPath) {
		t.Fatal("expected only the French page destination in the French menus")
	}
	unpublished := publish("en", "unpublish", content.Action{ExpectedVersion: &update.Version, RevisionID: live.RevisionID})
	assertMissing("en", englishPath)
	if !strings.Contains(request("/fr", 200), frenchLink) {
		t.Fatal("unpublishing English should leave the published French destination")
	}
	publish("en", "publish", content.Action{ExpectedVersion: &unpublished.Version, RevisionID: update.RevisionID})
	if html := request("/en", 200); strings.Count(html, ">"+in.Title+"</a>") != 3 {
		t.Fatal("publishing a block revision should update the existing menu link")
	}
	site.PublicLocales = []string{"en"}
	if _, err := settings.Apply(ctx, owner, "site", input()); err != nil {
		t.Fatal(err)
	}
	request("/fr", 404)
	if strings.Contains(request(publicSite+"fr", 200), key) {
		t.Fatal("public API advertised the hidden French destination")
	}
}
