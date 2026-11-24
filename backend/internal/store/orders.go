package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
)

type Customer struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Address    string `json:"address"`
	Address2   string `json:"address_2"`
	City       string `json:"city"`
	Region     string `json:"region"`
	PostalCode string `json:"postal_code"`
}

type OrderInput struct {
	QuoteInput
	Customer         Customer `json:"customer"`
	Key              string   `json:"key"`
	QuoteFingerprint string   `json:"quote_fingerprint"`
}

type OrderReceipt struct {
	Reference string `json:"reference"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type OrderStatus struct {
	OrderReceipt
	Snapshot    Snapshot `json:"snapshot"`
	Status      string   `json:"status"`
	Payment     string   `json:"payment"`
	Refund      string   `json:"refund"`
	Fulfillment string   `json:"fulfillment"`
}

type MerchantOrder struct {
	OrderStatus
	Customer    Customer     `json:"customer"`
	Locale      string       `json:"locale"`
	Operation   string       `json:"operation"`
	Reservation string       `json:"reservation"`
	Events      []OrderEvent `json:"events"`
}

type OrderEvent struct {
	Kind      string `json:"kind"`
	CreatedAt int64  `json:"created_at"`
}

type OrderListItem struct {
	OrderReceipt
	Status string `json:"status"`
}

type OrderPage struct {
	Orders []OrderListItem `json:"orders"`
	Next   string          `json:"next,omitempty"`
}

func (c Customer) valid() bool {
	address, err := mail.ParseAddress(c.Email)
	return err == nil && address.Name == "" && address.Address == c.Email &&
		plain(c.Email, 254, true, false) && plain(c.Name, 120, true, false) &&
		plain(c.Address, 180, true, false) && plain(c.Address2, 180, false, false) &&
		plain(c.City, 100, true, false) && plain(c.Region, 100, false, false) &&
		plain(c.PostalCode, 24, false, false)
}

func (s *Service) CreateOrder(ctx context.Context, guest string, in OrderInput) (OrderReceipt, error) {
	var receipt OrderReceipt
	if !media.ID.MatchString(guest) || !media.ID.MatchString(in.Key) {
		return receipt, ErrInvalid
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return receipt, err
	}
	mac := hmac.New(sha256.New, []byte(guest))
	_, _ = mac.Write(raw)
	fingerprint := hex.EncodeToString(mac.Sum(nil))
	guestHash := hash([]byte(guest))
	err = s.write(ctx, "", func(tx *sql.Tx) error {
		var prior string
		err := tx.QueryRowContext(ctx, `SELECT id,created_at,expires_at,fingerprint FROM store_orders WHERE guest_hash=? AND retry_key=?`, guestHash, in.Key).
			Scan(&receipt.Reference, &receipt.CreatedAt, &receipt.ExpiresAt, &prior)
		if err == nil {
			if !hmac.Equal([]byte(prior), []byte(fingerprint)) {
				return ErrOrderReplay
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !in.Customer.valid() || len(in.QuoteFingerprint) != 64 {
			return ErrInvalid
		}
		now := time.Now()
		var held int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM store_orders WHERE guest_hash=? AND reservation_state='held'`, guestHash).Scan(&held); err != nil {
			return err
		}
		if held >= 3 {
			return ErrLimited
		}
		quote, err := quoteCart(ctx, tx, in.QuoteInput)
		if err != nil {
			return err
		}
		if in.QuoteFingerprint != quote.Fingerprint {
			return ErrQuoteChanged
		}
		receipt, err = insertOrder(ctx, tx, guestHash, fingerprint, in, quote.Snapshot, now)
		return err
	})
	if err != nil {
		return OrderReceipt{}, err
	}
	return receipt, nil
}

