// Command gk-worker runs the maintenance job loop with the worker credential.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	journalReadyTimeout = 30 * time.Second
	loginReadyTimeout   = 30 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
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

	if cfg.Env != config.EnvLocal { // only local development may use another login
		if code, err := verifyLogin(ctx, pool); err != nil {
			logger.Error("database login", "error", err.Error())
			os.Exit(code)
		}
		logger.Info("database login verified", "group", "gk_worker")
	}

	handler := jobs.Unhandled
	if cfg.Email.Enabled() {
		d, code, err := emailDispatcher(ctx, cfg, pool, logger)
		if err != nil {
			logger.Error("email dispatch", "error", err.Error())
			os.Exit(code)
		}
		handler = d.Handle
		logger.Info("email dispatch enabled", "journal_root", journalRoot(cfg.Env))
	} else {
		logger.Warn("email dispatch not configured; notification jobs are retried, never sent")
	}

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

func journalRoot(env config.Env) string { return "recovery/" + string(env) + "/" }

// verifyLogin refuses to run jobs with any login other than a least-privilege
// gk_worker one (exit 2: the configuration must change), waiting up to
// loginReadyTimeout for a briefly unreachable database (exit 1).
func verifyLogin(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, loginReadyTimeout)
	defer cancel()
	err := database.WaitLogin(ctx, pool, "gk_worker", 2*time.Second)
	switch {
	case err == nil:
		return 0, nil
	case errors.Is(err, database.ErrLogin):
		return 2, err
	default:
		return 1, err
	}
}

// emailDispatcher wires the Resend client and the B2 recovery journal. It
// returns exit code 2 for configuration problems and 1 when the journal is
// unreachable or its key is wrongly scoped (no email may be sent without it).
func emailDispatcher(ctx context.Context, cfg config.Worker, pool *pgxpool.Pool, logger *slog.Logger) (*notifications.Dispatcher, int, error) {
	sealer, err := recovery.NewSealer(cfg.Email.EncryptionKeys.Reveal())
	if err != nil {
		return nil, 2, errors.New("RECOVERY_ENCRYPTION_KEYS: " + err.Error())
	}
	journal, err := recovery.NewB2(recovery.B2Config{KeyID: cfg.Email.B2KeyID, Key: cfg.Email.B2Key.Reveal(), Root: journalRoot(cfg.Env)})
	if err != nil {
		return nil, 2, err
	}
	coord := &recovery.Coordinator{Journal: journal, Sealer: sealer, Environment: string(cfg.Env)}
	readyCtx, cancel := context.WithTimeout(ctx, journalReadyTimeout)
	defer cancel()
	if err := coord.Ready(readyCtx); err != nil {
		return nil, 1, errors.New("recovery journal not ready: " + err.Error())
	}
	client, err := notifications.NewResend(notifications.ResendConfig{APIKey: cfg.Email.APIKey.Reveal()})
	if err != nil {
		return nil, 2, err
	}
	d, err := notifications.NewDispatcher(notifications.DispatcherConfig{
		DB: pool, Coordinator: coord, Sender: client, Environment: string(cfg.Env), Logger: logger,
	})
	if err != nil {
		return nil, 2, err
	}
	return d, 0, nil
}
