// Command gk-identity-admin runs the operator identity commands with the
// migration credential (MIGRATION_DATABASE_URL): grant or revoke a staff
// capability, disable or re-enable an account, or show an account. Each
// change is audited by the database. Used to bootstrap the first manager
// approved in GK-010 and to cut off access; staff management by a chef in the
// application comes later.
//
// Usage:
//
//	gk-identity-admin -action grant -subject <uuid> -capability chef -note "first manager (GK-010)"
//	gk-identity-admin -action show -subject <uuid>
//
// Exit status: 0 done (including "no change"), 1 failure, 2 invalid usage.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/database"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	os.Exit(code)
}

var (
	subjectPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	capabilities   = []string{"chef", "fulfillment", "technical_support"}
)

// command is one validated operator command.
type command struct {
	action, subject, capability, note string
}

func parse(args []string, out io.Writer) (command, error) {
	fs := flag.NewFlagSet("gk-identity-admin", flag.ContinueOnError)
	fs.SetOutput(out)
	var c command
	fs.StringVar(&c.action, "action", "", "grant, revoke, disable, enable or show")
	fs.StringVar(&c.subject, "subject", "", "provider user ID (lower-case UUID)")
	fs.StringVar(&c.capability, "capability", "", "chef, fulfillment or technical_support (grant, revoke)")
	fs.StringVar(&c.note, "note", "", "why (1-200 characters, one line; required for changes)")
	if err := fs.Parse(args); err != nil {
		return command{}, err
	}
	switch {
	case fs.NArg() > 0:
		return command{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	case !subjectPattern.MatchString(c.subject):
		return command{}, errors.New("-subject must be a lower-case UUID")
	}
	switch c.action {
	case "grant", "revoke":
		if !contains(capabilities, c.capability) {
			return command{}, errors.New("-capability must be chef, fulfillment or technical_support")
		}
	case "disable", "enable", "show":
		if c.capability != "" {
			return command{}, fmt.Errorf("-capability does not apply to %s", c.action)
		}
	default:
		return command{}, errors.New("-action must be grant, revoke, disable, enable or show")
	}
	if c.action != "show" && (len(c.note) < 1 || len(c.note) > 200 || strings.ContainsAny(c.note, "\r\n")) {
		return command{}, errors.New("-note must be one line of 1 to 200 characters")
	}
	return c, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) int {
	c, err := parse(args, out)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 2
	}
	cfg, err := config.LoadMigrate(getenv)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 2
	}
	conn, err := database.Connect(ctx, cfg.DatabaseURL, "gk-identity-admin")
	if err != nil {
		fmt.Fprintln(out, "error: connect:", err)
		return 1
	}
	defer conn.Close(context.WithoutCancel(ctx))
	msg, err := apply(ctx, conn, c)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "%s (%s)\n", msg, cfg.Env)
	return 0
}

// apply runs c in one transaction as gk_owner, the only role allowed to run
// the operator functions.
func apply(ctx context.Context, conn *pgx.Conn, c command) (string, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		return "", err
	}
	var (
		changed bool
		msg     string
	)
	switch c.action {
	case "grant":
		err = tx.QueryRow(ctx, "SELECT business.staff_grant($1::uuid, $2, $3)", c.subject, c.capability, c.note).Scan(&changed)
		msg = fmt.Sprintf("grant %s to %s: %s", c.capability, c.subject, pick(changed, "granted", "already held"))
	case "revoke":
		err = tx.QueryRow(ctx, "SELECT business.staff_revoke($1::uuid, $2, $3)", c.subject, c.capability, c.note).Scan(&changed)
		msg = fmt.Sprintf("revoke %s from %s: %s", c.capability, c.subject, pick(changed, "revoked", "not held"))
	case "disable", "enable":
		err = tx.QueryRow(ctx, "SELECT business.account_set_disabled($1::uuid, $2, $3)", c.subject, c.action == "disable", c.note).Scan(&changed)
		msg = fmt.Sprintf("%s account of %s: %s", c.action, c.subject, pick(changed, c.action+"d", "no change"))
	case "show":
		msg, err = show(ctx, tx, c.subject)
	}
	if err != nil {
		return "", err
	}
	return msg, tx.Commit(ctx)
}

func show(ctx context.Context, tx pgx.Tx, subject string) (string, error) {
	var (
		id       string
		disabled bool
		caps     []string
	)
	err := tx.QueryRow(ctx, `SELECT a.id::text, a.disabled_at IS NOT NULL,
		coalesce((SELECT array_agg(c.capability ORDER BY c.capability) FROM business.staff_capability c
		          WHERE c.account_id = a.id AND c.revoked_at IS NULL), '{}')
		FROM business.account a WHERE a.provider_subject = $1::uuid`, subject).Scan(&id, &disabled, &caps)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Sprintf("account of %s: none yet", subject), nil
	}
	if err != nil {
		return "", err
	}
	held := strings.Join(caps, ", ")
	if held == "" {
		held = "no staff capability"
	}
	return fmt.Sprintf("account %s of %s: %s; %s", id, subject, pick(disabled, "disabled", "enabled"), held), nil
}

func pick(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}
