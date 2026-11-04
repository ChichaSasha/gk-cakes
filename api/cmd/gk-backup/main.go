// Command gk-backup takes the daily independent database backup, or checks
// that the newest backup restores (GK-034 Milestone B).
//
//	gk-backup dump            pg_dump of business + migration history, sealed, to B2
//	gk-backup restore-check   newest backup → disposable loopback database, compared with its manifest
//
// Environment: APP_ENV; BACKUP_DATABASE_URL (dump: the gk_backup login) or
// RESTORE_DATABASE_URL (restore-check: loopback only); BACKUP_B2_KEY_ID and
// BACKUP_B2_KEY (restricted to the backup bucket's database/ prefix, no
// delete); BACKUP_ENCRYPTION_KEYS (kid:base64 32 bytes[,...]); optional
// PG_DUMP, PG_RESTORE and BACKUP_MAX_AGE (default 26h). Output is names,
// versions, counts and hashes only. Exit status: 0 ok, 1 failed, 2 usage.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/backup"
	"github.com/ChichaSasha/gk-cakes/api/internal/config"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) int {
	if len(args) != 1 || (args[0] != "dump" && args[0] != "restore-check") {
		fmt.Fprintln(out, "usage: gk-backup dump | restore-check")
		return 2
	}
	restore := args[0] == "restore-check"
	cfg, err := config.LoadBackup(getenv, restore)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 2
	}
	maxAge := 26 * time.Hour
	if raw := getenv("BACKUP_MAX_AGE"); raw != "" {
		if maxAge, err = time.ParseDuration(raw); err != nil || maxAge <= 0 {
			fmt.Fprintln(out, "error: BACKUP_MAX_AGE must be a positive duration such as 26h")
			return 2
		}
	}
	env := string(cfg.Env)
	sealer, err := recovery.NewSealer(cfg.EncryptionKeys.Reveal())
	if err != nil {
		fmt.Fprintln(out, "error: BACKUP_ENCRYPTION_KEYS:", err)
		return 2
	}
	store, err := recovery.NewB2(recovery.B2Config{KeyID: cfg.B2KeyID, Key: cfg.B2Key.Reveal(), Root: backup.Root(env), Area: "database/"})
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 2
	}
	if restore {
		r := &backup.Restorer{Env: env, Target: cfg.RestoreURL.Reveal(), PgRestore: getenv("PG_RESTORE"), Store: store, Sealer: sealer, MaxAge: maxAge}
		rep, err := r.Check(ctx)
		if rep.Manifest.Dump != "" {
			fmt.Fprintf(out, "backup %s · schema %s · taken %s (%s ago)\n", rep.Manifest.Dump, rep.Manifest.SchemaVersion,
				rep.Manifest.CreatedAt.Format(time.RFC3339), rep.Age.Round(time.Minute))
		}
		if err != nil {
			fmt.Fprintln(out, "restore check FAILED:", err)
			return 1
		}
		fmt.Fprintf(out, "restore check passed: %d migrations, %d business tables, %d rows match the manifest\n",
			len(rep.Manifest.Migrations), rep.Tables, rep.Rows)
		return 0
	}
	d := &backup.Dumper{Env: env, DB: cfg.DatabaseURL.Reveal(), PgDump: getenv("PG_DUMP"), Store: store, Sealer: sealer}
	m, err := d.Dump(ctx)
	if err != nil {
		fmt.Fprintln(out, "backup FAILED:", err)
		return 1
	}
	var total int64
	tables := make([]string, 0, len(m.Rows))
	for t, n := range m.Rows {
		tables = append(tables, t)
		total += n
	}
	sort.Strings(tables)
	fmt.Fprintf(out, "backup stored: %s (%d bytes, sha256 %s)\n", m.Dump, m.DumpBytes, m.DumpSHA256)
	fmt.Fprintf(out, "schema %s · %d migrations · %d business tables · %d rows · %s · server %s\n",
		m.SchemaVersion, len(m.Migrations), len(tables), total, m.PgDump, m.ServerVersion)
	return 0
}
