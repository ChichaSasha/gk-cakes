// Command gk-migrate applies pending schema migrations once, under a database
// advisory lock, using the migration credential (MIGRATION_DATABASE_URL). On a
// database that is ahead of this build (a rollback) it verifies its history,
// applies nothing and exits 0.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/migrate"
	"github.com/ChichaSasha/gk-cakes/api/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.LoadMigrate(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err.Error())
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conn, err := database.Connect(ctx, cfg.DatabaseURL, "gk-migrate")
	if err != nil {
		logger.Error("connect", "error", err.Error())
		os.Exit(1)
	}
	defer conn.Close(context.WithoutCancel(ctx))

	report, err := migrate.Apply(ctx, conn, migrations.FS, "")
	if err != nil {
		logger.Error("migrate", "error", err.Error(), "applied", report.Applied)
		os.Exit(1)
	}
	if len(report.Ahead) > 0 {
		// Rollback: an older release on a newer database. Forward-only
		// migrations stay in place; this build only verified its own history.
		logger.Info("database is ahead of this build; nothing applied", "ahead", report.Ahead, "env", string(cfg.Env))
		return
	}
	logger.Info("migrations complete", "applied", report.Applied, "env", string(cfg.Env))
}
