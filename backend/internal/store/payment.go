package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mbougarne/modawana/backend/internal/outbox"
)

// PaymentAdapter isolates effectful provider calls. Create and Capture must
// replay their StableID for the same operation. Verify obtains provider evidence
// independently of browser returns. A connection belongs to one merchant.
type PaymentAdapter interface {
	Create(ctx context.Context, purchase Purchase) (PaymentApproval, error)
	Capture(ctx context.Context, operation PaymentOperation) error
	Verify(ctx context.Context, operation PaymentOperation) (PaymentVerification, error)
	Refund(ctx context.Context, operation RefundOperation) (RefundEvidence, error)
	VerifyRefund(ctx context.Context, providerID string) (RefundEvidence, error)
}

type PaymentVerification struct {
	Evidence *PaymentEvidence
	Approved bool
	Refunded bool
}

type PaymentApproval struct {
	ProviderOrder string
	ApprovalURL   string
}

type PaymentOperation struct {
	StableID      string
	Order         string
	Connection    string
	ProviderOrder string
	Currency      string
	AmountMinor   int64
}

// PaymentEvidence is supplied only by a trusted adapter after verifying the
// merchant connection and provider operation. An ambiguous response stays held.
type PaymentEvidence struct {
	Operation          PaymentOperation
	CaptureID          string
	DefinitivelyUnpaid bool
}

type operationState struct {
	PaymentOperation
	State     string
	CaptureID string
	Expires   int64
}

func readOperation(ctx context.Context, tx *sql.Tx, id string) (operationState, error) {
	var out operationState
	err := tx.QueryRowContext(ctx, `SELECT p.id,p.order_id,coalesce(p.connection,''),coalesce(p.provider_order,''),p.currency,p.amount_minor,p.state,
		coalesce(p.capture_id,''),o.expires_at FROM store_payment_operations p JOIN store_orders o ON o.id=p.order_id WHERE p.id=?`, id).
		Scan(&out.StableID, &out.Order, &out.Connection, &out.ProviderOrder, &out.Currency, &out.AmountMinor, &out.State, &out.CaptureID, &out.Expires)
	return out, err
}

func (s *Service) RecordPayment(ctx context.Context, e PaymentEvidence) error {
	if (e.CaptureID == "") != e.DefinitivelyUnpaid ||
		(e.CaptureID != "" && !validProviderID(e.CaptureID)) {
		return ErrPaymentEvidence
	}
	return s.write(ctx, "", func(tx *sql.Tx) error {
		op, err := readOperation(ctx, tx, e.Operation.StableID)
		if err != nil {
			return err
		}
		if op.PaymentOperation != e.Operation || op.Connection == "" {
			return ErrPaymentEvidence
		}
		now := time.Now()
		switch {
		case e.DefinitivelyUnpaid:
			if op.State != "uncertain" && op.State != "pending" {
				return nil
			}
			return releaseOrder(ctx, tx, op.Order, "verified_unpaid", now)
		case op.State == "paid" || op.State == "late_paid":
			if op.CaptureID == e.CaptureID {
				return nil
			}
			return ErrPaymentEvidence
		case op.State == "released":
			if _, err = tx.ExecContext(ctx, `UPDATE store_orders SET status='review' WHERE id=?`, op.Order); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE store_payment_operations SET state='late_paid',capture_id=? WHERE id=?`, e.CaptureID, op.StableID); err != nil {
				return err
			}
			return recordOrderEvent(ctx, tx, op.Order, "paid_after_release", now)
		case op.State == "uncertain" || op.State == "pending":
			if err = settleStock(ctx, tx, op.Order, true); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE store_orders SET status='confirmed',reservation_state='sold' WHERE id=?`, op.Order); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE store_payment_operations SET state='paid',capture_id=? WHERE id=?`, e.CaptureID, op.StableID); err != nil {
				return err
			}
			if err = recordOrderEvent(ctx, tx, op.Order, "paid", now); err != nil {
				return err
			}
			return outbox.RecordOrderPaid(ctx, tx, op.Order, now)
		default:
			return ErrPaymentEvidence
		}
	})
}

func settleStock(ctx context.Context, tx *sql.Tx, order string, sold bool) error {
	_, err := tx.ExecContext(ctx, `UPDATE store_variants AS v SET reserved=reserved-line.quantity,
		stock=stock-(line.quantity*?) FROM store_order_lines AS line
		WHERE line.order_id=? AND line.sku=v.sku`, sold, order)
	return err
}

func releaseOrder(ctx context.Context, tx *sql.Tx, id, reason string, now time.Time) error {
	if err := releaseUnpaidReservation(ctx, tx, id); err != nil {
		return err
	}
	return recordOrderEvent(ctx, tx, id, reason, now)
}

func releaseUnpaidReservation(ctx context.Context, tx *sql.Tx, id string) error {
	if err := settleStock(ctx, tx, id, false); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_orders SET status='expired',reservation_state='released' WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_payment_operations SET state='released' WHERE order_id=?`, id); err != nil {
		return err
	}
	return nil
}

func (s *Service) expireOrders(ctx context.Context, now time.Time) error {
	return s.write(ctx, "", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT o.id FROM store_orders o JOIN store_payment_operations p ON p.order_id=o.id
			WHERE o.reservation_state='held' AND o.expires_at<=? AND p.state='pending' ORDER BY o.expires_at,o.id LIMIT 50`, now.Unix())
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err = releaseOrder(ctx, tx, id, "expired_unpaid", now); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM store_checkout_rate WHERE window<?`, now.Unix()/900)
		return err
	})
}
