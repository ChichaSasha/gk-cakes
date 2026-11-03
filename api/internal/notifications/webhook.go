package notifications

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
)

// Webhook limits. Svix's libraries accept timestamps within five minutes;
// replays inside that window are absorbed by the svix-id deduplication.
const (
	WebhookTolerance = 5 * time.Minute
	MaxWebhookBytes  = 256 << 10
)

// Webhook errors, mapped by the HTTP layer: an invalid signature is
// unauthorized, a verified but unusable event is malformed.
var (
	ErrInvalidSignature = errors.New("notifications: webhook signature invalid")
	ErrMalformedEvent   = errors.New("notifications: malformed webhook event")
)

// Webhook results returned by Receive.
const (
	WebhookRecorded  = "recorded"
	WebhookDuplicate = "duplicate"
	WebhookIgnored   = "ignored"
)

// recordedEvents are the email events kept as evidence. Opens and clicks are
// never recorded (no read tracking); other types are acknowledged and ignored.
var recordedEvents = map[string]bool{
	"email.sent": true, "email.delivered": true, "email.delivery_delayed": true, "email.complained": true,
	"email.bounced": true, "email.failed": true, "email.suppressed": true,
}

// Webhook verifies and records Resend (Svix-signed) webhook deliveries.
type Webhook struct {
	db     Querier // gk_api pool
	secret []byte
	env    string
	now    func() time.Time
	log    *slog.Logger
}

// WebhookConfig configures a Webhook.
type WebhookConfig struct {
	DB          Querier
	Secret      string // whsec_<base64>; never logged
	Environment string // APP_ENV; events tagged with another environment are ignored
	Logger      *slog.Logger
	Now         func() time.Time
}

