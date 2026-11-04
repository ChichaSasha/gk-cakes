package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// canary appears inside secret values; it must never surface in errors or output.
const canary = "c4n4ry-s3cr3t"

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func merge(base map[string]string, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

var validLocalAPI = map[string]string{
	"APP_ENV":      "local",
	"DATABASE_URL": "postgres://gk_api:" + canary + "@localhost:5432/gk?sslmode=disable",
	"UI_ORIGIN":    "http://localhost:5173",
}

var validProductionAPI = map[string]string{
	"APP_ENV":      "production",
	"DATABASE_URL": "postgresql://gk_api:" + canary + "@db.example.supabase.co:5432/postgres?sslmode=verify-full",
	"UI_ORIGIN":    "https://gkcake.com",
}

func TestLoadAPIRejectsMissingSecretAndOrigin(t *testing.T) {
	_, err := LoadAPI(env(map[string]string{"APP_ENV": "staging"}))
	if err == nil {
		t.Fatal("LoadAPI accepted configuration without DATABASE_URL and UI_ORIGIN")
	}
	for _, name := range []string{"DATABASE_URL", "UI_ORIGIN"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name missing %s", err, name)
		}
	}
}

func TestLoadAPIAcceptsValidConfiguration(t *testing.T) {
	local, err := LoadAPI(env(validLocalAPI))
	if err != nil {
		t.Fatalf("valid local configuration rejected: %v", err)
	}
	if local.Env != EnvLocal || local.Addr != ":8080" || local.UIOrigin != "http://localhost:5173" {
		t.Errorf("unexpected local configuration: env=%q addr=%q origin=%q", local.Env, local.Addr, local.UIOrigin)
	}
	if local.ShutdownTimeout != 20*time.Second {
		t.Errorf("default shutdown timeout = %v, want 20s", local.ShutdownTimeout)
	}
	if local.DatabaseURL.Reveal() != validLocalAPI["DATABASE_URL"] {
		t.Error("DATABASE_URL not preserved for the database client")
	}

	prod, err := LoadAPI(env(merge(validProductionAPI, map[string]string{
		"PORT":             "9000",
		"SHUTDOWN_TIMEOUT": "30s",
		"PAYMENT_SECRET":   "sk_live_" + canary,
	})))
	if err != nil {
		t.Fatalf("valid production configuration rejected: %v", err)
	}
	if prod.Addr != ":9000" || prod.ShutdownTimeout != 30*time.Second {
		t.Errorf("unexpected production configuration: addr=%q shutdown=%v", prod.Addr, prod.ShutdownTimeout)
	}
	if !prod.Providers.PaymentSecret.IsSet() {
		t.Error("PAYMENT_SECRET not loaded")
	}

	if _, err := LoadAPI(env(merge(validLocalAPI, map[string]string{
		"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres?sslmode=verify-full",
	}))); err != nil {
		t.Errorf("local process on a remote database with verify-full rejected: %v", err)
	}
}

func TestLoadAPIRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		mention string
	}{
		{"missing APP_ENV", merge(validLocalAPI, map[string]string{"APP_ENV": ""}), "APP_ENV"},
		{"unknown APP_ENV", merge(validLocalAPI, map[string]string{"APP_ENV": "prod"}), "APP_ENV"},
		{"non-postgres database URL", merge(validLocalAPI, map[string]string{"DATABASE_URL": "mysql://u:" + canary + "@localhost/db"}), "DATABASE_URL"},
		{"malformed database URL", merge(validLocalAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@local host:99999/db"}), "DATABASE_URL"},
		{"wildcard origin", merge(validLocalAPI, map[string]string{"UI_ORIGIN": "*"}), "UI_ORIGIN"},
		{"origin with path", merge(validLocalAPI, map[string]string{"UI_ORIGIN": "http://localhost:5173/admin"}), "UI_ORIGIN"},
		{"origin with trailing slash", merge(validLocalAPI, map[string]string{"UI_ORIGIN": "http://localhost:5173/"}), "UI_ORIGIN"},
		{"invalid port", merge(validLocalAPI, map[string]string{"PORT": "70000"}), "PORT"},
		{"invalid shutdown timeout", merge(validLocalAPI, map[string]string{"SHUTDOWN_TIMEOUT": "soon"}), "SHUTDOWN_TIMEOUT"},
		{"non-positive shutdown timeout", merge(validLocalAPI, map[string]string{"SHUTDOWN_TIMEOUT": "0s"}), "SHUTDOWN_TIMEOUT"},
		{"unrecognised payment secret", merge(validLocalAPI, map[string]string{"PAYMENT_SECRET": "pk_test_" + canary}), "PAYMENT_SECRET"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadAPI(env(tc.env))
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q does not name %s", err, tc.mention)
			}
			if strings.Contains(err.Error(), canary) {
				t.Errorf("error leaks a secret value: %q", err)
			}
		})
	}
}

