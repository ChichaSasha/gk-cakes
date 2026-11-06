package payments

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testCollection = "11111111-2222-4333-8444-555555555555"
	testOrder      = "66666666-7777-4888-8999-aaaaaaaaaaaa"
	testAccount    = "acct_1TestAccount"
	testSecret     = "whsec_0123456789abcdefghijklmnopqrstuv"
)

func testRequest() SessionRequest {
	return SessionRequest{CollectionID: testCollection, OrderID: testOrder, AmountMinor: 8585, Environment: "test",
		UIOrigin: "https://gk.example", ExpiresAt: time.Unix(1793827326, 0)}
}

func TestSessionFormIsExactAndDeterministic(t *testing.T) {
	form, err := testRequest().Form()
	if err != nil {
		t.Fatal(err)
	}
	want := "cancel_url=https%3A%2F%2Fgk.example%2Forders%2F" + testOrder + "%3Fpayment%3Dreturned" +
		"&client_reference_id=" + testOrder +
		"&expires_at=1793827326" +
		"&line_items%5B0%5D%5Bprice_data%5D%5Bcurrency%5D=gbp" +
		"&line_items%5B0%5D%5Bprice_data%5D%5Bproduct_data%5D%5Bname%5D=GK+Cakes+order" +
		"&line_items%5B0%5D%5Bprice_data%5D%5Bunit_amount%5D=8585" +
		"&line_items%5B0%5D%5Bquantity%5D=1" +
		"&metadata%5Bcollection_id%5D=" + testCollection +
		"&metadata%5Benvironment%5D=test" +
		"&metadata%5Border_id%5D=" + testOrder +
		"&mode=payment" +
		"&payment_intent_data%5Bmetadata%5D%5Bcollection_id%5D=" + testCollection +
		"&payment_intent_data%5Bmetadata%5D%5Benvironment%5D=test" +
		"&payment_intent_data%5Bmetadata%5D%5Border_id%5D=" + testOrder +
		"&success_url=https%3A%2F%2Fgk.example%2Forders%2F" + testOrder + "%3Fpayment%3Dreturned"
	if form != want {
		t.Fatalf("form\n%s\nwant\n%s", form, want)
	}
	for range 20 {
		if again, _ := testRequest().Form(); again != form {
			t.Fatal("form is not deterministic")
		}
	}
	v, _ := url.ParseQuery(form)
	for k := range v {
		if strings.Contains(k, "customer") || strings.Contains(k, "email") || strings.Contains(k, "phone") {
			t.Fatalf("personal data field %q", k)
		}
	}
	bad := testRequest()
	bad.AmountMinor = 0
	if _, err := bad.Form(); err == nil {
		t.Fatal("a zero amount was accepted")
	}
	bad = testRequest()
	bad.UIOrigin = "https://gk.example/path"
	if _, err := bad.Form(); err == nil {
		t.Fatal("an origin with a path was accepted")
	}
}

func TestSessionExpiryIsTheFirstDispatchSecondPlusTwoHours(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 7, 999_000_000, time.FixedZone("x", 3600))
	if got := SessionExpiry(now); !got.Equal(time.Date(2026, 11, 5, 11, 0, 7, 0, time.UTC)) {
		t.Fatalf("expiry %s", got)
	}
}

