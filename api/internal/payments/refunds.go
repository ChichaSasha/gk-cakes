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
)

// Attention reasons of refund jobs (no personal data).
const (
	reasonRefundPayload   = "stored refund payload refused by this build: not dispatched (see its attempts for any earlier ones)"
	reasonRefundAccount   = "refund belongs to another Stripe account or environment than this worker's: not dispatched or recorded"
	reasonRefundNoIntent  = "refund cannot be sent: the collection has no recorded payment intent; manager reconciliation"
	reasonRefundBlocked   = "refund blocked: a refund seen at Stripe is quarantined or this refund carries contradicting evidence; manager resolution before anything is sent"
	reasonRefundReconcile = "refund outcome unknown (idempotency conflict or window ended): manager reconciliation, never a blind new refund"
	reasonRefundRecovery  = "recovery operation conflict for this refund"
	reasonRefundRefused   = "Stripe refused the refund request (credentials or permissions): fix the key, then requeue"
	reasonRefundRejected  = "Stripe rejected the refund request: the refund failed; the full return is still owed (manager)"
	reasonRefundFailed    = "Stripe reports the refund failed: the full return is still owed and is never re-sent automatically (manager)"
	reasonRefundAction    = "Stripe reports the refund requires action: manager follow-up"
	reasonRefundMismatch  = "Stripe refund evidence does not match the refund operation: never trusted; manager reconciliation"
	// A decided refund Stripe now reports otherwise (for example succeeded,
	// then failed): the return may not have happened.
	reasonRefundContradiction = "Stripe now reports a decided refund otherwise (for example succeeded, then failed): the full return may be owed again; nothing is paid out automatically (manager)"
	reasonRefundQuarantine    = "a refund seen at Stripe does not match any refund operation: quarantined, blocks new refunds for the collection; manager resolution"
	reasonRefundUnknown       = "refund outcome unknown past its idempotency window and absent from Stripe's complete refund list: manager reconciliation, never a blind new refund"
	reasonRefundList          = "Stripe refused listing or reading the collection's refunds: check the key, then requeue"
)

// Refunds is the job handler for refund operations (migration 0016): the
// protected POST /v1/refunds dispatch and the collection's refund
// reconcile. Jobs of other kinds return jobs.ErrNoHandler.
type Refunds struct {
	DB          Querier // gk_worker pool
	Coordinator *recovery.Coordinator
	// Provider is Stripe; nil (no STRIPE_SECRET_KEY) makes refund jobs wait.
	Provider    RefundProvider
	Environment string // APP_ENV; operations of any other are refused
	Account     string // STRIPE_ACCOUNT_ID
	Logger      *slog.Logger
}

type refundBegin struct {
	Status, Purpose, CollectionID, OrderID, OperationID, OperationState string
	AmountMinor                                                         int64
	PaymentIntentID, ProviderRefundID                                   string
	Payload                                                             []byte
	ReplayUntil                                                         time.Time
	DBNow                                                               time.Time
	Environment, Account                                                string
	Livemode                                                            bool
}

// Handle runs the leased refund job.
func (w *Refunds) Handle(ctx context.Context, job jobs.Job) error {
	store := refundStore{db: w.DB, job: job}
	b, err := store.begin(ctx)
	if err != nil {
		return err
	}
	logger := w.Logger
	if logger == nil {
		logger = slog.Default()
	}
	log := logger.With("job_id", job.ID, "generation", job.Generation, "collection_id", b.CollectionID,
		"refund_operation_id", b.OperationID, "purpose", b.Purpose)
	switch b.Status {
	case "ready":
	case "not_refund":
		return jobs.ErrNoHandler
	case "frozen":
		return jobs.Defer(FreezeRecheck, recovery.ErrFrozen)
	case "closed":
		log.Info("refund dispatch closed before any Stripe call", "operation_state", b.OperationState)
		return nil
	case "no_payment_intent":
		return jobs.Attention(reasonRefundNoIntent, nil)
	default:
		return fmt.Errorf("payments: unexpected refund job status %q", b.Status)
	}
	if w.Provider == nil {
		return jobs.Defer(ProviderRetry, errors.New("payments: no Stripe key configured on this worker"))
	}
	if b.Account != w.Account || b.Environment != w.Environment || b.Livemode != (w.Environment == "production") {
		return jobs.Attention(reasonRefundAccount, nil)
	}
	log = log.With("order_id", b.OrderID)
	if b.Purpose == "dispatch" {
		return w.dispatch(ctx, log, store, b)
	}
	return w.reconcile(ctx, log, store, b)
}

