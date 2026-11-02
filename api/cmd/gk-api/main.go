// Command gk-api serves the GK Cakes HTTP API.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.LoadAPI(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err.Error())
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.OpenPool(ctx, cfg.DatabaseURL, "gk-api")
	if err != nil {
		logger.Error("database pool", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		logger.Error("listen", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("api listening", "addr", ln.Addr().String(), "env", string(cfg.Env))
	handler := httpapi.NewHandler(httpapi.Options{UIOrigin: cfg.UIOrigin, DB: pool, Logger: logger})
	if err := httpapi.Serve(ctx, ln, handler, cfg.ShutdownTimeout); err != nil {
		logger.Error("serve", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("api stopped")
}
