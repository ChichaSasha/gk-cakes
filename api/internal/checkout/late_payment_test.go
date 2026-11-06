package checkout

import (
	"encoding/json"
	"testing"
	"time"
)

// acceptable is computed at the store's clock, only for an offered block;
// representations without an offer are passed through byte for byte.
func TestWithAcceptable(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	s := &Store{Now: func() time.Time { return now }}
	for name, c := range map[string]struct {
		state, until string
		want         bool
	}{
		"before":     {"offered", "2026-11-14T12:30:00Z", true},
		"at":         {"offered", "2026-11-14T12:00:00Z", false},
		"after":      {"offered", "2026-11-14T11:00:00Z", false},
		"processing": {"processing", "2026-11-14T12:30:00Z", false},
	} {
		raw := []byte(`{"id":"o","late_payment":{"state":"` + c.state + `","offer":{"id":"f","accept_until":"` + c.until + `"},"paid_minor":9000}}`)
		var got struct {
			ID          string
			LatePayment struct {
				PaidMinor int64 `json:"paid_minor"`
				Offer     struct{ Acceptable *bool }
			} `json:"late_payment"`
		}
		if err := json.Unmarshal(s.withAcceptable(raw), &got); err != nil {
			t.Fatal(err)
		}
		if got.ID != "o" || got.LatePayment.PaidMinor != 9000 || got.LatePayment.Offer.Acceptable == nil ||
			*got.LatePayment.Offer.Acceptable != c.want {
			t.Errorf("%s: %+v", name, got)
		}
	}
	for _, raw := range []string{`{"id":"o","late_payment":null}`, `{"id":"o","late_payment":{"state":"waiting","offer":null}}`,
		`{"z":1,"a":2}`} {
		if got := string(s.withAcceptable([]byte(raw))); got != raw {
			t.Errorf("%s rewritten as %s", raw, got)
		}
	}
}
