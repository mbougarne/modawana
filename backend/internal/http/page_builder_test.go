package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestSavedPagePreviewMatchesPublicRenderer(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	settings := &siteconfig.Service{DB: c.DB, Files: c.Files}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(c.DB, cfg)
	mediaService := media.New(c.DB, c.Files, c.Access)
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, Config: cfg, Auth: a, SiteConfig: settings,
		Domain: &Domain{Content: c, Auth: a, SiteConfig: settings, Media: mediaService}})
	raw := strings.Repeat("p", 43)
	sum := sha256.Sum256([]byte(raw))
	session, _ := db.NewID()
	now := time.Now()
	if _, err := c.DB.Exec(`INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at)
		VALUES(?,?,?,'owner','passkey',1,?,?,?)`, session, owner, hex.EncodeToString(sum[:]), db.UTC(now), db.UTC(now.Add(time.Hour)), db.UTC(now)); err != nil {
		t.Fatal(err)
	}
	request := func(path string, withSession bool, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "https://example.test"+path, nil)
		if withSession {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: wanted %d, got %d: %s", path, status, w.Code, w.Body.String())
		}
		return w
	}
	pageBody := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		_, body, found := strings.Cut(w.Body.String(), `<article class="public-reader public-reader-page"`)
		end := strings.LastIndex(body, "</article>")
		if !found || end < 0 {
			t.Fatal("standalone page markup missing")
		}
		return body[:end]
	}

	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 12, 12))); err != nil {
		t.Fatal(err)
	}
	img, err := mediaService.Upload(ctx, owner, "hero.png", "image/png", "content", &imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	const key = "builder"
	var sourceRevision string
	for _, tc := range []struct{ lang, slug, text string }{
		{"en", "builder", "Welcome"},
		{"fr", "bienvenue", "Bienvenue"},
		{"ar", "marhaban", "مرحباً"},
	} {
		draft := content.DraftInput{ExpectedVersion: &zero, Kind: "page", PageTemplate: "landing",
			Title: tc.text, Summary: "Summary", Slug: tc.slug, SourceLocale: "en", SourceRevisionID: sourceRevision,
			BodyFormat: content.BlockBodyFormat, PageBlocks: json.RawMessage(fmt.Sprintf(`{"version":1,"blocks":[
			{"id":"hero","type":"core.hero","version":1,"properties":{"heading":%q,"text":"Text","media_id":%q,"alt":"Shape"}},
			{"id":"heading","type":"core.heading","version":1,"properties":{"text":"A < B","level":3}},
			{"id":"columns","type":"core.columns","version":1,"properties":{"columns":[[
				{"id":"copy","type":"core.markdown","version":1,"properties":{"markdown":"**Body**"}}
			],[
				{"id":"visual","type":"core.image","version":1,"properties":{"media_id":%q,"alt":"Photo"}}
			]]}},
			{"id":"button","type":"core.button","version":1,"properties":{"label":"Home","href":"/en"}},
			{"id":"cta","type":"core.call_to_action","version":1,"properties":{"heading":"Next","label":"Search","href":"/en/search"}},
			{"id":"posts","type":"core.content_list","version":1,"properties":{"section":"writing","limit":2}}
		]}`, tc.text, img.ID, img.ID))}
		saved, err := c.Save(ctx, owner, key, tc.lang, "", draft)
		if err != nil {
			t.Fatal(err)
		}
		previewPath := "/en/admin/page-preview/" + key + "/" + tc.lang + "/" + saved.RevisionID
		publicPath := "/" + tc.lang + "/pages/" + tc.slug
		request(previewPath, false, 401)
		request(publicPath, true, 404)
		if tc.lang == "en" {
			request(img.URL, false, 404)
			request(img.URL, true, 200)
		}
		preview := request(previewPath, true, 200)
		if !strings.Contains(preview.Header().Get("X-Robots-Tag"), "noindex") ||
			!strings.Contains(preview.Header().Get("Cache-Control"), "no-store") ||
			strings.Contains(preview.Body.String(), `rel="canonical"`) {
			t.Fatal("preview cache/index isolation missing")
		}
		if tc.lang == "ar" && !strings.Contains(preview.Body.String(), `<html lang="ar" dir="rtl">`) {
			t.Fatal("Arabic preview is not RTL")
		}
		before := pageBody(preview)
		for _, want := range []string{`data-page-template="landing"`, `<h3 dir="auto">A &lt; B</h3>`, "<strong>Body</strong>", img.URL, `data-block-id="posts"`} {
			if !strings.Contains(before, want) {
				t.Fatalf("preview missing %q", want)
			}
		}

		action := content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID, SourceRevisionID: sourceRevision}
		if tc.lang != "en" {
			review, err := c.ChangePublication(ctx, owner, key, tc.lang, "", "review", action)
			if err != nil {
				t.Fatal(err)
			}
			action.ExpectedVersion = &review.Version
		}
		live, err := c.ChangePublication(ctx, owner, key, tc.lang, "", "publish", action)
		if err != nil {
			t.Fatal(err)
		}
		if tc.lang == "en" {
			sourceRevision = live.RevisionID
		}
		if public := request(publicPath, false, 200); pageBody(public) != before ||
			!strings.Contains(public.Body.String(), `rel="canonical" href="https://example.test`+publicPath+`"`) {
			t.Fatal("publication diverged from the shared preview or lost its canonical URL")
		}
	}

	if err := c.Files.Exclusive(ctx, func() error { return c.Files.RemoveUploadLocked(img.File.Key) }); err != nil {
		t.Fatal(err)
	}
	draft, err := c.Draft(ctx, key, "en")
	if err != nil {
		t.Fatal(err)
	}
	action := content.Action{ExpectedVersion: &draft.Version, RevisionID: draft.Revision.ID}
	if _, err := c.ChangePublication(ctx, owner, key, "en", "", "publish", action); !errors.Is(err, content.ErrInvalid) {
		t.Fatal("publishing with a missing dependency must fail", err)
	}
	fr := request("/fr/pages/bienvenue", false, 200)
	if strings.Contains(fr.Body.String(), img.URL) || !strings.Contains(fr.Body.String(), "Bloc indisponible") {
		t.Fatal("a missing upload did not use the localized block fallback")
	}
}

func TestRendererKeepsUnsupportedBlockPropertiesOutOfHTML(t *testing.T) {
	doc := []byte(`{"version":1,"blocks":[{"id":"custom","type":"future.block","version":2,"properties":{"text":"Private imported data"}}]}`)
	html, err := content.RenderPageBlocks(doc, content.PageBlockRenderOptions{Unavailable: "Block unavailable"})
	if err != nil || !strings.Contains(string(html), "Block unavailable") || strings.Contains(string(html), "Private imported data") {
		t.Fatal(html, err)
	}
}
