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
	Created  string `json:"create_time"`
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
	if json.Unmarshal(body, &e) != nil {
		return ErrInvalid
	}
	row, relevant, err := c.normalize(e)
	if err != nil {
		return err
	}
	if err = c.signature(ctx, h, body); err != nil || !relevant {
		return err
	}
	digest := sha256.Sum256(body)
	return saveReceipt(ctx, s.store.DB, id, row, hex.EncodeToString(digest[:]), false)
}

func (c *client) normalize(e event) (Receipt, bool, error) {

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
		return Receipt{}, false, nil
	}
	if !remoteID.MatchString(e.ID) || !remoteID.MatchString(e.Resource.ID) ||
		(orderID != "" && !remoteID.MatchString(orderID)) || (captureID != "" && !remoteID.MatchString(captureID)) {
		return Receipt{}, false, ErrInvalid
	}
	return Receipt{EventID: e.ID, Kind: e.Kind, Resource: e.Resource.ID, Order: orderID, Capture: captureID}, true, nil
}

type receiptWriter interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func saveReceipt(ctx context.Context, q receiptWriter, connection string, row Receipt, fingerprint string, recovered bool) error {
	var saved string
	err := q.QueryRowContext(ctx, `INSERT INTO store_paypal_receipts(connection,event_id,fingerprint,kind,resource,related_order,related_capture,next_at,recovered)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(connection,event_id) DO UPDATE SET
		fingerprint=CASE WHEN store_paypal_receipts.fingerprint='' THEN excluded.fingerprint ELSE store_paypal_receipts.fingerprint END,
		recovered=max(store_paypal_receipts.recovered,excluded.recovered)
		WHERE store_paypal_receipts.kind=excluded.kind AND store_paypal_receipts.resource=excluded.resource
		  AND store_paypal_receipts.related_order=excluded.related_order AND store_paypal_receipts.related_capture=excluded.related_capture
		  AND (store_paypal_receipts.fingerprint='' OR excluded.fingerprint='' OR store_paypal_receipts.fingerprint=excluded.fingerprint)
		RETURNING event_id`, connection, row.EventID, fingerprint, row.Kind, row.Resource, row.Order, row.Capture, time.Now().Unix(), recovered).Scan(&saved)
	if errors.Is(err, sql.ErrNoRows) {
		return webhook.ErrConflict
	}
	return err
}

type Receipt struct {
	ID         int64  `json:"id"`
	EventID    string `json:"event_id"`
	Connection string `json:"connection"`
	Kind       string `json:"kind"`
	Resource   string `json:"resource"`
	Order      string `json:"provider_order,omitempty"`
	Capture    string `json:"provider_capture,omitempty"`
	State      string `json:"state"`
	Attempts   int    `json:"attempts"`
	Recovered  bool   `json:"recovered"`
	ReviewNote string `json:"review_note,omitempty"`
	ReviewedBy string `json:"reviewed_by,omitempty"`
	ReviewedAt int64  `json:"reviewed_at,omitempty"`
}

const receiptColumns = `ordinal,event_id,connection,kind,resource,related_order,coalesce(nullif(related_capture,''),refund_hold_capture),state,attempts,recovered,review_note,reviewed_by,reviewed_at`

func readReceipt(row interface{ Scan(...any) error }) (Receipt, error) {
	var out Receipt
	err := row.Scan(&out.ID, &out.EventID, &out.Connection, &out.Kind, &out.Resource, &out.Order, &out.Capture, &out.State, &out.Attempts, &out.Recovered, &out.ReviewNote, &out.ReviewedBy, &out.ReviewedAt)
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
	_, err := s.store.DB.ExecContext(ctx, `UPDATE store_paypal_receipts SET next_at=?,attempts=0,state='pending',review_note='',reviewed_by='',reviewed_at=0 WHERE ordinal=? AND state='review'`, time.Now().Unix(), id)
	return err
}

func (s *Service) processReceipt(ctx context.Context) error {
	for range 12 {
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
		if err == nil {
			_, err = s.applyRefund(ctx, op, e)
		}
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrMismatch) || errors.Is(err, store.ErrPaymentEvidence) {
			if _, saveErr := s.store.DB.ExecContext(ctx, `UPDATE store_paypal_receipts SET refund_hold_capture=? WHERE ordinal=?`,
				e.CaptureID, r.ID); saveErr != nil {
				return saveErr
			}
			return ErrReview
		}
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
