package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDeferIsBoundedToTheReleaseLimits(t *testing.T) {
	for in, want := range map[time.Duration]time.Duration{
		-time.Minute:    time.Second,
		0:               time.Second,
		5 * time.Minute: 5 * time.Minute,
		48 * time.Hour:  24 * time.Hour,
	} {
		var de *DeferError
		if err := Defer(in, errors.New("x")); !errors.As(err, &de) || de.After != want {
			t.Errorf("Defer(%s) = %v, want %s", in, err, want)
		}
	}
}

func TestAttentionKeepsTheReasonAndCause(t *testing.T) {
	cause := errors.New("provider said no")
	err := Attention("check the key", cause)
	var ae *AttentionError
	if !errors.As(err, &ae) || ae.Reason != "check the key" || !errors.Is(err, cause) || err.Error() != "check the key: provider said no" {
		t.Fatalf("Attention = %v", err)
	}
	if got := reason(string(make([]rune, 300))); len([]rune(got)) != maxReasonRunes {
		t.Fatalf("reason bound = %d runes, want %d", len([]rune(got)), maxReasonRunes)
	}
	if reason("") == "" {
		t.Fatal("empty reason stored")
	}
}

func TestChainOffersTheJobUntilAHandlerOwnsIt(t *testing.T) {
	var seen []string
	h := func(name string, err error) Handler {
		return func(context.Context, Job) error { seen = append(seen, name); return err }
	}
	owned := errors.New("owned")
	if err := Chain(h("a", ErrNoHandler), h("b", owned), h("c", nil))(context.Background(), Job{}); !errors.Is(err, owned) {
		t.Fatalf("Chain = %v, want b's answer", err)
	}
	if strings.Join(seen, ",") != "a,b" {
		t.Fatalf("handlers called %v, want a then b only", seen)
	}
	if err := Chain(h("x", ErrNoHandler))(context.Background(), Job{}); !errors.Is(err, ErrNoHandler) {
		t.Fatalf("unowned job: %v", err)
	}
}