func (w *Refunds) dispatch(ctx context.Context, log *slog.Logger, store refundStore, b refundBegin) error {
	if w.Coordinator == nil {
		return jobs.Defer(JournalRetry, errors.New("payments: no recovery journal configured for refund dispatch"))
	}
	if w.Coordinator.Environment != w.Environment {
		return errors.New("payments: worker and recovery journal must use the same environment")
	}
	req := RefundRequest{OperationID: b.OperationID, OrderID: b.OrderID, CollectionID: b.CollectionID,
		PaymentIntentID: b.PaymentIntentID, AmountMinor: b.AmountMinor, Environment: b.Environment}
	if len(b.Payload) == 0 {
		// First dispatch: fix the exact request now, on the database clock.
		payload, err := NewRefundPayload(req, b.Account)
		if err != nil {
			return jobs.Attention(reasonRefundPayload, err)
		}
		status, err := store.fix(ctx, b.OperationID, payload, b.DBNow, &b)
		switch {
		case err != nil:
			return err
		case status == "frozen":
			return jobs.Defer(FreezeRecheck, recovery.ErrFrozen)
		case status == "closed":
			log.Info("refund dispatch closed before any Stripe call")
			return nil
		case status == "blocked":
			return jobs.Attention(reasonRefundBlocked, nil)
		case status == "no_payment_intent":
			return jobs.Attention(reasonRefundNoIntent, nil)
		case status != "ready":
			return fmt.Errorf("payments: unexpected refund fix status %q", status)
		}
		log.Info("refund request fixed", "replay_until", b.ReplayUntil.UTC().Format(time.RFC3339))
	}
	d, err := CheckRefundPayload(b.Payload, RefundExpected{RefundRequest: req, Account: b.Account, Livemode: b.Livemode},
		w.Environment)
	if err != nil {
		return jobs.Attention(reasonRefundPayload, err)
	}
	op := recovery.Operation{ID: b.OperationID, Kind: recovery.KindDispatch, AggregateID: b.OperationID,
		Payload: b.Payload, ReplayUntil: b.ReplayUntil}
	outcome, err := w.Coordinator.Dispatch(ctx, store, op, w.effect(log, store, op.ID, d, b.Livemode))
	var (
		ae *refundAdmitError
		ce *refundCallError
	)
	switch {
	case err == nil:
		return refundOutcomeError(outcome, req, b.Livemode, log)
	case errors.As(err, &ae) && ae.status == "expired",
		errors.As(err, &ce) && ce.res.Kind == Conflict:
		// Never a new refund under another key: reconcile (and a manager).
		if _, rerr := store.MarkReconcile(ctx, op, refundReconcileReason(err)); rerr != nil {
			return jobs.Attention(reasonRefundReconcile, fmt.Errorf("payments: mark reconcile: %w (after %v)", rerr, err))
		}
		return jobs.Attention(reasonRefundReconcile, err)
	case errors.Is(err, recovery.ErrNeedsReconcile):
		return jobs.Attention(reasonRefundReconcile, err)
	case errors.Is(err, recovery.ErrFrozen), errors.As(err, &ae) && ae.status == "frozen":
		return jobs.Defer(FreezeRecheck, err)
	case errors.As(err, &ae) && ae.status == "blocked":
		return jobs.Attention(reasonRefundBlocked, err)
	case errors.Is(err, recovery.ErrBusy), errors.Is(err, recovery.ErrStale):
		return jobs.Attention(reasonRefundRecovery, err)
	case errors.As(err, &ce) && ce.res.Kind == Refused:
		return jobs.Attention(reasonRefundRefused, err)
	default:
		// Unknown or deferred: the same key and body are retried (counted).
		return err
	}
}

