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
	"github.com/ChichaSasha/gk-cakes/api/internal/identity"
	"github.com/ChichaSasha/gk-cakes/api/internal/schedule"
)

// Schedule serves schedules, day capacity and finished cake lots
// (schedule.Store).
type Schedule interface {
	Availability(ctx context.Context, from, to time.Time, quantity int) (schedule.Availability, error)
	Choices(ctx context.Context, day time.Time, mode string, quantity int) (schedule.Choices, error)
	PublicLots(ctx context.Context) ([]schedule.Lot, error)
	CurrentVersion(ctx context.Context, subject string) (schedule.Version, error)
	StaffDays(ctx context.Context, subject string, from, to time.Time) ([]schedule.StaffDay, error)
	StaffLots(ctx context.Context, subject string, includeInactive bool) ([]schedule.Lot, error)
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
// browsing creates no hold).
func schedulePublicRoutes(mux *http.ServeMux, sch Schedule, logger *slog.Logger) {
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
		// Only new purchases until orders exist (GK-025); existing checkouts
		// and pickup moves need an owned order context.
		if p := r.URL.Query().Get("purpose"); p != "" && p != "new_purchase" {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "purpose must be new_purchase", false)
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

	mux.HandleFunc("POST /api/v1/admin/schedule/versions", staff(id, logger, chef, func(w http.ResponseWriter, r *http.Request, a identity.Actor) {
		var in schedule.VersionInput
		key, ok := readCommand(w, r, &in)
		if !ok {
			return
		}
		res, err := sch.PublishVersion(r.Context(), a.Subject, key, in)
		respondCommand(w, r, logger, http.StatusCreated, res, err, func() (any, error) {
			return sch.CurrentVersion(r.Context(), a.Subject)
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
			return staffLot(r.Context(), sch, a.Subject, res.ID)
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
			return staffLot(r.Context(), sch, a.Subject, res.ID)
		})
	}))
}

func staffLot(ctx context.Context, sch Schedule, subject, id string) (any, error) {
	lots, err := sch.StaffLots(ctx, subject, true)
	if err != nil {
		return nil, err
	}
	for _, l := range lots {
		if l.ID == id {
			return l, nil
		}
	}
	return map[string]string{"id": id}, nil
}
