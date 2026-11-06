package checkout

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

// The delivery block's clock-dependent parts follow the store's clock:
// quoted/pay_by_passed (equality has passed) and payable only before
// pay_by; every other state and representation stays as the database made
// it.
func TestWithDeliveryClock(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 30, 0, 0, time.UTC)
	s := &Store{Now: func() time.Time { return now }}
	for name, c := range map[string]struct {
		state, payBy string
		payable      bool
		wantState    string
		wantPayable  bool
	}{
		"before":                   {"quoted", "2026-11-12T12:30:01Z", true, "quoted", true},
		"at":                       {"quoted", "2026-11-12T12:30:00Z", true, "pay_by_passed", false},
		"after":                    {"quoted", "2026-11-12T11:00:00Z", true, "pay_by_passed", false},
		"database passed, api not": {"pay_by_passed", "2026-11-12T13:00:00Z", true, "quoted", true},
		"order closed":             {"quoted", "2026-11-12T13:00:00Z", false, "quoted", false},
		"checking":                 {"payment_checking", "2026-11-12T11:00:00Z", false, "payment_checking", false},
		"paid":                     {"paid", "2026-11-12T11:00:00Z", false, "paid", false},
		"waived":                   {"waived", "2026-11-12T13:00:00Z", false, "waived", false},
	} {
		raw := []byte(`{"id":"o","delivery":{"state":"` + c.state + `","quote":{"id":"q","amount_minor":12000,"pay_by":"` + c.payBy +
			`"},"payable":` + map[bool]string{true: "true", false: "false"}[c.payable] + `,"payment":null}}`)
		var got struct {
			ID       string
			Delivery struct {
				State   string
				Payable *bool
				Quote   struct {
					AmountMinor int64 `json:"amount_minor"`
				}
			}
		}
		if err := json.Unmarshal(s.withDeliveryClock(raw), &got); err != nil {
			t.Fatal(err)
		}
		if got.ID != "o" || got.Delivery.State != c.wantState || got.Delivery.Payable == nil || *got.Delivery.Payable != c.wantPayable ||
			got.Delivery.Quote.AmountMinor != 12000 {
			t.Errorf("%s: %+v", name, got)
		}
	}
	for _, raw := range []string{`{"id":"o","delivery":null}`, `{"id":"o","delivery":{"state":"awaiting_quote","quote":null}}`,
		`{"id":"o","delivery":{"state":"paid"}}`, `{"z":1,"a":2}`} {
		if got := string(s.withDeliveryClock([]byte(raw))); got != raw {
			t.Errorf("%s rewritten as %s", raw, got)
		}
	}
}

func TestDeliveryInputs(t *testing.T) {
	note := func(s string) *string { return &s }
	for name, c := range map[string]struct {
		in    interface{ Validate() error }
		field string
	}{
		"quote ok":             {DeliveryQuoteInput{ExpectedVersion: 1, AmountMinor: 10000}, ""},
		"quote with note":      {DeliveryQuoteInput{ExpectedVersion: 1, AmountMinor: 12000, Note: note("Route via A13")}, ""},
		"quote at the bound":   {DeliveryQuoteInput{ExpectedVersion: 1, AmountMinor: MaxDeliveryMinor}, ""},
		"quote under £100":     {DeliveryQuoteInput{ExpectedVersion: 1, AmountMinor: 9999}, "amount_minor"},
		"quote over the bound": {DeliveryQuoteInput{ExpectedVersion: 1, AmountMinor: MaxDeliveryMinor + 1}, "amount_minor"},
		"quote no version":     {DeliveryQuoteInput{AmountMinor: 12000}, "expected_version"},
		"quote long note":      {DeliveryQuoteInput{ExpectedVersion: 1, AmountMinor: 12000, Note: note(strings.Repeat("a", 501))}, "note"},
		"quote control note":   {DeliveryQuoteInput{ExpectedVersion: 1, AmountMinor: 12000, Note: note("a\u2028b")}, "note"},
		"waiver ok":            {DeliveryWaiverInput{ExpectedVersion: 1, DistanceM: 1, DistanceSource: "Google Maps"}, ""},
		"waiver far ok":        {DeliveryWaiverInput{ExpectedVersion: 1, DistanceM: MaxDistanceM, DistanceSource: "odometer"}, ""},
		"waiver too far":       {DeliveryWaiverInput{ExpectedVersion: 1, DistanceM: MaxDistanceM + 1, DistanceSource: "map"}, "distance_m"},
		"waiver no source":     {DeliveryWaiverInput{ExpectedVersion: 1, DistanceM: 10}, "distance_source"},
		"waiver long source":   {DeliveryWaiverInput{ExpectedVersion: 1, DistanceM: 10, DistanceSource: strings.Repeat("m", 201)}, "distance_source"},
		"payment ok":           {DeliveryPaymentInput{ExpectedVersion: 2, QuoteID: "cccccccc-1111-4111-8111-11111111111c"}, ""},
		"payment upper-case":   {DeliveryPaymentInput{ExpectedVersion: 2, QuoteID: "CCCCCCCC-1111-4111-8111-11111111111C"}, "quote_id"},
		"payment no version":   {DeliveryPaymentInput{QuoteID: "cccccccc-1111-4111-8111-11111111111c"}, "expected_version"},
	} {
		err := c.in.Validate()
		var inv *catalog.InvalidError
		if (c.field == "") != (err == nil) || (c.field != "" && (!errors.As(err, &inv) || inv.Field != c.field)) {
			t.Errorf("%s: %v, want field %q", name, err, c.field)
		}
	}
}
