// Command gk-api serves the GK Cakes HTTP API.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/ChichaSasha/gk-cakes/api/internal/httpapi"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
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
	opts := httpapi.Options{UIOrigin: cfg.UIOrigin, DB: readiness(cfg.Env, pool, logger), Logger: logger}
	if cfg.Providers.EmailWebhookSecret.IsSet() {
		webhook, err := notifications.NewWebhook(notifications.WebhookConfig{
			DB: pool, Secret: cfg.Providers.EmailWebhookSecret.Reveal(), Environment: string(cfg.Env), Logger: logger,
		})
		if err != nil {
			logger.Error("email webhook", "error", err.Error())
			os.Exit(2)
		}
		opts.EmailWebhook = webhook
		logger.Info("email webhook enabled", "path", "/webhooks/email")
	} else {
		logger.Info("email webhook not configured; /webhooks/email is not served")
	}
	if cfg.Auth.Enabled() {
		opts.Identity = identityService(cfg.Auth, pool)
		logger.Info("identity enabled", "issuer", cfg.Auth.Issuer)
	} else {
		logger.Info("identity not configured; authenticated /api/v1 routes are not served")
	}
	handler := httpapi.NewHandler(opts)
	if err := httpapi.Serve(ctx, ln, handler, cfg.ShutdownTimeout); err != nil {
		logger.Error("serve", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("api stopped")
}

// apiGroup is the only group a deployed API's login may hold.
const apiGroup = "gk_api"

// readiness is the database check behind /readyz, the Railway health check.
// Outside local development it stays unavailable with any login other than a
// least-privilege gk_api one, so such a deploy never becomes ready and the
// previous deployment keeps serving.
func readiness(env config.Env, pool database.Pool, logger *slog.Logger) httpapi.Pinger {
	if env == config.EnvLocal { // only local development may use another login
		return pool
	}
	return &database.LoginGate{Pool: pool, Group: apiGroup, Logger: logger}
}

// identityService verifies the provider's tokens and resolves accounts
// through the gk_api login. Provider calls have their own short budget
// (proposed provider timeout 5s) inside the request deadline.
func identityService(a config.Auth, db identity.Querier) *identity.Service {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: identity.NoRedirects}
	return &identity.Service{
		Verifier:  &identity.Verifier{Issuer: a.Issuer, HTTP: client},
		Sessions:  &identity.SessionChecker{Issuer: a.Issuer, PublishableKey: a.PublishableKey, HTTP: client},
		Directory: &identity.Directory{DB: db},
	}
}
