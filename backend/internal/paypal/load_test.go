package paypal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/outbox"
	"github.com/mbougarne/modawana/backend/internal/store"
	"modernc.org/sqlite"
)

const loadOrders = 120
const loadInterval = 50 * time.Millisecond
const loadWebhookCount = loadOrders / 4
const loadOutboxCount = loadOrders/5 + 1

type loadTimings struct {
	quote, checkout, writer []time.Duration
	busy, retries           atomic.Int64
	mu                      sync.Mutex
}

func (m *loadTimings) record(sample *[]time.Duration, start time.Time) {
	duration := time.Since(start)
	m.mu.Lock()
	*sample = append(*sample, duration)
	m.mu.Unlock()
}

func (m *loadTimings) call(ctx context.Context, fn func() error) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := fn()
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || (sqliteErr.Code()&255 != 5 && sqliteErr.Code()&255 != 6) {
			return err
		}
		m.busy.Add(1)
		if attempt == 2 {
			return err
		}
		m.retries.Add(1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return errors.New("unreachable retry loop")
}

func percentile95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[(95*len(sorted)+99)/100-1]
}

func settleLoadOrder(ctx context.Context, s *Service, buyer, reference string, index int) error {
	p, err := s.store.GuestPurchase(ctx, buyer, reference)
	if err != nil {
		return err
	}
	id := fmt.Sprintf("LOAD-%06d", index)
	if err = s.store.BindApproval(ctx, p, store.PaymentApproval{ProviderOrder: id, ApprovalURL: "https://www.sandbox.paypal.com/checkoutnow?token=" + id}); err != nil {
		return err
	}
	p, err = s.store.GuestPurchase(ctx, buyer, reference)
	if err != nil {
		return err
	}
	op, err := s.store.StartProviderCapture(ctx, p)
	if err != nil {
		return err
	}
	return s.store.RecordPayment(ctx, store.PaymentEvidence{Operation: op, CaptureID: "CAPTURE-" + id})
}

