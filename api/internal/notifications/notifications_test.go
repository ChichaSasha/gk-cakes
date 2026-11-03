package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
)

const (
	testTransition = "11111111-1111-4111-8111-111111111111"
	testRecipient  = "22222222-2222-4222-8222-222222222222"
)

func validMessage() Message {
	return Message{To: "ms.example@example.org", Subject: "Your order is confirmed", Text: "Thank you — £42.50 paid."}
}

func TestNewNoticeFreezesTheApprovedRequest(t *testing.T) {
	n, err := NewNotice("staging", testTransition, testRecipient, "order-confirmed.v1", validMessage())
	if err != nil {
		t.Fatal(err)
	}
	if !uuidPattern.MatchString(n.IntentID) || !uuidPattern.MatchString(n.IdempotencyKey) || n.IntentID == n.IdempotencyKey {
		t.Fatalf("identifiers %q / %q, want two distinct UUIDs", n.IntentID, n.IdempotencyKey)
	}
	d, err := ParseDispatch(n.Payload, "staging", n.IntentID, n.IdempotencyKey)
	if err != nil {
		t.Fatalf("ParseDispatch of a fresh notice: %v", err)
	}
	var req Request
	if err := json.Unmarshal(d.Request, &req); err != nil {
		t.Fatal(err)
	}
	want := Request{From: From, To: []string{"ms.example@example.org"}, ReplyTo: []string{ReplyTo}, Subject: "Your order is confirmed",
		Text: "Thank you — £42.50 paid.", Tags: []Tag{{Name: "intent", Value: n.IntentID}, {Name: "env", Value: "staging"}}}
	if req.From != want.From || req.To[0] != want.To[0] || req.ReplyTo[0] != want.ReplyTo[0] || req.Subject != want.Subject ||
		req.Text != want.Text || len(req.Tags) != 2 || req.Tags[0] != want.Tags[0] || req.Tags[1] != want.Tags[1] || req.HTML != "" {
		t.Fatalf("request = %+v, want %+v", req, want)
	}
	if !bytes.Contains(n.Payload, d.Request) {
		t.Fatal("the dispatched request is not the exact stored bytes")
	}
	again, _ := ParseDispatch(n.Payload, "staging", n.IntentID, n.IdempotencyKey)
	if !bytes.Equal(again.Request, d.Request) {
		t.Fatal("parsing the same payload twice gave different request bytes")
	}
}

