package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Querier is satisfied by *pgx.Conn and *pgxpool.Pool.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// SQLStore is a Store over the owner-internal recovery functions, for
// restore tooling and tests that run as gk_owner. Domain stores wrap the same
// functions inside their own gateway functions so that a domain state change
// and its recovery phase commit in one transaction.
type SQLStore struct{ db Querier }

// NewSQLStore returns a store on an autocommit connection or pool. A
// transaction is refused: each phase must commit before the coordinator's
// next journal or provider call, and no lock may be held across them.
func NewSQLStore(db Querier) (SQLStore, error) {
	if _, inTx := db.(pgx.Tx); inTx || db == nil {
		return SQLStore{}, errors.New("recovery: SQLStore needs an autocommit connection or pool, not a transaction")
	}
	return SQLStore{db: db}, nil
}

// Reserve implements Store.
func (s SQLStore) Reserve(ctx context.Context, op Operation, envelopeKey string) (Reservation, error) {
	return s.reservation(ctx, "recovery_reserve", op, envelopeKey)
}

// Lookup implements Store.
func (s SQLStore) Lookup(ctx context.Context, op Operation, envelopeKey string) (Reservation, error) {
	return s.reservation(ctx, "recovery_lookup", op, envelopeKey)
}

func (s SQLStore) reservation(ctx context.Context, fn string, op Operation, envelopeKey string) (Reservation, error) {
	var (
		res     Reservation
		state   *string
		outcome []byte
		hash    *string
	)
	err := s.db.QueryRow(ctx,
		"SELECT result, op_state, op_outcome, op_outcome_hash FROM business."+fn+"($1::uuid, $2, $3::uuid, $4, $5, $6, $7)",
		op.ID, string(op.Kind), op.AggregateID, op.PriorVersion, op.PayloadHash(), envelopeKey, op.ReplayUntil,
	).Scan(&res.Result, &state, &outcome, &hash)
	if err != nil {
		return Reservation{}, err
	}
	if state != nil {
		res.State = State(*state)
	}
	if outcome != nil {
		if hash == nil || hashHex(outcome) != *hash {
			return Reservation{}, fmt.Errorf("recovery: recorded outcome of %s does not match its hash", op.ID)
		}
		var o Outcome
		if err := json.Unmarshal(outcome, &o); err != nil {
			return Reservation{}, fmt.Errorf("recovery: decode recorded outcome: %w", err)
		}
		res.Outcome = &o
	}
	return res, nil
}

func (s SQLStore) transition(ctx context.Context, sql string, args ...any) (State, error) {
	var state string
	if err := s.db.QueryRow(ctx, sql, args...).Scan(&state); err != nil {
		return "", err
	}
	return State(state), nil
}

// MarkPrepared implements Store.
func (s SQLStore) MarkPrepared(ctx context.Context, op Operation) (State, error) {
	return s.transition(ctx, "SELECT business.recovery_mark_prepared($1::uuid)", op.ID)
}

// MarkCommitted implements Store. The outcome is stored as exact JSON text
// so that its hash stays verifiable.
func (s SQLStore) MarkCommitted(ctx context.Context, op Operation, outcome Outcome, fromReconcile bool) (State, bool, error) {
	hash, body, err := outcome.encode()
	if err != nil {
		return "", false, err
	}
	var (
		state   string
		changed bool
	)
	err = s.db.QueryRow(ctx, "SELECT op_state, changed FROM business.recovery_mark_committed($1::uuid, $2::json, $3, $4)",
		op.ID, string(body), hash, fromReconcile).Scan(&state, &changed)
	return State(state), changed, err
}

// MarkSecured implements Store.
func (s SQLStore) MarkSecured(ctx context.Context, op Operation) (State, error) {
	return s.transition(ctx, "SELECT business.recovery_mark_secured($1::uuid)", op.ID)
}

// MarkReconcile implements Store.
func (s SQLStore) MarkReconcile(ctx context.Context, op Operation, reason string) (State, error) {
	if reason == "" || len(reason) > 200 {
		return "", errors.New("recovery: reconcile reason must be 1 to 200 characters")
	}
	return s.transition(ctx, "SELECT business.recovery_mark_reconcile($1::uuid, $2)", op.ID, reason)
}

// Frozen implements Store.
func (s SQLStore) Frozen(ctx context.Context) (bool, error) {
	var frozen bool
	err := s.db.QueryRow(ctx, "SELECT business.recovery_frozen()").Scan(&frozen)
	return frozen, err
}

// SetFreeze freezes or unfreezes outgoing effects (restore tooling only).
func (s SQLStore) SetFreeze(ctx context.Context, frozen bool, reason string) error {
	var ignored any
	return s.db.QueryRow(ctx, "SELECT business.recovery_set_freeze($1, $2)", frozen, reason).Scan(&ignored)
}
