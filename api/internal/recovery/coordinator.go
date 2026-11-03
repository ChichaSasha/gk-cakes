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
	// KindDispatch protects an outgoing provider effect (email, payment,
	// refund) that is idempotent for its identity until ReplayUntil.
	KindDispatch Kind = "dispatch"
	// KindCommand protects an accepted business command (for example an
	// order confirmation or change) committed by the effect.
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

// Errors returned by Dispatch.
var (
	ErrFrozen         = errors.New("recovery: outgoing effects are frozen")
	ErrBusy           = errors.New("recovery: another operation for this aggregate is unresolved")
	ErrNeedsReconcile = errors.New("recovery: operation needs manager reconciliation")
)

// Operation identifies one protected operation. Payload is the exact,
// immutable accepted intent (for a dispatch: provider, account, environment,
// idempotency key and body hash). It must be byte-identical whenever the same
// operation is resumed; a different payload under the same ID is refused.
type Operation struct {
	ID           string
	Kind         Kind
	AggregateID  string
	PriorVersion int
	Payload      json.RawMessage
	// ReplayUntil is required for dispatches: until then, repeating the
	// effect with the same identity cannot cause a second external effect.
	ReplayUntil time.Time
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (op Operation) validate() error {
	switch {
	case !uuidPattern.MatchString(op.ID) || !uuidPattern.MatchString(op.AggregateID):
		return errors.New("recovery: operation and aggregate IDs must be lower-case UUIDs")
	case op.Kind != KindDispatch && op.Kind != KindCommand:
		return fmt.Errorf("recovery: unknown operation kind %q", op.Kind)
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

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// PayloadHash is the hex SHA-256 of the payload bytes.
func (op Operation) PayloadHash() string { return hashHex(op.Payload) }

// Outcome is the bounded result of an effect, recorded locally and in the
// independent outcome envelope.
type Outcome struct {
	Status   string          `json:"status"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}

func (o Outcome) hash() (string, []byte, error) {
	b, err := json.Marshal(o)
	if err != nil {
		return "", nil, err
	}
	return hashHex(b), b, nil
}

// Reservation is the result of Store.Reserve.
type Reservation struct {
	Result  string // "reserved", "existing" or "busy"
	State   State
	Outcome *Outcome
}

// Store persists recovery phases. Domain implementations change their own
// state in the same transaction as each phase (for example an email intent
// becomes "sending" when its dispatch is marked prepared).
type Store interface {
	Reserve(ctx context.Context, op Operation, envelopeKey string) (Reservation, error)
	MarkPrepared(ctx context.Context, op Operation) (State, error)
	MarkCommitted(ctx context.Context, op Operation, outcome Outcome) (State, error)
	MarkSecured(ctx context.Context, op Operation) (State, error)
	MarkReconcile(ctx context.Context, op Operation, reason string) (State, error)
	Frozen(ctx context.Context) (bool, error)
}

// Effect performs the protected operation's external effect. For a
// dispatch it must be idempotent for the operation's identity until
// ReplayUntil (a provider idempotency key with the same body). An error
// means the outcome is unknown; the operation stays prepared.
type Effect func(ctx context.Context) (Outcome, error)

// Coordinator runs operations under the K11 protocol: the prepared envelope
// is independently acknowledged before the effect, and the outcome envelope
// before the operation is secured. No database transaction is held across
// journal or effect calls.
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

type envelope struct {
	V            int             `json:"v"`
	Environment  string          `json:"environment"`
	Phase        string          `json:"phase"`
	OperationID  string          `json:"operation_id"`
	Kind         Kind            `json:"kind"`
	AggregateID  string          `json:"aggregate_id"`
	PriorVersion int             `json:"prior_version"`
	PayloadHash  string          `json:"payload_hash"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	ReplayUntil  *time.Time      `json:"replay_until,omitempty"`
	Outcome      *Outcome        `json:"outcome,omitempty"`
	SealedAt     time.Time       `json:"sealed_at"`
}

// ensure makes sure the envelope for phase exists in the journal. When the
// operation may have written it before (fresh is false) an existing copy is
// verified and reused, so a resumed operation never writes a second version.
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
			if err := json.Unmarshal(plain, &got); err != nil || got.OperationID != env.OperationID ||
				got.PayloadHash != env.PayloadHash || got.Phase != phase {
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
	sealed, err := c.Sealer.Seal(name, plain)
	if err != nil {
		return err
	}
	if err := c.Journal.Put(ctx, name, sealed); err != nil {
		return fmt.Errorf("recovery: write %s envelope: %w", phase, err)
	}
	return nil
}

// Dispatch runs op to completion or to the first failure, resuming from
// whatever phase a previous attempt reached:
//
//	reserve -> prepared envelope -> prepared -> effect -> committed ->
//	outcome envelope -> secured
//
// The effect runs only after the prepared envelope is acknowledged and only
// while outgoing effects are not frozen. A prepared dispatch whose replay
// deadline has passed is sent to reconciliation instead of being retried.
// Any returned error other than ErrBusy/ErrFrozen/ErrNeedsReconcile is
// retryable by calling Dispatch again with the same operation.
func (c *Coordinator) Dispatch(ctx context.Context, store Store, op Operation, effect Effect) (Outcome, error) {
	if err := op.validate(); err != nil {
		return Outcome{}, err
	}
	op.ReplayUntil = op.ReplayUntil.UTC().Truncate(time.Microsecond)
	key, err := c.EnvelopeKey(op)
	if err != nil {
		return Outcome{}, err
	}
	if err := c.checkFrozen(ctx, store); err != nil {
		return Outcome{}, err
	}
	res, err := store.Reserve(ctx, op, key)
	if err != nil {
		return Outcome{}, fmt.Errorf("recovery: reserve: %w", err)
	}
	if res.Result == "busy" {
		return Outcome{}, ErrBusy
	}
	env := envelope{
		V: 1, Environment: c.Environment, OperationID: op.ID, Kind: op.Kind,
		AggregateID: op.AggregateID, PriorVersion: op.PriorVersion, PayloadHash: op.PayloadHash(),
	}
	state, outcome := res.State, res.Outcome

	if state == StateReserved {
		prepared := env
		prepared.Payload = op.Payload
		if op.Kind == KindDispatch {
			prepared.ReplayUntil = &op.ReplayUntil
		}
		if err := c.ensure(ctx, key, phasePrepared, prepared, res.Result == "reserved"); err != nil {
			return Outcome{}, err
		}
		if state, err = store.MarkPrepared(ctx, op); err != nil {
			return Outcome{}, fmt.Errorf("recovery: mark prepared: %w", err)
		}
	}

	justCommitted := false
	if state == StatePrepared {
		if op.Kind == KindDispatch && !c.now().Before(op.ReplayUntil) {
			if _, err := store.MarkReconcile(ctx, op, "provider replay window ended before the outcome was recorded"); err != nil {
				return Outcome{}, fmt.Errorf("recovery: mark reconcile: %w", err)
			}
			return Outcome{}, ErrNeedsReconcile
		}
		if err := c.checkFrozen(ctx, store); err != nil {
			return Outcome{}, err
		}
		got, err := effect(ctx)
		if err != nil {
			return Outcome{}, fmt.Errorf("recovery: effect outcome unknown: %w", err)
		}
		if state, err = store.MarkCommitted(ctx, op, got); err != nil {
			return Outcome{}, fmt.Errorf("recovery: mark committed: %w", err)
		}
		outcome, justCommitted = &got, true
	}

	if state == StateCommitted {
		if outcome == nil {
			return Outcome{}, fmt.Errorf("recovery: committed operation %s has no recorded outcome", op.ID)
		}
		final := env
		final.Outcome = outcome
		if err := c.ensure(ctx, key, phaseOutcome, final, justCommitted); err != nil {
			return Outcome{}, err
		}
		if state, err = store.MarkSecured(ctx, op); err != nil {
			return Outcome{}, fmt.Errorf("recovery: mark secured: %w", err)
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
// primary database loss, restore tooling resumes each with Dispatch and the
// same Effect (provider-idempotent until ReplayUntil); after that deadline
// Dispatch sends it to reconciliation instead of repeating it.
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
		if env.ReplayUntil != nil {
			op.ReplayUntil = *env.ReplayUntil
		}
		if op.PayloadHash() != env.PayloadHash || env.Phase != phasePrepared {
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
// attachment reference; identical bytes are stored once.
func (c *Coordinator) CopyBlob(ctx context.Context, data []byte) (string, error) {
	root, err := c.root()
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > MaxBlobBytes {
		return "", fmt.Errorf("recovery: blob must be 1 to %d bytes", MaxBlobBytes)
	}
	sum := hashHex(data)
	ref := "sha256:" + sum
	name := root + "blob/" + sum
	if existing, err := c.Journal.Get(ctx, name); err == nil {
		if plain, err := c.Sealer.Open(name, existing); err == nil && hashHex(plain) == sum {
			return ref, nil
		}
		// An unreadable copy is replaced by a new version (versions are kept).
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
	return ref, nil
}

var blobRef = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)

// ReadBlob returns the bytes for a reference made by CopyBlob, verified
// against the reference hash.
func (c *Coordinator) ReadBlob(ctx context.Context, ref string) ([]byte, error) {
	root, err := c.root()
	if err != nil {
		return nil, err
	}
	m := blobRef.FindStringSubmatch(ref)
	if m == nil {
		return nil, fmt.Errorf("recovery: %q is not a blob reference", ref)
	}
	name := root + "blob/" + m[1]
	data, err := c.Journal.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	plain, err := c.Sealer.Open(name, data)
	if err != nil {
		return nil, err
	}
	if hashHex(plain) != m[1] {
		return nil, fmt.Errorf("recovery: blob %s does not match its reference", ref)
	}
	return plain, nil
}
