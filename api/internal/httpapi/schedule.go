package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/catalog"
	"github.com/ChichaSasha/gk-cakes/api/internal/checkout"
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// Schedule serves schedules, day capacity and finished cake lots
// (schedule.Store).
type Schedule interface {
	Availability(ctx context.Context, from, to time.Time, quantity int) (schedule.Availability, error)
	Choices(ctx context.Context, day time.Time, mode string, quantity int) (schedule.Choices, error)
	MoveChoices(ctx context.Context, subject string, digest []byte, orderID string) (schedule.Choices, error)
	PublicLots(ctx context.Context) ([]schedule.Lot, error)
	CurrentVersion(ctx context.Context, subject string) (schedule.Version, error)
	VersionByID(ctx context.Context, subject, id string) (schedule.Version, error)
	StaffLot(ctx context.Context, subject, id string) (schedule.Lot, error)
	StaffDays(ctx context.Context, subject string, from, to time.Time) ([]schedule.StaffDay, error)
	StaffLots(ctx context.Context, subject string, includeInactive bool) ([]schedule.Lot, error)
	StaffChoices(ctx context.Context, subject, orderID string, day time.Time) (schedule.StaffChoices, error)
	PublishVersion(ctx context.Context, subject, key string, in schedule.VersionInput) (catalog.Result, error)
	SetDayLimit(ctx context.Context, subject, key string, day time.Time, in schedule.DayLimitInput) (catalog.Result, error)
	CreateLot(ctx context.Context, subject, key string, in schedule.LotInput) (catalog.Result, error)
	SetLotState(ctx context.Context, subject, key, lotID string, in schedule.LotStateInput) (catalog.Result, error)
}

var quantityPattern = regexp.MustCompile(`^[0-9]{1,2}$`)

// queryQuantity reads ?quantity= (1 to 20, default 1).
func queryQuantity(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("quantity")
	if raw == "" {
		return 1, true
	}
	n, err := strconv.Atoi(raw)
	if !quantityPattern.MatchString(raw) || err != nil || n < 1 || n > 20 {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "quantity must be 1 to 20", false)
		return 0, false
	}
	return n, true
}

func queryDate(w http.ResponseWriter, r *http.Request, name string) (time.Time, bool) {
	d, err := schedule.ParseDate(name, r.URL.Query().Get(name))
	if err != nil {
		catalogError(w, r, slog.Default(), err)
		return time.Time{}, false
	}
	return d, true
}

func queryRange(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	from, ok := queryDate(w, r, "from")
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	to, ok := queryDate(w, r, "to")
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	if err := schedule.CheckRange(from, to); err != nil {
		catalogError(w, r, slog.Default(), err)
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

// schedulePublicRoutes registers the customer schedule reads (no sign-in;
// browsing creates no hold). purpose=pickup_move needs the order's owner
// (bearer) or its guest capability with move_pickup.
func schedulePublicRoutes(mux *http.ServeMux, sch Schedule, id Identity, logger *slog.Logger) {
	mux.HandleFunc("GET /api/v1/availability", func(w http.ResponseWriter, r *http.Request) {
		from, to, ok := queryRange(w, r)
		if !ok {
			return
		}
		q, ok := queryQuantity(w, r)
		if !ok {
			return
		}
		a, err := sch.Availability(r.Context(), from, to, q)
		respond(w, r, logger, http.StatusOK, a, err)
	})
	mux.HandleFunc("GET /api/v1/schedule", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("purpose") == schedule.PurposePickupMove {
			moveChoices(w, r, sch, id, logger)
			return
		}
		day, ok := queryDate(w, r, "date")
		if !ok {
			return
		}
		mode := r.URL.Query().Get("fulfillment_mode")
		if !slices.Contains(schedule.Modes, mode) {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT",
				"fulfillment_mode must be collection, customer_courier or gk_delivery", false)
			return
		}
		// New purchases, or an owned pickup move (above); an existing
		// checkout keeps the time on its quote and order.
		if p := r.URL.Query().Get("purpose"); p != "" && p != "new_purchase" {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "purpose must be new_purchase or pickup_move", false)
			return
		}
		q, ok := queryQuantity(w, r)
		if !ok {
			return
		}
		c, err := sch.Choices(r.Context(), day, mode, q)
		respond(w, r, logger, http.StatusOK, c, err)
	})
	mux.HandleFunc("GET /api/v1/ready-cakes", func(w http.ResponseWriter, r *http.Request) {
		lots, err := sch.PublicLots(r.Context())
		respond(w, r, logger, http.StatusOK, map[string]any{"cakes": lots}, err)
	})
}

