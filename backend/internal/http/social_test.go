package httpserver

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestSocialImageSelectionPreservesPublicBoundaries(t *testing.T) {
	const origin = "https://blog.example"
	settings := siteconfig.Public{Site: siteconfig.Site{LogoMode: "image", LogoID: "logo"}, Profile: siteconfig.Profile{BannerEnabled: true, BannerID: "banner", PortraitID: "portrait"}, Effective: map[string]siteconfig.Effective{"name": {Text: "Name"}, "banner_alt": {Text: "Banner"}}}
	p := content.PageRoute{Kind: "article", Content: &content.PublicContent{CoverID: "cover", Media: []content.PublicMedia{{ID: "cover", Kind: "image", MIME: "image/png", Width: 1200, Height: 630}}}}
	if got := socialFor(p, settings, origin, "Title"); got.Image != origin+"/media/cover" || got.Type != "article" || got.Width != 1200 {
		t.Fatal(got)
	}
	p.Kind = "lesson"
	if got := socialFor(p, settings, origin, "Title"); got.Image != origin+"/media/banner" {
		t.Fatal("learner cover leaked", got)
	}
	p.Kind = "article"
	p.Content.Locked = true
	if got := socialFor(p, settings, origin, "Title"); got.Image != origin+"/media/banner" {
		t.Fatal("locked cover leaked", got)
	}
	p.Content.Locked = false
	p.Content.Media = nil
	if got := socialFor(p, settings, origin, "Title"); got.Image != origin+"/media/banner" {
		t.Fatal("unlisted cover leaked", got)
	}
	settings.Profile.BannerEnabled = false
	if got := socialFor(p, settings, origin, "Title"); got.Image != origin+"/media/portrait" {
		t.Fatal(got)
	}
	settings.Profile.PortraitID = ""
	if got := socialFor(p, settings, origin, "Title"); got.Image != origin+"/media/logo" {
		t.Fatal(got)
	}
	settings.Site.LogoMode = "wordmark"
	if got := socialFor(p, settings, origin, "Title"); got.Image != origin+"/social-card.png" || got.Width != 1200 || got.Height != 630 {
		t.Fatal(got)
	}
}

func TestSocialCardHTTPImageAndHead(t *testing.T) {
	h := New(Options{Config: config.Config{BaseURL: "https://blog.example"}})
	for _, method := range []string{"GET", "HEAD", "POST"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/social-card.png", nil))
		if method == "POST" {
			if w.Code != 405 {
				t.Fatal(w.Code)
			}
			continue
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("social image cache policy")
		}
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
			t.Fatal(w.Code, w.Header())
		}
		if method == "HEAD" {
			if w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
			continue
		}
		image, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if image.Bounds().Dx() != 1200 || image.Bounds().Dy() != 630 {
			t.Fatal(image.Bounds())
		}
	}
}

type socialOwnerFixture struct{}

func (socialOwnerFixture) Handle(http.ResponseWriter, *http.Request) bool { return false }
func (socialOwnerFixture) Authorized(*http.Request) bool                  { return true }

func TestInitialHTMLSocialCardsAndPrivateExclusion(t *testing.T) {
	assets, err := LoadAssets(buildFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, Domain: pageFixture{}, Auth: socialOwnerFixture{}, Config: config.Config{BaseURL: "https://blog.example"}})
	for _, path := range []string{"/ar/articles/%D9%85%D9%82%D8%AF%D9%85%D8%A9", "/ar/learn/path/locked", "/ar/articles/missing", "/en/login", "/en/admin/security"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "attacker.example"
		h.ServeHTTP(w, r)
		body := w.Body.String()
		if strings.Contains(body, "attacker.example") {
			t.Fatal("request host used")
		}
		if strings.Contains(path, "%D9") {
			for _, tag := range []string{`property="og:image" content="https://blog.example/social-card.png"`, `name="twitter:card" content="summary_large_image"`, `name="twitter:title" content="A &lt;script&gt;`, `property="og:image:width" content="1200"`} {
				if !strings.Contains(body, tag) {
					t.Fatal("missing "+tag, body)
				}
			}
		} else if strings.Contains(body, `property="og:image"`) || strings.Contains(body, `name="twitter:image"`) {
			t.Fatal("inaccessible/login social image", path, body)
		}
	}
}

type localizedSocialFixture struct{}

func (localizedSocialFixture) Handle(http.ResponseWriter, *http.Request) bool { return false }
func (localizedSocialFixture) ResolvePage(_ context.Context, path string) (content.PageRoute, error) {
	locale := strings.Split(path, "/")[1]
	title := map[string]string{"en": "Introduction", "ar": "مقدمة", "fr": "Présentation"}[locale]
	return content.PageRoute{Kind: "article", Locale: locale, URL: path, Title: title, Summary: title + " summary", Content: &content.PublicContent{CoverID: "published-cover", Media: []content.PublicMedia{{ID: "published-cover", Kind: "image", MIME: "image/jpeg", Width: 1200, Height: 630}}}}, nil
}
func TestLocalizedPublishedCoverInInitialHTML(t *testing.T) {
	assets, err := LoadAssets(buildFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, Domain: localizedSocialFixture{}, Config: config.Config{BaseURL: "https://blog.example"}})
	for locale, title := range map[string]string{"en": "Introduction", "ar": "مقدمة", "fr": "Présentation"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/"+locale+"/articles/post", nil))
		for _, tag := range []string{`property="og:image" content="https://blog.example/media/published-cover"`, `property="og:type" content="article"`, `name="twitter:title" content="` + title + `"`, `name="twitter:description" content="` + title + ` summary"`} {
			if !strings.Contains(w.Body.String(), tag) {
				t.Fatal("missing "+tag, w.Body.String())
			}
		}
	}
}
