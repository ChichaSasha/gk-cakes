-- GK-026 B1 part 2: production start, the late-payment resolution, K05
-- allocation across collections and the refund goal raised by a GK-caused
-- delivery failure (contracts r6.5 K05 "Scheduled preparation at
-- payment/free-confirmation+48h has scheduled effective_at and separate
-- observed/actual times", "Earlier chef start takes effect immediately",
-- "Allocate total by floor+largest remainder, stable cake then delivery
-- collection/component ID ties", "A distinct approved obligation (for
-- example GK-caused failed delivery) records its explicit collection floor",
-- "Only after a definitive prior outcome attempt the entire remaining
-- allocation once"; K04 Q2 paid_awaiting_agreement; K00 capabilities; K11).
--
-- Production start. One transition per order, keyed
-- 'production_start:<order>' (business.production_transition): the manual
-- start (chef or fulfillment, business.order_production_start: effective at
-- the receipt instant) and the timer (business.production_timer_run:
-- effective at the scheduled instant = paid_at or free_confirmed_at + 48 h,
-- observed when it ran) collapse on that key; whichever comes second
-- changes nothing. Only confirmed -> in_production; it never deducts
-- ingredients and never asserts the chef physically began. The timer job is
-- scheduled when an order becomes visibly confirmed (secured), due at the
-- scheduled instant; it is lease-fenced and reads the restore freeze first.
-- While a cancellation or late-payment decision on the order is unresolved
-- the timer waits (cancel-first keeps its version; production never changes
-- the entitlement either way: the rate depends only on the cancel receipt
-- instant). Not a K11 command: no customer commitment or money changes, and
-- the timer re-derives a lost transition from the immutable anchor.
--
-- Late payment (business.order_late_payment_request, chef). A K11 order
-- command 'late_payment' on a paid_awaiting_agreement order: 'honour'
-- needs the customer's agreement (channel, agreed_at not in the future,
-- evidence reference; silence never counts) and commits confirmed when the
-- order still has its original slot and its holds are committed (otherwise
-- refused holds_changed); 'full_return' cancels it (cancel_reason
-- late_payment_returned) with a 100% goal of the actual paid aggregate,
-- never 50%. The decision and agreement are stored immutably
-- (business.late_payment_decision); the feasibility is recorded by the chef
-- at the receipt instant.
--
-- Allocation (business.refund_plan). Over the order's succeeded
-- collections in stable order (cake, then delivery, then ID): each
-- collection's floor (a recorded obligation, capped at its paid amount)
-- first, then the rest of the target by floor + largest remainder over the
-- unfloored paid amounts; a new reservation for a collection is
-- min(its share - its S + R, its room, the aggregate remainder target - all
-- S - R), never while it is blocked (unresolved, quarantined, contradicted).
-- When prior actuals of a collection exceed its share and the aggregate
-- still needs money, nothing is reserved automatically (chef-confirmed
-- revised allocations: Not yet). business.refund_reserve and the
-- cancellation preview both use it.
--
-- Raised goal after a definitive outcome. When an unresolved refund
-- operation becomes succeeded or failed and the goal was raised after it was
-- reserved (goal version > the operation's), the remaining allocation is
-- reserved at once (trigger). A failure under an unchanged goal still waits
-- for a person (as in 0016).
--
-- Delivery failure floor (business.refund_delivery_failure_record, chef;
-- interim, no HTTP route). Records a 100% floor on a succeeded delivery
-- collection and raises the order goal to that floor plus everything
-- returned or reserved from the other collections (capped by paid), once.
-- Delivery collections (payment_collection kind 'delivery') exist only as
-- data: no delivery checkout creates them yet.
--
-- Expand-only (K09): new tables, replaced CHECK constraints and functions
-- with unchanged signatures. Lock order (K00): the order, its collections
-- by ID, the recovery operation, the goal, refund operations, the day.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------- schema

ALTER TABLE business.payment_collection DROP CONSTRAINT payment_collection_kind_check;
ALTER TABLE business.payment_collection
  ADD CONSTRAINT payment_collection_kind_check CHECK (kind IN ('cake', 'delivery'));

ALTER TABLE business.orders DROP CONSTRAINT orders_cancel_reason_check;
ALTER TABLE business.orders
  ADD CONSTRAINT orders_cancel_reason_check CHECK (cancel_reason IN (
    'payment_not_completed', 'free_not_confirmed', 'customer_cancelled', 'chef_cancelled', 'late_payment_returned'));

ALTER TABLE business.order_command DROP CONSTRAINT order_command_command_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_command_check CHECK (command IN ('free_confirm', 'paid_confirm', 'cancel', 'late_payment'));
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_late_payment CHECK (command <> 'late_payment' OR actor_kind = 'staff');

ALTER TABLE business.refund_goal DROP CONSTRAINT refund_goal_reason_check;
ALTER TABLE business.refund_goal
  ADD CONSTRAINT refund_goal_reason_check CHECK (reason IN (
    'late_capture_terminal', 'customer_cancelled', 'chef_cancelled', 'late_payment_returned', 'delivery_failure'));
ALTER TABLE business.refund_goal DROP CONSTRAINT refund_goal_policy;
ALTER TABLE business.refund_goal
  ADD CONSTRAINT refund_goal_policy CHECK ((policy_rate IS NULL) = (paid_basis_minor IS NULL)
                                          AND (policy_rate IS NULL) = (reason IN ('late_capture_terminal', 'delivery_failure')));

CREATE TABLE business.production_transition (
  transition_key   text PRIMARY KEY,
  order_id         uuid NOT NULL UNIQUE REFERENCES business.orders (id),
  source           text NOT NULL CHECK (source IN ('manual', 'timer')),
  -- paid_at or free_confirmed_at + 48 hours.
  scheduled_at     timestamptz NOT NULL,
  -- manual: the receipt instant; timer: the scheduled instant.
  effective_at     timestamptz NOT NULL,
  -- When the transition was recorded (database clock).
  observed_at      timestamptz NOT NULL DEFAULT now(),
  actor_account_id uuid NULL REFERENCES business.account (id),
  CONSTRAINT production_transition_key CHECK (transition_key = 'production_start:' || order_id::text),
  CONSTRAINT production_transition_actor CHECK ((source = 'manual') = (actor_account_id IS NOT NULL)),
  CONSTRAINT production_transition_timer CHECK (source <> 'timer' OR effective_at = scheduled_at)
);
CREATE TRIGGER production_transition_immutable BEFORE UPDATE OR DELETE ON business.production_transition
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER production_transition_no_truncate BEFORE TRUNCATE ON business.production_transition
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- The order's production timer job (job operation = the order; one per order).
CREATE TABLE business.production_timer_job (
  job_id       uuid PRIMARY KEY REFERENCES business.job (id),
  order_id     uuid NOT NULL UNIQUE REFERENCES business.orders (id),
  scheduled_at timestamptz NOT NULL
);

CREATE TABLE business.late_payment_decision (
  command_id       uuid PRIMARY KEY REFERENCES business.order_command (id),
  decision         text NOT NULL CHECK (decision IN ('honour', 'full_return')),
  channel          text NULL CHECK (channel IN ('email', 'phone', 'instagram', 'whatsapp', 'in_person', 'other')),
  agreed_at        timestamptz NULL,
  evidence_ref     text NULL CHECK (evidence_ref = btrim(evidence_ref) AND char_length(evidence_ref) BETWEEN 1 AND 200
                                    AND evidence_ref !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  -- The chef's feasibility check: who, at the receipt instant.
  recorded_by      uuid NOT NULL REFERENCES business.account (id),
  recorded_at      timestamptz NOT NULL,
  CONSTRAINT late_payment_agreement CHECK (
    (decision = 'honour') = (channel IS NOT NULL) AND (channel IS NULL) = (agreed_at IS NULL)
    AND (channel IS NULL) = (evidence_ref IS NULL) AND (agreed_at IS NULL OR agreed_at <= recorded_at))
);
CREATE TRIGGER late_payment_decision_immutable BEFORE UPDATE OR DELETE ON business.late_payment_decision
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER late_payment_decision_no_truncate BEFORE TRUNCATE ON business.late_payment_decision
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- A recorded obligation on one collection (K05 "explicit collection floor").
CREATE TABLE business.refund_floor (
  id            uuid PRIMARY KEY,
  order_id      uuid NOT NULL REFERENCES business.orders (id),
  collection_id uuid NOT NULL UNIQUE REFERENCES business.payment_collection (id),
  floor_minor   bigint NOT NULL CHECK (floor_minor > 0),
  reason        text NOT NULL CHECK (reason = 'gk_delivery_failure'),
  recorded_by   uuid NOT NULL REFERENCES business.account (id),
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER refund_floor_immutable BEFORE UPDATE OR DELETE ON business.refund_floor
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER refund_floor_no_truncate BEFORE TRUNCATE ON business.refund_floor
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- ----------------------------------------------------- K05 allocation

-- Owner-internal: each succeeded collection's K05 share of a goal of
-- p_target (floor first, then floor + largest remainder over the unfloored
-- paid amounts; stable cake, delivery, ID order) with its S + R, room and
-- whether it is blocked.
CREATE FUNCTION business.refund_shares(p_order uuid, p_target bigint)
RETURNS TABLE (collection_id uuid, ord integer, share_minor bigint, exposure_minor bigint, room_minor bigint, blocked boolean)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH c AS (
    SELECT pc.id, CASE pc.kind WHEN 'cake' THEN 0 ELSE 1 END AS ord, b.paid_minor AS paid,
           LEAST(coalesce(f.floor_minor, 0), b.paid_minor) AS fl, b.exposure_minor AS ex, b.room_minor AS room,
           (b.unresolved OR b.quarantined OR EXISTS (SELECT 1 FROM business.refund_operation ro
                                                    WHERE ro.collection_id = pc.id AND ro.mismatch_reason IS NOT NULL)) AS blocked
    FROM business.payment_collection pc
    CROSS JOIN LATERAL business.refund_balance(pc.id) b
    LEFT JOIN business.refund_floor f ON f.collection_id = pc.id
    WHERE pc.order_id = p_order AND pc.state = 'succeeded'
  ), t AS (
    SELECT GREATEST(0, LEAST(p_target, coalesce(sum(c.paid), 0)) - coalesce(sum(c.fl), 0))::bigint AS tp,
           coalesce(sum(c.paid - c.fl), 0)::bigint AS pw
    FROM c
  ), a AS (
    SELECT c.*, t.tp,
           CASE WHEN t.pw > 0 THEN (t.tp * (c.paid - c.fl)) / t.pw ELSE 0 END AS base,
           CASE WHEN t.pw > 0 THEN (t.tp * (c.paid - c.fl)) % t.pw ELSE 0 END AS rem
    FROM c, t
  ), r AS (
    SELECT a.*, row_number() OVER (ORDER BY a.rem DESC, a.ord, a.id) AS rn, a.tp - sum(a.base) OVER () AS leftover
    FROM a
  )
  SELECT r.id, r.ord, (r.fl + r.base + CASE WHEN r.rn <= r.leftover THEN 1 ELSE 0 END)::bigint, r.ex, r.room, r.blocked
  FROM r ORDER BY r.ord, r.id;
$$;

-- Owner-internal: what refund_reserve would reserve now per succeeded
-- collection for a goal of p_target (see the header), in stable order.
CREATE FUNCTION business.refund_plan(p_order uuid, p_target bigint)
RETURNS TABLE (collection_id uuid, amount_minor bigint)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_remaining bigint;
  v_take bigint;
  r record;
BEGIN
  SELECT GREATEST(0, p_target - coalesce(sum(b.succeeded_minor + b.reserved_minor), 0)) INTO v_remaining
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b
  WHERE pc.order_id = p_order;
  IF v_remaining <= 0 THEN
    RETURN;
  END IF;
  -- Skewed prior actuals while money is still owed: a chef decides.
  IF EXISTS (SELECT 1 FROM business.refund_shares(p_order, p_target) s WHERE s.exposure_minor > s.share_minor) THEN
    RETURN;
  END IF;
  FOR r IN SELECT * FROM business.refund_shares(p_order, p_target) s ORDER BY s.ord, s.collection_id LOOP
    CONTINUE WHEN r.blocked;
    v_take := LEAST(r.share_minor - r.exposure_minor, r.room_minor, v_remaining);
    CONTINUE WHEN v_take <= 0;
    collection_id := r.collection_id;
    amount_minor := v_take;
    RETURN NEXT;
    v_remaining := v_remaining - v_take;
  END LOOP;
END;
$$;

-- As in 0016, with the K05 allocation (business.refund_plan).
CREATE OR REPLACE FUNCTION business.refund_reserve(p_order uuid)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_g business.refund_goal%ROWTYPE;
  c business.payment_collection%ROWTYPE;
  p record;
  v_op uuid;
  v_job uuid;
  v_count integer := 0;
BEGIN
  -- Lock order (K00): the order, its collections by ID, then the goal (each
  -- a no-op when the caller already holds it).
  PERFORM 1 FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RETURN 0;
  END IF;
  FOR p IN SELECT * FROM business.refund_plan(p_order, v_g.target_minor) LOOP
    SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = p.collection_id;
    v_op := pg_catalog.gen_random_uuid();
    INSERT INTO business.refund_operation (id, order_id, collection_id, goal_id, goal_version, amount_minor, environment,
                                           provider_account_id, livemode, payment_intent_id)
    VALUES (v_op, p_order, c.id, v_g.id, v_g.version, p.amount_minor, c.environment, c.provider_account_id, c.livemode,
            c.payment_intent_id);
    v_job := business.job_enqueue(v_op, NULL);
    INSERT INTO business.refund_job (job_id, collection_id, refund_operation_id, purpose)
    VALUES (v_job, c.id, v_op, 'dispatch');
    PERFORM business.audit_record('system', NULL, 'refund.reserved', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('refund_operation_id', v_op, 'collection_id', c.id,
                                                               'amount_minor', p.amount_minor, 'goal_version', v_g.version));
    v_count := v_count + 1;
  END LOOP;
  RETURN v_count;
END;
$$;

-- As in 0021, with the K05 allocation.
CREATE OR REPLACE FUNCTION business.cancellation_allocations(p_order uuid, p_target bigint)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.json_agg(pg_catalog.json_build_object('collection_id', p.collection_id,
                                                                   'amount_minor', p.amount_minor)), '[]'::json)
  FROM business.refund_plan(p_order, p_target) p;
$$;

-- Trigger: a definitive outcome under a goal raised since the operation was
-- reserved reserves the remaining allocation once (see the header). The
-- settling functions hold the order and collection locks.
CREATE FUNCTION business.refund_operation_settled()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM business.refund_goal g WHERE g.id = NEW.goal_id AND g.version > NEW.goal_version) THEN
    PERFORM business.refund_reserve(NEW.order_id);
  END IF;
  RETURN NULL;
END;
$$;
CREATE TRIGGER refund_operation_settled AFTER UPDATE OF state ON business.refund_operation
  FOR EACH ROW
  WHEN (OLD.state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action')
        AND NEW.state IN ('succeeded', 'failed'))
  EXECUTE FUNCTION business.refund_operation_settled();

-- --------------------------------------------- terminal return (shared)

-- Owner-internal: cancel order p_order (locked by the caller with its
-- collections) as of p_at with p_reason and return p_rate% of the actual
-- paid aggregate: day place released, code use kept (K03), the goal
-- recorded or raised, the refund operations reserved; recovery_state
-- cancellation_pending until the command is secured. Returns the outcome.
CREATE FUNCTION business.order_terminal_return(p_order uuid, p_version integer, p_reason text, p_rate smallint,
                                               p_source text, p_actor_kind text, p_account uuid, p_operation uuid,
                                               p_at timestamptz)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_paid bigint := business.order_paid_minor(p_order);
  v_target bigint := business.cancellation_amount(v_paid, p_rate);
  v_g business.refund_goal%ROWTYPE;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = p_order AND a.day = v_o.handover_day) THEN
    PERFORM business.capacity_release(p_order, v_o.handover_day);
  END IF;
  UPDATE business.orders o
  SET lifecycle = 'cancelled', cancel_reason = p_reason, cancelled_at = p_at,
      recovery_state = 'cancellation_pending', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = p_order;
  IF v_target > 0 THEN
    SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
    IF NOT FOUND THEN
      INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source, policy_rate, paid_basis_minor)
      VALUES (pg_catalog.gen_random_uuid(), p_order, v_target, p_reason, p_source, p_rate, v_paid)
      RETURNING * INTO v_g;
      PERFORM business.audit_record('system', NULL, 'refund.goal_recorded', 'order', p_order, NULL,
                                    pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                                 'target_minor', v_target, 'reason', p_reason, 'rate', p_rate));
    ELSIF v_g.target_minor < v_target THEN
      UPDATE business.refund_goal g
      SET target_minor = v_target, version = g.version + 1, updated_at = pg_catalog.now()
      WHERE g.id = v_g.id RETURNING * INTO v_g;
      PERFORM business.audit_record('system', NULL, 'refund.goal_raised', 'order', p_order, NULL,
                                    pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                                 'target_minor', v_target, 'reason', p_reason));
    END IF;
    PERFORM business.refund_reserve(p_order);
  END IF;
  PERFORM business.audit_record(CASE p_actor_kind WHEN 'guest' THEN 'system' ELSE p_actor_kind END, p_account,
                                'order.cancelled', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', p_operation, 'reason', p_reason, 'rate', p_rate,
                                                             'refund_target_minor', v_target));
  RETURN pg_catalog.format(
    '{"status":"cancelled","evidence":{"order_version":%s,"reason":"%s","rate":%s,"paid_minor":%s,"refund_target_minor":%s}}',
    p_version + 1, p_reason, p_rate, v_paid, v_target);
