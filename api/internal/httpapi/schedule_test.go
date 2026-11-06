package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

type fakeSchedule struct {
	calls    int
	lastMode string
	lastQty  int
	// MoveChoices: the caller it saw, and an error to answer with.
	moveSubject string
	moveDigest  []byte
	moveErr     error
	// StaffChoices: what it was asked, and an error to answer with.
	choicesSubject, choicesOrder, choicesDay string
	choicesErr                               error
}

func (f *fakeSchedule) Availability(_ context.Context, _, _ time.Time, q int) (schedule.Availability, error) {
	f.lastQty = q
	return schedule.Availability{Timezone: schedule.Timezone, Days: []schedule.Day{}}, nil
}
func (f *fakeSchedule) Choices(_ context.Context, _ time.Time, mode string, q int) (schedule.Choices, error) {
	f.lastMode, f.lastQty = mode, q
	return schedule.Choices{Slots: []schedule.Slot{}}, nil
}
func (f *fakeSchedule) MoveChoices(_ context.Context, subject string, digest []byte, orderID string) (schedule.Choices, error) {
	f.moveSubject, f.moveDigest = subject, digest
	if f.moveErr != nil {
		return schedule.Choices{}, f.moveErr
	}
	return schedule.Choices{Purpose: schedule.PurposePickupMove, OrderID: orderID, Date: "2026-11-14",
		FulfillmentMode: schedule.Collection, Slots: []schedule.Slot{}}, nil
}
func (f *fakeSchedule) PublicLots(context.Context) ([]schedule.Lot, error) {
	return []schedule.Lot{}, nil
}
func (f *fakeSchedule) CurrentVersion(context.Context, string) (schedule.Version, error) {
	return schedule.Version{}, catalog.ErrNotFound
}
func (f *fakeSchedule) VersionByID(context.Context, string, string) (schedule.Version, error) {
	return schedule.Version{Revision: 1}, nil
}
func (f *fakeSchedule) StaffLot(_ context.Context, _, id string) (schedule.Lot, error) {
	return schedule.Lot{ID: id}, nil
}
func (f *fakeSchedule) StaffDays(context.Context, string, time.Time, time.Time) ([]schedule.StaffDay, error) {
	return []schedule.StaffDay{{Date: "2026-11-14"}}, nil
}
func (f *fakeSchedule) StaffLots(context.Context, string, bool) ([]schedule.Lot, error) {
	return []schedule.Lot{}, nil
}
func (f *fakeSchedule) StaffChoices(_ context.Context, subject, orderID string, day time.Time) (schedule.StaffChoices, error) {
	f.choicesSubject, f.choicesOrder, f.choicesDay = subject, orderID, day.Format(time.DateOnly)
	if f.choicesErr != nil {
		return schedule.StaffChoices{}, f.choicesErr
	}
	return schedule.StaffChoices{OrderID: orderID, Date: f.choicesDay, Purpose: schedule.PurposeAppointmentChange,
		DayStatus: "open", Slots: []schedule.StaffSlot{}}, nil
}
func (f *fakeSchedule) PublishVersion(context.Context, string, string, schedule.VersionInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "v"}, nil
}
func (f *fakeSchedule) SetDayLimit(context.Context, string, string, time.Time, schedule.DayLimitInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "d"}, nil
}
func (f *fakeSchedule) CreateLot(context.Context, string, string, schedule.LotInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "l"}, nil
}
func (f *fakeSchedule) SetLotState(context.Context, string, string, string, schedule.LotStateInput) (catalog.Result, error) {
	f.calls++
	return catalog.Result{ID: "l"}, nil
}