func insertOrder(ctx context.Context, tx *sql.Tx, guestHash, fingerprint string, in OrderInput, snapshot Snapshot, now time.Time) (OrderReceipt, error) {
	id, err := db.NewID()
	if err != nil {
		return OrderReceipt{}, err
	}
	operation, err := db.NewID()
	if err != nil {
		return OrderReceipt{}, err
	}
	out := OrderReceipt{id, now.Unix(), now.Add(reservationLifetime).Unix()}
	customer, _ := json.Marshal(in.Customer)
	totals, _ := json.Marshal(snapshot)
	if _, err = tx.ExecContext(ctx, `INSERT INTO store_orders(id,guest_hash,retry_key,fingerprint,locale,customer,snapshot,
		status,reservation_state,created_at,expires_at) VALUES(?,?,?,?,?,?,?,'awaiting_payment','held',?,?)`,
		id, guestHash, in.Key, fingerprint, in.Locale, string(customer), string(totals), out.CreatedAt, out.ExpiresAt); err != nil {
		return OrderReceipt{}, err
	}
	for _, line := range snapshot.Lines {
		if _, err = tx.ExecContext(ctx, `UPDATE store_variants SET reserved=reserved+? WHERE sku=?`, line.Quantity, line.SKU); err != nil {
			return OrderReceipt{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO store_order_lines(order_id,sku,quantity) VALUES(?,?,?)`, id, line.SKU, line.Quantity); err != nil {
			return OrderReceipt{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO store_payment_operations(id,order_id,amount_minor,currency,state)
		VALUES(?,?,?,?,'pending')`, operation, id, snapshot.TotalMinor, snapshot.Currency); err != nil {
		return OrderReceipt{}, err
	}
	return out, recordOrderEvent(ctx, tx, id, "reserved", now)
}

func recordOrderEvent(ctx context.Context, tx *sql.Tx, id, kind string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO store_order_events(order_id,kind,created_at) VALUES(?,?,?)`, id, kind, now.Unix())
	return err
}

const orderStatusColumns = `o.id,o.created_at,o.expires_at,o.snapshot,o.status,p.state,o.refund_state,o.fulfillment_state`
const orderTables = ` FROM store_orders o JOIN store_payment_operations p ON p.order_id=o.id`

func (s *Service) GuestOrder(ctx context.Context, guest, id string) (OrderStatus, error) {
	var out OrderStatus
	if !media.ID.MatchString(guest) || !media.ID.MatchString(id) {
		return out, sql.ErrNoRows
	}
	var snapshot string
	err := s.DB.QueryRowContext(ctx, "SELECT "+orderStatusColumns+orderTables+" WHERE o.id=? AND o.guest_hash=?", id, hash([]byte(guest))).
		Scan(&out.Reference, &out.CreatedAt, &out.ExpiresAt, &snapshot, &out.Status, &out.Payment, &out.Refund, &out.Fulfillment)
	if err == nil {
		err = json.Unmarshal([]byte(snapshot), &out.Snapshot)
	}
	return out, err
}

func (s *Service) GuestReceipt(ctx context.Context, guest, key string) (OrderReceipt, error) {
	var out OrderReceipt
	if !media.ID.MatchString(guest) || !media.ID.MatchString(key) {
		return out, sql.ErrNoRows
	}
	err := s.DB.QueryRowContext(ctx, `SELECT id,created_at,expires_at FROM store_orders WHERE guest_hash=? AND retry_key=?`, hash([]byte(guest)), key).
		Scan(&out.Reference, &out.CreatedAt, &out.ExpiresAt)
	return out, err
}

func (s *Service) Orders(ctx context.Context, cursor string) (OrderPage, error) {
	out := OrderPage{Orders: []OrderListItem{}}
	if cursor != "" && !media.ID.MatchString(cursor) {
		return out, ErrInvalid
	}
	statement := `SELECT id,created_at,expires_at,status FROM store_orders`
	var args []any
	if cursor != "" {
		statement += ` WHERE (created_at,id)<(SELECT created_at,id FROM store_orders WHERE id=?)`
		args = append(args, cursor)
	}
	rows, err := s.DB.QueryContext(ctx, statement+` ORDER BY created_at DESC,id DESC LIMIT 31`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Orders) == 30 {
			out.Next = out.Orders[29].Reference
			break
		}
		var row OrderListItem
		if err = rows.Scan(&row.Reference, &row.CreatedAt, &row.ExpiresAt, &row.Status); err != nil {
			return out, err
		}
		out.Orders = append(out.Orders, row)
	}
	return out, rows.Err()
}

func (s *Service) MerchantOrder(ctx context.Context, id string) (MerchantOrder, error) {
	out := MerchantOrder{Events: []OrderEvent{}}
	if !media.ID.MatchString(id) {
		return out, sql.ErrNoRows
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var snapshot, customer string
	err = tx.QueryRowContext(ctx, "SELECT "+orderStatusColumns+",o.customer,o.locale,p.id,o.reservation_state"+orderTables+" WHERE o.id=?", id).
		Scan(&out.Reference, &out.CreatedAt, &out.ExpiresAt, &snapshot, &out.Status, &out.Payment, &out.Refund, &out.Fulfillment,
			&customer, &out.Locale, &out.Operation, &out.Reservation)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(snapshot), &out.Snapshot); err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(customer), &out.Customer); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,created_at FROM store_order_events WHERE order_id=? ORDER BY id`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var row OrderEvent
		if err = rows.Scan(&row.Kind, &row.CreatedAt); err != nil {
			return out, err
		}
		out.Events = append(out.Events, row)
	}
	return out, rows.Err()
}

func validProviderID(value string) bool {
	return plain(value, 128, true, false) && strings.TrimSpace(value) == value
}
