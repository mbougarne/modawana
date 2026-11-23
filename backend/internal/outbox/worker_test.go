package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

const fixtureID = "00000000000000000000000000000001"

func queueFixture(t *testing.T, now time.Time) (string, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	d := openQueue(t, root)
	sqlExec(t, d, `INSERT INTO plugin_installs(id,slug,name,plugin_version,api_version,manifest,files,enabled,installed_at,updated_at)
		VALUES(?,'example','Example','1',5,'{}','[]',1,?,?)`, fixtureID, db.UTC(now), db.UTC(now))
	sqlExec(t, d, "INSERT INTO outbox_subscriptions VALUES(?,?,?)", fixtureID, ContentPublished, ContentPublishedVersion)
	sqlExec(t, d, "CREATE TABLE effects (operation_id TEXT NOT NULL)")
	tx, err := d.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := RecordPublication(t.Context(), tx, "source", 2, Publication{ContentID: "private-data", Locale: "en", RevisionID: "revision"}, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return root, d
}

func openQueue(t *testing.T, root string) *sql.DB {
	t.Helper()
	d, err := db.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func sqlExec(t *testing.T, d *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := d.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func requireEffectCount(t *testing.T, d *sql.DB, expected int) {
	t.Helper()
	var n int
	if err := d.QueryRow("SELECT count(*) FROM effects").Scan(&n); err != nil || n != expected {
		t.Fatal("effects", n, err)
	}
}

func TestDeliveryRecoversAfterWorkerStopsDuringDelivery(t *testing.T) {
	now := time.Now()
	root, d := queueFixture(t, now)
	workerCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan Delivery, 1)
	stopped := make(chan error, 1)
	w := Worker{DB: d, now: func() time.Time { return now }, Handle: func(ctx context.Context, delivery Delivery) (Effect, error) {
		started <- delivery
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	go func() {
		_, err := w.step(workerCtx)
		stopped <- err
	}()
	var first Delivery
	select {
	case first = <-started:
	case err := <-stopped:
		t.Fatal("worker exited before delivery", err)
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not start")
	}
	cancel()
	if err := <-stopped; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	requireEffectCount(t, d, 0)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	d = openQueue(t, root)
	next := Worker{DB: d, now: func() time.Time { return now }, Handle: func(ctx context.Context, retry Delivery) (Effect, error) {
		if retry.ID != first.ID || retry.Event.ID != first.Event.ID || string(retry.Event.Data) != string(first.Event.Data) {
			t.Fatal("retry changed the operation", retry)
		}
		return func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO effects VALUES(?)", retry.ID)
			return err
		}, nil
	}}
	if worked, err := next.step(t.Context()); worked || err != nil {
		t.Fatal("lease was taken before expiry", worked, err)
	}
	now = now.Add(leaseDuration + time.Second)
	if worked, err := next.step(t.Context()); !worked || err != nil {
		t.Fatal("expired delivery was not recovered", worked, err)
	}
	if worked, err := next.step(t.Context()); worked || err != nil {
		t.Fatal("successful delivery was replayed", worked, err)
	}
	requireEffectCount(t, d, 1)
	page, err := next.List(t.Context(), "")
	if err != nil || len(page.Items) != 1 || page.Items[0].State != "succeeded" || page.Items[0].Attempts != 2 {
		t.Fatal(page, err)
	}
}

func TestExpiredAttemptCannotApplyLocalEffect(t *testing.T) {
	now := time.Now()
	_, d := queueFixture(t, now)
	w := Worker{DB: d, now: func() time.Time { return now }}
	old, err := w.claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(leaseDuration + time.Second)
	current, err := w.claim(t.Context())
	if err != nil || current.delivery.ID != old.delivery.ID || current.token == old.token {
		t.Fatal(current, err)
	}
	effect := func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO effects VALUES(?)", old.delivery.ID)
		return err
	}
	if rejected, err := w.complete(t.Context(), old, effect); !errors.Is(rejected, ErrUnavailable) || err != nil {
		t.Fatal("expired attempt could finish", rejected, err)
	}
	requireEffectCount(t, d, 0)
	if rejected, err := w.complete(t.Context(), current, effect); rejected != nil || err != nil {
		t.Fatal(rejected, err)
	}
	requireEffectCount(t, d, 1)
}

func TestFailedEffectsRollbackAndOwnerRetryReusesOperation(t *testing.T) {
	now := time.Now()
	_, d := queueFixture(t, now)
	shouldFail := true
	var operationID string
	w := Worker{DB: d, now: func() time.Time { return now }, Handle: func(ctx context.Context, job Delivery) (Effect, error) {
		if operationID != "" && operationID != job.ID {
			t.Fatal("operation ID changed")
		}
		operationID = job.ID
		return func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO effects VALUES(?)", job.ID)
			if err == nil && shouldFail {
				return errors.New("private reason")
			}
			return err
		}, nil
	}}
	for i := range maxAttempts {
		if worked, err := w.step(t.Context()); !worked || err != nil {
			t.Fatal(i, worked, err)
		}
		if worked, err := w.step(t.Context()); worked || err != nil {
			t.Fatal("retry disregarded backoff", worked, err)
		}
		now = now.Add(time.Hour)
	}
	requireEffectCount(t, d, 0)
	page, err := w.List(t.Context(), "")
	if err != nil || len(page.Items) != 1 || !page.Items[0].CanRetry || page.Items[0].State != "failed" {
		t.Fatal(page, err)
	}
	data, err := json.Marshal(page)
	if err != nil || strings.Contains(string(data), "private") {
		t.Fatal(string(data), err)
	}
	sqlExec(t, d, "UPDATE plugin_installs SET enabled=0 WHERE id=?", fixtureID)
	if err := w.Retry(t.Context(), operationID); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("disabled plugin could be resumed", err)
	}
	sqlExec(t, d, "UPDATE plugin_installs SET enabled=1 WHERE id=?", fixtureID)
	if err := w.Retry(t.Context(), operationID); err != nil {
		t.Fatal(err)
	}
	if err := w.Retry(t.Context(), operationID); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("retry succeeded twice", err)
	}
	shouldFail = false
	if worked, err := w.step(t.Context()); !worked || err != nil {
		t.Fatal(worked, err)
	}
	requireEffectCount(t, d, 1)
}

