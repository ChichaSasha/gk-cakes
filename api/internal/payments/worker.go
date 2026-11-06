package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/jobs"
	"github.com/ChichaSasha/gk-cakes/api/internal/recovery"
	"github.com/jackc/pgx/v5/pgconn"
)

// Waits of the payment jobs (none of them counts as a failure).
const (
	// FreezeRecheck: a restore freezes payment dispatch and recording.
	FreezeRecheck = 5 * time.Minute
	// ProviderRetry: no Stripe key is configured on this worker.
	ProviderRetry = 15 * time.Minute
	// JournalRetry: no recovery journal is configured.
	JournalRetry = 15 * time.Minute
	// ReconcileRecheck: a session that is not decided yet is retrieved
	// again (a webhook brings the check forward).
	ReconcileRecheck = 15 * time.Minute
	// ExpireRetry: an expire request without an answer (no response, 5xx,
	// 409, 429) is asked again with the same key this much later (beyond
	// the 30-second admission guard of migration 0015).
	ExpireRetry = time.Minute
)

// Attention reasons stored on the job (no personal data).
const (
	reasonPayload       = "stored payment payload refused by this build: not dispatched (see its attempts for any earlier ones)"
	reasonAccount       = "payment collection belongs to another Stripe account than this worker's: not dispatched or recorded"
	reasonReconcile     = "payment session outcome unknown (idempotency conflict or window ended): manager reconciliation needed, never a blind new session"
	reasonRecovery      = "recovery operation conflict for this payment collection"
	reasonRefused       = "Stripe refused the request (credentials or permissions): fix the key, then requeue"
	reasonRejected      = "Stripe rejected the Checkout Session request: the collection failed and the customer may start a new one"
	reasonTooLate       = "payment never dispatched before its session would expire (worker delay): the collection failed without a Stripe call; the customer may start a new one"
	reasonMode          = "Checkout Session created in the wrong Stripe mode: collection unknown, never confirmed"
	reasonMismatch      = "Stripe evidence does not match the collection (amount, currency, account, mode or metadata): never confirmed; manager reconciliation"
	reasonRetrieve      = "Stripe refused reading the Checkout Session: check the key, then requeue"
	reasonStale         = "payment succeeded but the order already has a protected operation at this or a later version: nothing recorded; manager reconciliation"
	reasonExpireRefused = "Stripe refused the 15-minute expire request (credentials or permissions): fix the key, then requeue"
	reasonUnderReview   = "payment collection is under manager reconciliation (a recorded mismatch): this check changed nothing; the order's holds stay until a manager resolves it"
)

// Worker is the job handler for payment collections: the protected
// Checkout Session dispatch and the reconcile that verifies Stripe's
// evidence. Jobs of other kinds return jobs.ErrNoHandler.
type Worker struct {
	DB          Querier // gk_worker pool
	Coordinator *recovery.Coordinator
	// Provider is Stripe; nil (no STRIPE_SECRET_KEY) makes payment jobs
	// wait.
	Provider    Provider
	Environment string // APP_ENV; collections of any other are refused
	Account     string // STRIPE_ACCOUNT_ID
	Logger      *slog.Logger
}

// Handle runs the leased payment job.
func (w *Worker) Handle(ctx context.Context, job jobs.Job) error {
	store := leaseStore{db: w.DB, job: job}
	b, err := store.begin(ctx)
	if err != nil {
		return err
	}
	logger := w.Logger
	if logger == nil {
		logger = slog.Default()
	}
	log := logger.With("job_id", job.ID, "generation", job.Generation, "collection_id", b.CollectionID, "purpose", b.Purpose)
	switch b.Status {
	case "ready":
	case "not_payment":
		return jobs.ErrNoHandler
	case "frozen":
		return jobs.Defer(FreezeRecheck, recovery.ErrFrozen)
	case "settled", "no_session":
		log.Info("payment reconcile has nothing to check", "status", b.Status, "collection_state", b.CollectionState)
		return underReview(ctx, store, b)
	case "closed":
		// The never-dispatched cleanup closed the collection before its
		// request was fixed: nothing was or will be sent.
		log.Info("payment dispatch closed before any Stripe call", "collection_state", b.CollectionState)
		return nil
	default:
		return fmt.Errorf("payments: unexpected job status %q", b.Status)
	}
	if w.Provider == nil {
		return jobs.Defer(ProviderRetry, errors.New("payments: no Stripe key configured on this worker"))
	}
	if b.Account != w.Account || b.Environment != w.Environment || b.Livemode != (w.Environment == "production") {
		return jobs.Attention(reasonAccount, nil)
	}
	log = log.With("order_id", b.OrderID)
	if b.Purpose == "dispatch" {
		return w.dispatch(ctx, log, store, b)
	}
	return w.reconcile(ctx, log, store, b)
}

