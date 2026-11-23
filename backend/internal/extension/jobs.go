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

	slug   string
	stop   context.CancelFunc
	health *jobStatus
}

// JobStatus contains only owner-visible scheduler metadata.
type JobStatus struct {
	Name                string    `json:"name"`
	State               string    `json:"state"`
	NextRunAt           time.Time `json:"next_run_at,omitzero"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
}

type jobStatus struct {
	mu    sync.Mutex
	value JobStatus
}

func (j Job) setStatus(state string, delay time.Duration, failures int) {
	var next time.Time
	if delay > 0 {
		next = time.Now().Add(delay).UTC()
	}
	j.health.mu.Lock()
	defer j.health.mu.Unlock()
	j.health.value = JobStatus{j.Name, state, next, failures}
}

func (h *Host) JobStatuses(slug string) []JobStatus {
	var out []JobStatus
	if h == nil {
		return out
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, job := range h.jobs {
		if job.slug == slug {
			job.health.mu.Lock()
			out = append(out, job.health.value)
			job.health.mu.Unlock()
		}
	}
	return out
}

// Jobs returns the registered jobs in installation order.
func (h *Host) Jobs() []Job {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]Job(nil), h.jobs...)
}

type jobRunner struct {
	ctx      context.Context
	wg       sync.WaitGroup
	stopping bool // guarded by Host.mu
}

func (r *jobRunner) start(h *Host, job Job) context.CancelFunc {
	ctx, stop := context.WithCancel(r.ctx)
	job.setStatus("scheduled", job.Every, 0)
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
	runner.stopping = true
	h.mu.Unlock()
	runner.wg.Wait()
	h.mu.Lock()
	h.jobRunner = nil
	h.mu.Unlock()
}

func (h *Host) runJob(ctx context.Context, job Job) {
	delay := job.Every
	failures := 0
	defer func() { job.setStatus("idle", 0, failures) }()
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
		job.setStatus("running", 0, failures)
		started := time.Now()
		err := runJobOnce(ctx, job)
		if ctx.Err() != nil {
			return
		}
		delay = nextJobDelay(delay, job, err != nil, failures > 0)
		if err != nil {
			failures++
			job.setStatus("backoff", delay, failures)
			h.logger.Error("extension job failed", "extension", job.slug, "job", job.Name, "duration_ms", time.Since(started).Milliseconds(), "retry_in", delay.String(), "reason", err.Error())
		} else {
			failures = 0
			job.setStatus("scheduled", delay, failures)
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
