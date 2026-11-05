package checkout

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

func TestStaffCursor(t *testing.T) {
	c := staffCursor{handover: time.Date(2026, 11, 14, 12, 30, 0, 0, time.UTC),
		placed: time.Date(2026, 11, 5, 10, 0, 0, 123456000, time.FixedZone("x", 3600)), id: "aaaaaaaa-1111-4111-8111-11111111111b"}
	got, err := decodeStaffCursor(encodeStaffCursor(c))
	if err != nil || !got.handover.Equal(c.handover) || !got.placed.Equal(c.placed) || got.id != c.id {
		t.Fatalf("round trip %+v %v", got, err)
	}
	if got, err := decodeStaffCursor(""); got != nil || err != nil {
		t.Fatal("empty cursor is the first page")
	}
	for _, bad := range []string{"%%%", "bm90LWEtY3Vyc29y", encodeStaffCursor(staffCursor{id: "x"}),
		base64.RawURLEncoding.EncodeToString([]byte("2026-11-14T12:30:00Z|" + c.id)),
		base64.RawURLEncoding.EncodeToString([]byte("2026-11-14T12:30:00Z|yesterday|" + c.id))} {
		if _, err := decodeStaffCursor(bad); !errors.Is(err, catalog.ErrBadCursor) {
			t.Errorf("cursor %q: %v", bad, err)
		}
	}
}

func TestStaffOrderFilter(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	ok := StaffOrderFilter{From: d("2026-11-01"), To: d("2027-02-01"), Lifecycles: []string{"confirmed", "cancellation_pending"}, Limit: 100}
	if err := ok.Validate(); err != nil {
		t.Fatalf("93 days: %v", err)
	}
	for name, f := range map[string]StaffOrderFilter{
		"no from":   {To: d("2026-11-01")},
		"no to":     {From: d("2026-11-01")},
		"reversed":  {From: d("2026-11-02"), To: d("2026-11-01")},
		"94 days":   {From: d("2026-11-01"), To: d("2027-02-02")},
		"lifecycle": {From: d("2026-11-01"), To: d("2026-11-01"), Lifecycles: []string{"paid"}},
		"repeated":  {From: d("2026-11-01"), To: d("2026-11-01"), Lifecycles: []string{"ready", "ready"}},
		"limit":     {From: d("2026-11-01"), To: d("2026-11-01"), Limit: 101},
	} {
		var inv *catalog.InvalidError
		if err := f.Validate(); !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