END;
$$;

-- As in 0021, through the shared terminal return.
CREATE OR REPLACE FUNCTION business.order_cancel_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_chef boolean := v.actor_kind = 'staff';
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  IF v_o.version <> v.prior_version OR NOT business.cancellation_eligible(v_o.lifecycle)
     OR v_o.recovery_state <> 'secured' THEN
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.cancellation_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', 'order_changed'));
    RETURN '{"status":"refused","evidence":{"reason":"order_changed"}}';
  END IF;
  -- The persisted receipt instant decides the rate (never the commit time).
  RETURN business.order_terminal_return(
    v.order_id, v_o.version, CASE WHEN v_chef THEN 'chef_cancelled' ELSE 'customer_cancelled' END,
    business.cancellation_rate(v_o.full_refund_deadline_at, v.received_at, v_chef),
    CASE WHEN v_chef THEN 'chef' ELSE 'customer' END, v.actor_kind, v.actor_account_id, v.id, v.received_at);
END;
$$;

-- Owner-internal: the domain commit of a prepared late-payment decision
-- (the caller, business.order_command_apply, holds the locks and checked
-- the freeze). Returns the outcome.
CREATE FUNCTION business.order_late_payment_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_d business.late_payment_decision%ROWTYPE;
  v_snapshot json := (v.payload::json) -> 'order';
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  SELECT * INTO v_d FROM business.late_payment_decision d WHERE d.command_id = v.id;
  IF v_o.version <> v.prior_version OR v_o.lifecycle <> 'paid_awaiting_agreement' OR v_o.recovery_state <> 'secured' THEN
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.late_payment_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', 'order_changed'));
    RETURN '{"status":"refused","evidence":{"reason":"order_changed"}}';
  END IF;
  IF v_d.decision = 'full_return' THEN
    -- Never 50%: the whole actual paid aggregate.
    RETURN business.order_terminal_return(v.order_id, v_o.version, 'late_payment_returned', 100::smallint, 'chef',
                                          'staff', v.actor_account_id, v.id, v.received_at);
  END IF;
  -- Honour: the original slot is still the order's and its holds are the
  -- committed ones (no date move).
  IF v_o.slot_id::text IS DISTINCT FROM v_snapshot ->> 'slot_id'
     OR v_o.handover_day::text IS DISTINCT FROM v_snapshot ->> 'handover_day'
     OR EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = v.order_id AND a.state <> 'committed')
     OR EXISTS (SELECT 1 FROM business.order_lot_reservation r WHERE r.order_id = v.order_id AND r.state <> 'sold') THEN
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.late_payment_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', 'holds_changed'));
    RETURN '{"status":"refused","evidence":{"reason":"holds_changed"}}';
  END IF;
  UPDATE business.orders o
  SET lifecycle = 'confirmed', recovery_state = 'confirmation_pending', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = v.order_id;
  PERFORM business.audit_record('staff', v.actor_account_id, 'order.late_payment_honoured', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'channel', v_d.channel));
  RETURN pg_catalog.format('{"status":"confirmed","evidence":{"order_version":%s,"decision":"honour"}}', v_o.version + 1);