func (w *Worker) dispatch(ctx context.Context, log *slog.Logger, store leaseStore, b begin) error {
	if w.Coordinator == nil {
		return jobs.Defer(JournalRetry, errors.New("payments: no recovery journal configured for payment dispatch"))
	}
	if w.Coordinator.Environment != w.Environment {
		return errors.New("payments: worker and recovery journal must use the same environment")
	}
	kind, err := store.kind(ctx, b.CollectionID)
	if err != nil {
		return err
	}
	if len(b.Payload) == 0 {
		// First dispatch: fix the exact request now, on the database clock.
		payload, err := NewPayload(SessionRequest{CollectionID: b.CollectionID, OrderID: b.OrderID, AmountMinor: b.AmountMinor,
			Environment: b.Environment, UIOrigin: b.ReturnOrigin, ExpiresAt: SessionExpiry(b.DBNow), Kind: kind}, b.Account)
		if err != nil {
			return jobs.Attention(reasonPayload, err)
		}
		status, err := store.fix(ctx, b.CollectionID, payload, b.DBNow, &b)
		switch {
		case err != nil:
			return err
		case status == "frozen":
			return jobs.Defer(FreezeRecheck, recovery.ErrFrozen)
		case status == "closed":
			log.Info("payment dispatch closed before any Stripe call")
			return nil
		case status != "ready":
			return fmt.Errorf("payments: unexpected fix status %q", status)
		}
		log.Info("payment request fixed", "session_expires_at", b.SessionExpiresAt.UTC().Format(time.RFC3339))
	}
	d, err := CheckPayload(b.Payload, Expected{CollectionID: b.CollectionID, OrderID: b.OrderID, AmountMinor: b.AmountMinor,
		Environment: b.Environment, Account: b.Account, Livemode: b.Livemode, UIOrigin: b.ReturnOrigin,
		ExpiresAt: b.SessionExpiresAt, Kind: kind}, w.Environment)
	if err != nil {
		return jobs.Attention(reasonPayload, err)
	}
	op := recovery.Operation{ID: b.CollectionID, Kind: recovery.KindDispatch, AggregateID: b.CollectionID,
		Payload: b.Payload, ReplayUntil: b.ReplayUntil}
	outcome, err := w.Coordinator.Dispatch(ctx, store, op, w.effect(log, store, op.ID, d))
	var (
		ae *admitError
		ce *callError
	)
	switch {
	case err == nil && outcome.Status == string(Rejected):
		var ev struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(outcome.Evidence, &ev) == nil && ev.Reason == reasonExpiredBeforeDispatch {
			return jobs.Attention(reasonTooLate, nil)
		}
		if ev.Reason == reasonNeverDispatched {
			// Closed by the never-dispatched cleanup before any call was
			// admitted: zero Stripe calls; the hold review decides the order.
			log.Info("payment dispatch closed before any Stripe call")
			return nil
		}
		return jobs.Attention(reasonRejected, nil)
	case err == nil:
		var ev createdEvidence
		if json.Unmarshal(outcome.Evidence, &ev) != nil || ev.Livemode != b.Livemode {
			return jobs.Attention(reasonMode, nil)
		}
		log.Info("payment session created", "session_id", ev.SessionID)
		return nil
	case errors.As(err, &ae) && ae.status == "expired",
		errors.As(err, &ce) && ce.res.Kind == Conflict:
		// Never a new session under another key: a manager reconciles.
		if _, rerr := store.MarkReconcile(ctx, op, reconcileReason(err)); rerr != nil {
			return jobs.Attention(reasonReconcile, fmt.Errorf("payments: mark reconcile: %w (after %v)", rerr, err))
		}
		return jobs.Attention(reasonReconcile, err)
	case errors.Is(err, recovery.ErrNeedsReconcile):
		return jobs.Attention(reasonReconcile, err)
	case errors.Is(err, recovery.ErrFrozen), errors.As(err, &ae) && ae.status == "frozen":
		return jobs.Defer(FreezeRecheck, err)
	case errors.Is(err, recovery.ErrBusy), errors.Is(err, recovery.ErrStale):
		return jobs.Attention(reasonRecovery, err)
	case errors.As(err, &ce) && ce.res.Kind == Refused:
		return jobs.Attention(reasonRefused, err)
	default:
		// Unknown or deferred: the same key and body are retried.
		return err
	}
}

