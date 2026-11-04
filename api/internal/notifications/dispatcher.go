package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5"
)

// FreezeRecheck is how long a dispatch waits while outgoing effects are
// frozen (restore in progress) before checking again; it is not a failure.
const FreezeRecheck = 5 * time.Minute

// Attention reasons stored on the job (no personal data).
const (
	reasonSuspended  = "notification suspended (recipient or contact revoked): no further send attempts; see its attempts for any earlier ones"
	reasonPayload    = "stored notification payload refused by this build: not dispatched (see its attempts for any earlier ones)"
	reasonReconcile  = "email outcome unknown past the provider idempotency window; manager reconciliation needed, never a blind resend"
	reasonRecovery   = "recovery operation conflict for this notification"
	reasonRefused    = "email provider refused the request: check the API key and the sending domain, then requeue"
	reasonConflict   = "email provider reports this idempotency key used with a different body; outcome unknown"
	reasonRejected   = "email rejected by the provider for this attempt: if the notice is failed, reach the recipient another way; if unknown, an earlier attempt may have been sent"
	statusAccepted   = "accepted"
	statusRejected   = "rejected"
	providerEvidence = "provider_message_id"
)

// Dispatcher is the worker's job handler for notification intents.
type Dispatcher struct {
	db     Querier
	coord  *recovery.Coordinator
	sender Sender
	env    string
	log    *slog.Logger
}

// DispatcherConfig configures a Dispatcher.
type DispatcherConfig struct {
	DB          Querier // gk_worker pool
	Coordinator *recovery.Coordinator
	Sender      Sender
	Environment string // APP_ENV; payloads of any other environment are refused
	Logger      *slog.Logger
}

// NewDispatcher returns a Dispatcher.
func NewDispatcher(cfg DispatcherConfig) (*Dispatcher, error) {
	switch {
	case cfg.DB == nil || cfg.Coordinator == nil || cfg.Sender == nil || cfg.Logger == nil:
		return nil, errors.New("notifications: dispatcher needs a database, a coordinator, a sender and a logger")
	case !envPattern.MatchString(cfg.Environment) || cfg.Coordinator.Environment != cfg.Environment:
		return nil, errors.New("notifications: dispatcher and recovery journal must use the same environment")
	}
	return &Dispatcher{db: cfg.DB, coord: cfg.Coordinator, sender: cfg.Sender, env: cfg.Environment, log: cfg.Logger}, nil
}

// Handle dispatches the leased job's notification intent; jobs of other kinds
// return jobs.ErrNoHandler. Logs carry IDs and provider status only, never
// recipients or content.
func (d *Dispatcher) Handle(ctx context.Context, job jobs.Job) error {
	store := leaseStore{db: d.db, job: job}
	b, err := store.begin(ctx)
	if err != nil {
		return err
	}
	switch b.Status {
	case "ready":
	case "not_notification":
		return jobs.ErrNoHandler
	case "suspended":
		return jobs.Attention(reasonSuspended, nil)
	case "frozen":
		return jobs.Defer(FreezeRecheck, recovery.ErrFrozen)
	default:
		return fmt.Errorf("notifications: unexpected dispatch status %q", b.Status)
	}
	dispatch, err := ParseDispatch(b.Payload, d.env, b.IntentID, b.Key)
	if err != nil {
		return jobs.Attention(reasonPayload, err)
	}
	op := recovery.Operation{ID: b.Key, Kind: recovery.KindDispatch, AggregateID: b.IntentID,
		Payload: b.Payload, ReplayUntil: b.ReplayUntil}
	log := d.log.With("job_id", job.ID, "generation", job.Generation, "intent_id", b.IntentID)
	outcome, err := d.coord.Dispatch(ctx, store, op, d.effect(log, store, op.ID, dispatch))
	var ae *admitError
	switch {
	case err == nil && outcome.Status == statusRejected:
		// Settled and never retried, but a manager must know.
		return jobs.Attention(reasonRejected, nil)
	case errors.As(err, &ae) && ae.status == "expired":
		// The database clock says the window has ended: record the
		// reconciliation as the worker clock would have.
		if _, rerr := store.MarkReconcile(ctx, op, "provider replay window ended (database clock) before the call"); rerr != nil {
			// Nothing is sent either way; a manager sees it at once.
			return jobs.Attention(reasonReconcile, fmt.Errorf("notifications: mark reconcile: %w (after %v)", rerr, err))
		}
	}
	return outcomeError(err)
}