END;
$$;

-- As in 0021, plus the late-payment decision.
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
  -- Lock order (K00): the order, its collections by ID (all of them: a
  -- refused confirmation or a cancellation may record the return across
  -- them), then its recovery operation, then the goal and refund
  -- operations, then the day and lots inside the engines.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  IF v.command IN ('paid_confirm', 'cancel', 'late_payment') THEN
    PERFORM 1 FROM business.payment_collection c WHERE c.order_id = v.order_id ORDER BY c.id FOR UPDATE;
    SELECT * INTO v_c FROM business.payment_collection c WHERE c.id = v.collection_id;
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

  IF v.command IN ('cancel', 'late_payment') THEN
    v_outcome := CASE v.command WHEN 'cancel' THEN business.order_cancel_commit(v)
                                ELSE business.order_late_payment_commit(v) END;
    SELECT c.op_state INTO v_state
    FROM business.recovery_mark_committed(v.id, v_outcome::json,
           pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_outcome, 'UTF8')), 'hex'), false) c;
    SELECT * INTO v_r FROM business.recovery_operation r WHERE r.id = v.id;
    RETURN QUERY SELECT v_r.state, v_r.outcome, v_r.outcome_hash::text;
    RETURN;
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
      v_outcome := pg_catalog.format('{"status":"refused","evidence":{"reason":"%s","collection_id":"%s"}}',
                                     v_reason, v.collection_id);
      PERFORM business.audit_record('system', NULL, 'order.paid_confirmation_refused', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason,
                                                                 'collection_id', v.collection_id));
      IF v_o.lifecycle = 'cancelled' THEN
        PERFORM business.refund_late_capture(v.order_id);
      END IF;
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