// NewWebhook returns a Webhook.
func NewWebhook(cfg WebhookConfig) (*Webhook, error) {
	key, err := ParseWebhookSecret(cfg.Secret)
	if err != nil {
		return nil, err
	}
	switch {
	case cfg.DB == nil || cfg.Logger == nil:
		return nil, errors.New("notifications: webhook needs a database and a logger")
	case !envPattern.MatchString(cfg.Environment):
		return nil, errors.New("notifications: webhook environment must be a lower-case name")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Webhook{db: cfg.DB, secret: key, env: cfg.Environment, now: cfg.Now, log: cfg.Logger}, nil
}

// ParseWebhookSecret decodes a Svix signing secret ("whsec_" + base64).
// Errors never include the secret.
func ParseWebhookSecret(secret string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(secret, "whsec_")
	if !ok {
		return nil, errors.New("notifications: webhook secret must start with whsec_")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) < 16 {
		return nil, errors.New("notifications: webhook secret must be whsec_ followed by at least 16 base64-encoded bytes")
	}
	return key, nil
}

var svixIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// header returns a Svix header, accepting the white-labelled webhook- prefix.
func header(h http.Header, name string) string {
	if v := h.Get("svix-" + name); v != "" {
		return v
	}
	return h.Get("webhook-" + name)
}

// Verify checks the Svix signature over the raw body and the timestamp
// tolerance, and returns the svix-id.
func (w *Webhook) Verify(h http.Header, body []byte) (string, error) {
	id, ts, sigs := header(h, "id"), header(h, "timestamp"), header(h, "signature")
	if !svixIDPattern.MatchString(id) || ts == "" || sigs == "" {
		return "", fmt.Errorf("%w: missing or malformed signature headers", ErrInvalidSignature)
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", fmt.Errorf("%w: malformed timestamp", ErrInvalidSignature)
	}
	if d := w.now().Sub(time.Unix(sec, 0)); d > WebhookTolerance || d < -WebhookTolerance {
		return "", fmt.Errorf("%w: timestamp outside the tolerance", ErrInvalidSignature)
	}
	mac := hmac.New(sha256.New, w.secret)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, sig := range strings.Fields(sigs) {
		version, value, ok := strings.Cut(sig, ",")
		if !ok || version != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(value)
		if err == nil && hmac.Equal(got, want) {
			return id, nil
		}
	}
	return "", fmt.Errorf("%w: no matching signature", ErrInvalidSignature)
}

// Event is the minimized evidence extracted from a provider event.
type Event struct {
	Type              string
	CreatedAt         *time.Time
	ProviderMessageID string
	IntentTag         string
	Env               string
	Detail            map[string]string // categories only, never addresses or messages
}

var (
	eventTypePattern = regexp.MustCompile(`^[a-z0-9_]{1,30}(\.[a-z0-9_]{1,30}){1,4}$`)
	categoryPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// ParseEvent extracts the evidence the inbox keeps from a verified body. It is
// lenient beyond the event type: a field with an unexpected shape or value is
// dropped rather than failing the delivery (a signed event that can never be
// accepted would exhaust the provider's retries and disable the endpoint).
// Only a body that is not a JSON object with a plausible type is malformed.
func ParseEvent(body []byte) (Event, error) {
	var top struct {
		Type      string          `json:"type"`
		CreatedAt json.RawMessage `json:"created_at"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &top); err != nil || !eventTypePattern.MatchString(top.Type) {
		return Event{}, ErrMalformedEvent
	}
	ev := Event{Type: top.Type, Detail: map[string]string{}}
	if t, err := time.Parse(time.RFC3339Nano, jsonString(top.CreatedAt)); err == nil {
		t = t.UTC()
		ev.CreatedAt = &t
	}
	data := jsonObject(top.Data)
	if id := jsonString(data["email_id"]); messageIDPattern.MatchString(id) {
		ev.ProviderMessageID = id
	}
	tags := eventTags(data["tags"])
	if v := tags[intentTagName]; uuidPattern.MatchString(v) {
		ev.IntentTag = v
	}
	if v := tags[envTagName]; envPattern.MatchString(v) {
		ev.Env = v
	}
	category := func(key string, raw json.RawMessage) {
		if v := jsonString(raw); categoryPattern.MatchString(v) {
			ev.Detail[key] = v
		}
	}
	if b := jsonObject(data["bounce"]); b != nil {
		category("bounce_type", b["type"])
		category("bounce_sub_type", b["subType"])
	}
	if f := jsonObject(data["failed"]); f != nil {
		category("failed_reason", f["reason"])
	}
	if sup := jsonObject(data["suppressed"]); sup != nil {
		category("suppressed_type", sup["type"])
	}
	return ev, nil
}

func jsonString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

func jsonObject(raw json.RawMessage) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// eventTags reads tags as Resend sends them in events (an object), or as a
// list of name/value pairs.
func eventTags(raw json.RawMessage) map[string]string {
	tags := map[string]string{}
	if len(raw) == 0 {
		return tags
	}
	if obj := jsonObject(raw); obj != nil {
		for k, v := range obj {
			if s := jsonString(v); s != "" {
				tags[k] = s
			}
		}
		return tags
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, item := range list {
			t := jsonObject(item)
			if name, value := jsonString(t["name"]), jsonString(t["value"]); name != "" && value != "" {
				tags[name] = value
			}
		}
	}
	return tags
}

// Receive verifies, parses and records one delivery, returning
// WebhookRecorded, WebhookDuplicate or WebhookIgnored. It returns only after
// the event is committed, so a successful response means it is stored.
func (w *Webhook) Receive(ctx context.Context, h http.Header, body []byte) (string, error) {
	id, err := w.Verify(h, body)
	if err != nil {
		w.log.Warn("email webhook refused", "reason", strings.TrimPrefix(err.Error(), ErrInvalidSignature.Error()+": "))
		return "", err
	}
	ev, err := ParseEvent(body)
	if err != nil {
		w.log.Error("email webhook event malformed", "svix_id", id)
		return "", err
	}
	log := w.log.With("svix_id", id, "event_type", ev.Type)
	if !recordedEvents[ev.Type] || ev.Env != w.env {
		if recordedEvents[ev.Type] && ev.Env == "" && ev.IntentTag != "" {
			// One of ours without its environment tag: should never happen.
			log.Warn("email webhook event for an intent without an environment tag ignored", "intent_tag", ev.IntentTag)
		} else {
			log.Info("email webhook event ignored", "event_env", ev.Env)
		}
		return WebhookIgnored, nil
	}
	var detail *string
	if len(ev.Detail) > 0 {
		b, err := json.Marshal(ev.Detail)
		if err != nil {
			return "", err
		}
		s := string(b)
		detail = &s
	}
	sum := sha256.Sum256(body)
	var (
		result string
		intent *string
		state  *string
	)
	err = w.db.QueryRow(ctx, `SELECT result, event_intent_id::text, event_intent_state
		FROM business.email_event_record($1, $2, $3, $4, $5::uuid, $6::json, $7)`,
		id, ev.Type, ev.CreatedAt, nullString(ev.ProviderMessageID), nullString(ev.IntentTag), detail, hex.EncodeToString(sum[:]),
	).Scan(&result, &intent, &state)
	if err != nil {
		log.Error("email webhook event not stored", "error", err)
		return "", fmt.Errorf("notifications: store webhook event: %w", err)
	}
	log.Info("email webhook event", "result", result, "intent_id", deref(intent), "intent_state", deref(state))
	return result, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
