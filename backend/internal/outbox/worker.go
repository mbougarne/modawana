package outbox

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

const (
	maxAttempts     = 5
	leaseDuration   = time.Minute
	deliveryTimeout = 15 * time.Second
)

var ErrUnavailable = errors.New("subscriber unavailable")
var ErrRejected = errors.New("subscriber rejected the event")

// Delivery is the handler input. ID is the stable provider operation ID across
// retries, restarts and operator requeues. Lease tokens are private to the host.
type Delivery struct {
	ID, PluginID, PluginSlug string
	Event                    Event
}

// Effect applies only local SQLite changes. A handler prepares the effect
// before a write transaction opens. Provider adapters may only deliver with
// this job's stable idempotency key or reconcile the same operation on retry.
type Effect func(context.Context, *sql.Tx) error
type Handler func(context.Context, Delivery) (Effect, error)

type Worker struct {
	DB     *sql.DB
	Handle Handler
	Paused func() bool
	Logger *slog.Logger
	now    func() time.Time
}

type attempt struct {
	delivery Delivery
	token    string
	number   int
	approval int64
}

func (w *Worker) clock() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

func (w *Worker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if w.Paused == nil || !w.Paused() {
			found, err := w.step(ctx)
			if err != nil && ctx.Err() == nil && w.Logger != nil {
				w.Logger.Error("outbox worker database operation failed")
			}
			if err == nil && found {
				continue
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (w *Worker) step(ctx context.Context) (bool, error) {
	a, err := w.claim(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	delivery, cancel := context.WithTimeout(ctx, deliveryTimeout)
	effect, failure := prepare(delivery, w.Handle, a.delivery)
	if delivery.Err() != nil {
		failure = delivery.Err()
	}
	cancel()
	if ctx.Err() != nil {
		// Leave the lease intact. The next worker can resume after expiration.
		return true, ctx.Err()
	}
	if failure == nil {
		failure, err = w.complete(ctx, a, effect)
		if err != nil {
			return true, err
		}
	}
	if failure != nil {
		return true, w.fail(ctx, a, failure)
	}
	return true, nil
}

func prepare(ctx context.Context, handler Handler, delivery Delivery) (effect Effect, err error) {
	defer func() {
		if recover() != nil {
			effect, err = nil, ErrRejected
		}
	}()
	return handler(ctx, delivery)
}

// A subscription, enabled install, and contract all have to match. Deliveries
// survive removal for diagnostics, but a new install cannot inherit them.
const eligibleFrom = `FROM plugin_installs p JOIN outbox_subscriptions s ON s.plugin_id=p.id
	JOIN outbox_events e ON e.name=s.name AND e.contract_version=s.contract_version
	WHERE p.id=d.plugin_id AND p.enabled=1 AND e.id=d.event_id
	  AND (e.staff_actor_id IS NULL OR EXISTS (SELECT 1 FROM staff_members m
	    WHERE m.id=e.staff_actor_id AND m.revoked_at IS NULL AND m.role='owner'))`

func (w *Worker) claim(ctx context.Context) (attempt, error) {
	var a attempt
	now := w.clock()
	var ready bool
	const due = `d.attempts<? AND ((d.state='pending' AND d.available_at<=?) OR (d.state='running' AND d.lease_until<=?))
		AND EXISTS (SELECT 1 ` + eligibleFrom + `)`
	err := w.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM outbox_deliveries d WHERE `+due+`)
		OR EXISTS (SELECT 1 FROM outbox_deliveries WHERE state='running' AND lease_until<=? AND attempts>=?)`,
		maxAttempts, now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), maxAttempts).Scan(&ready)
	if err != nil {
		return a, err
	}
	if !ready {
		return a, sql.ErrNoRows
	}
	token, err := db.NewID()
	if err != nil {
		return a, err
	}
	tx, err := w.DB.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	now = w.clock()
	if _, err := tx.ExecContext(ctx, `UPDATE outbox_deliveries SET state='failed',last_error='lease_expired',
		lease_until=0,lease_token='',updated_at=? WHERE id IN (
		  SELECT id FROM outbox_deliveries WHERE state='running' AND lease_until<=? AND attempts>=? LIMIT 50
		)`, now.UnixMilli(), now.UnixMilli(), maxAttempts); err != nil {
		return a, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, `SELECT d.id,d.plugin_id,d.plugin_slug,d.attempts,p.version,
		e.id,e.name,e.contract_version,e.source_id,e.source_version,e.payload
		FROM outbox_deliveries d JOIN outbox_events e ON e.id=d.event_id JOIN plugin_installs p ON p.id=d.plugin_id
		WHERE `+due+`
		ORDER BY CASE WHEN d.state='pending' THEN d.available_at ELSE d.lease_until END,d.id LIMIT 1`,
		maxAttempts, now.UnixMilli(), now.UnixMilli()).Scan(
		&a.delivery.ID, &a.delivery.PluginID, &a.delivery.PluginSlug, &a.number, &a.approval,
		&a.delivery.Event.ID, &a.delivery.Event.Name, &a.delivery.Event.Version,
		&a.delivery.Event.SourceID, &a.delivery.Event.SourceVersion, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		if commitErr := tx.Commit(); commitErr != nil {
			return a, commitErr
		}
		return a, err
	}
	if err != nil {
		return a, err
	}
	a.delivery.Event.Data = []byte(payload)
	a.token = token
	a.number++
	if _, err := tx.ExecContext(ctx, `UPDATE outbox_deliveries SET state='running',attempts=?,lease_token=?,
		lease_until=?,updated_at=? WHERE id=?`, a.number, token, now.Add(leaseDuration).UnixMilli(), now.UnixMilli(), a.delivery.ID); err != nil {
		return a, err
	}
	return a, tx.Commit()
}

// complete fences old attempts before applying any local effect. The receipt
// and the effect commit together so redelivery cannot repeat the effect.
func (w *Worker) complete(ctx context.Context, a attempt, effect Effect) (failure, dbErr error) {
	tx, err := w.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := w.clock().UnixMilli()
	result, err := tx.ExecContext(ctx, `UPDATE outbox_deliveries AS d SET state='succeeded',last_error='',
		lease_until=0,lease_token='',updated_at=?
		WHERE d.id=? AND d.lease_token=? AND d.lease_until>? AND d.state='running'
		AND EXISTS (SELECT 1 `+eligibleFrom+` AND p.version=?)`, now, a.delivery.ID, a.token, now, a.approval)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return ErrUnavailable, nil
	}
	if effect != nil {
		if err := apply(ctx, tx, effect); err != nil {
			return ErrRejected, nil
		}
	}
	return nil, tx.Commit()
}

func apply(ctx context.Context, tx *sql.Tx, effect Effect) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrRejected
		}
	}()
	return effect(ctx, tx)
}

func (w *Worker) fail(ctx context.Context, a attempt, failure error) error {
	reason := "delivery_failed"
	switch {
	case errors.Is(failure, ErrUnavailable):
		reason = "subscriber_unavailable"
	case errors.Is(failure, ErrRejected):
		reason = "effect_rejected"
	case errors.Is(failure, context.DeadlineExceeded):
		reason = "timed_out"
	}
	state := "pending"
	if a.number >= maxAttempts {
		state = "failed"
	}
	now := w.clock()
	delay := (5 * time.Second) << (a.number - 1)
	_, err := w.DB.ExecContext(ctx, `UPDATE outbox_deliveries SET state=?,last_error=?,available_at=?,
		lease_until=0,lease_token='',updated_at=?
		WHERE id=? AND state='running' AND lease_token=? AND lease_until>?`,
		state, reason, now.Add(delay).UnixMilli(), now.UnixMilli(), a.delivery.ID, a.token, now.UnixMilli())
	return err
}
