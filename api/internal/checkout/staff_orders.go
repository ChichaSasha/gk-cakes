package checkout

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
)

// Staff order reads (migration 0029; K00 capabilities, K09 access). The
// database renders each view: chef sees everything, technical support no
// money or contact details, fulfillment only orders that were ever visibly
// confirmed, without money, contact or attention flags. Every read is
// audited there without personal data.

const (
	// MaxStaffOrderRangeDays bounds the handover-day range of the list.
	MaxStaffOrderRangeDays = 93
	// DefaultStaffOrderPage and MaxStaffOrderPage bound the page size.
	DefaultStaffOrderPage = 50
	MaxStaffOrderPage     = 100
)

// StaffLifecycles are the visible lifecycles the list filters on (K11:
// confirmation_pending and cancellation_pending as customers see them).
var StaffLifecycles = map[string]bool{
	"awaiting_payment": true, "confirmation_pending": true, "paid_awaiting_agreement": true, "confirmed": true,
	"in_production": true, "ready": true, "completed": true, "cancellation_pending": true, "cancelled": true,
}

// StaffOrderFilter selects the staff order list: London handover days
// From..To (inclusive, at most MaxStaffOrderRangeDays), optionally some
// visible lifecycles and whether the order needs attention (nil: either).
type StaffOrderFilter struct {
	From, To       time.Time
	Lifecycles     []string
	NeedsAttention *bool
	Cursor         string
	Limit          int
}

// Validate checks the filter (the cursor is checked when decoded).
func (f StaffOrderFilter) Validate() error {
	if f.From.IsZero() {
		return invalid("from", "must be a date YYYY-MM-DD")
	}
	if f.To.IsZero() {
		return invalid("to", "must be a date YYYY-MM-DD")
	}
	if f.To.Before(f.From) {
		return invalid("to", "must not be before from")
	}
	if f.To.Sub(f.From) > (MaxStaffOrderRangeDays-1)*24*time.Hour {
		return invalid("to", "the range must be at most %d days", MaxStaffOrderRangeDays)
	}
	seen := map[string]bool{}
	for _, l := range f.Lifecycles {
		if !StaffLifecycles[l] || seen[l] {
			return invalid("lifecycle", "must be distinct visible lifecycles")
		}
		seen[l] = true
	}
	if f.Limit < 0 || f.Limit > MaxStaffOrderPage {
		return invalid("limit", "must be 1 to %d", MaxStaffOrderPage)
	}
	return nil
}

// StaffOrderPage is one page of the staff order list; NextCursor is empty
// on the last page.
type StaffOrderPage struct {
	Items      json.RawMessage `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type staffCursor struct {
	handover, placed time.Time
	id               string
}

func encodeStaffCursor(c staffCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.handover.UTC().Format(time.RFC3339Nano) + "|" +
		c.placed.UTC().Format(time.RFC3339Nano) + "|" + c.id))
}

func decodeStaffCursor(s string) (*staffCursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) > 200 {
		return nil, catalog.ErrBadCursor
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || !catalog.ValidUUID(parts[2]) {
		return nil, catalog.ErrBadCursor
	}
	h, err1 := time.Parse(time.RFC3339Nano, parts[0])
	p, err2 := time.Parse(time.RFC3339Nano, parts[1])
	if err1 != nil || err2 != nil {
		return nil, catalog.ErrBadCursor
	}
	return &staffCursor{handover: h, placed: p, id: parts[2]}, nil
}

// StaffOrders reads a page of the staff order list for the staff member's
// view (chef, technical support or fulfillment).
func (s *Store) StaffOrders(ctx context.Context, subject string, f StaffOrderFilter) (StaffOrderPage, error) {
	if err := f.Validate(); err != nil {
		return StaffOrderPage{}, err
	}
	after, err := decodeStaffCursor(f.Cursor)
	if err != nil {
		return StaffOrderPage{}, err
	}
	limit := f.Limit
	if limit == 0 {
		limit = DefaultStaffOrderPage
	}
	var lifecycles []string
	if len(f.Lifecycles) > 0 {
		lifecycles = f.Lifecycles
	}
	var ah, ap *time.Time
	var aid *string
	if after != nil {
		ah, ap, aid = &after.handover, &after.placed, &after.id
	}
	var raw []byte
	err = s.DB.QueryRow(ctx, `SELECT business.order_staff_list($1::uuid, $2::date, $3::date, $4::text[], $5, $6, $7, $8::uuid, $9)::text`,
		subject, f.From.Format(time.DateOnly), f.To.Format(time.DateOnly), lifecycles, f.NeedsAttention, ah, ap, aid,
		limit).Scan(&raw)
	if err != nil {
		return StaffOrderPage{}, gatewayError("staff orders", err)
	}
	var out struct {
		Items json.RawMessage `json:"items"`
		Next  *struct {
			HandoverAt time.Time `json:"handover_at"`
			PlacedAt   time.Time `json:"placed_at"`
			ID         string    `json:"id"`
		} `json:"next"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return StaffOrderPage{}, gatewayError("staff orders", err)
	}
	page := StaffOrderPage{Items: out.Items}
	if out.Next != nil {
		page.NextCursor = encodeStaffCursor(staffCursor{handover: out.Next.HandoverAt, placed: out.Next.PlacedAt, id: out.Next.ID})
	}
	return page, nil
}

// StaffOrder reads one order for the staff member's view, or ErrNotFound
// (unknown, or outside a fulfillment view's scope).
func (s *Store) StaffOrder(ctx context.Context, subject, id string) (json.RawMessage, error) {
	if !catalog.ValidUUID(id) {
		return nil, catalog.ErrNotFound
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT business.order_staff_get($1::uuid, $2::uuid)::text`, subject, id).Scan(&raw); err != nil {
		return nil, gatewayError("staff order", err)
	}
	return json.RawMessage(raw), nil
}
