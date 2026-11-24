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
	"github.com/mbougarne/modawana/backend/internal/store"
)

func TestMerchantOrdersHTTPCapabilitiesAndReceipt(t *testing.T) {
	c, owner := newThemeContent(t)
	ctx := t.Context()
	cfg := config.Config{BaseURL: "https://example.test"}
	service := &store.Service{DB: c.DB, PaymentConnection: func(string) string { return "fixture" }}
	if _, err := c.DB.ExecContext(ctx, `INSERT INTO store_paypal_connections(id,mode,merchant,client_hash) VALUES('fixture','sandbox','fixture','fixture')`); err != nil {
		t.Fatal(err)
	}
	authService := auth.New(c.DB, cfg)
	host := extension.NewHost(nil)
	d := &Domain{Store: service, Content: c, Auth: authService, Extensions: host}
	for _, module := range d.Builtins() {
		if err := host.Install(module); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(Options{Domain: d, Auth: authService, Config: cfg, Extensions: host})
	ownerLogin, manager, editor := storeFixtureLogins(t, c, owner)
	request := func(method, path string, login storeLogin, csrf bool, input any, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(input)
		r := httptest.NewRequest(method, cfg.BaseURL+path, bytes.NewReader(raw))
		if input != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if login.cookie != "" {
			r.AddCookie(&http.Cookie{Name: login.cookie, Value: login.token})
		}
		if csrf {
			r.Header.Set("Origin", cfg.BaseURL)
			r.Header.Set("X-CSRF-Token", storeDigest("csrf:"+login.token))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s returned %d, wanted %d. %.300s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}

	if _, err := service.SaveSettings(ctx, store.SettingsInput{Version: 1, Enabled: true, Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveCheckoutSettings(ctx, store.CheckoutSettings{Version: 1, Enabled: true,
		Zones: []store.ShippingZone{{Name: "Local", Countries: []string{"US"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveDraft(ctx, "cup", "en", store.DraftInput{Slug: "cup", Title: "<Cup>", Description: "A ceramic cup."}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveVariant(ctx, "cup", "CUP", store.VariantInput{PriceMinor: 1000, Active: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publication(ctx, "cup", "en", 1, true); err != nil {
		t.Fatal(err)
	}
	key, _ := db.NewID()
	if _, err := service.Adjust(ctx, owner, "CUP", store.AdjustmentInput{ID: key, Delta: 1, Reason: "Fixture stock"}); err != nil {
		t.Fatal(err)
	}
	guest := storeLogin{checkoutCookie, strings.Repeat("e", 32)}
	other := storeLogin{checkoutCookie, strings.Repeat("f", 32)}
	in := store.QuoteInput{Locale: "en", Country: "US", Items: []store.CartItem{{SKU: "CUP", Quantity: 1}}}
	quote, err := service.Quote(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	key, _ = db.NewID()
	customer := store.Customer{Name: "Private Customer", Email: "test@example.test", Address: "Private Lane", City: "Portland"}
	receipt, err := service.CreateOrder(ctx, guest.token, store.OrderInput{QuoteInput: in, Customer: customer, Key: key, QuoteFingerprint: quote.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	id := receipt.Reference
	guestReceipt := checkoutAPI + "ar/orders/" + id + "/receipt"
	staffOrder := staffStore + "/orders/" + id
	anon := storeLogin{}
	request("GET", guestReceipt, guest, false, nil, 404)
	request("GET", staffStore+"/orders/export", editor, false, nil, 403)
	request("GET", staffStore+"/orders/export", anon, false, nil, 401)
	request("GET", ownerStore+"/orders/export", manager, false, nil, 401)
	key, _ = db.NewID()
	cancel := store.MerchantInput{ID: key, Reason: "Paid customer request"}
	request("POST", staffOrder+"/cancel", editor, true, cancel, 403)
	request("POST", staffOrder+"/cancel", manager, false, cancel, 403)

	p, err := service.GuestPurchase(ctx, guest.token, id)
	if err != nil {
		t.Fatal(err)
	}
	err = service.BindApproval(ctx, p, store.PaymentApproval{ProviderOrder: "FIXTURE-ORDER", ApprovalURL: "https://www.sandbox.paypal.com/checkoutnow?token=FIXTURE-ORDER"})
	if err != nil {
		t.Fatal(err)
	}
	p, err = service.GuestPurchase(ctx, guest.token, id)
	if err != nil {
		t.Fatal(err)
	}
	op, err := service.StartProviderCapture(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.RecordPayment(ctx, store.PaymentEvidence{Operation: op, CaptureID: "FIXTURE-CAPTURE"}); err != nil {
		t.Fatal(err)
	}
	request("POST", staffOrder+"/cancel", manager, true, cancel, 200)
	snapshot, err := service.MerchantOrder(ctx, id)
	if err != nil || snapshot.Status != "cancelled" || snapshot.Payment != "paid" || snapshot.Refund != "none" || snapshot.Reservation != "sold" {
		t.Fatal(snapshot, err)
	}
	if snapshot.Events[len(snapshot.Events)-1].Actor != "staff:store_manager" {
		t.Fatal(snapshot.Events)
	}
	w := request("GET", guestReceipt, guest, false, nil, 200)
	if !strings.Contains(w.Body.String(), `lang="ar" dir="rtl"`) || !strings.Contains(w.Body.String(), "&lt;Cup&gt;") ||
		strings.Contains(w.Body.String(), customer.Address) || strings.Contains(w.Body.String(), "FIXTURE-CAPTURE") ||
		w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "nonce-") {
		t.Fatal(w.Body.String(), w.Header())
	}
	request("GET", guestReceipt, other, false, nil, 404)
	request("GET", staffOrder+"/receipt", editor, false, nil, 403)
	request("GET", staffOrder+"/receipt", manager, false, nil, 200)
	request("GET", ownerStore+"/orders/"+id+"/receipt", ownerLogin, false, nil, 200)
	csv := request("GET", staffStore+"/orders/export", manager, false, nil, 200)
	if csv.Header().Get("Content-Disposition") != `attachment; filename="modawana-orders.csv"` || !strings.Contains(csv.Body.String(), customer.Email) {
		t.Fatal(csv.Header(), csv.Body.String())
	}
	if _, err = c.DB.ExecContext(ctx, `UPDATE staff_members SET revoked_at=? WHERE role='store_manager'`, db.UTC(authService.Now())); err != nil {
		t.Fatal(err)
	}
	request("GET", staffStore+"/orders/export", manager, false, nil, 401)
	request("POST", staffOrder+"/cancel", manager, true, cancel, 401)
}
