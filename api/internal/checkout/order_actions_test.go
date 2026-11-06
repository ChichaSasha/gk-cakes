package checkout

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A paid-for-delivery store at a fixed clock (no real date).
func actionStore(now time.Time) *Store {
	return &Store{Now: func() time.Time { return now }, Env: "test", StripeAccount: "acct_test123", UIOrigin: "https://gk.example"}
}

func actionsOf(t *testing.T, raw []byte) ([]string, map[string]json.RawMessage) {
	t.Helper()
	var v struct {
		Actions        []string                   `json:"actions"`
		DeliveryChoice map[string]json.RawMessage `json:"delivery_choice"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	if v.Actions == nil {
		t.Fatalf("no actions in %s", raw)
	}
	return v.Actions, v.DeliveryChoice
}

const allGrants = `["cancel","move_pickup","pay","read"]`

// The windows follow the store's clock: the quote window and lead time for
// pay and free confirmation, the six-hour cutoff for a same-day move, each
// at its boundary; grants limit a guest.
func TestActionsFromFactsAtTheStoreClock(t *testing.T) {
	base := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	quote := base.Add(24 * time.Hour)
	lead := base.Add(48 * time.Hour)
	view := `{"id":"o","late_payment":null,"delivery":null,"delivery_choice":null,"delivery_failure":null}`
	pay := `{"grants":` + allGrants + `,"pay":{"before":"` + quote.Format(time.RFC3339) + `","not_after":"` +
		lead.Format(time.RFC3339) + `"},"free_confirmation":null,"cancel":false,"move_pickup":null}`
	for name, c := range map[string]struct {
		now   time.Time
		facts string
		store func(*Store)
		want  string
	}{
		"pay in the window":            {base, pay, nil, "pay"},
		"pay at the quote expiry":      {quote, pay, nil, ""},
		"pay before the expiry":        {quote.Add(-time.Second), pay, nil, "pay"},
		"pay without payments":         {base, pay, func(s *Store) { s.StripeAccount = "" }, ""},
		"pay without the pay grant":    {base, strings.Replace(pay, allGrants, `["cancel","read"]`, 1), nil, ""},
		"free at the lead-time limit":  {lead, `{"grants":["pay","read"],"free_confirmation":{"before":"` + lead.Add(time.Hour).Format(time.RFC3339) + `","not_after":"` + lead.Format(time.RFC3339) + `"},"cancel":false}`, nil, "free_confirmation"},
		"free after the lead time":     {lead.Add(time.Second), `{"grants":["pay","read"],"free_confirmation":{"before":"` + lead.Add(time.Hour).Format(time.RFC3339) + `","not_after":"` + lead.Format(time.RFC3339) + `"},"cancel":false}`, nil, ""},
		"free without payments config": {base, `{"grants":["pay","read"],"free_confirmation":{"before":null,"not_after":null},"cancel":false}`, func(s *Store) { s.StripeAccount = "" }, "free_confirmation"},
		"cancel":                       {base, `{"grants":` + allGrants + `,"cancel":true}`, nil, "cancel"},
		"cancel read-only link":        {base, `{"grants":["read"],"cancel":true}`, nil, ""},
		"move at the cutoff":           {lead, `{"grants":["move_pickup","read"],"cancel":false,"move_pickup":{"before":null,"not_after":"` + lead.Format(time.RFC3339) + `"}}`, nil, "move_pickup"},
		"move after the cutoff":        {lead.Add(time.Nanosecond), `{"grants":["move_pickup","read"],"cancel":false,"move_pickup":{"before":null,"not_after":"` + lead.Format(time.RFC3339) + `"}}`, nil, ""},
		"cancel and move for an owner": {base, `{"grants":` + allGrants + `,"cancel":true,"move_pickup":{"not_after":"` + lead.Format(time.RFC3339) + `"}}`, nil, "cancel,move_pickup"},
	} {
		s := actionStore(c.now)
		if c.store != nil {
			c.store(s)
		}
		out, err := s.withActions([]byte(view), []byte(c.facts))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := actionsOf(t, out)
		if strings.Join(got, ",") != c.want {
			t.Errorf("%s: actions %v, want %q", name, got, c.want)
		}
		// Without a Q5 block the representation keeps its bytes.
		if !strings.HasPrefix(string(out), view[:len(view)-1]+`, "actions" : [`) {
			t.Errorf("%s: bytes not kept: %s", name, out)
		}
	}
	s := actionStore(base)
	for _, bad := range []string{`[]`, `{}`, `{"actions":[]}`, `nope`} {
		if _, err := s.withActions([]byte(bad), []byte(pay)); err == nil {
			t.Errorf("%s answered", bad)
		}
	}
}

// The blocks' actions follow the view at the store's clock: an offer to
// answer (declining has no limit, so even an offer no longer acceptable),
// a payable delivery quote, the Q5 choices this caller may make, an open
// failure choice; each needs its grant.
func TestActionsFromTheViewBlocks(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	payBy := now.Add(time.Hour).Format(time.RFC3339)
	for name, c := range map[string]struct {
		view, grants string
		want         string
		allowed      string
	}{
		"offer to answer": {`{"late_payment":{"state":"offered","offer":{"id":"x","accept_until":"` + now.Add(-time.Hour).Format(time.RFC3339) + `"}}}`,
			allGrants, "answer_late_payment", ""},
		"offer, no cancel grant": {`{"late_payment":{"state":"offered","offer":{"id":"x","accept_until":"` + payBy + `"}}}`,
			`["read","pay","move_pickup"]`, "", ""},
		"waiting for an offer": {`{"late_payment":{"state":"waiting","offer":null}}`, allGrants, "", ""},
		"payable delivery": {`{"delivery":{"state":"quoted","payable":true,"quote":{"id":"q","pay_by":"` + payBy + `"}}}`,
			allGrants, "pay_delivery", ""},
		"delivery past pay-by": {`{"delivery":{"state":"quoted","payable":true,"quote":{"id":"q","pay_by":"` + now.Format(time.RFC3339) + `"}}}`,
			allGrants, "", ""},
		"q5 open for the owner": {`{"delivery_choice":{"open":true,"reason":"not_possible","opens_at":null,"pending":false,"choices":["collection","customer_courier","full_refund"],"times":[]}}`,
			allGrants, "delivery_choice", "collection,customer_courier,full_refund"},
		"q5 with move_pickup only": {`{"delivery_choice":{"open":true,"reason":"not_possible","opens_at":null,"pending":false,"choices":["collection","customer_courier","full_refund"],"times":[]}}`,
			`["move_pickup","read"]`, "delivery_choice", "collection,customer_courier"},
		"q5 with cancel only": {`{"delivery_choice":{"open":true,"reason":"not_possible","opens_at":null,"pending":false,"choices":["collection","customer_courier","full_refund"],"times":[]}}`,
			`["cancel","read"]`, "delivery_choice", "full_refund"},
		"q5 read-only": {`{"delivery_choice":{"open":true,"reason":"not_possible","opens_at":null,"pending":false,"choices":["collection","customer_courier","full_refund"],"times":[]}}`,
			`["read"]`, "", ""},
		"q5 before pay-by": {`{"delivery_choice":{"open":false,"reason":null,"opens_at":"` + payBy + `","pending":false,"choices":[],"times":[]}}`,
			allGrants, "", ""},
		"q5 at pay-by": {`{"delivery_choice":{"open":false,"reason":null,"opens_at":"` + now.Format(time.RFC3339) + `","pending":false,"choices":[],"times":[]}}`,
			allGrants, "delivery_choice", "collection,customer_courier,full_refund"},
		"failure choice": {`{"delivery_failure":{"cause":"our_side","open":true,"choices":["redelivery","collection"],"times":[]}}`,
			allGrants, "delivery_failure_choice", ""},
		"failure choice, no move grant": {`{"delivery_failure":{"cause":"our_side","open":true,"choices":["redelivery","collection"],"times":[]}}`,
			`["cancel","pay","read"]`, "", ""},
		"failure closed": {`{"delivery_failure":{"cause":"our_side","open":false,"choices":[],"times":[]}}`, allGrants, "", ""},
	} {
		s := actionStore(now)
		out, err := s.withActions(s.atClock([]byte(c.view)), []byte(`{"grants":`+c.grants+`,"cancel":false}`))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, dc := actionsOf(t, out)
		if strings.Join(got, ",") != c.want {
			t.Errorf("%s: actions %v, want %q", name, got, c.want)
		}
		if dc != nil {
			var allowed []string
			if err := json.Unmarshal(dc["allowed_choices"], &allowed); err != nil || allowed == nil {
				t.Errorf("%s: allowed_choices %s", name, dc["allowed_choices"])
			}
			if strings.Join(allowed, ",") != c.allowed {
				t.Errorf("%s: allowed %v, want %q", name, allowed, c.allowed)
			}
		}
	}
}

// With customer commands off, nothing is offered.
func TestWithoutActions(t *testing.T) {
	in := `{"actions":["cancel","delivery_choice"],"delivery_choice":{"open":true,"choices":["full_refund"],"allowed_choices":["full_refund"]},"id":"o"}`
	got := string(WithoutActions(json.RawMessage(in)))
	if !strings.Contains(got, `"actions":[]`) || !strings.Contains(got, `"allowed_choices":[]`) || !strings.Contains(got, `"choices":["full_refund"]`) {
		t.Fatalf("%s", got)
	}
	if got := string(WithoutActions(json.RawMessage(`{"id":"o"}`))); got != `{"id":"o"}` {
		t.Fatalf("no actions: %s", got)
	}
}

// Account summaries: order_blocks never leave; delivery_state and the
// attention list follow the store's clock; an offer that can no longer be
// accepted does not ask for an answer.
func TestSummaryAttention(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 30, 0, 0, time.UTC)
	s := actionStore(now)
	row := func(blocks string) map[string]json.RawMessage {
		out, err := s.withSummaryAttention(json.RawMessage(`{"id":"o","total_minor":10000,"order_blocks":` + blocks + `}`))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["order_blocks"]; ok || strings.Contains(string(out), `"pay_by":`) {
			t.Fatalf("blocks leaked: %s", out)
		}
		return m
	}
	m := row(`{"late_payment":null,"delivery":null,"delivery_choice":null,"delivery_failure":null}`)
	if string(m["delivery_state"]) != "null" || string(m["needs_answer"]) != "false" || string(m["attention"]) != "[]" ||
		string(m["total_minor"]) != "10000" {
		t.Fatalf("plain row %v", m)
	}
	// The quote's pay-by is now: pay_by_passed, not payable; the Q5 choice
	// opens at that instant.
	m = row(`{"late_payment":null,"delivery":{"state":"quoted","payable":true,"quote":{"id":"q","pay_by":"` + now.Format(time.RFC3339) + `"}},` +
		`"delivery_choice":{"open":false,"reason":null,"opens_at":"` + now.Format(time.RFC3339) + `","pending":false,"choices":[],"times":[]},"delivery_failure":null}`)
	if string(m["delivery_state"]) != `"pay_by_passed"` || string(m["needs_answer"]) != "true" || string(m["attention"]) != `["delivery_choice"]` {
		t.Fatalf("pay-by passed %v", m)
	}
	m = row(`{"late_payment":null,"delivery":{"state":"quoted","payable":true,"quote":{"id":"q","pay_by":"` + now.Add(time.Minute).Format(time.RFC3339) + `"}},` +
		`"delivery_choice":{"open":false,"reason":null,"opens_at":"` + now.Add(time.Minute).Format(time.RFC3339) + `","pending":false,"choices":[],"times":[]},"delivery_failure":null}`)
	if string(m["delivery_state"]) != `"quoted"` || string(m["attention"]) != `["pay_delivery"]` {
		t.Fatalf("quoted %v", m)
	}
	m = row(`{"late_payment":{"state":"offered","offer":{"id":"x","accept_until":"` + now.Format(time.RFC3339) + `"}},"delivery":null,"delivery_choice":null,` +
		`"delivery_failure":{"cause":"our_side","open":true,"choices":["redelivery","collection"],"times":[]}}`)
	if string(m["attention"]) != `["delivery_failure_choice"]` {
		t.Fatalf("offer no longer acceptable %v", m)
	}
	m = row(`{"late_payment":{"state":"offered","offer":{"id":"x","accept_until":"` + now.Add(time.Second).Format(time.RFC3339) + `"}},"delivery":null,"delivery_choice":null,"delivery_failure":null}`)
	if string(m["attention"]) != `["answer_late_payment"]` || string(m["needs_answer"]) != "true" {
		t.Fatalf("acceptable offer %v", m)
	}
	if _, err := s.withSummaryAttention(json.RawMessage(`{"id":"o"}`)); err == nil {
		t.Fatal("a row without blocks was answered")
	}
}