func TestLoadAPIRejectsUnsafeProductionTestMixing(t *testing.T) {
	staging := merge(validProductionAPI, map[string]string{"APP_ENV": "staging", "UI_ORIGIN": "https://staging.gkcake.com"})
	cases := []struct {
		name    string
		env     map[string]string
		mention string
	}{
		{"production without TLS verification", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres?sslmode=require"}), "sslmode=verify-full"},
		{"staging without sslmode", merge(staging, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres"}), "sslmode=verify-full"},
		{"production on loopback database", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@127.0.0.1/postgres?sslmode=verify-full"}), "DATABASE_URL"},
		{"staging on localhost database", merge(staging, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@localhost/postgres?sslmode=verify-full"}), "DATABASE_URL"},
		{"production plain-HTTP origin", merge(validProductionAPI, map[string]string{"UI_ORIGIN": "http://gkcake.com"}), "UI_ORIGIN"},
		{"staging localhost origin", merge(staging, map[string]string{"UI_ORIGIN": "https://localhost:5173"}), "UI_ORIGIN"},
		{"production with test payment key", merge(validProductionAPI, map[string]string{"PAYMENT_SECRET": "sk_test_" + canary}), "PAYMENT_SECRET"},
		{"staging with live payment key", merge(staging, map[string]string{"PAYMENT_SECRET": "sk_live_" + canary}), "PAYMENT_SECRET"},
		{"test with live restricted payment key", merge(validLocalAPI, map[string]string{"APP_ENV": "test", "PAYMENT_SECRET": "rk_live_" + canary}), "PAYMENT_SECRET"},
		// pgx applies the last sslmode, the ssl alias, host= overrides and
		// multi-host fallbacks; validation must follow what the client does.
		{"duplicate sslmode disabling TLS", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres?sslmode=verify-full&sslmode=disable"}), "sslmode=verify-full"},
		{"ssl alias skipping verification", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres?sslmode=verify-full&ssl=true"}), "sslmode=verify-full"},
		{"host parameter overriding to loopback", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres?sslmode=verify-full&host=127.0.0.1"}), "local database"},
		{"loopback fallback host", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co,127.0.0.1/postgres?sslmode=verify-full"}), "local database"},
		{"unspecified address host", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@0.0.0.0/postgres?sslmode=verify-full"}), "local database"},
		{"unix socket host", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@/postgres?host=/var/run/postgresql&sslmode=verify-full"}), "local database"},
		{"prefer with plaintext fallback", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres?sslmode=prefer"}), "sslmode=verify-full"},
		{"verify-ca without hostname check", merge(validProductionAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres?sslmode=verify-ca"}), "sslmode=verify-full"},
		{"local process on a remote database without TLS", merge(validLocalAPI, map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@db.example.supabase.co/postgres?sslmode=disable"}), "sslmode=verify-full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadAPI(env(tc.env))
			if err == nil {
				t.Fatal("unsafe configuration accepted")
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q does not mention %s", err, tc.mention)
			}
			if strings.Contains(err.Error(), canary) {
				t.Errorf("error leaks a secret value: %q", err)
			}
		})
	}
}

func TestSecretsNeverFormatOrMarshal(t *testing.T) {
	cfg, err := LoadAPI(env(merge(validLocalAPI, map[string]string{
		"PAYMENT_SECRET":         "sk_test_" + canary,
		"PAYMENT_WEBHOOK_SECRET": "whsec_" + canary,
		"EMAIL_API_KEY":          "re_" + canary,
		"EMAIL_WEBHOOK_SECRET":   webhookSecret,
		"STORAGE_WRITE_KEY":      canary,
	})))
	if err != nil {
		t.Fatalf("LoadAPI: %v", err)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", cfg),
		"%+v":  fmt.Sprintf("%+v", cfg),
		"%#v":  fmt.Sprintf("%#v", cfg),
		"%s":   fmt.Sprintf("%s", cfg.DatabaseURL),
		"%q":   fmt.Sprintf("%q", cfg.DatabaseURL),
		"%x":   fmt.Sprintf("%x", cfg.DatabaseURL),
		"json": string(encoded),
	}
	for format, out := range outputs {
		if strings.Contains(out, canary) || strings.Contains(out, fmt.Sprintf("%x", canary)) || strings.Contains(out, webhookSecret[len("whsec_"):]) {
			t.Errorf("%s output leaks a secret value: %s", format, out)
		}
	}
	if !strings.Contains(outputs["%+v"], "[redacted]") {
		t.Errorf("%%+v output does not mark redaction: %s", outputs["%+v"])
	}
}

func TestLoadWorkerUsesOwnDatabaseCredential(t *testing.T) {
	_, err := LoadWorker(env(map[string]string{"APP_ENV": "local"}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("worker without DATABASE_URL: err = %v", err)
	}
	cfg, err := LoadWorker(env(map[string]string{
		"APP_ENV":      "local",
		"DATABASE_URL": "postgres://gk_worker:" + canary + "@localhost/gk",
	}))
	if err != nil {
		t.Fatalf("valid worker configuration rejected: %v", err)
	}
	if cfg.ShutdownTimeout != 20*time.Second {
		t.Errorf("default shutdown timeout = %v, want 20s", cfg.ShutdownTimeout)
	}
	_, err = LoadWorker(env(map[string]string{
		"APP_ENV":      "production",
		"DATABASE_URL": "postgres://gk_worker:" + canary + "@db.example.supabase.co/postgres?sslmode=disable",
	}))
	if err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("production worker without TLS verification: err = %v", err)
	}
}

var validEmailDispatch = map[string]string{
	"EMAIL_API_KEY":            "re_" + canary,
	"RECOVERY_B2_KEY_ID":       "0051a2b3c4d5e6f0000000001",
	"RECOVERY_B2_KEY":          "K005" + canary,
	"RECOVERY_ENCRYPTION_KEYS": "k1:" + canary,
}

var validStagingWorker = map[string]string{
	"APP_ENV":      "staging",
	"DATABASE_URL": "postgresql://gk_worker:" + canary + "@db.example.supabase.co:5432/postgres?sslmode=verify-full",
}

func TestLoadWorkerEmailDispatch(t *testing.T) {
	cfg, err := LoadWorker(env(merge(validStagingWorker, validEmailDispatch)))
	if err != nil {
		t.Fatalf("valid staging worker rejected: %v", err)
	}
	if !cfg.Email.Enabled() || cfg.Email.B2KeyID != validEmailDispatch["RECOVERY_B2_KEY_ID"] ||
		cfg.Email.B2Key.Reveal() != validEmailDispatch["RECOVERY_B2_KEY"] || cfg.Email.EncryptionKeys.Reveal() != validEmailDispatch["RECOVERY_ENCRYPTION_KEYS"] {
		t.Fatalf("email dispatch not loaded: %+v", cfg.Email)
	}
	for _, out := range []string{fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg.Email)} {
		if strings.Contains(out, canary) {
			t.Errorf("formatted configuration leaks a secret: %s", out)
		}
	}
	local := map[string]string{"APP_ENV": "local", "DATABASE_URL": "postgres://gk_worker:" + canary + "@localhost/gk"}
	if cfg, err := LoadWorker(env(local)); err != nil || cfg.Email.Enabled() {
		t.Fatalf("local worker without email: enabled %v, err %v; want accepted and disabled", cfg.Email.Enabled(), err)
	}

	cases := []struct {
		name    string
		env     map[string]string
		mention string
	}{
		{"staging without email dispatch", validStagingWorker, "required in staging"},
		{"production without email dispatch", merge(validStagingWorker, map[string]string{"APP_ENV": "production"}), "required in production"},
		{"staging missing the encryption keys", merge(merge(validStagingWorker, validEmailDispatch), map[string]string{"RECOVERY_ENCRYPTION_KEYS": ""}), "RECOVERY_ENCRYPTION_KEYS"},
		{"local with only the API key", merge(local, map[string]string{"EMAIL_API_KEY": "re_" + canary}), "set together"},
		{"local with only the journal", merge(local, map[string]string{"RECOVERY_B2_KEY_ID": "005abc", "RECOVERY_B2_KEY": canary, "RECOVERY_ENCRYPTION_KEYS": canary}), "set together"},
		{"not a Resend key", merge(merge(validStagingWorker, validEmailDispatch), map[string]string{"EMAIL_API_KEY": "sk_" + canary}), "EMAIL_API_KEY"},
		{"malformed B2 key ID", merge(merge(validStagingWorker, validEmailDispatch), map[string]string{"RECOVERY_B2_KEY_ID": "005/" + canary}), "RECOVERY_B2_KEY_ID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadWorker(env(tc.env))
			if err == nil {
				t.Fatal("invalid worker configuration accepted")
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q does not mention %s", err, tc.mention)
			}
			if strings.Contains(err.Error(), canary) {
				t.Errorf("error leaks a secret value: %q", err)
			}
		})
	}
}

// webhookSecret is a valid Svix signing secret used as a leak canary.
var webhookSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("c4n4ry-webhook-key-0123"))

func TestLoadAPIWebhookSecret(t *testing.T) {
	cfg, err := LoadAPI(env(merge(validLocalAPI, map[string]string{"EMAIL_WEBHOOK_SECRET": webhookSecret})))
	if err != nil || cfg.Providers.EmailWebhookSecret.Reveal() != webhookSecret {
		t.Fatalf("valid webhook secret: %v", err)
	}
	for _, bad := range []string{"whsec_" + canary, "c4n4ry" + webhookSecret, "whsec_" + base64.StdEncoding.EncodeToString([]byte("short"))} {
		_, err := LoadAPI(env(merge(validLocalAPI, map[string]string{"EMAIL_WEBHOOK_SECRET": bad})))
		if err == nil || !strings.Contains(err.Error(), "EMAIL_WEBHOOK_SECRET") || strings.Contains(err.Error(), canary) {
			t.Errorf("invalid webhook secret: err = %v", err)
		}
	}
}

func TestLoadMigrateRequiresMigrationCredential(t *testing.T) {
	_, err := LoadMigrate(env(map[string]string{
		"APP_ENV":      "local",
		"DATABASE_URL": "postgres://gk_api:" + canary + "@localhost/gk",
	}))
	if err == nil || !strings.Contains(err.Error(), "MIGRATION_DATABASE_URL") {
		t.Fatalf("migration must not fall back to the runtime credential: err = %v", err)
	}
	cfg, err := LoadMigrate(env(map[string]string{
		"APP_ENV":                "local",
		"MIGRATION_DATABASE_URL": "postgres://gk_migrator:" + canary + "@localhost/gk",
	}))
	if err != nil {
		t.Fatalf("valid migration configuration rejected: %v", err)
	}
	if !cfg.DatabaseURL.IsSet() {
		t.Error("MIGRATION_DATABASE_URL not loaded")
	}
}
