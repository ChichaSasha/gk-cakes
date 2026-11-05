package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/jackc/pgx/v5"
)

// fakePool answers Ping and fails any login query, so a check that runs it
// is visible.
type fakePool struct{}

func (fakePool) Ping(context.Context) error { return nil }
func (fakePool) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("login queried")
}

// Only local development may serve /readyz without the login check; test,
// staging and production gate it on a least-privilege gk_api login.
func TestReadinessGatesTheLoginOutsideLocal(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, env := range []config.Env{config.EnvLocal, config.EnvTest, config.EnvStaging, config.EnvProduction} {
		p := readiness(env, fakePool{}, logger)
		gate, gated := p.(*database.LoginGate)
		switch {
		case env == config.EnvLocal && gated:
			t.Errorf("local: readiness is gated; want the plain pool")
		case env == config.EnvLocal:
			if _, ok := p.(fakePool); !ok {
				t.Errorf("local: readiness is %T; want the pool", p)
			}
		case !gated:
			t.Errorf("%s: readiness is %T; want a login gate", env, p)
		case gate.Group != "gk_api" || gate.Pool == nil || gate.Logger == nil:
			t.Errorf("%s: gate %+v; want group gk_api on the pool", env, gate)
		}
	}
}
