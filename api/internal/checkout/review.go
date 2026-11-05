package checkout

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
)

// ReviewFreezeRecheck is how long a hold review waits while a restore
// freezes order changes.
const ReviewFreezeRecheck = 5 * time.Minute

// HoldReviews is the worker's job handler for order hold reviews (K03, K04;
// migration 0015): at the end of an order's 15-minute hold, and whenever one
// of its collections is cancelled or failed, business.order_hold_review_run
// closes never-dispatched collections and releases the holds of an order
// whose payment is proven not completed (or that was never paid or
// confirmed), cancelling it; while anything may still succeed it schedules
// the next review instead. A restore freeze defers every review before any
// outcome, an already decided one included (migration 0019). It needs
// neither Stripe nor the journal. Jobs of other kinds return
// jobs.ErrNoHandler.
type HoldReviews struct {
	DB     Querier // gk_worker pool
	Logger *slog.Logger
}

// Handle runs the leased hold review.
func (r *HoldReviews) Handle(ctx context.Context, job jobs.Job) error {
	var (
		status  string
		order   *string
		next    *time.Time
		cleaned int
	)
	err := r.DB.QueryRow(ctx, `SELECT status, order_id::text, next_at, cleaned
		FROM business.order_hold_review_run($1::uuid, $2::uuid, $3)`, job.ID, job.Worker, job.Generation,
	).Scan(&status, &order, &next, &cleaned)
	if err != nil {
		return leaseErr(err)
	}
	if status == "not_review" {
		return jobs.ErrNoHandler
	}
	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}
	log := logger.With("job_id", job.ID, "generation", job.Generation, "order_id", deref(order))
	switch status {
	case "frozen":
		return jobs.Defer(ReviewFreezeRecheck, recovery.ErrFrozen)
	case "waiting":
		attrs := []any{"never_dispatched_closed", cleaned}
		if next != nil {
			attrs = append(attrs, "next_review_at", next.UTC().Format(time.RFC3339))
		}
		log.Info("order hold kept: payment may still succeed or the hold has not ended", attrs...)
		return nil
	case "released":
		log.Info("order holds released: payment not completed", "never_dispatched_closed", cleaned)
		return nil
	case "done":
		log.Info("order hold review ended: the order no longer awaits payment or was paid")
		return nil
	}
	return fmt.Errorf("checkout: unexpected hold review status %q", status)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
