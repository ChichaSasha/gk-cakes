package schedule

import (
	"errors"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/jackc/pgx/v5/pgconn"
)

func iptr(v int) *int { return &v }

func validVersion() VersionInput {
	return VersionInput{ExpectedRevision: iptr(0), StandardLimit: iptr(7), BookableUntil: "2027-01-05",
		Weekdays: []WeekdayInput{
			{Weekday: "saturday", Opens: "12:30", Closes: "15:00", Starts: []string{"12:30", "13:00", "13:30", "14:00"}},
			{Weekday: "sunday", Opens: "11:00", Closes: "12:00", Starts: []string{"11:00"}},
		},
		ClosedDates: []string{"2026-12-25"}}
}

func TestVersionValidation(t *testing.T) {
	if err := validVersion().Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for name, mutate := range map[string]func(*VersionInput){
		"negative revision": func(in *VersionInput) { in.ExpectedRevision = iptr(-1) },
		"no revision":       func(in *VersionInput) { in.ExpectedRevision = nil },
		"limit 101":         func(in *VersionInput) { in.StandardLimit = iptr(101) },
		"no limit":          func(in *VersionInput) { in.StandardLimit = nil },
		"clock change hour": func(in *VersionInput) {
			in.Weekdays[1] = WeekdayInput{Weekday: "sunday", Opens: "00:00", Closes: "03:00", Starts: []string{"01:30"}}
		},
		"touches 01:00": func(in *VersionInput) {
			in.Weekdays[1] = WeekdayInput{Weekday: "sunday", Opens: "00:00", Closes: "03:00", Starts: []string{"00:30"}}
		},
		"past midnight": func(in *VersionInput) {
			in.Weekdays[1] = WeekdayInput{Weekday: "sunday", Opens: "00:00", Closes: "00:30", Starts: []string{"00:00"}}
		},
		"no horizon":       func(in *VersionInput) { in.BookableUntil = "" },
		"bad horizon":      func(in *VersionInput) { in.BookableUntil = "2027-02-30" },
		"unknown weekday":  func(in *VersionInput) { in.Weekdays[0].Weekday = "Saturday" },
		"repeated weekday": func(in *VersionInput) { in.Weekdays[1].Weekday = "saturday" },
		"bad clock":        func(in *VersionInput) { in.Weekdays[0].Opens = "24:00" },
		"closes first":     func(in *VersionInput) { in.Weekdays[0].Closes = "12:00" },
		"ends after close": func(in *VersionInput) { in.Weekdays[0].Starts = []string{"14:30"} }, // K06 case S05
		"before opening":   func(in *VersionInput) { in.Weekdays[0].Starts = []string{"12:00"} },
		"repeated start":   func(in *VersionInput) { in.Weekdays[0].Starts = []string{"13:00", "13:00"} },
		"one-digit hour":   func(in *VersionInput) { in.Weekdays[1].Starts = []string{"9:00"} },
		"bad closed date":  func(in *VersionInput) { in.ClosedDates = []string{"25/12/2026"} },
		"repeated closed":  func(in *VersionInput) { in.ClosedDates = []string{"2026-12-25", "2026-12-25"} },
	} {
		in := validVersion()
		mutate(&in)
		var inv *catalog.InvalidError
		if err := in.Validate(); !errors.As(err, &inv) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	// 00:00 ends at 01:00, before the clocks change: allowed.
	in0 := validVersion()
	in0.Weekdays[1] = WeekdayInput{Weekday: "sunday", Opens: "00:00", Closes: "01:00", Starts: []string{"00:00"}}
	if err := in0.Validate(); err != nil {
		t.Errorf("00:00-01:00: %v", err)
	}
	// The last start that ends exactly at closing is allowed (Sat 14:00-15:00).
	in := validVersion()
	in.Weekdays[0].Starts = []string{"14:00"}
	if err := in.Validate(); err != nil {
		t.Errorf("14:00 on Saturday: %v", err)
	}
}

func TestLotValidation(t *testing.T) {
	p := func(v int64) *int64 { return &v }
	v := "aaaaaaaa-1111-4111-8111-11111111111b"
	exp := "2026-11-10T12:00:00Z"
	ok := []LotInput{
		{VariantID: v, Kind: LotReady, Quantity: 1, PriceMinor: p(9000), ExpiresAt: exp},
		{VariantID: v, Kind: LotResale, Quantity: 1, ResaleBaseMinor: p(9000), OriginalHandoverAt: "2026-11-07T13:00:00+00:00", ExpiresAt: exp},
	}
	for i, in := range ok {
		if err := in.Validate(); err != nil {
			t.Errorf("valid %d: %v", i, err)
		}
	}
	bad := map[string]LotInput{
		"no price":           {VariantID: v, Kind: LotReady, Quantity: 1, ExpiresAt: exp},
		"resale with price":  {VariantID: v, Kind: LotResale, Quantity: 1, PriceMinor: p(1), ResaleBaseMinor: p(2), OriginalHandoverAt: exp, ExpiresAt: exp},
		"resale no original": {VariantID: v, Kind: LotResale, Quantity: 1, ResaleBaseMinor: p(2), ExpiresAt: exp},
		"ready with base":    {VariantID: v, Kind: LotReady, Quantity: 1, PriceMinor: p(1), ResaleBaseMinor: p(2), ExpiresAt: exp},
		"zero quantity":      {VariantID: v, Kind: LotReady, Quantity: 0, PriceMinor: p(1), ExpiresAt: exp},
		"no offset":          {VariantID: v, Kind: LotReady, Quantity: 1, PriceMinor: p(1), ExpiresAt: "2026-11-10T12:00:00"},
		"unknown kind":       {VariantID: v, Kind: "resale_ready", Quantity: 1, PriceMinor: p(1), ExpiresAt: exp},
		"bad variant":        {VariantID: "x", Kind: LotReady, Quantity: 1, PriceMinor: p(1), ExpiresAt: exp},
	}
	for name, in := range bad {
		if err := in.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := (DayLimitInput{ExpectedVersion: iptr(0), StandardLimit: iptr(5)}).Validate(); err != nil {
		t.Errorf("first override: %v", err)
	}
	if err := (DayLimitInput{ExpectedVersion: iptr(2), UseScheduleDefault: true}).Validate(); err != nil {
		t.Errorf("clear override: %v", err)
	}
	for name, in := range map[string]DayLimitInput{
		"negative limit":    {ExpectedVersion: iptr(0), StandardLimit: iptr(-1)},
		"omitted limit":     {ExpectedVersion: iptr(0)},
		"omitted version":   {StandardLimit: iptr(5)},
		"limit and default": {ExpectedVersion: iptr(1), StandardLimit: iptr(5), UseScheduleDefault: true},
	} {
		if err := in.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := (LotStateInput{ExpectedVersion: 1, Action: "sell"}).Validate(); err == nil {
		t.Error("unknown action accepted")
	}
}

func TestRangeAndDates(t *testing.T) {
	from, _ := ParseDate("from", "2026-11-01")
	if err := CheckRange(from, from.AddDate(0, 0, 30)); err != nil {
		t.Errorf("31 dates: %v", err)
	}
	if err := CheckRange(from, from.AddDate(0, 0, 31)); err == nil {
		t.Error("32 dates accepted")
	}
	if err := CheckRange(from, from.AddDate(0, 0, -1)); err == nil {
		t.Error("reversed range accepted")
	}
	for _, s := range []string{"2026-11-1", "2026-02-29", "20261101", " 2026-11-01"} {
		if _, err := ParseDate("d", s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	if _, err := ParseDate("d", "2028-02-29"); err != nil {
		t.Error("leap day refused")
	}
	_ = time.UTC
}

func TestGatewayErrors(t *testing.T) {
	var c *catalog.ConflictError
	if err := gatewayError("x", &pgconn.PgError{Code: "GK409", Message: "day_full"}); !errors.As(err, &c) || c.Code != "state_conflict" {
		t.Errorf("day_full: %v", err)
	}
	var inv *catalog.InvalidError
	if err := gatewayError("x", &pgconn.PgError{Code: "GK422", Message: "starts_invalid"}); !errors.As(err, &inv) || inv.Field != "weekdays" {
		t.Errorf("starts_invalid: %v", err)
	}
	if err := gatewayError("x", &pgconn.PgError{Code: "42501"}); errors.As(err, &inv) || errors.As(err, &c) {
		t.Errorf("42501 is a fault: %v", err)
	}
}
