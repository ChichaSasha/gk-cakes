// Command gk-stripe-proof proves the Checkout Session effect against the
// Stripe sandbox (GK-025 A3b): with a test-mode STRIPE_SECRET_KEY it builds
// a collection's exact request, creates the session twice with one
// idempotency key through the same client the worker uses (one session),
// retrieves it with its payment intent and charge, verifies it as the
// reconcile does (pending: unpaid), expires it under the worker's expire key
// (and shows a replay of that key and a second expiry of the now expired
// session) and verifies again (cancelled). It never accepts a live key and
// prints IDs and statuses only.
//
//	STRIPE_SECRET_KEY=sk_test_... [STRIPE_ACCOUNT_ID=acct_...] gk-stripe-proof
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Getenv, os.Stdout, ""))
}

// run returns the exit code: 0 proved, 1 failed, 2 misconfigured. baseURL
// overrides the Stripe API (tests only).
func run(ctx context.Context, getenv func(string) string, out io.Writer, baseURL string) int {
	client, err := payments.NewClient(payments.ClientConfig{Key: getenv("STRIPE_SECRET_KEY"), BaseURL: baseURL})
	if err != nil {
		fmt.Fprintln(out, "configuration:", err)
		return 2
	}
	account := getenv("STRIPE_ACCOUNT_ID")
	if err := prove(ctx, client, account, out); err != nil {
		fmt.Fprintln(out, "FAIL:", err)
		return 1
	}
	fmt.Fprintln(out, "PASS")
	return 0
}

func prove(ctx context.Context, client *payments.Client, account string, out io.Writer) error {
	if account != "" {
		got, err := client.AccountID(ctx)
		if err != nil {
			return err
		}
		if got != account {
			return errors.New("the key belongs to another account than STRIPE_ACCOUNT_ID")
		}
		fmt.Fprintln(out, "account:", got)
	}
	now := time.Now()
	req := payments.SessionRequest{CollectionID: payments.NewID(), OrderID: payments.NewID(), AmountMinor: 8585,
		Environment: "sandbox-proof", UIOrigin: "https://gkcake.com", ExpiresAt: payments.SessionExpiry(now)}
	form, err := req.Form()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "collection (idempotency key):", req.CollectionID)
	var first string
	for i := 1; i <= 2; i++ {
		res := client.CreateSession(ctx, req.CollectionID, form)
		if res.Kind != payments.Created {
			return fmt.Errorf("create %d: %s", i, res)
		}
		fmt.Fprintf(out, "create %d: %s livemode=%v status=%s\n", i, res.Session.ID, res.Session.Livemode, res.Session.Status)
		if res.Session.Livemode {
			return errors.New("the session is live-mode")
		}
		if i == 1 {
			first = res.Session.ID
		} else if res.Session.ID != first {
			return fmt.Errorf("the same idempotency key created a second session %s", res.Session.ID)
		}
	}
	exp := payments.Expectation{CollectionID: req.CollectionID, OrderID: req.OrderID, Environment: req.Environment,
		SessionID: first, AmountMinor: req.AmountMinor}
	res := client.RetrieveSession(ctx, first)
	if res.Kind != payments.Found {
		return fmt.Errorf("retrieve: %s", res)
	}
	if v := payments.Evaluate(res.Session, exp); v.Kind != payments.VerdictPending {
		return fmt.Errorf("open session verdict %s (%s), want pending", v.Kind, v.Reason)
	}
	fmt.Fprintln(out, "retrieve: open, verdict pending")
	// The worker's 15-minute expire request (GK-025 A3c1): the collection's
	// expire key expires the open session; the same key again replays that
	// answer; a new request for the expired session is refused as not open.
	key := payments.ExpireKey(req.CollectionID)
	if res := client.ExpireSession(ctx, key, first); res.Kind != payments.Expired || res.Session.Status != "expired" {
		return fmt.Errorf("expire: %s", res)
	}
	fmt.Fprintln(out, "expire (open session): expired")
	if res := client.ExpireSession(ctx, key, first); res.Kind != payments.Expired {
		return fmt.Errorf("expire replay under the same key: %s, want the first answer", res)
	}
	fmt.Fprintln(out, "expire replay under the same key: expired (the first answer)")
	again := client.ExpireSession(ctx, key+"-again", first)
	if again.Kind != payments.NotOpen {
		return fmt.Errorf("expire of an expired session: %s, want not_open", again)
	}
	fmt.Fprintf(out, "expire (already expired session): not_open (HTTP %d %s)\n", again.Status, again.Code)
	res = client.RetrieveSession(ctx, first)
	if res.Kind != payments.Found {
		return fmt.Errorf("retrieve after expiry: %s", res)
	}
	if v := payments.Evaluate(res.Session, exp); v.Kind != payments.VerdictCancelled {
		return fmt.Errorf("expired session verdict %s (%s), want cancelled", v.Kind, v.Reason)
	}
	fmt.Fprintln(out, "expired: verdict cancelled")
	// A different request under the used key is never a second session.
	other := req
	other.AmountMinor++
	otherForm, _ := other.Form()
	if res := client.CreateSession(ctx, req.CollectionID, otherForm); res.Kind != payments.Conflict {
		return fmt.Errorf("a changed request under the used key: %s, want an idempotency conflict", res)
	}
	fmt.Fprintln(out, "changed request under the used key: idempotency conflict")
	return nil
}
