package paypal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/store"
	"github.com/mbougarne/modawana/backend/internal/webhook"
)

type event struct {
	ID       string `json:"id"`
	Kind     string `json:"event_type"`
	Resource struct {
		ID            string `json:"id"`
		Links         []link `json:"links"`
		Supplementary struct {
			Related struct {
				Order   string `json:"order_id"`
				Capture string `json:"capture_id"`
			} `json:"related_ids"`
		} `json:"supplementary_data"`
	} `json:"resource"`
}

func (c *client) signature(ctx context.Context, h http.Header, body []byte) error {
	fields := map[string]string{"webhook_id": c.config.WebhookID}
	for _, field := range []string{"auth_algo", "cert_url", "transmission_id", "transmission_sig", "transmission_time"} {
		values := h.Values("Paypal-" + strings.ReplaceAll(field, "_", "-"))
		if len(values) != 1 || values[0] == "" || len(values[0]) > 2048 {
			return ErrSignature
		}
		fields[field] = values[0]
	}
	ts, tsErr := time.Parse(time.RFC3339, fields["transmission_time"])
	sig, sigErr := base64.StdEncoding.DecodeString(fields["transmission_sig"])
	cert, err := url.Parse(fields["cert_url"])
	if fields["auth_algo"] != "SHA256withRSA" || !remoteID.MatchString(fields["transmission_id"]) ||
		tsErr != nil || ts.After(time.Now().Add(5*time.Minute)) || ts.Before(time.Now().Add(-4*24*time.Hour)) ||
		sigErr != nil || len(sig) < 128 || len(sig) > 512 || err != nil || !safeCertificate(cert) {
		return ErrSignature
	}
	raw, _ := json.Marshal(fields)
	payload := append(raw[:len(raw)-1], []byte(`,"webhook_event":`)...)
	payload = append(payload, body...)
	payload = append(payload, '}')
	var result struct {
		Status string `json:"verification_status"`
	}
	if err = c.api(ctx, http.MethodPost, "/v1/notifications/verify-webhook-signature", "", json.RawMessage(payload), &result); err != nil {
		return err
	}
	if result.Status != "SUCCESS" {
		return ErrSignature
	}
	return nil
}

func safeCertificate(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return false
	}
	switch u.Host {
	case "api.paypal.com", "api-m.paypal.com", "api.sandbox.paypal.com", "api-m.sandbox.paypal.com":
	default:
		return false
	}
	id, ok := strings.CutPrefix(u.Path, "/v1/notifications/certs/")
	return ok && remoteID.MatchString(id)
}

func (s *Service) Accept(ctx context.Context, id string, h http.Header, body []byte) error {
	c := s.connections[id]
	if c == nil || len(body) > webhook.MaxBody {
		return ErrInvalid
	}
	var e event
	if json.Unmarshal(body, &e) != nil || !remoteID.MatchString(e.ID) || !remoteID.MatchString(e.Resource.ID) {
		return ErrInvalid
	}
	orderID, captureID := e.Resource.Supplementary.Related.Order, e.Resource.Supplementary.Related.Capture
	switch e.Kind {
	case "CHECKOUT.ORDER.APPROVED", "CHECKOUT.ORDER.COMPLETED":
		orderID = e.Resource.ID
	case "PAYMENT.CAPTURE.COMPLETED", "PAYMENT.CAPTURE.PENDING", "PAYMENT.CAPTURE.DENIED", "PAYMENT.CAPTURE.DECLINED", "PAYMENT.CAPTURE.REVERSED":
		captureID = e.Resource.ID
	case "PAYMENT.CAPTURE.REFUNDED", "PAYMENT.REFUND.PENDING", "PAYMENT.REFUND.FAILED":
		for _, link := range e.Resource.Links {
			if link.Rel == "up" && captureID == "" {
				captureID = c.captureLink(link.Href)
			}
		}
	default:
		// Unknown events have no effect. Only accept deliveries through
		// the verified webhook connection.
		return c.signature(ctx, h, body)
	}
	if (orderID != "" && !remoteID.MatchString(orderID)) || (captureID != "" && !remoteID.MatchString(captureID)) {
		return ErrInvalid
	}
	if err := c.signature(ctx, h, body); err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	var saved string
	err := s.store.DB.QueryRowContext(ctx, `INSERT INTO store_paypal_receipts(connection,event_id,fingerprint,kind,resource,related_order,related_capture,next_at)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(connection,event_id) DO UPDATE SET event_id=excluded.event_id RETURNING fingerprint`,
		id, e.ID, hash, e.Kind, e.Resource.ID, orderID, captureID, time.Now().Unix()).Scan(&saved)
	if err == nil && hash != saved {
		return webhook.ErrConflict
	}
	return err
}

