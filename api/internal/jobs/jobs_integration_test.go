//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/dbtest"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	db    *dbtest.DB
	owner *pgx.Conn // migrator; may SET ROLE gk_owner but does not inherit it
	admin *pgx.Conn // superuser observer for assertions
	pool  *pgxpool.Pool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := dbtest.New(t)
	owner := db.MigratorConn(t)
	if _, err := migrate.Run(ctx, owner, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &fixture{db: db, owner: owner, admin: db.AdminConn(t), pool: workerPool(t, db)}
}

func workerPool(t *testing.T, db *dbtest.DB) *pgxpool.Pool {
	t.Helper()
	cfg, err := config.LoadWorker(func(k string) string {
		return map[string]string{"APP_ENV": "test", "DATABASE_URL": db.LoginURL(t, "gk_worker")}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.OpenPool(context.Background(), cfg.DatabaseURL, "gk-worker-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// enqueue seeds a job as the owner, as a future domain command would.
func (f *fixture) enqueue(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	tx, err := f.owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := tx.QueryRow(ctx, "SELECT business.job_enqueue(gen_random_uuid(), NULL)::text").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

type jobRow struct {
	State       string
	LeaseOwner  *string
	Generation  int64
	NextAttempt time.Duration // next_attempt_at - now()
}

func (f *fixture) row(t *testing.T, id string) jobRow {
	t.Helper()
	var r jobRow
	err := f.admin.QueryRow(context.Background(), `
		SELECT state, lease_owner::text, generation, next_attempt_at - now()
		FROM business.job WHERE id = $1::uuid`, id).Scan(&r.State, &r.LeaseOwner, &r.Generation, &r.NextAttempt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func runWorker(ctx context.Context, w *jobs.Worker) <-chan error {
	out := make(chan error, 1)
	go func() { out <- w.Run(ctx) }()
	return out
}

func TestShutdownDrainsInFlightJobThenStops(t *testing.T) {
	f := newFixture(t)
	first := f.enqueue(t)
	second := f.enqueue(t)

	started := make(chan string, 2)
	release := make(chan struct{})
	w := jobs.New(f.pool, jobs.Options{
		Logger:          quietLogger(),
		ShutdownTimeout: 10 * time.Second,
		PollInterval:    50 * time.Millisecond,
		Handler: func(ctx context.Context, job jobs.Job) error {
			started <- job.ID
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runWorker(ctx, w)

	inFlight := <-started
	cancel()
	select {
	case err := <-done:
		t.Fatalf("worker exited (%v) before its in-flight job finished", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop after draining")
	}

	other := second
	if inFlight == second {
		other = first
	}
	if got := f.row(t, inFlight); got.State != "completed" || got.LeaseOwner != nil {
		t.Errorf("drained job = %+v, want completed without lease", got)
	}
	if got := f.row(t, other); got.State != "queued" || got.Generation != 0 {
		t.Errorf("job after shutdown = %+v, want untouched queued (no claims after shutdown)", got)
	}
}

func TestDrainDeadlineReleasesLeaseForAnotherWorker(t *testing.T) {
	f := newFixture(t)
	id := f.enqueue(t)

	started := make(chan struct{})
	w := jobs.New(f.pool, jobs.Options{
		Logger:          quietLogger(),
		ShutdownTimeout: 200 * time.Millisecond,
		PollInterval:    50 * time.Millisecond,
		Handler: func(ctx context.Context, _ jobs.Job) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runWorker(ctx, w)
	<-started
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop after the drain deadline")
	}

	got := f.row(t, id)
	if got.State != "queued" || got.LeaseOwner != nil || got.NextAttempt > 0 {
		t.Fatalf("released job = %+v, want immediately claimable queued job", got)
	}

	next := make(chan int64, 1)
	w2 := jobs.New(workerPool(t, f.db), jobs.Options{
		Logger:          quietLogger(),
		ShutdownTimeout: time.Second,
		PollInterval:    50 * time.Millisecond,
		Handler: func(_ context.Context, job jobs.Job) error {
			next <- job.Generation
			return nil
		},
	})
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done2 := runWorker(ctx2, w2)
	select {
	case gen := <-next:
		if gen != 2 {
			t.Errorf("reclaimed generation = %d, want 2", gen)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released job was not reclaimed by another worker")
	}
	cancel2()
	<-done2
}

func TestHandlerFailureSchedulesRetry(t *testing.T) {
	f := newFixture(t)
	id := f.enqueue(t)
	handled := make(chan struct{})
	w := jobs.New(f.pool, jobs.Options{
		Logger:          quietLogger(),
		ShutdownTimeout: time.Second,
		PollInterval:    50 * time.Millisecond,
		RetryAfter:      time.Minute,
		Handler: func(context.Context, jobs.Job) error {
			defer close(handled)
			return errors.New("provider unavailable")
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runWorker(ctx, w)
	<-handled
	deadline := time.Now().Add(5 * time.Second)
	for f.row(t, id).State == "running" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	got := f.row(t, id)
	if got.State != "retry" || got.LeaseOwner != nil || got.NextAttempt < 50*time.Second {
		t.Fatalf("failed job = %+v, want retry scheduled about 1m ahead", got)
	}
}

func TestUnhandledJobIsRetriedNotDropped(t *testing.T) {
	f := newFixture(t)
	id := f.enqueue(t)
	w := jobs.New(f.pool, jobs.Options{
		Logger:          quietLogger(),
		ShutdownTimeout: time.Second,
		PollInterval:    50 * time.Millisecond,
		Handler:         jobs.Unhandled,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runWorker(ctx, w)
	deadline := time.Now().Add(5 * time.Second)
	for f.row(t, id).State != "retry" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if got := f.row(t, id); got.State != "retry" || got.Generation != 1 || got.NextAttempt < 50*time.Second {
		t.Fatalf("unhandled job = %+v, want retry about 1m ahead", got)
	}
}

// TestGenerationAloneFencesSameWorker isolates the generation predicate: the
// same worker reclaims its own expired lease, so owner and state still match.
func TestGenerationAloneFencesSameWorker(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id := f.enqueue(t)
	const worker = "44444444-4444-4444-8444-444444444444"
	claim := func(lease string) int64 {
		t.Helper()
		var gen int64
		if err := f.pool.QueryRow(ctx, "SELECT job_generation FROM business.job_claim($1::uuid, $2::interval)", worker, lease).Scan(&gen); err != nil {
			t.Fatalf("claim: %v", err)
		}
		return gen
	}
	gen1 := claim("1 second")
	time.Sleep(1200 * time.Millisecond)
	gen2 := claim("60 seconds")
	if gen2 != gen1+1 {
		t.Fatalf("reclaimed generation %d, want %d", gen2, gen1+1)
	}
	var ok bool
	for _, sql := range []string{
		"SELECT business.job_heartbeat($1::uuid, $2::uuid, $3, interval '60 seconds') IS NOT NULL",
		"SELECT business.job_release($1::uuid, $2::uuid, $3, interval '0')",
		"SELECT business.job_complete($1::uuid, $2::uuid, $3)",
	} {
		if err := f.pool.QueryRow(ctx, sql, id, worker, gen1).Scan(&ok); err != nil || ok {
			t.Errorf("same worker, stale generation: %q = %v, %v; want fenced false", sql, ok, err)
		}
	}
	if err := f.pool.QueryRow(ctx, "SELECT business.job_complete($1::uuid, $2::uuid, $3)", id, worker, gen2).Scan(&ok); err != nil || !ok {
		t.Fatalf("current generation complete = %v, %v; want true", ok, err)
	}
}

func TestConcurrentClaimersNeverShareAJob(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	const jobsCount, claimers = 30, 10
	for range jobsCount {
		f.enqueue(t)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range claimers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := fmt.Sprintf("55555555-5555-4555-8555-%012d", i)
			<-start
			for {
				var id string
				err := f.pool.QueryRow(ctx, "SELECT job_id::text FROM business.job_claim($1::uuid, interval '60 seconds')", worker).Scan(&id)
				if errors.Is(err, pgx.ErrNoRows) {
					return
				}
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				mu.Lock()
				seen[id]++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(seen) != jobsCount {
		t.Fatalf("claimed %d distinct jobs, want %d", len(seen), jobsCount)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %s claimed %d times by concurrent workers", id, n)
		}
	}
}

func TestHeartbeatKeepsLongJobLeased(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id := f.enqueue(t)
	finished := make(chan struct{})
	w := jobs.New(f.pool, jobs.Options{
		Logger:          quietLogger(),
		ShutdownTimeout: time.Second,
		PollInterval:    50 * time.Millisecond,
		Lease:           1500 * time.Millisecond,
		Handler: func(ctx context.Context, _ jobs.Job) error {
			defer close(finished)
			select {
			case <-time.After(3500 * time.Millisecond): // more than twice the lease
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	runCtx, cancel := context.WithCancel(ctx)
	done := runWorker(runCtx, w)

	thief := workerPool(t, f.db)
	stolen := 0
probe:
	for {
		select {
		case <-finished:
			break probe
		case <-time.After(200 * time.Millisecond):
			var n int
			if err := thief.QueryRow(ctx, "SELECT count(*) FROM business.job_claim('66666666-6666-4666-8666-666666666666', interval '60 seconds')").Scan(&n); err != nil {
				t.Fatal(err)
			}
			stolen += n
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.row(t, id).State == "running" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if stolen != 0 {
		t.Fatalf("another worker claimed the job %d times while heartbeats were succeeding", stolen)
	}
	if got := f.row(t, id); got.State != "completed" || got.Generation != 1 {
		t.Fatalf("long job = %+v, want completed at generation 1", got)
	}
}

func TestLostLeaseCancelsHandlerWithoutFinalizing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id := f.enqueue(t)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var once sync.Once
	w := jobs.New(f.pool, jobs.Options{
		Logger:          quietLogger(),
		ShutdownTimeout: time.Second,
		PollInterval:    50 * time.Millisecond,
		Lease:           1500 * time.Millisecond,
		Handler: func(ctx context.Context, _ jobs.Job) error {
			first := false
			once.Do(func() { first = true; close(started) })
			<-ctx.Done()
			if first {
				close(cancelled)
			}
			return ctx.Err()
		},
	})
	runCtx, cancel := context.WithCancel(ctx)
	done := runWorker(runCtx, w)
	<-started

	// Simulate another worker reclaiming the lease.
	const other = "77777777-7777-4777-8777-777777777777"
	if _, err := f.admin.Exec(ctx, "UPDATE business.job SET lease_owner = $2::uuid, generation = generation + 1, lease_until = now() + interval '1 minute' WHERE id = $1::uuid", id, other); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not cancelled after the lease was lost")
	}
	cancel()
	<-done
	got := f.row(t, id)
	if got.State != "running" || got.LeaseOwner == nil || *got.LeaseOwner != other || got.Generation != 2 {
		t.Fatalf("job after lost lease = %+v, want untouched running lease of %s at generation 2", got, other)
	}
}

func TestHeartbeatFailuresPastLeaseCancelHandler(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id := f.enqueue(t)
	started := make(chan struct{})
	cancelled := make(chan time.Time, 1)
	var once sync.Once
	w := jobs.New(f.pool, jobs.Options{
		Logger:          quietLogger(),
		ShutdownTimeout: time.Second,
		PollInterval:    50 * time.Millisecond,
		Lease:           1500 * time.Millisecond,
		Handler: func(ctx context.Context, _ jobs.Job) error {
			// Once the lease runs out the loop may reclaim the job before the
			// test stops it; only the first run reports.
			first := false
			once.Do(func() { first = true; close(started) })
			<-ctx.Done()
			if first {
				cancelled <- time.Now()
			}
			return ctx.Err()
		},
	})
	runCtx, cancel := context.WithCancel(ctx)
	done := runWorker(runCtx, w)
	<-started
	began := time.Now()

	// Heartbeats now fail (as during a database outage) while the lease runs out.
	if _, err := f.admin.Exec(ctx, "REVOKE EXECUTE ON FUNCTION business.job_heartbeat(uuid, uuid, bigint, interval) FROM gk_worker"); err != nil {
		t.Fatal(err)
	}
	var at time.Time
	select {
	case at = <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("handler kept running after heartbeats failed past the lease")
	}
	cancel()
	<-done
	if elapsed := at.Sub(began); elapsed < time.Second {
		t.Errorf("handler cancelled after %v, before the lease could have expired", elapsed)
	}
	// A reclaimed second run may be released to queued during shutdown; the
	// abandoned first run must neither complete nor release (retry) the job.
	if got := f.row(t, id); got.State == "completed" || got.State == "retry" {
		t.Fatalf("job = %+v; a worker past its lease must not finalize the job", got)
	}
}

// TestExpiredLeaseIsReclaimedAndStaleFinalizeFenced exercises the gateway's
// generation fence: a stalled worker cannot finalize after reclaim.
func TestExpiredLeaseIsReclaimedAndStaleFinalizeFenced(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id := f.enqueue(t)
	const stalled = "11111111-1111-4111-8111-111111111111"
	const fresh = "22222222-2222-4222-8222-222222222222"

	var gen1 int64
	if err := f.pool.QueryRow(ctx, "SELECT job_generation FROM business.job_claim($1::uuid, interval '1 second')", stalled).Scan(&gen1); err != nil {
		t.Fatal(err)
	}
	var none int
	err := f.pool.QueryRow(ctx, "SELECT count(*) FROM business.job_claim($1::uuid, interval '60 seconds')", fresh).Scan(&none)
	if err != nil || none != 0 {
		t.Fatalf("claim of a live lease returned %d rows, err %v; want 0", none, err)
	}
	time.Sleep(1200 * time.Millisecond)

	var gen2 int64
	if err := f.pool.QueryRow(ctx, "SELECT job_generation FROM business.job_claim($1::uuid, interval '60 seconds')", fresh).Scan(&gen2); err != nil {
		t.Fatalf("reclaim after expiry: %v", err)
	}
	if gen2 != gen1+1 {
		t.Fatalf("reclaimed generation %d, want %d", gen2, gen1+1)
	}
	var ok bool
	for _, sql := range []string{
		"SELECT business.job_complete($1::uuid, $2::uuid, $3)",
		"SELECT business.job_heartbeat($1::uuid, $2::uuid, $3, interval '60 seconds') IS NOT NULL",
		"SELECT business.job_release($1::uuid, $2::uuid, $3, interval '0')",
	} {
		if err := f.pool.QueryRow(ctx, sql, id, stalled, gen1).Scan(&ok); err != nil || ok {
			t.Errorf("stale worker %q = %v, %v; want fenced false", sql, ok, err)
		}
	}
	if err := f.pool.QueryRow(ctx, "SELECT business.job_complete($1::uuid, $2::uuid, $3)", id, fresh, gen2).Scan(&ok); err != nil || !ok {
		t.Fatalf("current lease complete = %v, %v; want true", ok, err)
	}
}

func TestGatewayRejectsUnboundedLeases(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t)
	for _, lease := range []string{"0 seconds", "-1 second", "11 minutes"} {
		_, err := f.pool.Exec(context.Background(),
			"SELECT * FROM business.job_claim('33333333-3333-4333-8333-333333333333', $1::interval)", lease)
		if got := dbtest.SQLState(err); got != "22023" {
			t.Errorf("lease %s: SQLSTATE %q (err=%v), want 22023", lease, got, err)
		}
	}
}
