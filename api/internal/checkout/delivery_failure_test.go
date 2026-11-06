package checkout

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDeliveryFailureInputs(t *testing.T) {
	slot := "8d3f0f8e-5d55-4c3e-9a49-34a0e2a9f001"
	for name, c := range map[string]struct {
		in    validator
		field string
	}{
		"our side":                 {DeliveryFailedInput{ExpectedVersion: 3, Cause: CauseOurSide}, ""},
		"nobody available":         {DeliveryFailedInput{ExpectedVersion: 3, Cause: CauseNobodyAvailable, Reason: ptr("Rang twice")}, ""},
		"no version":               {DeliveryFailedInput{Cause: CauseOurSide}, "expected_version"},
		"no cause":                 {DeliveryFailedInput{ExpectedVersion: 3}, "cause"},
		"unknown cause":            {DeliveryFailedInput{ExpectedVersion: 3, Cause: "weather"}, "cause"},
		"cause in wrong case":      {DeliveryFailedInput{ExpectedVersion: 3, Cause: "Our_Side"}, "cause"},
		"empty reason":             {DeliveryFailedInput{ExpectedVersion: 3, Cause: CauseOurSide, Reason: new(string)}, "reason"},
		"two-line reason":          {DeliveryFailedInput{ExpectedVersion: 3, Cause: CauseOurSide, Reason: ptr("a\nb")}, "reason"},
		"padded reason":            {DeliveryFailedInput{ExpectedVersion: 3, Cause: CauseOurSide, Reason: ptr(" a")}, "reason"},
		"long reason":              {DeliveryFailedInput{ExpectedVersion: 3, Cause: CauseOurSide, Reason: ptr(strings.Repeat("a", 501))}, "reason"},
		"max reason":               {DeliveryFailedInput{ExpectedVersion: 3, Cause: CauseOurSide, Reason: ptr(strings.Repeat("é", 500))}, ""},
		"redelivery":               {DeliveryFailureChoiceInput{ExpectedVersion: 5, Choice: FailureRedelivery}, ""},
		"collection":               {DeliveryFailureChoiceInput{ExpectedVersion: 5, Choice: FailureCollection, TargetSlotID: slot}, ""},
		"choice no version":        {DeliveryFailureChoiceInput{Choice: FailureRedelivery}, "expected_version"},
		"unknown choice":           {DeliveryFailureChoiceInput{ExpectedVersion: 5, Choice: "full_refund"}, "choice"},
		"no choice":                {DeliveryFailureChoiceInput{ExpectedVersion: 5}, "choice"},
		"collection without slot":  {DeliveryFailureChoiceInput{ExpectedVersion: 5, Choice: FailureCollection}, "target_slot_id"},
		"collection upper slot":    {DeliveryFailureChoiceInput{ExpectedVersion: 5, Choice: FailureCollection, TargetSlotID: strings.ToUpper(slot)}, "target_slot_id"},
		"redelivery with a slot":   {DeliveryFailureChoiceInput{ExpectedVersion: 5, Choice: FailureRedelivery, TargetSlotID: slot}, "target_slot_id"},
		"choice in the wrong case": {DeliveryFailureChoiceInput{ExpectedVersion: 5, Choice: "Redelivery"}, "choice"},
	} {
		if got := fieldOf(c.in.Validate()); got != c.field {
			t.Errorf("%s: %v, want field %q", name, c.in.Validate(), c.field)
		}
	}
}

// The block's times follow the store's clock: listed only while the choice
// is open, and only those starting after now (the opening times still ahead
// today); choices only while open; blocks without the keys stay as they are.
func TestWithDeliveryFailureClock(t *testing.T) {
	now := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)
	s := &Store{Now: func() time.Time { return now }}
	times := `[{"id":"a","starts_at":"2026-11-14T12:30:00Z","ends_at":"2026-11-14T13:30:00Z"},` +
		`{"id":"b","starts_at":"2026-11-14T13:00:00Z","ends_at":"2026-11-14T14:00:00Z"},` +
		`{"id":"c","starts_at":"2026-11-14T13:30:00Z","ends_at":"2026-11-14T14:30:00Z"}]`
	for name, c := range map[string]struct {
		open      string
		wantTimes string
	}{
		"open":   {"true", "c"},
		"closed": {"false", ""},
	} {
		raw := `{"id":"o","delivery_failure":{"cause":"our_side","open":` + c.open +
			`,"choices":["redelivery","collection"],"times":` + times + `,"chosen":null,"delivery_paid_minor":12000}}`
		var got struct {
			ID              string
			DeliveryFailure struct {
				Cause             string
				Open              bool
				Choices           []string
				Times             []struct{ ID string }
				DeliveryPaidMinor int64 `json:"delivery_paid_minor"`
			} `json:"delivery_failure"`
		}
		if err := json.Unmarshal(s.withDeliveryFailureClock([]byte(raw)), &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b := got.DeliveryFailure
		var ids []string
		for _, t := range b.Times {
			ids = append(ids, t.ID)
		}
		if got.ID != "o" || b.Cause != "our_side" || strings.Join(ids, ",") != c.wantTimes || b.Choices == nil || b.Times == nil ||
			(b.Open != (len(b.Choices) == 2)) || b.DeliveryPaidMinor != 12000 {
			t.Errorf("%s: %+v", name, b)
		}
	}
	for _, raw := range []string{`{"id":"o","delivery_failure":null}`, `{"id":"o"}`, `{"z":1,"a":2}`,
		`{"id":"o","delivery_failure":{"cause":"nobody_available"}}`} {
		if got := string(s.withDeliveryFailureClock([]byte(raw))); got != raw {
			t.Errorf("%s rewritten as %s", raw, got)
		}
	}
	// Fulfilment's block (no choices or times) keeps its keys.
	raw := `{"delivery_failure":{"cause":"our_side","open":true,"pending":false,"chosen":null}}`
	if got := string(s.withDeliveryFailureClock([]byte(raw))); got !=
		`{"delivery_failure":{"cause":"our_side","chosen":null,"open":true,"pending":false}}` {
		t.Errorf("fulfilment block: %s", got)
	}
}
