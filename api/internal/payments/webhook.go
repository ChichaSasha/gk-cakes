package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Webhook limits: Stripe's libraries accept signatures within five minutes;
// a replay inside that window is absorbed by the event ID deduplication.
const (
	WebhookTolerance = 5 * time.Minute
	MaxWebhookBytes  = 256 << 10
	SignatureHeader  = "Stripe-Signature"
)

// Webhook errors, mapped by the HTTP layer to 400.
var (
	ErrInvalidSignature = errors.New("payments: webhook signature invalid")
	ErrMalformedEvent   = errors.New("payments: malformed webhook event")
)

// Webhook results returned by Receive.
const (
	WebhookRecorded  = "recorded"
	WebhookDuplicate = "duplicate"
)

// Querier is satisfied by *pgxpool.Pool and *pgx.Conn. Each gateway call
// commits on its own: no transaction is held across provider calls.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var webhookSecretPattern = regexp.MustCompile(`^whsec_[A-Za-z0-9+/=_-]{16,}$`)

// ValidWebhookSecret reports whether s looks like a Stripe signing secret.
func ValidWebhookSecret(s string) bool { return webhookSecretPattern.MatchString(s) }

// VerifySignature checks a Stripe-Signature header ("t=<unix>,v1=<hex>[,...]")
// over the raw body: one of the v1 signatures must be the HMAC-SHA256 of
// "<t>.<body>" keyed with the whole signing secret (compared in constant
// time), and t must be within WebhookTolerance of now in either direction.
func VerifySignature(header string, body []byte, secret string, now time.Time) error {
	if header == "" || len(header) > 4096 {
		return fmt.Errorf("%w: missing or malformed signature header", ErrInvalidSignature)
	}
	var (
		ts   string
		sigs [][]byte
	)
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			if b, err := hex.DecodeString(v); err == nil && len(b) == sha256.Size {
				sigs = append(sigs, b)
			}
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return fmt.Errorf("%w: missing or malformed signature header", ErrInvalidSignature)
	}
	if d := now.Sub(time.Unix(sec, 0)); d > WebhookTolerance || d < -WebhookTolerance {
		return fmt.Errorf("%w: timestamp outside the tolerance", ErrInvalidSignature)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, sig := range sigs {
		if hmac.Equal(sig, want) {
			return nil
		}
	}
	return fmt.Errorf("%w: no matching signature", ErrInvalidSignature)
}

// Event is the minimized evidence extracted from a verified event.
type Event struct {
	ID                 string
	Type               string
	Account            string // empty: the endpoint's own account
	Livemode           bool
	CreatedAt          *time.Time
	ObjectType         string
	ObjectID           string
	MetadataCollection string
	MetadataEnv        string
	Dropped            []string // names of present but unusable fields
}

var (
	eventIDPattern   = regexp.MustCompile(`^evt_[A-Za-z0-9_]{1,250}$`)
	eventTypePattern = regexp.MustCompile(`^[a-z0-9_]{1,40}(\.[a-z0-9_]{1,40}){1,4}$`)
	objectTypePat    = regexp.MustCompile(`^[a-z0-9_.]{1,60}$`)
)