// moveChoices answers GET /schedule?purpose=pickup_move&order_id=...: the
// same-day move choices of the caller's order (K06). date and
// fulfillment_mode are optional and must be the order's when sent (a date or
// mode change is the chef's, never this flow); quantity does not apply.
func moveChoices(w http.ResponseWriter, r *http.Request, sch Schedule, id Identity, logger *slog.Logger) {
	q := r.URL.Query()
	for k := range q {
		if !slices.Contains([]string{"purpose", "order_id", "date", "fulfillment_mode"}, k) {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", k+" does not apply to purpose=pickup_move", false)
			return
		}
	}
	orderID := q.Get("order_id")
	if !catalog.ValidUUID(orderID) {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "order_id must be the order's ID", false)
		return
	}
	a, signedIn, ok := optionalCustomer(id, w, r, logger)
	if !ok {
		return
	}
	var digest []byte
	if !signedIn {
		d, err := checkout.GuestProofDigest(r.Header.Get(OrderCapabilityHeader))
		if err != nil {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", false)
			return
		}
		digest = d
	}
	c, err := sch.MoveChoices(r.Context(), a.Subject, digest, orderID)
	if err != nil {
		catalogError(w, r, logger, err)
		return
	}
	if (q.Has("date") && q.Get("date") != c.Date) || (q.Has("fulfillment_mode") && q.Get("fulfillment_mode") != c.FulfillmentMode) {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT",
			"a pickup move keeps the order's date and fulfillment mode; ask the chef to change them", false)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// scheduleStaffRoutes registers the chef and fulfilment schedule routes.
// Reads: chef, fulfilment, technical support; changes: chef.
func scheduleStaffRoutes(mux *http.ServeMux, sch Schedule, id Identity, logger *slog.Logger) {
	readers := []string{identity.Chef, identity.Fulfillment, identity.TechnicalSupport}
	chef := []string{identity.Chef}

	mux.HandleFunc("GET /api/v1/admin/schedule", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		v, err := sch.CurrentVersion(r.Context(), a.Subject)
		respond(w, r, logger, http.StatusOK, v, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/days", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		from, to, ok := queryRange(w, r)
		if !ok {
			return
		}
		days, err := sch.StaffDays(r.Context(), a.Subject, from, to)
		respond(w, r, logger, http.StatusOK, map[string]any{"timezone": schedule.Timezone, "days": days}, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/lots", staff(id, logger, readers, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		inactive := r.URL.Query().Get("include_inactive")
		if inactive != "" && inactive != "true" && inactive != "false" {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "include_inactive must be true or false", false)
			return
		}
		lots, err := sch.StaffLots(r.Context(), a.Subject, inactive == "true")
		respond(w, r, logger, http.StatusOK, map[string]any{"lots": lots}, err)
	}))
	// The chef's choices of one day for one order: what the appointment
	// change (or, for an order paid late, an offer of another date) would
	// accept now. Reads only; date and order_id are required, nothing else
	// applies.
	mux.HandleFunc("GET /api/v1/admin/schedule/choices", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		q := r.URL.Query()
		for k := range q {
			if k != "date" && k != "order_id" {
				writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", k+" does not apply to staff schedule choices", false)
				return
			}
		}
		day, ok := queryDate(w, r, "date")
		if !ok {
			return
		}
		orderID := q.Get("order_id")
		if !catalog.ValidUUID(orderID) {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "order_id must be the order's ID", false)
			return
		}
		c, err := sch.StaffChoices(r.Context(), a.Subject, orderID, day)
		respond(w, r, logger, http.StatusOK, c, err)
	}))

	mux.HandleFunc("POST /api/v1/admin/schedule/versions", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in schedule.VersionInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := sch.PublishVersion(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return sch.VersionByID(r.Context(), a.Subject, res.ID) // the saved version, also on a replay
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/days/{date}/limit", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		day, err := schedule.ParseDate("date", r.PathValue("date"))
		if err != nil {
			catalogError(w, r, logger, err)
			return
		}
		var in schedule.DayLimitInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := sch.SetDayLimit(r.Context(), a.Subject, key, day, in)
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			days, err := sch.StaffDays(r.Context(), a.Subject, day, day)
			if err != nil || len(days) != 1 {
				return nil, err
			}
			return days[0], nil
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/lots", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in schedule.LotInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := sch.CreateLot(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return sch.StaffLot(r.Context(), a.Subject, res.ID)
		})
	}))
	mux.HandleFunc("POST /api/v1/admin/lots/{id}/state", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in schedule.LotStateInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := sch.SetLotState(r.Context(), a.Subject, key, r.PathValue("id"), in)
		respondCommand(w, r, logger, http.StatusOK, res, err, func() (any, error) {
			return sch.StaffLot(r.Context(), a.Subject, res.ID)
		})
	}))
}
