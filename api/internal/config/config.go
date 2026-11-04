// Package config loads and validates process configuration from the
// environment. Validation fails closed and error messages name variables only;
// secret values are never included in errors or formatted output.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Env identifies the deployment environment of a process.
type Env string

const (
	EnvLocal      Env = "local"
	EnvTest       Env = "test"
	EnvStaging    Env = "staging"
	EnvProduction Env = "production"
)

// deployed reports whether the environment runs on shared infrastructure,
// where TLS verification and non-loopback endpoints are mandatory.
func (e Env) deployed() bool { return e == EnvStaging || e == EnvProduction }

const defaultShutdownTimeout = 20 * time.Second

// Secret holds a sensitive value. Formatting and marshalling never reveal it;
// call Reveal only where the value is handed to its consuming client.
type Secret struct{ value string }

// NewSecret wraps a value obtained outside environment loading.
func NewSecret(value string) Secret { return Secret{value: value} }

// Reveal returns the secret value.
func (s Secret) Reveal() string { return s.value }

// IsSet reports whether a value is present.
func (s Secret) IsSet() bool { return s.value != "" }

func (s Secret) placeholder() string {
	if s.value == "" {
		return "[unset]"
	}
	return "[redacted]"
}

// Format implements fmt.Formatter for every verb.
func (s Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(s.placeholder())) }

// MarshalText implements encoding.TextMarshaler.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.placeholder()), nil }

// Providers holds backend-only provider credentials. They are optional until
// the adapter consuming each one exists, but are validated whenever present.
type Providers struct {
	PaymentSecret        Secret
	PaymentWebhookSecret Secret
	EmailAPIKey          Secret
	EmailWebhookSecret   Secret
	StorageWriteKey      Secret
}

// API is the configuration of the HTTP API process.
type API struct {
	Env             Env
	Addr            string
	DatabaseURL     Secret
	UIOrigin        string
	ShutdownTimeout time.Duration
	Providers       Providers
	Auth            Auth
}

// Auth configures verification of the identity provider's access tokens
// (Supabase, contracts K00). Both values are public. Required in staging and
// production; elsewhere both or neither are set, and without them the API
// serves no authenticated route.
type Auth struct {
	// Issuer is the exact token issuer, e.g. https://<ref>.supabase.co/auth/v1;
	// the signing keys and the session check are read from it.
	Issuer string
	// PublishableKey is the provider's public client key (sb_publishable_...).
	PublishableKey string
}

// Enabled reports whether token verification is configured.
func (a Auth) Enabled() bool { return a.Issuer != "" }

// Worker is the configuration of the maintenance/job process. It uses its own
// DATABASE_URL value, provisioned with the worker credential.
type Worker struct {
	Env             Env
	DatabaseURL     Secret
	ShutdownTimeout time.Duration
	Email           EmailDispatch
}

// EmailDispatch configures the worker's email engine: the Resend sending key
// and the independent recovery journal every dispatch is written to first
// (a B2 application key restricted to recovery/ and the envelope encryption
// keys). Required in staging and production; elsewhere all or none are set,
// and without them the worker dispatches no email.
type EmailDispatch struct {
	APIKey         Secret
	B2KeyID        string
	B2Key          Secret
	EncryptionKeys Secret
}

// Enabled reports whether email dispatch is configured.
func (e EmailDispatch) Enabled() bool { return e.APIKey.IsSet() }

// Migrate is the configuration of the migration command. It never falls back
// to the runtime DATABASE_URL.
type Migrate struct {
	Env         Env
	DatabaseURL Secret
}

