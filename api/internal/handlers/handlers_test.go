package handlers

import (
	"context"
	"slices"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
)

func TestNewListsTheWorkerKindsInOrder(t *testing.T) {
	hs := New(Config{})
	kinds := make([]string, len(hs))
	for i, h := range hs {
		if h.Handle == nil {
			t.Fatalf("%s has no handler", h.Kind)
		}
		kinds[i] = h.Kind
	}
	if !slices.Equal(kinds, Kinds) {
		t.Fatalf("kinds %v, want %v", kinds, Kinds)
	}
}

func TestChainOffersEachJobInOrder(t *testing.T) {
	var offered []string
	hs := []Named{}
	for _, k := range []string{"a", "b", "c"} {
		hs = append(hs, Named{k, func(context.Context, jobs.Job) error {
			offered = append(offered, k)
			if k == "b" {
				return nil
			}
			return jobs.ErrNoHandler
		}})
	}
	if err := Chain(hs)(context.Background(), jobs.Job{}); err != nil || !slices.Equal(offered, []string{"a", "b"}) {
		t.Fatalf("offered %v, %v", offered, err)
	}
}
