package backup

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FrozenReport is the result of the synthetic dispatch check on a restored
// database: counts only.
type FrozenReport struct {
	// Old leases: every job lease held at backup time, presented by its old
	// worker to each lease function (heartbeat, complete, release, fail,
	// attention); all must be refused.
	StaleLeaseCalls   int `json:"stale_lease_calls"`
	StaleLeaseRefused int `json:"stale_lease_refused"`
	// Jobs due for a claim, and jobs scheduled later (not claimable now).
	Claimable int `json:"claimable"`
	NotDue    int `json:"not_due"`
	// Jobs a fresh worker claimed and what the real handlers answered.
	Claimed          int            `json:"claimed"`
	DeferredFrozen   int            `json:"deferred_frozen"`
	DeferredByKind   map[string]int `json:"deferred_frozen_by_kind"`
	HeldForAttention int            `json:"held_for_attention"`
	Unowned          int            `json:"unowned"` // no handler owns the job: nothing to dispatch
	Violations       map[string]int `json:"violations,omitempty"`
	// Provider calls (email, Stripe sessions and refunds) and journal
	// envelopes written: all must be zero.
	ProviderCalls int `json:"provider_calls"`
	JournalWrites int `json:"journal_writes"`
	// Every business table except job (recovery operations included) has
	// the same content before and after.
	RecoveryOperationsUnchanged bool `json:"recovery_operations_unchanged"`
	TablesUnchanged             bool `json:"tables_unchanged"`
}

// errSyntheticJournal refuses every journal write during the check.
var errSyntheticJournal = errors.New("backup: synthetic dispatch check: journal writes are refused")

