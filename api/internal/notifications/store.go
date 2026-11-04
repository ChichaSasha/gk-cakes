package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is satisfied by *pgxpool.Pool and *pgx.Conn. Each gateway call
// commits on its own: no transaction is held across journal or provider calls.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ErrLeaseLost means the worker no longer holds the job's live lease; a
// newer lease holder owns the dispatch.
var ErrLeaseLost = errors.New("notifications: job lease lost")

const sqlStateLeaseLost = "GK001"

func gatewayErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == sqlStateLeaseLost {
		return fmt.Errorf("%w: %s", ErrLeaseLost, pgErr.Message)
	}
	return err
}

// leaseStore is the recovery.Store for one leased notification job. Every
// call is fenced by the job lease and bound to the intent's own dispatch in
// the database (business.notification_recovery_*).
type leaseStore struct {
	db  Querier
	job jobs.Job
}

var _ recovery.Store = leaseStore{}

func (s leaseStore) args(extra ...any) []any {
	return append([]any{s.job.ID, s.job.Worker, s.job.Generation}, extra...)
}

func (s leaseStore) Reserve(ctx context.Context, op recovery.Operation, envelopeKey string) (recovery.Reservation, error) {
	var (
		res     recovery.Reservation
		state   *string
		outcome []byte
		hash    *string
	)
	err := s.db.QueryRow(ctx, `SELECT result, op_state, op_outcome, op_outcome_hash
		FROM business.notification_recovery_reserve($1::uuid, $2::uuid, $3, $4::uuid, $5, $6::uuid, $7, $8, $9, $10)`,
		s.args(op.ID, string(op.Kind), op.AggregateID, op.PriorVersion, op.PayloadHash(), envelopeKey, op.ReplayUntil)...,
	).Scan(&res.Result, &state, &outcome, &hash)
	if err != nil {
		return recovery.Reservation{}, gatewayErr(err)
	}
	if state != nil {
		res.State = recovery.State(*state)
	}
	if outcome != nil {
		if hash == nil || recovery.HashHex(outcome) != *hash {
			return recovery.Reservation{}, fmt.Errorf("notifications: recorded outcome of %s does not match its hash", op.ID)
		}
		var o recovery.Outcome
		if err := json.Unmarshal(outcome, &o); err != nil {
			return recovery.Reservation{}, fmt.Errorf("notifications: decode recorded outcome: %w", err)
		}
		res.Outcome = &o
	}
	return res, nil
}

// Lookup is restore and manager tooling, never the worker's.
func (s leaseStore) Lookup(context.Context, recovery.Operation, string) (recovery.Reservation, error) {
	return recovery.Reservation{}, errors.New("notifications: the worker cannot look up or resolve operations")
}

func (s leaseStore) state(ctx context.Context, sql string, args ...any) (recovery.State, error) {
	var state string
	if err := s.db.QueryRow(ctx, sql, args...).Scan(&state); err != nil {
		return "", gatewayErr(err)
	}
	return recovery.State(state), nil
}

func (s leaseStore) MarkPrepared(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	return s.state(ctx, "SELECT business.notification_recovery_prepared($1::uuid, $2::uuid, $3, $4::uuid)", s.args(op.ID)...)
}

func (s leaseStore) MarkCommitted(ctx context.Context, op recovery.Operation, outcome recovery.Outcome, fromReconcile bool) (recovery.State, bool, error) {
	if fromReconcile {
		return "", false, errors.New("notifications: the worker cannot resolve a reconciliation")
	}
	hash, body, err := outcome.Encode()
	if err != nil {
		return "", false, err
	}
	var (
		state   string
		changed bool
	)
	err = s.db.QueryRow(ctx, `SELECT op_state, changed
		FROM business.notification_recovery_committed($1::uuid, $2::uuid, $3, $4::uuid, $5::json, $6)`,
		s.args(op.ID, string(body), hash)...).Scan(&state, &changed)
	if err != nil {
		return "", false, gatewayErr(err)
	}
	return recovery.State(state), changed, nil
}

func (s leaseStore) MarkSecured(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	return s.state(ctx, "SELECT business.notification_recovery_secured($1::uuid, $2::uuid, $3, $4::uuid)", s.args(op.ID)...)
}

func (s leaseStore) MarkReconcile(ctx context.Context, op recovery.Operation, reason string) (recovery.State, error) {
	return s.state(ctx, "SELECT business.notification_recovery_reconcile($1::uuid, $2::uuid, $3, $4::uuid, $5)", s.args(op.ID, reason)...)
}

func (s leaseStore) Frozen(ctx context.Context) (bool, error) {
	var frozen bool
	err := s.db.QueryRow(ctx, "SELECT business.notification_recovery_frozen($1::uuid, $2::uuid, $3)", s.args()...).Scan(&frozen)
	return frozen, gatewayErr(err)
}

// admit asks the database, right before the provider call, whether the
// call may happen: "ok", "frozen", "suspended" or "expired" (database clock).
func (s leaseStore) admit(ctx context.Context, opID string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, "SELECT business.notification_dispatch_admit($1::uuid, $2::uuid, $3, $4::uuid)", s.args(opID)...).Scan(&status)
	return status, gatewayErr(err)
}

// recordAttempt appends the provider call's result. It runs detached from
// the job context (bounded) so that evidence of a cancelled call is kept.
func (s leaseStore) recordAttempt(ctx context.Context, opID string, res Result) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var no int
	err := s.db.QueryRow(ctx, `SELECT business.notification_attempt_record($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7, $8)`,
		s.args(opID, string(res.Kind), nullInt(res.Status), nullString(res.Code), nullString(res.MessageID))...).Scan(&no)
	return gatewayErr(err)
}

type begin struct {
	Status      string
	IntentID    string
	Key         string
	Payload     []byte
	ReplayUntil time.Time
}

func (s leaseStore) begin(ctx context.Context) (begin, error) {
	var (
		b      begin
		intent *string
		key    *string
		until  *time.Time
	)
	err := s.db.QueryRow(ctx, `SELECT status, intent_id::text, idempotency_key::text, payload, replay_until
		FROM business.notification_dispatch_begin($1::uuid, $2::uuid, $3)`, s.args()...).Scan(&b.Status, &intent, &key, &b.Payload, &until)
	if err != nil {
		return begin{}, gatewayErr(err)
	}
	if intent != nil {
		b.IntentID = *intent
	}
	if key != nil {
		b.Key = *key
	}
	if until != nil {
		b.ReplayUntil = *until
	}
	return b, nil
}

func nullInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

func nullString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
