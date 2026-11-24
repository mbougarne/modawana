package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/outbox"
)

var ErrOrderState = errors.New("order action is unavailable in the current state")

type MerchantInput struct {
	ID             string `json:"id"`
	Reason         string `json:"reason,omitempty"`
	Carrier        string `json:"carrier,omitempty"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	SKU            string `json:"sku,omitempty"`
	Quantity       int64  `json:"quantity,omitempty"`
}

type MerchantOperation struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	CreatedAt int64  `json:"created_at"`
}

type Shipment struct {
	ShippedAt      int64  `json:"shipped_at"`
	Carrier        string `json:"carrier"`
	TrackingNumber string `json:"tracking_number"`
}

type Notice struct {
	Kind      string `json:"kind"`
	CreatedAt int64  `json:"created_at"`
}

func (i MerchantInput) valid(kind string) bool {
	tracking := i.Carrier != "" || i.TrackingNumber != ""
	switch kind {
	case "cancelled":
		return !tracking && i.SKU == "" && i.Quantity == 0 && plain(i.Reason, 300, true, false)
	case "shipped", "tracking_updated":
		return i.SKU == "" && i.Quantity == 0 && i.Reason == "" &&
			(!tracking || plain(i.Carrier, 80, true, false) && plain(i.TrackingNumber, 120, true, false))
	case "restocked":
		return !tracking && skuPattern.MatchString(i.SKU) && i.Quantity >= 1 && i.Quantity <= 99 && plain(i.Reason, 300, true, false)
	}
	return false
}

func (s *Service) MerchantAction(ctx context.Context, owner, id, kind string, in MerchantInput) (MerchantOperation, error) {
	if !media.ID.MatchString(id) || !media.ID.MatchString(in.ID) {
		return MerchantOperation{}, ErrInvalid
	}
	in.Reason = strings.TrimSpace(in.Reason)
	in.Carrier = strings.TrimSpace(in.Carrier)
	in.TrackingNumber = strings.TrimSpace(in.TrackingNumber)
	if !in.valid(kind) {
		return MerchantOperation{}, ErrInvalid
	}
	actor := access.Actor(ctx, owner)
	data, _ := json.Marshal(in)
	out := MerchantOperation{ID: in.ID, Kind: kind}
	err := s.write(ctx, id, func(tx *sql.Tx) error {
		var previousID, previousActor, previousKind, previousData string
		err := tx.QueryRowContext(ctx, "SELECT order_id,actor,kind,data,created_at FROM store_merchant_operations WHERE id=?", in.ID).
			Scan(&previousID, &previousActor, &previousKind, &previousData, &out.CreatedAt)
		if err == nil {
			if previousID != id || previousActor != actor || previousKind != kind || previousData != string(data) {
				return ErrOrderReplay
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var status, payment, refund, fulfillment, reservation string
		err = tx.QueryRowContext(ctx, `SELECT o.status,p.state,o.refund_state,o.fulfillment_state,o.reservation_state`+orderTables+
			" WHERE o.id=?", id).Scan(&status, &payment, &refund, &fulfillment, &reservation)
		if err != nil {
			return err
		}
		now := time.Now()
		switch kind {
		case "cancelled":
			if err = cancelOrder(ctx, tx, id, status, payment, fulfillment); err != nil {
				return err
			}
		case "shipped":
			if status != "confirmed" || payment != "paid" || reservation != "sold" || fulfillment != "unfulfilled" ||
				refund == "pending" || refund == "refunded" {
				return ErrOrderState
			}
			if err = shipOrder(ctx, tx, id, in, now); err != nil {
				return err
			}
		case "tracking_updated":
			if fulfillment != "shipped" {
				return ErrOrderState
			}
			if err = changed(tx.ExecContext(ctx, `UPDATE store_shipments SET carrier=?,tracking_number=? WHERE order_id=?`, in.Carrier, in.TrackingNumber, id)); err != nil {
				return err
			}
		case "restocked":
			if reservation != "sold" || (fulfillment == "unfulfilled" && refund != "refunded") {
				return ErrOrderState
			}
			if err = restockOrder(ctx, tx, id, actor, in, now); err != nil {
				return err
			}
		}
		out.CreatedAt = now.Unix()
		_, err = tx.ExecContext(ctx, `INSERT INTO store_merchant_operations(id,order_id,actor,kind,data,created_at)
			VALUES(?,?,?,?,?,?)`, in.ID, id, actor, kind, string(data), out.CreatedAt)
		if err != nil {
			return err
		}
		return recordMerchantEvent(ctx, tx, id, kind, actor, string(data), out.CreatedAt)
	})
	return out, err
}

func cancelOrder(ctx context.Context, tx *sql.Tx, id, status, payment, fulfillment string) error {
	if fulfillment != "unfulfilled" || (status != "awaiting_payment" && status != "confirmed") ||
		(payment != "pending" && payment != "paid") {
		return ErrOrderState
	}
	if payment == "pending" {
		// A pending operation has not begun a capture. Leave any provider
		// binding intact so verified late success moves to review.
		if err := releaseUnpaidReservation(ctx, tx, id); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE store_orders SET fulfillment_state='cancelled' WHERE id=?`, id)
	return err
}

func shipOrder(ctx context.Context, tx *sql.Tx, id string, in MerchantInput, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE store_orders SET fulfillment_state='shipped' WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO store_shipments(order_id,shipped_at,carrier,tracking_number) VALUES(?,?,?,?)`,
		id, now.Unix(), in.Carrier, in.TrackingNumber); err != nil {
		return err
	}
	return outbox.RecordOrderShipped(ctx, tx, id, now)
}

func restockOrder(ctx context.Context, tx *sql.Tx, id, actor string, in MerchantInput, now time.Time) error {
	res, err := tx.ExecContext(ctx, `UPDATE store_order_lines SET restocked=restocked+? WHERE order_id=? AND sku=? AND restocked+?<=quantity`,
		in.Quantity, id, in.SKU, in.Quantity)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrOrderState
	}
	adjustment := Adjustment{SKU: in.SKU, Actor: actor, AdjustmentInput: AdjustmentInput{ID: in.ID, Delta: in.Quantity, Reason: in.Reason}}
	return recordAdjustment(ctx, tx, &adjustment, now)
}

func PrepareNotice(_ context.Context, job outbox.Delivery) (outbox.Effect, error) {
	if job.Event.Version != 1 || !media.ID.MatchString(job.Event.SourceID) ||
		(job.Event.Name != outbox.OrderPaid && job.Event.Name != outbox.OrderShipped) {
		return nil, outbox.ErrRejected
	}
	return func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO store_order_notices(order_id,kind,created_at)
			SELECT source_id,name,created_at/1000 FROM outbox_events WHERE id=?
			ON CONFLICT(order_id,kind) DO NOTHING`, job.Event.ID)
		return err
	}, nil
}
