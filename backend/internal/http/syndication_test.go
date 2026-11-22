package httpserver

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestPublicSyndicationUsesPublishedLocaleAndCanonicalOrigin(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	settings := &siteconfig.Service{DB: c.DB, Files: c.Files}
	configDoc, err := settings.Get(ctx, "profile")
	if err != nil {
		t.Fatal(err)
	}
	var profile siteconfig.Profile
	if err := json.Unmarshal(configDoc.Snapshot, &profile); err != nil {
		t.Fatal(err)
	}
	profile.Name["en"] = "Test blog"
	profile.Links = []siteconfig.Link{{ID: "external-feed", Icon: "rss", URL: "https://external.test/feeds", Label: siteconfig.Localized{"en": "RSS"}, Visible: true}}
	configDoc.Snapshot, err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Apply(ctx, owner, "profile", siteconfig.Input{ExpectedVersion: &configDoc.Version, Snapshot: configDoc.Snapshot}); err != nil {
		t.Fatal(err)
	}

	const origin = "https://blog.example"
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, SiteConfig: settings, Config: config.Config{BaseURL: origin}, Domain: &Domain{Content: c}})
	request := func(method, path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://untrusted.test"+path, nil)
		r.Header.Set("X-Forwarded-Host", "untrusted.test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "untrusted.test") {
			t.Fatal(path, w.Code, w.Header(), w.Body.String())
		}
		return w
	}
	save := func(key, locale, slug, title string, version int64) content.Result {
		t.Helper()
		out, err := c.Save(ctx, owner, key, locale, "", content.DraftInput{
			ExpectedVersion: &version, Kind: "article", SourceLocale: locale, Slug: slug, Title: title,
			Summary: "Read <example> & more", Markdown: "Unsyndicated article body",
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	change := func(key, locale, op string, saved content.Result) content.Result {
		t.Helper()
		out, err := c.ChangePublication(ctx, owner, key, locale, "", op, content.Action{
			ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	const slug = "مقدمة"
	initial := change("arabic", "ar", "publish", save("arabic", "ar", slug, "عنوان & مقدمة", 0))
	change("french", "fr", "publish", save("french", "fr", "intro", "Article français", 0))
	save("future", "ar", "future", "Draft-only title", 0)
	pending := save("arabic", "ar", slug, "عنوان جديد", initial.Version)

	feed := func(path, locale string, count int) rssChannel {
		t.Helper()
		w := request(http.MethodGet, path, http.StatusOK)
		var parsed struct {
			XMLName xml.Name `xml:"rss"`
			Version string   `xml:"version,attr"`
			Channel struct {
				AtomSelf rssSelf `xml:"http://www.w3.org/2005/Atom link"`
				rssChannel
			} `xml:"channel"`
		}
		if err := xml.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
			t.Fatal(err, w.Body.String())
		}
		ch := parsed.Channel.rssChannel
		if w.Header().Get("Content-Type") != "application/rss+xml; charset=utf-8" || parsed.Version != "2.0" ||
			ch.Title != "Test blog" || ch.Language != locale || ch.Link != origin+"/"+locale ||
			parsed.Channel.AtomSelf.Href != origin+path || parsed.Channel.AtomSelf.Rel != "self" ||
			len(ch.Items) != count || strings.Contains(w.Body.String(), "Unsyndicated article body") {
			t.Fatal(w.Header(), w.Body.String())
		}
		head := request(http.MethodHead, path, http.StatusOK)
		if head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
			t.Fatal("feed HEAD", head.Header(), head.Body.Len())
		}
		return ch
	}
	ar := feed("/ar/feed.xml", "ar", 1).Items[0]
	if ar.Title != "عنوان & مقدمة" || ar.Description != "Read &lt;example&gt; &amp; more" ||
		ar.Link != origin+"/ar/articles/"+url.PathEscape(slug) || ar.GUID.ID == "" || ar.GUID.Permalink != "false" {
		t.Fatal(ar)
	}
	if _, err := time.Parse(time.RFC1123Z, ar.Published); err != nil {
		t.Fatal(err)
	}
	if fr := feed("/fr/feed.xml", "fr", 1); fr.Items[0].Title != "Article français" {
		t.Fatal(fr)
	}
	feed("/en/feed.xml", "en", 0)
	second := change("arabic", "ar", "publish", pending)
	if updated := feed("/ar/feed.xml", "ar", 1).Items[0]; updated.GUID.ID != ar.GUID.ID || updated.Title != "عنوان جديد" {
		t.Fatal("publication update changed the stable GUID or omitted new metadata", updated)
	}

	index := func() map[string]string {
		t.Helper()
		w := request(http.MethodGet, "/sitemap.xml", http.StatusOK)
		var doc sitemapDocument
		if err := xml.Unmarshal(w.Body.Bytes(), &doc); err != nil || w.Header().Get("Content-Type") != "application/xml; charset=utf-8" {
			t.Fatal(err, w.Header(), w.Body.String())
		}
		urls := map[string]string{}
		for _, entry := range doc.URLs {
			if _, duplicate := urls[entry.Location]; duplicate {
				t.Fatal("duplicate sitemap URL", entry.Location)
			}
			urls[entry.Location] = entry.Modified
		}
		return urls
	}
	urls := index()
	if len(urls) != 17 || urls[ar.Link] == "" || urls[origin+"/fr/articles/intro"] == "" {
		t.Fatal(urls)
	}
	change("arabic", "ar", "unpublish", second)
	feed("/ar/feed.xml", "ar", 0)
	if _, ok := index()[ar.Link]; ok {
		t.Fatal("unpublished page remains in sitemap")
	}
	head := request(http.MethodHead, "/sitemap.xml", http.StatusOK)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatal("sitemap HEAD", head.Header(), head.Body.Len())
	}
	if w := request(http.MethodGet, "/feed.xml", http.StatusPermanentRedirect); w.Header().Get("Location") != "/en/feed.xml" {
		t.Fatal(w.Header())
	}
	if w := request(http.MethodGet, "/robots.txt", http.StatusOK); !strings.Contains(w.Body.String(), "Sitemap: "+origin+"/sitemap.xml\n") {
		t.Fatal(w.Body.String())
	}
	request(http.MethodGet, "/de/feed.xml", http.StatusNotFound)
	request(http.MethodPost, "/ar/feed.xml", http.StatusMethodNotAllowed)
	pageHTML := request(http.MethodGet, "/ar", http.StatusOK).Body.String()
	if !strings.Contains(pageHTML, `rel="alternate" type="application/rss+xml" title="RSS" href="`+origin+`/ar/feed.xml"`) ||
		!strings.Contains(pageHTML, `href="https://external.test/feeds">RSS</a>`) ||
		strings.Contains(pageHTML, `href="/ar/feed.xml">RSS</a>`) {
		t.Fatal("feed discovery or configured footer link changed")
	}
	if login := request(http.MethodGet, "/ar/login", http.StatusOK); strings.Contains(login.Body.String(), "application/rss+xml") {
		t.Fatal("private shell advertises feed")
	}
}
