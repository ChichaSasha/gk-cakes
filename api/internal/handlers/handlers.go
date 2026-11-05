// Package handlers builds the worker's job handler chain in one place, so the
// deployed worker (cmd/gk-worker) and the restore's synthetic dispatch check
// (internal/backup) run exactly the same handlers in the same order.
package handlers

import (
	"log/slog"

	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds what the handlers need. Nil providers make their jobs wait,
// as in the worker without STRIPE_SECRET_KEY; a nil Email answers
// jobs.ErrNoHandler (notification jobs are retried, never sent).
type Config struct {
	DB          *pgxpool.Pool // gk_worker pool
	Coordinator *recovery.Coordinator
	Payments    payments.Provider
	Refunds     payments.RefundProvider
	Email       jobs.Handler // the notification dispatcher's Handle
	Environment string
	Account     string // STRIPE_ACCOUNT_ID
	Logger      *slog.Logger
}

// Named is one handler with the kind of job it owns.
type Named struct {
	Kind   string
	Handle jobs.Handler
}

// Kinds are the job kinds in the order the worker offers each job.
var Kinds = []string{"order_command", "payment", "refund", "hold_review", "production", "notification"}

// New returns the worker's handlers in order (Kinds).
func New(cfg Config) []Named {
	email := cfg.Email
	if email == nil {
		email = jobs.Unhandled
	}
	return []Named{
		// Order commands share the recovery journal; without it they wait.
		{"order_command", (&checkout.Commands{DB: cfg.DB, Coordinator: cfg.Coordinator, Logger: cfg.Logger}).Handle},
		// Payment collections need Stripe (and the journal to dispatch).
		{"payment", (&payments.Worker{DB: cfg.DB, Coordinator: cfg.Coordinator, Provider: cfg.Payments,
			Environment: cfg.Environment, Account: cfg.Account, Logger: cfg.Logger}).Handle},
		// Refunds (the full-return core) share Stripe and the journal.
		{"refund", (&payments.Refunds{DB: cfg.DB, Coordinator: cfg.Coordinator, Provider: cfg.Refunds,
			Environment: cfg.Environment, Account: cfg.Account, Logger: cfg.Logger}).Handle},
		// Order hold reviews need neither Stripe nor the journal.
		{"hold_review", (&checkout.HoldReviews{DB: cfg.DB, Logger: cfg.Logger}).Handle},
		// Scheduled production starts need neither Stripe nor the journal.
		{"production", (&checkout.Productions{DB: cfg.DB, Logger: cfg.Logger}).Handle},
		{"notification", email},
	}
}

// Chain offers each job to the handlers in order (jobs.Chain).
func Chain(hs []Named) jobs.Handler {
	chain := make([]jobs.Handler, len(hs))
	for i, h := range hs {
		chain[i] = h.Handle
	}
	return jobs.Chain(chain...)
}
