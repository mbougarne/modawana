package paypal

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/store"
	"github.com/mbougarne/modawana/backend/internal/webhook"
)

type sandboxTransport struct {
	t              *testing.T
	mu             sync.Mutex
	op             string
	paid           bool
	approved       bool
	mismatch       string
	creates        int
	captures       int
	verifyCalls    int
	refunds        map[string]refundResult
	refundRequests int
	failRefund     bool
}

func (f *sandboxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, value any) (*http.Response, error) {
		raw, _ := json.Marshal(value)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	}
	if r.URL.Scheme != "https" || r.URL.Host != "api-m.sandbox.paypal.com" {
		f.t.Fatalf("wrong PayPal environment: %s", r.URL.Host)
	}
	if r.URL.Path == "/v1/oauth2/token" {
		id, secret, ok := r.BasicAuth()
		if !ok || id != "fixture-client" || secret != "fixture-secret" {
			f.t.Fatal("unexpected simulated OAuth credentials")
		}
		return reply(200, map[string]any{"access_token": "fixture-access", "expires_in": 300})
	}
	if r.Header.Get("Authorization") != "Bearer fixture-access" {
		f.t.Fatal("gateway sent the wrong authorization")
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/v2/checkout/orders":
		f.creates++
		var in struct {
			Units []struct {
				Reference string `json:"reference_id"`
				Custom    string `json:"custom_id"`
				Amount    money  `json:"amount"`
				Shipping  struct {
					Address map[string]string `json:"address"`
				} `json:"shipping"`
			} `json:"purchase_units"`
			Payment struct {
				PayPal struct {
					Context map[string]string `json:"experience_context"`
				} `json:"paypal"`
			} `json:"payment_source"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Units) != 1 || in.Units[0].Reference != in.Units[0].Custom ||
			in.Units[0].Reference != r.Header.Get("PayPal-Request-Id") ||
			in.Units[0].Amount != (money{"USD", "12.50"}) || in.Units[0].Shipping.Address["country_code"] != "US" ||
			!strings.Contains(in.Payment.PayPal.Context["return_url"], "/en/store/checkout/cart?order=") ||
			in.Payment.PayPal.Context["shipping_preference"] != "SET_PROVIDED_ADDRESS" {
			f.t.Fatal("local price, shipping snapshot or provider binding did not reach PayPal")
		}
		f.op = in.Units[0].Reference
		return reply(201, order{ID: "ORDER123", Links: []link{{Rel: "payer-action", Href: "https://www.sandbox.paypal.com/checkoutnow?token=ORDER123"}}})
	case r.Method == "GET" && r.URL.Path == "/v2/checkout/orders/ORDER123":
		var u unit
		u.Reference, u.Custom, u.Amount, u.Payee.Merchant = f.op, f.op, money{"USD", "12.50"}, "TESTMERCHANT2"
		out := order{ID: "ORDER123", Intent: "CAPTURE", State: "CREATED"}
		switch f.mismatch {
		case "merchant":
			u.Payee.Merchant = "DIFFERENTMERCHANT"
		case "reference":
			u.Custom = "different-custom-id"
		case "amount":
			u.Amount.Value = "12.51"
		case "currency":
			u.Amount.Currency = "CAD"
		}
		if f.approved {
			out.State = "APPROVED"
		}
		if f.paid {
			out.State = "COMPLETED"
			u.Payments.Captures = []capture{{ID: "CAPTURE123", Amount: money{"USD", "12.50"}, State: "COMPLETED", Final: true}}
		}
		out.Units = []unit{u}
		return reply(200, out)
	case r.Method == "POST" && r.URL.Path == "/v2/checkout/orders/ORDER123/capture":
		if r.Header.Get("PayPal-Request-Id") != "cap-"+f.op {
			f.t.Fatal("capture has no stable effect ID")
		}
		f.captures++
		f.paid = true // The client never receives this successful provider outcome.
		return nil, errors.New("simulated timeout after capture")
	case r.Method == "POST" && r.URL.Path == "/v2/payments/captures/CAPTURE123/refund":
		var in struct {
			Invoice string `json:"invoice_id"`
			Amount  money  `json:"amount"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || "ref-"+in.Invoice != r.Header.Get("PayPal-Request-Id") || in.Amount.Currency != "USD" {
			f.t.Fatal("refund missed its stable binding")
		}
		f.refundRequests++
		out, exists := f.refunds[in.Invoice]
		if !exists {
			out = refundResult{ID: "REF-" + in.Invoice, Invoice: in.Invoice, State: "COMPLETED", Amount: in.Amount,
				Links: []link{{Rel: "up", Href: "https://api.sandbox.paypal.com/v2/payments/captures/CAPTURE123"}}}
			f.refunds[in.Invoice] = out
		}
		if f.failRefund {
			f.failRefund = false
			return nil, errors.New("simulated timeout after refund")
		}
		return reply(201, out)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v2/payments/refunds/REF-"):
		id := strings.TrimPrefix(r.URL.Path, "/v2/payments/refunds/REF-")
		return reply(200, f.refunds[id])
	case r.Method == "POST" && r.URL.Path == "/v1/notifications/verify-webhook-signature":
		f.verifyCalls++
		var in struct {
			ID        string `json:"webhook_id"`
			Signature string `json:"transmission_sig"`
			Event     event  `json:"webhook_event"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.ID != "WEBHOOK123" || in.Event.ID == "" {
			f.t.Fatal("verification request was not bound to the configured webhook")
		}
		status := "FAILURE"
		if in.Signature == base64.StdEncoding.EncodeToString(make([]byte, 256)) {
			status = "SUCCESS"
		}
		return reply(200, map[string]string{"verification_status": status})
	default:
		f.t.Fatalf("unexpected provider call: %s %s", r.Method, r.URL.Path)
	}
	return nil, errors.New("unexpected provider call")
}

const merchantConfig = `{"active":"sandbox-store","connections":[{"id":"sandbox-store","mode":"sandbox","merchant_id":"TESTMERCHANT2","client_id":"fixture-client","client_secret_file":%s,"webhook_id":"WEBHOOK123"}]}`
const guest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const buyerOrderKey = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func setupPayPal(t *testing.T) (*Service, *sandboxTransport, store.OrderReceipt, config.Config, string) {
	t.Helper()
	ctx := t.Context()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	dbpool, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbpool.Close() })
	catalog := &store.Service{DB: dbpool, PaymentAmount: Payable}
	filename := filepath.Join(t.TempDir(), "secret")
	if err = os.WriteFile(filename, []byte("fixture-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(filename)
	configText := strings.Replace(merchantConfig, "%s", string(raw), 1)
	cfg := config.Config{DataRoot: root, BaseURL: "https://shop.example"}
	fake := &sandboxTransport{t: t, refunds: map[string]refundResult{}}
	service, err := open(ctx, catalog, cfg, configText, nil, &http.Client{Transport: fake})
	if err != nil {
		t.Fatal(err)
	}
	catalog.PaymentConnection = service.Active
	if _, err = catalog.SaveSettings(ctx, store.SettingsInput{Version: 1, Enabled: true, Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveCheckoutSettings(ctx, store.CheckoutSettings{Version: 1, Enabled: true, Zones: []store.ShippingZone{{Name: "US", Countries: []string{"US"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveDraft(ctx, "mug", "en", store.DraftInput{Title: "Mug", Slug: "mug", Description: "Ceramic mug"}); err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveVariant(ctx, "mug", "MUG", store.VariantInput{PriceMinor: 1250, Active: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.Publication(ctx, "mug", "en", 1, true); err != nil {
		t.Fatal(err)
	}
	adjustment, _ := db.NewID()
	if _, err = catalog.Adjust(ctx, "owner", "MUG", store.AdjustmentInput{ID: adjustment, Delta: 1, Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	in := store.QuoteInput{Locale: "en", Country: "US", Items: []store.CartItem{{SKU: "MUG", Quantity: 1}}}
	quote, err := catalog.Quote(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := catalog.CreateOrder(ctx, guest, store.OrderInput{QuoteInput: in, Key: buyerOrderKey, QuoteFingerprint: quote.Fingerprint,
		Customer: store.Customer{Name: "Guest Customer", Email: "test@example.test", Address: "1 Shop Road", City: "Portland", Region: "OR", PostalCode: "97201"}})
	if err != nil {
		t.Fatal(err)
	}
	return service, fake, receipt, cfg, configText
}

func eventHeaders() http.Header {
	h := make(http.Header)
	h.Set("PayPal-Auth-Algo", "SHA256withRSA")
	h.Set("PayPal-Cert-Url", "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-TEST")
	h.Set("PayPal-Transmission-Id", "TX-TEST")
	h.Set("PayPal-Transmission-Sig", base64.StdEncoding.EncodeToString(make([]byte, 256)))
	h.Set("PayPal-Transmission-Time", time.Now().UTC().Format(time.RFC3339))
	return h
}

func retestEvent(id, kind, resource string) []byte {
	raw, _ := json.Marshal(map[string]any{"id": id, "event_type": kind, "resource": map[string]any{
		"id": resource, "supplementary_data": map[string]any{"related_ids": map[string]any{"order_id": "ORDER123"}},
	}})
	return raw
}

func TestPayPalApprovalVerifiedCaptureAndWebhookOrder(t *testing.T) {
	s, fake, receipt, cfg, raw := setupPayPal(t)
	ctx := t.Context()
	if _, err := s.Approval(ctx, "cccccccccccccccccccccccccccccccc", receipt.Reference); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign browser acquired PayPal approval", err)
	}
	for range 2 {
		if link, err := s.Approval(ctx, guest, receipt.Reference); err != nil || !strings.HasSuffix(link, "token=ORDER123") {
			t.Fatal(link, err)
		}
	}
	if fake.creates != 1 {
		t.Fatal(fake.creates)
	}
	if status, err := s.Finish(ctx, guest, receipt.Reference, true); err != nil || status.Payment != "pending" || fake.captures != 0 {
		t.Fatal(status, err)
	}
	fake.approved = true
	for _, field := range []string{"merchant", "reference", "currency", "amount"} {
		fake.mismatch = field
		if _, err := s.Finish(ctx, guest, receipt.Reference, true); !errors.Is(err, ErrMismatch) || fake.captures != 0 {
			t.Fatal(field, err)
		}
	}
	fake.mismatch = ""
	s.active = "" // retain credentials for previous operations
	if status, err := s.Finish(ctx, guest, receipt.Reference, true); err != nil || status.Payment != "pending" || fake.captures != 0 {
		t.Fatal(status, err)
	}
	if _, err := s.store.CheckoutOptions(ctx, "en"); !errors.Is(err, store.ErrCheckoutClosed) {
		t.Fatal(err)
	}
	s.active = "sandbox-store"
	status, err := s.Finish(ctx, guest, receipt.Reference, true)
	if err != nil || status.Payment != "paid" || fake.captures != 1 {
		t.Fatal(status, err)
	}
	restarted, err := open(ctx, s.store, cfg, raw, nil, &http.Client{Transport: fake})
	if err != nil {
		t.Fatal(err)
	}
	bad := eventHeaders()
	bad.Set("PayPal-Transmission-Sig", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 256))))
	e := retestEvent("EVENT-ONE", "PAYMENT.CAPTURE.COMPLETED", "CAPTURE123")
	if err = restarted.Accept(ctx, "sandbox-store", bad, e); !errors.Is(err, ErrSignature) {
		t.Fatal(err)
	}
	rejected := eventHeaders()
	rejected.Set("PayPal-Cert-Url", "https://untrusted.example/v1/notifications/certs/CERT-TEST")
	calls := fake.verifyCalls
	if err = restarted.Accept(ctx, "sandbox-store", rejected, e); !errors.Is(err, ErrSignature) || fake.verifyCalls != calls {
		t.Fatal("unexpected verification of an untrusted cert origin", err)
	}
	for range 2 {
		if err = restarted.Accept(ctx, "sandbox-store", eventHeaders(), e); err != nil {
			t.Fatal(err)
		}
	}
	if err = restarted.Accept(ctx, "sandbox-store", eventHeaders(), retestEvent("EVENT-ONE", "PAYMENT.CAPTURE.DENIED", "CAPTURE123")); !errors.Is(err, webhook.ErrConflict) {
		t.Fatal(err)
	}
	if err = restarted.Accept(ctx, "sandbox-store", eventHeaders(), retestEvent("EVENT-TWO", "PAYMENT.CAPTURE.PENDING", "CAPTURE123")); err != nil {
		t.Fatal(err)
	}
	if err = restarted.processReceipt(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := restarted.Receipts(ctx, 0)
	if err != nil || len(items) != 2 || items[0].State != "applied" || items[1].State != "applied" {
		t.Fatal(items, err)
	}
	var count int
	if err = s.store.DB.QueryRow(`SELECT count(*) FROM outbox_events WHERE name='order.paid' AND source_id=?`, receipt.Reference).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = open(ctx, s.store, cfg, strings.Replace(raw, "TESTMERCHANT2", "TESTMERCHANT3", 1), nil, &http.Client{Transport: fake}); !errors.Is(err, ErrMismatch) {
		t.Fatal("connection id can be reassigned", err)
	}
}

func TestPayPalRefundFullPartialAndTimeout(t *testing.T) {
	for _, firstAmount := range []int64{450, 1250} {
		t.Run(map[int64]string{450: "partial and remainder", 1250: "full"}[firstAmount], func(t *testing.T) {
			s, fake, receipt, _, _ := setupPayPal(t)
			ctx := t.Context()
			if _, err := s.Approval(ctx, guest, receipt.Reference); err != nil {
				t.Fatal(err)
			}
			fake.approved = true
			if _, err := s.Finish(ctx, guest, receipt.Reference, true); err != nil {
				t.Fatal(err)
			}
			fresh := func(*sql.Tx) error { return nil }
			denied := errors.New("owner authorization failed")
			in := store.RefundInput{Key: "cccccccccccccccccccccccccccccccc", AmountMinor: firstAmount}
			if _, err := s.Refund(ctx, receipt.Reference, in, func(*sql.Tx) error { return denied }); !errors.Is(err, denied) {
				t.Fatal(err)
			}
			fake.failRefund = true
			first, err := s.Refund(ctx, receipt.Reference, in, fresh)
			if !errors.Is(err, ErrUnavailable) || first.ID == "" || first.State != "pending" {
				t.Fatal(first, err)
			}
			if in.AmountMinor < 1250 {
				if _, err = s.Refund(ctx, receipt.Reference, store.RefundInput{Key: "dddddddddddddddddddddddddddddddd", AmountMinor: 1250}, fresh); !errors.Is(err, store.ErrInvalid) {
					t.Fatal(err)
				}
			}
			// A verified callback can recover the operation before a merchant
			// retries the request whose response was lost.
			event := retestEvent("REFUND-EVENT", "PAYMENT.CAPTURE.REFUNDED", "REF-"+first.ID)
			if err = s.Accept(ctx, "sandbox-store", eventHeaders(), event); err != nil {
				t.Fatal(err)
			}
			if err = s.processReceipt(ctx); err != nil {
				t.Fatal(err)
			}
			replay, err := s.ResumeRefund(ctx, first.ID, fresh)
			if err != nil || replay.ID != first.ID || replay.State != "complete" || fake.refundRequests != 1 {
				t.Fatal(replay, err, fake.refundRequests)
			}
			if in.AmountMinor < 1250 {
				status, err := s.store.GuestOrder(ctx, guest, receipt.Reference)
				if err != nil || status.Refund != "partial" {
					t.Fatal(status, err)
				}
				if _, err = s.Refund(ctx, receipt.Reference, store.RefundInput{Key: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", AmountMinor: 1250 - firstAmount}, fresh); err != nil {
					t.Fatal(err)
				}
			}
			status, err := s.store.GuestOrder(ctx, guest, receipt.Reference)
			if err != nil || status.Refund != "refunded" || status.Payment != "paid" {
				t.Fatal(status, err)
			}
			draft, err := s.store.Draft(ctx, "mug", "en")
			if err != nil || draft.Variants[0].Stock != 0 {
				t.Fatal(draft, err)
			}
		})
	}
}
