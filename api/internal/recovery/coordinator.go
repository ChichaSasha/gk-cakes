package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Kind is the kind of protected operation.
type Kind string

// Operation kinds.
const (
	// KindDispatch protects an outgoing provider effect (email now; payments
	// and refunds later) that is idempotent for its identity until
	// ReplayUntil. Run it with Dispatch.
	KindDispatch Kind = "dispatch"
	// KindCommand protects an accepted domain command (an order confirmation
	// or change, K11): its exact accepted payload is acknowledged in the
	// independent journal before the domain commit, and its outcome before
	// the result is exposed. The commit is local and idempotent for the
	// operation's identity forever, so a command has no replay deadline. Run
	// it with Command.
	KindCommand Kind = "command"
)

// State is a protected operation's recovery phase.
type State string

// Recovery phases (see migration 0003).
const (
	StateReserved  State = "reserved"
	StatePrepared  State = "prepared"
	StateCommitted State = "committed"
	StateSecured   State = "secured"
	StateReconcile State = "reconcile"
)

// Errors returned by Dispatch and Resolve.
var (
	ErrFrozen         = errors.New("recovery: outgoing effects are frozen")
	ErrBusy           = errors.New("recovery: another operation for this aggregate is unresolved")
	ErrStale          = errors.New("recovery: the aggregate already has an operation at this or a later version")
	ErrNeedsReconcile = errors.New("recovery: operation needs manager reconciliation")
)

