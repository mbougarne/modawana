package extension

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Job is a scheduled unit of work run by the host job runner.
type Job struct {
	// Name identifies the job in outcome records.
	Name string
	// Every is the cadence after a successful run; the first run happens one
	// cadence after the runner starts.
	Every time.Duration
	// Timeout bounds one run: the run's context is canceled when it elapses.
	// Zero means the job is bounded by its own cadence.
	Timeout time.Duration
	// MaxBackoff caps the failure delay. After a failure the next run keeps
	// the regular cadence, then doubles per consecutive failure up to this
	// cap, returning to the cadence after a success. Zero means 4 × Every.
	MaxBackoff time.Duration
	// Run does the work and reports the outcome the host records.
	Run func(ctx context.Context) error

	slug string
	stop context.CancelFunc
}

// Jobs returns the registered jobs in installation order.
func (h *Host) Jobs() []Job {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]Job(nil), h.jobs...)
}

type jobRunner struct {
	ctx context.Context
	wg  sync.WaitGroup
}

func (r *jobRunner) start(h *Host, job Job) context.CancelFunc {
	ctx, stop := context.WithCancel(r.ctx)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		h.runJob(ctx, job)
	}()
	return stop
}

// RunJobs follows the live job registry until the server context ends.
func (h *Host) RunJobs(ctx context.Context) {
	runner := &jobRunner{ctx: ctx}
	h.mu.Lock()
	if h.jobRunner != nil {
		h.mu.Unlock()
		return
	}
	h.jobRunner = runner
	for i, job := range h.jobs {
		h.jobs[i].stop = runner.start(h, job)
	}
	h.mu.Unlock()
	<-ctx.Done()
	h.mu.Lock()
	h.jobRunner = nil
	h.mu.Unlock()
	runner.wg.Wait()
}

func (h *Host) runJob(ctx context.Context, job Job) {
	delay := job.Every
	failed := false
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := runJobOnce(ctx, job)
		if ctx.Err() != nil {
			return
		}
		delay = nextJobDelay(delay, job, err != nil, failed)
		failed = err != nil
		if err != nil {
			h.logger.Error("extension job failed", "extension", job.slug, "job", job.Name, "duration_ms", time.Since(started).Milliseconds(), "retry_in", delay.String(), "reason", err.Error())
		} else {
			h.logger.Debug("extension job completed", "extension", job.slug, "job", job.Name, "duration_ms", time.Since(started).Milliseconds())
		}
		timer.Reset(delay)
	}
}

// nextJobDelay keeps the cadence after a success and on a first failure, then
// doubles per consecutive failure up to the job's cap.
func nextJobDelay(current time.Duration, job Job, failed, failedBefore bool) time.Duration {
	if !failed || !failedBefore {
		return job.Every
	}
	return min(current*2, job.MaxBackoff)
}

func runJobOnce(ctx context.Context, job Job) (err error) {
	bounded, cancel := context.WithTimeout(ctx, job.Timeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			err = errors.New("job panic")
		}
	}()
	return job.Run(bounded)
}