// ParseEvent extracts what the inbox keeps. Beyond the event ID, type and
// mode it is lenient: an unusable field is dropped rather than failing the
// delivery (a signed event that can never be accepted would exhaust
// Stripe's retries and disable the endpoint).
func ParseEvent(body []byte) (Event, error) {
	var top struct {
		ID       string          `json:"id"`
		Object   string          `json:"object"`
		Type     string          `json:"type"`
		Account  json.RawMessage `json:"account"`
		Livemode *bool           `json:"livemode"`
		Created  json.RawMessage `json:"created"`
		Data     struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &top); err != nil || !eventIDPattern.MatchString(top.ID) ||
		!eventTypePattern.MatchString(top.Type) || top.Livemode == nil || (top.Object != "" && top.Object != "event") {
		return Event{}, ErrMalformedEvent
	}
	ev := Event{ID: top.ID, Type: top.Type, Livemode: *top.Livemode}
	if len(top.Account) > 0 && string(top.Account) != "null" {
		var a string
		if json.Unmarshal(top.Account, &a) == nil && accountPattern.MatchString(a) {
			ev.Account = a
		} else {
			ev.Dropped = append(ev.Dropped, "account")
		}
	}
	var created int64
	if json.Unmarshal(top.Created, &created) == nil && created > 0 {
		t := time.Unix(created, 0).UTC()
		ev.CreatedAt = &t
	}
	var obj struct {
		ID       json.RawMessage `json:"id"`
		Object   json.RawMessage `json:"object"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if json.Unmarshal(top.Data.Object, &obj) != nil {
		ev.Dropped = append(ev.Dropped, "data.object")
		return ev, nil
	}
	var s string
	if json.Unmarshal(obj.ID, &s) == nil && objectIDPattern.MatchString(s) {
		ev.ObjectID = s
	} else if len(obj.ID) > 0 {
		ev.Dropped = append(ev.Dropped, "data.object.id")
	}
	if json.Unmarshal(obj.Object, &s) == nil && objectTypePat.MatchString(s) {
		ev.ObjectType = s
	}
	var md map[string]json.RawMessage
	if json.Unmarshal(obj.Metadata, &md) == nil {
		if raw, ok := md["collection_id"]; ok {
			if json.Unmarshal(raw, &s) == nil && uuidPattern.MatchString(s) {
				ev.MetadataCollection = s
			} else {
				ev.Dropped = append(ev.Dropped, "metadata.collection_id")
			}
		}
		if raw, ok := md["environment"]; ok {
			if json.Unmarshal(raw, &s) == nil && envPattern.MatchString(s) {
				ev.MetadataEnv = s
			} else {
				ev.Dropped = append(ev.Dropped, "metadata.environment")
			}
		}
	}
	return ev, nil
}

// Webhook verifies and records Stripe webhook deliveries.
type Webhook struct {
	db      Querier
	secret  string
	env     string
	account string
	now     func() time.Time
	log     *slog.Logger
}

// WebhookConfig configures a Webhook.
type WebhookConfig struct {
	DB          Querier // gk_api pool
	Secret      string  // whsec_...; never logged
	Environment string  // APP_ENV
	Account     string  // STRIPE_ACCOUNT_ID: events without an account are this one's
	Logger      *slog.Logger
	Now         func() time.Time
}

// NewWebhook returns a Webhook.
func NewWebhook(cfg WebhookConfig) (*Webhook, error) {
	switch {
	case !webhookSecretPattern.MatchString(cfg.Secret):
		return nil, errors.New("payments: the Stripe webhook secret must be whsec_...")
	case !accountPattern.MatchString(cfg.Account):
		return nil, errors.New("payments: the Stripe account ID (acct_...) is required for the webhook")
	case !envPattern.MatchString(cfg.Environment):
		return nil, errors.New("payments: webhook environment must be a lower-case name")
	case cfg.DB == nil || cfg.Logger == nil:
		return nil, errors.New("payments: webhook needs a database and a logger")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Webhook{db: cfg.DB, secret: cfg.Secret, env: cfg.Environment, account: cfg.Account, now: cfg.Now, log: cfg.Logger}, nil
}

// Receive verifies, parses and records one delivery, returning
// WebhookRecorded or WebhookDuplicate only after the event is committed (a
// success response means it is stored). Logs carry IDs and types only.
func (w *Webhook) Receive(ctx context.Context, h http.Header, body []byte) (string, error) {
	if err := VerifySignature(h.Get(SignatureHeader), body, w.secret, w.now()); err != nil {
		w.log.Warn("stripe webhook refused", "reason", strings.TrimPrefix(err.Error(), ErrInvalidSignature.Error()+": "))
		return "", err
	}
	ev, err := ParseEvent(body)
	if err != nil {
		w.log.Error("stripe webhook event malformed")
		return "", err
	}
	log := w.log.With("event_id", ev.ID, "event_type", ev.Type, "livemode", ev.Livemode)
	if len(ev.Dropped) > 0 {
		log.Warn("stripe webhook event fields dropped", "fields", strings.Join(ev.Dropped, ","))
	}
	account := ev.Account
	if account == "" {
		account = w.account
	}
	sum := sha256.Sum256(body)
	var (
		result     string
		collection *string
	)
	err = w.db.QueryRow(ctx, `SELECT result, event_collection_id::text FROM business.payment_event_record(
		$1, $2, $3, $4, $5, $6, $7, $8, $9::uuid, $10, $11)`,
		w.env, account, ev.Livemode, ev.ID, ev.Type, ev.CreatedAt, nullString(ev.ObjectType), nullString(ev.ObjectID),
		nullString(ev.MetadataCollection), nullString(ev.MetadataEnv), hex.EncodeToString(sum[:]),
	).Scan(&result, &collection)
	if err != nil {
		log.Error("stripe webhook event not stored", "error", err)
		return "", fmt.Errorf("payments: store webhook event: %w", err)
	}
	log.Info("stripe webhook event", "result", result, "collection_id", deref(collection),
		"foreign_account", account != w.account)
	return result, nil
}

func nullString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
