package notifications

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// RecordSettings records this environment and the UI origin of order page
// links (business.notice_settings_record, migration 0030): order notices are
// rendered in the database, inside the transaction of their transition, and
// a database without these settings records its notices as not_configured.
func RecordSettings(ctx context.Context, db Querier, env, uiOrigin string) error {
	var one int
	err := db.QueryRow(ctx, "SELECT 1 FROM business.notice_settings_record($1, $2)", env, strings.ToLower(uiOrigin)).Scan(&one)
	if err != nil {
		return fmt.Errorf("notifications: record notice settings: %w", err)
	}
	return nil
}

// SettingsGate is the API's readiness check: after Pinger succeeds it
// records the notice settings once (RecordSettings), and it reports ready
// only when they have been recorded, so a deploy never serves before its
// order notices can be rendered.
type SettingsGate struct {
	Pinger interface {
		Ping(ctx context.Context) error
	}
	DB          Querier
	Environment string
	UIOrigin    string
	recorded    atomic.Bool
}

// Ping checks the database and records the settings until that succeeds.
func (g *SettingsGate) Ping(ctx context.Context) error {
	if err := g.Pinger.Ping(ctx); err != nil {
		return err
	}
	if g.recorded.Load() {
		return nil
	}
	if err := RecordSettings(ctx, g.DB, g.Environment, g.UIOrigin); err != nil {
		return err
	}
	g.recorded.Store(true)
	return nil
}
