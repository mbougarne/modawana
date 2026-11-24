package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/store"
)

type storeLogin struct{ cookie, token string }

func storeFixtureLogins(t *testing.T, c *content.Service, owner string) (storeLogin, storeLogin, storeLogin) {
	t.Helper()
	ownerLogin := storeLogin{"__Host-owner", strings.Repeat("a", 43)}
	manager := storeLogin{"__Host-staff", strings.Repeat("b", 43)}
	editor := storeLogin{"__Host-staff", strings.Repeat("c", 43)}
	now := time.Now()
	sid, _ := db.NewID()
	if _, err := c.DB.Exec(`INSERT INTO sessions
		(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at) VALUES(?,?,?,'owner','passkey',1,?,?,?)`,
		sid, owner, storeDigest(ownerLogin.token), db.UTC(now), db.UTC(now.Add(time.Hour)), db.UTC(now)); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		role  string
		login storeLogin
	}{{"store_manager", manager}, {"editor", editor}} {
		if _, err := c.DB.Exec("INSERT INTO staff_members(id,owner_id,name,role,credential_id,credential) VALUES(?,?,?,?,?,'{}')",
			item.role, owner, "Catalog user", item.role, item.role+"-key"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.DB.Exec("INSERT INTO staff_sessions VALUES(?,?,?)", storeDigest(item.login.token), item.role, db.UTC(now.Add(time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	return ownerLogin, manager, editor
}

func storeDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func TestStoreRolesAndPublicRoutesSharePublication(t *testing.T) {
	c, owner := newThemeContent(t)
	s := &store.Service{DB: c.DB}
	c.Access.StoreImage = s.ImageVisible
	settings := &siteconfig.Service{DB: c.DB, Files: c.Files}
	ownerLogin, manager, editor := storeFixtureLogins(t, c, owner)
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(c.DB, cfg)
	host := extension.NewHost(nil)
	d := &Domain{Store: s, Content: c, Media: media.New(c.DB, c.Files, c.Access), Auth: a, SiteConfig: settings, Extensions: host}
	for _, module := range d.Builtins() {
		if err := host.Install(module); err != nil {
			t.Fatal(err)
		}
	}
	h := New(Options{Assets: assets, Config: cfg, SiteConfig: settings, Auth: a, Domain: d, Extensions: host})
	request := func(method, path string, body io.Reader, mime string, login storeLogin, withCSRF bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, cfg.BaseURL+path, body)
		req.Header.Set("Accept", "text/html")
		if mime != "" {
			req.Header.Set("Content-Type", mime)
		}
		if login.cookie != "" {
			req.AddCookie(&http.Cookie{Name: login.cookie, Value: login.token})
		}
		if withCSRF {
			req.Header.Set("X-CSRF-Token", storeDigest("csrf:"+login.token))
			req.Header.Set("Origin", cfg.BaseURL)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s got %d want %d: %.300s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	anon := storeLogin{}
	get := func(path string, login storeLogin, want int) *httptest.ResponseRecorder {
		t.Helper()
		return request("GET", path, nil, "", login, false, want)
	}
	put := func(path string, in any, login storeLogin, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		return request("PUT", path, bytes.NewReader(raw), "application/json", login, true, want)
	}
	get("/en/store", anon, 404)
	get(staffStore+"?locale=en", editor, 403)
	get(staffStore+"?locale=en", manager, 200)
	get(ownerStore+"?locale=en", ownerLogin, 200)
	get(ownerStore+"?locale=en", manager, 401)
	get(staffStore+"?locale=en", anon, 401)
	enable := store.SettingsInput{Version: 1, Enabled: true, Currency: "MAD"}
	put(staffStore+"/settings", enable, manager, 403)
	put(ownerStore+"/settings", enable, ownerLogin, 200)
	get("/en/store", anon, 200)
	category := store.Category{Key: "desk", Locale: "en", Title: "Desk goods"}
	put(staffStore+"/categories", category, manager, 200)
	// Upload through the real private catalog image endpoint.
	var payload bytes.Buffer
	mw := multipart.NewWriter(&payload)
	part, err := mw.CreateFormFile("file", "sample.png")
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(part, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	if err = mw.Close(); err != nil {
		t.Fatal(err)
	}
	uploaded := request("POST", staffStore+"/images", &payload, mw.FormDataContentType(), manager, true, 201)
	var imageRecord media.Record
	if err = json.Unmarshal(uploaded.Body.Bytes(), &imageRecord); err != nil || imageRecord.ID == "" {
		t.Fatal("image response", err)
	}
	privateImage := staffStore + "/images/" + imageRecord.ID
	get(privateImage, manager, 200)
	get(privateImage, editor, 403)
	get(imageRecord.URL, anon, 404)
	en := store.DraftInput{Slug: "lamp", Title: "Desk lamp", Description: "A lamp for the desk",
		Category: "desk", Images: []store.Image{{ID: imageRecord.ID, Alt: "Lamp on a desk"}}}
	missingCSRF, _ := json.Marshal(en)
	request("PUT", staffStore+"/products/lamp/en", bytes.NewReader(missingCSRF), "application/json", manager, false, 403)
	put(staffStore+"/products/lamp/en", en, manager, 200)
	put(staffStore+"/products/lamp/variants/LAMP-A", store.VariantInput{
		Labels: map[string]string{"en": "Natural", "ar": "طبيعي"}, PriceMinor: 12345, Active: true}, manager, 200)
	get("/en/store/lamp", anon, 404)
	get("/api/v1/public/page?path=%2Fen%2Fstore%2Flamp", anon, 404)
	if matches := get("/api/v1/public/search?locale=en&kind=product&q=lamp", anon, 200).Body.String(); strings.Contains(matches, "Desk lamp") {
		t.Fatal("draft leaked into search")
	}
	put(staffStore+"/products/lamp/en/publication", map[string]any{"version": 1, "published": true}, manager, 200)
	get(imageRecord.URL, anon, 200)
	for _, body := range []string{
		get("/en/store/lamp", anon, 200).Body.String(),
		get("/api/v1/public/page?path=%2Fen%2Fstore%2Flamp", anon, 200).Body.String(),
		get("/en/search?q=lamp", anon, 200).Body.String(),
	} {
		if !strings.Contains(body, "Desk lamp") || !strings.Contains(body, "/en/store/lamp") {
			t.Fatal("missing public product or canonical path")
		}
	}
	html := get("/en/store/lamp", anon, 200).Body.String()
	for _, text := range []string{`"@type":"Product"`, "MAD 123.45", imageRecord.URL, "Lamp on a desk"} {
		if !strings.Contains(html, text) {
			t.Fatal("missing product metadata or display", text)
		}
	}
	en.Version, en.Title, en.Slug = 2, "Unaired replacement", "hidden-lamp"
	put(ownerStore+"/products/lamp/en", en, ownerLogin, 200)
	get("/en/store/hidden-lamp", anon, 404)
	if body := get("/en/store/lamp", anon, 200).Body.String(); strings.Contains(body, en.Title) {
		t.Fatal("new draft replaced published copy")
	}
	ar := store.DraftInput{Slug: "مصباح", Title: "مصباح المكتب", Description: "وصف المصباح"}
	put(staffStore+"/products/lamp/ar", ar, manager, 200)
	put(staffStore+"/products/lamp/ar/publication", map[string]any{"version": 1, "published": true}, manager, 200)
	if body := get("/ar/store/"+url.PathEscape(ar.Slug), anon, 200).Body.String(); !strings.Contains(body, ar.Title) || !strings.Contains(body, `dir="rtl"`) {
		t.Fatal("localized product not rendered")
	}
	es := store.DraftInput{Slug: "lampara", Title: "Lámpara", Description: "Descripción pública"}
	put(staffStore+"/products/lamp/es", es, manager, 200)
	put(staffStore+"/products/lamp/es/publication", map[string]any{"version": 1, "published": true}, manager, 200)
	get("/es/store/lampara", anon, 404)
	get("/api/v1/public/search?locale=es&kind=product", anon, 404)
	put(staffStore+"/products/lamp/en/publication", map[string]any{"version": 3, "published": false}, manager, 200)
	get("/en/store/lamp", anon, 404)
	get(imageRecord.URL, anon, 404)
	get(privateImage, manager, 200)

	adjustment := store.AdjustmentInput{ID: strings.Repeat("f", 32), Delta: 4, Reason: "Delivery"}
	raw, _ := json.Marshal(adjustment)
	w := request("POST", staffStore+"/inventory/LAMP-A", bytes.NewReader(raw), "application/json", manager, true, 200)
	var receipt store.Adjustment
	if err = json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Actor != "staff:store_manager" || receipt.After != 4 {
		t.Fatal(receipt, err)
	}
	if _, err = c.DB.Exec("UPDATE staff_members SET revoked_at=? WHERE id='store_manager'", db.UTC(time.Now())); err != nil {
		t.Fatal(err)
	}
	get(staffStore+"?locale=en", manager, 401)
	put(ownerStore+"/settings", store.SettingsInput{Version: 2, Currency: "MAD"}, ownerLogin, 200)
	get("/ar/store", anon, 404)
}
