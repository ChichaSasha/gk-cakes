package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrLogin reports a runtime login that is not least-privilege: it does not
// hold exactly its one group role, or it has an elevated attribute. It is
// permanent until an operator fixes the login or the service's DATABASE_URL.
var ErrLogin = errors.New("database: runtime login is not least-privilege")

// Querier is satisfied by *pgxpool.Pool and *pgx.Conn.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// loginQuery mirrors the provisioning checks in api/README.md (Database
// roles): every role the login is a member of, directly or through another
// role, and whether it has any elevated attribute.
const loginQuery = `SELECT r.rolname::text,
	r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls,
	ARRAY(SELECT g.rolname::text FROM pg_roles g
		WHERE g.oid <> r.oid AND pg_has_role(r.oid, g.oid, 'MEMBER') ORDER BY 1)
FROM pg_roles r WHERE r.rolname = current_user`

// CheckLogin verifies that the connected login holds exactly group (for
// example gk_api) and no superuser, createrole, createdb, replication or
// bypassrls attribute, so a service can never run with an administrator or
// migration credential, or with another service's login. It returns an
// ErrLogin-wrapped error naming the login and its roles (never credentials),
// or a plain error when the check itself could not run.
func CheckLogin(ctx context.Context, q Querier, group string) error {
	var (
		user     string
		elevated bool
		groups   []string
	)
	if err := q.QueryRow(ctx, loginQuery).Scan(&user, &elevated, &groups); err != nil {
		return fmt.Errorf("database: check login: %w", err)
	}
	if elevated || len(groups) != 1 || groups[0] != group {
		held := strings.Join(groups, ", ")
		if held == "" {
			held = "no role"
		}
		attr := ""
		if elevated {
			attr = " and an elevated attribute"
		}
		return fmt.Errorf("%w: %s must hold only the %s group and no elevated attribute; it holds %s%s",
			ErrLogin, user, group, held, attr)
	}
	return nil
}

// WaitLogin runs CheckLogin until it passes, fails with ErrLogin, or ctx ends,
// retrying other errors (an unreachable database) every interval. A process
// that refuses to start on ErrLogin should still start once a briefly
// unavailable database answers.
func WaitLogin(ctx context.Context, q Querier, group string, interval time.Duration) error {
	for {
		err := CheckLogin(ctx, q, group)
		if err == nil || errors.Is(err, ErrLogin) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last error: %v)", ctx.Err(), err)
		case <-time.After(interval):
		}
	}
}

// Pool is the part of *pgxpool.Pool that LoginGate uses.
type Pool interface {
	Querier
	Ping(ctx context.Context) error
}

// LoginGate is a readiness check: the database answers and, once per process,
// CheckLogin passed for Group. A deployment whose health check reads it never
// becomes ready with the wrong login, so the previous one keeps serving.
type LoginGate struct {
	Pool   Pool
	Group  string
	Logger *slog.Logger

	verified atomic.Bool
	reported atomic.Bool
}

// Ping implements the readiness check.
func (g *LoginGate) Ping(ctx context.Context) error {
	if err := g.Pool.Ping(ctx); err != nil {
		return err
	}
	if g.verified.Load() {
		return nil
	}
	err := CheckLogin(ctx, g.Pool, g.Group)
	switch {
	case err == nil:
		if g.verified.CompareAndSwap(false, true) {
			g.Logger.Info("database login verified", "group", g.Group)
		}
	case errors.Is(err, ErrLogin) && g.reported.CompareAndSwap(false, true):
		g.Logger.Error("database login refused", "error", err.Error())
	}
	return err
}