func TestExhaustedLeaseBecomesRetryable(t *testing.T) {
	now := time.Now()
	_, d := queueFixture(t, now)
	w := Worker{DB: d, now: func() time.Time { return now }}
	for range maxAttempts {
		if _, err := w.claim(t.Context()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(leaseDuration + time.Second)
	}
	if _, err := w.claim(t.Context()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("exhausted job was leased again", err)
	}
	page, err := w.List(t.Context(), "")
	if err != nil || len(page.Items) != 1 || page.Items[0].State != "failed" ||
		!page.Items[0].CanRetry || page.Items[0].LastError != "lease_expired" {
		t.Fatal(page, err)
	}
}

func TestStaffRevocationFencesQueuedPublicationEffect(t *testing.T) {
	now := time.Now()
	_, d := queueFixture(t, now)
	const ownerID = "0123456789abcdef0123456789abcdef"
	const memberID = "abcdef0123456789abcdef0123456789"
	sqlExec(t, d, "DELETE FROM outbox_deliveries")
	sqlExec(t, d, "DELETE FROM outbox_events")
	sqlExec(t, d, "INSERT INTO owner(id,login_email) VALUES(?,?)", ownerID, "outbox@example.test")
	sqlExec(t, d, `INSERT INTO staff_members(id,owner_id,name,role,credential_id,credential)
		VALUES(?,?,'Publisher','owner','credential','{}')`, memberID, ownerID)
	tx, err := d.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx := access.WithMutation(t.Context(), access.Mutation{StaffID: memberID})
	if err := RecordPublication(ctx, tx, "article", 2, Publication{ContentID: "article", Locale: "en", RevisionID: "rev"}, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	w := Worker{DB: d, now: func() time.Time { return now }}
	a, err := w.claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sqlExec(t, d, "UPDATE staff_members SET revoked_at=? WHERE id=?", db.UTC(now), memberID)
	effect := func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO effects VALUES(?)", a.delivery.ID)
		return err
	}
	if failure, err := w.complete(ctx, a, effect); !errors.Is(failure, ErrUnavailable) || err != nil {
		t.Fatal(failure, err)
	}
	requireEffectCount(t, d, 0)
	page, err := w.List(ctx, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].State != "paused" {
		t.Fatal(page, err)
	}
}
