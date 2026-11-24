package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/outbox"
)

func orderID(t *testing.T) string {
	t.Helper()
	id, err := db.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMerchantOrderTracksPaymentShipmentRefundAndExport(t *testing.T) {
	s, in := checkoutFixture(t, 3)
	ctx := t.Context()
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO store_paypal_connections(id,mode,merchant,client_hash) VALUES('test','sandbox','test','test')`); err != nil {
		t.Fatal(err)
	}
	s.PaymentConnection = func(string) string { return "test" }
	guest := strings.Repeat("a", 32)
	place := func() OrderReceipt {
		t.Helper()
		order := guestCheckout(t, s, in)
		order.Customer.Name = "  =Buyer"
		receipt, err := s.CreateOrder(ctx, guest, order)
		if err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	merchant := access.WithMutation(ctx, access.Mutation{Actor: "staff:warehouse"})
	action := func(ctx context.Context, receipt OrderReceipt, kind string, in MerchantInput) (MerchantOperation, error) {
		return s.MerchantAction(ctx, "owner", receipt.Reference, kind, in)
	}

	unpaid := place()
	if _, err := action(merchant, unpaid, "shipped", MerchantInput{ID: orderID(t)}); !errors.Is(err, ErrOrderState) {
		t.Fatal("an unpaid order could ship", err)
	}
	cancel := MerchantInput{ID: orderID(t), Reason: "Buyer changed request"}
	first, err := action(merchant, unpaid, "cancelled", cancel)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := action(merchant, unpaid, "cancelled", cancel); err != nil || replay != first {
		t.Fatal(replay, err)
	}
	cancel.Reason = "Different cancellation"
	if _, err = action(merchant, unpaid, "cancelled", cancel); !errors.Is(err, ErrOrderReplay) {
		t.Fatal(err)
	}
	if _, err = action(merchant, unpaid, "restocked", MerchantInput{ID: orderID(t), SKU: "TEA", Quantity: 1, Reason: "Incorrect return"}); !errors.Is(err, ErrOrderState) {
		t.Fatal("the released hold could be restocked", err)
	}
	status, err := s.GuestOrder(ctx, guest, unpaid.Reference)
	if err != nil || status.Status != "cancelled" || status.Payment != "released" {
		t.Fatal(status, err)
	}

	receipt := place()
	p, err := s.GuestPurchase(ctx, guest, receipt.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindApproval(ctx, p, PaymentApproval{ProviderOrder: "PROVIDER-ORDER", ApprovalURL: "https://www.sandbox.paypal.com/checkoutnow?token=PROVIDER-ORDER"}); err != nil {
		t.Fatal(err)
	}
	p, err = s.GuestPurchase(ctx, guest, receipt.Reference)
	if err != nil {
		t.Fatal(err)
	}
	op, err := s.StartProviderCapture(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = action(merchant, receipt, "cancelled", MerchantInput{ID: orderID(t), Reason: "Premature cancellation"}); !errors.Is(err, ErrOrderState) {
		t.Fatal("uncertain capture should keep its hold", err)
	}
	if err = s.RecordPayment(ctx, PaymentEvidence{Operation: op, CaptureID: "CAPTURE-REF"}); err != nil {
		t.Fatal(err)
	}
	ship := MerchantInput{ID: orderID(t), Carrier: "Fixture Shipping", TrackingNumber: "TRACK-1"}
	denied := errors.New("grant revoked")
	revoked := access.WithMutation(ctx, access.Mutation{Actor: "staff:warehouse", Check: func(*sql.Tx, string) error { return denied }})
	if _, err = action(revoked, receipt, "shipped", ship); !errors.Is(err, denied) {
		t.Fatal("order action ignored a revoked grant", err)
	}
	sent, err := action(merchant, receipt, "shipped", ship)
	if err != nil {
		t.Fatal(err)
	}
	if retry, err := action(merchant, receipt, "shipped", ship); err != nil || retry != sent {
		t.Fatal(retry, err)
	}
	changed := MerchantInput{ID: orderID(t), Carrier: "Fixture Shipping", TrackingNumber: "+TRACK-2"}
	if _, err = action(merchant, receipt, "tracking_updated", changed); err != nil {
		t.Fatal(err)
	}
	if _, err = action(merchant, receipt, "cancelled", MerchantInput{ID: orderID(t), Reason: "Already shipped"}); !errors.Is(err, ErrOrderState) {
		t.Fatal(err)
	}

	auth := func(*sql.Tx) error { return nil }
	owner := access.WithMutation(ctx, access.Mutation{Actor: "owner-id"})
	refInput := RefundInput{Key: orderID(t), AmountMinor: 100}
	ref, err := s.PrepareRefund(owner, receipt.Reference, refInput, auth)
	if err != nil {
		t.Fatal(err)
	}
	if refReplay, err := s.PrepareRefund(owner, receipt.Reference, refInput, auth); err != nil || refReplay.ID != ref.ID {
		t.Fatal(refReplay, err)
	}
	for range 2 {
		if _, err = s.RecordRefund(ctx, ref, "PROVIDER-REFUND", "complete"); err != nil {
			t.Fatal(err)
		}
	}
	before, err := s.Draft(ctx, "tea", "en")
	if err != nil || before.Variants[0].Stock != 2 || before.Variants[0].Reserved != 0 {
		t.Fatal("refund changed physical inventory", before, err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		input := MerchantInput{ID: orderID(t), Reason: "Parcel returned", SKU: "TEA", Quantity: 1}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := action(merchant, receipt, "restocked", input)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var success, conflict int
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrOrderState):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	after, err := s.Draft(ctx, "tea", "en")
	if err != nil || after.Variants[0].Stock != 3 {
		t.Fatal(after, err)
	}
	adjustments, err := s.Adjustments(ctx, "TEA")
	if err != nil || len(adjustments) != 2 {
		t.Fatal(adjustments, err)
	}
	for _, row := range adjustments {
		if row.Reason == "Parcel returned" && (row.Actor != "staff:warehouse" || row.Delta != 1) {
			t.Fatal(row)
		}
	}
	worker := outbox.Worker{DB: s.DB, Handle: PrepareNotice}
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(workerCtx)
	}()
	defer func() { stop(); <-done }()
	deadline := time.Now().Add(4 * time.Second)
	for {
		status, err = s.GuestOrder(ctx, guest, receipt.Reference)
		if err != nil {
			t.Fatal(err)
		}
		if len(status.Notices) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the two durable notifications were not delivered", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Shipment.TrackingNumber != "+TRACK-2" || status.Refund != "partial" || status.RefundedMinor != 100 || status.PaidAt == 0 {
		t.Fatal(status)
	}
	merchantOrder, err := s.MerchantOrder(ctx, receipt.Reference)
	if err != nil || merchantOrder.PaymentRecord.Capture != "CAPTURE-REF" || merchantOrder.Restocked["TEA"] != 1 ||
		len(merchantOrder.Refunds) != 1 || merchantOrder.Refunds[0].ProviderID != "PROVIDER-REFUND" {
		t.Fatal(merchantOrder, err)
	}
	want := []string{"reserved", "capture_pending", "paid", "shipped", "tracking_updated", "refund_requested", "refund_complete", "restocked"}
	if len(merchantOrder.Events) != len(want) {
		t.Fatal(merchantOrder.Events)
	}
	for i, event := range merchantOrder.Events {
		if event.Kind != want[i] {
			t.Fatal(i, event.Kind, want[i])
		}
		if event.Kind == "refund_requested" && (event.Actor != "owner-id" || event.Detail.RefundID != ref.ID || event.Detail.AmountMinor != 100) {
			t.Fatal(event)
		}
	}
	var output bytes.Buffer
	if err = s.ExportOrders(ctx, &output); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	found := false
	for _, row := range rows[1:] {
		if row[0] != receipt.Reference {
			continue
		}
		found = true
		if row[21] != "'  =Buyer" || row[20] != "'+TRACK-2" || row[10] != "CAPTURE-REF" || row[17] != "100" ||
			!strings.Contains(row[29], `"sku":"TEA"`) || !strings.Contains(row[30], `"TEA":1`) ||
			!strings.Contains(row[31], "PROVIDER-REFUND") {
			t.Fatal(row)
		}
	}
	if !found {
		t.Fatal("paid order missing from export")
	}
}
