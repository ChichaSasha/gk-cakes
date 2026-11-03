package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testCoordinator(t *testing.T) (*Coordinator, *MemoryJournal) {
	t.Helper()
	s, err := NewSealer("k1:" + testKey(3))
	if err != nil {
		t.Fatal(err)
	}
	j := NewMemoryJournal()
	return &Coordinator{Journal: j, Sealer: s, Environment: "test"}, j
}

// nopStore fails the test if Dispatch reaches the store.
type nopStore struct{ t *testing.T }

func (n nopStore) fail() error { n.t.Fatal("store reached for an invalid operation"); return nil }
func (n nopStore) Reserve(context.Context, Operation, string) (Reservation, error) {
	return Reservation{}, n.fail()
}
func (n nopStore) MarkPrepared(context.Context, Operation) (State, error) { return "", n.fail() }
func (n nopStore) MarkCommitted(context.Context, Operation, Outcome, bool) (State, bool, error) {
	return "", false, n.fail()
}
func (n nopStore) MarkSecured(context.Context, Operation) (State, error) { return "", n.fail() }
func (n nopStore) MarkReconcile(context.Context, Operation, string) (State, error) {
	return "", n.fail()
}
func (n nopStore) Frozen(context.Context) (bool, error) { return false, n.fail() }

func TestDispatchRejectsInvalidOperationsBeforeTouchingState(t *testing.T) {
	c, _ := testCoordinator(t)
	valid := Operation{ID: "abcdef00-0000-4000-8000-000000000001", Kind: KindDispatch,
		AggregateID: "00000000-0000-4000-8000-000000000002", Payload: json.RawMessage(`{}`), ReplayUntil: time.Now().Add(time.Hour)}
	cases := map[string]func(*Operation){
		"upper-case ID":           func(o *Operation) { o.ID = strings.ToUpper(o.ID) },
		"bad aggregate":           func(o *Operation) { o.AggregateID = "order-1" },
		"unknown kind":            func(o *Operation) { o.Kind = "refund" },
		"negative version":        func(o *Operation) { o.PriorVersion = -1 },
		"payload not JSON":        func(o *Operation) { o.Payload = json.RawMessage(`{`) },
		"dispatch without replay": func(o *Operation) { o.ReplayUntil = time.Time{} },
		"command (not supported)": func(o *Operation) { o.Kind = "command" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			op := valid
			mutate(&op)
			if _, err := c.Dispatch(context.Background(), nopStore{t}, op, nil); err == nil {
				t.Fatal("invalid operation accepted")
			}
		})
	}
	bad := *c
	bad.Environment = "Staging/../x"
	if _, err := bad.Dispatch(context.Background(), nopStore{t}, valid, nil); err == nil {
		t.Fatal("unsafe environment segment accepted")
	}
}

func TestEnvelopeKeyOrdersByAggregateVersionAndCarriesOnlyIDs(t *testing.T) {
	c, _ := testCoordinator(t)
	op := Operation{ID: "00000000-0000-4000-8000-000000000001", AggregateID: "00000000-0000-4000-8000-0000000000aa", PriorVersion: 7}
	key, err := c.EnvelopeKey(op)
	if err != nil {
		t.Fatal(err)
	}
	if key != "recovery/test/op/00000000-0000-4000-8000-0000000000aa/0000000007-00000000-0000-4000-8000-000000000001" {
		t.Fatalf("EnvelopeKey = %s", key)
	}
	op.PriorVersion = 10
	later, _ := c.EnvelopeKey(op)
	if later <= key {
		t.Fatalf("version 10 key %s does not sort after version 7 key %s", later, key)
	}
}

