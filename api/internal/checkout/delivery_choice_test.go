package checkout

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

func TestDeliveryChoiceInputs(t *testing.T) {
	slot := "8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"
	reason := "Van off the road"
	for name, c := range map[string]struct {
		in    validator
		field string
	}{
		"not possible":                {DeliveryNotPossibleInput{ExpectedVersion: 3}, ""},
		"not possible with reason":    {DeliveryNotPossibleInput{ExpectedVersion: 3, Reason: &reason}, ""},
		"not possible no version":     {DeliveryNotPossibleInput{}, "expected_version"},
		"not possible empty reason":   {DeliveryNotPossibleInput{ExpectedVersion: 3, Reason: new(string)}, "reason"},
		"not possible two lines":      {DeliveryNotPossibleInput{ExpectedVersion: 3, Reason: ptr("a\nb")}, "reason"},
		"not possible long reason":    {DeliveryNotPossibleInput{ExpectedVersion: 3, Reason: ptr(strings.Repeat("a", 501))}, "reason"},
		"collection":                  {DeliveryChoiceInput{ExpectedVersion: 5, Choice: ChoiceCollection, TargetSlotID: slot}, ""},
		"courier":                     {DeliveryChoiceInput{ExpectedVersion: 5, Choice: ChoiceCourier, TargetSlotID: slot}, ""},
		"full refund":                 {DeliveryChoiceInput{ExpectedVersion: 5, Choice: ChoiceFullRefund}, ""},
		"no version":                  {DeliveryChoiceInput{Choice: ChoiceFullRefund}, "expected_version"},
		"unknown choice":              {DeliveryChoiceInput{ExpectedVersion: 5, Choice: "redeliver"}, "choice"},
		"time without slot":           {DeliveryChoiceInput{ExpectedVersion: 5, Choice: ChoiceCollection}, "target_slot_id"},
		"time with upper-case slot":   {DeliveryChoiceInput{ExpectedVersion: 5, Choice: ChoiceCourier, TargetSlotID: strings.ToUpper(slot)}, "target_slot_id"},
		"full refund with a slot":     {DeliveryChoiceInput{ExpectedVersion: 5, Choice: ChoiceFullRefund, TargetSlotID: slot}, "target_slot_id"},
		"not possible max reason len": {DeliveryNotPossibleInput{ExpectedVersion: 3, Reason: ptr(strings.Repeat("é", 500))}, ""},
	} {
		err := c.in.Validate()
		if got := fieldOf(err); got != c.field {
			t.Errorf("%s: %v, want field %q", name, err, c.field)
		}
	}
}

type validator interface{ Validate() error }

func ptr(s string) *string { return &s }

func fieldOf(err error) string {
	if err == nil {
		return ""
	}
	var inv *catalog.InvalidError
	if errors.As(err, &inv) {
		return inv.Field
	}
	return "?"
}

// The block's clock-dependent parts follow the store's clock: an unpaid
// quote's choice opens at its pay-by (equality has passed) unless a
// protected change is pending; choices and times only while open, times
// after now only; a chef decision keeps its reason; blocks without the
// keys stay as they are.
func TestWithDeliveryChoiceClock(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 30, 0, 0, time.UTC)
	s := &Store{Now: func() time.Time { return now }}
	times := `[{"id":"a","starts_at":"2026-11-12T12:30:00Z","ends_at":"2026-11-12T13:30:00Z"},` +
		`{"id":"b","starts_at":"2026-11-12T13:00:00Z","ends_at":"2026-11-12T14:00:00Z"}]`
	type block struct {
		Open    bool
		Reason  *string
		Choices []string
		Times   []struct{ ID string }
		Chosen  json.RawMessage
	}
	for name, c := range map[string]struct {
		open, reason, opensAt, pending string
		wantOpen                       bool
		wantReason                     string
		wantTimes                      string
	}{
		"before pay-by":            {"false", "null", `"2026-11-12T12:30:01Z"`, "false", false, "", ""},
		"at pay-by":                {"false", "null", `"2026-11-12T12:30:00Z"`, "false", true, "pay_by_passed", "b"},
		"database passed, api not": {"true", `"pay_by_passed"`, `"2026-11-12T13:00:00Z"`, "false", false, "", ""},
		"passed but pending":       {"false", `"pay_by_passed"`, `"2026-11-12T11:00:00Z"`, "true", false, "pay_by_passed", ""},
		"chef decision":            {"true", `"not_possible"`, "null", "false", true, "not_possible", "b"},
		"chef decision pending":    {"false", `"not_possible"`, "null", "true", false, "not_possible", ""},
		"closed":                   {"false", "null", "null", "false", false, "", ""},
	} {
		raw := `{"id":"o","delivery_choice":{"open":` + c.open + `,"reason":` + c.reason + `,"opens_at":` + c.opensAt +
			`,"pending":` + c.pending + `,"choices":["collection","customer_courier","full_refund"],"times":` + times +
			`,"chosen":{"choice":"collection","state":"refused"}}}`
		var got struct {
			ID             string
			DeliveryChoice block `json:"delivery_choice"`
		}
		if err := json.Unmarshal(s.withDeliveryChoiceClock([]byte(raw)), &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b := got.DeliveryChoice
		reason := ""
		if b.Reason != nil {
			reason = *b.Reason
		}
		var ids []string
		for _, t := range b.Times {
			ids = append(ids, t.ID)
		}
		if got.ID != "o" || b.Open != c.wantOpen || reason != c.wantReason || strings.Join(ids, ",") != c.wantTimes ||
			(b.Open != (len(b.Choices) == 3)) || b.Choices == nil || b.Times == nil ||
			string(b.Chosen) != `{"choice":"collection","state":"refused"}` {
			t.Errorf("%s: %+v", name, b)
		}
	}
	// Fulfilment's block (no choices or times) follows the clock too.
	raw := `{"delivery_choice":{"open":false,"reason":null,"opens_at":"2026-11-12T12:00:00Z","pending":false,"chosen":null}}`
	if got := string(s.withDeliveryChoiceClock([]byte(raw))); got !=
		`{"delivery_choice":{"chosen":null,"open":true,"opens_at":"2026-11-12T12:00:00Z","pending":false,"reason":"pay_by_passed"}}` {
		t.Errorf("fulfilment block: %s", got)
	}
	for _, raw := range []string{`{"id":"o","delivery_choice":null}`, `{"id":"o"}`, `{"z":1,"a":2}`,
		`{"id":"o","delivery_choice":{"open":true}}`} {
		if got := string(s.withDeliveryChoiceClock([]byte(raw))); got != raw {
			t.Errorf("%s rewritten as %s", raw, got)
		}
	}
}
