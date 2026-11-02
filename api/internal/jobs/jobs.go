// Package jobs runs the maintenance worker loop over the business.job lease
// gateway. Shutdown stops new claims, lets the in-flight job finish within the
// drain budget, and otherwise releases its lease for another worker.
package jobs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Defaults from the architecture proposal: 60s lease with heartbeat; the
// first retry delay is 1m. Attempt counting/attention belongs to GK-024.
const (
	DefaultLease        = 60 * time.Second
	DefaultPollInterval = 2 * time.Second
	DefaultRetryAfter   = time.Minute
	finalizeTimeout     = 5 * time.Second
	abandonWait         = 5 * time.Second
)

// ErrNoHandler is returned by Unhandled; such jobs are scheduled for retry.
var ErrNoHandler = errors.New("jobs: no handler registered for this operation")

// Job is a leased unit of work.
type Job struct {
	ID          string
	OperationID string
	Generation  int64
}

// Handler performs a job. It must return promptly when ctx is cancelled.
type Handler func(ctx context.Context, job Job) error

// Unhandled is the bootstrap handler: no job kinds exist yet.
func Unhandled(context.Context, Job) error { return ErrNoHandler }

// Options configures a worker.
type Options struct {
	Handler         Handler
	Logger          *slog.Logger
	ShutdownTimeout time.Duration
	Lease           time.Duration
	PollInterval    time.Duration
	RetryAfter      time.Duration
	// WorkerID identifies this process's leases; generated when empty.
	WorkerID string
}

// Worker claims and runs jobs through the gateway functions.
type Worker struct {
	pool *pgxpool.Pool
	opts Options
}

// New returns a worker using pool, which must authenticate as a gk_worker login.
func New(pool *pgxpool.Pool, opts Options) *Worker {
	if opts.Lease == 0 {
		opts.Lease = DefaultLease
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = DefaultPollInterval
	}
	if opts.RetryAfter == 0 {
		opts.RetryAfter = DefaultRetryAfter
	}
	if opts.WorkerID == "" {
		opts.WorkerID = newUUID()
	}
	return &Worker{pool: pool, opts: opts}
}

// Run processes jobs until ctx is cancelled and any in-flight job is settled.
func (w *Worker) Run(ctx context.Context) error {
	log := w.opts.Logger.With("worker_id", w.opts.WorkerID)
	log.Info("worker started")
	for {
		if ctx.Err() != nil {
			log.Info("worker stopped")
			return nil
		}
		claimedAt := time.Now()
		job, ok, err := w.claim(ctx)
		switch {
		case err != nil && ctx.Err() != nil:
			continue
		case err != nil:
			log.Error("claim failed", "error", err)
			sleep(ctx, w.opts.PollInterval)
		case !ok:
			sleep(ctx, w.opts.PollInterval)
		default:
			w.process(ctx, log.With("job_id", job.ID, "generation", job.Generation), job, claimedAt.Add(w.opts.Lease))
		}
	}
}

// process runs one job. leaseDeadline is a conservative local estimate of
// when the lease expires (measured from before the claim); if heartbeats keep
// failing past it, the handler is cancelled because another worker may now
// hold the job.
func (w *Worker) process(ctx context.Context, log *slog.Logger, job Job, leaseDeadline time.Time) {
	// The handler deliberately outlives ctx: shutdown starts a bounded drain
	// (ShutdownTimeout) instead of interrupting work mid-way; cancelJob ends it.
	jobCtx, cancelJob := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelJob()
	done := make(chan error, 1)
	go func() { done <- w.opts.Handler(jobCtx, job) }()

	heartbeat := time.NewTicker(w.opts.Lease / 3)
	defer heartbeat.Stop()
	shutdown := ctx.Done()
	var drainDeadline <-chan time.Time

	for {
		select {
		case err := <-done:
			w.finalize(log, job, err)
			return
		case <-heartbeat.C:
			attempt := time.Now()
			held, err := w.heartbeat(job)
			switch {
			case err != nil && time.Now().Before(leaseDeadline):
				log.Warn("heartbeat failed", "error", err)
				continue
			case err != nil:
				log.Error("heartbeats failed past the lease; abandoning job without finalizing", "error", err)
			case !held:
				log.Warn("lease lost; abandoning job without finalizing")
			default:
				leaseDeadline = attempt.Add(w.opts.Lease)
				continue
			}
			cancelJob()
			if !waitAbandon(done) {
				log.Error("handler ignored cancellation after losing its lease")
			}
			return
		case <-shutdown:
			shutdown = nil
			drainDeadline = time.After(w.opts.ShutdownTimeout)
			log.Info("shutdown requested; draining in-flight job", "drain_timeout", w.opts.ShutdownTimeout.String())
		case <-drainDeadline:
			log.Warn("drain deadline exceeded; releasing lease")
			cancelJob()
			if !waitAbandon(done) {
				log.Error("handler ignored cancellation; lease left to expire")
				return
			}
			w.release(log, job, 0)
			return
		}
	}
}

func (w *Worker) finalize(log *slog.Logger, job Job, handlerErr error) {
	if handlerErr != nil {
		log.Warn("job failed; scheduling retry", "error", handlerErr, "retry_after", w.opts.RetryAfter.String())
		w.release(log, job, w.opts.RetryAfter)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), finalizeTimeout)
	defer cancel()
	var ok bool
	err := w.pool.QueryRow(ctx, "SELECT business.job_complete($1::uuid, $2::uuid, $3)", job.ID, w.opts.WorkerID, job.Generation).Scan(&ok)
	switch {
	case err != nil:
		log.Error("complete failed; lease will expire for reclaim", "error", err)
	case !ok:
		log.Warn("complete fenced: lease no longer held")
	default:
		log.Info("job completed")
	}
}

func (w *Worker) release(log *slog.Logger, job Job, after time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), finalizeTimeout)
	defer cancel()
	var ok bool
	err := w.pool.QueryRow(ctx, "SELECT business.job_release($1::uuid, $2::uuid, $3, $4)",
		job.ID, w.opts.WorkerID, job.Generation, after).Scan(&ok)
	switch {
	case err != nil:
		log.Error("release failed; lease will expire for reclaim", "error", err)
	case !ok:
		log.Warn("release fenced: lease no longer held")
	}
}

func (w *Worker) claim(ctx context.Context) (Job, bool, error) {
	var job Job
	err := w.pool.QueryRow(ctx,
		"SELECT job_id::text, job_operation_id::text, job_generation FROM business.job_claim($1::uuid, $2)",
		w.opts.WorkerID, w.opts.Lease).Scan(&job.ID, &job.OperationID, &job.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("claim: %w", err)
	}
	return job, true, nil
}

func (w *Worker) heartbeat(job Job) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), finalizeTimeout)
	defer cancel()
	var until *time.Time
	err := w.pool.QueryRow(ctx, "SELECT business.job_heartbeat($1::uuid, $2::uuid, $3, $4)",
		job.ID, w.opts.WorkerID, job.Generation, w.opts.Lease).Scan(&until)
	if err != nil {
		return false, fmt.Errorf("heartbeat: %w", err)
	}
	return until != nil, nil
}

func waitAbandon(done <-chan error) bool {
	select {
	case <-done:
		return true
	case <-time.After(abandonWait):
		return false
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