func refundReconcileReason(err error) string {
	var ae *refundAdmitError
	if errors.As(err, &ae) {
		return "Stripe idempotency window ended (database clock) before the refund call"
	}
	return "Stripe reports the refund idempotency key used with another request"
}

// refundOutcomeError maps a secured dispatch outcome to the job's result:
// a matching succeeded or pending refund completes (a pending one is
// reconciled by its scheduled job); anything a manager must look at goes to
// attention.
func refundOutcomeError(o recovery.Outcome, req RefundRequest, livemode bool, log *slog.Logger) error {
	var ev RefundEvidence
	_ = json.Unmarshal(o.Evidence, &ev)
	log.Info("refund dispatched", "outcome", o.Status, "refund_id", ev.RefundID)
	switch o.Status {
	case "settled":
		// A reconcile decided the operation first: nothing was sent now.
		return nil
	case string(Rejected):
		return jobs.Attention(reasonRefundRejected, nil)
	}
	if reason := RefundEvidenceMismatch(ev, req, livemode); reason != "" {
		return jobs.Attention(reasonRefundMismatch, errors.New(reason))
	}
	switch o.Status {
	case string(RefundSucceeded), string(RefundPending):
		return nil
	case string(RefundRequiresAction):
		return jobs.Attention(reasonRefundAction, nil)
	case string(RefundFailed):
		return jobs.Attention(reasonRefundFailed, nil)
	}
	return fmt.Errorf("payments: unexpected refund outcome %q", o.Status)
}

// RefundEvidenceMismatch is why a refund's evidence does not belong to the
// operation ("" when it does): the same checks the database applies before
// trusting it (business.refund_recovery_committed).
func RefundEvidenceMismatch(ev RefundEvidence, req RefundRequest, livemode bool) string {
	switch {
	case !refundIDPattern.MatchString(ev.RefundID):
		return "refund object without a valid ID"
	case ev.AmountMinor != req.AmountMinor:
		return "refund amount differs from the operation"
	case ev.Currency != "gbp":
		return "refund currency is not gbp"
	case ev.PaymentIntentID != req.PaymentIntentID:
		return "refund of another payment intent"
	case ev.Livemode != livemode:
		return "refund created in the wrong Stripe mode"
	}
	return ""
}

// effect admits and posts the stored refund request once, records the call
// and returns a recordable outcome; anything else leaves the outcome
// unknown, so the dispatch stays prepared and is replayed with the same key
// and body.
func (w *Refunds) effect(log *slog.Logger, store refundStore, opID string, d Dispatch, livemode bool) recovery.Effect {
	return func(ctx context.Context) (recovery.Outcome, error) {
		switch status, err := store.admit(ctx, opID); {
		case err != nil:
			return recovery.Outcome{}, fmt.Errorf("payments: refund admission: %w", err)
		case status == "settled", status == "closed":
			// Decided (or withdrawn) without this call: nothing is sent.
			return recovery.Outcome{Status: "settled", Evidence: json.RawMessage(`{"source":"database"}`)}, nil
		case status != "ok":
			return recovery.Outcome{}, &refundAdmitError{status: status}
		}
		callCtx, cancel := context.WithTimeout(ctx, ProviderTimeout)
		res := w.Provider.CreateRefund(callCtx, d.IdempotencyKey, d.Form)
		cancel()
		refundID := ""
		if res.Refund != nil {
			refundID = sanitize(refundIDPattern, res.Refund.ID)
		}
		log.Info("stripe create refund call", "result", string(res.Kind), "http_status", res.Status,
			"provider_code", res.Code, "refund_id", refundID, "error", errString(res.Err))
		if err := store.recordAttempt(ctx, opID, res); err != nil {
			return recovery.Outcome{}, fmt.Errorf("payments: record refund attempt: %w", err)
		}
		return refundOutcome(res, livemode)
	}
}

// refundOutcome is the recordable dispatch outcome of a create result, or a
// *refundCallError when the outcome is unknown.
func refundOutcome(res RefundResult, livemode bool) (recovery.Outcome, error) {
	status := RefundOutcomeStatus(res.Kind)
	if status == "" {
		return recovery.Outcome{}, &refundCallError{res: res}
	}
	var ev RefundEvidence
	if res.Refund != nil {
		ev = EvidenceOfRefund(res.Refund, livemode)
	}
	ev.HTTPStatus, ev.ProviderCode = res.Status, sanitize(codePattern, res.Code)
	b, err := json.Marshal(ev)
	return recovery.Outcome{Status: status, Evidence: b}, err
}

