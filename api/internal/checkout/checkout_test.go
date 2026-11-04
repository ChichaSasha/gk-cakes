package checkout

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/jackc/pgx/v5/pgconn"
)

func p(v int) *int { return &v }

func TestGuestProof(t *testing.T) {
	raw := make([]byte, 32)
	raw[0] = 7
	proof := base64.RawURLEncoding.EncodeToString(raw)
	d, err := GuestProofDigest(proof)
	want := sha256.Sum256(raw)
	if err != nil || string(d) != string(want[:]) {
		t.Fatalf("digest = %x, %v", d, err)
	}
	for _, bad := range []string{"", proof[:42], proof + "A", base64.StdEncoding.EncodeToString(raw), strings.Repeat("=", 43), proof[:42] + "B"} {
		if _, err := GuestProofDigest(bad); err == nil {
			t.Errorf("proof %q accepted", bad)
		}
	}
}

func TestCodeValidation(t *testing.T) {
	ok := []CodeInput{
		{Code: " spring10 ", CodeTerms: CodeTerms{Kind: KindPercentage, BasisPoints: p(1000)}},
		{Code: "FREEDELIVERY", CodeTerms: CodeTerms{Kind: KindFreeDelivery, TotalLimit: p(50), PerCustomerLimit: p(1)}},
		{Code: "ALL", CodeTerms: CodeTerms{Kind: KindPercentage, BasisPoints: p(10000), ExpiresAt: "2026-12-31T23:59:59Z"}},
	}
	for i, in := range ok {
		if err := in.Validate(); err != nil {
			t.Errorf("valid %d: %v", i, err)
		}
	}
	for name, in := range map[string]CodeInput{
		"short":           {Code: "AB", CodeTerms: CodeTerms{Kind: KindFreeDelivery}},
		"space inside":    {Code: "SPRING 10", CodeTerms: CodeTerms{Kind: KindFreeDelivery}},
		"no bps":          {Code: "PCT", CodeTerms: CodeTerms{Kind: KindPercentage}},
		"zero bps":        {Code: "PCT", CodeTerms: CodeTerms{Kind: KindPercentage, BasisPoints: p(0)}},
		"over 100%":       {Code: "PCT", CodeTerms: CodeTerms{Kind: KindPercentage, BasisPoints: p(10001)}},
		"delivery bps":    {Code: "FD", CodeTerms: CodeTerms{Kind: KindFreeDelivery, BasisPoints: p(10)}},
		"fixed amount":    {Code: "FIX", CodeTerms: CodeTerms{Kind: "fixed"}},
		"bad expiry":      {Code: "EXP", CodeTerms: CodeTerms{Kind: KindFreeDelivery, ExpiresAt: "2026-12-31"}},
		"zero limit":      {Code: "LIM", CodeTerms: CodeTerms{Kind: KindFreeDelivery, TotalLimit: p(0)}},
		"zero per person": {Code: "LIM", CodeTerms: CodeTerms{Kind: KindFreeDelivery, PerCustomerLimit: p(0)}},
	} {
		if err := in.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := (CodeRevision{CodeTerms: CodeTerms{Kind: KindFreeDelivery}}).Validate(); err == nil {
		t.Error("revision without a version accepted")
	}
}

func TestQuoteValidation(t *testing.T) {
	v := "aaaaaaaa-1111-4111-8111-11111111111b"
	l := "bbbbbbbb-1111-4111-8111-11111111111b"
	base := func() QuoteInput {
		return QuoteInput{Date: "2026-11-14", SlotID: v, FulfillmentMode: "collection",
			Lines: []QuoteLine{{VariantID: v, Quantity: 1}, {LotID: l, Quantity: 2}}}
	}
	if err := base().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*QuoteInput){
		"bad date":       func(in *QuoteInput) { in.Date = "14/11/2026" },
		"bad slot":       func(in *QuoteInput) { in.SlotID = "slot-1" },
		"bad mode":       func(in *QuoteInput) { in.FulfillmentMode = "post" },
		"no lines":       func(in *QuoteInput) { in.Lines = nil },
		"both ids":       func(in *QuoteInput) { in.Lines[0].LotID = l },
		"no id":          func(in *QuoteInput) { in.Lines[0].VariantID = "" },
		"duplicate":      func(in *QuoteInput) { in.Lines[1] = QuoteLine{VariantID: v, Quantity: 1} },
		"zero quantity":  func(in *QuoteInput) { in.Lines[0].Quantity = 0 },
		"21 cakes":       func(in *QuoteInput) { in.Lines[0].Quantity = 19; in.Lines[1].Quantity = 2 },
		"bad code":       func(in *QuoteInput) { in.DiscountCode = "no!" },
		"too many lines": func(in *QuoteInput) { in.Lines = make([]QuoteLine, 21) },
	} {
		in := base()
		mutate(&in)
		if err := in.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// The guest proof and code case do not change the command body.
	a, b := base(), base()
	a.GuestProof, a.DiscountCode = "proof-a", "spring10"
	b.GuestProof, b.DiscountCode = "proof-b", " SPRING10 "
	if catalog.BodyHash(CmdQuoteCreate, "", a.commandBody()) != catalog.BodyHash(CmdQuoteCreate, "", b.commandBody()) {
		t.Fatal("the body hash depends on the guest proof or code spelling")
	}
}

func TestQuoteErrors(t *testing.T) {
	var c *catalog.ConflictError
	if err := gatewayError("q", &pgconn.PgError{Code: "GK409", Message: "slot_unavailable"}); !errors.As(err, &c) || c.Code != "slot_unavailable" {
		t.Errorf("slot_unavailable: %v", err)
	}
	var inv *catalog.InvalidError
	if err := gatewayError("q", &pgconn.PgError{Code: "GK422", Message: "code_requires_sign_in"}); !errors.As(err, &inv) || inv.Field != "discount_code" {
		t.Errorf("code_requires_sign_in: %v", err)
	}
}
