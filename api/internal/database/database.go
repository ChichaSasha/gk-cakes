// Package database opens PostgreSQL connections for runtime processes and
// migrations without exposing connection strings in errors.
package database

import (
	"context"
	"errors"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runtime budgets proposed in architecture.md: 2s lock/statement per DB call.
const (
	statementTimeout = "2000"
	lockTimeout      = "2000"
)

var errInvalidURL = errors.New("database: invalid connection URL")

// OpenPool returns a lazily connecting pool for a runtime process. The pool
// applies the runtime statement and lock budgets to every connection.
func OpenPool(ctx context.Context, url config.Secret, application string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url.Reveal())
	if err != nil {
		return nil, errInvalidURL
	}
	params := cfg.ConnConfig.RuntimeParams
	params["application_name"] = application
	params["statement_timeout"] = statementTimeout
	params["lock_timeout"] = lockTimeout
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Connect opens a single connection, as migrations need one session for the
// advisory lock. Migration statements are not subject to runtime budgets.
func Connect(ctx context.Context, url config.Secret, application string) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(url.Reveal())
	if err != nil {
		return nil, errInvalidURL
	}
	cfg.RuntimeParams["application_name"] = application
	return pgx.ConnectConfig(ctx, cfg)
}