// refundAdmitError means Stripe was not called: admission refused right
// before the call ("frozen", "blocked" or "expired").
type refundAdmitError struct{ status string }

func (e *refundAdmitError) Error() string { return "refund dispatch not admitted: " + e.status }

// refundCallError is a refund call whose outcome is not recordable.
type refundCallError struct{ res RefundResult }

func (e *refundCallError) Error() string { return e.res.String() }

func (e *refundCallError) Unwrap() error { return e.res.Err }

type reconcileOp struct {
	ID, State, PaymentIntentID, ProviderRefundID string
	AmountMinor                                  int64
}

// refundVerdict decides an operation from a refund Stripe reported: its
// status when the refund matches the operation, otherwise a mismatch.
func refundVerdict(op reconcileOp, r *Refund) (verdict, reason string) {
	switch {
	case op.ProviderRefundID != "" && r.ID != op.ProviderRefundID:
		return "mismatch", "another refund than the one recorded for this operation"
	case r.Amount != op.AmountMinor:
		return "mismatch", "refund amount differs from the operation"
	case r.Currency != "gbp":
		return "mismatch", "refund currency is not gbp"
	case op.PaymentIntentID != "" && r.PaymentIntent != op.PaymentIntentID:
		return "mismatch", "refund of another payment intent"
	}
	switch refundStatusKind(r.Status) {
	case RefundSucceeded:
		return "succeeded", ""
	case RefundPending:
		return "pending", ""
	case RefundRequiresAction:
		return "requires_action", ""
	case RefundFailed:
		return "failed", sanitize(codePattern, r.FailureReason)
	}
	return "mismatch", "unknown refund status"
}

// findRefund is the listed refund of operation op: the one holding its
// recorded refund ID, else the one whose metadata names it.
func findRefund(op reconcileOp, list []Refund) *Refund {
	var named *Refund
	for i := range list {
		r := &list[i]
		if op.ProviderRefundID != "" && r.ID == op.ProviderRefundID {
			return r
		}
		if named == nil && r.OperationID() == op.ID {
			named = r
		}
	}
	return named
}

