package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5/pgconn"
)

// CmdFreeConfirm is the customer's explicit free confirmation (K03).
const CmdFreeConfirm = "order.free_confirm"

// FreeConfirmInput confirms a zero-amount order at the version the customer
// reviewed (K00 expected_version).
type FreeConfirmInput struct {
	ExpectedVersion int `json:"expected_version"`
}

// Validate checks the input.
func (in FreeConfirmInput) Validate() error {
	if in.ExpectedVersion < 1 {
		return invalid("expected_version", "must be the order's version")
	}
	return nil
}

// Confirming is the 202 answer to a free confirmation: the command is
// recorded and the order becomes confirmed once its K11 protection is
// complete; poll the order.
type Confirming struct {
	OrderID          string `json:"order_id"`
	OperationID      string `json:"operation_id"`
	State            string `json:"state"`
	PollAfterSeconds int    `json:"poll_after_seconds"`
}

var envPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// FreeConfirm records the customer's free confirmation of order id and
// enqueues its protected commit (migration 0013). subject is the signed-in
// owner's verified subject, or "" for a guest with the order capability
// digest.
func (s *Store) FreeConfirm(ctx context.Context, id, subject string, digest []byte, key string, in FreeConfirmInput) (catalog.Result, error) {
	if !catalog.ValidUUID(id) || (subject == "" && digest == nil) {
		return catalog.Result{}, catalog.ErrNotFound
	}
	if !envPattern.MatchString(s.Env) {
		return catalog.Result{}, errors.New("checkout: the recovery journal environment is not configured")
	}
	var owner *string
	if subject != "" {
		owner, digest = &subject, nil
	}
	var r catalog.Result
	err := s.DB.QueryRow(ctx, `SELECT result_id::text, replayed FROM business.order_free_confirm_request(
		$1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8)`,
		owner, digest, id, key, catalog.BodyHash(CmdFreeConfirm, id, in), in.ExpectedVersion, s.Env, s.now().UTC(),
	).Scan(&r.ID, &r.Replayed)
	if err != nil {
		return catalog.Result{}, gatewayError("free confirmation", err)
	}
	return r, nil
}

// Commands is the worker's job handler for protected order commands: it
// runs each under the K11 protocol (recovery.Coordinator.Command). Jobs of
// other kinds return jobs.ErrNoHandler.
type Commands struct {
	DB          Querier // gk_worker pool
	Coordinator *recovery.Coordinator
	Logger      *slog.Logger
}

// CommandFreezeRecheck is how long a command waits while a restore freezes
// protected effects.
const CommandFreezeRecheck = 5 * time.Minute

// JournalRetry is how long a command waits when no journal is configured.
const JournalRetry = 15 * time.Minute

const (
	reasonCommandPayload   = "order command payload does not match its operation"
	reasonCommandRecovery  = "recovery operation conflict for this order command"
	reasonCommandReconcile = "order command needs manager reconciliation"
)

// Handle runs the leased job's order command.
func (c *Commands) Handle(ctx context.Context, job jobs.Job) error {
	store := commandStore{db: c.DB, job: job}
	b, err := store.begin(ctx)
	if err != nil {
		return err
	}
	switch b.Status {
	case "ready":
	case "not_order_command":
		return jobs.ErrNoHandler
	case "frozen":
		return jobs.Defer(CommandFreezeRecheck, recovery.ErrFrozen)
	default:
		return fmt.Errorf("checkout: unexpected order command status %q", b.Status)
	}
	if c.Coordinator == nil {
		// No journal: nothing may be committed without its protection.
		return jobs.Defer(JournalRetry, errors.New("checkout: no recovery journal configured for order commands"))
	}
	op := recovery.Operation{ID: b.OperationID, Kind: recovery.KindCommand, AggregateID: b.OrderID,
		PriorVersion: b.PriorVersion, Payload: json.RawMessage(b.Payload)}
	if key, err := c.Coordinator.EnvelopeKey(op); err != nil || key != b.EnvelopeKey || !json.Valid(op.Payload) {
		return jobs.Attention(reasonCommandPayload, err)
	}
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	log := logger.With("job_id", job.ID, "generation", job.Generation, "order_id", b.OrderID, "operation_id", op.ID)
	outcome, err := c.Coordinator.Command(ctx, store, op, func(ctx context.Context) (recovery.Outcome, recovery.State, error) {
		return store.apply(ctx, op.ID)
	})
	switch {
	case err == nil:
		log.Info("order command secured", "outcome", outcome.Status)
		return nil
	case errors.Is(err, recovery.ErrFrozen):
		return jobs.Defer(CommandFreezeRecheck, err)
	case errors.Is(err, recovery.ErrNeedsReconcile):
		return jobs.Attention(reasonCommandReconcile, err)
	case errors.Is(err, recovery.ErrBusy), errors.Is(err, recovery.ErrStale):
		return jobs.Attention(reasonCommandRecovery, err)
	default:
		return err
	}
}

// commandStore is the recovery.Store for one leased order-command job; every
// call is fenced by the job lease (business.order_command_*).
type commandStore struct {
	db  Querier
	job jobs.Job
}

var _ recovery.Store = commandStore{}

// ErrLeaseLost means the worker no longer holds the job's live lease.
var ErrLeaseLost = errors.New("checkout: job lease lost")

