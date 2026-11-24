package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
)

type RefundInput struct {
	Key         string `json:"key"`
	AmountMinor int64  `json:"amount_minor"`
}

type RefundOperation struct {
	ID           string `json:"id"`
	Order        string `json:"-"`
	AmountMinor  int64  `json:"amount_minor"`
	State        string `json:"state"`
	Created      int64  `json:"created_at"`
	ProviderID   string `json:"-"`
	Connection   string `json:"-"`
	Currency     string `json:"-"`
	CaptureID    string `json:"-"`
	CaptureTotal int64  `json:"-"`
}

type RefundEvidence struct {
	OperationID string
	ProviderID  string
	Connection  string
	CaptureID   string
	Currency    string
	Value       string
	State       string
}

const refundColumns = `r.id,r.order_id,r.amount_minor,r.state,r.created_at,coalesce(r.provider_refund,''),r.connection,p.currency,p.capture_id,p.amount_minor`
const refundTables = ` FROM store_paypal_refunds r JOIN store_payment_operations p ON p.order_id=r.order_id`

func readRefund(row scanner) (RefundOperation, error) {
	var out RefundOperation
	err := row.Scan(&out.ID, &out.Order, &out.AmountMinor, &out.State, &out.Created, &out.ProviderID, &out.Connection, &out.Currency, &out.CaptureID, &out.CaptureTotal)
	return out, err
}

