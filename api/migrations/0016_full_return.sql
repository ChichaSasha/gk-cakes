-- GK-025 A3c2: the minimum full-return core: refund goals, refund
-- operations dispatched to Stripe under K11, refund reconciliation and the
-- quarantine of external refund observations, for a payment captured late on
-- an order that can no longer be fulfilled (contracts r6.5 K04 "Full-return
-- recovery is mandatory for terminal orders that cannot be fulfilled", "An
-- unresolved outcome never implies refund already sent"; K05 refund states,
-- S/R accounting, one unresolved operation per collection, external
-- observation quarantine; K11 dispatch protection). GK-026 extends this
-- (customer/chef cancellation, partial goals and allocations, the Q5 manual
-- alternative) without a second subsystem.
--
-- Goal (refund_goal). One versioned goal per order: target_minor is the
-- cumulative whole-order obligation (an obligation, not reserved money);
-- raising it bumps the version and never sends a second request while an
-- operation is unresolved.
--
-- Operation (refund_operation). One per attempt to return money from one
-- collection: its ID is the Stripe idempotency key and the K11 recovery
-- operation (kind 'dispatch', aggregate = the operation, prior version 0).
-- States (K05): reserved (allocation recorded, nothing sent), submitting
-- (admitted: a request may be at Stripe), pending / requires_action (Stripe
-- accepted it, not final), unknown (the outcome was lost or contradicts the
-- operation), succeeded, failed (definitive: nothing was or will be
-- returned by it; the remaining obligation reopens), cancelled (withdrawn
-- before any request). At most one unresolved operation (reserved,
-- submitting, pending, unknown, requires_action) per collection. The exact
-- POST /v1/refunds form body is fixed at the first dispatch with
-- replay_until = first dispatch + 23 hours (Stripe keeps keys 24 hours) and
-- never rebuilt.
--
-- Observation (refund_observation). Every refund seen at Stripe for a
-- collection (from the reconcile's complete list or a refund event), once
-- per (account, mode, refund ID). One whose metadata names a local operation
-- of the collection with the same amount and currency (and no other refund
-- already recorded for it) is matched and counted through that operation;
-- any other is quarantined: it is real money counted as exposure, and an
-- unresolved quarantined observation blocks every new dispatch for the
-- collection until a manager resolves it (manager tooling: Not yet).
--
-- Accounting (K05), per collection C (succeeded cake collections now):
-- S = succeeded local operations + succeeded unmatched observations; R =
-- unresolved local operations + pending/requires_action unmatched
-- observations; each provider refund counted once. room = max(0, C - S - R);
-- exposure = S + R (the money that may have gone back). A new operation may
-- take min(room, goal remaining) where goal remaining = max(0, target - the
-- order's S + R), and only when the collection has no unresolved operation
-- and no unresolved quarantined observation (business.refund_available).
--
-- Trigger: late capture on a terminal order. When a paid confirmation is
-- refused (the order changed or lost its holds before the success was
-- recorded: a released, cancelled order), business.order_command_apply
-- records, in the same transaction, the order's goal (target = all its
-- succeeded collections, reason late_capture_terminal, source system; once,
-- raised if another collection succeeds later) and reserves one operation
-- per succeeded collection for its available amount, with its dispatch job.
-- The order stays cancelled (never reopened), nothing is fulfilled and the
-- paid confirmation job still goes to attention (the manager sees it).
-- paid_awaiting_agreement creates no goal (honour or full return is a
-- chef/customer decision: GK-026).
--
-- order_get adds "refund" (the goal target, the succeeded total and a state:
-- requested, pending, partly_returned, returned or needs_attention; never
-- returned while unresolved). payment_event_record also schedules the
-- collection's refund reconcile for refund events (charge.refunded,
-- charge.refund.updated, refund.*).
--
-- Expand-only (K09): new tables, indexes and functions; the replaced
-- functions keep their signatures. Lock order (K00): the order, its
-- collections by ID, the goal, the operations, the recovery operation.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.refund_goal (
  id           uuid PRIMARY KEY,
  order_id     uuid NOT NULL UNIQUE REFERENCES business.orders (id),
  version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  -- The cumulative whole-order obligation (never more than paid; enforced
  -- by the functions that set it).
  target_minor bigint NOT NULL CHECK (target_minor >= 0),
  -- GK-026 extends the set (customer/chef cancellation, agreed partial).
  reason       text NOT NULL CHECK (reason IN ('late_capture_terminal')),
  source       text NOT NULL CHECK (source IN ('system', 'chef')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE business.refund_operation (
  -- Also the Stripe idempotency key and the dispatch's recovery operation.
  id                  uuid PRIMARY KEY,
  order_id            uuid NOT NULL REFERENCES business.orders (id),
  collection_id       uuid NOT NULL REFERENCES business.payment_collection (id),
  goal_id             uuid NOT NULL REFERENCES business.refund_goal (id),
  -- The goal version this allocation was reserved under.
  goal_version        integer NOT NULL CHECK (goal_version >= 1),
  -- manual_alternative is the accepted Q5 path (GK-026): unused yet.
  channel             text NOT NULL DEFAULT 'provider' CHECK (channel IN ('provider', 'manual_alternative')),
  state               text NOT NULL DEFAULT 'reserved'
                      CHECK (state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action', 'succeeded',
                                       'failed', 'cancelled')),
  amount_minor        bigint NOT NULL CHECK (amount_minor > 0),
  currency            text NOT NULL DEFAULT 'GBP' CHECK (currency = 'GBP'),
  environment         text NOT NULL CHECK (environment ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
  provider_account_id text NOT NULL CHECK (provider_account_id ~ '^acct_[A-Za-z0-9]{1,64}$'),
  livemode            boolean NOT NULL,
  -- The collection's payment intent (the refund's target).
  payment_intent_id   text NULL CHECK (payment_intent_id ~ '^pi_[A-Za-z0-9_]{1,250}$'),
  -- The exact dispatch payload (JSON text with the form-encoded request),
  -- fixed at the first dispatch; its hash is the recovery payload hash.
  payload             text NULL CHECK (octet_length(payload) BETWEEN 2 AND 16384),
  payload_sha256      char(64) NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  first_dispatch_at   timestamptz NULL,
  replay_until        timestamptz NULL,
  -- The first admitted call (a request may exist at Stripe from here on).
  dispatch_started_at timestamptz NULL,
  provider_refund_id  text NULL CHECK (provider_refund_id ~ '^(re|pyr)_[A-Za-z0-9_]{1,250}$'),
  provider_status     text NULL CHECK (provider_status ~ '^[a-z_]{1,40}$'),
  failure_reason      text NULL CHECK (failure_reason ~ '^[a-z][a-z0-9_]{0,63}$'),
  -- Bounded evidence: IDs, statuses, amounts, currency, times only.
  evidence            json NULL CHECK (evidence IS NULL OR octet_length(evidence::text) <= 4096),
  mismatch_reason     text NULL CHECK (length(mismatch_reason) BETWEEN 1 AND 200),
  mismatch_evidence   json NULL CHECK (mismatch_evidence IS NULL OR octet_length(mismatch_evidence::text) <= 4096),
  last_observed_at    timestamptz NULL,
  settled_at          timestamptz NULL,
  version             integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT refund_operation_window CHECK ((first_dispatch_at IS NULL) = (replay_until IS NULL)
                                            AND (replay_until IS NULL OR replay_until = first_dispatch_at + interval '23 hours')),
  CONSTRAINT refund_operation_body CHECK ((first_dispatch_at IS NULL) = (payload IS NULL)
                                          AND (payload IS NULL) = (payload_sha256 IS NULL)),
  CONSTRAINT refund_operation_admitted CHECK (dispatch_started_at IS NULL OR first_dispatch_at IS NOT NULL),
  -- Reserved: nothing sent; a state Stripe reports needs an admitted call.
  CONSTRAINT refund_operation_sent CHECK ((state <> 'reserved' OR dispatch_started_at IS NULL)
                                          AND (state IN ('reserved', 'failed', 'cancelled') OR dispatch_started_at IS NOT NULL)),
  CONSTRAINT refund_operation_cancelled CHECK (state <> 'cancelled' OR dispatch_started_at IS NULL),
  CONSTRAINT refund_operation_settled CHECK ((state IN ('succeeded', 'failed', 'cancelled')) = (settled_at IS NOT NULL)),
  CONSTRAINT refund_operation_succeeded CHECK (state <> 'succeeded' OR provider_refund_id IS NOT NULL)
);

-- K05: at most one unresolved operation per collection.
CREATE UNIQUE INDEX refund_operation_unresolved_idx ON business.refund_operation (collection_id)
  WHERE state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action');
CREATE UNIQUE INDEX refund_operation_provider_idx ON business.refund_operation (provider_account_id, livemode, provider_refund_id)
  WHERE provider_refund_id IS NOT NULL;
CREATE INDEX refund_operation_order_idx ON business.refund_operation (order_id, created_at);
CREATE INDEX refund_operation_collection_idx ON business.refund_operation (collection_id, created_at);

-- One row per Stripe refund create call, in order. Never updated.
CREATE TABLE business.refund_attempt (
  id                  uuid PRIMARY KEY,
  refund_operation_id uuid NOT NULL REFERENCES business.refund_operation (id),
  attempt_no          integer NOT NULL CHECK (attempt_no >= 1),
  job_id              uuid NOT NULL,
  job_generation      bigint NOT NULL CHECK (job_generation >= 1),
  -- succeeded / pending / requires_action / failed: a refund object with
  -- that status (Stripe's canceled is failed); rejected: definitively
  -- refused (nothing created); deferred: not processed now (rate limit);
  -- unknown: no or ambiguous response; refused: credentials or
  -- permissions; conflict: the key was used with another request.
  result              text NOT NULL CHECK (result IN ('succeeded', 'pending', 'requires_action', 'failed', 'rejected',
                                                      'deferred', 'unknown', 'refused', 'conflict')),
  http_status         integer NULL CHECK (http_status BETWEEN 100 AND 599),
  provider_code       text NULL CHECK (provider_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT refund_attempt_order UNIQUE (refund_operation_id, attempt_no)
);

CREATE TRIGGER refund_attempt_immutable BEFORE UPDATE OR DELETE ON business.refund_attempt
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();

-- Which refund work each job serves: the dispatch of one operation (job
-- operation = the refund operation) or the reconcile of a collection's
-- refunds (job operation = the collection; at most one waits).
CREATE TABLE business.refund_job (
  job_id              uuid PRIMARY KEY REFERENCES business.job (id),
  collection_id       uuid NOT NULL REFERENCES business.payment_collection (id),
  refund_operation_id uuid NULL REFERENCES business.refund_operation (id),
  purpose             text NOT NULL CHECK (purpose IN ('dispatch', 'reconcile')),
  created_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT refund_job_target CHECK ((purpose = 'dispatch') = (refund_operation_id IS NOT NULL))
);

CREATE UNIQUE INDEX refund_job_dispatch_idx ON business.refund_job (refund_operation_id) WHERE purpose = 'dispatch';
CREATE INDEX refund_job_collection_idx ON business.refund_job (collection_id, purpose);

-- Refunds seen at Stripe (see the header). Minimized: IDs, amounts,
-- statuses, times; never customer or card details.
CREATE TABLE business.refund_observation (
  id                    uuid PRIMARY KEY,
  collection_id         uuid NOT NULL REFERENCES business.payment_collection (id),
  provider_account_id   text NOT NULL CHECK (provider_account_id ~ '^acct_[A-Za-z0-9]{1,64}$'),
  livemode              boolean NOT NULL,
  provider_refund_id    text NOT NULL CHECK (provider_refund_id ~ '^(re|pyr)_[A-Za-z0-9_]{1,250}$'),
  amount_minor          bigint NOT NULL CHECK (amount_minor >= 0),
  -- As Stripe reports it (lower case); anything but gbp is quarantined.
  currency              text NOT NULL CHECK (currency ~ '^[a-z]{3}$'),
  -- Stripe's refund status (pending, requires_action, succeeded, failed,
  -- canceled), the latest seen.
  status                text NOT NULL CHECK (status ~ '^[a-z_]{1,40}$'),
  -- metadata[refund_operation_id] as received (not trusted on its own).
  metadata_operation_id uuid NULL,
  matched_operation_id  uuid NULL REFERENCES business.refund_operation (id),
  quarantined           boolean NOT NULL,
  quarantine_reason     text NULL CHECK (quarantine_reason IN ('unmatched', 'amount_mismatch', 'currency_mismatch',
                                                                'payment_intent_mismatch', 'operation_already_refunded')),
  source                text NOT NULL CHECK (source IN ('reconcile', 'webhook')),
  provider_created_at   timestamptz NULL,
  observed_at           timestamptz NOT NULL DEFAULT now(),
  last_observed_at      timestamptz NOT NULL DEFAULT now(),
  -- A manager's resolution of a quarantined observation (tooling: Not yet).
  resolved_at           timestamptz NULL,
  resolved_by           uuid NULL REFERENCES business.account (id),
  resolution_note       text NULL CHECK (length(resolution_note) BETWEEN 1 AND 500),
  CONSTRAINT refund_observation_identity UNIQUE (provider_account_id, livemode, provider_refund_id),
  CONSTRAINT refund_observation_quarantine CHECK (quarantined = (matched_operation_id IS NULL)
                                                  AND quarantined = (quarantine_reason IS NOT NULL)),
  CONSTRAINT refund_observation_resolution CHECK ((resolved_at IS NULL) = (resolution_note IS NULL)
                                                  AND (resolved_at IS NULL OR quarantined)
                                                  AND (resolved_by IS NULL OR resolved_at IS NOT NULL))
);

CREATE INDEX refund_observation_collection_idx ON business.refund_observation (collection_id, observed_at);
CREATE UNIQUE INDEX refund_observation_matched_idx ON business.refund_observation (matched_operation_id)
  WHERE matched_operation_id IS NOT NULL;

-- Owner-internal: the K05 balance of one collection (see the header).
-- paid is the collection's amount once succeeded (0 otherwise); local and
-- external split the exposure between local operations and unmatched
-- observations; unresolved: an unresolved local operation exists;
-- quarantined: an unresolved quarantined observation exists.
CREATE FUNCTION business.refund_balance(p_collection uuid)
RETURNS TABLE (paid_minor bigint, succeeded_minor bigint, reserved_minor bigint, local_minor bigint,
               external_minor bigint, exposure_minor bigint, room_minor bigint, unresolved boolean, quarantined boolean)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH c AS (
    SELECT CASE WHEN pc.state = 'succeeded' THEN pc.amount_minor ELSE 0 END AS paid
    FROM business.payment_collection pc WHERE pc.id = p_collection
  ), l AS (
    SELECT coalesce(sum(o.amount_minor) FILTER (WHERE o.state = 'succeeded'), 0)::bigint AS s,
           coalesce(sum(o.amount_minor) FILTER (WHERE o.state IN ('reserved', 'submitting', 'pending', 'unknown',
                                                                  'requires_action')), 0)::bigint AS r
    FROM business.refund_operation o WHERE o.collection_id = p_collection
  ), x AS (
    SELECT coalesce(sum(b.amount_minor) FILTER (WHERE b.status = 'succeeded'), 0)::bigint AS s,
           coalesce(sum(b.amount_minor) FILTER (WHERE b.status IN ('pending', 'requires_action')), 0)::bigint AS r,
           coalesce(bool_or(b.resolved_at IS NULL), false) AS q
    FROM business.refund_observation b WHERE b.collection_id = p_collection AND b.matched_operation_id IS NULL
  )
  SELECT c.paid, l.s + x.s, l.r + x.r, l.s + l.r, x.s + x.r, l.s + l.r + x.s + x.r,
         GREATEST(0, c.paid - l.s - l.r - x.s - x.r),
         l.r > 0, x.q
  FROM c, l, x;
$$;

-- Owner-internal: what the order's goal still needs beyond everything
-- returned or reserved across its collections (0 without a goal).
CREATE FUNCTION business.refund_goal_remaining(p_order uuid)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce((SELECT GREATEST(0, g.target_minor - coalesce((
                     SELECT sum(b.succeeded_minor + b.reserved_minor)
                     FROM business.payment_collection pc
                     CROSS JOIN LATERAL business.refund_balance(pc.id) b
                     WHERE pc.order_id = g.order_id), 0))::bigint
                   FROM business.refund_goal g WHERE g.order_id = p_order), 0);
$$;

-- Owner-internal: what a new operation may return from the collection now
-- (K05): 0 while an operation is unresolved, a quarantined observation is
-- unresolved or an operation carries contradicting evidence (no automatic
-- payout after a contradiction; a manager clears it), otherwise min(room,
-- goal remaining).
CREATE FUNCTION business.refund_available(p_collection uuid)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN b.unresolved OR b.quarantined
                   OR EXISTS (SELECT 1 FROM business.refund_operation ro
                              WHERE ro.collection_id = pc.id AND ro.mismatch_reason IS NOT NULL) THEN 0
              ELSE LEAST(b.room_minor, business.refund_goal_remaining(pc.order_id)) END
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b
  WHERE pc.id = p_collection;
$$;

-- Owner-internal: schedule the collection's refund reconcile no later than
-- p_at: a waiting one is brought forward, otherwise a new one is enqueued
-- (at most one waiting per collection: the collection row lock).
CREATE FUNCTION business.refund_schedule_reconcile(p_collection uuid, p_at timestamptz)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_job uuid;
BEGIN
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = p_collection FOR UPDATE;
  SELECT j.id INTO v_job
  FROM business.refund_job rj JOIN business.job j ON j.id = rj.job_id
  WHERE rj.collection_id = p_collection AND rj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')
  ORDER BY j.next_attempt_at, j.id
  LIMIT 1
  FOR UPDATE OF j;
  IF FOUND THEN
    UPDATE business.job j
    SET next_attempt_at = LEAST(j.next_attempt_at, GREATEST(p_at, pg_catalog.now())),
        version = j.version + 1, updated_at = pg_catalog.now()
    WHERE j.id = v_job;
    RETURN v_job;
  END IF;
  v_job := business.job_enqueue(p_collection, p_at);
  INSERT INTO business.refund_job (job_id, collection_id, purpose) VALUES (v_job, p_collection, 'reconcile');
  RETURN v_job;
END;
$$;

-- Owner-internal: reserve, for every succeeded collection of the order (by
-- ID), one operation for what is available now (business.refund_available,
-- the goal remaining shrinking as it is allocated), each with its dispatch
-- job and an audit event. The caller holds the order lock. Returns how many
-- were reserved (0 without a goal, while one is unresolved, under
-- quarantine, or when nothing remains). Sends nothing.
CREATE FUNCTION business.refund_reserve(p_order uuid)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_g business.refund_goal%ROWTYPE;
  c business.payment_collection%ROWTYPE;
  v_amount bigint;
  v_op uuid;
  v_job uuid;
  v_count integer := 0;
BEGIN
  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RETURN 0;
  END IF;
  FOR c IN SELECT * FROM business.payment_collection pc
           WHERE pc.order_id = p_order AND pc.state = 'succeeded' ORDER BY pc.id FOR UPDATE LOOP
    v_amount := business.refund_available(c.id);
    CONTINUE WHEN v_amount <= 0;
    v_op := pg_catalog.gen_random_uuid();
    INSERT INTO business.refund_operation (id, order_id, collection_id, goal_id, goal_version, amount_minor, environment,
                                           provider_account_id, livemode, payment_intent_id)
    VALUES (v_op, p_order, c.id, v_g.id, v_g.version, v_amount, c.environment, c.provider_account_id, c.livemode,
            c.payment_intent_id);
    v_job := business.job_enqueue(v_op, NULL);
    INSERT INTO business.refund_job (job_id, collection_id, refund_operation_id, purpose)
    VALUES (v_job, c.id, v_op, 'dispatch');
    PERFORM business.audit_record('system', NULL, 'refund.reserved', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('refund_operation_id', v_op, 'collection_id', c.id,
                                                               'amount_minor', v_amount, 'goal_version', v_g.version));
    v_count := v_count + 1;
  END LOOP;
  RETURN v_count;
END;
$$;

-- Owner-internal: the full return of a late capture on a terminal order
-- (see the header). The caller holds the order lock (and may hold its
-- collections'). Records the goal once (target = every succeeded
-- collection's amount), raises it when more was captured since, then
-- reserves what is available. Idempotent; returns the goal (NULL when
-- nothing was captured).
CREATE FUNCTION business.refund_late_capture(p_order uuid)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_paid bigint;
  v_g business.refund_goal%ROWTYPE;
BEGIN
  SELECT coalesce(sum(pc.amount_minor), 0) INTO v_paid
  FROM business.payment_collection pc WHERE pc.order_id = p_order AND pc.state = 'succeeded';
  IF v_paid = 0 THEN
    RETURN NULL;
  END IF;
  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source)
    VALUES (pg_catalog.gen_random_uuid(), p_order, v_paid, 'late_capture_terminal', 'system')
    RETURNING * INTO v_g;
    PERFORM business.audit_record('system', NULL, 'refund.goal_recorded', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_paid, 'reason', v_g.reason));
  ELSIF v_g.target_minor < v_paid THEN
    UPDATE business.refund_goal g
    SET target_minor = v_paid, version = g.version + 1, updated_at = pg_catalog.now()
    WHERE g.id = v_g.id
    RETURNING * INTO v_g;
    PERFORM business.audit_record('system', NULL, 'refund.goal_raised', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_paid, 'reason', 'late_capture_terminal'));
  END IF;
  PERFORM business.refund_reserve(p_order);
  RETURN v_g.id;
