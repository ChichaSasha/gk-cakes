package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func commandOp() Operation {
	return Operation{ID: "abcdef00-0000-4000-8000-0000000000c1", Kind: KindCommand,
		AggregateID: "00000000-0000-4000-8000-0000000000a1", PriorVersion: 1,
		Payload: json.RawMessage(`{"kind":"order.free_confirm","order_version":1}`)}
}

// applyStore is memStore whose apply records the commit the way a domain
// gateway does: idempotently, in the same step as the committed phase.
type applyStore struct {
	memStore
	frozen  bool
	applies int
}

func (a *applyStore) Frozen(context.Context) (bool, error) { return a.frozen, nil }

func (a *applyStore) apply(o Outcome) Apply {
	return func(context.Context) (Outcome, State, error) {
		if a.state == StateCommitted || a.state == StateSecured {
			return *a.outcome, a.state, nil
		}
		if a.state != StatePrepared {
			return Outcome{}, "", errors.New("apply before prepared")
		}
		a.applies++
		a.state, a.outcome = StateCommitted, &o
		return o, a.state, nil
	}
}

func TestCommandRejectsInvalidOperations(t *testing.T) {
	c, _ := testCoordinator(t)
	cases := map[string]func(*Operation){
		"replay deadline":  func(o *Operation) { o.ReplayUntil = time.Now().Add(time.Hour) },
		"dispatch kind":    func(o *Operation) { o.Kind = KindDispatch; o.ReplayUntil = time.Now().Add(time.Hour) },
		"payload not JSON": func(o *Operation) { o.Payload = json.RawMessage(`{`) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			op := commandOp()
			mutate(&op)
			if _, err := c.Command(context.Background(), nopStore{t}, op, nil); err == nil {
				t.Fatal("invalid command accepted")
			}
		})
	}
}

func TestCommandJournalsThePayloadBeforeTheCommitAndTheOutcomeBeforeSecuring(t *testing.T) {
	ctx := context.Background()
	c, j := testCoordinator(t)
	op := commandOp()
	key, _ := c.EnvelopeKey(op)
	store := &applyStore{}
	want := Outcome{Status: "confirmed", Evidence: json.RawMessage(`{"order_version":2}`)}

	// The journal is down: nothing is committed.
	j.Fail = func(string, string) error { return errors.New("b2 down") }
	if _, err := c.Command(ctx, store, op, store.apply(want)); err == nil || store.applies != 0 || store.state != StateReserved {
		t.Fatalf("journal outage: err %v, applies %d, state %s; want no commit", err, store.applies, store.state)
	}

	// The prepared envelope is written, the commit happens, then the outcome
	// envelope fails: committed but not secured.
	j.Fail = func(op, name string) error {
		if op == "put" && strings.HasSuffix(name, ".outcome") {
			return errors.New("b2 down")
		}
		return nil
	}
	if _, err := c.Command(ctx, store, op, store.apply(want)); err == nil || store.applies != 1 || store.state != StateCommitted {
		t.Fatalf("outcome outage: err %v, applies %d, state %s; want committed, not secured", err, store.applies, store.state)
	}
	prepared, err := j.Get(ctx, key+".prepared")
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := c.Sealer.Open(key+".prepared", prepared)
	var env envelope
	if err := json.Unmarshal(plain, &env); err != nil || env.Kind != KindCommand || env.ReplayUntil != nil ||
		string(env.Payload) != string(op.Payload) {
		t.Fatalf("prepared envelope %s: want the exact command payload without a replay deadline", plain)
	}

	// Resuming secures it without a second commit.
	j.Fail = nil
	got, err := c.Command(ctx, store, op, store.apply(want))
	if err != nil || got.Status != "confirmed" || store.applies != 1 || store.state != StateSecured {
		t.Fatalf("resume: %+v, %v, applies %d, state %s", got, err, store.applies, store.state)
	}
	// A repeat returns the recorded outcome and writes nothing new.
	if _, err := c.Command(ctx, store, op, store.apply(want)); err != nil || j.Puts(key+".prepared") != 1 || j.Puts(key+".outcome") != 1 {
		t.Fatalf("repeat: %v, prepared puts %d, outcome puts %d", err, j.Puts(key+".prepared"), j.Puts(key+".outcome"))
	}
	if pending, err := c.Pending(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("Pending = %+v, %v; want none after securing", pending, err)
	}
}

func TestCommandWaitsWhileFrozenAndAPendingCommandIsListedForRestore(t *testing.T) {
	ctx := context.Background()
	c, _ := testCoordinator(t)
	op := commandOp()
	store := &applyStore{frozen: true}
	if _, err := c.Command(ctx, store, op, store.apply(Outcome{Status: "confirmed"})); !errors.Is(err, ErrFrozen) || store.state != "" {
		t.Fatalf("frozen: %v, state %q; want ErrFrozen before reserving", err, store.state)
	}
	// Prepared, then a crash before the commit: restore sees the command.
	store.frozen = false
	if _, err := c.Command(ctx, store, op, func(context.Context) (Outcome, State, error) {
		return Outcome{}, "", errors.New("crash")
	}); err == nil || store.state != StatePrepared {
		t.Fatalf("crash: %v, state %s", err, store.state)
	}
	pending, err := c.Pending(ctx)
	if err != nil || len(pending) != 1 || pending[0].Kind != KindCommand || !pending[0].ReplayUntil.IsZero() ||
		string(pending[0].Payload) != string(op.Payload) {
		t.Fatalf("Pending = %+v, %v; want the prepared command", pending, err)
	}
	// A prepared envelope written for a different payload is refused.
	other := op
	other.Payload = json.RawMessage(`{"kind":"order.free_confirm","order_version":9}`)
	store2 := &applyStore{}
	if _, err := c.Command(ctx, store2, other, store2.apply(Outcome{Status: "confirmed"})); err == nil || store2.applies != 0 {
		t.Fatalf("different payload under the same identity: %v, applies %d", err, store2.applies)
	}
}
