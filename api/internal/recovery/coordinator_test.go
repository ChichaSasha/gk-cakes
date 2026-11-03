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
func (n nopStore) MarkCommitted(context.Context, Operation, Outcome) (State, error) {
	return "", n.fail()
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
		"upper-case ID":            func(o *Operation) { o.ID = strings.ToUpper(o.ID) },
		"bad aggregate":            func(o *Operation) { o.AggregateID = "order-1" },
		"unknown kind":             func(o *Operation) { o.Kind = "refund" },
		"negative version":         func(o *Operation) { o.PriorVersion = -1 },
		"payload not JSON":         func(o *Operation) { o.Payload = json.RawMessage(`{`) },
		"dispatch without replay":  func(o *Operation) { o.ReplayUntil = time.Time{} },
		"command with replay time": func(o *Operation) { o.Kind = KindCommand },
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

func TestCopyBlobStoresSealedVerifiedBytesOnce(t *testing.T) {
	ctx := context.Background()
	c, j := testCoordinator(t)
	data := []byte("essential attachment bytes")
	ref, err := c.CopyBlob(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "sha256:") {
		t.Fatalf("ref = %s", ref)
	}
	if again, err := c.CopyBlob(ctx, data); err != nil || again != ref {
		t.Fatalf("second copy = %s, %v", again, err)
	}
	name := "recovery/test/blob/" + strings.TrimPrefix(ref, "sha256:")
	if j.Puts(name) != 1 {
		t.Fatalf("identical blob written %d times, want once", j.Puts(name))
	}
	stored, _ := j.Get(ctx, name)
	if bytes.Contains(stored, data) {
		t.Fatal("blob stored unencrypted")
	}
	got, err := c.ReadBlob(ctx, ref)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("ReadBlob = %q, %v", got, err)
	}
	// A different blob stored under this name (tampering) is rejected.
	other, _ := c.Sealer.Seal(name, []byte("other bytes"))
	_ = j.Put(ctx, name, other)
	if _, err := c.ReadBlob(ctx, ref); err == nil {
		t.Fatal("ReadBlob accepted bytes that do not match the reference")
	}
	if _, err := c.ReadBlob(ctx, "sha256:abc"); err == nil {
		t.Fatal("malformed reference accepted")
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

func TestPendingRejectsTamperedOrMisplacedEnvelopes(t *testing.T) {
	ctx := context.Background()
	c, j := testCoordinator(t)
	op := Operation{ID: "00000000-0000-4000-8000-000000000001", Kind: KindCommand,
		AggregateID: "00000000-0000-4000-8000-0000000000aa", Payload: json.RawMessage(`{"a":1}`)}
	key, _ := c.EnvelopeKey(op)
	env := envelope{V: 1, Environment: "test", OperationID: op.ID, Kind: op.Kind, AggregateID: op.AggregateID,
		PayloadHash: op.PayloadHash(), Payload: op.Payload}
	if err := c.ensure(ctx, key, phasePrepared, env, true); err != nil {
		t.Fatal(err)
	}
	pending, err := c.Pending(ctx)
	if err != nil || len(pending) != 1 || pending[0].ID != op.ID {
		t.Fatalf("Pending = %+v, %v", pending, err)
	}
	// The same envelope copied under another operation's name does not open.
	moved := strings.Replace(key, op.ID, "00000000-0000-4000-8000-000000000009", 1) + ".prepared"
	data, _ := j.Get(ctx, key+".prepared")
	_ = j.Put(ctx, moved, data)
	if _, err := c.Pending(ctx); err == nil {
		t.Fatal("Pending accepted an envelope stored under another name")
	}
}