func serveSchedule(t *testing.T, sch *fakeSchedule, id Identity, method, path, auth, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	var logs bytes.Buffer
	h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Schedule: sch,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPublicScheduleQueries(t *testing.T) {
	sch := &fakeSchedule{}
	for path, status := range map[string]int{
		"/api/v1/availability?from=2026-11-12&to=2026-11-15":                                 200,
		"/api/v1/availability?from=2026-11-12&to=2026-11-15&quantity=20":                     200,
		"/api/v1/availability?from=2026-11-12&to=2026-11-15&quantity=21":                     422,
		"/api/v1/availability?from=2026-11-12&to=2026-11-15&quantity=+1":                     422,
		"/api/v1/availability?from=2026-11-12":                                               422,
		"/api/v1/availability?from=2026-11-15&to=2026-11-12":                                 422,
		"/api/v1/availability?from=2026-11-01&to=2026-12-02":                                 422,
		"/api/v1/schedule?date=2026-11-14&fulfillment_mode=collection":                       200,
		"/api/v1/schedule?date=2026-11-14&fulfillment_mode=gk_delivery&purpose=new_purchase": 200,
		"/api/v1/schedule?date=2026-11-14":                                                   422,
		"/api/v1/schedule?date=2026-11-14&fulfillment_mode=collection&purpose=pickup_move":   422,
		"/api/v1/schedule?date=14-11-2026&fulfillment_mode=collection":                       422,
		"/api/v1/ready-cakes": 200,
	} {
		if rec := serveSchedule(t, sch, nil, http.MethodGet, path, "", "", ""); rec.Code != status {
			t.Errorf("%s: %d %s, want %d", path, rec.Code, rec.Body.String(), status)
		}
	}
	if rec := serveSchedule(t, sch, nil, http.MethodGet, "/api/v1/admin/days?from=2026-11-12&to=2026-11-15", "chef", "", ""); rec.Code != 404 {
		t.Fatalf("staff routes without identity: %d", rec.Code)
	}
}

func TestStaffScheduleAuthorization(t *testing.T) {
	sch := &fakeSchedule{}
	id := catalogIdentity()
	id.actors["fulfillment"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"fulfillment": {"fulfillment", 200}, "support": {"support", 200}, "chef": {"chef", 200}, "customer": {secretToken, 403}, "anonymous": {"", 401}} {
		if rec := serveSchedule(t, sch, id, http.MethodGet, "/api/v1/admin/days?from=2026-11-12&to=2026-11-15", c.auth, "", ""); rec.Code != c.status {
			t.Errorf("day view by %s: %d, want %d", name, rec.Code, c.status)
		}
	}
	body := `{"expected_version":0,"standard_limit":5}`
	for name, c := range map[string]struct {
		auth   string
		status int
	}{"fulfillment": {"fulfillment", 403}, "support": {"support", 403}, "chef": {"chef", 200}} {
		if rec := serveSchedule(t, sch, id, http.MethodPost, "/api/v1/admin/days/2026-11-14/limit", c.auth, "k", body); rec.Code != c.status {
			t.Errorf("limit by %s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	if sch.calls != 1 {
		t.Fatalf("commands reached the store %d times, want once (chef)", sch.calls)
	}
	if rec := serveSchedule(t, sch, id, http.MethodPost, "/api/v1/admin/days/2026-02-30/limit", "chef", "k", body); rec.Code != 422 {
		t.Fatalf("invalid path date: %d", rec.Code)
	}
	if rec := serveSchedule(t, sch, id, http.MethodGet, "/api/v1/admin/lots?include_inactive=yes", "chef", "", ""); rec.Code != 422 {
		t.Fatalf("include_inactive=yes: %d", rec.Code)
	}
	if rec := serveSchedule(t, sch, id, http.MethodGet, "/api/v1/admin/schedule", "chef", "", ""); rec.Code != 404 {
		t.Fatalf("no schedule yet: %d", rec.Code)
	}
	// An omitted limit is refused, not read as 0.
	if rec := serveSchedule(t, sch, id, http.MethodPost, "/api/v1/admin/days/2026-11-14/limit", "chef", "k", `{"expected_version":0}`); rec.Code != 422 || sch.calls != 1 {
		t.Fatalf("omitted limit: %d, calls %d", rec.Code, sch.calls)
	}
	// A resale lot with its own price is refused before the store.
	lot := `{"variant_id":"aaaaaaaa-1111-4111-8111-11111111111b","kind":"resale","quantity":1,"price_minor":100,
		"resale_base_minor":9000,"original_handover_at":"2026-11-07T13:00:00Z","expires_at":"2026-11-10T12:00:00Z"}`
	if rec := serveSchedule(t, sch, id, http.MethodPost, "/api/v1/admin/lots", "chef", "k", lot); rec.Code != 422 || sch.calls != 1 {
		t.Fatalf("resale with a price: %d, calls %d", rec.Code, sch.calls)
	}
}

// GET /schedule?purpose=pickup_move: the order's owner or its guest
// capability; date and mode only as the order's; nothing else applies.
func TestPickupMoveChoicesQuery(t *testing.T) {
	id := catalogIdentity()
	base := "/api/v1/schedule?purpose=pickup_move&order_id=aaaaaaaa-1111-4111-8111-11111111111b"
	serve := func(sch *fakeSchedule, path, auth string, hdr map[string]string) *httptest.ResponseRecorder {
		h := NewHandler(Options{UIOrigin: "https://gkcake.com", DB: fakePinger{}, Identity: id, Schedule: sch,
			Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))})
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	guest := map[string]string{OrderCapabilityHeader: testProof}
	sch := &fakeSchedule{}
	rec := serve(sch, base, "", guest)
	if rec.Code != 200 || len(sch.moveDigest) != 32 || sch.moveSubject != "" || !strings.Contains(rec.Body.String(), `"purpose":"pickup_move"`) ||
		rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("guest choices: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(sch, base, secretToken, nil); rec.Code != 200 || sch.moveDigest != nil || sch.moveSubject == "" {
		t.Fatalf("owner choices: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		path   string
		auth   string
		hdr    map[string]string
		status int
	}{
		"anonymous":         {base, "", nil, 404},
		"malformed proof":   {base, "", map[string]string{OrderCapabilityHeader: "not a proof!"}, 404},
		"forged token":      {base, "forged", nil, 401},
		"no order":          {"/api/v1/schedule?purpose=pickup_move", "", guest, 422},
		"quantity":          {base + "&quantity=2", "", guest, 422},
		"same date":         {base + "&date=2026-11-14&fulfillment_mode=collection", "", guest, 200},
		"another date":      {base + "&date=2026-11-15", "", guest, 422},
		"another mode":      {base + "&fulfillment_mode=gk_delivery", "", guest, 422},
		"order for new buy": {"/api/v1/schedule?date=2026-11-14&fulfillment_mode=collection&order_id=x", "", nil, 200},
	} {
		if rec := serve(&fakeSchedule{}, c.path, c.auth, c.hdr); rec.Code != c.status {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	if rec := serve(&fakeSchedule{moveErr: catalog.ErrNotFound}, base, "", guest); rec.Code != 404 {
		t.Fatalf("not the caller's order: %d", rec.Code)
	}
}

// GET /admin/schedule/choices (0039): the chef only; date and order_id
// required and nothing else; an unknown order is 404.
func TestStaffScheduleChoicesQuery(t *testing.T) {
	id := catalogIdentity()
	id.actors["fulfillment"] = identity.Actor{AccountID: "acc-ful", Subject: "sub-ful", Capabilities: []string{identity.Fulfillment}}
	const order = "aaaaaaaa-1111-4111-8111-11111111111b"
	base := "/api/v1/admin/schedule/choices?date=2026-11-21&order_id=" + order
	sch := &fakeSchedule{}
	rec := serveSchedule(t, sch, id, http.MethodGet, base, "chef", "", "")
	if rec.Code != 200 || sch.choicesSubject != "sub-chef" || sch.choicesOrder != order || sch.choicesDay != "2026-11-21" ||
		!strings.Contains(rec.Body.String(), `"purpose":"appointment_change"`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("chef choices: %d %s %+v", rec.Code, rec.Body.String(), sch)
	}
	for name, c := range map[string]struct {
		path   string
		auth   string
		status int
	}{
		"fulfilment":     {base, "fulfillment", 403},
		"support":        {base, "support", 403},
		"customer":       {base, secretToken, 403},
		"anonymous":      {base, "", 401},
		"no date":        {"/api/v1/admin/schedule/choices?order_id=" + order, "chef", 422},
		"bad date":       {"/api/v1/admin/schedule/choices?date=21-11-2026&order_id=" + order, "chef", 422},
		"no order":       {"/api/v1/admin/schedule/choices?date=2026-11-21", "chef", 422},
		"bad order":      {"/api/v1/admin/schedule/choices?date=2026-11-21&order_id=x", "chef", 422},
		"extra purpose":  {base + "&purpose=new_purchase", "chef", 422},
		"extra quantity": {base + "&quantity=2", "chef", 422},
	} {
		if rec := serveSchedule(t, &fakeSchedule{}, id, http.MethodGet, c.path, c.auth, "", ""); rec.Code != c.status {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.status)
		}
	}
	if rec := serveSchedule(t, &fakeSchedule{choicesErr: catalog.ErrNotFound}, id, http.MethodGet, base, "chef", "", ""); rec.Code != 404 {
		t.Fatalf("unknown order: %d", rec.Code)
	}
	if rec := serveSchedule(t, &fakeSchedule{}, nil, http.MethodGet, base, "chef", "", ""); rec.Code != 404 {
		t.Fatalf("without identity: %d", rec.Code)
	}
}
