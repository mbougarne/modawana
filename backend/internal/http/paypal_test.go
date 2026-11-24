package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/paypal"
	"github.com/mbougarne/modawana/backend/internal/store"
)

func TestPayPalHTTPAuthorizationAndNoConnection(t *testing.T) {
	c, owner := newThemeContent(t)
	ctx := t.Context()
	catalog := &store.Service{DB: c.DB}
	cfg := config.Config{BaseURL: "https://example.test"}
	payments, err := paypal.New(ctx, catalog, cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog.PaymentConnection = payments.Active
	host := extension.NewHost(nil)
	authService := auth.New(c.DB, cfg)
	d := &Domain{PayPal: payments, Store: catalog, Content: c, Auth: authService, Extensions: host}
	for _, module := range d.Builtins() {
		if err = host.Install(module); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = catalog.SaveSettings(ctx, store.SettingsInput{Version: 1, Enabled: true, Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveCheckoutSettings(ctx, store.CheckoutSettings{Version: 1, Enabled: true, Zones: []store.ShippingZone{{Name: "US", Countries: []string{"US"}}}}); err != nil {
		t.Fatal(err)
	}
	ownerLogin, manager, _ := storeFixtureLogins(t, c, owner)
	handler := New(Options{Domain: d, Auth: authService, Config: cfg, Extensions: host})
	id := strings.Repeat("a", 32)
	request := func(method, path string, login storeLogin, origin string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.BaseURL+path, bytes.NewBufferString("{\"key\":\""+id+"\",\"amount_minor\":100}"))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", storeDigest("csrf:"+login.token))
		}
		if login.cookie != "" {
			r.AddCookie(&http.Cookie{Name: login.cookie, Value: login.token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s returned %d, wanted %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	anon := storeLogin{}
	request("GET", checkoutAPI+"en/options", anon, "", 404)
	request("GET", ownerStore+"/paypal", anon, "", 401)
	if status := request("GET", ownerStore+"/paypal", ownerLogin, "", 200); !strings.Contains(status.Body.String(), `"ready":false`) {
		t.Fatal(status.Body.String())
	}
	request("POST", ownerStore+"/paypal/orders/"+id+"/refunds", manager, cfg.BaseURL, 401)
	request("POST", ownerStore+"/paypal/orders/"+id+"/refunds", ownerLogin, "", 403)
	request("POST", checkoutAPI+"en/orders/"+id+"/paypal/finish", storeLogin{checkoutCookie, id}, "https://untrusted.example", 403)
	request("POST", "/api/v1/webhooks/paypal/unknown", anon, "", 404)
}
