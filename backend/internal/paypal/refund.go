package paypal

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/store"
)

type refundResult struct {
	ID      string `json:"id"`
	Invoice string `json:"invoice_id"`
	State   string `json:"status"`
	Amount  money  `json:"amount"`
	Links   []link `json:"links"`
}

func (c *client) Refund(ctx context.Context, op store.RefundOperation) (store.RefundEvidence, error) {
	var out store.RefundEvidence
	if !remoteID.MatchString(op.CaptureID) || op.Connection != c.config.ID {
		return out, ErrMismatch
	}
	amount, err := formatMoney(op.Currency, op.AmountMinor)
	if err != nil {
		return out, err
	}
	in := map[string]any{"invoice_id": op.ID, "custom_id": op.ID, "amount": amount}
	var result refundResult
	if err = c.api(ctx, http.MethodPost, "/v2/payments/captures/"+op.CaptureID+"/refund", "ref-"+op.ID, in, &result); err != nil {
		return out, err
	}
	return c.refundEvidence(result)
}

func (c *client) VerifyRefund(ctx context.Context, providerID string) (store.RefundEvidence, error) {
	var out store.RefundEvidence
	if !remoteID.MatchString(providerID) {
		return out, ErrInvalid
	}
	var result refundResult
	if err := c.api(ctx, http.MethodGet, "/v2/payments/refunds/"+providerID, "", nil, &result); err != nil {
		return out, err
	}
	if result.ID != providerID {
		return out, ErrMismatch
	}
	return c.refundEvidence(result)
}

func (c *client) refundEvidence(result refundResult) (store.RefundEvidence, error) {
	var out store.RefundEvidence
	if !remoteID.MatchString(result.ID) || !remoteID.MatchString(result.Invoice) {
		return out, ErrMismatch
	}
	for _, link := range result.Links {
		if link.Rel == "up" {
			out.CaptureID = c.captureLink(link.Href)
		}
	}
	if out.CaptureID == "" {
		return out, ErrMismatch
	}
	switch result.State {
	case "PENDING":
		out.State = "pending"
	case "COMPLETED":
		out.State = "complete"
	case "CANCELLED", "FAILED":
		out.State = "failed"
	default:
		return out, ErrMismatch
	}
	out.OperationID, out.ProviderID, out.Connection = result.Invoice, result.ID, c.config.ID
	out.Currency, out.Value = result.Amount.Currency, result.Amount.Value
	return out, nil
}

func (c *client) captureLink(raw string) string {
	host := "api.paypal.com"
	if c.config.Mode == "sandbox" {
		host = "api.sandbox.paypal.com"
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" ||
		(u.Host != c.apiHost() && u.Host != host) {
		return ""
	}
	id, ok := strings.CutPrefix(u.Path, "/v2/payments/captures/")
	if !ok || !remoteID.MatchString(id) {
		return ""
	}
	return id
}

func (s *Service) applyRefund(ctx context.Context, op store.RefundOperation, e store.RefundEvidence) (store.RefundOperation, error) {
	if e.OperationID != op.ID || e.Connection != op.Connection || e.CaptureID != op.CaptureID ||
		!matchesMoney(money{e.Currency, e.Value}, op.Currency, op.AmountMinor) {
		return store.RefundOperation{}, ErrMismatch
	}
	return s.store.RecordRefund(ctx, op, e.ProviderID, e.State)
}

func (s *Service) Refund(ctx context.Context, order string, in store.RefundInput, fresh func(*sql.Tx) error) (store.RefundOperation, error) {
	p, err := s.store.MerchantPurchase(ctx, order)
	if err != nil {
		return store.RefundOperation{}, err
	}
	if !s.HasConnection(p.Operation.Connection) {
		return store.RefundOperation{}, ErrReview
	}
	op, err := s.store.PrepareRefund(ctx, order, in, fresh)
	if err != nil || op.State != "pending" {
		return op, err
	}
	return s.dispatchRefund(ctx, op)
}

func (s *Service) ResumeRefund(ctx context.Context, id string, fresh func(*sql.Tx) error) (store.RefundOperation, error) {
	op, err := s.store.ResumeRefund(ctx, id, fresh)
	if err != nil || op.State != "pending" {
		return op, err
	}
	return s.dispatchRefund(ctx, op)
}

func (s *Service) dispatchRefund(ctx context.Context, op store.RefundOperation) (store.RefundOperation, error) {
	client := s.connections[op.Connection]
	if client == nil {
		return op, ErrReview
	}
	if op.ProviderID != "" {
		return s.verifyRefund(ctx, client, op)
	}
	// A POST's result can be unknown. Reuse the same provider ID within a
	// deliberately short window; older attempts require merchant review.
	if time.Now().Unix() >= op.Created+1800 {
		return op, ErrReview
	}
	evidence, err := client.Refund(ctx, op)
	if err != nil {
		return op, err
	}
	return s.applyRefund(ctx, op, evidence)
}

func (s *Service) verifyRefund(ctx context.Context, c *client, op store.RefundOperation) (store.RefundOperation, error) {
	e, err := c.VerifyRefund(ctx, op.ProviderID)
	if err != nil {
		return op, err
	}
	return s.applyRefund(ctx, op, e)
}