func leaseErr(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "GK001":
		return fmt.Errorf("%w: %s", ErrLeaseLost, pgErr.Message)
	case errors.As(err, &pgErr) && pgErr.Code == "GK002":
		// The commit found a restore freeze under its own lock.
		return fmt.Errorf("%w (checked at the commit)", recovery.ErrFrozen)
	}
	return err
}

func (s commandStore) args(extra ...any) []any {
	return append([]any{s.job.ID, s.job.Worker, s.job.Generation}, extra...)
}

type commandBegin struct {
	Status       string
	OperationID  string
	OrderID      string
	PriorVersion int
	Payload      string
	EnvelopeKey  string
}

func (s commandStore) begin(ctx context.Context) (commandBegin, error) {
	var (
		b                     commandBegin
		op, order, payload, k *string
		prior                 *int
	)
	err := s.db.QueryRow(ctx, `SELECT status, operation_id::text, order_id::text, prior_version, payload, envelope_key
		FROM business.order_command_begin($1::uuid, $2::uuid, $3)`, s.args()...).Scan(&b.Status, &op, &order, &prior, &payload, &k)
	if err != nil {
		return commandBegin{}, leaseErr(err)
	}
	if op != nil && order != nil && prior != nil && payload != nil && k != nil {
		b.OperationID, b.OrderID, b.PriorVersion, b.Payload, b.EnvelopeKey = *op, *order, *prior, *payload, *k
	}
	return b, nil
}

func decodeOutcome(opID string, raw []byte, hash *string) (*recovery.Outcome, error) {
	if raw == nil {
		return nil, nil
	}
	if hash == nil || recovery.HashHex(raw) != *hash {
		return nil, fmt.Errorf("checkout: recorded outcome of %s does not match its hash", opID)
	}
	var o recovery.Outcome
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("checkout: decode recorded outcome: %w", err)
	}
	return &o, nil
}

func (s commandStore) Reserve(ctx context.Context, op recovery.Operation, envelopeKey string) (recovery.Reservation, error) {
	var (
		res   recovery.Reservation
		state *string
		raw   []byte
		hash  *string
	)
	err := s.db.QueryRow(ctx, `SELECT result, op_state, op_outcome, op_outcome_hash
		FROM business.order_command_reserve($1::uuid, $2::uuid, $3, $4::uuid, $5, $6::uuid, $7, $8, $9)`,
		s.args(op.ID, string(op.Kind), op.AggregateID, op.PriorVersion, op.PayloadHash(), envelopeKey)...,
	).Scan(&res.Result, &state, &raw, &hash)
	if err != nil {
		return recovery.Reservation{}, leaseErr(err)
	}
	if state != nil {
		res.State = recovery.State(*state)
	}
	if res.Outcome, err = decodeOutcome(op.ID, raw, hash); err != nil {
		return recovery.Reservation{}, err
	}
	return res, nil
}

// Lookup is restore and manager tooling, never the worker's.
func (s commandStore) Lookup(context.Context, recovery.Operation, string) (recovery.Reservation, error) {
	return recovery.Reservation{}, errors.New("checkout: the worker cannot look up or resolve operations")
}

func (s commandStore) MarkPrepared(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	var state string
	err := s.db.QueryRow(ctx, "SELECT business.order_command_prepared($1::uuid, $2::uuid, $3, $4::uuid)", s.args(op.ID)...).Scan(&state)
	return recovery.State(state), leaseErr(err)
}

// MarkCommitted is never used for commands: the domain commit records the
// committed phase itself (apply).
func (s commandStore) MarkCommitted(context.Context, recovery.Operation, recovery.Outcome, bool) (recovery.State, bool, error) {
	return "", false, errors.New("checkout: order commands commit through their domain apply")
}

func (s commandStore) MarkSecured(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	var state string
	err := s.db.QueryRow(ctx, "SELECT business.order_command_secured($1::uuid, $2::uuid, $3, $4::uuid)", s.args(op.ID)...).Scan(&state)
	return recovery.State(state), leaseErr(err)
}

// MarkReconcile is never used for commands (their commit is local).
func (s commandStore) MarkReconcile(context.Context, recovery.Operation, string) (recovery.State, error) {
	return "", errors.New("checkout: order commands do not go to reconciliation")
}

func (s commandStore) Frozen(ctx context.Context) (bool, error) {
	var frozen bool
	err := s.db.QueryRow(ctx, "SELECT business.order_command_frozen($1::uuid, $2::uuid, $3)", s.args()...).Scan(&frozen)
	return frozen, leaseErr(err)
}

func (s commandStore) apply(ctx context.Context, opID string) (recovery.Outcome, recovery.State, error) {
	var (
		state string
		raw   []byte
		hash  *string
	)
	err := s.db.QueryRow(ctx, `SELECT op_state, op_outcome, op_outcome_hash
		FROM business.order_command_apply($1::uuid, $2::uuid, $3, $4::uuid)`, s.args(opID)...).Scan(&state, &raw, &hash)
	if err != nil {
		return recovery.Outcome{}, "", leaseErr(err)
	}
	o, err := decodeOutcome(opID, raw, hash)
	if err != nil {
		return recovery.Outcome{}, "", err
	}
	if o == nil {
		return recovery.Outcome{}, "", fmt.Errorf("checkout: command %s has no recorded outcome after its commit", opID)
	}
	return *o, recovery.State(state), nil
}
