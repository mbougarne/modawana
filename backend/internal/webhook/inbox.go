package webhook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

// Accept returns only after a verified receipt is durable. A matching replay
// reports a duplicate. Conflicting bytes under the same event ID are rejected.
func (i *Inbox) Accept(ctx context.Context, adapter, connection string, headers http.Header, body []byte) (bool, error) {
	return i.accept(ctx, adapter, connection, headers, body, time.Now())
}

func (i *Inbox) accept(ctx context.Context, adapter, connection string, headers http.Header, body []byte, now time.Time) (bool, error) {
	c, ok := i.connections[connection]
	if !ok || c.Adapter != adapter {
		return false, ErrUnknown
	}
	if len(body) > MaxBody {
		return false, ErrInvalid
	}
	id, event, err := i.verifyStandardV1(c, headers, body, now)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(body)
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Recheck the durable connection in case it was disabled during verification.
	var enabled bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM webhook_connections
		WHERE id=? AND enabled=1 AND adapter=? AND account=? AND mode=?)`,
		c.ID, c.Adapter, c.Account, c.Mode).Scan(&enabled)
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, ErrUnknown
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO webhook_receipts
		(connection_id,event_id,body_digest,subject,subject_version,subject_status,received_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(connection_id,event_id) DO NOTHING`,
		c.ID, id, digest[:], event.Data.Subject, event.Data.Version, event.Data.Status, db.UTC(now))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		var previous []byte
		if err := tx.QueryRowContext(ctx, "SELECT body_digest FROM webhook_receipts WHERE connection_id=? AND event_id=?", c.ID, id).Scan(&previous); err != nil {
			return false, err
		}
		if !bytes.Equal(previous, digest[:]) {
			return false, ErrConflict
		}
	}
	return n == 0, tx.Commit()
}

const nextReceipt = `FROM webhook_receipts r JOIN webhook_connections c ON c.id=r.connection_id
	WHERE r.state='pending' AND c.enabled=1`

// process commits the reference adapter's state update and acknowledgement
// together. Local-only processing needs no lease. An interrupted transaction
// leaves the receipt pending for the next worker.
func (i *Inbox) process(ctx context.Context) (bool, error) {
	var ready bool
	if err := i.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 "+nextReceipt+")").Scan(&ready); err != nil || !ready {
		return false, err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var ordinal, version int64
	var connection, subject, status string
	err = tx.QueryRowContext(ctx, `SELECT r.ordinal,r.connection_id,r.subject,r.subject_version,r.subject_status `+nextReceipt+
		" ORDER BY r.ordinal LIMIT 1").Scan(&ordinal, &connection, &subject, &version, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO webhook_resource_status (connection_id,subject,version,status)
		VALUES(?,?,?,?) ON CONFLICT(connection_id,subject) DO UPDATE SET version=excluded.version,status=excluded.status
		WHERE excluded.version>webhook_resource_status.version`, connection, subject, version, status)
	if err != nil {
		return false, err
	}
	updates, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	state := "applied"
	if updates == 0 {
		var currentVersion int64
		var currentStatus string
		if err := tx.QueryRowContext(ctx, "SELECT version,status FROM webhook_resource_status WHERE connection_id=? AND subject=?",
			connection, subject).Scan(&currentVersion, &currentStatus); err != nil {
			return false, err
		}
		state = "superseded"
		if currentVersion == version && currentStatus != status {
			state = "conflict"
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE webhook_receipts SET state=? WHERE ordinal=?", state, ordinal); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (i *Inbox) Run(ctx context.Context, paused func() bool, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if paused == nil || !paused() {
			processed, err := i.process(ctx)
			if err != nil && ctx.Err() == nil && logger != nil {
				logger.Error("webhook inbox database operation failed")
			}
			if err == nil && processed {
				continue
			}
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

type Receipt struct {
	ID         int64  `json:"id"`
	Connection string `json:"connection"`
	Adapter    string `json:"adapter"`
	State      string `json:"state"`
	ReceivedAt string `json:"received_at"`
}

type Page struct {
	Items  []Receipt `json:"items"`
	Before int64     `json:"before,omitempty"`
}

// List exposes receipt status without signed headers, provider IDs or event data.
func (i *Inbox) List(ctx context.Context, before int64) (Page, error) {
	out := Page{Items: []Receipt{}}
	const limit = 50
	rows, err := i.db.QueryContext(ctx, `SELECT r.ordinal,c.id,c.adapter,
		CASE WHEN r.state='pending' AND c.enabled=0 THEN 'paused' ELSE r.state END,r.received_at
		FROM webhook_receipts r JOIN webhook_connections c ON c.id=r.connection_id
		WHERE (?=0 OR r.ordinal<?) ORDER BY r.ordinal DESC LIMIT ?`, before, before, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Items) == limit {
			out.Before = out.Items[limit-1].ID
			break
		}
		var item Receipt
		if err := rows.Scan(&item.ID, &item.Connection, &item.Adapter, &item.State, &item.ReceivedAt); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}
