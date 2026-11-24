package store

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

func checkoutFixture(t *testing.T, stock int64) (*Service, QuoteInput) {
	t.Helper()
	s := storeFixture(t)
	ctx := t.Context()
	if _, err := s.SaveSettings(ctx, SettingsInput{Version: 1, Enabled: true, Currency: "MAD"}); err != nil {
		t.Fatal(err)
	}
	cfg := CheckoutSettings{Version: 1, Enabled: true,
		Zones:   []ShippingZone{{Name: "Morocco", Countries: []string{"MA"}, RateMinor: 125, TaxBPS: 1000, TaxShipping: true}},
		Coupons: []Coupon{{Code: "WELCOME", DiscountBPS: 1500}, {Code: "FIXED", DiscountMinor: 10}},
	}
	if _, err := s.SaveCheckoutSettings(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(ctx, "tea", "en", DraftInput{Slug: "tea", Title: "Loose-leaf tea", Description: "Tea"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveVariant(ctx, "tea", "TEA", VariantInput{PriceMinor: 999, Active: true, Labels: map[string]string{"en": "Small"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publication(ctx, "tea", "en", 1, true); err != nil {
		t.Fatal(err)
	}
	id, _ := db.NewID()
	if _, err := s.Adjust(ctx, "owner", "TEA", AdjustmentInput{ID: id, Delta: stock, Reason: "Delivery"}); err != nil {
		t.Fatal(err)
	}
	return s, QuoteInput{Locale: "en", Country: "MA", Coupon: "Welcome", Items: []CartItem{{SKU: "TEA", Quantity: 1}}}
}

func guestCheckout(t *testing.T, s *Service, in QuoteInput) OrderInput {
	t.Helper()
	quote, err := s.Quote(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := db.NewID()
	return OrderInput{QuoteInput: in, Key: id, QuoteFingerprint: quote.Fingerprint,
		Customer: Customer{Name: "Test Buyer", Email: "buyer@example.test", Address: "1 Local Street", City: "Rabat"}}
}

func TestCheckoutLastUnitSnapshotAndReplay(t *testing.T) {
	s, input := checkoutFixture(t, 1)
	ctx := t.Context()
	quote, err := s.Quote(ctx, input)
	if err != nil || quote.Currency != "MAD" || quote.Lines[0].Title != "Loose-leaf tea" ||
		quote.SubtotalMinor != 999 || quote.DiscountMinor != 150 || quote.ShippingMinor != 125 || quote.TaxMinor != 97 || quote.TotalMinor != 1071 {
		t.Fatal(quote, err)
	}
	guest1, guest2 := strings.Repeat("a", 32), strings.Repeat("b", 32)
	in1, in2 := guestCheckout(t, s, input), guestCheckout(t, s, input)
	type result struct {
		guest   string
		in      OrderInput
		receipt OrderReceipt
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, entry := range []result{{guest: guest1, in: in1}, {guest: guest2, in: in2}} {
		wg.Add(1)
		go func(item result) {
			defer wg.Done()
			<-start
			item.receipt, item.err = s.CreateOrder(ctx, item.guest, item.in)
			results <- item
		}(entry)
	}
	close(start)
	wg.Wait()
	close(results)
	var winner, loser result
	for item := range results {
		if item.err == nil {
			winner = item
		} else if errors.Is(item.err, ErrStock) {
			loser = item
		} else {
			t.Fatal(item.err)
		}
	}
	if winner.receipt.Reference == "" || loser.guest == "" {
		t.Fatal("expected one acceptance and one stock rejection")
	}
	draft, err := s.Draft(ctx, "tea", "en")
	if err != nil || draft.Variants[0].Stock != 1 || draft.Variants[0].Reserved != 1 {
		t.Fatal(draft, err)
	}
	public, err := s.PublicProduct(ctx, "en", "tea")
	if err != nil || public.Product.Variants[0].InStock {
		t.Fatal(public, err)
	}
	adjustment, _ := db.NewID()
	if _, err = s.Adjust(ctx, "owner", "TEA", AdjustmentInput{ID: adjustment, Delta: -1, Reason: "Remove unit"}); !errors.Is(err, ErrStock) {
		t.Fatal("manual adjustment consumed a reserved unit", err)
	}
	if _, err = s.GuestOrder(ctx, loser.guest, winner.receipt.Reference); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("another guest could retrieve an order", err)
	}
	if _, err = s.GuestReceipt(ctx, loser.guest, winner.in.Key); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("another guest could look up an attempt", err)
	}
	merchant, err := s.MerchantOrder(ctx, winner.receipt.Reference)
	if err != nil || !reflect.DeepEqual(merchant.Snapshot, quote.Snapshot) || merchant.Customer != winner.in.Customer ||
		merchant.Operation == "" || merchant.Payment != "pending" || merchant.Refund != "none" ||
		merchant.Fulfillment != "unfulfilled" || len(merchant.Events) != 1 {
		t.Fatal(merchant, err)
	}
	if _, err = s.SaveVariant(ctx, "tea", "TEA", VariantInput{Version: 1, PriceMinor: 1500, Active: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSettings(ctx, SettingsInput{Version: 2, Enabled: false, Currency: "MAD"}); err != nil {
		t.Fatal(err)
	}
	if replayed, err := s.CreateOrder(ctx, winner.guest, winner.in); err != nil || replayed != winner.receipt {
		t.Fatal(replayed, err)
	}
	if replayed, err := s.GuestReceipt(ctx, winner.guest, winner.in.Key); err != nil || replayed != winner.receipt {
		t.Fatal(replayed, err)
	}
	winner.in.Customer.City = "Casablanca"
	if _, err = s.CreateOrder(ctx, winner.guest, winner.in); !errors.Is(err, ErrOrderReplay) {
		t.Fatal(err)
	}
	status, err := s.GuestOrder(ctx, winner.guest, winner.receipt.Reference)
	if err != nil || !reflect.DeepEqual(status.Snapshot, quote.Snapshot) {
		t.Fatal(status, err)
	}
	if err = s.expireOrders(ctx, time.Now().Add(reservationLifetime)); err != nil {
		t.Fatal(err)
	}
	draft, err = s.Draft(ctx, "tea", "en")
	if err != nil || draft.Variants[0].Stock != 1 || draft.Variants[0].Reserved != 0 {
		t.Fatal(draft, err)
	}
	status, err = s.GuestOrder(ctx, winner.guest, winner.receipt.Reference)
	if err != nil || status.Status != "expired" || status.Payment != "released" {
		t.Fatal(status, err)
	}
}

func TestCheckoutStaleUnavailableAndPrivateInputs(t *testing.T) {
	s, input := checkoutFixture(t, 2)
	ctx := t.Context()
	guest := strings.Repeat("a", 32)
	order := guestCheckout(t, s, input)
	if _, err := s.SaveVariant(ctx, "tea", "TEA", VariantInput{Version: 1, PriceMinor: 1000, Active: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrder(ctx, guest, order); !errors.Is(err, ErrQuoteChanged) {
		t.Fatal(err)
	}
	input.Country = "FR"
	if _, err := s.Quote(ctx, input); !errors.Is(err, ErrShipping) {
		t.Fatal(err)
	}
	input.Country, input.Coupon = "MA", "invalid"
	if _, err := s.Quote(ctx, input); !errors.Is(err, ErrCoupon) {
		t.Fatal(err)
	}
	input.Coupon, input.Locale = "FIXED", "es"
	if _, err := s.Quote(ctx, input); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	input.Locale = "fr"
	if _, err := s.Quote(ctx, input); !errors.Is(err, ErrStock) {
		t.Fatal("unpublished translation entered the cart", err)
	}
	input.Locale = "en"
	q, err := s.Quote(ctx, input)
	if err != nil || q.DiscountMinor != 10 {
		t.Fatal(q, err)
	}
	orders, err := s.Orders(ctx, "")
	if err != nil || len(orders.Orders) != 0 {
		t.Fatal(orders, err)
	}
}

func TestCheckoutPaymentHoldsAndLateCapture(t *testing.T) {
	s, input := checkoutFixture(t, 1)
	ctx := t.Context()
	guest := strings.Repeat("a", 32)
	first, err := s.CreateOrder(ctx, guest, guestCheckout(t, s, input))
	if err != nil {
		t.Fatal(err)
	}
	order, err := s.MerchantOrder(ctx, first.Reference)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := s.BeginCapture(ctx, order.Operation, "merchant", "provider-order-1")
	if err != nil || operation.AmountMinor != order.Snapshot.TotalMinor {
		t.Fatal(operation, err)
	}
	if retry, err := s.BeginCapture(ctx, order.Operation, "merchant", "provider-order-1"); err != nil || retry != operation {
		t.Fatal(retry, err)
	}
	if err := s.expireOrders(ctx, time.Now().Add(reservationLifetime)); err != nil {
		t.Fatal(err)
	}
	status, err := s.GuestOrder(ctx, guest, first.Reference)
	if err != nil || status.Status != "review" || status.Payment != "uncertain" {
		t.Fatal(status, err)
	}
	if _, err = s.Quote(ctx, input); !errors.Is(err, ErrStock) {
		t.Fatal("uncertain capture lost its hold", err)
	}
	wrong := operation
	wrong.ProviderOrder = "different-provider-order"
	if err = s.RecordPayment(ctx, PaymentEvidence{Operation: wrong, CaptureID: "capture"}); !errors.Is(err, ErrPaymentEvidence) {
		t.Fatal(err)
	}
	wrong = operation
	wrong.AmountMinor++
	if err = s.RecordPayment(ctx, PaymentEvidence{Operation: wrong, CaptureID: "capture"}); !errors.Is(err, ErrPaymentEvidence) {
		t.Fatal(err)
	}
	if err = s.RecordPayment(ctx, PaymentEvidence{Operation: operation, DefinitivelyUnpaid: true}); err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateOrder(ctx, guest, guestCheckout(t, s, input))
	if err != nil {
		t.Fatal(err)
	}
	late := PaymentEvidence{Operation: operation, CaptureID: "late-capture"}
	for range 2 {
		if err = s.RecordPayment(ctx, late); err != nil {
			t.Fatal(err)
		}
	}
	order, err = s.MerchantOrder(ctx, first.Reference)
	if err != nil || order.Payment != "late_paid" || order.Status != "review" || order.Reservation != "released" ||
		len(order.Events) != 4 || order.Events[3].Kind != "paid_after_release" {
		t.Fatal(order, err)
	}
	order, err = s.MerchantOrder(ctx, second.Reference)
	if err != nil {
		t.Fatal(err)
	}
	secondOperation, err := s.BeginCapture(ctx, order.Operation, "merchant", "provider-order-2")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.RecordPayment(ctx, PaymentEvidence{Operation: secondOperation, CaptureID: "capture-2"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.RecordPayment(ctx, PaymentEvidence{Operation: secondOperation, DefinitivelyUnpaid: true}); err != nil {
		t.Fatal(err)
	}
	order, err = s.MerchantOrder(ctx, second.Reference)
	if err != nil || order.Status != "confirmed" || order.Payment != "paid" || order.Reservation != "sold" || len(order.Events) != 3 {
		t.Fatal(order, err)
	}
	draft, err := s.Draft(ctx, "tea", "en")
	if err != nil || draft.Variants[0].Stock != 0 || draft.Variants[0].Reserved != 0 {
		t.Fatal(draft, err)
	}
	var events int
	if err = s.DB.QueryRow(`SELECT count(*) FROM outbox_events WHERE name='order.paid' AND source_id=? AND payload='{}'`, second.Reference).Scan(&events); err != nil || events != 1 {
		t.Fatal(events, err)
	}
}
