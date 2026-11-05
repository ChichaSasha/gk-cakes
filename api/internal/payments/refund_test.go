package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const (
	testRefundOp = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
	testPI       = "pi_3TestPaymentIntent"
)

func testRefundRequest() RefundRequest {
	return RefundRequest{OperationID: testRefundOp, OrderID: testOrder, CollectionID: testCollection,
		PaymentIntentID: testPI, AmountMinor: 10000, Environment: "test"}
}

func TestRefundFormIsExactAndDeterministic(t *testing.T) {
	form, err := testRefundRequest().Form()
	if err != nil {
		t.Fatal(err)
	}
	want := "amount=10000" +
		"&metadata%5Bcollection_id%5D=" + testCollection +
		"&metadata%5Benvironment%5D=test" +
		"&metadata%5Border_id%5D=" + testOrder +
		"&metadata%5Brefund_operation_id%5D=" + testRefundOp +
		"&payment_intent=" + testPI +
		"&reason=requested_by_customer"
	if form != want {
		t.Fatalf("form\n%s\nwant\n%s", form, want)
	}
	for _, mutate := range []func(*RefundRequest){
		func(r *RefundRequest) { r.AmountMinor = 0 },
		func(r *RefundRequest) { r.PaymentIntentID = "" },
		func(r *RefundRequest) { r.PaymentIntentID = "ch_123" },
		func(r *RefundRequest) { r.OperationID = "not-a-uuid" },
		func(r *RefundRequest) { r.Environment = "Test" },
	} {
		r := testRefundRequest()
		mutate(&r)
		if _, err := r.Form(); err == nil {
			t.Errorf("invalid request %+v accepted", r)
		}
	}
}