// checkFrozen proves the restored jobs stay frozen (K11): as a fresh,
// temporary gk_worker login it presents the backup's old leases to the
// lease functions, then runs the worker's real job handlers (order
// commands, payments, refunds, hold reviews, email) over every claimable
// job with recording providers and a journal that refuses writes. Claimed
// jobs are released for a later attempt; nothing else may change.
func checkFrozen(ctx context.Context, owner *pgx.Conn, target, env string, leases []Lease) (FrozenReport, error) {
	rep := FrozenReport{Violations: map[string]int{}, DeferredByKind: map[string]int{}}
	login, password := "gk_restore_check_"+randomHex(4), randomHex(24)
	verifier, err := scramVerifier(password)
	if err != nil {
		return rep, err
	}
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	// The statement carries a SCRAM verifier, never the password itself.
	if _, err := owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{login}.Sanitize()+" LOGIN PASSWORD '"+verifier+
		"' VALID UNTIL '"+until+"' IN ROLE gk_worker"); err != nil {
		return rep, fmt.Errorf("backup: create the synthetic worker login: %w", err)
	}
	defer func() {
		_, _ = owner.Exec(context.WithoutCancel(ctx), "DROP ROLE IF EXISTS "+pgx.Identifier{login}.Sanitize())
	}()
	workerURL, err := loginURL(target, login, password)
	if err != nil {
		return rep, err
	}
	pcfg, err := pgxpool.ParseConfig(workerURL)
	if err != nil {
		return rep, errors.New("backup: invalid synthetic worker URL")
	}
	pcfg.MaxConns = 4
	pcfg.ConnConfig.RuntimeParams["application_name"] = "gk-backup-restore-check"
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return rep, err
	}
	defer pool.Close()
	if err := database.CheckLogin(ctx, pool, "gk_worker"); err != nil {
		return rep, fmt.Errorf("backup: the synthetic worker login: %w", err)
	}

	before, err := tableDigests(ctx, owner)
	if err != nil {
		return rep, err
	}
	// The migration credential reads jobs as gk_owner.
	if err := ownerTx(ctx, owner, true, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE (state IN ('queued', 'retry') AND next_attempt_at <= now()) OR (state = 'running' AND lease_until < now())),
			count(*) FILTER (WHERE state IN ('queued', 'retry') AND next_attempt_at > now())
			FROM business.job`).Scan(&rep.Claimable, &rep.NotDue)
	}); err != nil {
		return rep, fmt.Errorf("backup: count claimable jobs: %w", err)
	}

	staleLeases(ctx, pool, leases, &rep)

	rec := &recorder{}
	sealer, err := recovery.NewSealer("k1:" + base64.StdEncoding.EncodeToString(randomBytes(32)))
	if err != nil {
		return rep, err
	}
	coord := &recovery.Coordinator{Journal: rec, Sealer: sealer, Environment: env}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	email, err := notifications.NewDispatcher(notifications.DispatcherConfig{DB: pool, Coordinator: coord, Sender: rec, Environment: env, Logger: logger})
	if err != nil {
		return rep, err
	}
	const account = "acct_restorecheck" // never reached: no provider is called
	kinds := []struct {
		name string
		h    jobs.Handler
	}{
		{"order_command", (&checkout.Commands{DB: pool, Coordinator: coord, Logger: logger}).Handle},
		{"payment", (&payments.Worker{DB: pool, Coordinator: coord, Provider: rec, Environment: env, Account: account, Logger: logger}).Handle},
		{"refund", (&payments.Refunds{DB: pool, Coordinator: coord, Provider: rec, Environment: env, Account: account, Logger: logger}).Handle},
		{"hold_review", (&checkout.HoldReviews{DB: pool, Logger: logger}).Handle},
		{"notification", email.Handle},
	}
	var (
		mu       sync.Mutex
		answered string
	)
	chain := make([]jobs.Handler, len(kinds))
	for i, k := range kinds {
		chain[i] = func(ctx context.Context, job jobs.Job) error {
			err := k.h(ctx, job)
			if !errors.Is(err, jobs.ErrNoHandler) {
				answered = k.name
			}
			return err
		}
	}
	all := jobs.Chain(chain...) // the worker's order (cmd/gk-worker)
	handler := func(ctx context.Context, job jobs.Job) error {
		mu.Lock()
		defer mu.Unlock()
		answered = ""
		err := all(ctx, job)
		rep.Claimed++
		var (
			deferral  *jobs.DeferError
			attention *jobs.AttentionError
		)
		switch {
		case errors.As(err, &deferral) && errors.Is(err, recovery.ErrFrozen):
			rep.DeferredFrozen++
			rep.DeferredByKind[answered]++
			return err
		case errors.Is(err, jobs.ErrNoHandler):
			rep.Unowned++
		case errors.As(err, &attention) && answered == "notification" && suspended(ctx, owner, job.OperationID):
			// A manager hold (a suspended notice) is checked before the
			// freeze; nothing is dispatched. Any other attention means a
			// handler went past the freeze.
			rep.HeldForAttention++
		case errors.As(err, &attention):
			rep.Violations[answered+":attention"]++
		case err == nil:
			rep.Violations[answered+":completed"]++
		case errors.As(err, &deferral):
			rep.Violations[answered+":deferred_not_frozen"]++
		default:
			rep.Violations[answered+":failed"]++
		}
		// Never fail, complete or escalate a restored job in this check: the
		// deferral must not wrap an attention error (the worker would
		// escalate it).
		return jobs.Defer(time.Hour, fmt.Errorf("restore check: %v", err))
	}
	w := jobs.New(pool, jobs.Options{Handler: handler, Logger: logger, ShutdownTimeout: 5 * time.Second, WorkerID: newUUID()})
	for i := 0; i <= rep.Claimable+rep.NotDue; i++ {
		ok, err := w.RunOnce(ctx)
		if err != nil {
			return rep, fmt.Errorf("backup: synthetic claim: %w", err)
		}
		if !ok {
			break
		}
	}

	after, err := tableDigests(ctx, owner)
	if err != nil {
		return rep, err
	}
	rep.RecoveryOperationsUnchanged = before["recovery_operation"] == after["recovery_operation"]
	rep.TablesUnchanged = maps.Equal(before, after)
	rep.ProviderCalls, rep.JournalWrites = rec.counts()
	if len(rep.Violations) == 0 {
		rep.Violations = nil
	}
	switch {
	case rep.StaleLeaseRefused != rep.StaleLeaseCalls:
		return rep, fmt.Errorf("backup: %d of %d old-lease calls were accepted", rep.StaleLeaseCalls-rep.StaleLeaseRefused, rep.StaleLeaseCalls)
	case len(rep.Violations) > 0:
		return rep, fmt.Errorf("backup: restored jobs were not frozen: %v", rep.Violations)
	case rep.Claimed < rep.Claimable:
		return rep, fmt.Errorf("backup: %d jobs were claimable, the synthetic worker claimed %d", rep.Claimable, rep.Claimed)
	case rep.ProviderCalls > 0 || rep.JournalWrites > 0:
		return rep, fmt.Errorf("backup: %d provider calls and %d journal writes while frozen", rep.ProviderCalls, rep.JournalWrites)
	case !rep.TablesUnchanged:
		var changed []string
		for _, t := range slices.Sorted(maps.Keys(before)) {
			if before[t] != after[t] {
				changed = append(changed, t)
			}
		}
		return rep, fmt.Errorf("backup: the synthetic dispatch changed business tables %v", changed)
	}
	return rep, nil
}

// suspended reports whether the notification intent is on a manager hold.
func suspended(ctx context.Context, owner *pgx.Conn, intent string) bool {
	var held bool
	err := ownerTx(ctx, owner, true, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT suspended_reason IS NOT NULL FROM business.notification_intent WHERE id = $1::uuid", intent).Scan(&held)
	})
	return err == nil && held
}

// staleLeases presents each old lease to every lease function.
func staleLeases(ctx context.Context, pool *pgxpool.Pool, leases []Lease, rep *FrozenReport) {
	for _, l := range leases {
		calls := []struct {
			sql     string
			refused func(pgx.Row) bool
		}{
			{"SELECT business.job_heartbeat($1::uuid, $2::uuid, $3, interval '1 minute')", func(r pgx.Row) bool {
				var until *time.Time
				return r.Scan(&until) == nil && until == nil
			}},
			{"SELECT business.job_complete($1::uuid, $2::uuid, $3)", refusedBool},
			{"SELECT business.job_release($1::uuid, $2::uuid, $3, interval '0')", refusedBool},
			{"SELECT business.job_attention($1::uuid, $2::uuid, $3, 'restore check: old lease')", refusedBool},
			{"SELECT job_state FROM business.job_fail($1::uuid, $2::uuid, $3, 'restore check: old lease')", func(r pgx.Row) bool {
				var state string
				return errors.Is(r.Scan(&state), pgx.ErrNoRows)
			}},
		}
		for _, c := range calls {
			rep.StaleLeaseCalls++
			if c.refused(pool.QueryRow(ctx, c.sql, l.Job, l.Worker, l.Generation)) {
				rep.StaleLeaseRefused++
			}
		}
	}
}

func refusedBool(r pgx.Row) bool {
	var ok bool
	return r.Scan(&ok) == nil && !ok
}

// tableDigests hashes the content of every business table except job.
func tableDigests(ctx context.Context, owner *pgx.Conn) (map[string]string, error) {
	out := map[string]string{}
	err := ownerTx(ctx, owner, true, func(tx pgx.Tx) error {
		counts, err := countRows(ctx, tx)
		if err != nil {
			return err
		}
		for t := range counts {
			if t == "job" {
				continue
			}
			var d string
			if err := tx.QueryRow(ctx, "SELECT coalesce(md5(string_agg(t::text, E'\\n' ORDER BY t::text)), '') FROM business."+
				pgx.Identifier{t}.Sanitize()+" t").Scan(&d); err != nil {
				return fmt.Errorf("backup: digest business.%s: %w", t, err)
			}
			out[t] = d
		}
		return nil
	})
	return out, err
}

// recorder stands in for every outgoing provider and the journal: it
// counts calls and performs none.
type recorder struct {
	mu       sync.Mutex
	provider int
	writes   int
}

func (r *recorder) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.provider, r.writes
}

func (r *recorder) call() {
	r.mu.Lock()
	r.provider++
	r.mu.Unlock()
}

var errSyntheticProvider = errors.New("backup: synthetic dispatch check: no provider is called")

func (r *recorder) Send(context.Context, string, []byte) notifications.Result {
	r.call()
	return notifications.Result{Err: errSyntheticProvider}
}

func (r *recorder) CreateSession(context.Context, string, string) payments.Result {
	r.call()
	return payments.Result{Err: errSyntheticProvider}
}

func (r *recorder) RetrieveSession(context.Context, string) payments.Result {
	r.call()
	return payments.Result{Err: errSyntheticProvider}
}

func (r *recorder) ExpireSession(context.Context, string, string) payments.Result {
	r.call()
	return payments.Result{Err: errSyntheticProvider}
}

func (r *recorder) CreateRefund(context.Context, string, string) payments.RefundResult {
	r.call()
	return payments.RefundResult{Err: errSyntheticProvider}
}

func (r *recorder) RetrieveRefund(context.Context, string) payments.RefundResult {
	r.call()
	return payments.RefundResult{Err: errSyntheticProvider}
}

func (r *recorder) ListRefunds(context.Context, string) payments.RefundResult {
	r.call()
	return payments.RefundResult{Err: errSyntheticProvider}
}

func (r *recorder) Put(context.Context, string, []byte) error {
	r.mu.Lock()
	r.writes++
	r.mu.Unlock()
	return errSyntheticJournal
}

func (r *recorder) Get(context.Context, string) ([]byte, error) { return nil, recovery.ErrNotFound }

func (r *recorder) List(context.Context, string) ([]string, error) { return nil, nil }

func (r *recorder) Ping(context.Context) error { return nil }

var (
	_ notifications.Sender    = (*recorder)(nil)
	_ payments.Provider       = (*recorder)(nil)
	_ payments.RefundProvider = (*recorder)(nil)
	_ recovery.Journal        = (*recorder)(nil)
)

// loginURL is target with another user and password. A Supavisor pooler
// user ("postgres.<project ref>") keeps its tenant suffix.
func loginURL(target, user, password string) (string, error) {
	u, err := url.Parse(target)
	if err != nil || u.User == nil {
		return "", errors.New("backup: invalid restore target URL")
	}
	if _, suffix, ok := strings.Cut(u.User.Username(), "."); ok {
		user += "." + suffix
	}
	u.User = url.UserPassword(user, password)
	return u.String(), nil
}

// scramVerifier returns PostgreSQL's SCRAM-SHA-256 verifier for password
// (RFC 5802/7677; the format pg_authid stores), so CREATE ROLE never carries
// the password itself into server logs.
func scramVerifier(password string) (string, error) {
	const iterations = 4096
	salt := randomBytes(16)
	salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
	if err != nil {
		return "", err
	}
	mac := func(key []byte, msg string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(msg))
		return h.Sum(nil)
	}
	stored := sha256.Sum256(mac(salted, "Client Key"))
	server := mac(salted, "Server Key")
	b64 := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations, b64(salt), b64(stored[:]), b64(server)), nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func randomHex(n int) string { return hex.EncodeToString(randomBytes(n)) }

func newUUID() string {
	b := randomBytes(16)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