END;
$$;

-- As in 0014; a refused paid confirmation (a late capture on an order that
-- can no longer accept it) also records the full return (see the header).
CREATE OR REPLACE FUNCTION business.order_command_apply(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS TABLE (op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_o business.orders%ROWTYPE;
  v_c business.payment_collection%ROWTYPE;
  v_r business.recovery_operation%ROWTYPE;
  v_expires timestamptz;
  v_reason text;
  v_lot record;
  v_outcome text;
  v_lifecycle text;
  v_state text;
  v_frozen boolean;
BEGIN
  -- Lock order (K00): the order, its collection, then its recovery
  -- operation, then the day and lots inside the engines.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  IF v.command = 'paid_confirm' THEN
    SELECT * INTO v_c FROM business.payment_collection c WHERE c.id = v.collection_id FOR UPDATE;
  END IF;
  SELECT * INTO v_r FROM business.recovery_operation r WHERE r.id = v.id FOR UPDATE;
  IF v_r.state IN ('committed', 'secured') THEN
    RETURN QUERY SELECT v_r.state, v_r.outcome, v_r.outcome_hash::text;
    RETURN;
  ELSIF v_r.state <> 'prepared' THEN
    RAISE EXCEPTION 'recovery operation % is %, not prepared', v.id, v_r.state USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- A restore freeze stops the commit even if it landed after the worker's
  -- last check: the share lock makes a concurrent freeze wait for this
  -- commit, or this commit see the freeze.
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'frozen' USING ERRCODE = 'GK002';
  END IF;

  IF v.command = 'paid_confirm' THEN
    IF v_o.version <> v.prior_version OR v_o.lifecycle <> 'awaiting_payment' OR v_o.recovery_state <> 'prepared'
       OR v_o.payment_requirement <> 'full' OR v_c.state IS DISTINCT FROM 'succeeded' THEN
      v_reason := 'order_changed';
    ELSIF NOT business.order_holds_intact(v.order_id) THEN
      v_reason := 'holds_released';
    END IF;
  ELSE
    SELECT q.expires_at INTO v_expires FROM business.checkout_quote q WHERE q.id = v_o.quote_id;
    IF v_o.version <> v.prior_version OR v_o.lifecycle <> 'awaiting_payment' OR v_o.recovery_state <> 'prepared'
       OR v_o.payment_requirement <> 'none' THEN
      v_reason := 'order_changed';
    ELSIF NOT business.order_holds_intact(v.order_id) THEN
      v_reason := 'holds_released';
    ELSIF v.received_at >= v_expires OR v_o.original_handover_at - v.received_at < business.schedule_lead_time() THEN
      v_reason := 'window_closed';
    END IF;
  END IF;

  IF v_reason IS NULL THEN
    IF EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = v.order_id) THEN
      PERFORM business.capacity_commit(v.order_id, v_o.handover_day);
    END IF;
    FOR v_lot IN SELECT r.lot_id, r.quantity FROM business.order_lot_reservation r
                 WHERE r.order_id = v.order_id ORDER BY r.lot_id LOOP
      PERFORM business.lot_settle(v_lot.lot_id, v_lot.quantity, true);
      UPDATE business.order_lot_reservation r SET state = 'sold' WHERE r.order_id = v.order_id AND r.lot_id = v_lot.lot_id;
    END LOOP;
    UPDATE business.code_application c SET state = 'consumed', updated_at = pg_catalog.now()
    WHERE c.order_id = v.order_id;
    IF v.command = 'paid_confirm' THEN
      -- K04: 192 elapsed hours from the authoritative success to the
      -- handover, equality allowed; otherwise Q2 paid_awaiting_agreement.
      v_lifecycle := CASE WHEN v_o.original_handover_at - v.received_at >= business.schedule_lead_time()
                          THEN 'confirmed' ELSE 'paid_awaiting_agreement' END;
      UPDATE business.orders o
      SET lifecycle = v_lifecycle, paid_at = v.received_at, recovery_state = 'confirmation_pending',
          version = o.version + 1, updated_at = pg_catalog.now()
      WHERE o.id = v.order_id;
      v_outcome := pg_catalog.format('{"status":"%s","evidence":{"order_version":%s,"collection_id":"%s"}}',
                                     v_lifecycle, v_o.version + 1, v.collection_id);
      PERFORM business.audit_record('system', NULL,
                                    CASE WHEN v_lifecycle = 'confirmed' THEN 'order.paid_confirmed'
                                         ELSE 'order.paid_awaiting_agreement' END,
                                    'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id, 'collection_id', v.collection_id));
    ELSE
      UPDATE business.orders o
      SET lifecycle = 'confirmed', free_confirmed_at = v.received_at, recovery_state = 'confirmation_pending',
          version = o.version + 1, updated_at = pg_catalog.now()
      WHERE o.id = v.order_id;
      v_outcome := pg_catalog.format('{"status":"confirmed","evidence":{"order_version":%s}}', v_o.version + 1);
      PERFORM business.audit_record(CASE WHEN v.actor_account_id IS NULL THEN 'system' ELSE 'customer' END,
                                    v.actor_account_id, 'order.free_confirmed', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id));
    END IF;
  ELSE
    -- Nothing changes but the version: later commands reserve the next one.
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    IF v.command = 'paid_confirm' THEN
      -- The money is real and stays succeeded: the full return is
      -- recorded now (goal + reserved operation); the order is never
      -- reopened and the worker still moves the job to attention.
      v_outcome := pg_catalog.format('{"status":"refused","evidence":{"reason":"%s","collection_id":"%s"}}',
                                     v_reason, v.collection_id);
      PERFORM business.audit_record('system', NULL, 'order.paid_confirmation_refused', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason,
                                                                 'collection_id', v.collection_id));
      PERFORM business.refund_late_capture(v.order_id);
    ELSE
      v_outcome := pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', v_reason);
      PERFORM business.audit_record('system', NULL, 'order.free_confirmation_refused', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason));
    END IF;
  END IF;
  SELECT c.op_state INTO v_state
  FROM business.recovery_mark_committed(v.id, v_outcome::json,
         pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_outcome, 'UTF8')), 'hex'), false) c;
  SELECT * INTO v_r FROM business.recovery_operation r WHERE r.id = v.id;
  RETURN QUERY SELECT v_r.state, v_r.outcome, v_r.outcome_hash::text;