func reconcileReason(err error) string {
	var ae *admitError
	if errors.As(err, &ae) {
		return "Stripe idempotency window ended (database clock) before the call"
	}
	return "Stripe reports the idempotency key used with another request"
}

type createdEvidence struct {
	SessionID       string `json:"session_id"`
	URL             string `json:"url"`
	ExpiresAt       int64  `json:"expires_at"`
	Livemode        bool   `json:"livemode"`
	PaymentIntentID string `json:"payment_intent_id,omitempty"`
	SessionStatus   string `json:"session_status,omitempty"`
}

// effect posts the stored request once and records the call. Created and
// rejected are known outcomes; anything else leaves the outcome unknown, so
// the dispatch stays prepared and is replayed with the same key and body.
func (w *Worker) effect(log *slog.Logger, store leaseStore, opID string, d Dispatch) recovery.Effect {
	return func(ctx context.Context) (recovery.Outcome, error) {
		switch status, err := store.admit(ctx, opID); {
		case err != nil:
			return recovery.Outcome{}, fmt.Errorf("payments: admission: %w", err)
		case status == "too_late":
			// No call was ever admitted and Stripe would refuse this one: a
			// definitive failure without a call (nothing can exist at Stripe).
			log.Warn("payment session would expire before dispatch; failed without a Stripe call")
			b, err := json.Marshal(map[string]string{"reason": reasonExpiredBeforeDispatch})
			return recovery.Outcome{Status: string(Rejected), Evidence: b}, err
		case status == "closed":
			// The never-dispatched cleanup won: no call may ever be made.
			b, err := json.Marshal(map[string]string{"reason": reasonNeverDispatched})
			return recovery.Outcome{Status: string(Rejected), Evidence: b}, err
		case status != "ok":
			return recovery.Outcome{}, &admitError{status: status}
		}
		callCtx, cancel := context.WithTimeout(ctx, ProviderTimeout)
		res := w.Provider.CreateSession(callCtx, d.IdempotencyKey, d.Form)
		cancel()
		sessionID := ""
		if res.Session != nil {
			sessionID = sanitize(sessionIDPattern, res.Session.ID)
		}
		log.Info("stripe create session call", "result", string(res.Kind), "http_status", res.Status,
			"provider_code", res.Code, "session_id", sessionID, "error", errString(res.Err))
		if err := store.recordAttempt(ctx, opID, res); err != nil {
			return recovery.Outcome{}, fmt.Errorf("payments: record attempt: %w", err)
		}
		switch res.Kind {
		case Created:
			s := res.Session
			ev := createdEvidence{SessionID: s.ID, URL: s.URL, ExpiresAt: s.ExpiresAt, Livemode: s.Livemode,
				SessionStatus: sanitize(statusPattern, s.Status)}
			if s.PaymentIntent != nil {
				ev.PaymentIntentID = sanitize(piIDPattern, s.PaymentIntent.ID)
			}
			b, err := json.Marshal(ev)
			return recovery.Outcome{Status: string(Created), Evidence: b}, err
		case Rejected:
			b, err := json.Marshal(struct {
				HTTPStatus int    `json:"http_status"`
				Code       string `json:"provider_code,omitempty"`
			}{res.Status, res.Code})
			return recovery.Outcome{Status: string(Rejected), Evidence: b}, err
		default:
			return recovery.Outcome{}, &callError{res: res}
		}
	}
}

// reasonExpiredBeforeDispatch is the outcome evidence of a dispatch refused
// before any call because its session would expire too soon.
const reasonExpiredBeforeDispatch = "expired_before_dispatch"

