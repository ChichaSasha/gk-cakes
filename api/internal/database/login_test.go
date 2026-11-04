package database

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// fakeRow scans one login-query answer, or fails. current defaults to user
// and usable to groups (a login acting as itself, inheriting its groups).
type fakeRow struct {
	user, current  string
	elevated       bool
	groups, usable []string
	err            error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.current == "" {
		r.current = r.user
	}
	if r.usable == nil {
		r.usable = r.groups
	}
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*bool) = r.user, r.current, r.elevated
	*dest[3].(*[]string), *dest[4].(*[]string) = r.groups, r.usable
	return nil
}

// fakePool answers the login query from rows in turn (the last repeats) and
// counts queries; pingErr fails Ping.
type fakePool struct {
	rows    []fakeRow
	queries int
	pingErr error
}

func (p *fakePool) QueryRow(context.Context, string, ...any) pgx.Row {
	r := p.rows[min(p.queries, len(p.rows)-1)]
	p.queries++
	return r
}

func (p *fakePool) Ping(context.Context) error { return p.pingErr }

func TestCheckLoginDecisions(t *testing.T) {
	ctx := context.Background()
	good := fakeRow{user: "gk_api_staging", groups: []string{"gk_api"}}
	if err := CheckLogin(ctx, &fakePool{rows: []fakeRow{good}}, "gk_api"); err != nil {
		t.Fatalf("exactly gk_api: %v", err)
	}
	for name, c := range map[string]struct {
		row  fakeRow
		want string
	}{
		"elevated":     {fakeRow{user: "postgres", elevated: true, groups: []string{"gk_api"}}, "elevated attribute"},
		"other group":  {fakeRow{user: "x", groups: []string{"gk_worker"}}, "holds gk_worker"},
		"extra group":  {fakeRow{user: "x", groups: []string{"gk_api", "gk_owner"}}, "holds gk_api, gk_owner"},
		"no group":     {fakeRow{user: "x"}, "holds no role"},
		"names itself": {fakeRow{user: "gk_api_staging", groups: []string{}}, "gk_api_staging must hold only the gk_api group"},
		"switched role": {fakeRow{user: "postgres", current: "gk_api_staging", groups: []string{"gk_api"}},
			"the session of postgres acts as role gk_api_staging"},
		"not inheriting": {fakeRow{user: "x", groups: []string{"gk_api"}, usable: []string{}}, "does not inherit"},
	} {
		err := CheckLogin(ctx, &fakePool{rows: []fakeRow{c.row}}, "gk_api")
		if !errors.Is(err, ErrLogin) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want ErrLogin mentioning %q", name, err, c.want)
		}
	}
	down := errors.New("connection refused")
	if err := CheckLogin(ctx, &fakePool{rows: []fakeRow{{err: down}}}, "gk_api"); errors.Is(err, ErrLogin) || !errors.Is(err, down) {
		t.Errorf("query failure: %v, want the query error, not ErrLogin", err)
	}
}

func TestWaitLoginRetriesOnlyUnreachable(t *testing.T) {
	ctx := context.Background()
	down := fakeRow{err: errors.New("connection refused")}
	good := fakeRow{user: "w", groups: []string{"gk_worker"}}
	p := &fakePool{rows: []fakeRow{down, down, good}}
	if err := WaitLogin(ctx, p, "gk_worker", time.Millisecond); err != nil || p.queries != 3 {
		t.Fatalf("WaitLogin: %v after %d queries, want pass after 3", err, p.queries)
	}
	bounded, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	wrong := &fakePool{rows: []fakeRow{{user: "w", groups: []string{"gk_api"}}}}
	if err := WaitLogin(bounded, wrong, "gk_worker", time.Millisecond); !errors.Is(err, ErrLogin) || wrong.queries != 1 {
		t.Fatalf("wrong login: %v after %d queries, want ErrLogin at once", err, wrong.queries)
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	stuck := &fakePool{rows: []fakeRow{down}}
	err := WaitLogin(short, stuck, "gk_worker", time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrLogin) || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("unreachable until the deadline: %v", err)
	}
}

func TestLoginGate(t *testing.T) {
	ctx := context.Background()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	down := &fakePool{pingErr: errors.New("down"), rows: []fakeRow{{user: "a", groups: []string{"gk_api"}}}}
	if err := (&LoginGate{Pool: down, Group: "gk_api", Logger: logger}).Ping(ctx); err == nil || down.queries != 0 {
		t.Fatalf("database down: %v (%d login queries), want the ping error first", err, down.queries)
	}

	p := &fakePool{rows: []fakeRow{{err: errors.New("timeout")}, {user: "a", groups: []string{"gk_api"}}}}
	g := &LoginGate{Pool: p, Group: "gk_api", Logger: logger}
	if err := g.Ping(ctx); err == nil || errors.Is(err, ErrLogin) {
		t.Fatalf("check failed to run: %v, want a non-ErrLogin error", err)
	}
	for i := 0; i < 3; i++ {
		if err := g.Ping(ctx); err != nil {
			t.Fatalf("verified login: %v", err)
		}
	}
	if p.queries != 2 {
		t.Errorf("login queried %d times, want 2 (once verified it is not re-checked)", p.queries)
	}

	bad := &fakePool{rows: []fakeRow{{user: "postgres", elevated: true, groups: []string{"gk_api"}}}}
	b := &LoginGate{Pool: bad, Group: "gk_api", Logger: logger}
	for i := 0; i < 3; i++ {
		if err := b.Ping(ctx); !errors.Is(err, ErrLogin) {
			t.Fatalf("elevated login: %v, want ErrLogin every time", err)
		}
	}
	if n := strings.Count(logs.String(), `"database login refused"`); n != 1 {
		t.Errorf("refusal logged %d times, want once", n)
	}
	if n := strings.Count(logs.String(), `"database login verified"`); n != 1 {
		t.Errorf("verification logged %d times, want once", n)
	}
	if strings.Contains(logs.String(), "timeout") {
		t.Error("a failed check that could not run must not be reported as a refused login")
	}
}