func (w *Refunds) reconcile(ctx context.Context, log *slog.Logger, store refundStore, b refundBegin) error {
	log = log.With("payment_intent_id", b.PaymentIntentID)
	callCtx, cancel := context.WithTimeout(ctx, 4*ProviderTimeout)
	list := w.Provider.ListRefunds(callCtx, b.PaymentIntentID)
	cancel()
	log.Info("stripe list refunds call", "result", string(list.Kind), "http_status", list.Status, "pages", list.Pages,
		"refunds", len(list.Refunds), "error", errString(list.Err))
	switch list.Kind {
	case Found:
	case Refused, Rejected:
		return jobs.Attention(reasonRefundList, errors.New(list.String()))
	default:
		// Incomplete: nothing may be concluded from absence; retried.
		return errors.New(list.String())
	}
	var attention []string
	for i := range list.Refunds {
		r := &list.Refunds[i]
		obs, err := store.observe(ctx, b.CollectionID, r)
		if err != nil {
			return err
		}
		if obs.Quarantined && !obs.Resolved {
			log.Warn("refund observation quarantined", "refund_id", r.ID, "reason", obs.Reason, "result", obs.Result)
			attention = append(attention, reasonRefundQuarantine)
		}
		if obs.Result == "contradiction" {
			// Stripe now reports a status that contradicts the decided
			// operation (for example a succeeded refund that failed later).
			log.Warn("refund contradicts its decided operation", "refund_id", r.ID, "status", r.Status)
			attention = append(attention, reasonRefundContradiction)
		}
	}
	ops, err := store.operations(ctx, b.CollectionID)
	if err != nil {
		return err
	}
	open := false
	for _, op := range ops {
		r := findRefund(op, list.Refunds)
		if r == nil && op.ProviderRefundID != "" {
			// Listed refunds may lag: read the recorded one directly.
			callCtx, cancel := context.WithTimeout(ctx, ProviderTimeout)
			got := w.Provider.RetrieveRefund(callCtx, op.ProviderRefundID)
			cancel()
			log.Info("stripe retrieve refund call", "result", string(got.Kind), "http_status", got.Status,
				"refund_id", op.ProviderRefundID, "error", errString(got.Err))
			switch got.Kind {
			case Found:
				r = got.Refund
			case NotFound:
			case Refused, Rejected:
				return jobs.Attention(reasonRefundList, errors.New(got.String()))
			default:
				return errors.New(got.String())
			}
		}
		verdict, reason, refundID := "not_found", "", ""
		var ev RefundEvidence
		if r != nil {
			verdict, reason = refundVerdict(op, r)
			ev = EvidenceOfRefund(r, b.Livemode)
			if verdict != "mismatch" {
				refundID = r.ID
			}
		} else if op.ProviderRefundID != "" {
			verdict, reason = "mismatch", "the recorded refund is not found at Stripe"
		}
		state, ended, err := store.decide(ctx, op.ID, verdict, refundID, ev, reason)
		if err != nil {
			return err
		}
		log.Info("refund reconciled", "operation", op.ID, "verdict", verdict, "reason", reason, "state", state,
			"window_ended", ended)
		if op.State == "succeeded" || op.State == "failed" {
			// Decided before: only a contradiction is news (it is recorded
			// beside the operation; nothing is paid out or reopened).
			if verdict == "mismatch" || (verdict != "not_found" && verdict != op.State) {
				attention = append(attention, reasonRefundContradiction)
			}
			continue
		}
		switch {
		case verdict == "mismatch":
			attention = append(attention, reasonRefundMismatch)
		case state == "requires_action":
			attention = append(attention, reasonRefundAction)
		case state == "failed":
			attention = append(attention, reasonRefundFailed)
		case state == "unknown" && ended:
			attention = append(attention, reasonRefundUnknown)
		case state == "submitting", state == "pending", state == "unknown":
			open = true
		}
	}
	if len(attention) > 0 {
		return jobs.Attention(attention[0], fmt.Errorf("payments: %d refund findings need a manager", len(attention)))
	}
	if open {
		return jobs.Defer(ReconcileRecheck, errors.New("payments: refund not decided yet"))
	}
	return nil
}

// refundStore is the recovery.Store for one leased refund job; every call
// is fenced by the job lease (business.refund_*).
type refundStore struct {
	db  Querier
	job jobs.Job
}

var _ recovery.Store = refundStore{}

func (s refundStore) args(extra ...any) []any {
	return append([]any{s.job.ID, s.job.Worker, s.job.Generation}, extra...)
}

func (s refundStore) begin(ctx context.Context) (refundBegin, error) {
	var (
		b                                                 refundBegin
		purpose, coll, order, op, state, pi, refund, body *string
		env, account                                      *string
		amount                                            *int64
		until, started, now                               *time.Time
		live                                              *bool
	)
	err := s.db.QueryRow(ctx, `SELECT status, purpose, collection_id::text, order_id::text, operation_id::text,
		operation_state, amount_minor, payment_intent_id, provider_refund_id, payload, replay_until, dispatch_started_at,
		db_now, environment, account, livemode FROM business.refund_job_begin($1::uuid, $2::uuid, $3)`, s.args()...).Scan(
		&b.Status, &purpose, &coll, &order, &op, &state, &amount, &pi, &refund, &body, &until, &started, &now, &env,
		&account, &live)
	if err != nil {
		return refundBegin{}, gatewayErr(err)
	}
	b.Purpose, b.CollectionID, b.OrderID, b.OperationID = deref(purpose), deref(coll), deref(order), deref(op)
	b.OperationState, b.PaymentIntentID, b.ProviderRefundID = deref(state), deref(pi), deref(refund)
	b.Environment, b.Account = deref(env), deref(account)
	if body != nil {
		b.Payload = []byte(*body)
	}
	if amount != nil {
		b.AmountMinor = *amount
	}
	if until != nil {
		b.ReplayUntil = *until
	}
	if now != nil {
		b.DBNow = *now
	}
	if live != nil {
		b.Livemode = *live
	}
	return b, nil
}