func TestCommerceMixedLoad(t *testing.T) {
	if os.Getenv("MODAWANA_COMMERCE_LOAD") != "1" {
		t.Skip("set MODAWANA_COMMERCE_LOAD=1 to run the paced local workload")
	}
	s, fake, initial, _, _ := setupPayPal(t)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
	defer cancel()
	if _, err := s.Approval(ctx, guest, initial.Reference); err != nil {
		t.Fatal(err)
	}
	fake.approved = true
	if _, err := s.Finish(ctx, guest, initial.Reference, true); err != nil {
		t.Fatal(err)
	}
	stockID, _ := db.NewID()
	if _, err := s.store.Adjust(ctx, "owner", "MUG", store.AdjustmentInput{ID: stockID, Delta: loadOrders, Reason: "load fixture"}); err != nil {
		t.Fatal(err)
	}

	workerCtx, stop := context.WithCancel(ctx)
	host := extension.NewHost(nil)
	if err := host.Install(s); err != nil {
		t.Fatal(err)
	}
	deliveries := &outbox.Worker{DB: s.store.DB, Handle: store.PrepareNotice}
	var workers sync.WaitGroup
	worker := func(run func(context.Context)) {
		workers.Go(func() { run(workerCtx) })
	}
	worker(host.RunJobs)
	worker(deliveries.Run)
	defer func() { stop(); workers.Wait() }()

	// Each timestamp is published before ingress. The observer records when
	// durable application first becomes visible, with a 50 ms sampling step.
	var timestamps [loadWebhookCount]atomic.Int64
	var queueLag []time.Duration
	var peakPending, applied int
	observerCtx, stopObserver := context.WithCancel(ctx)
	observerResult := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(loadInterval)
		defer ticker.Stop()
		seen := map[int]bool{}
		for {
			select {
			case <-observerCtx.Done():
				observerResult <- nil
				return
			case <-ticker.C:
			}
			rows, err := s.store.DB.QueryContext(observerCtx, `SELECT event_id,state FROM store_paypal_receipts WHERE event_id LIKE 'LOAD-%'`)
			if err != nil {
				if observerCtx.Err() != nil {
					err = nil
				}
				observerResult <- err
				return
			}
			var pending int
			for rows.Next() {
				var id, state string
				if err = rows.Scan(&id, &state); err != nil {
					break
				}
				var index int
				if _, err = fmt.Sscanf(id, "LOAD-%d", &index); err != nil || index < 0 || index >= len(timestamps) {
					err = errors.New("unexpected load receipt identifier")
					break
				}
				if state == "pending" {
					pending++
				}
				if state == "applied" && !seen[index] {
					seen[index] = true
					queueLag = append(queueLag, time.Since(time.Unix(0, timestamps[index].Load())))
				}
			}
			if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
				if observerCtx.Err() != nil {
					err = nil
				}
				observerResult <- err
				return
			}
			applied = len(seen)
			peakPending = max(peakPending, pending)
			if applied == loadWebhookCount {
				observerResult <- nil
				return
			}
		}
	}()
	defer stopObserver()

	metrics := &loadTimings{}
	probeCtx, stopProbe := context.WithCancel(ctx)
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		ticker := time.NewTicker(loadInterval)
		defer ticker.Stop()
		for {
			select {
			case <-probeCtx.Done():
				return
			case <-ticker.C:
			}
			start := time.Now()
			err := metrics.call(probeCtx, func() error {
				tx, err := s.store.DB.BeginTx(probeCtx, nil)
				if err != nil {
					return err
				}
				return tx.Rollback()
			})
			if err == nil {
				metrics.record(&metrics.writer, start)
			}
		}
	}()
	defer func() { stopProbe(); <-probeDone }()

	before := s.store.DB.Stats()
	ticker := time.NewTicker(loadInterval)
	defer ticker.Stop()
	errs := make(chan error, loadOrders)
	var requests sync.WaitGroup
	started := time.Now()
	for i := range loadOrders {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
		requests.Go(func() {
			buyer := fmt.Sprintf("%032x", i+100)
			input := store.QuoteInput{Locale: "en", Country: "US", Items: []store.CartItem{{SKU: "MUG", Quantity: 1}}}
			var quote store.Quote
			start := time.Now()
			if err := metrics.call(ctx, func() error {
				var err error
				quote, err = s.store.Quote(ctx, input)
				return err
			}); err != nil {
				errs <- err
				return
			}
			metrics.record(&metrics.quote, start)
			order := store.OrderInput{QuoteInput: input, Key: buyer, QuoteFingerprint: quote.Fingerprint,
				Customer: store.Customer{Name: "Load Fixture", Email: "fixture@example.test", Address: "123 Test", City: "Portland"}}
			var receipt store.OrderReceipt
			start = time.Now()
			if err := metrics.call(ctx, func() error {
				var err error
				receipt, err = s.store.CreateOrder(ctx, buyer, order)
				return err
			}); err != nil {
				errs <- err
				return
			}
			metrics.record(&metrics.checkout, start)
			if i%5 == 0 {
				if err := settleLoadOrder(ctx, s, buyer, receipt.Reference, i); err != nil {
					errs <- err
					return
				}
			}
			if i%4 == 0 {
				index := i / 4
				id := fmt.Sprintf("LOAD-%06d", index)
				payload := retestEvent(id, "PAYMENT.CAPTURE.COMPLETED", "CAPTURE123")
				timestamps[index].Store(time.Now().UnixNano())
				if err := s.Accept(ctx, "sandbox-store", eventHeaders(), payload); err != nil {
					errs <- err
					return
				}
				if index%5 == 0 {
					errs <- s.Accept(ctx, "sandbox-store", eventHeaders(), payload)
				}
			}
		})
	}
	requests.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	workloadDuration := time.Since(started)
	select {
	case err := <-observerResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("mixed workload did not drain", ctx.Err())
	}
	// The webhook and notification workers drain independently. The last
	// notice may still be leased when the webhook observer finishes.
	for {
		var delivered int
		if err := s.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM outbox_deliveries
			WHERE plugin_slug='store' AND state='succeeded'`).Scan(&delivered); err != nil {
			t.Fatal(err)
		}
		if delivered >= loadOutboxCount {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("store notices did not drain", ctx.Err())
		case <-time.After(loadInterval):
		}
	}
	stopProbe()
	<-probeDone

	rows, err := s.store.DB.QueryContext(ctx, `SELECT d.updated_at-e.created_at FROM outbox_deliveries d
		JOIN outbox_events e ON e.id=d.event_id WHERE d.plugin_slug='store' AND d.state='succeeded'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var notificationLag []time.Duration
	for rows.Next() {
		var ms int64
		if err = rows.Scan(&ms); err != nil {
			t.Fatal(err)
		}
		notificationLag = append(notificationLag, time.Duration(ms)*time.Millisecond)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	after := s.store.DB.Stats()
	q95, c95, w95, h95, o95 := percentile95(metrics.quote), percentile95(metrics.checkout), percentile95(metrics.writer), percentile95(queueLag), percentile95(notificationLag)
	t.Logf("orders=%d bursts=%s quote_p95=%s reservation_p95=%s writer_p95=%s sqlite_busy=%d client_retries=%d pool_waits=%d pool_wait_duration=%s",
		len(metrics.checkout), workloadDuration, q95, c95, w95, metrics.busy.Load(), metrics.retries.Load(), after.WaitCount-before.WaitCount, after.WaitDuration-before.WaitDuration)
	t.Logf("webhooks_applied=%d peak_pending=%d observed_webhook_p95=%s outbox_delivered=%d outbox_p95=%s",
		applied, peakPending, h95, len(notificationLag), o95)
	if len(metrics.checkout) != loadOrders || applied != loadWebhookCount || len(notificationLag) != loadOutboxCount {
		t.Fatal("expected one durable result per order, webhook, and notice")
	}
	if q95 > 250*time.Millisecond || c95 > 250*time.Millisecond || w95 > 100*time.Millisecond ||
		h95 > 15*time.Second || o95 > 5*time.Second {
		t.Fatal("one or more local launch budgets were exceeded")
	}
}
