//go:build integration

package notifications_test

import (
	"bytes"
	"encoding/json"
	mrand "math/rand/v2"
	"regexp"
	"testing"

	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/jackc/pgx/v5"
)

// sqlText evaluates an owner-internal expression returning text.
func (h *harness) sqlText(sql string, args ...any) string {
	h.t.Helper()
	var out *string
	if err := h.ownerTx(func(tx pgx.Tx) error { return tx.QueryRow(h.ctx, sql, args...).Scan(&out) }); err != nil {
		h.t.Fatalf("%s %v: %v", sql, args, err)
	}
	if out == nil {
		return "<null>"
	}
	return *out
}

func goJSON(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

// Order notices are rendered in SQL (migration 0030): its JSON strings must
// be byte-identical to the engine's canonical encoding, money and London
// times must follow the owner rules, and its address check must never
// accept what the engine refuses.
func TestOrderNoticeHelpersMatchTheEngine(t *testing.T) {
	h := newHarness(t)
	var ctrl []rune
	for r := rune(1); r < 0x20; r++ {
		ctrl = append(ctrl, r)
	}
	samples := []string{"", "plain", `quo"te`, `back\slash`, "tab\tnew\nline\rcr", "\b\f", string(ctrl), "\x7f",
		"<b>&amp;</b>", "\u2028\u2029", "£95.01 × é 😀 · −", "it's", `\u0041`, "GK Cakes <orders@gkcake.com>"}
	alphabet := []rune("ab\"\\\n\r\t\b\f\x01\x1f\x7f<>&'£×é😀\u2028\u2029 ·−/")
	rng := mrand.New(mrand.NewPCG(1, 2))
	for range 300 {
		n := rng.IntN(24)
		r := make([]rune, n)
		for i := range r {
			r[i] = alphabet[rng.IntN(len(alphabet))]
		}
		samples = append(samples, string(r))
	}
	for _, s := range samples {
		if got, want := h.sqlText("SELECT business.notice_json($1)", s), goJSON(t, s); got != want {
			t.Errorf("notice_json(%q) = %s, want %s", s, got, want)
		}
	}
	for minor, want := range map[int64]string{0: "£0", 5: "£0.05", 9501: "£95.01", 7000: "£70", 5293: "£52.93",
		4751: "£47.51", 104050: "£1,040.50", 100000000: "£1,000,000"} {
		if got := h.sqlText("SELECT business.notice_money($1)", minor); got != want {
			t.Errorf("notice_money(%d) = %s, want %s", minor, got, want)
		}
	}
	for at, want := range map[string]string{
		"2026-11-14T12:30:00Z": "Saturday 14 November 2026 12:30", // GMT
		"2027-06-12T11:30:00Z": "Saturday 12 June 2027 12:30",     // BST
		"2027-03-28T00:59:59Z": "Sunday 28 March 2027 00:59",      // the spring change
		"2027-03-28T01:00:00Z": "Sunday 28 March 2027 02:00",
		"2026-10-25T00:30:00Z": "Sunday 25 October 2026 01:30", // the autumn change: BST
		"2026-10-25T01:30:00Z": "Sunday 25 October 2026 01:30", // then GMT
		"2026-12-31T23:59:00Z": "Thursday 31 December 2026 23:59",
	} {
		if got := h.sqlText("SELECT business.notice_day($1::timestamptz) || ' ' || business.notice_time($1::timestamptz)", at); got != want {
			t.Errorf("London time of %s = %s, want %s", at, got, want)
		}
	}
	if got := h.sqlText(`SELECT business.notice_html($1)`, `<a href="x">'&`); got != "&lt;a href=&quot;x&quot;&gt;&#39;&amp;" {
		t.Errorf("notice_html = %s", got)
	}
	for _, addr := range []string{"test.customer@example.com", "a+b@x.co.uk", "o'neil@example.org", "x_y-z@sub-1.example.com",
		"!#$%&*+/=?^_`{|}~-@example.org"} {
		if h.sqlText("SELECT business.notice_address_ok($1)::text", addr) != "true" {
			t.Errorf("notice_address_ok(%q) refused", addr)
		}
		msg := message()
		msg.To = addr
		if _, err := notifications.NewNotice("test", nextUUID(), nextUUID(), "order-confirmed.v1", msg); err != nil {
			t.Errorf("SQL accepts %q but the engine refuses it: %v", addr, err)
		}
	}
	for _, addr := range []string{`a"b@x.co`, "a b@x.co", "a@b", "<a@b.co>", "a@b.co,c@d.co", "élise@x.co", "a..b@x.co",
		".a@x.co", "a.@x.co", "a@x..co", "a@-", "Name <a@b.co>", "a@b.co\n", "a@[127.0.0.1]"} {
		if h.sqlText("SELECT business.notice_address_ok($1)::text", addr) != "false" {
			t.Errorf("notice_address_ok(%q) accepted", addr)
		}
	}
	a, b := h.sqlText("SELECT business.notice_uuid('x')::text"), h.sqlText("SELECT business.notice_uuid('y')::text")
	if a == b || a != h.sqlText("SELECT business.notice_uuid('x')::text") ||
		!regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(a) {
		t.Errorf("notice_uuid %s %s", a, b)
	}
}
