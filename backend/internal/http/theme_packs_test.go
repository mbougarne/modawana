package httpserver

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

func TestThemePackApprovalPreviewSelectionAndFallback(t *testing.T) {
	ctx := t.Context()
	content, owner := newThemeContent(t)
	root := t.TempDir()
	packs := &theme.Service{DB: content.DB, DataRoot: root}
	settings := &siteconfig.Service{DB: content.DB, Files: content.Files, Themes: packs}
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, Auth: themePreviewOwner{}, SiteConfig: settings, Domain: &Domain{SiteConfig: settings, Content: content}})
	id := "pack-review"
	filename := filepath.Join(root, theme.PackPath(id))
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	writePack := func(name string) {
		t.Helper()
		raw, err := json.Marshal(map[string]any{
			"api_version": 1, "name": name,
			"layout": `{{template "banner" .}}{{template "header" .}}{{template "content" .}}{{template "footer" .}}`,
			"css":    ".vb-title { letter-spacing: 0.02em; }", "banner_png": pngBytes.Bytes(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	list := func(state string) theme.Status {
		t.Helper()
		items, err := packs.List(ctx)
		if err != nil || len(items) != 1 || items[0].State != state {
			t.Fatal(items, err)
		}
		return items[0]
	}
	writePack("Editorial")
	candidate := list("available")
	noAuth := get(t, h, http.MethodGet, packAPI)
	if noAuth.Code != http.StatusUnauthorized || strings.Contains(noAuth.Body.String(), "Editorial") {
		t.Fatal("owner catalog disclosed", noAuth.Code)
	}
	input, err := settings.Get(ctx, "site")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot siteconfig.Site
	if err := json.Unmarshal(input.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.PublicTheme = id
	publish := func() error {
		t.Helper()
		b, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		_, err = settings.Apply(ctx, owner, "site", siteconfig.Input{ExpectedVersion: &input.Version, Snapshot: b})
		return err
	}
	if err := publish(); err == nil {
		t.Fatal("accepted unapproved theme")
	}
	check := func(*sql.Tx) error { return nil }
	denied := errors.New("fresh session required")
	if _, err := packs.Approve(ctx, id, candidate.CandidateChecksum, "", func(*sql.Tx) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("approval ignored session recheck", err)
	}
	if _, err := packs.Approve(ctx, id, strings.Repeat("0", 64), "", check); !errors.Is(err, theme.ErrPackChanged) {
		t.Fatal("stale candidate accepted", err)
	}
	approved, err := packs.Approve(ctx, id, candidate.CandidateChecksum, "", check)
	if err != nil {
		t.Fatal(err)
	}
	list("installed")
	if _, err := packs.Approve(ctx, id, approved.Checksum, "", check); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("stale approval revision accepted", err)
	}
	pack, err := packs.ApprovedPack(ctx, packs.DB, id)
	if err != nil {
		t.Fatal(err)
	}
	previewPath := "/ar/admin/settings/theme-preview?theme=" + id
	previewRequest := func() *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, previewPath, nil)
		r.Header.Set("Accept", "text/html")
		r.Header.Set("X-Test-Owner", "yes")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	preview := previewRequest()
	if preview.Code != 200 || preview.Header().Get("X-Robots-Tag") != "noindex" ||
		!strings.Contains(preview.Body.String(), `dir="rtl"`) || !strings.Contains(preview.Body.String(), pack.BannerURL) {
		t.Fatal("owner preview failed", preview.Code)
	}
	if w := get(t, h, http.MethodGet, "/en?theme="+id); !strings.Contains(w.Body.String(), `data-public-theme="classic"`) {
		t.Fatal("request query overrode the active theme")
	}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	page := get(t, h, http.MethodGet, "/fr")
	body := page.Body.String()
	if page.Code != 200 || !strings.Contains(body, `data-public-theme="`+id+`"`) ||
		!strings.Contains(body, `[data-public-theme='`+id+`'] .vb-title`) ||
		strings.Index(body, pack.BannerURL) < 0 || strings.Index(body, pack.BannerURL) > strings.Index(body, "<header") {
		t.Fatal("approved pack layout or styles missing", page.Code)
	}
	if w := get(t, h, http.MethodGet, pack.BannerURL); w.Code != 200 ||
		w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), pngBytes.Bytes()) {
		t.Fatal("approved image is unavailable", w.Code)
	}
	writePack("Edited")
	update := list("changed")
	if w := get(t, h, http.MethodGet, pack.BannerURL); w.Code != 404 {
		t.Fatal("served image from changed files")
	}
	if w := get(t, h, http.MethodGet, "/ar"); w.Code != 200 || !strings.Contains(w.Body.String(), `data-public-theme="classic"`) {
		t.Fatal("failed to fall back after file changes")
	}
	if w := previewRequest(); w.Code != 422 {
		t.Fatal("preview rendered an unapproved update", w.Code)
	}
	reapproved, err := packs.Approve(ctx, id, update.CandidateChecksum, approved.Revision, check)
	if err != nil {
		t.Fatal(err)
	}
	if w := get(t, h, http.MethodGet, "/ar"); w.Code != 200 || !strings.Contains(w.Body.String(), `data-public-theme="`+id+`"`) {
		t.Fatal("the approved update did not resume the applied theme")
	}
	if err := packs.Remove(ctx, id, approved.Revision, check); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("stale removal accepted", err)
	}
	if err := packs.Remove(ctx, id, reapproved.Revision, check); err != nil {
		t.Fatal(err)
	}
	list("available")
}