-- As in 0021; a late-payment full return is secured like a cancellation
-- (an honour like a confirmation).
CREATE OR REPLACE FUNCTION business.order_command_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_state text;
  v_status text;
BEGIN
  PERFORM 1 FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  v_state := business.recovery_mark_secured(v.id);
  SELECT r.outcome ->> 'status' INTO v_status FROM business.recovery_operation r WHERE r.id = v.id;
  UPDATE business.orders o SET recovery_state = 'secured', updated_at = pg_catalog.now()
  WHERE o.id = v.order_id
    AND ((o.recovery_state = 'confirmation_pending' AND v.command <> 'cancel'
          AND v_status IN ('confirmed', 'paid_awaiting_agreement'))
         OR (o.recovery_state = 'cancellation_pending' AND v.command IN ('cancel', 'late_payment')
             AND v_status = 'cancelled'));
  RETURN v_state;
END;
$$;

-- ------------------------------------------------------- late payment

-- API: the chef's decision on a paid_awaiting_agreement order (see the
-- header). Returns the command (the recovery operation) and whether it
-- already existed.
CREATE FUNCTION business.order_late_payment_request(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_decision text,
  p_channel text, p_agreed_at timestamptz, p_evidence_ref text, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_o business.orders%ROWTYPE;
  v_prior business.order_command%ROWTYPE;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
BEGIN
  IF p_now IS NULL OR p_order IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_decision IS NULL OR p_decision NOT IN ('honour', 'full_return') THEN
    RAISE EXCEPTION 'decision_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_decision = 'honour' THEN
    -- Silence never counts: the customer's agreement, fully evidenced.
    IF p_channel IS NULL OR p_channel NOT IN ('email', 'phone', 'instagram', 'whatsapp', 'in_person', 'other')
       OR p_agreed_at IS NULL OR p_agreed_at > p_now
       OR p_evidence_ref IS NULL OR p_evidence_ref <> pg_catalog.btrim(p_evidence_ref)
       OR pg_catalog.char_length(p_evidence_ref) NOT BETWEEN 1 AND 200
       OR p_evidence_ref ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]' THEN
      RAISE EXCEPTION 'agreement_invalid' USING ERRCODE = 'GK422';
    END IF;
  ELSIF p_channel IS NOT NULL OR p_agreed_at IS NOT NULL OR p_evidence_ref IS NOT NULL THEN
    RAISE EXCEPTION 'agreement_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT * INTO v_prior FROM business.order_command c
  WHERE c.order_id = p_order AND c.command = 'late_payment' AND c.idempotency_key = p_key AND c.actor_account_id = v_account;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior.id, true;
    RETURN;
  END IF;
  IF EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
             WHERE c.order_id = p_order AND c.command IN ('late_payment', 'cancel') AND r.state <> 'secured') THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.lifecycle <> 'paid_awaiting_agreement' OR v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;

  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.late_payment', 'operation_id', v_id, 'order_id', p_order,
    'prior_version', v_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', 'staff', 'account_id', v_account),
    'decision', pg_catalog.json_build_object('decision', p_decision, 'channel', p_channel, 'agreed_at', p_agreed_at,
                                             'evidence_ref', p_evidence_ref),
    'order', business.order_recovery_snapshot(p_order))::text;
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_payload, 'UTF8')), 'hex');
  SELECT r.result INTO v_reserve
  FROM business.recovery_reserve(v_id, 'command', p_order, v_o.version, v_hash, v_key, NULL) r;
  IF v_reserve = 'busy' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  ELSIF v_reserve <> 'reserved' THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  v_job := business.job_enqueue(v_id, NULL);
  INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, idempotency_key, body_sha256,
                                      prior_version, received_at, payload, payload_sha256, envelope_key, job_id)
  VALUES (v_id, p_order, 'late_payment', 'staff', v_account, p_key, p_body_sha256, v_o.version, p_now, v_payload, v_hash,
          v_key, v_job);
  INSERT INTO business.late_payment_decision (command_id, decision, channel, agreed_at, evidence_ref, recorded_by, recorded_at)
  VALUES (v_id, p_decision, p_channel, p_agreed_at, p_evidence_ref, v_account, p_now);
  PERFORM business.audit_record('staff', v_account, 'order.late_payment_decided', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id, 'decision', p_decision));
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- ---------------------------------------------------- production start

