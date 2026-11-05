package notifications

import (
	"context"
	"fmt"
	"strings"
)

// RecordSettings records this environment, the UI origin of order page
// links and the recipient allowlist (EMAIL_RECIPIENT_ALLOWLIST; required in
// staging, empty in production) with business.notice_settings_record
// (migration 0030). Order notices are rendered in the database inside the
// transaction of their transition: a database without these settings
// records its notices as not_configured, and outside production a notice to
// a recipient off the allowlist is never queued. The call writes only when
// something changed.
func RecordSettings(ctx context.Context, db Querier, env, uiOrigin string, allowlist []string) error {
	entries := make([]string, len(allowlist))
	for i, e := range allowlist {
		entries[i] = strings.ToLower(e)
	}
	var one int
	err := db.QueryRow(ctx, "SELECT 1 FROM business.notice_settings_record($1, $2, $3::text[])",
		env, strings.ToLower(uiOrigin), entries).Scan(&one)
	if err != nil {
		return fmt.Errorf("notifications: record notice settings: %w", err)
	}
	return nil
}

// SettingsGate is the API's readiness check: after Pinger succeeds it
// records the notice settings (RecordSettings) on every probe, which costs
// one statement and writes nothing unless they changed, so settings lost
// with a restored database or changed by a redeploy are recorded again
// before the API reports ready.
type SettingsGate struct {
	Pinger interface {
		Ping(ctx context.Context) error
	}
	DB          Querier
	Environment string
	UIOrigin    string
	Allowlist   []string
}

// Ping checks the database, then records the settings.
func (g *SettingsGate) Ping(ctx context.Context) error {
	if err := g.Pinger.Ping(ctx); err != nil {
		return err
	}
	return RecordSettings(ctx, g.DB, g.Environment, g.UIOrigin, g.Allowlist)
}
