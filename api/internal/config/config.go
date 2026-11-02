// Package config loads and validates process configuration from the
// environment. Validation fails closed and error messages name variables only;
// secret values are never included in errors or formatted output.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
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
}

// Worker is the configuration of the maintenance/job process. It uses its own
// DATABASE_URL value, provisioned with the worker credential.
type Worker struct {
	Env             Env
	DatabaseURL     Secret
	ShutdownTimeout time.Duration
}

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
		EmailWebhookSecret:   l.optionalSecret("EMAIL_WEBHOOK_SECRET"),
		StorageWriteKey:      l.optionalSecret("STORAGE_WRITE_KEY"),
	}
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
	return cfg, l.err()
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

// databaseURL validates a PostgreSQL URL without ever echoing it: url.Parse
// errors embed the raw input, so they are replaced with a fixed message.
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
	if env.deployed() {
		if u.Query().Get("sslmode") != "verify-full" {
			l.fail("%s must set sslmode=verify-full in %s", name, env)
		}
		if isLoopback(u.Hostname()) {
			l.fail("%s must not target a local database in %s", name, env)
		}
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

func (l *loader) optionalSecret(name string) Secret { return Secret{value: l.get(name)} }

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

func isLoopback(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
