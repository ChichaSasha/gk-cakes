// Command gk-worker runs the maintenance job loop with the worker credential.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
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

	// No job kinds exist yet; owning tickets register real handlers.
	w := jobs.New(pool, jobs.Options{
		Handler:         jobs.Unhandled,
		Logger:          logger,
		ShutdownTimeout: cfg.ShutdownTimeout,
	})
	if err := w.Run(ctx); err != nil {
		logger.Error("worker", "error", err.Error())
		os.Exit(1)
	}
}