// reasonNeverDispatched is the outcome evidence of a dispatch whose
// collection the never-dispatched cleanup closed before any call was
// admitted (migration 0015).
const reasonNeverDispatched = "never_dispatched"

// admitError means Stripe was not called: the dispatch was refused
// admission right before the call ("frozen" or "expired").
type admitError struct{ status string }

func (e *admitError) Error() string { return "payment dispatch not admitted: " + e.status }

// callError is a Stripe call whose outcome is not a recordable one.
type callError struct{ res Result }

func (e *callError) Error() string { return e.res.String() }

func (e *callError) Unwrap() error { return e.res.Err }

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// expire makes the K04 15-minute expire request when the database admits
// it (pending, session last seen open, first admitted dispatch at least 15
// minutes ago, not answered yet) and records the answer. A nil error means
// the reconcile goes on to retrieve and verify, which alone decides; Stripe
// refusing because the session is no longer open is fine. retry reports a
// lost or deferred answer: the reconcile still verifies now, and a still
// pending session asks again later with the same key (harmless).
func (w *Worker) expire(ctx context.Context, log *slog.Logger, store leaseStore, b begin) (retry bool, err error) {
	status, err := store.expireAdmit(ctx, b.CollectionID)
	switch {
	case err != nil:
		return false, err
	case status == "frozen":
		return false, jobs.Defer(FreezeRecheck, recovery.ErrFrozen)
	case status != "ok":
		return false, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, ProviderTimeout)
	res := w.Provider.ExpireSession(callCtx, ExpireKey(b.CollectionID), b.SessionID)
	cancel()
	log.Info("stripe expire session call", "result", string(res.Kind), "http_status", res.Status,
		"provider_code", res.Code, "error", errString(res.Err))
	if err := store.recordExpire(ctx, b.CollectionID, res); err != nil {
		return false, fmt.Errorf("payments: record expire request: %w", err)
	}
	switch res.Kind {
	case Expired, NotOpen, Rejected:
		return false, nil
	case Refused:
		return false, jobs.Attention(reasonExpireRefused, errors.New(res.String()))
	default: // Unknown, Deferred: verify now, the same key later
		return true, nil
	}
}

func (w *Worker) reconcile(ctx context.Context, log *slog.Logger, store leaseStore, b begin) error {
	log = log.With("session_id", b.SessionID)
	retryExpire, err := w.expire(ctx, log, store, b)
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, ProviderTimeout)
	res := w.Provider.RetrieveSession(callCtx, b.SessionID)
	cancel()
	log.Info("stripe retrieve session call", "result", string(res.Kind), "http_status", res.Status,
		"provider_code", res.Code, "error", errString(res.Err))
	var v Verdict
	switch res.Kind {
	case Found:
		v = Evaluate(res.Session, Expectation{CollectionID: b.CollectionID, OrderID: b.OrderID, Environment: b.Environment,
			SessionID: b.SessionID, AmountMinor: b.AmountMinor, Livemode: b.Livemode})
	case NotFound:
		v = Verdict{Kind: VerdictMismatch, Reason: "session not found in this Stripe account",
			Evidence: Evidence{SessionID: b.SessionID, Livemode: b.Livemode}}
	case Refused, Rejected:
		return jobs.Attention(reasonRetrieve, errors.New(res.String()))
	default:
		return errors.New(res.String())
	}
	state, cmd, err := store.recordReconcile(ctx, b.CollectionID, b.SessionID, res.Session, v)
	switch {
	case errors.Is(err, recovery.ErrFrozen):
		return jobs.Defer(FreezeRecheck, err)
	case errors.Is(err, errOrderBusy):
		// A counted retry (1m, 5m, 15m, 1h, then attention): busy is
		// normally brief, never silent for long.
		return err
	case errors.Is(err, errOrderStale):
		return jobs.Attention(reasonStale, err)
	case err != nil:
		return err
	}
	log.Info("payment reconciled", "verdict", v.Kind, "reason", v.Reason, "collection_state", state, "command_id", cmd)
	if v.Kind == VerdictMismatch {
		return jobs.Attention(reasonMismatch, errors.New(v.Reason))
	}
	if err := underReview(ctx, store, b); err != nil {
		return err
	}
	if v.Kind == VerdictPending {
		if retryExpire {
			return jobs.Defer(ExpireRetry, errors.New("payments: session not decided yet; the expire request is asked again with the same key"))
		}
		return jobs.Defer(ReconcileRecheck, errors.New("payments: session not decided yet"))
	}
	return nil
}

