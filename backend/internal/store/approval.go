package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/mbougarne/modawana/backend/internal/media"
)

type Purchase struct {
	Operation   PaymentOperation
	Customer    Customer
	Country     string
	Locale      string
	ApprovalURL string
	State       string
	Expires     int64
}

const purchaseColumns = `p.id,o.id,a.connection,coalesce(p.provider_order,''),p.currency,p.amount_minor,p.state,
	o.expires_at,o.locale,o.customer,json_extract(o.snapshot,'$.country'),coalesce(a.approval_url,'')`
const purchaseTables = ` FROM store_paypal_approvals a JOIN store_payment_operations p ON p.id=a.operation
	JOIN store_orders o ON o.id=p.order_id`

func readPurchase(row scanner) (Purchase, error) {
	var out Purchase
	var customer string
	op := &out.Operation
	err := row.Scan(&op.StableID, &op.Order, &op.Connection, &op.ProviderOrder, &op.Currency, &op.AmountMinor, &out.State,
		&out.Expires, &out.Locale, &customer, &out.Country, &out.ApprovalURL)
	if err == nil {
		err = json.Unmarshal([]byte(customer), &out.Customer)
	}
	return out, err
}

func (s *Service) GuestPurchase(ctx context.Context, guest, order string) (Purchase, error) {
	if !media.ID.MatchString(guest) || !media.ID.MatchString(order) {
		return Purchase{}, sql.ErrNoRows
	}
	return readPurchase(s.DB.QueryRowContext(ctx, "SELECT "+purchaseColumns+purchaseTables+" WHERE o.id=? AND o.guest_hash=?", order, hash([]byte(guest))))
}

func (s *Service) MerchantPurchase(ctx context.Context, order string) (Purchase, error) {
	return readPurchase(s.DB.QueryRowContext(ctx, "SELECT "+purchaseColumns+purchaseTables+" WHERE o.id=?", order))
}

func (s *Service) ProviderPurchase(ctx context.Context, connection, order, capture string) (Purchase, error) {
	if connection == "" || (order == "" && capture == "") {
		return Purchase{}, sql.ErrNoRows
	}
	return readPurchase(s.DB.QueryRowContext(ctx, "SELECT "+purchaseColumns+purchaseTables+
		" WHERE a.connection=? AND ((?<>'' AND p.provider_order=?) OR (?='' AND p.capture_id=?))",
		connection, order, order, order, capture))
}

func (s *Service) CheckPurchaseOpen(ctx context.Context, p Purchase) error {
	var enabled bool
	err := s.DB.QueryRowContext(ctx, `SELECT s.enabled AND c.enabled AND c.payment_review_required=0 FROM store_settings s,store_checkout_settings c WHERE s.id=1 AND c.id=1`).Scan(&enabled)
	if err != nil {
		return err
	}
	if !s.checkoutOpen(enabled, p.Operation.Currency) || s.PaymentConnection == nil || s.PaymentConnection(p.Operation.Currency) != p.Operation.Connection {
		return ErrCheckoutClosed
	}
	return nil
}

// BindApproval records the provider identifier before the approval link can
// be sent to a buyer. If the HTTP response is lost, the same provider request
// ID recovers the order, and concurrent callers can only set the same binding.
func (s *Service) BindApproval(ctx context.Context, p Purchase, result PaymentApproval) error {
	if !validProviderID(result.ProviderOrder) || result.ApprovalURL == "" || len(result.ApprovalURL) > 2048 {
		return ErrInvalid
	}
	return s.write(ctx, "", func(tx *sql.Tx) error {
		current, err := readPurchase(tx.QueryRowContext(ctx, "SELECT "+purchaseColumns+purchaseTables+" WHERE p.id=?", p.Operation.StableID))
		if err != nil {
			return err
		}
		if current.Operation.Connection != p.Operation.Connection {
			return ErrPaymentEvidence
		}
		if current.Operation.ProviderOrder != "" {
			if current.Operation.ProviderOrder != result.ProviderOrder {
				return ErrPaymentEvidence
			}
			return nil
		}
		if current.State != "pending" && current.State != "released" {
			return ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE store_payment_operations SET connection=?,provider_order=? WHERE id=?`,
			p.Operation.Connection, result.ProviderOrder, p.Operation.StableID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE store_paypal_approvals SET approval_url=? WHERE operation=?`, result.ApprovalURL, p.Operation.StableID)
		return err
	})
}

// StartProviderCapture admits a new capture or a bounded exact replay. It
// checks current checkout settings and records the effect before network I/O.
func (s *Service) StartProviderCapture(ctx context.Context, p Purchase) (PaymentOperation, error) {
	if p.Operation.ProviderOrder == "" {
		return PaymentOperation{}, ErrConflict
	}
	var out PaymentOperation
	err := s.write(ctx, "", func(tx *sql.Tx) error {
		current, err := readPurchase(tx.QueryRowContext(ctx, "SELECT "+purchaseColumns+purchaseTables+" WHERE p.id=?", p.Operation.StableID))
		if err != nil {
			return err
		}
		if current.Operation != p.Operation {
			return ErrPaymentEvidence
		}
		_, _, err = s.checkout(ctx, tx, current.Locale)
		if err != nil {
			return err
		}
		if s.PaymentConnection == nil || s.PaymentConnection(current.Operation.Currency) != current.Operation.Connection {
			return ErrCheckoutClosed
		}
		now := time.Now()
		switch current.State {
		case "uncertain":
			var start int64
			if err = tx.QueryRowContext(ctx, `SELECT created_at FROM store_order_events WHERE order_id=? AND kind='capture_pending' ORDER BY id LIMIT 1`,
				current.Operation.Order).Scan(&start); err != nil {
				return err
			}
			if now.Unix() >= start+int64(reservationLifetime/time.Second) {
				return ErrConflict
			}
		case "pending":
			if now.Unix() >= current.Expires {
				return ErrConflict
			}
			if _, err = tx.ExecContext(ctx, `UPDATE store_payment_operations SET state='uncertain' WHERE id=?`, current.Operation.StableID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE store_orders SET status='review' WHERE id=?`, current.Operation.Order); err != nil {
				return err
			}
			if err = recordOrderEvent(ctx, tx, current.Operation.Order, "capture_pending", now); err != nil {
				return err
			}
		default:
			return ErrConflict
		}
		out = current.Operation
		return nil
	})
	return out, err
}
