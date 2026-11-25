package paypal

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/mbougarne/modawana/backend/internal/access"
)

const historyPath = "/v1/notifications/webhooks-events"
const pageLimit = 10
const recoveryWindow = 24 * time.Hour
const maxHistoryAge = 30 * 24 * time.Hour

type Recovery struct {
	Cutoff      int64                `json:"cutoff"`
	Target      int64                `json:"target"`
	Connections []RecoveryConnection `json:"connections"`
	Pending     int                  `json:"pending"`
	Unresolved  int                  `json:"unresolved"`
	Uncertain   int                  `json:"uncertain"`
	CanConfirm  bool                 `json:"can_confirm"`
	required    bool
}

type RecoveryConnection struct {
	ID        string `json:"id"`
	Mode      string `json:"mode"`
	Connected bool   `json:"connected"`
	Start     int64  `json:"start"`
	Through   int64  `json:"through"`
	Note      string `json:"note,omitempty"`
}

type recoveryReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Service) Recovery(ctx context.Context) (Recovery, error) {
	return s.readRecovery(ctx, s.store.DB)
}

func (s *Service) readRecovery(ctx context.Context, q recoveryReader) (Recovery, error) {
	out := Recovery{Connections: []RecoveryConnection{}}
	err := q.QueryRowContext(ctx, `SELECT r.snapshot_started_at,r.target_at,c.payment_review_required,
		(SELECT count(*) FROM store_paypal_receipts WHERE state='pending'),
		(SELECT count(*) FROM store_paypal_receipts WHERE state='review' AND reviewed_at=0),
		(SELECT count(*) FROM store_payment_operations WHERE state='uncertain') FROM store_paypal_recovery r,store_checkout_settings c
		WHERE r.id=1 AND c.id=1`).Scan(&out.Cutoff, &out.Target, &out.required, &out.Pending, &out.Unresolved, &out.Uncertain)
	if err != nil {
		return out, err
	}
	out.CanConfirm = out.required && out.Pending == 0 && out.Unresolved == 0
	rows, err := q.QueryContext(ctx, `SELECT c.id,c.mode,coalesce(r.start_at,0),coalesce(r.through_at,0),coalesce(r.note,'')
		FROM store_paypal_connections c LEFT JOIN store_paypal_recovery_coverage r ON r.connection=c.id ORDER BY c.id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c RecoveryConnection
		if err = rows.Scan(&c.ID, &c.Mode, &c.Start, &c.Through, &c.Note); err != nil {
			return out, err
		}
		c.Connected = s.HasConnection(c.ID)
		out.Connections = append(out.Connections, c)
		out.CanConfirm = out.CanConfirm && out.Target > 0 && c.Through >= out.Target
	}
	return out, rows.Err()
}

func (s *Service) ConfirmRecovery(ctx context.Context, fresh func(*sql.Tx) error) error {
	if fresh == nil {
		return ErrInvalid
	}
	return s.store.AcknowledgePaymentReview(ctx, func(tx *sql.Tx) error {
		if err := fresh(tx); err != nil {
			return err
		}
		review, err := s.readRecovery(ctx, tx)
		if err != nil {
			return err
		}
		if review.required && !review.CanConfirm {
			return ErrReview
		}
		return nil
	})
}

func recoveryNote(raw string) (string, bool) {
	note := strings.TrimSpace(raw)
	return note, note != "" && len([]rune(note)) <= 300 && !strings.ContainsFunc(note, unicode.IsControl)
}

func (s *Service) ReviewReceipt(ctx context.Context, id int64, raw string, fresh func(*sql.Tx) error) error {
	note, ok := recoveryNote(raw)
	if !ok || id < 1 || fresh == nil {
		return ErrInvalid
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fresh(tx); err != nil {
		return err
	}
	actor := access.Actor(ctx, "")
	if err = oneRecoveryChange(tx.ExecContext(ctx, `UPDATE store_paypal_receipts SET review_note=?,reviewed_by=?,
		reviewed_at=CASE WHEN reviewed_at=0 THEN ? ELSE reviewed_at END
		WHERE ordinal=? AND state='review' AND (reviewed_at=0 OR (review_note=? AND reviewed_by=?))`,
		note, actor, time.Now().Unix(), id, note, actor)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) RecordManualCoverage(ctx context.Context, id, raw string, fresh func(*sql.Tx) error) error {
	note, ok := recoveryNote(raw)
	if !ok || !connectionID.MatchString(id) || fresh == nil {
		return ErrInvalid
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fresh(tx); err != nil {
		return err
	}
	if err = oneRecoveryChange(tx.ExecContext(ctx, `INSERT INTO store_paypal_recovery_coverage(connection,start_at,through_at,note,actor)
		SELECT ?,r.snapshot_started_at,r.target_at,?,? FROM store_paypal_recovery r JOIN store_checkout_settings c ON c.id=1
		WHERE r.id=1 AND r.target_at>0 AND c.payment_review_required=1
		AND EXISTS(SELECT 1 FROM store_paypal_connections WHERE id=?)
		ON CONFLICT(connection) DO UPDATE SET through_at=excluded.through_at,note=excluded.note,actor=excluded.actor`,
		id, note, access.Actor(ctx, ""), id)); err != nil {
		return err
	}
	return tx.Commit()
}

func oneRecoveryChange(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrReview
	}
	return err
}

func (s *Service) RecoverEvents(ctx context.Context, id string) error {
	client := s.connections[id]
	if client == nil {
		return ErrReview
	}
	var cutoff, target, start, through int64
	var required bool
	err := s.store.DB.QueryRowContext(ctx, `SELECT r.snapshot_started_at,r.target_at,coalesce(h.start_at,0),coalesce(h.through_at,0),c.payment_review_required
		FROM store_paypal_recovery r JOIN store_checkout_settings c ON c.id=1 LEFT JOIN store_paypal_recovery_coverage h ON h.connection=?
		WHERE r.id=1`, id).Scan(&cutoff, &target, &start, &through, &required)
	if err != nil {
		return err
	}
	if !required || cutoff <= 0 || target < cutoff || time.Unix(cutoff, 0).Before(time.Now().Add(-maxHistoryAge)) {
		return ErrReview
	}
	if through >= target && through > 0 {
		return nil
	}
	from := cutoff
	if start != 0 {
		from = through + 1
	}
	until := min(target, from+int64(recoveryWindow/time.Second)-1)
	var events []event
	for range 6 {
		var complete bool
		events, complete, err = client.recoveryEvents(ctx, from, until)
		if err != nil {
			return err
		}
		if complete {
			return s.saveRecoveryBatch(ctx, client, events, cutoff, through, until, target)
		}
		if from == until {
			break
		}
		until = from + (until-from)/2
	}
	return ErrReview
}

func (c *client) recoveryEvents(ctx context.Context, from, until int64) ([]event, bool, error) {
	q := url.Values{
		"start_time": {time.Unix(from, 0).UTC().Format(time.RFC3339)},
		// An inclusive fractional end includes events throughout the final second.
		"end_time":  {time.Unix(until, 999999999).UTC().Format(time.RFC3339Nano)},
		"page_size": {"10"},
	}
	var page struct {
		Events *[]event `json:"events"`
		Count  *int     `json:"count"`
		Links  []link   `json:"links"`
	}
	if err := c.api(ctx, http.MethodGet, historyPath+"?"+q.Encode(), "", nil, &page); err != nil {
		return nil, false, err
	}
	for _, l := range page.Links {
		if l.Rel == "next" {
			return nil, false, nil
		}
	}
	if page.Events == nil {
		if page.Count != nil && *page.Count == 0 {
			return nil, true, nil
		}
		return nil, false, ErrMismatch
	}
	events := *page.Events
	if len(events) >= pageLimit || (page.Count != nil && *page.Count > len(events)) {
		return nil, false, nil
	}
	for _, e := range events {
		at, err := time.Parse(time.RFC3339, e.Created)
		if err != nil || at.Unix() < from || at.Unix() > until {
			return nil, false, ErrMismatch
		}
	}
	return events, true, nil
}

func (s *Service) saveRecoveryBatch(ctx context.Context, c *client, events []event, cutoff, previous, until, target int64) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range events {
		row, relevant, err := c.normalize(e)
		if err != nil {
			return err
		}
		if relevant {
			if err = saveReceipt(ctx, tx, c.config.ID, row, "", true); err != nil {
				return err
			}
		}
	}
	err = oneRecoveryChange(tx.ExecContext(ctx, `INSERT INTO store_paypal_recovery_coverage(connection,start_at,through_at)
		SELECT ?,?,? FROM store_paypal_recovery r JOIN store_checkout_settings c ON c.id=1
		WHERE r.id=1 AND r.target_at=? AND c.payment_review_required=1
		ON CONFLICT(connection) DO UPDATE SET through_at=excluded.through_at
		WHERE store_paypal_recovery_coverage.through_at=? AND store_paypal_recovery_coverage.note=''`,
		c.config.ID, cutoff, until, target, previous))
	if err != nil {
		return err
	}
	return tx.Commit()
}
