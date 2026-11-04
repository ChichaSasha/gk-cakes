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
// roles) for the session's login (session_user, which SET ROLE or a role
// connection option cannot change): whether the session still acts as that
// login, any elevated attribute, every role the login is a member of
// (directly or through another role), and every role whose privileges it can
// use without SET ROLE (membership with INHERIT, PostgreSQL 16+ semantics).
const loginQuery = `SELECT r.rolname::text, current_user::text,
	r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls,
	ARRAY(SELECT g.rolname::text FROM pg_roles g
		WHERE g.oid <> r.oid AND pg_has_role(r.oid, g.oid, 'MEMBER') ORDER BY 1),
	ARRAY(SELECT g.rolname::text FROM pg_roles g
		WHERE g.oid <> r.oid AND pg_has_role(r.oid, g.oid, 'USAGE') ORDER BY 1)
FROM pg_roles r WHERE r.rolname = session_user`

// CheckLogin verifies that the session's login holds exactly group (for
// example gk_api), inherits its privileges, has no superuser, createrole,
// createdb, replication or bypassrls attribute, and has not switched role, so
// a service can never run with an administrator or migration credential, with
// another service's login, or with a login that cannot use its grants. It
// returns an ErrLogin-wrapped error naming the login and its roles (never
// credentials), or a plain error when the check itself could not run.
func CheckLogin(ctx context.Context, q Querier, group string) error {
	var (
		user, current   string
		elevated        bool
		members, usable []string
	)
	if err := q.QueryRow(ctx, loginQuery).Scan(&user, &current, &elevated, &members, &usable); err != nil {
		return fmt.Errorf("database: check login: %w", err)
	}
	only := func(roles []string) bool { return len(roles) == 1 && roles[0] == group }
	switch {
	case current != user:
		return fmt.Errorf("%w: the session of %s acts as role %s; a service must not change role", ErrLogin, user, current)
	case elevated || !only(members):
		held := strings.Join(members, ", ")
		if held == "" {
			held = "no role"
		}
		attr := ""
		if elevated {
			attr = " and an elevated attribute"
		}
		return fmt.Errorf("%w: %s must hold only the %s group and no elevated attribute; it holds %s%s",
			ErrLogin, user, group, held, attr)
	case !only(usable):
		return fmt.Errorf("%w: %s is a member of %s but does not inherit its privileges (NOINHERIT, or granted WITH INHERIT FALSE)",
			ErrLogin, user, group)
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
