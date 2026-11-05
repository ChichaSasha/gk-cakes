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
-- (K05): 0 while an operation is unresolved or a quarantined observation is
-- unresolved, otherwise min(room, goal remaining).
CREATE FUNCTION business.refund_available(p_collection uuid)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN b.unresolved OR b.quarantined THEN 0
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
