// Command gk-migrate applies pending schema migrations once, under a database
// advisory lock, using the migration credential (MIGRATION_DATABASE_URL).
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

	applied, err := migrate.Run(ctx, conn, migrations.FS)
	if err != nil {
		logger.Error("migrate", "error", err.Error(), "applied", applied)
		os.Exit(1)
	}
	logger.Info("migrations complete", "applied", applied, "env", string(cfg.Env))
}