func TestPayloadRoundTripsAndRefusesForeignRequests(t *testing.T) {
	raw, err := NewPayload(testRequest(), testAccount)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `\u0026`) {
		t.Fatalf("payload escapes the form: %s", raw)
	}
	exp := Expected{CollectionID: testCollection, OrderID: testOrder, AmountMinor: 8585, Environment: "test", Account: testAccount,
		UIOrigin: "https://gk.example", ExpiresAt: time.Unix(1793827326, 0)}
	d, err := CheckPayload(raw, exp, "test")
	if err != nil {
		t.Fatal(err)
	}
	form, _ := testRequest().Form()
	if d.IdempotencyKey != testCollection || d.Form != form {
		t.Fatalf("dispatch %+v", d)
	}
	cases := map[string]func(Expected) (Expected, string){
		"other amount":      func(e Expected) (Expected, string) { e.AmountMinor = 8584; return e, "test" },
		"other order":       func(e Expected) (Expected, string) { e.OrderID = testCollection; return e, "test" },
		"other collection":  func(e Expected) (Expected, string) { e.CollectionID = testOrder; return e, "test" },
		"other account":     func(e Expected) (Expected, string) { e.Account = "acct_other"; return e, "test" },
		"other environment": func(e Expected) (Expected, string) { return e, "staging" },
		"live mode":         func(e Expected) (Expected, string) { e.Livemode = true; return e, "test" },
		"other origin":      func(e Expected) (Expected, string) { e.UIOrigin = "https://other.example"; return e, "test" },
		"other expiry":      func(e Expected) (Expected, string) { e.ExpiresAt = e.ExpiresAt.Add(time.Second); return e, "test" },
	}
	for name, change := range cases {
		e, env := change(exp)
		if _, err := CheckPayload(raw, e, env); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	var p Payload
	_ = json.Unmarshal(raw, &p)
	tamper := func(f func(*Payload)) []byte {
		q := p
		f(&q)
		b, _ := encode(q)
		return b
	}
	for name, b := range map[string][]byte{
		"extra field":    tamper(func(q *Payload) { q.Request += "&customer_email=a%40b.example" }),
		"repeated field": tamper(func(q *Payload) { q.Request += "&mode=payment" }),
		"other amount":   tamper(func(q *Payload) { q.Request = strings.Replace(q.Request, "=8585", "=1", 1) }),
		"other return": tamper(func(q *Payload) {
			q.Request = strings.Replace(q.Request, "gk.example%2Forders", "evil.example%2Forders", 1)
		}),
		"unknown key": []byte(strings.Replace(string(raw), `"v":1`, `"v":1,"x":1`, 1)),
	} {
		if _, err := CheckPayload(b, exp, "test"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestClassifyCreate(t *testing.T) {
	ok := `{"id":"cs_test_a1","object":"checkout.session","url":"https://checkout.stripe.com/c/pay/cs_test_a1","expires_at":1793827326,"livemode":false,"status":"open"}`
	cases := []struct {
		status int
		body   string
		want   ResultKind
		code   string
	}{
		{200, ok, Created, ""},
		{200, `{"id":"cs_test_a1","object":"checkout.session"}`, Unknown, ""},
		{200, `not json`, Unknown, ""},
		{400, `{"error":{"type":"invalid_request_error","message":"The expires_at timestamp must be at least 30 minutes"}}`, Rejected, "invalid_request_error"},
		{402, `{"error":{"type":"card_error"}}`, Rejected, "card_error"},
		{400, `{"error":{"type":"idempotency_error"}}`, Conflict, "idempotency_error"},
		{401, `{"error":{"type":"authentication_error"}}`, Refused, "authentication_error"},
		{403, `{"error":{"type":"invalid_request_error"}}`, Refused, "invalid_request_error"},
		{404, `{}`, Refused, ""},
		{409, `{"error":{"type":"idempotency_error"}}`, Deferred, "idempotency_error"},
		{429, `{"error":{"type":"rate_limit_error"}}`, Deferred, "rate_limit_error"},
		{500, `{"error":{"type":"api_error"}}`, Unknown, "api_error"},
		{502, ``, Unknown, ""},
	}
	for _, c := range cases {
		got := ClassifyCreate(c.status, []byte(c.body))
		if got.Kind != c.want || got.Code != c.code {
			t.Errorf("%d %s: %s/%q, want %s/%q", c.status, c.body, got.Kind, got.Code, c.want, c.code)
		}
		if strings.Contains(got.String(), "30 minutes") {
			t.Errorf("Stripe's message leaked into %q", got.String())
		}
	}
	if r := ClassifyCreate(200, []byte(ok)); r.Session == nil || r.Session.ID != "cs_test_a1" {
		t.Fatalf("created session %+v", r)
	}
	for status, want := range map[int]ResultKind{200: Unknown, 404: NotFound, 401: Refused, 429: Deferred, 400: Rejected, 503: Unknown} {
		if got := ClassifyRetrieve(status, []byte(`{}`)); got.Kind != want {
			t.Errorf("retrieve %d: %s, want %s", status, got.Kind, want)
		}
	}
}

// The expire answers seen on the sandbox (2026-11-04): an open session is
// expired (200); the same key replays that answer; an expired session is
// refused with 400 invalid_request_error (not open).
func TestClassifyExpire(t *testing.T) {
	expired := `{"id":"cs_test_a1","object":"checkout.session","status":"expired","livemode":false}`
	complete := `{"id":"cs_test_a1","object":"checkout.session","status":"complete","livemode":false}`
	cases := []struct {
		status int
		body   string
		want   ResultKind
	}{
		{200, expired, Expired},
		{200, complete, NotOpen},
		{200, `{"object":"checkout.session"}`, Unknown},
		{400, `{"error":{"type":"invalid_request_error","message":"Only Checkout Sessions with a status in [\"open\"] can be expired."}}`, NotOpen},
		{400, `{"error":{"type":"idempotency_error"}}`, Rejected},
		{404, `{"error":{"type":"invalid_request_error"}}`, Rejected},
		{401, `{"error":{"type":"authentication_error"}}`, Refused},
		{403, `{}`, Refused},
		{409, `{}`, Deferred},
		{429, `{"error":{"type":"rate_limit_error"}}`, Deferred},
		{500, `{}`, Unknown},
		{502, ``, Unknown},
	}
	for _, c := range cases {
		got := ClassifyExpire(c.status, []byte(c.body))
		if got.Kind != c.want {
			t.Errorf("expire %d %s: %s, want %s", c.status, c.body, got.Kind, c.want)
		}
		if strings.Contains(got.String(), "can be expired") {
			t.Errorf("Stripe's message leaked into %q", got.String())
		}
	}
	if ExpireKey("0da4b77c-2c5b-40d4-9215-2510342da911") != "0da4b77c-2c5b-40d4-9215-2510342da911-expire" {
		t.Fatal("expire key")
	}
}

func TestNewClientRefusesLiveKeysOutsideProduction(t *testing.T) {
	for _, key := range []string{"sk_live_x", "rk_live_x", "pk_test_x", "", "whsec_x"} {
		if _, err := NewClient(ClientConfig{Key: key}); err == nil {
			t.Errorf("%q accepted outside production", key)
		}
	}
	for _, key := range []string{"sk_test_x", "rk_test_x"} {
		if _, err := NewClient(ClientConfig{Key: key}); err != nil {
			t.Errorf("%q refused: %v", key, err)
		}
		if _, err := NewClient(ClientConfig{Key: key, Live: true}); err == nil {
			t.Errorf("%q accepted in production", key)
		}
	}
	if _, err := NewClient(ClientConfig{Key: "sk_live_x", Live: true}); err != nil {
		t.Errorf("live key refused in production: %v", err)
	}
}

func sign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"id":"evt_1","type":"checkout.session.completed","livemode":false}`)
	good := sign(testSecret, now.Unix(), body)
	header := func(ts int64, sigs ...string) string {
		h := fmt.Sprintf("t=%d", ts)
		for _, s := range sigs {
			h += ",v1=" + s
		}
		return h
	}
	if err := VerifySignature(header(now.Unix(), good), body, testSecret, now); err != nil {
		t.Fatalf("valid: %v", err)
	}
	// One of several v1 signatures (secret rotation), plus a v0 to ignore.
	if err := VerifySignature(header(now.Unix(), strings.Repeat("ab", 32), good)+",v0=00", body, testSecret, now); err != nil {
		t.Fatalf("multiple v1: %v", err)
	}
	// Within the tolerance both ways; outside it refused.
	for _, d := range []time.Duration{5 * time.Minute, -5 * time.Minute} {
		ts := now.Add(d).Unix()
		if err := VerifySignature(header(ts, sign(testSecret, ts, body)), body, testSecret, now); err != nil {
			t.Errorf("at %s: %v", d, err)
		}
	}
	for _, d := range []time.Duration{5*time.Minute + time.Second, -5*time.Minute - time.Second} {
		ts := now.Add(d).Unix()
		if err := VerifySignature(header(ts, sign(testSecret, ts, body)), body, testSecret, now); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("at %s: %v, want refused", d, err)
		}
	}
	refused := map[string]string{
		"tampered body": header(now.Unix(), sign(testSecret, now.Unix(), append([]byte{' '}, body...))),
		"wrong secret":  header(now.Unix(), sign("whsec_another_secret_entirely_0000", now.Unix(), body)),
		"no v1":         fmt.Sprintf("t=%d,v0=%s", now.Unix(), good),
		"no timestamp":  "v1=" + good,
		"empty":         "",
		"bad timestamp": "t=abc,v1=" + good,
	}
	for name, h := range refused {
		if err := VerifySignature(h, body, testSecret, now); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("%s: %v, want refused", name, err)
		}
	}
}

func TestParseEvent(t *testing.T) {
	body := `{"id":"evt_1Abc","object":"event","type":"checkout.session.completed","livemode":false,"created":1800000000,
		"data":{"object":{"id":"cs_test_a1","object":"checkout.session","customer_details":{"email":"x@example.com"},
		"metadata":{"collection_id":"` + testCollection + `","environment":"test","order_id":"` + testOrder + `"}}}}`
	ev, err := ParseEvent([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != "evt_1Abc" || ev.Type != "checkout.session.completed" || ev.Livemode || ev.Account != "" ||
		ev.ObjectID != "cs_test_a1" || ev.ObjectType != "checkout.session" || ev.MetadataCollection != testCollection ||
		ev.MetadataEnv != "test" || ev.CreatedAt == nil || ev.CreatedAt.Unix() != 1800000000 || len(ev.Dropped) != 0 {
		t.Fatalf("event %+v", ev)
	}
	// Lenient beyond identity: bad metadata is dropped, not refused.
	ev, err = ParseEvent([]byte(`{"id":"evt_2","type":"charge.refunded","livemode":true,"account":"acct_X1",
		"data":{"object":{"id":"ch_1","object":"charge","metadata":{"collection_id":"nope"}}}}`))
	if err != nil || ev.Account != "acct_X1" || !ev.Livemode || ev.MetadataCollection != "" || len(ev.Dropped) != 1 {
		t.Fatalf("lenient %+v %v", ev, err)
	}
	for _, bad := range []string{`[]`, `{"id":"evt_3","type":"x","livemode":false}`, `{"id":"nope","type":"a.b","livemode":false}`,
		`{"id":"evt_3","type":"a.b"}`, `{"id":"evt_3","type":"a.b","livemode":false,"object":"charge"}`} {
		if _, err := ParseEvent([]byte(bad)); !errors.Is(err, ErrMalformedEvent) {
			t.Errorf("%s: %v, want malformed", bad, err)
		}
	}
}

func i64(v int64) *int64 { return &v }

// paidSession is a fully paid session for the test collection.
func paidSession() *Session {
	md := map[string]string{"collection_id": testCollection, "order_id": testOrder, "environment": "test"}
	return &Session{ID: "cs_test_a1", Object: "checkout.session", Status: "complete", PaymentStatus: "paid",
		AmountTotal: i64(8585), Currency: "gbp", ClientReferenceID: testOrder, Metadata: md,
		PaymentIntent: &PaymentIntent{ID: "pi_1", Status: "succeeded", Amount: 8585, AmountReceived: 8585, Currency: "gbp",
			Metadata: md, Expanded: true, LatestCharge: &Charge{ID: "ch_1", Status: "succeeded", Paid: true, Captured: true,
				Amount: 8585, AmountCaptured: 8585, Currency: "gbp", Created: 1_800_000_123, Expanded: true}}}
}

func testExpectation() Expectation {
	return Expectation{CollectionID: testCollection, OrderID: testOrder, Environment: "test", SessionID: "cs_test_a1", AmountMinor: 8585}
}

func TestEvaluateMatrix(t *testing.T) {
	v := Evaluate(paidSession(), testExpectation())
	if v.Kind != VerdictPaid || !v.SuccessAt.Equal(time.Unix(1_800_000_123, 0)) {
		t.Fatalf("paid: %+v", v)
	}
	ev, _ := json.Marshal(v.Evidence)
	if len(ev) > 1024 || strings.Contains(string(ev), "example.com") {
		t.Fatalf("evidence %s", ev)
	}
	cases := map[string]struct {
		change func(*Session)
		want   string
	}{
		"open": {func(s *Session) { s.Status, s.PaymentStatus, s.PaymentIntent = "open", "unpaid", nil }, VerdictPending},
		"processing": {func(s *Session) {
			s.PaymentStatus = "unpaid"
			s.PaymentIntent.Status = "processing"
			s.PaymentIntent.LatestCharge.Status = "pending"
		}, VerdictPending},
		"requires_capture": {func(s *Session) {
			s.PaymentIntent.Status = "requires_capture"
			s.PaymentIntent.LatestCharge.Captured = false
		}, VerdictPending},
		"unpaid":                  {func(s *Session) { s.PaymentStatus = "unpaid" }, VerdictPending},
		"no_payment_required":     {func(s *Session) { s.PaymentStatus = "no_payment_required" }, VerdictPending},
		"complete without intent": {func(s *Session) { s.PaymentIntent = nil }, VerdictPending},
		"intent not expanded":     {func(s *Session) { s.PaymentIntent = &PaymentIntent{ID: "pi_1"} }, VerdictPending},
		"charge not expanded":     {func(s *Session) { s.PaymentIntent.LatestCharge = &Charge{ID: "ch_1"} }, VerdictPending},
		"charge not paid":         {func(s *Session) { s.PaymentIntent.LatestCharge.Paid = false }, VerdictPending},
		"charge failed":           {func(s *Session) { s.PaymentIntent.LatestCharge.Status = "failed" }, VerdictPending},
		"charge without time":     {func(s *Session) { s.PaymentIntent.LatestCharge.Created = 0 }, VerdictPending},
		"expired unpaid":          {func(s *Session) { s.Status, s.PaymentStatus, s.PaymentIntent = "expired", "unpaid", nil }, VerdictCancelled},
		"expired with failed PI":  {func(s *Session) { s.Status, s.PaymentStatus = "expired", "unpaid"; s.PaymentIntent.Status = "canceled" }, VerdictCancelled},
		"expired needing a method": {func(s *Session) {
			s.Status, s.PaymentStatus = "expired", "unpaid"
			s.PaymentIntent.Status = "requires_payment_method"
		}, VerdictCancelled},
		"expired processing": {func(s *Session) {
			s.Status, s.PaymentStatus = "expired", "unpaid"
			s.PaymentIntent.Status = "processing"
		}, VerdictPending},
		"expired requires_capture": {func(s *Session) {
			s.Status, s.PaymentStatus = "expired", "unpaid"
			s.PaymentIntent.Status = "requires_capture"
		}, VerdictPending},
		"expired requires_action": {func(s *Session) {
			s.Status, s.PaymentStatus = "expired", "unpaid"
			s.PaymentIntent.Status = "requires_action"
		}, VerdictPending},
		"expired intent unexpanded": {func(s *Session) {
			s.Status, s.PaymentStatus = "expired", "unpaid"
			s.PaymentIntent = &PaymentIntent{ID: "pi_1"}
		}, VerdictPending},
		"fully refunded": {func(s *Session) {
			s.PaymentIntent.LatestCharge.Refunded, s.PaymentIntent.LatestCharge.AmountRefunded = true, 8585
		}, VerdictMismatch},
		"partly refunded":           {func(s *Session) { s.PaymentIntent.LatestCharge.AmountRefunded = 1 }, VerdictMismatch},
		"refunded flag only":        {func(s *Session) { s.PaymentIntent.LatestCharge.Refunded = true }, VerdictMismatch},
		"disputed":                  {func(s *Session) { s.PaymentIntent.LatestCharge.Disputed = true }, VerdictMismatch},
		"expired with succeeded PI": {func(s *Session) { s.Status, s.PaymentStatus = "expired", "unpaid" }, VerdictMismatch},
		"wrong amount":              {func(s *Session) { s.AmountTotal = i64(8584) }, VerdictMismatch},
		"no amount":                 {func(s *Session) { s.AmountTotal = nil }, VerdictMismatch},
		"wrong currency":            {func(s *Session) { s.Currency = "eur" }, VerdictMismatch},
		"live mode":                 {func(s *Session) { s.Livemode = true }, VerdictMismatch},
		"other collection": {func(s *Session) {
			s.Metadata = map[string]string{"collection_id": testOrder, "order_id": testOrder, "environment": "test"}
		}, VerdictMismatch},
		"other environment": {func(s *Session) {
			s.Metadata = map[string]string{"collection_id": testCollection, "order_id": testOrder, "environment": "staging"}
		}, VerdictMismatch},
		"no metadata":             {func(s *Session) { s.Metadata = nil }, VerdictMismatch},
		"other client reference":  {func(s *Session) { s.ClientReferenceID = testCollection }, VerdictMismatch},
		"other session":           {func(s *Session) { s.ID = "cs_test_other" }, VerdictMismatch},
		"intent received less":    {func(s *Session) { s.PaymentIntent.AmountReceived = 100 }, VerdictMismatch},
		"intent currency":         {func(s *Session) { s.PaymentIntent.Currency = "usd" }, VerdictMismatch},
		"intent live":             {func(s *Session) { s.PaymentIntent.Livemode = true }, VerdictMismatch},
		"intent other collection": {func(s *Session) { s.PaymentIntent.Metadata = map[string]string{"collection_id": testOrder} }, VerdictMismatch},
		"charge captured less":    {func(s *Session) { s.PaymentIntent.LatestCharge.AmountCaptured = 100 }, VerdictMismatch},
		"charge currency":         {func(s *Session) { s.PaymentIntent.LatestCharge.Currency = "eur" }, VerdictMismatch},
		"charge live":             {func(s *Session) { s.PaymentIntent.LatestCharge.Livemode = true }, VerdictMismatch},
	}
	for name, c := range cases {
		s := paidSession()
		c.change(s)
		got := Evaluate(s, testExpectation())
		if got.Kind != c.want {
			t.Errorf("%s: %s (%s), want %s", name, got.Kind, got.Reason, c.want)
		}
		if (got.Kind == VerdictMismatch) != (got.Reason != "") {
			t.Errorf("%s: reason %q with verdict %s", name, got.Reason, got.Kind)
		}
	}
}

func TestSessionDecodesIDOrExpandedIntent(t *testing.T) {
	var s Session
	if err := json.Unmarshal([]byte(`{"id":"cs_1","payment_intent":"pi_1"}`), &s); err != nil || s.PaymentIntent == nil ||
		s.PaymentIntent.ID != "pi_1" || s.PaymentIntent.Expanded {
		t.Fatalf("id form %+v %v", s.PaymentIntent, err)
	}
	if err := json.Unmarshal([]byte(`{"id":"cs_1","payment_intent":{"id":"pi_1","status":"succeeded","latest_charge":"ch_1"}}`), &s); err != nil ||
		!s.PaymentIntent.Expanded || s.PaymentIntent.LatestCharge == nil || s.PaymentIntent.LatestCharge.Expanded {
		t.Fatalf("expanded form %+v %v", s.PaymentIntent, err)
	}
	if err := json.Unmarshal([]byte(`{"id":"cs_1","payment_intent":null}`), &s); err != nil || s.PaymentIntent != nil {
		t.Fatalf("null form %+v %v", s.PaymentIntent, err)
	}
}

// A delivery collection's line item reads "GK Cakes delivery" (0035); a
// delivery request fixed by an older build (named like the cake's) is still
// accepted for that collection, but a cake collection never accepts the
// delivery name, and neither name can be swapped for anything else.
func TestDeliveryLineItem(t *testing.T) {
	cake := testRequest()
	delivery := testRequest()
	delivery.Kind = KindDelivery
	cakeRaw, err := NewPayload(cake, testAccount)
	if err != nil {
		t.Fatal(err)
	}
	deliveryRaw, err := NewPayload(delivery, testAccount)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(deliveryRaw), "GK+Cakes+delivery") || strings.Contains(string(deliveryRaw), "GK+Cakes+order") ||
		!strings.Contains(string(cakeRaw), "GK+Cakes+order") {
		t.Fatalf("line items: cake %s delivery %s", cakeRaw, deliveryRaw)
	}
	exp := Expected{CollectionID: testCollection, OrderID: testOrder, AmountMinor: 8585, Environment: "test", Account: testAccount,
		UIOrigin: "https://gk.example", ExpiresAt: time.Unix(1793827326, 0)}
	deliveryExp := exp
	deliveryExp.Kind = KindDelivery
	for name, c := range map[string]struct {
		raw []byte
		exp Expected
		ok  bool
	}{
		"cake":                    {cakeRaw, exp, true},
		"delivery":                {deliveryRaw, deliveryExp, true},
		"older delivery request":  {cakeRaw, deliveryExp, true},
		"delivery name on a cake": {deliveryRaw, exp, false},
		"other name": {[]byte(strings.Replace(string(deliveryRaw), "GK+Cakes+delivery", "Something+else", 1)),
			deliveryExp, false},
	} {
		if _, err := CheckPayload(c.raw, c.exp, "test"); (err == nil) != c.ok {
			t.Errorf("%s: %v, want ok %v", name, err, c.ok)
		}
	}
}