func TestNewNoticeRefusesUnsafeMessages(t *testing.T) {
	long := strings.Repeat("x", MaxSubjectRunes+1)
	cases := map[string]Message{
		"no recipient":          {Subject: "s", Text: "t"},
		"display name":          {To: "Ms Example <ms@example.org>", Subject: "s", Text: "t"},
		"two recipients":        {To: "a@example.org, b@example.org", Subject: "s", Text: "t"},
		"header injection":      {To: "a@example.org\r\nBcc: x@example.org", Subject: "s", Text: "t"},
		"undotted domain":       {To: "a@localhost", Subject: "s", Text: "t"},
		"subject CR":            {To: "a@example.org", Subject: "a\rb", Text: "t"},
		"subject LF":            {To: "a@example.org", Subject: "a\nBcc: x@example.org", Text: "t"},
		"long subject":          {To: "a@example.org", Subject: long, Text: "t"},
		"blank subject":         {To: "a@example.org", Subject: "  ", Text: "t"},
		"blank body":            {To: "a@example.org", Subject: "s", Text: " "},
		"huge body":             {To: "a@example.org", Subject: "s", Text: strings.Repeat("x", MaxTextBytes+1)},
		"huge HTML":             {To: "a@example.org", Subject: "s", Text: "t", HTML: strings.Repeat("x", MaxHTMLBytes+1)},
		"NUL in body":           {To: "a@example.org", Subject: "s", Text: "a\x00b"},
		"invalid UTF-8 subject": {To: "a@example.org", Subject: "\xff", Text: "t"},
	}
	for name, msg := range cases {
		if _, err := NewNotice("test", testTransition, testRecipient, "t.v1", msg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewNotice("test", testTransition, testRecipient, "Bad Revision", validMessage()); err == nil {
		t.Error("invalid template revision accepted")
	}
	if _, err := NewNotice("test", "not-a-uuid", testRecipient, "t.v1", validMessage()); err == nil {
		t.Error("invalid transition ID accepted")
	}
}

func TestParseDispatchRefusesForeignPayloads(t *testing.T) {
	n, err := NewNotice("staging", testTransition, testRecipient, "t.v1", validMessage())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseDispatch(n.Payload, "production", n.IntentID, n.IdempotencyKey); err == nil {
		t.Error("payload of another environment accepted")
	}
	if _, err := ParseDispatch(n.Payload, "staging", n.IntentID, testTransition); err == nil {
		t.Error("payload with another idempotency key accepted")
	}
	if _, err := ParseDispatch(n.Payload, "staging", testTransition, n.IdempotencyKey); err == nil {
		t.Error("payload tagged with another intent accepted")
	}
	tamper := func(old, repl string) []byte { return bytes.Replace(n.Payload, []byte(old), []byte(repl), 1) }
	for name, p := range map[string][]byte{
		"another sender":   tamper(From, "GK Cakes <orders@evil.example>"),
		"another reply-to": tamper(ReplyTo, "someone@evil.example"),
		"extra recipient":  tamper(`"to":["ms.example@example.org"]`, `"to":["ms.example@example.org","x@evil.example"]`),
		"unknown field":    tamper(`"subject":`, `"bcc":["x@evil.example"],"subject":`),
		"case-variant key": tamper(`"from":"`+From+`"`, `"from":"Evil <e@evil.example>","FROM":"`+From+`"`),
		"duplicate key":    tamper(`"from":"`+From+`"`, `"from":"Evil <e@evil.example>","from":"`+From+`"`),
		"not canonical":    tamper(`"subject":`, `"subject" :`),
		"another provider": tamper(`"provider":"resend"`, `"provider":"other"`),
		"not JSON":         []byte("{"),
	} {
		if _, err := ParseDispatch(p, "staging", n.IntentID, n.IdempotencyKey); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestClassifyFollowsTheResendErrorReference(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   ResultKind
		code   string
	}{
		{200, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`, Accepted, ""},
		{200, `{}`, Unknown, ""},
		{200, `{"id":"bad id with spaces"}`, Unknown, ""},
		{200, `not json`, Unknown, ""},
		{400, `{"name":"validation_error","message":"x@example.org is invalid"}`, Rejected, "validation_error"},
		{400, `{"name":"invalid_idempotency_key"}`, Refused, "invalid_idempotency_key"},
		{422, `{"name":"missing_required_field"}`, Rejected, "missing_required_field"},
		{401, `{"name":"missing_api_key"}`, Refused, "missing_api_key"},
		{403, `{"name":"validation_error","message":"The gkcake.com domain is not verified"}`, Refused, "validation_error"},
		{404, `{"name":"not_found"}`, Refused, "not_found"},
		{405, `{"name":"method_not_allowed"}`, Refused, "method_not_allowed"},
		{409, `{"name":"invalid_idempotent_request"}`, Conflict, "invalid_idempotent_request"},
		{409, `{"name":"concurrent_idempotent_requests"}`, Deferred, "concurrent_idempotent_requests"},
		{429, `{"name":"daily_quota_exceeded"}`, Deferred, "daily_quota_exceeded"},
		{500, `{"name":"application_error"}`, Unknown, "application_error"},
		{503, `<html>unavailable</html>`, Unknown, ""},
		{418, ``, Unknown, ""},
		{422, `{"name":"Not A Code; x@example.org"}`, Refused, ""},
		{422, `{"name":"invalid_parameter"}`, Rejected, "invalid_parameter"},
		{400, `{"statusCode":400,"name":"validation_error","message":"API key is invalid"}`, Refused, "validation_error"},
		{400, ``, Refused, ""},
		{400, `{"name":"unexpected_error"}`, Refused, "unexpected_error"},
	}
	for _, tc := range cases {
		got := Classify(tc.status, []byte(tc.body))
		if got.Kind != tc.want || got.Code != tc.code || got.Status != tc.status {
			t.Errorf("Classify(%d, %s) = %+v, want %s with code %q", tc.status, tc.body, got, tc.want, tc.code)
		}
	}
}

func TestResendSendsTheExactBodyWithTheKey(t *testing.T) {
	body := []byte(`{"from":"GK Cakes <orders@gkcake.com>","to":["a@example.org"]}`)
	var got struct {
		method, path, auth, key, ctype string
		body                           []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		got.key, got.ctype = r.Header.Get("Idempotency-Key"), r.Header.Get("Content-Type")
		got.body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"re-1"}`))
	}))
	defer srv.Close()
	c, err := NewResend(ResendConfig{APIKey: "re_secret", BaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	res := c.Send(context.Background(), "key-1", body)
	if res.Kind != Accepted || res.MessageID != "re-1" {
		t.Fatalf("Send = %+v", res)
	}
	if got.method != http.MethodPost || got.path != "/emails" || got.auth != "Bearer re_secret" || got.key != "key-1" ||
		got.ctype != "application/json" || !bytes.Equal(got.body, body) {
		t.Fatalf("request = %+v", got)
	}
}

func TestResendTimeoutIsUnknown(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c, err := NewResend(ResendConfig{APIKey: "re_secret", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res := c.Send(ctx, "k", []byte(`{}`))
	if res.Kind != Unknown || res.Err == nil || !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Fatalf("Send after timeout = %+v, want unknown with the deadline", res)
	}
	if strings.Contains(res.Err.Error(), "re_secret") {
		t.Fatal("error leaks the API key")
	}
}

func TestNewResendRequiresAResendKey(t *testing.T) {
	if _, err := NewResend(ResendConfig{APIKey: "sk_live_x"}); err == nil {
		t.Fatal("non-Resend key accepted")
	}
}

func TestOutcomeErrorMapsToJobDecisions(t *testing.T) {
	for kind, attention := range map[ResultKind]bool{Refused: true, Conflict: true, Unknown: false, Deferred: false} {
		err := outcomeError(fmt.Errorf("recovery: effect outcome unknown: %w", &callError{res: Result{Kind: kind, Status: 500}}))
		var ae *jobs.AttentionError
		if errors.As(err, &ae) != attention {
			t.Errorf("%s: %v, want attention %v", kind, err, attention)
		}
	}
	var (
		ae *jobs.AttentionError
		de *jobs.DeferError
	)
	if !errors.As(outcomeError(recovery.ErrNeedsReconcile), &ae) || !strings.Contains(ae.Reason, "reconciliation") {
		t.Error("an expired unknown outcome must go to manager reconciliation")
	}
	if !errors.As(outcomeError(recovery.ErrFrozen), &de) || de.After != FreezeRecheck {
		t.Error("a freeze must defer without a failure")
	}
	if outcomeError(nil) != nil {
		t.Error("success must complete the job")
	}
	for _, reason := range []string{reasonSuspended, reasonPayload, reasonReconcile, reasonRecovery, reasonRefused, reasonConflict, reasonRejected} {
		if len([]rune(reason)) > 200 {
			t.Errorf("attention reason longer than the database limit: %q", reason)
		}
	}
}