-- Owner-internal: the scheduled production instant of an order (its
-- payment or free confirmation + 48 hours), NULL without one.
CREATE FUNCTION business.production_scheduled_at(p_o business.orders)
RETURNS timestamptz
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(p_o.paid_at, p_o.free_confirmed_at) + interval '48 hours';
$$;

-- Trigger: an order that became visibly confirmed gets its production timer
-- job (once), due at its scheduled instant.
CREATE FUNCTION business.production_schedule()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_at timestamptz := business.production_scheduled_at(NEW);
  v_job uuid;
BEGIN
  IF v_at IS NULL OR EXISTS (SELECT 1 FROM business.production_timer_job p WHERE p.order_id = NEW.id) THEN
    RETURN NULL;
  END IF;
  v_job := business.job_enqueue(NEW.id, v_at);
  INSERT INTO business.production_timer_job (job_id, order_id, scheduled_at) VALUES (v_job, NEW.id, v_at);
  RETURN NULL;
END;
$$;
CREATE TRIGGER orders_production_schedule AFTER INSERT OR UPDATE OF lifecycle, recovery_state ON business.orders
  FOR EACH ROW WHEN (NEW.lifecycle = 'confirmed' AND NEW.recovery_state = 'secured')
  EXECUTE FUNCTION business.production_schedule();