END;
$$;



-- Refund dispatch and reconciliation (worker), lease-fenced like the
-- payment gateway of 0014/0015.
--
-- Dispatch. The job's operation is the refund operation. refund_job_begin
-- returns what the purpose needs; before the first dispatch the worker
-- (holding its Stripe key and journal) builds the exact POST /v1/refunds
-- request (payment_intent, amount, reason=requested_by_customer,
-- metadata[order_id|collection_id|refund_operation_id|environment]) and
-- refund_dispatch_fix validates and stores it with replay_until = the first
-- dispatch + 23 hours (never rebuilt). The worker reserves the K11 recovery
-- operation with that payload's hash (refund_recovery_reserve), writes the
-- prepared envelope (refund_recovery_prepared) and, right before every
-- call, asks refund_dispatch_admit: under the collection and operation
-- locks it re-checks the restore freeze, that no unresolved quarantined
-- observation exists for the collection, that the operation is not decided
-- or withdrawn and that the window is open, then records
-- dispatch_started_at (first admission) and reserved -> submitting. Every
-- call is appended by refund_attempt_record (an unknown, deferred, refused
-- or conflicting answer leaves the operation unknown); a recordable answer
-- is committed by refund_recovery_committed, which verifies the refund
-- object against the operation (amount, currency gbp, payment intent, mode)
-- and settles it: succeeded; pending / requires_action (a reconcile in 15
-- minutes); failed (Stripe's failed or canceled: the remaining obligation
-- reopens; a new operation is reserved only by a later decision, never
-- automatically); rejected (a definitive 400: failed, and the collection's
-- refunds are reconciled now: a refund made elsewhere is a common cause); a
-- contradicting object leaves it unknown with the mismatch and schedules a
-- reconcile.
-- refund_recovery_reconcile (idempotency conflict, or the window ended
-- before an answer) marks the recovery operation for reconciliation: an
-- admitted operation stays unknown (a reconcile is scheduled), one never
-- admitted is withdrawn (cancelled: provably nothing was sent).
--
-- Reconcile. The job's operation is the collection (at most one waits;
-- scheduled by a pending/unknown outcome and by refund events). The worker
-- lists the payment intent's refunds completely (all pages) and records
-- each with refund_observation_record (matched or quarantined, once per
-- refund), then decides every admitted unresolved operation of the
-- collection (refund_reconcile_operations) with refund_reconcile_record:
-- the refund's status, 'not_found' (no refund for it in the complete list:
-- it stays unresolved and never fails on a timer; past its window the
-- worker asks a manager) or 'mismatch'.