func TestRefundPayloadRoundTripAndTampering(t *testing.T) {
	raw, err := NewRefundPayload(testRefundRequest(), testAccount)
	if err != nil {
		t.Fatal(err)
	}
	exp := RefundExpected{RefundRequest: testRefundRequest(), Account: testAccount}
	d, err := CheckRefundPayload(raw, exp, "test")
	if err != nil || d.IdempotencyKey != testRefundOp || !strings.Contains(d.Form, "amount=10000") {
		t.Fatalf("check %+v %v", d, err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	// The keys migration 0016's refund_dispatch_fix reads.
	for _, k := range []string{"v", "provider", "kind", "environment", "account", "livemode", "idempotency_key",
		"refund_operation_id", "order_id", "collection_id", "payment_intent_id", "amount_minor", "request"} {
		if _, ok := p[k]; !ok {
			t.Errorf("payload lacks %q", k)
		}
	}
	if p["kind"] != "refund" || p["idempotency_key"] != testRefundOp || p["livemode"] != false {
		t.Fatalf("payload %s", raw)
	}
	tamper := func(k string, v any) []byte {
		q := map[string]any{}
		_ = json.Unmarshal(raw, &q)
		q[k] = v
		b, _ := json.Marshal(q)
		return b
	}
	cases := map[string][]byte{
		"amount":       tamper("amount_minor", 9999),
		"request":      tamper("request", strings.Replace(p["request"].(string), "amount=10000", "amount=20000", 1)),
		"extra field":  tamper("request", p["request"].(string)+"&refund_application_fee=true"),
		"unknown key":  tamper("customer", "x"),
		"account":      tamper("account", "acct_other"),
		"environment":  tamper("environment", "staging"),
		"livemode":     tamper("livemode", true),
		"pi":           tamper("payment_intent_id", "pi_other"),
		"key":          tamper("idempotency_key", testCollection),
		"kind":         tamper("kind", "session"),
		"not json":     []byte("{"),
		"trailing doc": append(append([]byte{}, raw...), []byte(" {}")...),
	}
	for name, b := range cases {
		if _, err := CheckRefundPayload(b, exp, "test"); err == nil {
			t.Errorf("%s: tampered payload accepted", name)
		}
	}
	if _, err := CheckRefundPayload(raw, exp, "staging"); err == nil {
		t.Error("another worker environment accepted")
	}
	other := exp
	other.AmountMinor = 5000
	if _, err := CheckRefundPayload(raw, other, "test"); err == nil {
		t.Error("payload of another amount accepted")
	}
}

func refundJSON(id, status string, amount int64, pi string) string {
	return fmt.Sprintf(`{"id":%q,"object":"refund","amount":%d,"currency":"gbp","status":%q,"payment_intent":%q,
		"charge":"ch_1","created":1793827326,"reason":"requested_by_customer","failure_reason":null,
		"metadata":{"refund_operation_id":%q,"collection_id":%q}}`, id, amount, status, pi, testRefundOp, testCollection)
}

func TestClassifyRefundCreate(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   ResultKind
	}{
		{200, refundJSON("re_1", "succeeded", 10000, testPI), RefundSucceeded},
		{200, refundJSON("re_1", "pending", 10000, testPI), RefundPending},
		{200, refundJSON("re_1", "requires_action", 10000, testPI), RefundRequiresAction},
		{200, refundJSON("re_1", "failed", 10000, testPI), RefundFailed},
		{200, refundJSON("re_1", "canceled", 10000, testPI), RefundFailed},
		{200, refundJSON("re_1", "mystery", 10000, testPI), Unknown},
		{200, `{"id":"cs_1","object":"checkout.session"}`, Unknown},
		{200, `not json`, Unknown},
		{400, `{"error":{"type":"idempotency_error"}}`, Conflict},
		{400, `{"error":{"type":"invalid_request_error","code":"amount_too_large"}}`, Rejected},
		{402, `{"error":{"type":"card_error"}}`, Rejected},
		{409, `{"error":{"type":"api_error"}}`, Deferred},
		{429, `{"error":{"type":"rate_limit_error"}}`, Deferred},
		{401, `{"error":{"type":"authentication_error"}}`, Refused},
		{403, `{"error":{"type":"permission_error"}}`, Refused},
		{500, `{"error":{"type":"api_error"}}`, Unknown},
		{502, ``, Unknown},
	}
	for _, c := range cases {
		if got := ClassifyRefundCreate(c.status, []byte(c.body)); got.Kind != c.want {
			t.Errorf("%d %s: %s, want %s", c.status, c.body, got.Kind, c.want)
		}
	}
	res := ClassifyRefundCreate(200, []byte(refundJSON("re_1", "succeeded", 10000, testPI)))
	if res.Refund == nil || res.Refund.PaymentIntent != testPI || res.Refund.OperationID() != testRefundOp {
		t.Fatalf("refund %+v", res.Refund)
	}
	ev := EvidenceOfRefund(res.Refund, false)
	if ev.RefundID != "re_1" || ev.Status != "succeeded" || ev.AmountMinor != 10000 || ev.Currency != "gbp" ||
		ev.PaymentIntentID != testPI || ev.Livemode {
		t.Fatalf("evidence %+v", ev)
	}
	for kind, want := range map[ResultKind]string{RefundSucceeded: "succeeded", RefundFailed: "failed", Rejected: "rejected",
		RefundPending: "pending", RefundRequiresAction: "requires_action", Unknown: "", Deferred: "", Conflict: "", Refused: ""} {
		if got := RefundOutcomeStatus(kind); got != want {
			t.Errorf("outcome of %s: %q", kind, got)
		}
	}
	// Expanded payment intent and a free-text failure reason.
	r, err := decodeRefund([]byte(`{"id":"re_2","object":"refund","amount":1,"currency":"gbp","status":"failed",
		"payment_intent":{"id":"` + testPI + `","object":"payment_intent"},"failure_reason":"Card closed! by bank"}`))
	if err != nil || r.PaymentIntent != testPI || r.FailureReason != "" {
		t.Fatalf("decode %+v %v", r, err)
	}
}

func TestClassifyRefundRetrieve(t *testing.T) {
	for status, want := range map[int]ResultKind{404: NotFound, 429: Deferred, 409: Deferred, 401: Refused, 403: Refused,
		400: Rejected, 500: Unknown} {
		if got := ClassifyRefundRetrieve(status, []byte(`{"error":{"type":"x"}}`)); got.Kind != want {
			t.Errorf("%d: %s, want %s", status, got.Kind, want)
		}
	}
	if got := ClassifyRefundRetrieve(200, []byte(refundJSON("re_1", "pending", 5, testPI))); got.Kind != Found || got.Refund.Status != "pending" {
		t.Fatalf("found %+v", got)
	}
}

// fakeRefunds is a minimal Stripe refunds endpoint: create with
// idempotency (the same key and body replay the refund; another body is an
// idempotency_error), retrieve, and a paginated list by payment intent.
type fakeRefunds struct {
	mu       sync.Mutex
	t        *testing.T
	refunds  []map[string]any // in creation order
	byKey    map[string]int
	forms    map[string]string
	requests []string
	pageSize int
}

func (f *fakeRefunds) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if user, _, ok := r.BasicAuth(); !ok || user != "sk_test_fake" || r.Header.Get("Stripe-Version") != APIVersion {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"type":"authentication_error"}}`)
		return
	}
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/refunds":
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			w.WriteHeader(400)
			return
		}
		body, _ := io.ReadAll(r.Body)
		key := r.Header.Get("Idempotency-Key")
		if i, seen := f.byKey[key]; seen {
			if f.forms[key] != string(body) {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"type":"idempotency_error"}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(f.refunds[i])
			return
		}
		v, _ := url.ParseQuery(string(body))
		var amount int64
		_, _ = fmt.Sscan(v.Get("amount"), &amount)
		re := map[string]any{"id": fmt.Sprintf("re_fake%d", len(f.refunds)+1), "object": "refund", "amount": amount,
			"currency": "gbp", "status": "succeeded", "payment_intent": v.Get("payment_intent"),
			"metadata": map[string]string{"refund_operation_id": v.Get("metadata[refund_operation_id]")}}
		f.refunds = append(f.refunds, re)
		f.byKey[key], f.forms[key] = len(f.refunds)-1, string(body)
		_ = json.NewEncoder(w).Encode(re)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/refunds":
		q := r.URL.Query()
		if q.Get("limit") != "100" {
			w.WriteHeader(400)
			return
		}
		var match []map[string]any
		for i := len(f.refunds) - 1; i >= 0; i-- { // newest first, as Stripe lists
			if f.refunds[i]["payment_intent"] == q.Get("payment_intent") {
				match = append(match, f.refunds[i])
			}
		}
		start := 0
		if after := q.Get("starting_after"); after != "" {
			for i, re := range match {
				if re["id"] == after {
					start = i + 1
				}
			}
		}
		end := min(start+f.pageSize, len(match))
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": match[start:end], "has_more": end < len(match)})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/refunds/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/refunds/")
		for _, re := range f.refunds {
			if re["id"] == id {
				_ = json.NewEncoder(w).Encode(re)
				return
			}
		}
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error"}}`)
	default:
		w.WriteHeader(404)
	}
}