// Operation identifies one protected dispatch.
//
// Payload is the exact, immutable provider request the effect sends: for an
// email, the provider account and environment, idempotency key and the full
// request (from, reply-to, recipient, subject, rendered body, headers). After
// a primary database loss it is the only copy, so the effect must be
// rebuildable from it alone; providers such as Resend reject a repeated key
// with a different body. It must be byte-identical whenever the operation is
// resumed; a different payload under the same ID is refused.
//
// AggregateID is the object whose dispatches are serialized: use the
// dispatch's own intent (for example the notification intent), not the order,
// so an unresolved email never blocks unrelated order work. PriorVersion
// numbers successive operations on it (0 for the first; a successor resend
// uses the next number); it only increases.
type Operation struct {
	ID           string
	Kind         Kind
	AggregateID  string
	PriorVersion int
	Payload      json.RawMessage
	// ReplayUntil is the end of the provider's idempotency window, fixed at
	// the first reservation with a safety margin (for Resend's 24 hours, at
	// most first dispatch + 23 hours) and never extended: until then,
	// repeating the effect with the same identity cannot cause a second
	// external effect; afterwards an unknown outcome goes to reconciliation.
	ReplayUntil time.Time
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (op Operation) validate() error {
	switch {
	case !uuidPattern.MatchString(op.ID) || !uuidPattern.MatchString(op.AggregateID):
		return errors.New("recovery: operation and aggregate IDs must be lower-case UUIDs")
	case op.Kind != KindDispatch && op.Kind != KindCommand:
		return fmt.Errorf("recovery: unsupported operation kind %q", op.Kind)
	case op.PriorVersion < 0:
		return errors.New("recovery: prior version must not be negative")
	case !json.Valid(op.Payload):
		return errors.New("recovery: payload must be JSON")
	case op.Kind == KindDispatch && op.ReplayUntil.IsZero():
		return errors.New("recovery: a dispatch needs its provider replay deadline")
	case op.Kind == KindCommand && !op.ReplayUntil.IsZero():
		return errors.New("recovery: a command has no replay deadline")
	}
	return nil
}

// replayUntil is the replay deadline as recorded: none for a command.
func (op Operation) replayUntil() *time.Time {
	if op.Kind == KindCommand {
		return nil
	}
	t := op.ReplayUntil
	return &t
}

// ReplayUntilArg is the replay deadline as a store passes it (nil for a
// command; the database records 'infinity').
func (op Operation) ReplayUntilArg() *time.Time { return op.replayUntil() }

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashHex is the hex SHA-256 used for payload and outcome hashes.
func HashHex(b []byte) string { return hashHex(b) }

// PayloadHash is the hex SHA-256 of the payload bytes.
func (op Operation) PayloadHash() string { return hashHex(op.Payload) }

// Outcome is the bounded result of an effect, recorded locally and in the
// independent outcome envelope.
type Outcome struct {
	Status   string          `json:"status"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}

// MaxOutcomeBytes bounds an outcome's JSON encoding (database limit).
const MaxOutcomeBytes = 4096

var statusPattern = regexp.MustCompile(`^[a-z][a-z_]{0,39}$`)

// encode returns the outcome's canonical JSON and its hash, refusing
// outcomes the recovery record cannot hold.
func (o Outcome) encode() (string, []byte, error) {
	if !statusPattern.MatchString(o.Status) {
		return "", nil, fmt.Errorf("recovery: outcome status %q must be lower_snake_case, at most 40 characters", o.Status)
	}
	b, err := json.Marshal(o)
	if err != nil {
		return "", nil, err
	}
	if len(b) > MaxOutcomeBytes {
		return "", nil, fmt.Errorf("recovery: outcome is %d bytes, more than %d", len(b), MaxOutcomeBytes)
	}
	return hashHex(b), b, nil
}

// Encode returns the hash and canonical JSON a store records for the
// outcome, refusing outcomes the recovery record cannot hold.
func (o Outcome) Encode() (hash string, body []byte, err error) { return o.encode() }

func sameOutcome(a, b Outcome) bool {
	ha, _, errA := a.encode()
	hb, _, errB := b.encode()
	return errA == nil && errB == nil && ha == hb
}

// Reservation is the result of Store.Reserve or Store.Lookup.
type Reservation struct {
	Result  string // "reserved", "existing", "busy", "stale" or (Lookup) "missing"
	State   State
	Outcome *Outcome
}

// Store persists recovery phases, each committed before Dispatch makes its
// next journal or provider call (never one transaction across them). Domain
// implementations change their own state in the same transaction as each
// phase (for example an email intent becomes "sending" when its dispatch is
// marked prepared).
type Store interface {
	Reserve(ctx context.Context, op Operation, envelopeKey string) (Reservation, error)
	// Lookup reads an existing operation without reserving ("existing" or
	// "missing"), with the same identity check as Reserve.
	Lookup(ctx context.Context, op Operation, envelopeKey string) (Reservation, error)
	MarkPrepared(ctx context.Context, op Operation) (State, error)
	// MarkCommitted records the outcome (from prepared, or from reconcile when
	// fromReconcile); changed reports whether this call recorded it.
	MarkCommitted(ctx context.Context, op Operation, outcome Outcome, fromReconcile bool) (state State, changed bool, err error)
	MarkSecured(ctx context.Context, op Operation) (State, error)
	MarkReconcile(ctx context.Context, op Operation, reason string) (State, error)
	Frozen(ctx context.Context) (bool, error)
}

// Effect performs the dispatch, built only from the operation's payload. It
// must be idempotent for the operation's identity until ReplayUntil (a
// provider idempotency key with the same body). An error means the outcome is
// unknown; the operation stays prepared.
type Effect func(ctx context.Context) (Outcome, error)

// Coordinator runs dispatches under the K11 protocol: the prepared envelope
// is independently acknowledged before the effect, and the outcome envelope
// before the operation is secured. No database transaction is held across
// journal or effect calls. Callers serialize work on one operation (for
// example with a job lease); concurrent calls stay safe through the store's
// checks and provider idempotency but may repeat journal reads.
type Coordinator struct {
	Journal     Journal
	Sealer      *Sealer
	Environment string // object path segment, e.g. "staging"
	Now         func() time.Time
}

var segmentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func (c *Coordinator) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Coordinator) root() (string, error) {
	if !segmentPattern.MatchString(c.Environment) || c.Journal == nil || c.Sealer == nil {
		return "", errors.New("recovery: coordinator needs a journal, a sealer and a lower-case environment")
	}
	return "recovery/" + c.Environment + "/", nil
}

// EnvelopeKey is the operation's journal base name. Names carry only IDs and
// a version (no personal data); lexical order is per-aggregate version order.
func (c *Coordinator) EnvelopeKey(op Operation) (string, error) {
	root, err := c.root()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%sop/%s/%010d-%s", root, op.AggregateID, op.PriorVersion, op.ID), nil
}

const (
	phasePrepared = "prepared"
	phaseOutcome  = "outcome"
)

// envelopeVersion 2 carries the payload as opaque bytes (base64): JSON
// re-encoding would escape or compact it, and a replay must send the exact
// bytes. No version 1 envelope was ever written outside tests.
const envelopeVersion = 2

type envelope struct {
	V            int        `json:"v"`
	Environment  string     `json:"environment"`
	Phase        string     `json:"phase"`
	OperationID  string     `json:"operation_id"`
	Kind         Kind       `json:"kind"`
	AggregateID  string     `json:"aggregate_id"`
	PriorVersion int        `json:"prior_version"`
	PayloadHash  string     `json:"payload_hash"`
	Payload      []byte     `json:"payload,omitempty"`
	ReplayUntil  *time.Time `json:"replay_until,omitempty"`
	Outcome      *Outcome   `json:"outcome,omitempty"`
	SealedAt     time.Time  `json:"sealed_at"`
}

// matches reports whether an existing envelope belongs to the operation as
// env describes it: same identity, and for a prepared envelope the same
// payload bytes and replay deadline (a resume must never extend the
// provider window).
func (got envelope) matches(env envelope, phase string) bool {
	switch {
	case got.V != envelopeVersion || got.Phase != phase || got.OperationID != env.OperationID ||
		got.PayloadHash != env.PayloadHash || got.AggregateID != env.AggregateID || got.PriorVersion != env.PriorVersion:
		return false
	case got.Kind != env.Kind:
		return false
	case phase == phasePrepared && env.Kind == KindCommand:
		return hashHex(got.Payload) == env.PayloadHash && got.ReplayUntil == nil && env.ReplayUntil == nil
	case phase == phasePrepared:
		return hashHex(got.Payload) == env.PayloadHash && got.ReplayUntil != nil && env.ReplayUntil != nil &&
			got.ReplayUntil.Equal(*env.ReplayUntil)
	case phase == phaseOutcome:
		return got.Outcome != nil && env.Outcome != nil && sameOutcome(*got.Outcome, *env.Outcome)
	}
	return false
}

// ensure makes sure the envelope for phase exists in the journal. When the
// operation may have written it before (fresh is false) an existing copy is
// verified against the operation (and recorded outcome) and reused, so a
// resumed operation does not write a second version.
func (c *Coordinator) ensure(ctx context.Context, key, phase string, env envelope, fresh bool) error {
	name := key + "." + phase
	if !fresh {
		data, err := c.Journal.Get(ctx, name)
		switch {
		case err == nil:
			plain, err := c.Sealer.Open(name, data)
			if err != nil {
				return fmt.Errorf("recovery: existing %s envelope: %w", phase, err)
			}
			var got envelope
			if err := json.Unmarshal(plain, &got); err != nil || !got.matches(env, phase) {
				return fmt.Errorf("recovery: existing %s envelope for %s does not match the operation", phase, env.OperationID)
			}
			return nil
		case !errors.Is(err, ErrNotFound):
			return fmt.Errorf("recovery: read %s envelope: %w", phase, err)
		}
	}
	env.Phase = phase
	env.SealedAt = c.now().UTC()
	plain, err := json.Marshal(env)
	if err != nil {
		return err
	}
	var check envelope
	if err := json.Unmarshal(plain, &check); err != nil || !check.matches(env, phase) {
		return fmt.Errorf("recovery: %s envelope for %s does not round-trip", phase, env.OperationID)
	}
	sealed, err := c.Sealer.Seal(name, plain)
	if err != nil {
		return err
	}
	if err := c.Journal.Put(ctx, name, sealed); err != nil {
		return fmt.Errorf("recovery: write %s envelope: %w", phase, err)
	}
	return nil
}

func (c *Coordinator) start(ctx context.Context, store Store, op Operation) (string, Reservation, envelope, error) {
	if err := op.validate(); err != nil {
		return "", Reservation{}, envelope{}, err
	}
	key, err := c.EnvelopeKey(op)
	if err != nil {
		return "", Reservation{}, envelope{}, err
	}
	res, err := store.Reserve(ctx, op, key)
	if err != nil {
		return "", Reservation{}, envelope{}, fmt.Errorf("recovery: reserve: %w", err)
	}
	switch res.Result {
	case "busy":
		return "", Reservation{}, envelope{}, ErrBusy
	case "stale":
		return "", Reservation{}, envelope{}, ErrStale
	}
	env := envelope{
		V: envelopeVersion, Environment: c.Environment, OperationID: op.ID, Kind: op.Kind,
		AggregateID: op.AggregateID, PriorVersion: op.PriorVersion, PayloadHash: op.PayloadHash(),
	}
	return key, res, env, nil
}

// secure writes the outcome envelope (reusing an existing one unless this
// call just recorded the outcome) and marks the operation secured.
func (c *Coordinator) secure(ctx context.Context, store Store, op Operation, key string, env envelope, outcome Outcome, fresh bool) (State, error) {
	env.Outcome = &outcome
	if err := c.ensure(ctx, key, phaseOutcome, env, fresh); err != nil {
		return "", err
	}
	state, err := store.MarkSecured(ctx, op)
	if err != nil {
		return "", fmt.Errorf("recovery: mark secured: %w", err)
	}
	return state, nil
}

// Dispatch runs op to completion or to the first failure, resuming from
// whatever phase a previous attempt reached:
//
//	reserve -> prepared envelope -> prepared -> effect -> committed ->
//	outcome envelope -> secured
//
// The effect runs only after the prepared envelope is acknowledged and only
// while outgoing effects are not frozen (checked at the start and again
// right before it). A prepared dispatch whose replay deadline has passed, or
// whose effect returned an outcome that cannot be recorded, is sent to
// reconciliation instead of being repeated. Errors other than ErrBusy,
// ErrStale, ErrFrozen and ErrNeedsReconcile are retryable by calling
// Dispatch again with the same operation.
func (c *Coordinator) Dispatch(ctx context.Context, store Store, op Operation, effect Effect) (Outcome, error) {
	op.ReplayUntil = op.ReplayUntil.UTC().Truncate(time.Microsecond)
	if err := op.validate(); err != nil {
		return Outcome{}, err
	}
	if op.Kind != KindDispatch {
		return Outcome{}, errors.New("recovery: Dispatch runs dispatch operations only; use Command")
	}
	if _, err := c.EnvelopeKey(op); err != nil {
		return Outcome{}, err
	}
	if err := c.checkFrozen(ctx, store); err != nil {
		return Outcome{}, err
	}
	key, res, env, err := c.start(ctx, store, op)
	if err != nil {
		return Outcome{}, err
	}
	state, outcome := res.State, res.Outcome

	if state == StateReserved {
		prepared := env
		prepared.Payload = op.Payload
		prepared.ReplayUntil = op.replayUntil()
		// Always check for an existing copy: a restore replay reserves anew
		// in a fresh database although the envelope already exists.
		if err := c.ensure(ctx, key, phasePrepared, prepared, false); err != nil {
			return Outcome{}, err
		}
		if state, err = store.MarkPrepared(ctx, op); err != nil {
			return Outcome{}, fmt.Errorf("recovery: mark prepared: %w", err)
		}
	}

	justCommitted := false
	if state == StatePrepared {
		if !c.now().Before(op.ReplayUntil) {
			return Outcome{}, c.reconcile(ctx, store, op, "provider replay window ended before the outcome was recorded")
		}
		if err := c.checkFrozen(ctx, store); err != nil {
			return Outcome{}, err
		}
		got, err := effect(ctx)
		if err != nil {
			return Outcome{}, fmt.Errorf("recovery: effect outcome unknown: %w", err)
		}
		if _, _, err := got.encode(); err != nil {
			// The effect happened but its outcome cannot be recorded; never
			// repeat it.
			return Outcome{}, c.reconcile(ctx, store, op, "effect outcome could not be recorded")
		}
		if state, justCommitted, err = store.MarkCommitted(ctx, op, got, false); err != nil {
			return Outcome{}, fmt.Errorf("recovery: mark committed: %w", err)
		}
		outcome = &got
	}

	if state == StateCommitted {
		if outcome == nil {
			return Outcome{}, fmt.Errorf("recovery: committed operation %s has no recorded outcome", op.ID)
		}
		if state, err = c.secure(ctx, store, op, key, env, *outcome, justCommitted); err != nil {
			return Outcome{}, err
		}
	}

	switch {
	case state == StateSecured && outcome != nil:
		return *outcome, nil
	case state == StateReconcile:
		return Outcome{}, ErrNeedsReconcile
	default:
		return Outcome{}, fmt.Errorf("recovery: operation %s ended in unexpected state %q", op.ID, state)
	}
}

// Apply performs an accepted command's domain commit: in one database
// transaction it re-validates the command against the current aggregate,
// changes the domain state (or decides a refusal) and records the outcome as
// the operation's committed phase. It must be idempotent for the operation:
// when the operation is already committed it returns the recorded outcome
// and state without changing anything. An error leaves the operation
// prepared (retryable).
type Apply func(ctx context.Context) (Outcome, State, error)

// Command runs an accepted command (KindCommand) to completion or to the
// first failure, resuming from whatever phase a previous attempt reached:
//
//	reserve -> prepared envelope -> prepared -> domain commit (committed) ->
//	outcome envelope -> secured
//
// The domain commit happens only after the exact accepted payload is
// independently acknowledged, and only while outgoing effects are not
// frozen (restore). Callers expose the committed result only once the
// operation is secured (K11: a commit without the outcome acknowledgment
// stays confirmation_pending). Errors other than ErrBusy, ErrStale,
// ErrFrozen and ErrNeedsReconcile are retryable by calling Command again
// with the same operation.
func (c *Coordinator) Command(ctx context.Context, store Store, op Operation, apply Apply) (Outcome, error) {
	if err := op.validate(); err != nil {
		return Outcome{}, err
	}
	if op.Kind != KindCommand {
		return Outcome{}, errors.New("recovery: Command runs command operations only; use Dispatch")
	}
	if err := c.checkFrozen(ctx, store); err != nil {
		return Outcome{}, err
	}
	key, res, env, err := c.start(ctx, store, op)
	if err != nil {
		return Outcome{}, err
	}
	state, outcome := res.State, res.Outcome

	if state == StateReserved {
		prepared := env
		prepared.Payload = op.Payload
		if err := c.ensure(ctx, key, phasePrepared, prepared, false); err != nil {
			return Outcome{}, err
		}
		if state, err = store.MarkPrepared(ctx, op); err != nil {
			return Outcome{}, fmt.Errorf("recovery: mark prepared: %w", err)
		}
	}

	if state == StatePrepared {
		if err := c.checkFrozen(ctx, store); err != nil {
			return Outcome{}, err
		}
		got, gotState, err := apply(ctx)
		if err != nil {
			return Outcome{}, fmt.Errorf("recovery: command commit: %w", err)
		}
		if _, _, err := got.encode(); err != nil {
			return Outcome{}, fmt.Errorf("recovery: command outcome: %w", err)
		}
		state, outcome = gotState, &got
	}

	if state == StateCommitted {
		if outcome == nil {
			return Outcome{}, fmt.Errorf("recovery: committed operation %s has no recorded outcome", op.ID)
		}
		// Always check for an existing outcome envelope: the apply may have
		// found the commit already recorded by an earlier attempt.
		if state, err = c.secure(ctx, store, op, key, env, *outcome, false); err != nil {
			return Outcome{}, err
		}
	}

	switch {
	case state == StateSecured && outcome != nil:
		return *outcome, nil
	case state == StateReconcile:
		return Outcome{}, ErrNeedsReconcile
	default:
		return Outcome{}, fmt.Errorf("recovery: operation %s ended in unexpected state %q", op.ID, state)
	}
}

func (c *Coordinator) reconcile(ctx context.Context, store Store, op Operation, reason string) error {
	if _, err := store.MarkReconcile(ctx, op, reason); err != nil {
		return fmt.Errorf("recovery: mark reconcile: %w", err)
	}
	return ErrNeedsReconcile
}

// Resolve records a manager-determined outcome for an operation in
// reconciliation (for example "sent" with evidence from the provider
// dashboard, or "abandoned"), journals it and secures the operation, which
// frees the aggregate for a successor at the next prior version. It is
// idempotent for the same outcome and refuses a different one.
func (c *Coordinator) Resolve(ctx context.Context, store Store, op Operation, outcome Outcome) (Outcome, error) {
	if !op.ReplayUntil.IsZero() {
		op.ReplayUntil = op.ReplayUntil.UTC().Truncate(time.Microsecond)
	}
	if _, _, err := outcome.encode(); err != nil {
		return Outcome{}, err
	}
	if err := op.validate(); err != nil {
		return Outcome{}, err
	}
	key, err := c.EnvelopeKey(op)
	if err != nil {
		return Outcome{}, err
	}
	// Read-only: resolving must never reserve an operation the database
	// does not know (for example before a restore replay).
	res, err := store.Lookup(ctx, op, key)
	if err != nil {
		return Outcome{}, fmt.Errorf("recovery: lookup: %w", err)
	}
	if res.Result != "existing" {
		return Outcome{}, fmt.Errorf("recovery: operation %s is not recorded; nothing to resolve", op.ID)
	}
	env := envelope{
		V: envelopeVersion, Environment: c.Environment, OperationID: op.ID, Kind: op.Kind,
		AggregateID: op.AggregateID, PriorVersion: op.PriorVersion, PayloadHash: op.PayloadHash(),
	}
	state, changed := res.State, false
	switch state {
	case StateReconcile:
		if state, changed, err = store.MarkCommitted(ctx, op, outcome, true); err != nil {
			return Outcome{}, fmt.Errorf("recovery: resolve: %w", err)
		}
	case StateCommitted, StateSecured:
		if res.Outcome == nil || !sameOutcome(*res.Outcome, outcome) {
			return Outcome{}, fmt.Errorf("recovery: operation %s already has a different outcome", op.ID)
		}
	default:
		return Outcome{}, fmt.Errorf("recovery: operation %s is %s, not in reconciliation", op.ID, state)
	}
	if state == StateCommitted {
		if state, err = c.secure(ctx, store, op, key, env, outcome, changed); err != nil {
			return Outcome{}, err
		}
	}
	if state != StateSecured {
		return Outcome{}, fmt.Errorf("recovery: operation %s ended in unexpected state %q", op.ID, state)
	}
	return outcome, nil
}

func (c *Coordinator) checkFrozen(ctx context.Context, store Store) error {
	frozen, err := store.Frozen(ctx)
	if err != nil {
		return fmt.Errorf("recovery: freeze check: %w", err)
	}
	if frozen {
		return ErrFrozen
	}
	return nil
}

// Ready reports whether the independent journal is reachable and correctly
// scoped. Callers block new protected operations while it is not.
func (c *Coordinator) Ready(ctx context.Context) error {
	if _, err := c.root(); err != nil {
		return err
	}
	return c.Journal.Ping(ctx)
}

// Pending returns, in journal order, the operations whose prepared envelope
// has no outcome envelope: their effect may or may not have happened. After a
// primary database loss, restore tooling resumes each dispatch with Dispatch
// and the same Effect (provider-idempotent until ReplayUntil; after that
// deadline Dispatch sends it to reconciliation instead of repeating it), and
// each command with Command after checking the restored aggregate.
func (c *Coordinator) Pending(ctx context.Context) ([]Operation, error) {
	root, err := c.root()
	if err != nil {
		return nil, err
	}
	names, err := c.Journal.List(ctx, root+"op/")
	if err != nil {
		return nil, err
	}
	phases := map[string]map[string]bool{}
	for _, name := range names {
		base, phase, ok := cutPhase(name)
		if !ok {
			continue
		}
		if phases[base] == nil {
			phases[base] = map[string]bool{}
		}
		phases[base][phase] = true
	}
	bases := make([]string, 0, len(phases))
	for base, p := range phases {
		if p[phasePrepared] && !p[phaseOutcome] {
			bases = append(bases, base)
		}
	}
	sort.Strings(bases)
	ops := make([]Operation, 0, len(bases))
	for _, base := range bases {
		name := base + "." + phasePrepared
		data, err := c.Journal.Get(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("recovery: read %s: %w", name, err)
		}
		plain, err := c.Sealer.Open(name, data)
		if err != nil {
			return nil, fmt.Errorf("recovery: open %s: %w", name, err)
		}
		var env envelope
		if err := json.Unmarshal(plain, &env); err != nil {
			return nil, fmt.Errorf("recovery: decode %s: %w", name, err)
		}
		op := Operation{ID: env.OperationID, Kind: env.Kind, AggregateID: env.AggregateID, PriorVersion: env.PriorVersion, Payload: env.Payload}
		if env.Kind == KindCommand && env.ReplayUntil != nil {
			return nil, fmt.Errorf("recovery: %s is a command with a replay deadline", name)
		}
		if env.ReplayUntil != nil {
			op.ReplayUntil = *env.ReplayUntil
		}
		if env.V != envelopeVersion || op.PayloadHash() != env.PayloadHash || env.Phase != phasePrepared {
			return nil, fmt.Errorf("recovery: %s does not match its payload hash", name)
		}
		if key, _ := c.EnvelopeKey(op); key != base {
			return nil, fmt.Errorf("recovery: %s is stored under the wrong name", name)
		}
		ops = append(ops, op)
	}
	return ops, nil
}

func cutPhase(name string) (string, string, bool) {
	for _, phase := range []string{phasePrepared, phaseOutcome} {
		if base, ok := strings.CutSuffix(name, "."+phase); ok {
			return base, phase, true
		}
	}
	return "", "", false
}

// MaxBlobBytes bounds one essential attachment copy.
const MaxBlobBytes = 25 << 20

// CopyBlob stores an essential attachment's bytes, sealed, in the journal
// and returns its reference ("sha256:<hex>"). Call it before committing the
// attachment reference; identical bytes are stored once per key. Object names
// are keyed (see Sealer), so the bucket does not reveal content hashes.
func (c *Coordinator) CopyBlob(ctx context.Context, data []byte) (string, error) {
	root, err := c.root()
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > MaxBlobBytes {
		return "", fmt.Errorf("recovery: blob must be 1 to %d bytes", MaxBlobBytes)
	}
	sum := hashHex(data)
	name := root + "blob/" + c.Sealer.blobTokens(sum)[0]
	if existing, err := c.Journal.Get(ctx, name); err == nil {
		if plain, err := c.Sealer.Open(name, existing); err == nil && hashHex(plain) == sum {
			return "sha256:" + sum, nil
		}
		// An unreadable or different copy is superseded by a new version.
	} else if !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("recovery: read blob: %w", err)
	}
	sealed, err := c.Sealer.Seal(name, data)
	if err != nil {
		return "", err
	}
	if err := c.Journal.Put(ctx, name, sealed); err != nil {
		return "", fmt.Errorf("recovery: write blob: %w", err)
	}
	return "sha256:" + sum, nil
}

var blobRef = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)

// ReadBlob returns the bytes for a reference made by CopyBlob, verified
// against the reference hash. Copies made under any configured key are found.
func (c *Coordinator) ReadBlob(ctx context.Context, ref string) ([]byte, error) {
	root, err := c.root()
	if err != nil {
		return nil, err
	}
	m := blobRef.FindStringSubmatch(ref)
	if m == nil {
		return nil, fmt.Errorf("recovery: %q is not a blob reference", ref)
	}
	lastErr := ErrNotFound
	for _, token := range c.Sealer.blobTokens(m[1]) {
		name := root + "blob/" + token
		data, err := c.Journal.Get(ctx, name)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		plain, err := c.Sealer.Open(name, data)
		if err == nil && hashHex(plain) != m[1] {
			err = fmt.Errorf("recovery: blob %s does not match its reference", ref)
		}
		if err != nil {
			lastErr = err // try the copies under other keys
			continue
		}
		return plain, nil
	}
	return nil, lastErr
}