-- Owner-internal: the refund job the caller holds a live lease on (GK001
-- when the lease is not held; NULL when the job is not a refund job).
CREATE FUNCTION business.refund_leased(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS business.refund_job
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_operation uuid;
  v business.refund_job;
BEGIN
  SELECT j.operation_id INTO v_operation
  FROM business.job j
  WHERE j.id = p_job_id AND j.state = 'running' AND j.lease_owner = p_worker AND j.generation = p_generation
    AND j.lease_until > pg_catalog.now()
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'job % is not leased to this worker generation', p_job_id USING ERRCODE = 'GK001';
  END IF;
  SELECT * INTO v FROM business.refund_job rj
  WHERE rj.job_id = p_job_id
    AND ((rj.purpose = 'dispatch' AND rj.refund_operation_id = v_operation)
         OR (rj.purpose = 'reconcile' AND rj.collection_id = v_operation));
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  RETURN v;
END;
$$;

-- Owner-internal: the leased dispatch job of refund operation p_operation_id.
CREATE FUNCTION business.refund_leased_dispatch(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS business.refund_job
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.job_id IS NULL OR v.purpose <> 'dispatch' OR v.refund_operation_id IS DISTINCT FROM p_operation_id THEN
    RAISE EXCEPTION 'refund operation % is not the dispatch of job %', p_operation_id, p_job_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v;
END;
$$;

-- Owner-internal: the leased reconcile job of collection p_collection.
CREATE FUNCTION business.refund_leased_reconcile(p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid)
RETURNS business.refund_job
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.job_id IS NULL OR v.purpose <> 'reconcile' OR v.collection_id IS DISTINCT FROM p_collection THEN
    RAISE EXCEPTION 'collection % is not the refund reconcile of job %', p_collection, p_job_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v;
END;
$$;

-- Owner-internal: whether the collection has an unresolved quarantined
-- observation (it blocks every new dispatch).
CREATE FUNCTION business.refund_quarantined(p_collection uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.refund_observation b
                 WHERE b.collection_id = p_collection AND b.quarantined AND b.resolved_at IS NULL);
$$;

-- Worker: start (or resume) the leased refund job. status: 'not_refund'
-- (another handler owns the job), 'frozen' (restore), 'closed' (a dispatch
-- whose operation was withdrawn before its request was fixed: nothing to
-- send), 'no_payment_intent' (the collection has no payment intent to
-- refund: manager), or 'ready'. A dispatch returns its operation (payload
-- NULL until fixed: the worker builds it from db_now and calls
-- refund_dispatch_fix); a dispatch whose operation a reconcile already
-- decided is still 'ready' (admission answers 'settled', so the protected
-- operation is closed without a call). A reconcile returns the collection,
-- its payment intent and amount (operation fields NULL).
CREATE FUNCTION business.refund_job_begin(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS TABLE (status text, purpose text, collection_id uuid, order_id uuid, operation_id uuid, operation_state text,
               amount_minor bigint, payment_intent_id text, provider_refund_id text, payload text,
               replay_until timestamptz, dispatch_started_at timestamptz, db_now timestamptz, environment text,
               account text, livemode boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased(p_job_id, p_worker, p_generation);
  c business.payment_collection%ROWTYPE;
  o business.refund_operation%ROWTYPE;
BEGIN
  IF v.job_id IS NULL THEN
    RETURN QUERY SELECT 'not_refund'::text, NULL::text, NULL::uuid, NULL::uuid, NULL::uuid, NULL::text, NULL::bigint,
      NULL::text, NULL::text, NULL::text, NULL::timestamptz, NULL::timestamptz, NULL::timestamptz, NULL::text,
      NULL::text, NULL::boolean;
    RETURN;
  END IF;
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id;
  IF business.recovery_frozen() THEN
    RETURN QUERY SELECT 'frozen'::text, v.purpose, c.id, c.order_id, v.refund_operation_id, NULL::text, NULL::bigint,
      NULL::text, NULL::text, NULL::text, NULL::timestamptz, NULL::timestamptz, pg_catalog.now(), c.environment,
      c.provider_account_id, c.livemode;
    RETURN;
  END IF;
  IF v.purpose = 'reconcile' THEN
    RETURN QUERY SELECT CASE WHEN c.payment_intent_id IS NULL THEN 'no_payment_intent' ELSE 'ready' END, v.purpose, c.id,
      c.order_id, NULL::uuid, NULL::text, c.amount_minor, c.payment_intent_id, NULL::text, NULL::text,
      NULL::timestamptz, NULL::timestamptz, pg_catalog.now(), c.environment, c.provider_account_id, c.livemode;
    RETURN;
  END IF;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id;
  RETURN QUERY SELECT
    CASE WHEN o.first_dispatch_at IS NULL AND o.state IN ('cancelled', 'failed', 'succeeded') THEN 'closed'
         WHEN coalesce(o.payment_intent_id, c.payment_intent_id) IS NULL THEN 'no_payment_intent'
         ELSE 'ready' END,
    v.purpose, c.id, o.order_id, o.id, o.state, o.amount_minor, coalesce(o.payment_intent_id, c.payment_intent_id),
    o.provider_refund_id, o.payload, o.replay_until, o.dispatch_started_at, pg_catalog.now(), o.environment,
    o.provider_account_id, o.livemode;
END;
$$;

-- Worker: fix the leased dispatch's exact request at its first dispatch.
-- p_at is the database instant the worker built it from (refund_job_begin's
-- db_now, at most a minute old). The payload must belong to this operation:
-- {"v":1,"provider":"stripe","kind":"refund","environment","account",
-- "livemode","idempotency_key"=operation,"refund_operation_id","order_id",
-- "collection_id","payment_intent_id","amount_minor","request"} with the
-- form pairs payment_intent, amount, reason=requested_by_customer and the
-- four metadata keys. status: 'ready' (the fixed payload and window,
-- unchanged when already fixed: p_payload is then ignored), 'frozen',
-- 'closed' (withdrawn or decided before any request), 'blocked' (an
-- unresolved quarantined observation on the collection) or
-- 'no_payment_intent'; only 'ready' fixes anything.
CREATE FUNCTION business.refund_dispatch_fix(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_payload text, p_at timestamptz)
RETURNS TABLE (status text, payload text, replay_until timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  o business.refund_operation%ROWTYPE;
  v_pi text;
  v_p json;
  v_request text;
BEGIN
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id FOR UPDATE;
  IF o.first_dispatch_at IS NOT NULL THEN
    RETURN QUERY SELECT 'ready'::text, o.payload, o.replay_until;
    RETURN;
  END IF;
  IF o.state IN ('cancelled', 'failed', 'succeeded') THEN
    RETURN QUERY SELECT 'closed'::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF business.recovery_frozen() THEN
    RETURN QUERY SELECT 'frozen'::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF business.refund_quarantined(o.collection_id) THEN
    RETURN QUERY SELECT 'blocked'::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  v_pi := coalesce(o.payment_intent_id, (SELECT pc.payment_intent_id FROM business.payment_collection pc
                                         WHERE pc.id = o.collection_id));
  IF v_pi IS NULL THEN
    RETURN QUERY SELECT 'no_payment_intent'::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF p_at IS NULL OR p_at > pg_catalog.now() OR p_at <= pg_catalog.now() - interval '1 minute' THEN
    RAISE EXCEPTION 'dispatch instant must be the database clock of the last minute' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  BEGIN
    v_p := p_payload::json;
  EXCEPTION WHEN others THEN
    v_p := NULL;
  END;
  IF v_p IS NOT NULL AND pg_catalog.json_typeof(v_p) = 'object' THEN
    v_request := v_p ->> 'request';
  END IF;
  IF v_request IS NULL OR octet_length(p_payload) > 16384
     OR (v_p ->> 'v') IS DISTINCT FROM '1' OR (v_p ->> 'provider') IS DISTINCT FROM 'stripe'
     OR (v_p ->> 'kind') IS DISTINCT FROM 'refund'
     OR (v_p ->> 'environment') IS DISTINCT FROM o.environment
     OR (v_p ->> 'account') IS DISTINCT FROM o.provider_account_id
     OR (v_p ->> 'livemode') IS DISTINCT FROM o.livemode::text
     OR (v_p ->> 'idempotency_key') IS DISTINCT FROM o.id::text
     OR (v_p ->> 'refund_operation_id') IS DISTINCT FROM o.id::text
     OR (v_p ->> 'order_id') IS DISTINCT FROM o.order_id::text
     OR (v_p ->> 'collection_id') IS DISTINCT FROM o.collection_id::text
     OR (v_p ->> 'payment_intent_id') IS DISTINCT FROM v_pi
     OR (v_p ->> 'amount_minor') IS DISTINCT FROM o.amount_minor::text
     OR NOT business.payment_form_has(v_request, 'payment_intent', v_pi)
     OR NOT business.payment_form_has(v_request, 'amount', o.amount_minor::text)
     OR NOT business.payment_form_has(v_request, 'reason', 'requested_by_customer')
     OR NOT business.payment_form_has(v_request, 'metadata%5Border_id%5D', o.order_id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Bcollection_id%5D', o.collection_id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Brefund_operation_id%5D', o.id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Benvironment%5D', o.environment) THEN
    RAISE EXCEPTION 'refund payload does not belong to refund operation %', o.id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.refund_operation ro
  SET payload = p_payload,
      payload_sha256 = pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(p_payload, 'UTF8')), 'hex'),
      payment_intent_id = v_pi, first_dispatch_at = p_at, replay_until = p_at + interval '23 hours',
      version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = o.id
  RETURNING ro.payload, ro.replay_until INTO o.payload, o.replay_until;
  RETURN QUERY SELECT 'ready'::text, o.payload, o.replay_until;
END;
$$;

-- Worker: recovery_reserve for exactly the leased refund dispatch
-- (aggregate = the operation, prior version 0, the fixed payload's hash and
-- window, envelope recovery/<environment>/op/<id>/0000000000-<id>).
CREATE FUNCTION business.refund_recovery_reserve(
  p_job_id uuid, p_worker uuid, p_generation bigint,
  p_operation_id uuid, p_kind text, p_aggregate_id uuid, p_prior_version integer,
  p_payload_hash text, p_envelope_key text, p_replay_until timestamptz)
RETURNS TABLE (result text, op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  o business.refund_operation%ROWTYPE;
BEGIN
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id;
  IF p_kind IS DISTINCT FROM 'dispatch' OR p_aggregate_id IS DISTINCT FROM o.id OR p_prior_version IS DISTINCT FROM 0
     OR o.payload_sha256 IS NULL OR p_payload_hash IS DISTINCT FROM o.payload_sha256::text OR o.replay_until IS NULL
     OR p_replay_until IS DISTINCT FROM o.replay_until
     OR p_envelope_key IS DISTINCT FROM ('recovery/' || o.environment || '/op/' || o.id::text || '/0000000000-' || o.id::text) THEN
    RAISE EXCEPTION 'recovery operation % does not match the dispatch of refund operation %', p_operation_id, o.id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN QUERY SELECT * FROM business.recovery_reserve(p_operation_id, p_kind, p_aggregate_id, p_prior_version,
                                                      p_payload_hash, p_envelope_key, p_replay_until);
END;
$$;

CREATE FUNCTION business.refund_recovery_prepared(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  RETURN business.recovery_mark_prepared(v.refund_operation_id);
END;
$$;

-- Worker: admission right before the Stripe call, on the database clock
-- (see the header): 'ok' (call after this commits), 'frozen', 'blocked'
-- (quarantine, or the operation carries a mismatch), 'expired' (the
-- idempotency window has ended), 'closed' (withdrawn before any request) or
-- 'settled' (a reconcile already decided it: record its state as the
-- outcome without calling).
CREATE FUNCTION business.refund_dispatch_admit(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  o business.refund_operation%ROWTYPE;
BEGIN
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id FOR UPDATE;
  IF o.state = 'cancelled' THEN
    RETURN 'closed';
  ELSIF o.state IN ('succeeded', 'failed') THEN
    RETURN 'settled';
  ELSIF business.recovery_frozen() THEN
    RETURN 'frozen';
  ELSIF business.refund_quarantined(o.collection_id) OR o.mismatch_reason IS NOT NULL THEN
    RETURN 'blocked';
  ELSIF o.replay_until IS NULL OR pg_catalog.now() >= o.replay_until THEN
    RETURN 'expired';
  END IF;
  UPDATE business.refund_operation ro
  SET dispatch_started_at = coalesce(ro.dispatch_started_at, pg_catalog.now()),
      state = CASE WHEN ro.state = 'reserved' THEN 'submitting' ELSE ro.state END,
      version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = o.id;
  RETURN 'ok';
END;
$$;

-- Worker: record one Stripe refund create call. Evidence is kept even from a
-- worker whose lease has since been lost (the call happened). Any answer
-- that is not a refund object or a definitive refusal leaves a submitting
-- operation unknown.
CREATE FUNCTION business.refund_attempt_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid,
  p_result text, p_http_status integer, p_provider_code text)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  o business.refund_operation%ROWTYPE;
  v_no integer;
BEGIN
  SELECT ro.* INTO o
  FROM business.job j
  JOIN business.refund_job rj ON rj.job_id = j.id AND rj.purpose = 'dispatch' AND rj.refund_operation_id = j.operation_id
  JOIN business.refund_operation ro ON ro.id = rj.refund_operation_id
  WHERE j.id = p_job_id AND ro.id = p_operation_id AND p_worker IS NOT NULL AND p_generation BETWEEN 1 AND j.generation;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'attempt does not belong to job % generation %', p_job_id, p_generation
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Lock order (K00): the collection, then the operation.
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = o.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = o.id FOR UPDATE;
  IF o.dispatch_started_at IS NULL THEN
    RAISE EXCEPTION 'refund operation % was never admitted for dispatch', o.id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT coalesce(max(a.attempt_no), 0) + 1 INTO v_no FROM business.refund_attempt a WHERE a.refund_operation_id = o.id;
  INSERT INTO business.refund_attempt (id, refund_operation_id, attempt_no, job_id, job_generation, result, http_status,
                                       provider_code)
  VALUES (pg_catalog.gen_random_uuid(), o.id, v_no, p_job_id, p_generation, p_result, p_http_status, p_provider_code);
  IF p_result IN ('unknown', 'deferred', 'refused', 'conflict') THEN
    UPDATE business.refund_operation ro
    SET state = 'unknown', version = ro.version + 1, updated_at = pg_catalog.now()
    WHERE ro.id = o.id AND ro.state = 'submitting';
    -- The collection's refunds are looked at as well (a lost answer is
    -- matched by its metadata even if the dispatch never hears back).
    PERFORM business.refund_schedule_reconcile(o.collection_id, pg_catalog.now() + interval '5 minutes');
  END IF;
  RETURN v_no;
END;
$$;

-- Owner-internal: settle refund operation p_op (locked by the caller, with
-- its order and collection) from a refund object Stripe returned or listed:
-- p_status is the refund's status (canceled is failed), p_provider_status
-- Stripe's exact word. Only an unresolved operation moves; it returns the
-- state afterwards and audits the change.
CREATE FUNCTION business.refund_settle(p_op uuid, p_status text, p_provider_status text, p_refund_id text, p_evidence json,
                                       p_failure text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  o business.refund_operation%ROWTYPE;
  v_state text := CASE p_status WHEN 'canceled' THEN 'failed' ELSE p_status END;
BEGIN
  IF v_state IS NULL OR v_state NOT IN ('succeeded', 'pending', 'requires_action', 'failed') THEN
    RAISE EXCEPTION 'unsupported refund status %', p_status USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_op;
  IF o.state NOT IN ('submitting', 'pending', 'unknown', 'requires_action') THEN
    RETURN o.state;
  END IF;
  UPDATE business.refund_operation ro
  SET state = v_state, provider_refund_id = coalesce(ro.provider_refund_id, p_refund_id),
      provider_status = p_provider_status, evidence = p_evidence,
      failure_reason = CASE WHEN v_state = 'failed' THEN coalesce(p_failure, 'refund_failed') END,
      settled_at = CASE WHEN v_state IN ('succeeded', 'failed') THEN pg_catalog.now() END,
      last_observed_at = pg_catalog.now(), version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = o.id;
  IF v_state IN ('pending', 'requires_action') THEN
    PERFORM business.refund_schedule_reconcile(o.collection_id, pg_catalog.now() + interval '15 minutes');
  END IF;
  IF v_state IS DISTINCT FROM o.state THEN
    PERFORM business.audit_record('system', NULL, 'refund.' || v_state, 'order', o.order_id, NULL,
                                  pg_catalog.json_build_object('refund_operation_id', o.id, 'collection_id', o.collection_id,
                                                               'amount_minor', o.amount_minor));
  END IF;
  RETURN v_state;
END;
$$;

-- Owner-internal: record that refund operation p_op's evidence contradicts
-- it (the caller holds the locks): an unresolved operation becomes unknown;
-- a decided one keeps its state; the observation is kept beside it and a
-- reconcile is scheduled for an unresolved one.
CREATE FUNCTION business.refund_mismatch(p_op uuid, p_reason text, p_evidence json)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  o business.refund_operation%ROWTYPE;
BEGIN
  UPDATE business.refund_operation ro
  SET state = CASE WHEN ro.state IN ('submitting', 'pending', 'requires_action') THEN 'unknown' ELSE ro.state END,
      mismatch_reason = p_reason, mismatch_evidence = p_evidence, last_observed_at = pg_catalog.now(),
      version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = p_op
  RETURNING * INTO o;
  PERFORM business.audit_record('system', NULL, 'refund.mismatch', 'order', o.order_id, NULL,
                                pg_catalog.json_build_object('refund_operation_id', o.id, 'reason', p_reason));
  IF o.state = 'unknown' THEN
    PERFORM business.refund_schedule_reconcile(o.collection_id, pg_catalog.now());
  END IF;
  RETURN o.state;
END;
$$;

-- Worker: record the dispatch outcome (recovery committed) and settle the
-- operation (see the header). p_outcome: {"status": succeeded | pending |
-- requires_action | failed | rejected | settled, "evidence": {refund_id,
-- status, amount_minor, currency, payment_intent_id, livemode,
-- failure_reason, http_status, provider_code}}; a refund object is verified
-- against the operation before it is trusted. 'settled' (admission found
-- the operation decided by a reconcile) changes nothing. Later reconcile
-- evidence is never overwritten.
CREATE FUNCTION business.refund_recovery_committed(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_outcome json, p_outcome_hash text)
RETURNS TABLE (op_state text, changed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  v_status text := p_outcome ->> 'status';
  v_ev json := p_outcome -> 'evidence';
  o business.refund_operation%ROWTYPE;
  v_order uuid;
  v_state text;
  v_changed boolean;
  v_reason text;
BEGIN
  IF v_status IS NULL
     OR v_status NOT IN ('succeeded', 'pending', 'requires_action', 'failed', 'rejected', 'settled') THEN
    RAISE EXCEPTION 'unsupported refund dispatch outcome %', v_status USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Lock order (K00): the order, the collection, the operation.
  SELECT ro.order_id INTO v_order FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id;
  PERFORM 1 FROM business.orders ord WHERE ord.id = v_order FOR UPDATE;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id FOR UPDATE;
  SELECT c.op_state, c.changed INTO v_state, v_changed
  FROM business.recovery_mark_committed(p_operation_id, p_outcome, p_outcome_hash, false) c;
  IF v_changed AND v_status = 'rejected' THEN
    UPDATE business.refund_operation ro
    SET state = 'failed', failure_reason = coalesce(v_ev ->> 'provider_code', 'rejected_by_stripe'),
        evidence = v_ev, settled_at = pg_catalog.now(), version = ro.version + 1, updated_at = pg_catalog.now()
    WHERE ro.id = o.id AND ro.state IN ('submitting', 'unknown');
    IF FOUND THEN
      PERFORM business.audit_record('system', NULL, 'refund.failed', 'order', o.order_id, NULL,
                                    pg_catalog.json_build_object('refund_operation_id', o.id, 'reason', 'rejected_by_stripe'));
      -- A refusal may come from a refund made elsewhere (for example in
      -- the dashboard): look at the payment intent's refunds now.
      PERFORM business.refund_schedule_reconcile(o.collection_id, pg_catalog.now());
    END IF;
  ELSIF v_changed AND v_status <> 'settled' THEN
    v_reason := CASE
      WHEN (v_ev ->> 'refund_id') IS NULL OR (v_ev ->> 'refund_id') !~ '^(re|pyr)_[A-Za-z0-9_]{1,250}$'
        THEN 'refund object without a valid ID'
      WHEN o.provider_refund_id IS NOT NULL AND o.provider_refund_id <> (v_ev ->> 'refund_id')
        THEN 'another refund than the one recorded for this operation'
      WHEN (v_ev ->> 'amount_minor') IS DISTINCT FROM o.amount_minor::text THEN 'refund amount differs from the operation'
      WHEN (v_ev ->> 'currency') IS DISTINCT FROM 'gbp' THEN 'refund currency is not gbp'
      WHEN (v_ev ->> 'payment_intent_id') IS DISTINCT FROM o.payment_intent_id THEN 'refund of another payment intent'
      WHEN (v_ev ->> 'livemode') IS DISTINCT FROM o.livemode::text THEN 'refund created in the wrong Stripe mode'
      WHEN EXISTS (SELECT 1 FROM business.refund_operation x
                   WHERE x.provider_account_id = o.provider_account_id AND x.livemode = o.livemode
                     AND x.provider_refund_id = (v_ev ->> 'refund_id') AND x.id <> o.id)
        THEN 'refund already recorded for another operation'
    END;
    IF v_reason IS NOT NULL THEN
      PERFORM business.refund_mismatch(o.id, v_reason, v_ev);
    ELSE
      PERFORM business.refund_settle(o.id, v_ev ->> 'status', v_ev ->> 'status', v_ev ->> 'refund_id', v_ev,
                                     CASE WHEN (v_ev ->> 'failure_reason') ~ '^[a-z][a-z0-9_]{0,63}$'
                                          THEN v_ev ->> 'failure_reason' END);
    END IF;
  END IF;
  RETURN QUERY SELECT v_state, v_changed;
END;
$$;

CREATE FUNCTION business.refund_recovery_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  RETURN business.recovery_mark_secured(v.refund_operation_id);
END;
$$;

-- Worker: the dispatch outcome cannot be established by the dispatch
-- (idempotency conflict, or the window ended before an answer): the
-- recovery operation goes to reconciliation. An admitted operation stays
-- unknown and its collection's reconcile is scheduled now; one never
-- admitted is withdrawn (cancelled: no request was ever made).
CREATE FUNCTION business.refund_recovery_reconcile(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_reason text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  o business.refund_operation%ROWTYPE;
  v_state text;
BEGIN
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id FOR UPDATE;
  v_state := business.recovery_mark_reconcile(p_operation_id, p_reason);
  IF o.state = 'reserved' THEN
    UPDATE business.refund_operation ro
    SET state = 'cancelled', failure_reason = 'window_ended_before_dispatch', settled_at = pg_catalog.now(),
        version = ro.version + 1, updated_at = pg_catalog.now()
    WHERE ro.id = o.id;
    PERFORM business.audit_record('system', NULL, 'refund.cancelled', 'order', o.order_id, NULL,
                                  pg_catalog.json_build_object('refund_operation_id', o.id,
                                                               'reason', 'window_ended_before_dispatch'));
  ELSIF o.state IN ('submitting', 'pending', 'unknown', 'requires_action') THEN
    UPDATE business.refund_operation ro
    SET state = 'unknown', version = ro.version + 1, updated_at = pg_catalog.now()
    WHERE ro.id = o.id AND ro.state = 'submitting';
    PERFORM business.refund_schedule_reconcile(o.collection_id, pg_catalog.now());
  END IF;
  RETURN v_state;
END;
$$;

CREATE FUNCTION business.refund_recovery_frozen(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.job_id IS NULL THEN
    RAISE EXCEPTION 'job % is not a refund job', p_job_id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN business.recovery_frozen();
END;
$$;

-- Worker: the admitted, unresolved refund operations of the leased
-- reconcile's collection (what the reconcile must decide).
CREATE FUNCTION business.refund_reconcile_operations(p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid)
RETURNS TABLE (operation_id uuid, operation_state text, amount_minor bigint, payment_intent_id text,
               provider_refund_id text, dispatch_started_at timestamptz, replay_until timestamptz, mismatch_reason text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_reconcile(p_job_id, p_worker, p_generation, p_collection);
BEGIN
  RETURN QUERY
  SELECT ro.id, ro.state, ro.amount_minor, ro.payment_intent_id, ro.provider_refund_id, ro.dispatch_started_at,
         ro.replay_until, ro.mismatch_reason
  FROM business.refund_operation ro
  WHERE ro.collection_id = v.collection_id AND ro.dispatch_started_at IS NOT NULL
    AND ro.state IN ('submitting', 'pending', 'unknown', 'requires_action')
  ORDER BY ro.created_at, ro.id;
END;
$$;

-- Worker: record one refund seen at Stripe for the leased reconcile's
-- collection (p_source 'reconcile'; see the header): once per (account,
-- mode, refund ID); a later sighting updates only its status and time.
-- p_payment_intent_id must be the collection's (the list is by payment
-- intent). Matching: the operation named by p_metadata_operation_id (of
-- this collection) or the operation already holding this refund ID, with
-- the same amount, currency gbp and payment intent, and no other refund
-- recorded for it; anything else is quarantined with the reason. Returns
-- the observation, its match, whether it is quarantined (and why), result
-- 'recorded' (new), 'updated' (status changed) or 'unchanged', and whether a
-- manager has resolved its quarantine.
CREATE FUNCTION business.refund_observation_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid, p_refund_id text, p_amount_minor bigint,
  p_currency text, p_status text, p_payment_intent_id text, p_metadata_operation_id uuid,
  p_provider_created_at timestamptz)
RETURNS TABLE (observation_id uuid, matched_operation_id uuid, quarantined boolean, quarantine_reason text, result text,
               resolved boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_reconcile(p_job_id, p_worker, p_generation, p_collection);
  c business.payment_collection%ROWTYPE;
  b business.refund_observation%ROWTYPE;
  o business.refund_operation%ROWTYPE;
  v_reason text;
  v_result text;
BEGIN
  IF p_refund_id IS NULL OR p_refund_id !~ '^(re|pyr)_[A-Za-z0-9_]{1,250}$' OR p_amount_minor IS NULL OR p_amount_minor < 0
     OR p_currency IS NULL OR p_currency !~ '^[a-z]{3}$' OR p_status IS NULL OR p_status !~ '^[a-z_]{1,40}$' THEN
    RAISE EXCEPTION 'invalid refund observation' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  IF p_payment_intent_id IS DISTINCT FROM c.payment_intent_id THEN
    RAISE EXCEPTION 'refund % is not of the payment intent of collection %', p_refund_id, c.id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT * INTO b FROM business.refund_observation ob
  WHERE ob.provider_account_id = c.provider_account_id AND ob.livemode = c.livemode AND ob.provider_refund_id = p_refund_id
  FOR UPDATE;
  IF FOUND THEN
    IF b.collection_id <> c.id THEN
      RAISE EXCEPTION 'refund % is recorded for another collection', p_refund_id USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_result := 'unchanged';
    IF b.status IS DISTINCT FROM p_status THEN
      v_result := 'updated';
    END IF;
    UPDATE business.refund_observation ob SET status = p_status, last_observed_at = pg_catalog.now()
    WHERE ob.id = b.id;
    RETURN QUERY SELECT b.id, b.matched_operation_id, b.quarantined, b.quarantine_reason, v_result, b.resolved_at IS NOT NULL;
    RETURN;
  END IF;

  SELECT * INTO o FROM business.refund_operation ro
  WHERE ro.collection_id = c.id
    AND (ro.id = p_metadata_operation_id
         OR (ro.provider_account_id = c.provider_account_id AND ro.livemode = c.livemode
             AND ro.provider_refund_id = p_refund_id))
  ORDER BY (ro.provider_refund_id = p_refund_id) DESC NULLS LAST
  LIMIT 1
  FOR UPDATE;
  IF NOT FOUND THEN
    v_reason := CASE WHEN p_currency <> 'gbp' THEN 'currency_mismatch' ELSE 'unmatched' END;
  ELSIF o.provider_refund_id IS NOT NULL AND o.provider_refund_id <> p_refund_id
        OR EXISTS (SELECT 1 FROM business.refund_observation ob WHERE ob.matched_operation_id = o.id) THEN
    v_reason := 'operation_already_refunded';
  ELSIF p_currency <> 'gbp' THEN
    v_reason := 'currency_mismatch';
  ELSIF p_amount_minor <> o.amount_minor THEN
    v_reason := 'amount_mismatch';
  ELSIF o.payment_intent_id IS DISTINCT FROM p_payment_intent_id THEN
    v_reason := 'payment_intent_mismatch';
  END IF;
  INSERT INTO business.refund_observation (id, collection_id, provider_account_id, livemode, provider_refund_id, amount_minor,
                                           currency, status, metadata_operation_id, matched_operation_id, quarantined,
                                           quarantine_reason, source, provider_created_at)
  VALUES (pg_catalog.gen_random_uuid(), c.id, c.provider_account_id, c.livemode, p_refund_id, p_amount_minor, p_currency,
          p_status, p_metadata_operation_id, CASE WHEN v_reason IS NULL THEN o.id END, v_reason IS NOT NULL, v_reason,
          'reconcile', p_provider_created_at)
  RETURNING * INTO b;
  IF v_reason IS NOT NULL THEN
    PERFORM business.audit_record('system', NULL, 'refund.observation_quarantined', 'order', c.order_id, NULL,
                                  pg_catalog.json_build_object('observation_id', b.id, 'collection_id', c.id,
                                                               'amount_minor', p_amount_minor, 'reason', v_reason));
  END IF;
  RETURN QUERY SELECT b.id, b.matched_operation_id, b.quarantined, b.quarantine_reason, 'recorded'::text, false;
END;
$$;

-- Worker: decide one admitted refund operation of the leased reconcile's
-- collection from Stripe's evidence. p_verdict: succeeded | pending |
-- requires_action | failed (Stripe's failed or canceled; p_reason its
-- failure reason, optional) with p_refund_id and p_provider_status;
-- 'not_found' (no refund for it in the complete list: nothing changes but
-- the observation time; a submitting operation is unknown); 'mismatch'
-- (p_reason required). A decided operation keeps its state (a
-- contradiction is recorded beside it as a mismatch). Returns the state
-- afterwards and whether its idempotency window has ended (an unknown
-- operation past it needs a manager: never a blind new request).
CREATE FUNCTION business.refund_reconcile_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_verdict text, p_refund_id text,
  p_provider_status text, p_evidence json, p_reason text)
RETURNS TABLE (operation_state text, window_ended boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  o business.refund_operation%ROWTYPE;
  v business.refund_job;
  v_state text;
BEGIN
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_operation_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'refund operation % not found', p_operation_id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  v := business.refund_leased_reconcile(p_job_id, p_worker, p_generation, o.collection_id);
  IF p_verdict IS NULL OR p_verdict NOT IN ('succeeded', 'pending', 'requires_action', 'failed', 'not_found', 'mismatch')
     OR (p_verdict = 'mismatch' AND (p_reason IS NULL OR length(p_reason) NOT BETWEEN 1 AND 200))
     OR (p_verdict = 'failed' AND p_reason IS NOT NULL AND p_reason !~ '^[a-z][a-z0-9_]{0,63}$')
     OR (p_verdict IN ('succeeded', 'pending', 'requires_action', 'not_found') AND p_reason IS NOT NULL)
     OR (p_verdict IN ('succeeded', 'pending', 'requires_action', 'failed')) <> (p_refund_id IS NOT NULL)
     OR p_refund_id !~ '^(re|pyr)_[A-Za-z0-9_]{1,250}$'
     OR p_provider_status !~ '^[a-z_]{1,40}$' THEN
    RAISE EXCEPTION 'invalid refund reconcile verdict %', p_verdict USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Lock order (K00): the order, the collection, the operation.
  PERFORM 1 FROM business.orders ord WHERE ord.id = o.order_id FOR UPDATE;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = o.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_operation_id FOR UPDATE;
  IF o.dispatch_started_at IS NULL THEN
    RAISE EXCEPTION 'refund operation % was never admitted for dispatch', o.id USING ERRCODE = 'invalid_parameter_value';
  END IF;

  IF p_verdict = 'mismatch' THEN
    v_state := business.refund_mismatch(o.id, p_reason, p_evidence);
  ELSIF p_verdict = 'not_found' THEN
    UPDATE business.refund_operation ro
    SET state = CASE WHEN ro.state = 'submitting' THEN 'unknown' ELSE ro.state END,
        last_observed_at = pg_catalog.now(), version = ro.version + 1, updated_at = pg_catalog.now()
    WHERE ro.id = o.id
    RETURNING ro.state INTO v_state;
  ELSIF o.state IN ('succeeded', 'failed', 'cancelled') THEN
    IF o.state <> p_verdict THEN
      -- Never an automatic payout or reopening after a contradiction.
      v_state := business.refund_mismatch(o.id, 'Stripe now reports ' || p_verdict || ' for a ' || o.state || ' refund', p_evidence);
    ELSE
      UPDATE business.refund_operation ro SET last_observed_at = pg_catalog.now() WHERE ro.id = o.id;
      v_state := o.state;
    END IF;
  ELSIF o.provider_refund_id IS NOT NULL AND o.provider_refund_id <> p_refund_id THEN
    v_state := business.refund_mismatch(o.id, 'another refund than the one recorded for this operation', p_evidence);
  ELSE
    v_state := business.refund_settle(o.id, p_verdict, coalesce(p_provider_status, p_verdict), p_refund_id, p_evidence,
                                      p_reason);
  END IF;
  RETURN QUERY SELECT v_state, o.replay_until IS NOT NULL AND pg_catalog.now() >= o.replay_until;
END;
$$;

-- As in 0014; refund events also schedule the collection's refund reconcile.
CREATE OR REPLACE FUNCTION business.payment_event_record(
  p_env text, p_account text, p_livemode boolean, p_event_id text, p_event_type text, p_created timestamptz,
  p_object_type text, p_object_id text, p_metadata_collection uuid, p_metadata_env text, p_payload_sha256 text)
RETURNS TABLE (result text, event_collection_id uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_seen uuid;
  v_seen_collection uuid;
  v_c business.payment_collection%ROWTYPE;
BEGIN
  -- Serialize deliveries of one event so a concurrent retry is a duplicate.
  PERFORM pg_catalog.pg_advisory_xact_lock(
    pg_catalog.hashtextextended('gk.stripe_event:' || p_account || ':' || p_livemode::text || ':' || p_event_id, 0));
  SELECT e.id, e.collection_id INTO v_seen, v_seen_collection FROM business.stripe_event e
  WHERE e.account = p_account AND e.livemode = p_livemode AND e.event_id = p_event_id;
  IF FOUND THEN
    RETURN QUERY SELECT 'duplicate'::text, v_seen_collection;
    RETURN;
  END IF;
  IF p_metadata_collection IS NOT NULL AND p_metadata_env IS NOT DISTINCT FROM p_env THEN
    -- Locked before the event row references it (its foreign key would
    -- otherwise share-lock it first and concurrent deliveries deadlock when
    -- scheduling the reconcile).
    SELECT * INTO v_c FROM business.payment_collection c
    WHERE c.id = p_metadata_collection AND c.environment = p_env AND c.provider_account_id = p_account
      AND c.livemode = p_livemode
    FOR UPDATE;
  END IF;
  INSERT INTO business.stripe_event
    (id, account, livemode, event_id, event_type, environment, provider_created_at, object_type, object_id,
     metadata_collection_id, metadata_environment, collection_id, payload_sha256)
  VALUES (pg_catalog.gen_random_uuid(), p_account, p_livemode, p_event_id, p_event_type, p_env, p_created, p_object_type,
          p_object_id, p_metadata_collection, p_metadata_env, v_c.id, p_payload_sha256);
  IF v_c.id IS NOT NULL AND v_c.state IN ('created', 'pending', 'unknown') THEN
    PERFORM business.payment_schedule_reconcile(v_c.id, pg_catalog.now());
  END IF;
  -- A refund event for a succeeded collection brings its refund reconcile
  -- forward (it records unmatched refunds as quarantined observations).
  IF v_c.id IS NOT NULL AND v_c.state = 'succeeded'
     AND (p_event_type IN ('charge.refunded', 'charge.refund.updated') OR p_event_type LIKE 'refund.%') THEN
    PERFORM business.refund_schedule_reconcile(v_c.id, pg_catalog.now());
  END IF;
  RETURN QUERY SELECT 'recorded'::text, v_c.id;
END;
$$;

-- As in 0015, plus "refund" (the order's full return, when it has a goal).
CREATE OR REPLACE FUNCTION business.order_get(p_order uuid, p_owner uuid, p_guest_digest bytea)
RETURNS json
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_result json;
BEGIN
  SELECT pg_catalog.json_build_object(
           'id', o.id, 'version', o.version,
           -- K11: a commit not yet secured, or a recorded payment whose paid
           -- confirmation is not secured yet ("payment received,
           -- confirmation being secured"), is confirmation_pending.
           'lifecycle', CASE WHEN o.recovery_state = 'confirmation_pending'
                               OR (o.lifecycle = 'awaiting_payment'
                                   AND EXISTS (SELECT 1 FROM business.order_command pcmd
                                               JOIN business.recovery_operation pr ON pr.id = pcmd.id
                                               WHERE pcmd.order_id = o.id AND pcmd.command = 'paid_confirm'
                                                 AND pr.state <> 'secured'))
                             THEN 'confirmation_pending' ELSE o.lifecycle END,
           'cancel_reason', o.cancel_reason, 'cancelled_at', o.cancelled_at,
           'payment_requirement', o.payment_requirement,
           'placed_at', o.placed_at, 'date', o.handover_day, 'fulfillment_mode', o.fulfillment_mode,
           'slot', pg_catalog.json_build_object('id', o.slot_id, 'starts_at', o.original_handover_at, 'ends_at', o.handover_ends_at),
           'full_refund_deadline_at', o.full_refund_deadline_at, 'policy_version_id', o.policy_version_id,
           'customer_full_name', o.customer_full_name, 'different_payer_name', o.different_payer_name,
           'contact_email', o.contact_email, 'subtotal_minor', o.subtotal_minor, 'discount_minor', o.discount_minor,
           'total_minor', o.total_minor, 'currency', o.currency,
           'paid_at', CASE WHEN o.recovery_state = 'secured' THEN o.paid_at END,
           'free_confirmed_at', CASE WHEN o.recovery_state = 'secured' THEN o.free_confirmed_at END,
           'free_confirmation', (SELECT pg_catalog.json_build_object(
                                   'state', CASE WHEN r.state <> 'secured' THEN 'processing'
                                                 ELSE r.outcome ->> 'status' END,
                                   'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                                   'requested_at', c.received_at)
                                 FROM business.order_command c
                                 JOIN business.recovery_operation r ON r.id = c.id
                                 WHERE c.order_id = o.id AND c.command = 'free_confirm'
                                 ORDER BY c.created_at DESC, c.id DESC LIMIT 1),
           'payment', (SELECT pg_catalog.json_build_object(
                         'id', pc.id, 'state', pc.state, 'amount_minor', pc.amount_minor, 'currency', pc.currency,
                         'requested_at', pc.initiated_at,
                         'checkout_url', CASE WHEN pc.state = 'pending' AND pc.session_status = 'open'
                                                   AND pc.provider_expires_at > pg_catalog.now()
                                                   AND o.lifecycle = 'awaiting_payment'
                                                   AND pc.expire_requested_at IS NULL
                                              THEN pc.checkout_url END,
                         'expires_at', pc.provider_expires_at)
                       FROM business.payment_collection pc
                       WHERE pc.order_id = o.id
                       ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1),
           -- The full return (K05): requested until Stripe has it, pending
           -- while it is being processed or checked, returned only when
           -- every penny of the goal succeeded; never returned while
           -- unresolved.
           'refund', (SELECT pg_catalog.json_build_object(
                        'state', CASE WHEN rf.attention THEN 'needs_attention'
                                      WHEN rf.succeeded >= g.target_minor THEN 'returned'
                                      WHEN rf.succeeded > 0 THEN 'partly_returned'
                                      WHEN rf.sent THEN 'pending'
                                      ELSE 'requested' END,
                        'target_minor', g.target_minor, 'succeeded_minor', rf.succeeded,
                        'in_progress_minor', rf.reserved, 'currency', 'GBP', 'requested_at', g.created_at,
                        'goal_version', g.version)
                      FROM business.refund_goal g
                      CROSS JOIN LATERAL (
                        SELECT coalesce(sum(b.succeeded_minor), 0) AS succeeded,
                               coalesce(sum(b.reserved_minor), 0) AS reserved,
                               EXISTS (SELECT 1 FROM business.refund_operation ro
                                       WHERE ro.order_id = g.order_id AND ro.state IN ('submitting', 'pending', 'unknown'))
                                 AS sent,
                               -- requires action, a contradiction, a quarantined
                               -- observation, a dispatch under reconciliation, or a
                               -- failed return nothing replaced yet.
                               (EXISTS (SELECT 1 FROM business.refund_operation ro
                                        WHERE ro.order_id = g.order_id
                                          AND (ro.state = 'requires_action' OR ro.mismatch_reason IS NOT NULL
                                               OR (ro.state IN ('submitting', 'pending', 'unknown')
                                                   AND EXISTS (SELECT 1 FROM business.recovery_operation r
                                                               WHERE r.id = ro.id AND r.state = 'reconcile'))))
                                OR coalesce(bool_or(b.quarantined), false)
                                OR (EXISTS (SELECT 1 FROM business.refund_operation ro
                                            WHERE ro.order_id = g.order_id AND ro.state = 'failed')
                                    AND coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) < g.target_minor))
                                 AS attention
                        FROM business.payment_collection pc
                        CROSS JOIN LATERAL business.refund_balance(pc.id) b
                        WHERE pc.order_id = g.order_id) rf
                      WHERE g.order_id = o.id),
           'items', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                       'line_no', i.line_no, 'variant_id', i.variant_id, 'lot_id', i.physical_lot_id,
                       'recipe_version_id', i.recipe_version_id, 'cake_name', c.name, 'flavour', r.flavour_name,
                       'size_code', r.size_code, 'quantity', i.quantity, 'unit_price_minor', i.unit_price_minor,
                       'discount_minor', i.discount_minor, 'net_paid_minor', i.net_paid_minor) ORDER BY i.line_no)
                     FROM business.order_item i
                     JOIN business.variant v ON v.id = i.variant_id
                     JOIN business.cake c ON c.id = v.cake_id
                     JOIN business.recipe_version r ON r.id = i.recipe_version_id
                     WHERE i.order_id = o.id))
  INTO v_result
  FROM business.orders o
  WHERE o.id = p_order
    AND ((p_owner IS NOT NULL AND o.owner_account_id = (SELECT a.id FROM business.account a
                                                       WHERE a.provider_subject = p_owner AND a.disabled_at IS NULL))
         OR (p_guest_digest IS NOT NULL AND EXISTS (
               SELECT 1 FROM business.order_guest_capability g
               WHERE g.order_id = o.id AND g.capability_digest = p_guest_digest AND g.revoked_at IS NULL
                 AND 'read' = ANY (g.allowed_commands))));
  RETURN v_result;
END;
$$;


REVOKE ALL ON TABLE business.refund_goal FROM PUBLIC;
REVOKE ALL ON TABLE business.refund_operation FROM PUBLIC;
REVOKE ALL ON TABLE business.refund_attempt FROM PUBLIC;
REVOKE ALL ON TABLE business.refund_job FROM PUBLIC;
REVOKE ALL ON TABLE business.refund_observation FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_balance(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_goal_remaining(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_available(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_schedule_reconcile(uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_reserve(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_late_capture(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_leased(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_leased_dispatch(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_leased_reconcile(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_quarantined(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_settle(uuid, text, text, text, json, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_mismatch(uuid, text, json) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_job_begin(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_dispatch_fix(uuid, uuid, bigint, uuid, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_recovery_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_recovery_prepared(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_dispatch_admit(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_attempt_record(uuid, uuid, bigint, uuid, text, integer, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_recovery_committed(uuid, uuid, bigint, uuid, json, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_recovery_secured(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_recovery_reconcile(uuid, uuid, bigint, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_recovery_frozen(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_reconcile_operations(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_observation_record(uuid, uuid, bigint, uuid, text, bigint, text, text, text, uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_reconcile_record(uuid, uuid, bigint, uuid, text, text, text, json, text) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.refund_job_begin(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_dispatch_fix(uuid, uuid, bigint, uuid, text, timestamptz) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_recovery_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamptz) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_recovery_prepared(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_dispatch_admit(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_attempt_record(uuid, uuid, bigint, uuid, text, integer, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_recovery_committed(uuid, uuid, bigint, uuid, json, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_recovery_secured(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_recovery_reconcile(uuid, uuid, bigint, uuid, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_recovery_frozen(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_reconcile_operations(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_observation_record(uuid, uuid, bigint, uuid, text, bigint, text, text, text, uuid, timestamptz) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_reconcile_record(uuid, uuid, bigint, uuid, text, text, text, json, text) TO gk_worker;