func newFakeRefunds(t *testing.T, pageSize int) (*fakeRefunds, *Client) {
	f := &fakeRefunds{t: t, byKey: map[string]int{}, forms: map[string]string{}, pageSize: pageSize}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientConfig{Key: "sk_test_fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

func TestRefundClientCreateRetrieveAndCompleteList(t *testing.T) {
	f, c := newFakeRefunds(t, 2)
	ctx := context.Background()
	form, _ := testRefundRequest().Form()
	first := c.CreateRefund(ctx, testRefundOp, form)
	again := c.CreateRefund(ctx, testRefundOp, form)
	if first.Kind != RefundSucceeded || again.Kind != RefundSucceeded || first.Refund.ID != again.Refund.ID {
		t.Fatalf("create %v / replay %v", first, again)
	}
	if res := c.CreateRefund(ctx, testRefundOp, strings.Replace(form, "amount=10000", "amount=1", 1)); res.Kind != Conflict {
		t.Fatalf("another body under the key: %v", res)
	}
	if got := c.RetrieveRefund(ctx, first.Refund.ID); got.Kind != Found || got.Refund.Amount != 10000 {
		t.Fatalf("retrieve %v", got)
	}
	if got := c.RetrieveRefund(ctx, "re_missing"); got.Kind != NotFound {
		t.Fatalf("retrieve missing %v", got)
	}
	if got := c.RetrieveRefund(ctx, "../v1/account"); got.Kind != Refused {
		t.Fatalf("retrieve bad id %v", got)
	}
	// Five refunds of the payment intent (and one of another): three pages.
	for i := range 4 {
		r := testRefundRequest()
		r.OperationID = fmt.Sprintf("bbbbbbbb-cccc-4ddd-8eee-%012d", i)
		r.AmountMinor = int64(100 + i)
		form, _ := r.Form()
		if res := c.CreateRefund(ctx, r.OperationID, form); res.Kind != RefundSucceeded {
			t.Fatal(res)
		}
	}
	other := testRefundRequest()
	other.OperationID, other.PaymentIntentID = "bbbbbbbb-cccc-4ddd-8eee-999999999999", "pi_other"
	oform, _ := other.Form()
	c.CreateRefund(ctx, other.OperationID, oform)
	f.requests = nil
	list := c.ListRefunds(ctx, testPI)
	if list.Kind != Found || len(list.Refunds) != 5 || list.Pages != 3 {
		t.Fatalf("list %v: %d refunds, %d pages", list, len(list.Refunds), list.Pages)
	}
	seen := map[string]bool{}
	for _, r := range list.Refunds {
		if seen[r.ID] || r.PaymentIntent != testPI {
			t.Fatalf("refund %+v listed twice or of another intent", r)
		}
		seen[r.ID] = true
	}
	want := []string{
		"GET /v1/refunds?limit=100&payment_intent=" + testPI,
		"GET /v1/refunds?limit=100&payment_intent=" + testPI + "&starting_after=" + list.Refunds[1].ID,
		"GET /v1/refunds?limit=100&payment_intent=" + testPI + "&starting_after=" + list.Refunds[3].ID,
	}
	if strings.Join(f.requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests\n%s\nwant\n%s", strings.Join(f.requests, "\n"), strings.Join(want, "\n"))
	}
	if got := c.ListRefunds(ctx, "ch_123"); got.Kind != Refused {
		t.Fatalf("list by a charge %v", got)
	}
}

func TestRefundListIsNeverCompleteWhenAPageFails(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case r.URL.Query().Get("payment_intent") == "pi_endless":
			fmt.Fprintf(w, `{"object":"list","data":[%s],"has_more":true}`, refundJSON(fmt.Sprintf("re_%d", calls), "succeeded", 1, "pi_endless"))
		case calls == 1:
			fmt.Fprintf(w, `{"object":"list","data":[%s],"has_more":true}`, refundJSON("re_a", "succeeded", 1, testPI))
		case r.URL.Query().Get("payment_intent") == "pi_foreign":
			fmt.Fprintf(w, `{"object":"list","data":[%s],"has_more":false}`, refundJSON("re_x", "succeeded", 1, testPI))
		default:
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(srv.Close)
	c, _ := NewClient(ClientConfig{Key: "sk_test_fake", BaseURL: srv.URL})
	if got := c.ListRefunds(context.Background(), testPI); got.Kind != Unknown || got.Refunds != nil {
		t.Fatalf("second page failed: %v (%d refunds)", got, len(got.Refunds))
	}
	if got := c.ListRefunds(context.Background(), "pi_foreign"); got.Kind != Unknown {
		t.Fatalf("foreign refund in the list: %v", got)
	}
	calls = 0
	if got := c.ListRefunds(context.Background(), "pi_endless"); got.Kind != Unknown || got.Pages != MaxRefundPages {
		t.Fatalf("endless list: %v after %d pages", got, got.Pages)
	}
}

func TestRefundCreateTransportFailureIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	c, _ := NewClient(ClientConfig{Key: "sk_test_fake", BaseURL: srv.URL})
	form, _ := testRefundRequest().Form()
	res := c.CreateRefund(context.Background(), testRefundOp, form)
	if res.Kind != Unknown || res.Err == nil || strings.Contains(res.String(), "sk_test_fake") {
		t.Fatalf("lost answer %v", res)
	}
	if res := c.CreateRefund(context.Background(), "not-a-key", form); res.Kind != Refused {
		t.Fatalf("bad key %v", res)
	}
}