-- Confirmed orders before this migration get their timer now.
DO $$
DECLARE
  o business.orders%ROWTYPE;
  v_job uuid;
BEGIN
  FOR o IN SELECT * FROM business.orders WHERE lifecycle = 'confirmed' AND recovery_state = 'secured' ORDER BY id LOOP
    CONTINUE WHEN business.production_scheduled_at(o) IS NULL;
    v_job := business.job_enqueue(o.id, business.production_scheduled_at(o));
    INSERT INTO business.production_timer_job (job_id, order_id, scheduled_at)
    VALUES (v_job, o.id, business.production_scheduled_at(o));
  END LOOP;
END;
$$;

-- Owner-internal: whether a cancellation or late-payment decision on the
-- order is unresolved (production waits for it: cancel-first).
CREATE FUNCTION business.order_terminal_pending(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                 WHERE c.order_id = p_order AND c.command IN ('cancel', 'late_payment') AND r.state <> 'secured');
$$;

-- API: the chef's or fulfillment's production start of order p_order (see
-- the header). Returns the order and whether nothing changed (the same key
-- again, or production already started by the timer or another request).
CREATE FUNCTION business.order_production_start(p_actor uuid, p_order uuid, p_key text, p_body_sha256 text,
                                                p_expected_version integer, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef', 'fulfillment']);
  v_prior uuid;
  v_o business.orders%ROWTYPE;
  v_frozen boolean;
BEGIN
  IF p_now IS NULL OR p_order IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  v_prior := business.command_claim(v_account, 'order.start_production', p_order::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF EXISTS (SELECT 1 FROM business.production_transition t WHERE t.order_id = p_order) THEN
    -- Already started (timer or another request): the shared key collapses it.
    PERFORM business.command_finish(v_account, 'order.start_production', p_order::text, p_key, p_order);
    RETURN QUERY SELECT p_order, true;
    RETURN;
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.lifecycle <> 'confirmed' OR v_o.recovery_state <> 'secured' OR business.order_terminal_pending(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  INSERT INTO business.production_transition (transition_key, order_id, source, scheduled_at, effective_at, actor_account_id)
  VALUES ('production_start:' || p_order::text, p_order, 'manual', business.production_scheduled_at(v_o), p_now, v_account);
  UPDATE business.orders o SET lifecycle = 'in_production', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = p_order;
  PERFORM business.audit_record('staff', v_account, 'order.production_started', 'order', p_order, NULL,
                                pg_catalog.json_build_object('source', 'manual', 'effective_at', p_now));
  PERFORM business.command_finish(v_account, 'order.start_production', p_order::text, p_key, p_order);
  RETURN QUERY SELECT p_order, false;
END;
$$;

-- Owner-internal: the order whose production timer job the caller holds a
-- live lease on (GK001 without the lease; NULL for another kind of job).
CREATE FUNCTION business.production_timer_leased(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_operation uuid;
BEGIN
  SELECT j.operation_id INTO v_operation
  FROM business.job j
  WHERE j.id = p_job_id AND j.state = 'running' AND j.lease_owner = p_worker AND j.generation = p_generation
    AND j.lease_until > pg_catalog.now()
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'job % is not leased to this worker generation', p_job_id USING ERRCODE = 'GK001';
  END IF;
  RETURN (SELECT p.order_id FROM business.production_timer_job p WHERE p.job_id = p_job_id AND p.order_id = v_operation);
END;
$$;

-- Worker: run the leased production timer. status: 'not_production'
-- (another handler owns the job), 'frozen' (restore: read first, nothing
-- changes), 'waiting' (a cancellation or late-payment decision is
-- unresolved, or the confirmation is not secured: try again later),
-- 'started' (confirmed -> in_production, effective at the scheduled
-- instant) or 'done' (already started, or no longer confirmed).
CREATE FUNCTION business.production_timer_run(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS TABLE (status text, order_id uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_order uuid := business.production_timer_leased(p_job_id, p_worker, p_generation);
  v_frozen boolean;
  v_o business.orders%ROWTYPE;
  v_at timestamptz;
BEGIN
  IF v_order IS NULL THEN
    RETURN QUERY SELECT 'not_production'::text, NULL::uuid;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN QUERY SELECT 'frozen'::text, v_order;
    RETURN;
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v_order FOR UPDATE;
  IF EXISTS (SELECT 1 FROM business.production_transition t WHERE t.order_id = v_order) OR v_o.lifecycle <> 'confirmed' THEN
    RETURN QUERY SELECT 'done'::text, v_order;
    RETURN;
  END IF;
  IF v_o.recovery_state <> 'secured' OR business.order_terminal_pending(v_order) THEN
    RETURN QUERY SELECT 'waiting'::text, v_order;
    RETURN;
  END IF;
  v_at := business.production_scheduled_at(v_o);
  INSERT INTO business.production_transition (transition_key, order_id, source, scheduled_at, effective_at)
  VALUES ('production_start:' || v_order::text, v_order, 'timer', v_at, v_at);
  UPDATE business.orders o SET lifecycle = 'in_production', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = v_order;
  PERFORM business.audit_record('system', NULL, 'order.production_started', 'order', v_order, NULL,
                                pg_catalog.json_build_object('source', 'timer', 'effective_at', v_at));
  RETURN QUERY SELECT 'started'::text, v_order;
END;
$$;

-- ---------------------------------------------- delivery failure floor

-- API (chef; interim, no HTTP route yet): record a GK-caused failed
-- delivery on the order's succeeded delivery collection p_collection: a
-- 100% floor on it, and the goal raised to that floor plus everything
-- returned or reserved from the other collections (capped by paid); then
-- the remaining allocation is reserved. Returns the floor and whether it
-- already existed.
CREATE FUNCTION business.refund_delivery_failure_record(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                                        p_collection uuid)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  v_c business.payment_collection%ROWTYPE;
  v_g business.refund_goal%ROWTYPE;
  v_floor uuid;
  v_paid bigint;
  v_others bigint;
  v_target bigint;
BEGIN
  IF p_order IS NULL OR p_collection IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v_prior := business.command_claim(v_account, 'refund.delivery_failure', p_collection::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM 1 FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  SELECT * INTO v_c FROM business.payment_collection pc WHERE pc.id = p_collection AND pc.order_id = p_order;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'collection not found' USING ERRCODE = 'GK404';
  END IF;
  IF v_c.kind <> 'delivery' OR v_c.state <> 'succeeded' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  SELECT f.id INTO v_floor FROM business.refund_floor f WHERE f.collection_id = p_collection;
  IF FOUND THEN
    PERFORM business.command_finish(v_account, 'refund.delivery_failure', p_collection::text, p_key, v_floor);
    RETURN QUERY SELECT v_floor, true;
    RETURN;
  END IF;
  v_floor := pg_catalog.gen_random_uuid();
  INSERT INTO business.refund_floor (id, order_id, collection_id, floor_minor, reason, recorded_by)
  VALUES (v_floor, p_order, p_collection, v_c.amount_minor, 'gk_delivery_failure', v_account);
  v_paid := business.order_paid_minor(p_order);
  SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_others
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b
  WHERE pc.order_id = p_order AND pc.id <> p_collection;
  v_target := LEAST(v_paid, v_c.amount_minor + v_others);
  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source)
    VALUES (pg_catalog.gen_random_uuid(), p_order, v_target, 'delivery_failure', 'chef')
    RETURNING * INTO v_g;
    PERFORM business.audit_record('staff', v_account, 'refund.goal_recorded', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_target, 'reason', 'delivery_failure'));
  ELSIF v_g.target_minor < v_target THEN
    UPDATE business.refund_goal g SET target_minor = v_target, version = g.version + 1, updated_at = pg_catalog.now()
    WHERE g.id = v_g.id RETURNING * INTO v_g;
    PERFORM business.audit_record('staff', v_account, 'refund.goal_raised', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_target, 'reason', 'delivery_failure'));
  END IF;
  PERFORM business.audit_record('staff', v_account, 'refund.floor_recorded', 'order', p_order, NULL,
                                pg_catalog.json_build_object('floor_id', v_floor, 'collection_id', p_collection,
                                                             'floor_minor', v_c.amount_minor));
  PERFORM business.refund_reserve(p_order);
  PERFORM business.command_finish(v_account, 'refund.delivery_failure', p_collection::text, p_key, v_floor);
  RETURN QUERY SELECT v_floor, false;
END;
$$;

REVOKE ALL ON TABLE business.production_transition FROM PUBLIC;
REVOKE ALL ON TABLE business.production_timer_job FROM PUBLIC;
REVOKE ALL ON TABLE business.late_payment_decision FROM PUBLIC;
REVOKE ALL ON TABLE business.refund_floor FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_shares(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_plan(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_operation_settled() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_terminal_return(uuid, integer, text, smallint, text, text, uuid, uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_late_payment_commit(business.order_command) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_late_payment_request(uuid, uuid, text, text, integer, text, text, timestamptz, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.production_scheduled_at(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.production_schedule() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_terminal_pending(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_production_start(uuid, uuid, text, text, integer, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.production_timer_leased(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.production_timer_run(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_delivery_failure_record(uuid, text, text, uuid, uuid) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_late_payment_request(uuid, uuid, text, text, integer, text, text, timestamptz, text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_production_start(uuid, uuid, text, text, integer, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.refund_delivery_failure_record(uuid, text, text, uuid, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.production_timer_run(uuid, uuid, bigint) TO gk_worker;