func TestCopyBlobStoresSealedVerifiedBytesOnceUnderKeyedNames(t *testing.T) {
	ctx := context.Background()
	c, j := testCoordinator(t)
	data := []byte("essential attachment bytes")
	ref, err := c.CopyBlob(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	sum := strings.TrimPrefix(ref, "sha256:")
	if len(sum) != 64 {
		t.Fatalf("ref = %s", ref)
	}
	names := j.Names()
	if len(names) != 1 || strings.Contains(names[0], sum) || !strings.HasPrefix(names[0], "recovery/test/blob/k1-") {
		t.Fatalf("blob names %v: want one keyed name that does not reveal the content hash", names)
	}
	name := names[0]
	if again, err := c.CopyBlob(ctx, data); err != nil || again != ref || j.Puts(name) != 1 {
		t.Fatalf("second copy = %s, %v, puts %d; want the same ref and no second write", again, err, j.Puts(name))
	}
	stored, _ := j.Get(ctx, name)
	if bytes.Contains(stored, data) {
		t.Fatal("blob stored unencrypted")
	}
	got, err := c.ReadBlob(ctx, ref)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("ReadBlob = %q, %v", got, err)
	}

	// Different bytes stored under the name (tampering) are rejected on read
	// and superseded on the next copy.
	other, _ := c.Sealer.Seal(name, []byte("other bytes"))
	_ = j.Put(ctx, name, other)
	if _, err := c.ReadBlob(ctx, ref); err == nil {
		t.Fatal("ReadBlob accepted bytes that do not match the reference")
	}
	if again, err := c.CopyBlob(ctx, data); err != nil || again != ref || j.Puts(name) != 3 {
		t.Fatalf("copy over a mismatching blob = %s, %v, puts %d; want a new version", again, err, j.Puts(name))
	}
	if got, err := c.ReadBlob(ctx, ref); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("ReadBlob after the new copy = %q, %v", got, err)
	}

	// After a key rotation, copies made under the old key are still found.
	rotated, err := NewSealer("k2:" + testKey(4) + ",k1:" + testKey(3))
	if err != nil {
		t.Fatal(err)
	}
	c2 := &Coordinator{Journal: j, Sealer: rotated, Environment: "test"}
	if got, err := c2.ReadBlob(ctx, ref); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("ReadBlob after rotation = %q, %v", got, err)
	}

	if _, err := c.ReadBlob(ctx, "sha256:abc"); err == nil {
		t.Fatal("malformed reference accepted")
	}
	if _, err := c.ReadBlob(ctx, "sha256:"+strings.Repeat("0", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing blob: err = %v, want ErrNotFound", err)
	}
	if _, err := c.CopyBlob(ctx, nil); err == nil {
		t.Fatal("empty blob accepted")
	}
	j.Fail = func(op, _ string) error {
		if op == "put" {
			return errors.New("b2 unavailable")
		}
		return nil
	}
	if _, err := c.CopyBlob(ctx, []byte("new essential bytes")); err == nil {
		t.Fatal("CopyBlob reported success without the journal")
	}
}

func pendingOp() Operation {
	return Operation{ID: "00000000-0000-4000-8000-000000000001", Kind: KindDispatch,
		AggregateID: "00000000-0000-4000-8000-0000000000aa", Payload: json.RawMessage(`{"a":1}`),
		ReplayUntil: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)}
}

func putPrepared(t *testing.T, c *Coordinator, name string, env envelope) {
	t.Helper()
	env.Phase, env.V, env.Environment = phasePrepared, 1, "test"
	plain, _ := json.Marshal(env)
	sealed, err := c.Sealer.Seal(name, plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Journal.Put(context.Background(), name, sealed); err != nil {
		t.Fatal(err)
	}
}

func TestPendingRejectsTamperedOrMisplacedEnvelopes(t *testing.T) {
	ctx := context.Background()
	op := pendingOp()
	good := envelope{OperationID: op.ID, Kind: op.Kind, AggregateID: op.AggregateID, PayloadHash: op.PayloadHash(),
		Payload: op.Payload, ReplayUntil: &op.ReplayUntil}

	c, _ := testCoordinator(t)
	key, _ := c.EnvelopeKey(op)
	putPrepared(t, c, key+".prepared", good)
	pending, err := c.Pending(ctx)
	if err != nil || len(pending) != 1 || pending[0].ID != op.ID || !pending[0].ReplayUntil.Equal(op.ReplayUntil) {
		t.Fatalf("Pending = %+v, %v", pending, err)
	}

	cases := map[string]func(c *Coordinator){
		"payload does not match its hash": func(c *Coordinator) {
			bad := good
			bad.Payload = json.RawMessage(`{"a":2}`)
			putPrepared(t, c, key+".prepared", bad)
		},
		"validly sealed under another operation's name": func(c *Coordinator) {
			other := op
			other.ID = "00000000-0000-4000-8000-000000000009"
			otherKey, _ := c.EnvelopeKey(other)
			putPrepared(t, c, otherKey+".prepared", good) // fields say op, name says other
		},
		"copied ciphertext under another name": func(c *Coordinator) {
			putPrepared(t, c, key+".prepared", good)
			data, _ := c.Journal.Get(ctx, key+".prepared")
			moved := strings.Replace(key, op.ID, "00000000-0000-4000-8000-000000000009", 1) + ".prepared"
			_ = c.Journal.Put(ctx, moved, data)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := testCoordinator(t)
			setup(c)
			if _, err := c.Pending(ctx); err == nil {
				t.Fatal("Pending accepted the envelope")
			}
		})
	}
}