// underReview sends a reconcile of a collection under manager
// reconciliation (a recorded mismatch) to attention: whatever this check
// saw, it changed nothing, and the hold review keeps the order's holds
// until a manager resolves it (migration 0017). Never silent.
func underReview(ctx context.Context, store leaseStore, b begin) error {
	if b.Purpose != "reconcile" {
		return nil
	}
	under, err := store.underReconciliation(ctx, b.CollectionID)
	switch {
	case err != nil:
		return err
	case under:
		return jobs.Attention(reasonUnderReview, nil)
	}
	return nil
}

// leaseStore is the recovery.Store for one leased payment job; every call
// is fenced by the job lease (business.payment_*).
type leaseStore struct {
	db  Querier
	job jobs.Job
}

var _ recovery.Store = leaseStore{}

// ErrLeaseLost means the worker no longer holds the job's live lease.
var ErrLeaseLost = errors.New("payments: job lease lost")

var (
	errOrderBusy  = errors.New("payments: another protected operation on the order is unresolved")
	errOrderStale = errors.New("payments: the order already has an operation at this or a later version")
)

func gatewayErr(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "GK001":
		return fmt.Errorf("%w: %s", ErrLeaseLost, pgErr.Message)
	case errors.As(err, &pgErr) && pgErr.Code == "GK002":
		return fmt.Errorf("%w (checked at the record)", recovery.ErrFrozen)
	case errors.As(err, &pgErr) && pgErr.Code == "GK409" && pgErr.Message == "order_busy":
		return errOrderBusy
	case errors.As(err, &pgErr) && pgErr.Code == "GK409" && pgErr.Message == "order_stale":
		return errOrderStale
	}
	return err
}

func (s leaseStore) args(extra ...any) []any {
	return append([]any{s.job.ID, s.job.Worker, s.job.Generation}, extra...)
}

type begin struct {
	Status           string
	Purpose          string
	CollectionID     string
	OrderID          string
	CollectionState  string
	Payload          []byte
	ReplayUntil      time.Time
	SessionExpiresAt time.Time
	ReturnOrigin     string
	DBNow            time.Time
	SessionID        string
	AmountMinor      int64
	Environment      string
	Account          string
	Livemode         bool
}

