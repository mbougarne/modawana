package paypal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/operations"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"github.com/mbougarne/modawana/backend/internal/store"
)

func TestCommerceStaleBackupReconcilesProviderActivity(t *testing.T) {
	s, fake, receipt, cfg, raw := setupPayPal(t)
	ctx := access.WithMutation(t.Context(), access.Mutation{Actor: "fixture-owner"})
	ownerFresh := func(*sql.Tx) error { return nil }
	if _, err := s.Approval(ctx, guest, receipt.Reference); err != nil {
		t.Fatal(err)
	}
	purchase, err := s.store.GuestPurchase(ctx, guest, receipt.Reference)
	if err != nil {
		t.Fatal(err)
	}
	originalOp, err := s.store.StartProviderCapture(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	// Hold a second capture across recovery without ever receiving evidence.
	stockID, _ := db.NewID()
	if _, err = s.store.Adjust(ctx, "owner", "MUG", store.AdjustmentInput{ID: stockID, Delta: 1, Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	quoteInput := store.QuoteInput{Locale: "en", Country: "US", Items: []store.CartItem{{SKU: "MUG", Quantity: 1}}}
	quote, err := s.store.Quote(ctx, quoteInput)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, _ := db.NewID()
	second, err := s.store.CreateOrder(ctx, guest, store.OrderInput{QuoteInput: quoteInput, Key: secondKey, QuoteFingerprint: quote.Fingerprint,
		Customer: store.Customer{Name: "Fixture", Email: "fixture@example.test", Address: "123 Test", City: "Portland"}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.store.GuestPurchase(ctx, guest, second.Reference)
	if err != nil {
		t.Fatal(err)
	}
	binding := store.PaymentApproval{ProviderOrder: "ORDER-ON-HOLD", ApprovalURL: "https://www.sandbox.paypal.com/checkoutnow?token=ORDER-ON-HOLD"}
	if err = s.store.BindApproval(ctx, other, binding); err != nil {
		t.Fatal(err)
	}
	other, err = s.store.GuestPurchase(ctx, guest, second.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.StartProviderCapture(ctx, other); err != nil {
		t.Fatal(err)
	}
	saved := retestEvent("SNAPSHOT-EVENT", "CHECKOUT.ORDER.APPROVED", "ORDER123")
	if err = s.Accept(ctx, "sandbox-store", eventHeaders(), saved); err != nil {
		t.Fatal(err)
	}
	files, err := storage.Open(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	root := t.TempDir()
	archive := filepath.Join(root, "backup.zip")
	file, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = operations.Backup(ctx, s.store.DB, files, cfg.DataRoot, file)
	if err = errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	backupTime := time.Since(started)
	// The original installation advances after the archive. The restored
	// ledger retains the capture operation, but has never seen its result.
	fake.approved = true
	status, err := s.Finish(ctx, guest, receipt.Reference, true)
	if err != nil || status.Payment != "paid" {
		t.Fatal(status, err)
	}
	fake.failRefund = true
	refundKey, _ := db.NewID()
	refund, err := s.Refund(ctx, receipt.Reference, store.RefundInput{Key: refundKey, AmountMinor: 450}, ownerFresh)
	if !errors.Is(err, ErrUnavailable) || refund.ID == "" {
		t.Fatal(refund, err)
	}
	makeEvent := func(id, kind, resource, related string) event {
		t.Helper()
		var item event
		if err := json.Unmarshal(retestEvent(id, kind, resource), &item); err != nil {
			t.Fatal(err)
		}
		item.Created = time.Now().UTC().Format(time.RFC3339Nano)
		item.Resource.Supplementary.Related.Order = related
		return item
	}
	matched := makeEvent("AFTER-BACKUP", "PAYMENT.CAPTURE.COMPLETED", "CAPTURE123", "ORDER123")
	unmatched := makeEvent("ORPHAN-CAPTURE", "PAYMENT.CAPTURE.COMPLETED", "OTHER-CAPTURE", "ORDER-LOST")
	missingRefund := makeEvent("ORPHAN-REFUND", "PAYMENT.CAPTURE.REFUNDED", "REF-"+refund.ID, "ORDER123")
	fake.history = []event{missingRefund, unmatched, matched}

	restoredDir := filepath.Join(root, "restored")
	started = time.Now()
	if err = operations.Restore(ctx, archive, restoredDir); err != nil {
		t.Fatal(err)
	}
	restoreTime := time.Since(started)
	if !operations.Maintenance(restoredDir) {
		t.Fatal("restore bypassed maintenance")
	}
	d, err := db.Open(ctx, restoredDir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	catalog := &store.Service{DB: d, PaymentAmount: Payable}
	cfg.DataRoot = restoredDir
	recovered, err := open(ctx, catalog, cfg, raw, nil, &http.Client{Transport: fake})
	if err != nil {
		t.Fatal(err)
	}
	catalog.PaymentConnection = recovered.Active
	state, err := recovered.Recovery(ctx)
	if err != nil || state.Cutoff <= 0 || state.Target < state.Cutoff || state.Pending != 1 || state.Uncertain != 2 {
		t.Fatal(state, err)
	}
	if _, err = catalog.CheckoutOptions(ctx, "en"); !errors.Is(err, store.ErrCheckoutClosed) {
		t.Fatal("restored checkout accepted new activity", err)
	}
	if err = recovered.ConfirmRecovery(ctx, ownerFresh); !errors.Is(err, ErrReview) {
		t.Fatal("checkout review never checked provider coverage", err)
	}

	// An incomplete page cannot move the recorded coverage boundary.
	fake.truncatedHistory = true
	if err = recovered.RecoverEvents(ctx, "sandbox-store"); !errors.Is(err, ErrReview) {
		t.Fatal("a partial PayPal history was accepted", err)
	}
	state, err = recovered.Recovery(ctx)
	if err != nil || state.Connections[0].Through != 0 {
		t.Fatal(state, err)
	}
	fake.truncatedHistory = false
	if err = recovered.RecoverEvents(ctx, "sandbox-store"); err != nil {
		t.Fatal(err)
	}
	if err = recovered.RecoverEvents(ctx, "sandbox-store"); err != nil {
		t.Fatal(err)
	}
	// The same event may arrive as a signed callback with distinct encoding.
	if err = recovered.Accept(ctx, "sandbox-store", eventHeaders(), retestEvent("AFTER-BACKUP", "PAYMENT.CAPTURE.COMPLETED", "CAPTURE123")); err != nil {
		t.Fatal(err)
	}
	if err = recovered.processReceipt(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = recovered.Recovery(ctx)
	if err != nil || state.Pending != 0 || state.Unresolved != 2 || state.Uncertain != 1 || state.CanConfirm {
		t.Fatal(state, err)
	}
	events, err := recovered.Receipts(ctx, 0)
	if err != nil || len(events) != 4 {
		t.Fatal(events, err)
	}
	known := false
	for _, row := range events {
		if row.EventID == "SNAPSHOT-EVENT" {
			known = row.ID == 1
		}
		if row.EventID == "AFTER-BACKUP" && (!row.Recovered || row.State != "applied") {
			t.Fatal(row)
		}
	}
	if !known {
		t.Fatal("backup did not preserve the saved receipt ID")
	}
	if _, err = catalog.RefundByID(ctx, refund.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("reconciliation invented a refund record from remote data", err)
	}
	local, err := catalog.MerchantOrder(ctx, receipt.Reference)
	if err != nil || local.Payment != "paid" || local.Operation != originalOp.StableID || local.PaymentRecord.Capture != "CAPTURE123" {
		t.Fatal(local, err)
	}
	if _, err = recovered.Refund(ctx, receipt.Reference, store.RefundInput{Key: refundKey, AmountMinor: 450}, ownerFresh); !errors.Is(err, store.ErrCheckoutClosed) {
		t.Fatal("an unresolved restored refund could be replayed", err)
	}
	if err = recovered.ConfirmRecovery(ctx, ownerFresh); !errors.Is(err, ErrReview) {
		t.Fatal(err)
	}
	for _, row := range events {
		if row.State != "review" {
			continue
		}
		denied := errors.New("fresh owner required")
		if err = recovered.ReviewReceipt(ctx, row.ID, "fixture reconciliation", func(*sql.Tx) error { return denied }); !errors.Is(err, denied) {
			t.Fatal(err)
		}
		if err = recovered.ReviewReceipt(ctx, row.ID, "fixture reconciliation", ownerFresh); err != nil {
			t.Fatal(err)
		}
	}
	if err = recovered.ConfirmRecovery(ctx, ownerFresh); err != nil {
		t.Fatal(err)
	}
	if _, err = recovered.Refund(ctx, receipt.Reference, store.RefundInput{Key: refundKey, AmountMinor: 450}, ownerFresh); !errors.Is(err, store.ErrPaymentEvidence) {
		t.Fatal("restored capture with missing refund lost its hold after review", err)
	}
	waiting, err := catalog.MerchantOrder(ctx, second.Reference)
	if err != nil || waiting.Payment != "uncertain" || waiting.Reservation != "held" {
		t.Fatal(waiting, err)
	}
	if _, err = catalog.CheckoutOptions(ctx, "en"); !errors.Is(err, store.ErrCheckoutClosed) {
		t.Fatal("confirmation turned checkout on automatically", err)
	}
	if fake.captures != 1 || fake.refundRequests != 1 {
		t.Fatal("recovery sent an effectful provider request", fake.captures, fake.refundRequests)
	}
	t.Logf("local synthetic archive %s, paused restore %s", backupTime, restoreTime)
}

func TestCommerceManualCoverageRequiresFreshOwnerAndKnownAccount(t *testing.T) {
	s, _, _, _, _ := setupPayPal(t)
	ctx := access.WithMutation(t.Context(), access.Mutation{Actor: "owner"})
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE store_checkout_settings SET payment_review_required=1,enabled=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE store_paypal_recovery SET target_at=unixepoch() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverEvents(ctx, "sandbox-store"); !errors.Is(err, ErrReview) {
		t.Fatal("a legacy archive lacked an exact cutoff", err)
	}
	denied := errors.New("fresh authentication required")
	if err := s.RecordManualCoverage(ctx, "sandbox-store", "report R-17", func(*sql.Tx) error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	fresh := func(*sql.Tx) error { return nil }
	if err := s.RecordManualCoverage(ctx, "wrong-account", "report R-17", fresh); !errors.Is(err, ErrReview) {
		t.Fatal(err)
	}
	if err := s.ConfirmRecovery(ctx, fresh); !errors.Is(err, ErrReview) {
		t.Fatal(err)
	}
	if err := s.RecordManualCoverage(ctx, "sandbox-store", "report R-17", fresh); err != nil {
		t.Fatal(err)
	}
	current, err := s.Recovery(ctx)
	if err != nil || !current.CanConfirm || current.Connections[0].Note != "report R-17" {
		t.Fatal(current, err)
	}
	if err = s.ConfirmRecovery(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	after, err := s.store.CheckoutSettings(ctx)
	if err != nil || after.Enabled || after.PaymentReviewRequired {
		t.Fatal(after, err)
	}
}
