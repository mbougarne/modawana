package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/store"
)

const checkoutScript = "/assets/store-checkout.1034567890abcdef.js"

func TestCheckoutHTTPPrivacyAndGuestFlow(t *testing.T) {
	c, owner := newThemeContent(t)
	service := &store.Service{DB: c.DB}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(c.DB, cfg)
	host := extension.NewHost(nil)
	site := &siteconfig.Service{DB: c.DB, Files: c.Files}
	d := &Domain{Store: service, Content: c, Auth: a, Extensions: host, SiteConfig: site}
	for _, m := range d.Builtins() {
		if err := host.Install(m); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Domain: d, SiteConfig: site, Auth: a, Extensions: host, Assets: assets, Config: cfg})
	ownerLogin, manager, editor := storeFixtureLogins(t, c, owner)
	anon := storeLogin{}
	request := func(method, path string, login storeLogin, origin string, payload any, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(payload)
		r := httptest.NewRequest(method, cfg.BaseURL+path, bytes.NewReader(raw))
		r.Header.Set("Accept", "text/html")
		if payload != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", storeDigest("csrf:"+login.token))
		}
		if login.cookie != "" {
			r.AddCookie(&http.Cookie{Name: login.cookie, Value: login.token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s got %d, want %d. %.300s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	root := checkoutAPI + "en"
	request("GET", root+"/options", anon, "", nil, 404)
	request("GET", ownerStore+"/orders", anon, "", nil, 401)
	request("GET", staffStore+"/orders", editor, "", nil, 403)
	request("GET", staffStore+"/checkout", manager, "", nil, 403)
	ctx := t.Context()
	if _, err = service.SaveSettings(ctx, store.SettingsInput{Version: 1, Enabled: true, Currency: "MAD"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveDraft(ctx, "cup", "en", store.DraftInput{Slug: "checkout", Title: "A Cup", Description: "A ceramic cup."}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveVariant(ctx, "cup", "CUP", store.VariantInput{PriceMinor: 1000, Active: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Publication(ctx, "cup", "en", 1, true); err != nil {
		t.Fatal(err)
	}
	id, _ := db.NewID()
	if _, err = service.Adjust(ctx, owner, "CUP", store.AdjustmentInput{ID: id, Delta: 1, Reason: "Fixture"}); err != nil {
		t.Fatal(err)
	}
	input := store.CheckoutSettings{Version: 1, Enabled: true, Zones: []store.ShippingZone{{Name: "Morocco", Countries: []string{"MA"}}}}
	request("PUT", staffStore+"/checkout", manager, cfg.BaseURL, input, 403)
	request("PUT", ownerStore+"/checkout", ownerLogin, "", input, 403)
	request("PUT", ownerStore+"/checkout", ownerLogin, cfg.BaseURL, input, 200)
	product := request("GET", "/en/store/checkout", anon, "", nil, 200)
	if !strings.Contains(product.Body.String(), `data-store-add="CUP"`) || !strings.Contains(product.Body.String(), checkoutScript) {
		t.Fatal("product slug checkout lost cart controls")
	}
	page := request("GET", "/ar/store/checkout/cart", anon, "", nil, 200)
	if !strings.Contains(page.Body.String(), `lang="ar" dir="rtl"`) || !strings.Contains(page.Body.String(), "data-store-checkout") ||
		page.Header().Get("X-Robots-Tag") != "noindex" || page.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("checkout should be localized, private, and excluded from search", page.Header())
	}
	request("GET", checkoutAPI+"es/options", anon, "", nil, 404)
	session := request("GET", root+"/options", anon, "", nil, 200)
	cookies := session.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != checkoutCookie || !cookies[0].HttpOnly || !cookies[0].Secure ||
		cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
		t.Fatal("expected a scope-protected guest cookie")
	}
	guest := storeLogin{checkoutCookie, cookies[0].Value}
	quoteIn := store.QuoteInput{Locale: "en", Country: "MA", Items: []store.CartItem{{SKU: "CUP", Quantity: 1}}}
	request("POST", root+"/quote", guest, "https://other.test", quoteIn, 403)
	request("POST", root+"/quote", anon, cfg.BaseURL, quoteIn, 403)
	quoteReply := request("POST", root+"/quote", guest, cfg.BaseURL, quoteIn, 200)
	var quote store.Quote
	if err = json.Unmarshal(quoteReply.Body.Bytes(), &quote); err != nil || quote.TotalMinor != 1000 {
		t.Fatal(quote, err)
	}
	key, _ := db.NewID()
	customer := store.Customer{Name: "Fixture Guest", Email: "guest@example.test", Address: "Fixture Lane", City: "Rabat"}
	in := store.OrderInput{QuoteInput: quoteIn, Customer: customer, Key: key, QuoteFingerprint: quote.Fingerprint}
	placed := request("POST", root+"/orders", guest, cfg.BaseURL, in, 200)
	var receipt store.OrderReceipt
	if err = json.Unmarshal(placed.Body.Bytes(), &receipt); err != nil || receipt.Reference == "" {
		t.Fatal(receipt, err)
	}
	if retry := request("POST", root+"/orders", guest, cfg.BaseURL, in, 200); placed.Body.String() != retry.Body.String() {
		t.Fatal("the retry did not preserve the exact receipt")
	}
	in.Customer.City = "Another city"
	request("POST", root+"/orders", guest, cfg.BaseURL, in, 409)
	other := storeLogin{checkoutCookie, strings.Repeat("f", 32)}
	path := root + "/orders/" + receipt.Reference
	attempt := root + "/attempts/" + key
	request("GET", path, other, "", nil, 404)
	request("GET", attempt, other, "", nil, 404)
	if w := request("GET", attempt, guest, "", nil, 200); w.Body.String() != placed.Body.String() {
		t.Fatal("attempt lookup did not recover the exact receipt")
	}
	lookup := request("GET", path, guest, "", nil, 200)
	if strings.Contains(lookup.Body.String(), customer.Email) || strings.Contains(lookup.Body.String(), customer.Address) ||
		strings.Contains(lookup.Body.String(), guest.token) {
		t.Fatal("guest projection exposed private records")
	}
	private := request("GET", staffStore+"/orders/"+receipt.Reference, manager, "", nil, 200)
	if !strings.Contains(private.Body.String(), customer.Address) {
		t.Fatal("authorized merchant did not receive shipping details")
	}
	request("GET", staffStore+"/orders/"+receipt.Reference, editor, "", nil, 403)
	input.Version, input.Enabled = 2, false
	request("PUT", ownerStore+"/checkout", ownerLogin, cfg.BaseURL, input, 200)
	request("GET", root+"/options", guest, "", nil, 404)
	request("GET", path, guest, "", nil, 200)
}