func (s leaseStore) begin(ctx context.Context) (begin, error) {
	var (
		b                                    begin
		purpose, coll, order, state, payload *string
		session, env, account, origin        *string
		until, expires, now                  *time.Time
		amount                               *int64
		live                                 *bool
	)
	err := s.db.QueryRow(ctx, `SELECT status, purpose, collection_id::text, order_id::text, collection_state, payload,
		replay_until, session_expires_at, return_origin, db_now, session_id, amount_minor, environment, account, livemode
		FROM business.payment_job_begin($1::uuid, $2::uuid, $3)`, s.args()...).Scan(&b.Status, &purpose, &coll, &order,
		&state, &payload, &until, &expires, &origin, &now, &session, &amount, &env, &account, &live)
	if err != nil {
		return begin{}, gatewayErr(err)
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	b.Purpose, b.CollectionID, b.OrderID, b.CollectionState = str(purpose), str(coll), str(order), str(state)
	b.SessionID, b.Environment, b.Account, b.ReturnOrigin = str(session), str(env), str(account), str(origin)
	if expires != nil {
		b.SessionExpiresAt = *expires
	}
	if now != nil {
		b.DBNow = *now
	}
	if payload != nil {
		b.Payload = []byte(*payload)
	}
	if until != nil {
		b.ReplayUntil = *until
	}
	if amount != nil {
		b.AmountMinor = *amount
	}
	if live != nil {
		b.Livemode = *live
	}
	return b, nil
}

// fix stores the first dispatch's exact request (payment_dispatch_fix) and
// updates b with the fixed payload, window and expiry.
func (s leaseStore) fix(ctx context.Context, opID string, payload []byte, at time.Time, b *begin) (string, error) {
	var (
		status         string
		stored         *string
		until, expires *time.Time
	)
	err := s.db.QueryRow(ctx, `SELECT status, payload, replay_until, session_expires_at
		FROM business.payment_dispatch_fix($1::uuid, $2::uuid, $3, $4::uuid, $5, $6)`,
		s.args(opID, string(payload), at)...).Scan(&status, &stored, &until, &expires)
	if err != nil {
		return "", gatewayErr(err)
	}
	if status == "ready" {
		if stored == nil || until == nil || expires == nil {
			return "", errors.New("payments: fixed dispatch without its payload")
		}
		b.Payload, b.ReplayUntil, b.SessionExpiresAt = []byte(*stored), *until, *expires
	}
	return status, nil
}

func (s leaseStore) Reserve(ctx context.Context, op recovery.Operation, envelopeKey string) (recovery.Reservation, error) {
	var (
		res     recovery.Reservation
		state   *string
		outcome []byte
		hash    *string
	)
	err := s.db.QueryRow(ctx, `SELECT result, op_state, op_outcome, op_outcome_hash
		FROM business.payment_recovery_reserve($1::uuid, $2::uuid, $3, $4::uuid, $5, $6::uuid, $7, $8, $9, $10)`,
		s.args(op.ID, string(op.Kind), op.AggregateID, op.PriorVersion, op.PayloadHash(), envelopeKey, op.ReplayUntil)...,
	).Scan(&res.Result, &state, &outcome, &hash)
	if err != nil {
		return recovery.Reservation{}, gatewayErr(err)
	}
	if state != nil {
		res.State = recovery.State(*state)
	}
	if outcome != nil {
		if hash == nil || recovery.HashHex(outcome) != *hash {
			return recovery.Reservation{}, fmt.Errorf("payments: recorded outcome of %s does not match its hash", op.ID)
		}
		var o recovery.Outcome
		if err := json.Unmarshal(outcome, &o); err != nil {
			return recovery.Reservation{}, fmt.Errorf("payments: decode recorded outcome: %w", err)
		}
		res.Outcome = &o
	}
	return res, nil
}

// Lookup is restore and manager tooling, never the worker's.
func (s leaseStore) Lookup(context.Context, recovery.Operation, string) (recovery.Reservation, error) {
	return recovery.Reservation{}, errors.New("payments: the worker cannot look up or resolve operations")
}

func (s leaseStore) state(ctx context.Context, sql string, args ...any) (recovery.State, error) {
	var state string
	if err := s.db.QueryRow(ctx, sql, args...).Scan(&state); err != nil {
		return "", gatewayErr(err)
	}
	return recovery.State(state), nil
}

func (s leaseStore) MarkPrepared(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	return s.state(ctx, "SELECT business.payment_recovery_prepared($1::uuid, $2::uuid, $3, $4::uuid)", s.args(op.ID)...)
}

func (s leaseStore) MarkCommitted(ctx context.Context, op recovery.Operation, outcome recovery.Outcome, fromReconcile bool) (recovery.State, bool, error) {
	if fromReconcile {
		return "", false, errors.New("payments: the worker cannot resolve a reconciliation")
	}
	hash, body, err := outcome.Encode()
	if err != nil {
		return "", false, err
	}
	var (
		state   string
		changed bool
	)
	err = s.db.QueryRow(ctx, `SELECT op_state, changed
		FROM business.payment_recovery_committed($1::uuid, $2::uuid, $3, $4::uuid, $5::json, $6)`,
		s.args(op.ID, string(body), hash)...).Scan(&state, &changed)
	if err != nil {
		return "", false, gatewayErr(err)
	}
	return recovery.State(state), changed, nil
}

func (s leaseStore) MarkSecured(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	return s.state(ctx, "SELECT business.payment_recovery_secured($1::uuid, $2::uuid, $3, $4::uuid)", s.args(op.ID)...)
}

func (s leaseStore) MarkReconcile(ctx context.Context, op recovery.Operation, reason string) (recovery.State, error) {
	return s.state(ctx, "SELECT business.payment_recovery_reconcile($1::uuid, $2::uuid, $3, $4::uuid, $5)", s.args(op.ID, reason)...)
}

func (s leaseStore) Frozen(ctx context.Context) (bool, error) {
	var frozen bool
	err := s.db.QueryRow(ctx, "SELECT business.payment_recovery_frozen($1::uuid, $2::uuid, $3)", s.args()...).Scan(&frozen)
	return frozen, gatewayErr(err)
}

// kind is the leased dispatch's collection kind (payment_dispatch_kind,
// migration 0035): cake or delivery.
func (s leaseStore) kind(ctx context.Context, opID string) (string, error) {
	var kind string
	err := s.db.QueryRow(ctx, "SELECT business.payment_dispatch_kind($1::uuid, $2::uuid, $3, $4::uuid)", s.args(opID)...).Scan(&kind)
	return kind, gatewayErr(err)
}

func (s leaseStore) admit(ctx context.Context, opID string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, "SELECT business.payment_dispatch_admit($1::uuid, $2::uuid, $3, $4::uuid)", s.args(opID)...).Scan(&status)
	return status, gatewayErr(err)
}

