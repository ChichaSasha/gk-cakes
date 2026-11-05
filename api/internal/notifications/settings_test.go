package notifications

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

type settingsDB struct {
	calls int
	args  []any
	fail  error
}

func (d *settingsDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	d.calls++
	d.args = args
	return settingsRow{d.fail}
}

type settingsRow struct{ err error }

func (r settingsRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*int) = 1
	return nil
}

// Readiness records the notice settings on every probe after the database
// answers (lower-case origin and allowlist, an empty list rather than
// NULL), and is not ready while recording fails.
func TestSettingsGate(t *testing.T) {
	down := errors.New("database down")
	pingErr := down
	db := &settingsDB{fail: errors.New("no function")}
	g := &SettingsGate{Pinger: pingFunc(func(context.Context) error { return pingErr }), DB: db,
		Environment: "staging", UIOrigin: "https://Staging.GKcake.com", Allowlist: []string{"@GKcake.com"}}
	if err := g.Ping(context.Background()); !errors.Is(err, down) || db.calls != 0 {
		t.Fatalf("database down: %v, %d calls", err, db.calls)
	}
	pingErr = nil
	if err := g.Ping(context.Background()); err == nil || db.calls != 1 {
		t.Fatalf("recording failed: %v, %d calls; want not ready", err, db.calls)
	}
	db.fail = nil
	for range 3 {
		if err := g.Ping(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if db.calls != 4 || db.args[0] != "staging" || db.args[1] != "https://staging.gkcake.com" ||
		fmt.Sprint(db.args[2]) != "[@gkcake.com]" {
		t.Fatalf("%d calls with %v; want one record per probe, lower-case", db.calls, db.args)
	}
	g.Allowlist = nil
	if err := g.Ping(context.Background()); err != nil || db.args[2] == nil || len(db.args[2].([]string)) != 0 {
		t.Fatalf("an unset allowlist must be recorded as empty, not NULL: %#v", db.args[2])
	}
}

func TestAllowlistAllows(t *testing.T) {
	a := Allowlist{"@gkcake.com", "tester@example.org"}
	for addr, want := range map[string]bool{
		"chef@gkcake.com": true, "Chef@GKCake.COM": true, "tester@example.org": true, "TESTER@example.org": true,
		"x@sub.gkcake.com": false, "x@gkcake.com.example": false, "a.tester@example.org": false, "a@b@gkcake.com": false,
		"customer@example.com": false, "": false,
	} {
		if got := a.Allows(addr); got != want {
			t.Errorf("Allows(%q) = %v, want %v", addr, got, want)
		}
	}
	if !(Allowlist{}).Allows("anyone@example.com") || !Allowlist(nil).Allows("anyone@example.com") {
		t.Error("an empty allowlist must allow everyone")
	}
}