func (s *Service) PrepareRefund(ctx context.Context, id string, in RefundInput, fresh func(*sql.Tx) error) (RefundOperation, error) {
	var out RefundOperation
	if !media.ID.MatchString(id) || !media.ID.MatchString(in.Key) || in.AmountMinor < 1 || in.AmountMinor > maxCharge || fresh == nil {
		return out, ErrInvalid
	}
	err := s.write(ctx, "", func(tx *sql.Tx) error {
		if err := fresh(tx); err != nil {
			return err
		}
		if err := checkPaymentReview(ctx, tx); err != nil {
			return err
		}
		var err error
		out, err = readRefund(tx.QueryRowContext(ctx, "SELECT "+refundColumns+refundTables+" WHERE r.order_id=? AND r.retry_key=?", id, in.Key))
		if err == nil {
			if out.AmountMinor != in.AmountMinor {
				return ErrOrderReplay
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT p.connection,p.currency,p.capture_id,p.amount_minor
			FROM store_payment_operations p JOIN store_paypal_approvals a ON a.operation=p.id
			WHERE p.order_id=? AND p.state IN ('paid','late_paid')`, id).
			Scan(&out.Connection, &out.Currency, &out.CaptureID, &out.CaptureTotal)
		if err != nil {
			return err
		}
		var reserved int64
		if err = tx.QueryRowContext(ctx, `SELECT coalesce(sum(amount_minor),0) FROM store_paypal_refunds WHERE order_id=? AND state<>'failed'`, id).Scan(&reserved); err != nil {
			return err
		}
		if in.AmountMinor > out.CaptureTotal-reserved || (s.PaymentAmount != nil && !s.PaymentAmount(out.Currency, in.AmountMinor)) {
			return ErrInvalid
		}
		out.ID, err = db.NewID()
		if err != nil {
			return err
		}
		out.Order, out.AmountMinor, out.Created, out.State = id, in.AmountMinor, time.Now().Unix(), "pending"
		if _, err = tx.ExecContext(ctx, `INSERT INTO store_paypal_refunds(id,order_id,retry_key,amount_minor,connection,state,created_at)
			VALUES(?,?,?,?,?,'pending',?)`, out.ID, id, in.Key, out.AmountMinor, out.Connection, out.Created); err != nil {
			return err
		}
		if err = recordRefundEvent(ctx, tx, out, "refund_requested", access.Actor(ctx, ""), out.Created); err != nil {
			return err
		}
		return updateRefundState(ctx, tx, id, out.CaptureTotal)
	})
	return out, err
}

func (s *Service) RefundByID(ctx context.Context, id string) (RefundOperation, error) {
	return readRefund(s.DB.QueryRowContext(ctx, "SELECT "+refundColumns+refundTables+" WHERE r.id=?", id))
}

func (s *Service) Refunds(ctx context.Context, order string) ([]RefundOperation, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+refundColumns+refundTables+" WHERE r.order_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT 100", order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RefundOperation{}
	for rows.Next() {
		item, err := readRefund(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// RecordRefund is fed by verified provider data. A completed refund is
// monotonic, and doesn't return inventory to sellable stock.
func (s *Service) RecordRefund(ctx context.Context, operation RefundOperation, providerID, state string) (RefundOperation, error) {
	if !validProviderID(providerID) || (state != "pending" && state != "failed" && state != "complete") {
		return RefundOperation{}, ErrPaymentEvidence
	}
	var current RefundOperation
	err := s.write(ctx, "", func(tx *sql.Tx) error {
		var err error
		current, err = readRefund(tx.QueryRowContext(ctx, "SELECT "+refundColumns+refundTables+" WHERE r.id=?", operation.ID))
		if err != nil {
			return err
		}
		if current.Order != operation.Order || current.Connection != operation.Connection ||
			current.CaptureID != operation.CaptureID || current.Currency != operation.Currency || current.AmountMinor != operation.AmountMinor ||
			(current.ProviderID != "" && current.ProviderID != providerID) {
			return ErrPaymentEvidence
		}
		if current.State == "complete" || (current.State == "failed" && state == "pending") ||
			(current.State == state && current.ProviderID == providerID) {
			return nil
		}
		if current.State == "failed" && state == "complete" {
			// A later completion of a failed operation may conflict with a
			// replacement refund. Leave it for reconciliation.
			return ErrPaymentEvidence
		}
		if _, err = tx.ExecContext(ctx, `UPDATE store_paypal_refunds SET state=?,provider_refund=? WHERE id=?`, state, providerID, current.ID); err != nil {
			return err
		}
		current.State, current.ProviderID = state, providerID
		if err = recordRefundEvent(ctx, tx, current, "refund_"+state, "", time.Now().Unix()); err != nil {
			return err
		}
		return updateRefundState(ctx, tx, current.Order, current.CaptureTotal)
	})
	return current, err
}

func updateRefundState(ctx context.Context, tx *sql.Tx, order string, total int64) error {
	var completed int64
	var pending bool
	err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(CASE WHEN state='complete' THEN amount_minor ELSE 0 END),0),
		coalesce(max(state='pending'),0) FROM store_paypal_refunds WHERE order_id=?`, order).Scan(&completed, &pending)
	if err != nil {
		return err
	}
	state := "none"
	switch {
	case completed == total:
		state = "refunded"
	case pending:
		state = "pending"
	case completed > 0:
		state = "partial"
	}
	_, err = tx.ExecContext(ctx, `UPDATE store_orders SET refund_state=? WHERE id=?`, state, order)
	return err
}

func (s *Service) ResumeRefund(ctx context.Context, id string, fresh func(*sql.Tx) error) (RefundOperation, error) {
	var op RefundOperation
	if !media.ID.MatchString(id) || fresh == nil {
		return op, ErrInvalid
	}
	err := s.write(ctx, "", func(tx *sql.Tx) error {
		if err := fresh(tx); err != nil {
			return err
		}
		if err := checkPaymentReview(ctx, tx); err != nil {
			return err
		}
		var err error
		op, err = readRefund(tx.QueryRowContext(ctx, "SELECT "+refundColumns+refundTables+" WHERE r.id=?", id))
		if err != nil || op.State != "pending" {
			return err
		}
		return recordRefundEvent(ctx, tx, op, "refund_retry", access.Actor(ctx, ""), time.Now().Unix())
	})
	return op, err
}

func recordRefundEvent(ctx context.Context, tx *sql.Tx, op RefundOperation, kind, actor string, at int64) error {
	data, _ := json.Marshal(OrderEventDetail{RefundID: op.ID, AmountMinor: op.AmountMinor})
	return recordMerchantEvent(ctx, tx, op.Order, kind, actor, string(data), at)
}