type Receipt struct {
	ID         int64  `json:"id"`
	Connection string `json:"connection"`
	Kind       string `json:"kind"`
	Resource   string `json:"resource"`
	Order      string `json:"provider_order,omitempty"`
	Capture    string `json:"provider_capture,omitempty"`
	State      string `json:"state"`
	Attempts   int    `json:"attempts"`
}

const receiptColumns = `ordinal,connection,kind,resource,related_order,related_capture,state,attempts`

func readReceipt(row interface{ Scan(...any) error }) (Receipt, error) {
	var out Receipt
	err := row.Scan(&out.ID, &out.Connection, &out.Kind, &out.Resource, &out.Order, &out.Capture, &out.State, &out.Attempts)
	return out, err
}

func (s *Service) Receipts(ctx context.Context, before int64) ([]Receipt, error) {
	rows, err := s.store.DB.QueryContext(ctx, "SELECT "+receiptColumns+" FROM store_paypal_receipts WHERE (?=0 OR ordinal<?) ORDER BY ordinal DESC LIMIT 50", before, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Receipt{}
	for rows.Next() {
		r, err := readReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) RetryReceipt(ctx context.Context, id int64) error {
	_, err := s.store.DB.ExecContext(ctx, `UPDATE store_paypal_receipts SET next_at=?,attempts=0,state='pending' WHERE ordinal=? AND state='review'`, time.Now().Unix(), id)
	return err
}

func (s *Service) processReceipt(ctx context.Context) error {
	for range 5 {
		now := time.Now()
		r, err := readReceipt(s.store.DB.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM store_paypal_receipts
			WHERE state='pending' AND next_at<=? ORDER BY next_at,ordinal LIMIT 1`, now.Unix()))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		workCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = s.applyReceipt(workCtx, r)
		cancel()
		state := "applied"
		switch {
		case errors.Is(err, ErrMismatch), errors.Is(err, store.ErrPaymentEvidence), errors.Is(err, ErrReview), r.Attempts >= 8 && err != nil:
			state = "review"
		case err != nil:
			state = "pending"
		}
		next := now.Add(time.Duration(1<<min(r.Attempts, 8)) * time.Minute).Unix()
		if _, err = s.store.DB.ExecContext(ctx, `UPDATE store_paypal_receipts SET state=?,attempts=attempts+1,next_at=? WHERE ordinal=?`, state, next, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyReceipt(ctx context.Context, r Receipt) error {
	c := s.connections[r.Connection]
	if c == nil {
		return ErrReview
	}
	switch r.Kind {
	case "PAYMENT.CAPTURE.REFUNDED", "PAYMENT.REFUND.PENDING", "PAYMENT.REFUND.FAILED":
		e, err := c.VerifyRefund(ctx, r.Resource)
		if err != nil {
			return err
		}
		if r.Capture != "" && e.CaptureID != r.Capture {
			return ErrMismatch
		}
		op, err := s.store.RefundByID(ctx, e.OperationID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReview
		}
		if err != nil {
			return err
		}
		_, err = s.applyRefund(ctx, op, e)
		return err
	case "PAYMENT.CAPTURE.REVERSED":
		return ErrReview
	}
	p, err := s.store.ProviderPurchase(ctx, r.Connection, r.Order, r.Capture)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReview
	}
	if err != nil {
		return err
	}
	result, err := c.Verify(ctx, p.Operation)
	if err != nil {
		return err
	}
	if result.Evidence == nil {
		if r.Kind == "PAYMENT.CAPTURE.COMPLETED" || r.Kind == "CHECKOUT.ORDER.COMPLETED" {
			return ErrUnavailable
		}
		return nil
	}
	if result.Evidence.CaptureID != "" && r.Capture != "" && result.Evidence.CaptureID != r.Capture {
		return ErrMismatch
	}
	return s.recordVerification(ctx, p, result)
}
