package outbox

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

var ErrInvalid = errors.New("invalid delivery lookup")

type Status struct {
	ID          string `json:"id"`
	Plugin      string `json:"plugin"`
	Event       string `json:"event"`
	State       string `json:"state"`
	Attempts    int    `json:"attempts"`
	UpdatedAt   string `json:"updated_at"`
	NextAttempt string `json:"next_attempt,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	CanRetry    bool   `json:"can_retry"`
}

type StatusPage struct {
	Items []Status `json:"items"`
	Next  string   `json:"next,omitempty"`
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// List returns status metadata only. Event data, settings, lease tokens and
// invocation errors stay out of owner diagnostics.
func (w *Worker) List(ctx context.Context, cursor string) (StatusPage, error) {
	page := StatusPage{Items: []Status{}}
	var before int64
	var beforeID string
	if cursor != "" {
		stamp, id, ok := strings.Cut(cursor, ":")
		n, err := strconv.ParseInt(stamp, 10, 64)
		if !ok || err != nil || n <= 0 || !validID(id) {
			return page, ErrInvalid
		}
		before, beforeID = n, id
	}
	const limit = 25
	rows, err := w.DB.QueryContext(ctx, `SELECT d.id,d.plugin_slug,e.name,d.state,d.attempts,
		d.updated_at,d.available_at,d.last_error,EXISTS (SELECT 1 `+eligibleFrom+`)
		FROM outbox_deliveries d JOIN outbox_events e ON e.id=d.event_id
		WHERE (?=0 OR (d.updated_at,d.id)<(?,?)) ORDER BY d.updated_at DESC,d.id DESC LIMIT ?`,
		before, before, beforeID, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	var updated int64
	for rows.Next() {
		if len(page.Items) == limit {
			page.Next = strconv.FormatInt(updated, 10) + ":" + page.Items[limit-1].ID
			break
		}
		var item Status
		var available int64
		var eligible bool
		if err := rows.Scan(&item.ID, &item.Plugin, &item.Event, &item.State, &item.Attempts,
			&updated, &available, &item.LastError, &eligible); err != nil {
			return page, err
		}
		item.UpdatedAt = db.UTC(time.UnixMilli(updated))
		item.CanRetry = eligible && item.State == "failed"
		if !eligible && (item.State == "pending" || item.State == "running") {
			item.State = "paused"
		}
		if item.State == "pending" {
			item.NextAttempt = db.UTC(time.UnixMilli(available))
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

// Retry renews the attempt budget of a failed delivery. Its exact event and
// stable operation ID are preserved.
func (w *Worker) Retry(ctx context.Context, id string) error {
	if !validID(id) {
		return ErrInvalid
	}
	now := w.clock().UnixMilli()
	result, err := w.DB.ExecContext(ctx, `UPDATE outbox_deliveries AS d SET
		state='pending',attempts=0,last_error='',available_at=?,updated_at=?
		WHERE d.id=? AND d.state='failed' AND EXISTS (SELECT 1 `+eligibleFrom+`)`, now, now, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return db.ErrVersionConflict
	}
	return nil
}
