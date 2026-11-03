package notifications

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type nopQuerier struct{}

func (nopQuerier) QueryRow(context.Context, string, ...any) pgx.Row { panic("database reached") }

func testWebhook(t *testing.T, secret string, now time.Time) *Webhook {
	t.Helper()
	w, err := NewWebhook(WebhookConfig{DB: nopQuerier{}, Secret: secret, Environment: "test",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func svixHeaders(id, ts, sig string) http.Header {
	h := http.Header{}
	h.Set("svix-id", id)
	h.Set("svix-timestamp", ts)
	h.Set("svix-signature", sig)
	return h
}

// TestVerifyMatchesTheSvixReferenceVector uses the example from Svix's
// manual-verification documentation.
func TestVerifyMatchesTheSvixReferenceVector(t *testing.T) {
	const (
		secret  = "whsec_plJ3nmyCDGBKInavdOK15jsl"
		payload = `{"event_type":"ping","data":{"success":true}}`
		id      = "msg_loFOjxBNrRLzqYUf"
		ts      = "1731705121"
		sig     = "v1,rAvfW3dJ/X/qxhsaXPOyyCGmRKsaKWcsNccKXlIktD0="
	)
	w := testWebhook(t, secret, time.Unix(1731705121, 0).Add(time.Minute))
	if got, err := w.Verify(svixHeaders(id, ts, sig), []byte(payload)); err != nil || got != id {
		t.Fatalf("Verify(reference vector) = %q, %v", got, err)
	}
	// Several signatures (a secret rotation): any v1 match is enough.
	if _, err := w.Verify(svixHeaders(id, ts, "v1,AAAA v2,BBBB "+sig), []byte(payload)); err != nil {
		t.Fatalf("Verify with several signatures: %v", err)
	}
	// White-labelled webhook-* headers.
	h := http.Header{}
	h.Set("webhook-id", id)
	h.Set("webhook-timestamp", ts)
	h.Set("webhook-signature", sig)
	if _, err := w.Verify(h, []byte(payload)); err != nil {
		t.Fatalf("Verify with webhook-* headers: %v", err)
	}

	refused := map[string]struct {
		h    http.Header
		body string
		now  time.Time
	}{
		"tampered body":           {svixHeaders(id, ts, sig), `{"event_type":"ping","data":{"success":false}}`, time.Unix(1731705121, 0)},
		"another message id":      {svixHeaders("msg_other", ts, sig), payload, time.Unix(1731705121, 0)},
		"timestamp too old":       {svixHeaders(id, ts, sig), payload, time.Unix(1731705121, 0).Add(WebhookTolerance + time.Second)},
		"timestamp in the future": {svixHeaders(id, ts, sig), payload, time.Unix(1731705121, 0).Add(-WebhookTolerance - time.Second)},
		"only a v2 signature":     {svixHeaders(id, ts, strings.Replace(sig, "v1,", "v2,", 1)), payload, time.Unix(1731705121, 0)},
		"no signature":            {svixHeaders(id, ts, ""), payload, time.Unix(1731705121, 0)},
		"no id":                   {svixHeaders("", ts, sig), payload, time.Unix(1731705121, 0)},
		"malformed id":            {svixHeaders("msg/../x", ts, sig), payload, time.Unix(1731705121, 0)},
		"malformed timestamp":     {svixHeaders(id, "soon", sig), payload, time.Unix(1731705121, 0)},
		"malformed signature":     {svixHeaders(id, ts, "v1,!!!"), payload, time.Unix(1731705121, 0)},
	}
	for name, tc := range refused {
		w := testWebhook(t, secret, tc.now)
		if _, err := w.Verify(tc.h, []byte(tc.body)); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("%s: err = %v, want ErrInvalidSignature", name, err)
		}
	}
	other := testWebhook(t, "whsec_"+base64.StdEncoding.EncodeToString([]byte("another-secret-key!")), time.Unix(1731705121, 0))
	if _, err := other.Verify(svixHeaders(id, ts, sig), []byte(payload)); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("another secret: err = %v, want ErrInvalidSignature", err)
	}
}

func TestParseWebhookSecretNeverEchoesIt(t *testing.T) {
	for _, bad := range []string{"", "plJ3nmyCDGBKInavdOK15jsl", "whsec_not base64!", "whsec_" + base64.StdEncoding.EncodeToString([]byte("short"))} {
		_, err := ParseWebhookSecret(bad)
		if err == nil || (bad != "" && strings.Contains(err.Error(), bad)) {
			t.Errorf("ParseWebhookSecret(%q) = %v", bad, err)
		}
	}
}

func TestParseEventKeepsOnlyMinimizedEvidence(t *testing.T) {
	body := `{"type":"email.bounced","created_at":"2026-11-22T23:41:12.126Z","data":{
		"created_at":"2026-11-22T23:41:11.894Z","email_id":"56761188-7520-42d8-8898-ff6fc54ce618",
		"from":"GK Cakes <orders@gkcake.com>","to":["ms.example@example.org"],"subject":"Your order",
		"bounce":{"diagnosticCode":["smtp; 550 5.1.1 ms.example@example.org unknown"],"message":"ms.example@example.org does not exist",
		"subType":"MessageRejected","type":"Permanent"},
		"tags":{"intent":"11111111-1111-4111-8111-111111111111","env":"test"}}}`
	ev, err := ParseEvent([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "email.bounced" || ev.ProviderMessageID != "56761188-7520-42d8-8898-ff6fc54ce618" ||
		ev.IntentTag != "11111111-1111-4111-8111-111111111111" || ev.Env != "test" || ev.CreatedAt == nil ||
		!ev.CreatedAt.Equal(time.Date(2026, 11, 22, 23, 41, 12, 126e6, time.UTC)) {
		t.Fatalf("event = %+v", ev)
	}
	if len(ev.Detail) != 2 || ev.Detail["bounce_type"] != "Permanent" || ev.Detail["bounce_sub_type"] != "MessageRejected" {
		t.Fatalf("detail = %v, want only the bounce categories", ev.Detail)
	}
	list, err := ParseEvent([]byte(`{"type":"email.failed","data":{"email_id":"m1","failed":{"reason":"reached_daily_quota"},
		"tags":[{"name":"intent","value":"11111111-1111-4111-8111-111111111111"},{"name":"env","value":"test"}]}}`))
	if err != nil || list.IntentTag == "" || list.Env != "test" || list.Detail["failed_reason"] != "reached_daily_quota" || list.CreatedAt != nil {
		t.Fatalf("event with tag list = %+v, %v", list, err)
	}
	odd, err := ParseEvent([]byte(`{"type":"email.suppressed","data":{"suppressed":{"type":"On account list; x@example.org"},"tags":{"intent":"not-a-uuid","env":"Prod/../x"}}}`))
	if err != nil || len(odd.Detail) != 0 || odd.IntentTag != "" || odd.Env != "" {
		t.Fatalf("unsafe values were kept: %+v, %v", odd, err)
	}
	for _, bad := range []string{`{`, `[]`, `{"type":""}`, `{"type":"Email Bounced"}`, `{"type":7}`, `{"type":"email"}`} {
		if _, err := ParseEvent([]byte(bad)); !errors.Is(err, ErrMalformedEvent) {
			t.Errorf("ParseEvent(%s) = %v, want ErrMalformedEvent", bad, err)
		}
	}
	// Beyond the type, unexpected shapes are dropped, never fatal: a signed
	// delivery that can never succeed would exhaust the provider's retries.
	for _, odd := range []string{
		`{"type":"email.sent","data":{"email_id":"bad id@example.org"}}`,
		`{"type":"email.bounced","data":{"bounce":"Permanent","tags":"intent"}}`,
		`{"type":"email.failed","created_at":12,"data":{"failed":true,"tags":[{"name":1},{"value":"x"}]}}`,
		`{"type":"email.v2.sent","data":[]}`,
		`{"type":"contact.updated","data":{"failed":true}}`,
	} {
		ev, err := ParseEvent([]byte(odd))
		if err != nil || ev.ProviderMessageID != "" || ev.IntentTag != "" || ev.Env != "" || len(ev.Detail) != 0 || ev.CreatedAt != nil {
			t.Errorf("ParseEvent(%s) = %+v, %v; want the type only", odd, ev, err)
		}
	}
}

// sign returns Svix headers for body signed with secret at now.
func sign(t *testing.T, secret, id string, now time.Time, body []byte) http.Header {
	t.Helper()
	key, err := ParseWebhookSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	return svixHeaders(id, ts, "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

func TestReceiveIgnoresOtherEnvironmentsAndEventTypesWithoutStoring(t *testing.T) {
	const secret = "whsec_plJ3nmyCDGBKInavdOK15jsl"
	now := time.Now()
	w := testWebhook(t, secret, now) // the database panics if reached
	for name, body := range map[string]string{
		"another environment": `{"type":"email.delivered","data":{"email_id":"m1","tags":{"intent":"11111111-1111-4111-8111-111111111111","env":"production"}}}`,
		"no environment tag":  `{"type":"email.delivered","data":{"email_id":"m1"}}`,
		"an open":             `{"type":"email.opened","data":{"email_id":"m1","tags":{"env":"test"}}}`,
		"a click":             `{"type":"email.clicked","data":{"email_id":"m1","tags":{"env":"test"}}}`,
		"a domain event":      `{"type":"domain.updated","data":{"id":"d1"}}`,
		"another environment with an unexpected shape": `{"type":"email.bounced","data":{"email_id":"x@y","bounce":"odd","tags":{"env":"production"}}}`,
		"a newer event type":                           `{"type":"email.v2.sent","data":{"tags":{"env":"test"}}}`,
		"a contact event":                              `{"type":"contact.updated","data":{"failed":true}}`,
	} {
		got, err := w.Receive(context.Background(), sign(t, secret, "msg_"+strings.ReplaceAll(name, " ", "_"), now, []byte(body)), []byte(body))
		if err != nil || got != WebhookIgnored {
			t.Errorf("%s: %q, %v; want ignored", name, got, err)
		}
	}
}