// LoadAPI reads API configuration using getenv.
func LoadAPI(getenv func(string) string) (API, error) {
	var l loader
	l.getenv = getenv
	cfg := API{
		Env:             l.env(),
		Addr:            l.addr("PORT", 8080),
		ShutdownTimeout: l.duration("SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
	}
	cfg.DatabaseURL = l.databaseURL("DATABASE_URL", cfg.Env)
	cfg.UIOrigin = l.origin("UI_ORIGIN", cfg.Env)
	cfg.Providers = Providers{
		PaymentSecret:        l.paymentSecret("PAYMENT_SECRET", cfg.Env),
		PaymentWebhookSecret: l.optionalSecret("PAYMENT_WEBHOOK_SECRET"),
		EmailAPIKey:          l.optionalSecret("EMAIL_API_KEY"),
		EmailWebhookSecret:   l.webhookSecret("EMAIL_WEBHOOK_SECRET"),
		StorageWriteKey:      l.optionalSecret("STORAGE_WRITE_KEY"),
	}
	cfg.Auth = l.auth(cfg.Env)
	return cfg, l.err()
}

// LoadWorker reads worker configuration using getenv.
func LoadWorker(getenv func(string) string) (Worker, error) {
	var l loader
	l.getenv = getenv
	cfg := Worker{
		Env:             l.env(),
		ShutdownTimeout: l.duration("SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
	}
	cfg.DatabaseURL = l.databaseURL("DATABASE_URL", cfg.Env)
	cfg.Email = l.emailDispatch(cfg.Env)
	return cfg, l.err()
}

var b2KeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

func (l *loader) emailDispatch(env Env) EmailDispatch {
	e := EmailDispatch{
		APIKey:         l.optionalSecret("EMAIL_API_KEY"),
		B2KeyID:        l.get("RECOVERY_B2_KEY_ID"),
		B2Key:          l.optionalSecret("RECOVERY_B2_KEY"),
		EncryptionKeys: l.optionalSecret("RECOVERY_ENCRYPTION_KEYS"),
	}
	set := 0
	for _, ok := range []bool{e.APIKey.IsSet(), e.B2KeyID != "", e.B2Key.IsSet(), e.EncryptionKeys.IsSet()} {
		if ok {
			set++
		}
	}
	const names = "EMAIL_API_KEY, RECOVERY_B2_KEY_ID, RECOVERY_B2_KEY and RECOVERY_ENCRYPTION_KEYS"
	switch {
	case set < 4 && env.deployed():
		l.fail("%s are required in %s", names, env)
	case set > 0 && set < 4:
		l.fail("%s must be set together", names)
	}
	if e.APIKey.IsSet() && !strings.HasPrefix(e.APIKey.value, "re_") {
		l.fail("EMAIL_API_KEY must be a Resend API key")
	}
	if e.B2KeyID != "" && !b2KeyIDPattern.MatchString(e.B2KeyID) {
		l.fail("RECOVERY_B2_KEY_ID must be a B2 application key ID")
	}
	return e
}

// LoadMigrate reads migration configuration using getenv.
func LoadMigrate(getenv func(string) string) (Migrate, error) {
	var l loader
	l.getenv = getenv
	cfg := Migrate{Env: l.env()}
	cfg.DatabaseURL = l.databaseURL("MIGRATION_DATABASE_URL", cfg.Env)
	return cfg, l.err()
}

// loader accumulates every validation problem so one run reports them all.
type loader struct {
	getenv   func(string) string
	problems []error
}

func (l *loader) fail(format string, args ...any) {
	l.problems = append(l.problems, fmt.Errorf("config: "+format, args...))
}

func (l *loader) err() error { return errors.Join(l.problems...) }

func (l *loader) get(name string) string { return strings.TrimSpace(l.getenv(name)) }

func (l *loader) env() Env {
	raw := l.get("APP_ENV")
	switch e := Env(raw); e {
	case EnvLocal, EnvTest, EnvStaging, EnvProduction:
		return e
	case "":
		l.fail("APP_ENV is required (local, test, staging or production)")
	default:
		l.fail("APP_ENV must be local, test, staging or production")
	}
	return ""
}

func (l *loader) addr(name string, def int) string {
	raw := l.get(name)
	if raw == "" {
		return ":" + strconv.Itoa(def)
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		l.fail("%s must be a port number between 1 and 65535", name)
		return ""
	}
	return ":" + strconv.Itoa(port)
}

func (l *loader) duration(name string, def time.Duration) time.Duration {
	raw := l.get(name)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		l.fail("%s must be a positive Go duration such as 20s", name)
		return 0
	}
	return d
}

// databaseURL validates a PostgreSQL URL without ever echoing it (parser
// errors embed the raw input, so they are replaced with fixed messages).
// TLS and host rules are checked on the configuration pgx will actually use,
// so duplicate parameters, the ssl alias, host= overrides and multi-host
// fallbacks cannot bypass them. Any non-local database requires verify-full;
// staging and production additionally forbid local targets.
func (l *loader) databaseURL(name string, env Env) Secret {
	raw := l.get(name)
	if raw == "" {
		l.fail("%s is required", name)
		return Secret{}
	}
	u, err := url.Parse(raw)
	if err != nil {
		l.fail("%s is not a valid URL", name)
		return Secret{}
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		l.fail("%s must use the postgres:// or postgresql:// scheme", name)
		return Secret{}
	}
	parsed, err := pgconn.ParseConfig(raw)
	if err != nil {
		l.fail("%s is not a valid PostgreSQL URL, or its certificate files cannot be read", name)
		return Secret{}
	}
	targets := append([]*pgconn.FallbackConfig{{Host: parsed.Host, Port: parsed.Port, TLSConfig: parsed.TLSConfig}}, parsed.Fallbacks...)
	var local, remote, unverified bool
	for _, t := range targets {
		if isLoopback(t.Host) {
			local = true
		} else {
			remote = true
		}
		if t.TLSConfig == nil || t.TLSConfig.InsecureSkipVerify || t.TLSConfig.ServerName == "" {
			unverified = true
		}
	}
	switch {
	case unverified && env.deployed():
		l.fail("%s must use sslmode=verify-full with no plaintext or unverified fallback in %s", name, env)
	case unverified && remote:
		l.fail("%s must use sslmode=verify-full for a non-local database", name)
	}
	if local && env.deployed() {
		l.fail("%s must not target a local database in %s", name, env)
	}
	return Secret{value: raw}
}

// origin validates an exact browser origin: scheme://host[:port], no wildcard,
// path, query or fragment. Deployed environments require public HTTPS origins.
func (l *loader) origin(name string, env Env) string {
	raw := l.get(name)
	if raw == "" {
		l.fail("%s is required", name)
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		strings.Contains(u.Host, "*") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		l.fail("%s must be an exact origin such as https://example.com", name)
		return ""
	}
	if env.deployed() && (u.Scheme != "https" || isLoopback(u.Hostname())) {
		l.fail("%s must be a public https origin in %s", name, env)
		return ""
	}
	return raw
}

var publishableKey = regexp.MustCompile(`^sb_publishable_[A-Za-z0-9_-]{16,}$`)

// auth validates the identity provider settings: an exact https issuer
// ending in /auth/v1 (http only for a loopback provider outside deployed
// environments) and a publishable key, never a secret one.
func (l *loader) auth(env Env) Auth {
	a := Auth{Issuer: l.get("AUTH_ISSUER"), PublishableKey: l.get("AUTH_PUBLISHABLE_KEY")}
	switch {
	case a.Issuer == "" && a.PublishableKey == "":
		if env.deployed() {
			l.fail("AUTH_ISSUER and AUTH_PUBLISHABLE_KEY are required in %s", env)
		}
		return Auth{}
	case a.Issuer == "" || a.PublishableKey == "":
		l.fail("AUTH_ISSUER and AUTH_PUBLISHABLE_KEY must be set together")
		return Auth{}
	}
	u, err := url.Parse(a.Issuer)
	switch {
	case err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		u.Path != "/auth/v1" || (u.Scheme != "https" && u.Scheme != "http"):
		l.fail("AUTH_ISSUER must be the provider's auth URL, such as https://<project>.supabase.co/auth/v1")
		return Auth{}
	case env.deployed() && (u.Scheme != "https" || isLoopback(u.Hostname())):
		l.fail("AUTH_ISSUER must be a public https URL in %s", env)
		return Auth{}
	case u.Scheme != "https" && !isLoopback(u.Hostname()):
		l.fail("AUTH_ISSUER must use https for a non-local provider")
		return Auth{}
	}
	if !publishableKey.MatchString(a.PublishableKey) {
		l.fail("AUTH_PUBLISHABLE_KEY must be the provider's publishable key (sb_publishable_...), never a secret key")
		return Auth{}
	}
	return a
}

func (l *loader) optionalSecret(name string) Secret { return Secret{value: l.get(name)} }

// webhookSecret checks a Svix signing secret: "whsec_" and at least 16
// base64-encoded bytes.
func (l *loader) webhookSecret(name string) Secret {
	s := l.optionalSecret(name)
	if !s.IsSet() {
		return s
	}
	encoded, ok := strings.CutPrefix(s.value, "whsec_")
	if raw, err := base64.StdEncoding.DecodeString(encoded); !ok || err != nil || len(raw) < 16 {
		l.fail("%s must be a webhook signing secret (whsec_ followed by base64)", name)
	}
	return s
}

// paymentSecret enforces Stripe key mode: live keys only in production, and
// production only with live keys. Prefixes per Stripe API key conventions.
func (l *loader) paymentSecret(name string, env Env) Secret {
	s := l.optionalSecret(name)
	if !s.IsSet() {
		return s
	}
	live := strings.HasPrefix(s.value, "sk_live_") || strings.HasPrefix(s.value, "rk_live_")
	test := strings.HasPrefix(s.value, "sk_test_") || strings.HasPrefix(s.value, "rk_test_")
	switch {
	case !live && !test:
		l.fail("%s must be a Stripe secret or restricted key", name)
	case env == EnvProduction && !live:
		l.fail("%s must be a live-mode key in production", name)
	case env != EnvProduction && live:
		l.fail("%s must not be a live-mode key outside production", name)
	}
	return s
}

// isLoopback reports local targets: loopback or unspecified addresses,
// localhost names and Unix sockets.
func isLoopback(host string) bool {
	if host == "" || strings.HasPrefix(host, "/") || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