// recordAttempt appends the call's result, detached from the job context
// (bounded) so evidence of a cancelled call is kept.
func (s leaseStore) recordAttempt(ctx context.Context, opID string, res Result) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var (
		no     int
		status *int
		code   *string
	)
	if res.Status != 0 {
		status = &res.Status
	}
	if codePattern.MatchString(res.Code) {
		code = &res.Code
	}
	err := s.db.QueryRow(ctx, `SELECT business.payment_attempt_record($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7)`,
		s.args(opID, string(res.Kind), status, code)...).Scan(&no)
	return gatewayErr(err)
}

func (s leaseStore) recordReconcile(ctx context.Context, collection, sessionID string, sess *Session, v Verdict) (string, string, error) {
	ev, err := json.Marshal(v.Evidence)
	if err != nil {
		return "", "", err
	}
	var (
		sessionStatus, paymentStatus, piID *string
		successAt                          *time.Time
		reason                             *string
		state                              string
		cmd                                *string
	)
	if sess != nil {
		sessionStatus = nullString(sanitize(statusPattern, sess.Status))
		paymentStatus = nullString(sanitize(statusPattern, sess.PaymentStatus))
		if sess.PaymentIntent != nil {
			piID = nullString(sanitize(piIDPattern, sess.PaymentIntent.ID))
		}
	}
	if v.Kind == VerdictPaid {
		successAt = &v.SuccessAt
	}
	if v.Kind == VerdictMismatch {
		reason = &v.Reason
	}
	err = s.db.QueryRow(ctx, `SELECT collection_state, command_id::text FROM business.payment_reconcile_record(
		$1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7, $8, $9, $10::json, $11, $12)`,
		s.args(collection, v.Kind, sessionID, sessionStatus, paymentStatus, piID, string(ev), successAt, reason)...,
	).Scan(&state, &cmd)
	if err != nil {
		return "", "", gatewayErr(err)
	}
	return state, deref(cmd), nil
}

// expireAdmit asks whether the leased reconcile may request the session's
// expiry now (business.payment_expire_admit): "ok", "skip" or "frozen".
func (s leaseStore) expireAdmit(ctx context.Context, collection string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, "SELECT business.payment_expire_admit($1::uuid, $2::uuid, $3, $4::uuid)",
		s.args(collection)...).Scan(&status)
	return status, gatewayErr(err)
}

// underReconciliation reports whether the leased reconcile's collection
// carries a recorded mismatch (business.payment_under_reconciliation).
func (s leaseStore) underReconciliation(ctx context.Context, collection string) (bool, error) {
	var under bool
	err := s.db.QueryRow(ctx, "SELECT business.payment_under_reconciliation($1::uuid, $2::uuid, $3, $4::uuid)",
		s.args(collection)...).Scan(&under)
	return under, gatewayErr(err)
}

// recordExpire appends the expire request's answer, detached from the job
// context (bounded) so evidence of a cancelled call is kept.
func (s leaseStore) recordExpire(ctx context.Context, collection string, res Result) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var (
		no     int
		status *int
		code   *string
	)
	if res.Status != 0 {
		status = &res.Status
	}
	if codePattern.MatchString(res.Code) {
		code = &res.Code
	}
	err := s.db.QueryRow(ctx, `SELECT business.payment_expire_record($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7)`,
		s.args(collection, string(res.Kind), status, code)...).Scan(&no)
	return gatewayErr(err)
}