// fix stores the first dispatch's exact request (refund_dispatch_fix) and
// updates b with the fixed payload and window.
func (s refundStore) fix(ctx context.Context, opID string, payload []byte, at time.Time, b *refundBegin) (string, error) {
	var (
		status string
		stored *string
		until  *time.Time
	)
	err := s.db.QueryRow(ctx, `SELECT status, payload, replay_until FROM business.refund_dispatch_fix($1::uuid, $2::uuid, $3,
		$4::uuid, $5, $6)`, s.args(opID, string(payload), at)...).Scan(&status, &stored, &until)
	if err != nil {
		return "", gatewayErr(err)
	}
	if status == "ready" {
		if stored == nil || until == nil {
			return "", errors.New("payments: fixed refund dispatch without its payload")
		}
		b.Payload, b.ReplayUntil = []byte(*stored), *until
	}
	return status, nil
}

func (s refundStore) Reserve(ctx context.Context, op recovery.Operation, envelopeKey string) (recovery.Reservation, error) {
	var (
		res     recovery.Reservation
		state   *string
		outcome []byte
		hash    *string
	)
	err := s.db.QueryRow(ctx, `SELECT result, op_state, op_outcome, op_outcome_hash
		FROM business.refund_recovery_reserve($1::uuid, $2::uuid, $3, $4::uuid, $5, $6::uuid, $7, $8, $9, $10)`,
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
			return recovery.Reservation{}, fmt.Errorf("payments: recorded refund outcome of %s does not match its hash", op.ID)
		}
		var o recovery.Outcome
		if err := json.Unmarshal(outcome, &o); err != nil {
			return recovery.Reservation{}, fmt.Errorf("payments: decode recorded refund outcome: %w", err)
		}
		res.Outcome = &o
	}
	return res, nil
}

// Lookup is restore and manager tooling, never the worker's.
func (s refundStore) Lookup(context.Context, recovery.Operation, string) (recovery.Reservation, error) {
	return recovery.Reservation{}, errors.New("payments: the worker cannot look up or resolve refund operations")
}

func (s refundStore) state(ctx context.Context, sql string, args ...any) (recovery.State, error) {
	var state string
	if err := s.db.QueryRow(ctx, sql, args...).Scan(&state); err != nil {
		return "", gatewayErr(err)
	}
	return recovery.State(state), nil
}

func (s refundStore) MarkPrepared(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	return s.state(ctx, "SELECT business.refund_recovery_prepared($1::uuid, $2::uuid, $3, $4::uuid)", s.args(op.ID)...)
}

