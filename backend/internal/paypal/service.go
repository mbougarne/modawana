package paypal

import (
	"context"
	"errors"
	"time"

	"github.com/mbougarne/modawana/backend/internal/store"
)

type Status struct {
	ReviewRequired bool   `json:"review_required"`
	Mode           string `json:"mode,omitempty"`
	Ready          bool   `json:"ready"`
	ReviewReceipts int    `json:"review_receipts"`
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	var out Status
	current, err := s.store.Settings(ctx)
	if err != nil {
		return out, err
	}
	checkout, err := s.store.CheckoutSettings(ctx)
	if err != nil {
		return out, err
	}
	out.ReviewRequired = checkout.PaymentReviewRequired
	out.Ready = s.Active(current.Currency) != "" && !out.ReviewRequired
	if client := s.connections[s.active]; client != nil {
		out.Mode = client.config.Mode
	}
	err = s.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM store_paypal_receipts WHERE state='review'`).Scan(&out.ReviewReceipts)
	return out, err
}

func (s *Service) Approval(ctx context.Context, guest, id string) (string, error) {
	p, err := s.store.GuestPurchase(ctx, guest, id)
	if err != nil {
		return "", err
	}
	if err = s.store.CheckPurchaseOpen(ctx, p); err != nil {
		return "", err
	}
	c := s.connections[p.Operation.Connection]
	if c == nil || p.State != "pending" || time.Now().Unix() >= p.Expires {
		return "", store.ErrConflict
	}
	if p.ApprovalURL != "" {
		if !c.safeApproval(p.ApprovalURL, p.Operation.ProviderOrder) {
			return "", ErrMismatch
		}
		return p.ApprovalURL, nil
	}
	approval, err := c.Create(ctx, p)
	if err != nil {
		return "", err
	}
	if err = s.store.BindApproval(ctx, p, approval); err != nil {
		return "", err
	}
	return approval.ApprovalURL, nil
}

// Finish uses only the server-bound provider ID. The caller can request a
// status check without permitting a new capture.
func (s *Service) Finish(ctx context.Context, guest, id string, allowCapture bool) (store.OrderStatus, error) {
	p, err := s.store.GuestPurchase(ctx, guest, id)
	if err != nil {
		return store.OrderStatus{}, err
	}
	if p.Operation.ProviderOrder != "" && p.State != "paid" && p.State != "late_paid" {
		if err = s.reconcile(ctx, p, allowCapture); err != nil {
			return store.OrderStatus{}, err
		}
	}
	return s.store.GuestOrder(ctx, guest, id)
}

func (s *Service) Reconcile(ctx context.Context, id string) error {
	p, err := s.store.MerchantPurchase(ctx, id)
	if err != nil {
		return err
	}
	if p.Operation.ProviderOrder != "" {
		if err = s.reconcile(ctx, p, false); err != nil {
			return err
		}
	}
	refunds, err := s.store.Refunds(ctx, id)
	if err != nil {
		return err
	}
	for _, op := range refunds {
		if op.State != "pending" || op.ProviderID == "" {
			continue
		}
		client := s.connections[op.Connection]
		if client == nil {
			return ErrReview
		}
		if _, err = s.verifyRefund(ctx, client, op); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) reconcile(ctx context.Context, p store.Purchase, allowCapture bool) error {
	client := s.connections[p.Operation.Connection]
	if client == nil {
		return ErrReview
	}
	result, err := client.Verify(ctx, p.Operation)
	if err != nil {
		return err
	}
	if result.Evidence != nil {
		return s.recordVerification(ctx, p, result)
	}
	if !allowCapture || !result.Approved {
		return nil
	}
	op, err := s.store.StartProviderCapture(ctx, p)
	if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrCheckoutClosed) {
		// Another request may have finished this operation, or the shop
		// stopped accepting payments. Status remains available either way.
		return nil
	}
	if err != nil {
		return err
	}
	attemptErr := client.Capture(ctx, op)
	result, err = client.Verify(ctx, op)
	if err != nil {
		return errors.Join(attemptErr, err)
	}
	if result.Evidence != nil {
		return s.recordVerification(ctx, p, result)
	}
	return nil // no conclusive evidence; the ledger keeps the hold
}

func (s *Service) recordVerification(ctx context.Context, p store.Purchase, result store.PaymentVerification) error {
	if result.Refunded && p.State != "paid" && p.State != "late_paid" {
		return ErrReview
	}
	return s.store.RecordPayment(ctx, *result.Evidence)
}
