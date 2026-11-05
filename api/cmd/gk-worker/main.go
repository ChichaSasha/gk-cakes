// Command gk-worker runs the maintenance job loop with the worker credential.
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/handlers"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/ChichaSasha/gk-cakes/api/internal/payments"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	journalReadyTimeout = 30 * time.Second
	loginReadyTimeout   = 30 * time.Second
)

// startLine is logged first at every start; the staging deploy counts it
// (gk-railway-deploy -start-line in .github/workflows/staging.yml, kept in
// step by a test) to catch a crash and restart.
const startLine = "worker starting"

// workerGroup is the only group a deployed worker's login may hold.
const workerGroup = "gk_worker"

// startLogger returns the process logger after logging startLine, so the
// start line always comes first, before any configuration can fail.
func startLogger(w io.Writer) *slog.Logger {
	logger := slog.New(slog.NewJSONHandler(w, nil))
	logger.Info(startLine)
	return logger
}

func main() {
	logger := startLogger(os.Stdout)
	cfg, err := config.LoadWorker(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err.Error())
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.OpenPool(ctx, cfg.DatabaseURL, "gk-worker")
	if err != nil {
		logger.Error("database pool", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	checked, code, err := startupLogin(ctx, cfg.Env, pool, loginReadyTimeout, 2*time.Second)
	switch {
	case err != nil && code == 0:
		logger.Info("worker stopped while waiting for the database")
		return
	case err != nil:
		logger.Error("database login", "error", err.Error())
		os.Exit(code)
	case checked:
		logger.Info("database login verified", "group", workerGroup)
	}

	emailHandler := jobs.Unhandled
	var coord *recovery.Coordinator
	if cfg.Email.Enabled() {
		d, c, code, err := emailDispatcher(ctx, cfg, pool, logger)
		if err != nil {
			logger.Error("email dispatch", "error", err.Error())
			os.Exit(code)
		}
		emailHandler, coord = d.Handle, c
		logger.Info("email dispatch enabled", "journal_root", journalRoot(cfg.Env),
			"recipient_allowlist_entries", len(cfg.EmailRecipientAllowlist))
	} else {
		logger.Warn("email dispatch not configured; notification jobs are retried, never sent; order commands wait for the recovery journal")
	}
	var (
		stripePayments payments.Provider
		stripeRefunds  payments.RefundProvider
	)
	if cfg.Stripe.Enabled() {
		client, code, err := stripeClient(ctx, cfg, logger)
		if err != nil {
			logger.Error("stripe", "error", err.Error())
			os.Exit(code)
		}
		stripePayments, stripeRefunds = client, client
		logger.Info("stripe payments enabled", "account", cfg.Stripe.AccountID, "api_version", payments.APIVersion)
	} else {
		logger.Warn("stripe not configured; payment and refund jobs wait")
	}
	// One constructor for the worker and the restore's synthetic dispatch
	// check (internal/handlers): order commands, payments, refunds, hold
	// reviews, email. Commands wait without the journal; payments and
	// refunds without Stripe.
	handler := handlers.Chain(workerHandlers(pool, coord, stripePayments, stripeRefunds, emailHandler, string(cfg.Env), cfg.Stripe.AccountID, logger))

	w := jobs.New(pool, jobs.Options{
		Handler:         handler,
		Logger:          logger,
		ShutdownTimeout: cfg.ShutdownTimeout,
	})
	if err := w.Run(ctx); err != nil {
		logger.Error("worker", "error", err.Error())
		os.Exit(1)
	}
}

// workerHandlers is the worker's handler chain (internal/handlers).
func workerHandlers(pool *pgxpool.Pool, coord *recovery.Coordinator, pay payments.Provider, refunds payments.RefundProvider,
	email jobs.Handler, env, account string, logger *slog.Logger) []handlers.Named {
	return handlers.New(handlers.Config{DB: pool, Coordinator: coord, Payments: pay, Refunds: refunds, Email: email,
		Environment: env, Account: account, Logger: logger})
}

func journalRoot(env config.Env) string { return "recovery/" + string(env) + "/" }

// startupLogin decides whether the worker may run jobs. Every environment
// except local development requires a least-privilege gk_worker login
// (checked reports that the check ran); see verifyLogin for the exit codes.
func startupLogin(ctx context.Context, env config.Env, q database.Querier, timeout, interval time.Duration) (checked bool, code int, err error) {
	if env == config.EnvLocal { // only local development may use another login
		return false, 0, nil
	}
	code, err = verifyLogin(ctx, q, timeout, interval)
	return true, code, err
}

// verifyLogin refuses to run jobs with any login other than a least-privilege
// gk_worker one (exit 2: the configuration must change), waiting up to
// timeout for a briefly unreachable database (exit 1). A shutdown signal
// while waiting returns code 0 with the context error: a normal stop.
func verifyLogin(parent context.Context, q database.Querier, timeout, interval time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := database.WaitLogin(ctx, q, workerGroup, interval)
	switch {
	case err == nil:
		return 0, nil
	case errors.Is(err, database.ErrLogin):
		return 2, err
	case parent.Err() != nil:
		return 0, err
	default:
		return 1, err
	}
}

// emailDispatcher wires the Resend client and the B2 recovery journal (also
// returned for order commands). It
// returns exit code 2 for configuration problems and 1 when the journal is
// unreachable or its key is wrongly scoped (no email may be sent without it).
func emailDispatcher(ctx context.Context, cfg config.Worker, pool *pgxpool.Pool, logger *slog.Logger) (*notifications.Dispatcher, *recovery.Coordinator, int, error) {
	sealer, err := recovery.NewSealer(cfg.Email.EncryptionKeys.Reveal())
	if err != nil {
		return nil, nil, 2, errors.New("RECOVERY_ENCRYPTION_KEYS: " + err.Error())
	}
	journal, err := recovery.NewB2(recovery.B2Config{KeyID: cfg.Email.B2KeyID, Key: cfg.Email.B2Key.Reveal(), Root: journalRoot(cfg.Env)})
	if err != nil {
		return nil, nil, 2, err
	}
	coord := &recovery.Coordinator{Journal: journal, Sealer: sealer, Environment: string(cfg.Env)}
	readyCtx, cancel := context.WithTimeout(ctx, journalReadyTimeout)
	defer cancel()
	if err := coord.Ready(readyCtx); err != nil {
		return nil, nil, 1, errors.New("recovery journal not ready: " + err.Error())
	}
	client, err := notifications.NewResend(notifications.ResendConfig{APIKey: cfg.Email.APIKey.Reveal()})
	if err != nil {
		return nil, nil, 2, err
	}
	d, err := notifications.NewDispatcher(notifications.DispatcherConfig{
		DB: pool, Coordinator: coord, Sender: client, Environment: string(cfg.Env),
		Allowlist: cfg.EmailRecipientAllowlist, Logger: logger,
	})
	if err != nil {
		return nil, nil, 2, err
	}
	return d, coord, 0, nil
}

// stripeClient builds the Stripe client: a test-mode key outside production
// (refused otherwise, exit 2) whose account must be STRIPE_ACCOUNT_ID (a
// definite mismatch is exit 2; an unreachable Stripe or a restricted key
// that cannot read the account is only logged, so email and order commands
// keep running).
func stripeClient(ctx context.Context, cfg config.Worker, logger *slog.Logger) (*payments.Client, int, error) {
	client, err := payments.NewClient(payments.ClientConfig{Key: cfg.Stripe.SecretKey.Reveal(), Live: cfg.Env == config.EnvProduction})
	if err != nil {
		return nil, 2, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	account, err := client.AccountID(checkCtx)
	switch {
	case err != nil:
		logger.Warn("stripe account not verified at start", "error", err.Error())
	case account != cfg.Stripe.AccountID:
		return nil, 2, errors.New("STRIPE_SECRET_KEY belongs to another account than STRIPE_ACCOUNT_ID")
	}
	return client, 0, nil
}