// effect sends the stored request once and records the call. Accepted and
// rejected are known outcomes; anything else leaves the outcome unknown, so
// the dispatch stays prepared and is replayed with the same key and body.
func (d *Dispatcher) effect(log *slog.Logger, store leaseStore, opID string, dispatch Dispatch) recovery.Effect {
	return func(ctx context.Context) (recovery.Outcome, error) {
		// Last check before the call, on the database clock: a freeze, a
		// revocation or the end of the window since the dispatch began.
		switch status, err := store.admit(ctx, opID); {
		case err != nil:
			return recovery.Outcome{}, fmt.Errorf("notifications: admission: %w", err)
		case status != "ok":
			return recovery.Outcome{}, &admitError{status: status}
		}
		callCtx, cancel := context.WithTimeout(ctx, ProviderTimeout)
		res := d.sender.Send(callCtx, dispatch.IdempotencyKey, dispatch.Request)
		cancel()
		log.Info("email provider call", "result", string(res.Kind), "http_status", res.Status, "provider_code", res.Code,
			"provider_message_id", res.MessageID, "error", errString(res.Err))
		if err := store.recordAttempt(ctx, opID, res); err != nil {
			return recovery.Outcome{}, fmt.Errorf("notifications: record attempt: %w", err)
		}
		switch res.Kind {
		case Accepted:
			ev, err := json.Marshal(map[string]string{providerEvidence: res.MessageID})
			return recovery.Outcome{Status: statusAccepted, Evidence: ev}, err
		case Rejected:
			ev, err := json.Marshal(struct {
				HTTPStatus int    `json:"http_status"`
				Code       string `json:"provider_code,omitempty"`
			}{res.Status, res.Code})
			return recovery.Outcome{Status: statusRejected, Evidence: ev}, err
		default:
			return recovery.Outcome{}, &callError{res: res}
		}
	}
}

// admitError means the provider was not called: the dispatch was refused
// admission right before the call.
type admitError struct{ status string }

func (e *admitError) Error() string { return "email dispatch not admitted: " + e.status }

// callError is a provider call whose outcome is not a recordable one.
type callError struct{ res Result }

func (e *callError) Error() string {
	msg := "email provider call " + string(e.res.Kind)
	if e.res.Status != 0 {
		msg += fmt.Sprintf(" (HTTP %d", e.res.Status)
		if e.res.Code != "" {
			msg += " " + e.res.Code
		}
		msg += ")"
	}
	if e.res.Err != nil {
		msg += ": " + e.res.Err.Error()
	}
	return msg
}

func (e *callError) Unwrap() error { return e.res.Err }

// outcomeError maps a dispatch result to the job's next step: done, a
// counted retry (unknown, deferred, journal or database trouble), a deferral
// while frozen, or manager attention.
func outcomeError(err error) error {
	var (
		ce *callError
		ae *admitError
	)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, recovery.ErrNeedsReconcile), errors.As(err, &ae) && ae.status == "expired":
		return jobs.Attention(reasonReconcile, err)
	case errors.Is(err, recovery.ErrFrozen), errors.As(err, &ae) && ae.status == "frozen":
		return jobs.Defer(FreezeRecheck, err)
	case errors.As(err, &ae) && ae.status == "suspended":
		return jobs.Attention(reasonSuspended, err)
	case errors.Is(err, recovery.ErrBusy), errors.Is(err, recovery.ErrStale):
		return jobs.Attention(reasonRecovery, err)
	case errors.As(err, &ce) && ce.res.Kind == Refused:
		return jobs.Attention(reasonRefused, err)
	case errors.As(err, &ce) && ce.res.Kind == Conflict:
		return jobs.Attention(reasonConflict, err)
	default:
		return err
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Enqueue stores n with its job and audit event in tx, which must run as
// gk_owner inside the transaction committing the transition (domain gateway
// functions call business.notification_enqueue the same way in SQL).
// actorKind is "system", "staff" or "customer"; actorID is empty for system.
func Enqueue(ctx context.Context, tx pgx.Tx, n Notice, actorKind, actorID string) error {
	_, err := tx.Exec(ctx, `SELECT business.notification_enqueue($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, $6::json, $7, $8::uuid)`,
		n.IntentID, n.TransitionID, n.RecipientID, n.TemplateRevision, n.IdempotencyKey, string(n.Payload), actorKind, nullString(actorID))
	return err
}
