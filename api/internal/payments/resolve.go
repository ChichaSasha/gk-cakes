package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5/pgconn"
)

// RefundResolves is the job handler that records a manager's refund
// decision in the independent journal (migration 0027; K11): the refund
// dispatch's recovery record, prepared or in reconciliation when the
// manager decided the operation, gets the outcome manager_failed or
// manager_succeeded (evidence: the decision and the operation state) with
// recovery.Coordinator.Resolve, outside any transaction, and is secured.
// Jobs of other kinds return jobs.ErrNoHandler.
type RefundResolves struct {
	DB          Querier // gk_worker pool
	Coordinator *recovery.Coordinator
	Logger      *slog.Logger
}

const reasonRefundResolve = "the refund's dispatch record could not be resolved with the manager decision (another outcome is recorded): manager follow-up"

// Handle runs the leased resolve job.
func (w *RefundResolves) Handle(ctx context.Context, job jobs.Job) error {
	store := resolveStore{db: w.DB, job: job}
	var (
		status                  string
		opID, payload, decision *string
		state                   *string
		until                   *time.Time
	)
	err := w.DB.QueryRow(ctx, `SELECT status, operation_id::text, payload, replay_until, decision_id::text, operation_state
		FROM business.refund_resolve_begin($1::uuid, $2::uuid, $3)`, job.ID, job.Worker, job.Generation).Scan(
		&status, &opID, &payload, &until, &decision, &state)
	if err != nil {
		return gatewayErr(err)
	}
	logger := w.Logger
	if logger == nil {
		logger = slog.Default()
	}
	switch status {
	case "not_resolve":
		return jobs.ErrNoHandler
	case "frozen":
		return jobs.Defer(FreezeRecheck, recovery.ErrFrozen)
	case "done":
		logger.Info("refund decision needs no journal resolution", "job_id", job.ID, "refund_operation_id", deref(opID))
		return nil
	case "ready":
	default:
		return fmt.Errorf("payments: unexpected refund resolve status %q", status)
	}
	if w.Coordinator == nil {
		return jobs.Defer(JournalRetry, errors.New("payments: no recovery journal configured for refund resolutions"))
	}
	op := recovery.Operation{ID: *opID, Kind: recovery.KindDispatch, AggregateID: *opID, Payload: json.RawMessage(*payload),
		ReplayUntil: *until}
	evidence, _ := json.Marshal(map[string]string{"decision_id": *decision, "refund_operation_state": *state})
	outcome := recovery.Outcome{Status: "manager_" + *state, Evidence: evidence}
	if _, err := w.Coordinator.Resolve(ctx, store, op, outcome); err != nil {
		if errors.Is(err, ErrLeaseLost) || errors.Is(err, recovery.ErrFrozen) {
			return err
		}
		var already *alreadyResolved
		if errors.As(err, &already) {
			return jobs.Attention(reasonRefundResolve, err)
		}
		return err
	}
	logger.Info("refund decision journaled", "job_id", job.ID, "refund_operation_id", *opID, "decision_id", *decision,
		"outcome", outcome.Status)
	return nil
}

type alreadyResolved struct{ err error }

func (e *alreadyResolved) Error() string { return e.err.Error() }
func (e *alreadyResolved) Unwrap() error { return e.err }

// resolveStore is the recovery.Store for one leased resolve job; every call
// is fenced by the job lease (business.refund_resolve_*). Only the calls
// Coordinator.Resolve makes are supported.
type resolveStore struct {
	db  Querier
	job jobs.Job
}

var _ recovery.Store = resolveStore{}

var errResolveOnly = errors.New("payments: a refund resolve job only looks up, commits and secures")

func (s resolveStore) Reserve(context.Context, recovery.Operation, string) (recovery.Reservation, error) {
	return recovery.Reservation{}, errResolveOnly
}

func (s resolveStore) Lookup(ctx context.Context, op recovery.Operation, envelopeKey string) (recovery.Reservation, error) {
	var (
		res     recovery.Reservation
		state   *string
		outcome []byte
		hash    *string
	)
	err := s.db.QueryRow(ctx, `SELECT result, op_state, op_outcome, op_outcome_hash
		FROM business.refund_resolve_lookup($1::uuid, $2::uuid, $3, $4::uuid, $5, $6::uuid, $7, $8, $9, $10)`,
		s.job.ID, s.job.Worker, s.job.Generation, op.ID, string(op.Kind), op.AggregateID, op.PriorVersion, op.PayloadHash(),
		envelopeKey, op.ReplayUntilArg()).Scan(&res.Result, &state, &outcome, &hash)
	if err != nil {
		return recovery.Reservation{}, gatewayErr(err)
	}
	if state != nil {
		res.State = recovery.State(*state)
	}
	if outcome != nil {
		if hash == nil || recovery.HashHex(outcome) != *hash {
			return recovery.Reservation{}, fmt.Errorf("payments: recorded outcome of %s does not match its hash", op.ID)
		}
		var o recovery.Outcome
		if err := json.Unmarshal(outcome, &o); err != nil {
			return recovery.Reservation{}, err
		}
		res.Outcome = &o
	}
	return res, nil
}

func (s resolveStore) MarkPrepared(context.Context, recovery.Operation) (recovery.State, error) {
	return "", errResolveOnly
}

func (s resolveStore) MarkCommitted(ctx context.Context, op recovery.Operation, outcome recovery.Outcome, fromReconcile bool) (recovery.State, bool, error) {
	if !fromReconcile {
		return "", false, errResolveOnly
	}
	hash, body, err := outcome.Encode()
	if err != nil {
		return "", false, err
	}
	var (
		state   string
		changed bool
	)
	err = s.db.QueryRow(ctx, `SELECT op_state, changed FROM business.refund_resolve_committed($1::uuid, $2::uuid, $3, $4::uuid, $5::json, $6)`,
		s.job.ID, s.job.Worker, s.job.Generation, op.ID, string(body), hash).Scan(&state, &changed)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return "", false, &alreadyResolved{err}
	}
	if err != nil {
		return "", false, gatewayErr(err)
	}
	return recovery.State(state), changed, nil
}

func (s resolveStore) MarkSecured(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	var state string
	if err := s.db.QueryRow(ctx, `SELECT business.refund_resolve_secured($1::uuid, $2::uuid, $3, $4::uuid)`,
		s.job.ID, s.job.Worker, s.job.Generation, op.ID).Scan(&state); err != nil {
		return "", gatewayErr(err)
	}
	return recovery.State(state), nil
}

func (s resolveStore) MarkReconcile(context.Context, recovery.Operation, string) (recovery.State, error) {
	return "", errResolveOnly
}

func (s resolveStore) Frozen(context.Context) (bool, error) { return false, errResolveOnly }