func (s refundStore) MarkCommitted(ctx context.Context, op recovery.Operation, outcome recovery.Outcome, fromReconcile bool) (recovery.State, bool, error) {
	if fromReconcile {
		return "", false, errors.New("payments: the worker cannot resolve a refund reconciliation")
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
		FROM business.refund_recovery_committed($1::uuid, $2::uuid, $3, $4::uuid, $5::json, $6)`,
		s.args(op.ID, string(body), hash)...).Scan(&state, &changed)
	if err != nil {
		return "", false, gatewayErr(err)
	}
	return recovery.State(state), changed, nil
}

func (s refundStore) MarkSecured(ctx context.Context, op recovery.Operation) (recovery.State, error) {
	return s.state(ctx, "SELECT business.refund_recovery_secured($1::uuid, $2::uuid, $3, $4::uuid)", s.args(op.ID)...)
}

func (s refundStore) MarkReconcile(ctx context.Context, op recovery.Operation, reason string) (recovery.State, error) {
	return s.state(ctx, "SELECT business.refund_recovery_reconcile($1::uuid, $2::uuid, $3, $4::uuid, $5)", s.args(op.ID, reason)...)
}

func (s refundStore) Frozen(ctx context.Context) (bool, error) {
	var frozen bool
	err := s.db.QueryRow(ctx, "SELECT business.refund_recovery_frozen($1::uuid, $2::uuid, $3)", s.args()...).Scan(&frozen)
	return frozen, gatewayErr(err)
}

func (s refundStore) admit(ctx context.Context, opID string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, "SELECT business.refund_dispatch_admit($1::uuid, $2::uuid, $3, $4::uuid)", s.args(opID)...).Scan(&status)
	return status, gatewayErr(err)
}

// recordAttempt appends the call's result, detached from the job context
// (bounded) so evidence of a cancelled call is kept.
func (s refundStore) recordAttempt(ctx context.Context, opID string, res RefundResult) error {
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
	err := s.db.QueryRow(ctx, `SELECT business.refund_attempt_record($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7)`,
		s.args(opID, string(res.Kind), status, code)...).Scan(&no)
	return gatewayErr(err)
}

type observation struct {
	Result, Reason        string
	Quarantined, Resolved bool
}

func (s refundStore) observe(ctx context.Context, collection string, r *Refund) (observation, error) {
	var (
		o                 observation
		id                string
		matched, reason   *string
		metaOp            *string
		created           *time.Time
		paymentIntentArgs *string
	)
	if op := r.OperationID(); op != "" {
		metaOp = &op
	}
	if r.Created > 0 {
		t := time.Unix(r.Created, 0).UTC()
		created = &t
	}
	if r.PaymentIntent != "" {
		paymentIntentArgs = &r.PaymentIntent
	}
	err := s.db.QueryRow(ctx, `SELECT observation_id::text, matched_operation_id::text, quarantined, quarantine_reason, result,
		resolved FROM business.refund_observation_record($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7, $8, $9, $10::uuid, $11)`,
		s.args(collection, r.ID, r.Amount, r.Currency, r.Status, paymentIntentArgs, metaOp, created)...).Scan(
		&id, &matched, &o.Quarantined, &reason, &o.Result, &o.Resolved)
	o.Reason = deref(reason)
	return o, gatewayErr(err)
}

// operations are the admitted refund operations of the leased reconcile's
// collection: unresolved, succeeded or failed (one row: the gateway's rows
// as JSON).
func (s refundStore) operations(ctx context.Context, collection string) ([]reconcileOp, error) {
	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT coalesce(json_agg(json_build_object('id', o.operation_id, 'state', o.operation_state,
		'amount_minor', o.amount_minor, 'payment_intent_id', o.payment_intent_id, 'provider_refund_id', o.provider_refund_id)
		ORDER BY o.dispatch_started_at, o.operation_id), '[]')
		FROM business.refund_reconcile_operations($1::uuid, $2::uuid, $3, $4::uuid) o`, s.args(collection)...).Scan(&raw)
	if err != nil {
		return nil, gatewayErr(err)
	}
	var rows []struct {
		ID               string  `json:"id"`
		State            string  `json:"state"`
		AmountMinor      int64   `json:"amount_minor"`
		PaymentIntentID  *string `json:"payment_intent_id"`
		ProviderRefundID *string `json:"provider_refund_id"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("payments: decode refund operations: %w", err)
	}
	ops := make([]reconcileOp, 0, len(rows))
	for _, r := range rows {
		ops = append(ops, reconcileOp{ID: r.ID, State: r.State, AmountMinor: r.AmountMinor,
			PaymentIntentID: deref(r.PaymentIntentID), ProviderRefundID: deref(r.ProviderRefundID)})
	}
	return ops, nil
}

// decide records a reconcile verdict for one operation
// (refund_reconcile_record): its state afterwards and whether its
// idempotency window has ended.
func (s refundStore) decide(ctx context.Context, opID, verdict, refundID string, ev RefundEvidence, reason string) (string, bool, error) {
	body, err := json.Marshal(ev)
	if err != nil {
		return "", false, err
	}
	var (
		state       string
		ended       bool
		status, why *string
	)
	if refundID != "" && ev.Status != "" {
		status = &ev.Status
	}
	if reason != "" {
		why = &reason
	}
	err = s.db.QueryRow(ctx, `SELECT operation_state, window_ended FROM business.refund_reconcile_record(
		$1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7, $8::json, $9)`,
		s.args(opID, verdict, nullString(refundID), status, string(body), why)...).Scan(&state, &ended)
	if err != nil {
		return "", false, gatewayErr(err)
	}
	return state, ended, nil
}
