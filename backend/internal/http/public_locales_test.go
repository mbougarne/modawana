package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestPublicLanguageVisibility(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	cfg := config.Config{BaseURL: "https://example.test"}
	settings := &siteconfig.Service{DB: c.DB, Files: c.Files}
	a := auth.New(c.DB, cfg)
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	token := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(token))
	session, err := db.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DB.Exec(`INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at) VALUES(?,?,?,'owner','passkey',1,?,?,?)`, session, owner, hex.EncodeToString(hash[:]), db.UTC(now), db.UTC(now.Add(time.Hour)), db.UTC(now)); err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, SiteConfig: settings, Config: cfg, Auth: a, Domain: &Domain{
		Auth: a, SiteConfig: settings, Content: c, Learning: learning.NewIdentity(c.DB, 180, 100),
	}})
	request := func(method, path string, authenticated bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.BaseURL+path, nil)
		r.Header.Set("Accept", "text/html")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, expected %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	publish := func(key, kind, locale string) {
		t.Helper()
		zero := int64(0)
		saved, err := c.Save(ctx, owner, key, locale, "", content.DraftInput{
			ExpectedVersion: &zero, Kind: kind, SourceLocale: locale, Slug: key,
			Title: key, Summary: "Public summary", Markdown: "Published text for " + key,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ChangePublication(ctx, owner, key, locale, "", "publish", content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID}); err != nil {
			t.Fatal(err)
		}
	}
	publish("english-guide", "doc", "en")
	publish("article-fr", "article", "fr")
	publish("article-ar", "article", "ar")
	apply := func(locales []string) {
		t.Helper()
		doc, err := settings.Get(ctx, "site")
		if err != nil {
			t.Fatal(err)
		}
		var site siteconfig.Site
		if err := json.Unmarshal(doc.Snapshot, &site); err != nil {
			t.Fatal(err)
		}
		site.PublicLocales = locales
		snapshot, err := json.Marshal(site)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := settings.Apply(ctx, owner, "site", siteconfig.Input{ExpectedVersion: &doc.Version, Snapshot: snapshot}); err != nil {
			t.Fatal(err)
		}
	}
	request("GET", "/en/doc/english-guide", false, 200)
	apply([]string{"fr", "ar"})
	root := request("GET", "/", false, 200).Body.String()
	if !strings.Contains(root, `lang="ar" dir="rtl"`) || !strings.Contains(root, `hreflang="fr"`) || strings.Contains(root, `hreflang="en"`) || strings.Contains(root, `href="/en"`) {
		t.Fatal("root did not use the effective default and visible language links")
	}
	for _, path := range []string{
		"/en", "/en/doc/english-guide", "/en/feed.xml", "/api/v1/public/page?path=/en/doc/english-guide",
		"/api/v1/public/content/en/english-guide", "/api/v1/public/alternatives/english-guide",
		"/api/v1/public/content?locale=en", "/api/v1/public/search?locale=en", "/api/v1/public/taxonomy?locale=en",
		"/api/v1/public/collections?locale=en&kind=learning", "/api/v1/public/collections/en/example",
		"/api/v1/learning/content/english-guide/en/quiz",
	} {
		w := request("GET", path, false, 404)
		if strings.Contains(w.Body.String(), "Published text for english-guide") {
			t.Fatal("hidden content leaked", path)
		}
	}
	if w := request("HEAD", "/en/doc/english-guide", false, 404); w.Header().Get("X-Robots-Tag") != "noindex" || w.Body.Len() != 0 {
		t.Fatal("unexpected hidden HEAD response", w.Header())
	}
	if got := request("GET", "/feed.xml", false, 308).Header().Get("Location"); got != "/ar/feed.xml" {
		t.Fatal(got)
	}
	sitemap := request("GET", "/sitemap.xml", false, 200).Body.String()
	if strings.Contains(sitemap, "https://example.test/en") || !strings.Contains(sitemap, "https://example.test/fr/articles/article-fr") || !strings.Contains(sitemap, "https://example.test/ar/articles/article-ar") {
		t.Fatal(sitemap)
	}
	var runtime config.Public
	if err := json.Unmarshal(request("GET", "/api/v1/public/runtime", false, 200).Body.Bytes(), &runtime); err != nil || runtime.DefaultLocale != "ar" || !slices.Equal(runtime.Locales, []string{"ar", "fr"}) {
		t.Fatal(runtime, err)
	}
	page := request("GET", "/api/v1/public/page?path=/", false, 200).Body.String()
	if !strings.Contains(page, `"locale":"ar"`) || strings.Contains(page, `"locale":"en"`) {
		t.Fatal(page)
	}
	request("GET", "/en/login", false, 200)
	request("GET", "/en/admin/settings", true, 200)
	request("GET", "/api/v1/owner/about-documents?locale=en", false, 401)
	documents := request("GET", "/api/v1/owner/about-documents?locale=en", true, 200).Body.String()
	if !strings.Contains(documents, `"key":"english-guide"`) {
		t.Fatal(documents)
	}
	apply([]string{"fr"})
	if body := request("GET", "/fr", false, 200).Body.String(); strings.Contains(body, `id="language-menu"`) {
		t.Fatal("language switcher shown with one language")
	}
	apply(nil)
	request("GET", "/en/doc/english-guide", false, 200)
}
