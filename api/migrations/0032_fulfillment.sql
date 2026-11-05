-- GK-031 B1 part 2: fulfilment transitions (contracts r6.5 K05 "Separate
-- lifecycle awaiting_payment→confirmed→in_production→ready→completed",
-- "Fulfillment separate awaiting_handover/out_for_delivery/completed/
-- exception. Completed/no-show history is retained; post-handover personal
-- chef refund request creates financial case, never rewinds fulfilled
-- history", "no-show/post-handover 50%"; K06 "GK Delivery responsibility
-- through customer handover; hours match pickup, chef quote and paid/waived
-- separate collection required before dispatch. Delivery failure is
-- actionable, not automatic completed", "Future courier uses a
-- reference/status mapping only"; K00 "fulfillment can prepare/ready/
-- dispatch/handover only", "chef manages ... cancellations/refunds"; K11
-- accepted order-change protection; K08 notices; owner decision
-- BO-GK-003.no_show ca5fb5c0; BO-GK-001.ready_cancellation 93a75483).
--
-- One route, POST /api/v1/admin/orders/{id}/transitions {action,
-- expected_version, reason}. The fulfilment state is a new nullable column
-- orders.fulfillment_state (NULL reads as awaiting_handover while the order
-- is confirmed, in production or ready; otherwise as nothing). Every
-- transition appends a business.fulfillment_event row (action, from and to
-- states, actor, receipt instant, optional reason, the K11 command when there
-- is one) and moves the order's version.
--
-- Plain audited transactions (no money, no change to the customer's
-- commitment; as the production start, 0022), chef or fulfilment, freeze
-- first (GK503), refused while any K11 command on the order is unresolved
-- (K11: conflicting commands wait), the same key replays
-- (business.command_receipt), and a transition that already happened by
-- another request collapses to the current state (ready, dispatch):
--   ready               in_production -> ready (fulfilment awaiting_handover).
--   dispatch            GK delivery only: ready and awaiting_handover or
--                       exception -> out_for_delivery, only when a delivery
--                       collection of the order succeeded (409
--                       delivery_not_paid). There is no delivery waiver in
--                       the data model and checkout creates no delivery
--                       collection yet, so today every GK delivery dispatch
--                       is refused until delivery payment exists (recorded in
--                       the README). Optional reason: a courier reference.
--   delivery_exception  ready and awaiting_handover, out_for_delivery or
--                       exception -> exception with a required reason
--                       (failed delivery, or a missed collection: the
--                       fulfilment team's observation the chef acts on). Not
--                       completion, no refund.
-- K11 order commands (202, protected before visible):
--   handover            chef or fulfilment: ready, and awaiting_handover or
--                       exception (collection, customer courier) or
--                       out_for_delivery (GK delivery) -> lifecycle
--                       completed, fulfilment completed, with the actor and
--                       the receipt instant. Terminal: it ends the customer's
--                       cancellation entitlement (completed is not
--                       cancellable; later money is the chef's financial
--                       case), so it changes the customer's commitment.
--   no_show             chef only (K00: the money part of a no-show is a
--                       refund decision; fulfilment records the observation
--                       as delivery_exception): ready, awaiting_handover or
--                       exception (collection, courier) / out_for_delivery or
--                       exception after a dispatch (GK delivery), at or
--                       after the current appointment start (409
--                       handover_not_due) -> lifecycle completed, fulfilment
--                       no_show, the K05 50% policy goal of the actual paid
--                       aggregate (business.refund_goal_apply, reason
--                       no_show) and its refund operations reserved
--                       (business.refund_reserve); the day place stays
--                       (the cake was made).
-- Until such a command is secured the order shows its previous lifecycle
-- and fulfilment state and no refund block, and no refund_returned email is
-- queued; securing a no-show queues the no_show notice (0030 machinery,
-- order-no-show.v1). Ready and handover send no email (decision: no noise;
-- the chef sends collection details separately, K06). Completed and no-show
-- are never rewound (orders_guard keeps completed terminal).
--
-- Expand-only (K09): a nullable column, a new table, replaced CHECK
-- constraints, new functions; replaced functions keep signatures and grants.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------- schema

ALTER TABLE business.orders
  ADD COLUMN fulfillment_state text NULL CHECK (fulfillment_state IN ('awaiting_handover', 'out_for_delivery', 'completed',
                                                                      'exception', 'no_show'));

ALTER TABLE business.order_command DROP CONSTRAINT order_command_command_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_command_check CHECK (command IN ('free_confirm', 'paid_confirm', 'cancel', 'late_payment',
                                                                'manual_payout', 'manual_payout_outcome', 'pickup_move',
                                                                'appointment_change', 'handover', 'no_show'));
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_handover CHECK (command NOT IN ('handover', 'no_show') OR actor_kind = 'staff');

ALTER TABLE business.order_notice DROP CONSTRAINT order_notice_kind_check;
ALTER TABLE business.order_notice
  ADD CONSTRAINT order_notice_kind_check CHECK (kind IN ('confirmed', 'paid_awaiting_agreement', 'released', 'cancelled',
                                                         'refund_returned', 'pickup_moved', 'appointment_changed',
                                                         'no_show'));

ALTER TABLE business.refund_goal DROP CONSTRAINT refund_goal_reason_check;
ALTER TABLE business.refund_goal
  ADD CONSTRAINT refund_goal_reason_check CHECK (reason IN (
    'late_capture_terminal', 'customer_cancelled', 'chef_cancelled', 'late_payment_returned', 'delivery_failure',
    'chef_exception', 'no_show'));

-- The fulfilment history: one row per transition, never changed.
CREATE TABLE business.fulfillment_event (
  id               uuid PRIMARY KEY,
  order_id         uuid NOT NULL REFERENCES business.orders (id),
  action           text NOT NULL CHECK (action IN ('ready', 'dispatch', 'delivery_exception', 'handover', 'no_show')),
  from_state       text NOT NULL,
  to_state         text NOT NULL CHECK (to_state IN ('awaiting_handover', 'out_for_delivery', 'completed', 'exception',
                                                     'no_show')),
  actor_account_id uuid NOT NULL REFERENCES business.account (id),
  -- The persisted receipt instant of the request (K00 time rules).
  received_at      timestamptz NOT NULL,
  -- A courier reference (dispatch) or what went wrong (delivery_exception).
  reason           text NULL CHECK (reason = btrim(reason) AND char_length(reason) BETWEEN 1 AND 500
                                    AND reason !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  -- The K11 command of a handover or no-show.
  command_id       uuid NULL UNIQUE REFERENCES business.order_command (id),
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fulfillment_event_command CHECK ((action IN ('handover', 'no_show')) = (command_id IS NOT NULL)),
  CONSTRAINT fulfillment_event_reason CHECK ((action = 'delivery_exception') <= (reason IS NOT NULL)
                                             AND (action IN ('dispatch', 'delivery_exception') OR reason IS NULL))
);
CREATE INDEX fulfillment_event_order_idx ON business.fulfillment_event (order_id, created_at);
CREATE TRIGGER fulfillment_event_immutable BEFORE UPDATE OR DELETE ON business.fulfillment_event
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER fulfillment_event_no_truncate BEFORE TRUNCATE ON business.fulfillment_event
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- ------------------------------------------------------------ helpers

-- Owner-internal: the order's fulfilment state as stored (NULL reads as
-- awaiting_handover while confirmed, in production or ready).
CREATE FUNCTION business.order_fulfillment_state(p_order business.orders)
RETURNS text
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(p_order.fulfillment_state,
                  CASE WHEN p_order.lifecycle IN ('confirmed', 'in_production', 'ready') THEN 'awaiting_handover' END)
$$;

-- Owner-internal: whether a handover or no-show command of the order is not
-- secured yet (K11: not visible, no email).
CREATE FUNCTION business.order_handover_pending(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                 WHERE c.order_id = p_order AND c.command IN ('handover', 'no_show') AND r.state <> 'secured');
$$;

-- Owner-internal: the fulfilment block customers and staff see: {state,
-- at (the latest visible transition's receipt), pending (a handover or
-- no-show being protected: processing)}. A committed but unsecured command
-- shows the state it replaces.
CREATE FUNCTION business.order_visible_fulfillment(p_order business.orders)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'state', coalesce(u.from_state, business.order_fulfillment_state(p_order)),
           'at', (SELECT e.received_at FROM business.fulfillment_event e
                  LEFT JOIN business.recovery_operation r ON r.id = e.command_id
                  WHERE e.order_id = p_order.id AND (e.command_id IS NULL OR r.state = 'secured')
                  ORDER BY e.created_at DESC, e.id DESC LIMIT 1),
           'pending', CASE WHEN business.order_handover_pending(p_order.id) THEN
                        (SELECT c.command FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                         WHERE c.order_id = p_order.id AND c.command IN ('handover', 'no_show') AND r.state <> 'secured'
                         ORDER BY c.created_at DESC LIMIT 1) END)
  FROM (SELECT 1) one
  LEFT JOIN LATERAL (SELECT e.from_state FROM business.fulfillment_event e
                     JOIN business.recovery_operation r ON r.id = e.command_id
                     WHERE e.order_id = p_order.id AND r.state = 'committed'
                     ORDER BY e.created_at DESC LIMIT 1) u ON true;
$$;

-- Owner-internal: the order's fulfilment history for staff, oldest first;
-- reasons (courier references, what went wrong) for chef and fulfilment
-- only (technical support: K09 necessary support).
CREATE FUNCTION business.order_staff_fulfillment(p_order uuid, p_reasons boolean)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
           'action', e.action, 'from', e.from_state, 'to', e.to_state, 'received_at', e.received_at,
           'command_id', e.command_id,
           'state', CASE WHEN e.command_id IS NULL OR r.state = 'secured' THEN 'done' ELSE 'processing' END)
           || CASE WHEN p_reasons THEN pg_catalog.jsonb_build_object('reason', e.reason) ELSE '{}'::jsonb END
           ORDER BY e.created_at, e.id), '[]'::jsonb)
  FROM business.fulfillment_event e
  LEFT JOIN business.recovery_operation r ON r.id = e.command_id
  WHERE e.order_id = p_order;
$$;

-- Owner-internal: why a fulfilment action is not possible on the order now
-- (NULL when it is): transition_not_allowed, delivery_not_paid or
-- handover_not_due. p_at is the receipt instant.
CREATE FUNCTION business.fulfillment_refusal(p_o business.orders, p_action text, p_at timestamptz)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE
    WHEN p_action = 'ready' THEN
      CASE WHEN p_o.lifecycle = 'in_production' THEN NULL ELSE 'transition_not_allowed' END
    WHEN p_o.lifecycle <> 'ready' THEN 'transition_not_allowed'
    WHEN p_action = 'dispatch' THEN
      CASE WHEN p_o.fulfillment_mode <> 'gk_delivery'
                OR business.order_fulfillment_state(p_o) NOT IN ('awaiting_handover', 'exception') THEN 'transition_not_allowed'
           WHEN NOT EXISTS (SELECT 1 FROM business.payment_collection pc
                            WHERE pc.order_id = p_o.id AND pc.kind = 'delivery' AND pc.state = 'succeeded')
             THEN 'delivery_not_paid' END
    WHEN p_action = 'delivery_exception' THEN
      CASE WHEN business.order_fulfillment_state(p_o) IN ('awaiting_handover', 'out_for_delivery', 'exception') THEN NULL
           ELSE 'transition_not_allowed' END
    WHEN p_action = 'handover' THEN
      CASE WHEN (p_o.fulfillment_mode = 'gk_delivery' AND business.order_fulfillment_state(p_o) = 'out_for_delivery')
                OR (p_o.fulfillment_mode <> 'gk_delivery'
                    AND business.order_fulfillment_state(p_o) IN ('awaiting_handover', 'exception')) THEN NULL
           ELSE 'transition_not_allowed' END
    WHEN p_action = 'no_show' THEN
      -- GK delivery: only once it was sent out (out for delivery, or an
      -- exception after a dispatch); before that a failed handover is not
      -- the customer's absence.
      CASE WHEN (p_o.fulfillment_mode <> 'gk_delivery'
                 AND business.order_fulfillment_state(p_o) NOT IN ('awaiting_handover', 'exception'))
                OR (p_o.fulfillment_mode = 'gk_delivery'
                    AND NOT (business.order_fulfillment_state(p_o) = 'out_for_delivery'
                             OR (business.order_fulfillment_state(p_o) = 'exception'
                                 AND EXISTS (SELECT 1 FROM business.fulfillment_event e
                                             WHERE e.order_id = p_o.id AND e.action = 'dispatch'))))
             THEN 'transition_not_allowed'
           WHEN p_at < business.order_handover_at(p_o) THEN 'handover_not_due' END
    ELSE 'transition_not_allowed' END
$$;

CREATE FUNCTION business.fulfillment_target(p_action text)
RETURNS text
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE p_action WHEN 'ready' THEN 'awaiting_handover' WHEN 'dispatch' THEN 'out_for_delivery'
                       WHEN 'delivery_exception' THEN 'exception' WHEN 'handover' THEN 'completed'
                       WHEN 'no_show' THEN 'no_show' END
$$;

-- Owner-internal: the common checks of a fulfilment request.
CREATE FUNCTION business.fulfillment_check_input(p_key text, p_body_sha256 text, p_expected_version integer, p_action text,
                                                 p_reason text, p_now timestamptz)
RETURNS void
LANGUAGE plpgsql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_action IS NULL OR p_action NOT IN ('ready', 'dispatch', 'delivery_exception', 'handover', 'no_show') THEN
    RAISE EXCEPTION 'action_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF (p_action = 'delivery_exception' AND p_reason IS NULL)
     OR (p_action NOT IN ('dispatch', 'delivery_exception') AND p_reason IS NOT NULL)
     OR (p_reason IS NOT NULL AND (p_reason <> pg_catalog.btrim(p_reason) OR pg_catalog.char_length(p_reason) NOT BETWEEN 1 AND 500
                                   OR p_reason ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]')) THEN
    RAISE EXCEPTION 'reason_invalid' USING ERRCODE = 'GK422';
  END IF;
END;
$$;

-- ---------------------------------------------------------------- API

-- API: a plain fulfilment transition (ready, dispatch, delivery_exception;
-- see the header). Returns the order, whether nothing changed (the same key
-- again, or the transition already happened), and the order's current
-- lifecycle and fulfilment state.
CREATE FUNCTION business.order_fulfillment_transition(p_actor uuid, p_order uuid, p_key text, p_body_sha256 text,
                                                      p_expected_version integer, p_action text, p_reason text,
                                                      p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean, lifecycle text, fulfillment_state text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef', 'fulfillment']);
  v_command text;
  v_prior uuid;
  v_frozen boolean;
  v_o business.orders%ROWTYPE;
  v_refusal text;
  v_to text;
BEGIN
  PERFORM business.fulfillment_check_input(p_key, p_body_sha256, p_expected_version, p_action, p_reason, p_now);
  IF p_action NOT IN ('ready', 'dispatch', 'delivery_exception') OR p_order IS NULL THEN
    RAISE EXCEPTION 'action_invalid' USING ERRCODE = 'GK422';
  END IF;
  v_command := 'order.fulfillment_' || p_action;
  -- The same key replays (also during a freeze): it changes nothing.
  v_prior := business.command_claim(v_account, v_command, p_order::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    SELECT * INTO v_o FROM business.orders o WHERE o.id = v_prior;
    RETURN QUERY SELECT v_prior, true, business.order_visible_lifecycle(v_o),
                        business.order_visible_fulfillment(v_o) ->> 'state';
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND OR NOT business.order_fulfillment_scope(p_order) THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v_to := business.fulfillment_target(p_action);
  -- Already done by another request (ready, dispatch): collapse.
  IF p_action <> 'delivery_exception' AND business.order_fulfillment_state(v_o) = v_to
     AND (p_action <> 'ready' OR v_o.lifecycle = 'ready') THEN
    PERFORM business.command_finish(v_account, v_command, p_order::text, p_key, p_order);
    RETURN QUERY SELECT p_order, true, business.order_visible_lifecycle(v_o), business.order_visible_fulfillment(v_o) ->> 'state';
    RETURN;
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured'
     OR EXISTS (SELECT 1 FROM business.recovery_operation r
                WHERE r.aggregate_id = p_order AND r.kind = 'command' AND r.state <> 'secured') THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  v_refusal := business.fulfillment_refusal(v_o, p_action, p_now);
  IF v_refusal IS NOT NULL THEN
    RAISE EXCEPTION '%', v_refusal USING ERRCODE = 'GK409';
  END IF;
  INSERT INTO business.fulfillment_event (id, order_id, action, from_state, to_state, actor_account_id, received_at, reason)
  VALUES (pg_catalog.gen_random_uuid(), p_order, p_action, business.order_fulfillment_state(v_o), v_to, v_account, p_now,
          p_reason);
  UPDATE business.orders o
  SET lifecycle = CASE WHEN p_action = 'ready' THEN 'ready' ELSE o.lifecycle END, fulfillment_state = v_to,
      version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = p_order
  RETURNING * INTO v_o;
  PERFORM business.audit_record('staff', v_account, 'order.fulfillment_' || p_action, 'order', p_order, NULL,
                                pg_catalog.json_build_object('effective_at', p_now));
  PERFORM business.command_finish(v_account, v_command, p_order::text, p_key, p_order);
  RETURN QUERY SELECT p_order, false, v_o.lifecycle, v_o.fulfillment_state;
END;
$$;

-- API: a handover (chef or fulfilment) or no-show (chef) as a K11 order
-- command (see the header). Returns the command and whether it existed.
CREATE FUNCTION business.order_handover_request(p_actor uuid, p_order uuid, p_key text, p_body_sha256 text,
                                                p_expected_version integer, p_action text, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
  v_o business.orders%ROWTYPE;
  v_prior business.order_command%ROWTYPE;
  v_frozen boolean;
  v_refusal text;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
BEGIN
  PERFORM business.fulfillment_check_input(p_key, p_body_sha256, p_expected_version, p_action, NULL, p_now);
  IF p_action NOT IN ('handover', 'no_show') OR p_order IS NULL THEN
    RAISE EXCEPTION 'action_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  -- K00: a no-show's refund is the chef's decision.
  v_account := business.catalog_staff_account(p_actor, CASE WHEN p_action = 'no_show' THEN ARRAY['chef']
                                                            ELSE ARRAY['chef', 'fulfillment'] END);
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND OR NOT business.order_fulfillment_scope(p_order) THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT * INTO v_prior FROM business.order_command c
  WHERE c.order_id = p_order AND c.command = p_action AND c.idempotency_key = p_key AND c.actor_account_id = v_account;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior.id, true;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  v_refusal := business.fulfillment_refusal(v_o, p_action, p_now);
  IF v_refusal IS NOT NULL THEN
    RAISE EXCEPTION '%', v_refusal USING ERRCODE = 'GK409';
  END IF;
  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.' || p_action, 'operation_id', v_id, 'order_id', p_order,
    'prior_version', v_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', 'staff', 'account_id', v_account),
    'fulfillment', pg_catalog.json_build_object('action', p_action, 'from', business.order_fulfillment_state(v_o),
                                                'to', business.fulfillment_target(p_action),
                                                'rate', CASE WHEN p_action = 'no_show' THEN 50 END),
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
  VALUES (v_id, p_order, p_action, 'staff', v_account, p_key, p_body_sha256, v_o.version, p_now, v_payload, v_hash, v_key,
          v_job);
  PERFORM business.audit_record('staff', v_account, 'order.' || p_action || '_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id));
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- ------------------------------------------------------------- commit

-- Owner-internal: the domain commit of a prepared handover or no-show (the
-- caller, business.order_command_apply, holds the order, its collections and
-- the recovery operation and has checked the freeze). Returns the outcome.
CREATE FUNCTION business.order_handover_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_reason text;
  v_to text := business.fulfillment_target(v.command);
  v_paid bigint;
  v_target bigint;
  v_before bigint;
  v_after bigint;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id;
  IF v_o.version <> v.prior_version OR v_o.recovery_state <> 'secured' THEN
    v_reason := 'order_changed';
  ELSE
    v_reason := business.fulfillment_refusal(v_o, v.command, v.received_at);
  END IF;
  IF v_reason IS NOT NULL THEN
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.' || v.command || '_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason));
    RETURN pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', v_reason);
  END IF;
  INSERT INTO business.fulfillment_event (id, order_id, action, from_state, to_state, actor_account_id, received_at, command_id)
  VALUES (pg_catalog.gen_random_uuid(), v.order_id, v.command, business.order_fulfillment_state(v_o), v_to,
          v.actor_account_id, v.received_at, v.id);
  UPDATE business.orders o
  SET lifecycle = 'completed', fulfillment_state = v_to, version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = v.order_id;
  IF v.command = 'handover' THEN
    PERFORM business.audit_record('staff', v.actor_account_id, 'order.handed_over', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'effective_at', v.received_at));
    RETURN pg_catalog.format('{"status":"completed","evidence":{"order_version":%s}}', v_o.version + 1);
  END IF;
  -- No-show (K05): 50% of the actual paid aggregate, a policy goal (the
  -- cumulative order obligation; earlier returns count) and its refunds.
  -- What the customer was already returned or has on its way (S + R over
  -- every collection) before and after: the email states only what this
  -- no-show newly reserved.
  v_paid := business.order_paid_minor(v.order_id);
  v_target := business.cancellation_amount(v_paid, 50::smallint);
  SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_before
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b WHERE pc.order_id = v.order_id;
  IF v_target > 0 THEN
    PERFORM business.refund_goal_apply(v.order_id, 'no_show', 'chef', 50::smallint, v_paid, v_target, 'staff',
                                       v.actor_account_id);
    PERFORM business.refund_reserve(v.order_id);
  END IF;
  SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_after
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b WHERE pc.order_id = v.order_id;
  PERFORM business.audit_record('staff', v.actor_account_id, 'order.no_show', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'rate', 50, 'refund_target_minor', v_target,
                                                             'new_refund_minor', v_after - v_before));
  RETURN pg_catalog.format(
    '{"status":"no_show","evidence":{"order_version":%s,"rate":50,"paid_minor":%s,"refund_target_minor":%s,'
    '"returned_before_minor":%s,"new_refund_minor":%s}}',
    v_o.version + 1, v_paid, v_target, v_before, v_after - v_before);
END;
$$;

-- As in 0031, and no appointment change once the order is out for delivery
-- or handed over.
CREATE OR REPLACE FUNCTION business.appointment_change_eligible(p_order business.orders)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_order.lifecycle IN ('confirmed', 'in_production', 'ready')
         AND coalesce(p_order.fulfillment_state, 'awaiting_handover') IN ('awaiting_handover', 'exception')
$$;

-- ------------------------------------------------- replaced functions

-- As in 0029, and a completed order whose handover or no-show is not
-- secured yet still reads ready (K11).
CREATE OR REPLACE FUNCTION business.order_visible_lifecycle(p_order business.orders)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN p_order.recovery_state = 'confirmation_pending'
                OR (p_order.lifecycle = 'awaiting_payment'
                    AND EXISTS (SELECT 1 FROM business.order_command pcmd
                                JOIN business.recovery_operation pr ON pr.id = pcmd.id
                                WHERE pcmd.order_id = p_order.id AND pcmd.command = 'paid_confirm'
                                  AND pr.state <> 'secured'))
              THEN 'confirmation_pending'
              WHEN p_order.recovery_state = 'cancellation_pending'
                OR EXISTS (SELECT 1 FROM business.order_command ccmd
                           JOIN business.recovery_operation cr ON cr.id = ccmd.id
                           LEFT JOIN business.late_payment_decision cd ON cd.command_id = ccmd.id
                           WHERE ccmd.order_id = p_order.id AND cr.state <> 'secured'
                             AND (ccmd.command = 'cancel' OR cd.decision = 'full_return'))
              THEN 'cancellation_pending'
              WHEN p_order.lifecycle = 'completed' AND business.order_handover_pending(p_order.id) THEN 'ready'
              ELSE p_order.lifecycle END;
$$;

-- As in 0031, with the fulfilment block and no refund block while a no-show
-- is not secured.
CREATE OR REPLACE FUNCTION business.order_view(p_order uuid)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.json_build_object(
           'id', o.id, 'version', o.version,
           'lifecycle', business.order_visible_lifecycle(o),
           'cancel_reason', CASE WHEN o.recovery_state <> 'cancellation_pending' THEN o.cancel_reason END,
           'cancelled_at', CASE WHEN o.recovery_state <> 'cancellation_pending' THEN o.cancelled_at END,
           'payment_requirement', o.payment_requirement,
           'placed_at', o.placed_at, 'date', (va.a ->> 'date')::date, 'fulfillment_mode', o.fulfillment_mode,
           'slot', va.a - 'date',
           'fulfillment', business.order_visible_fulfillment(o),
           'pickup_move_until', CASE WHEN o.fulfillment_mode IN ('collection', 'customer_courier')
                                     THEN (va.a ->> 'starts_at')::timestamptz - business.pickup_move_cutoff() END,
           'appointment_change', (SELECT pg_catalog.json_build_object(
                                    'kind', ch.kind,
                                    'state', CASE WHEN r.state <> 'secured' THEN 'processing' ELSE r.outcome ->> 'status' END,
                                    'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                                    'requested_by', CASE ch.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
                                    'requested_at', ch.received_at,
                                    'to', pg_catalog.json_build_object('date', ch.to_day, 'id', ch.to_slot_id,
                                                                       'starts_at', ch.to_starts_at, 'ends_at', ch.to_ends_at))
                                  FROM business.pickup_change ch
                                  JOIN business.recovery_operation r ON r.id = ch.command_id
                                  WHERE ch.order_id = o.id
                                  ORDER BY ch.created_at DESC, ch.command_id DESC LIMIT 1),
           'full_refund_deadline_at', o.full_refund_deadline_at, 'policy_version_id', o.policy_version_id,
           'customer_full_name', o.customer_full_name, 'different_payer_name', o.different_payer_name,
           'contact_email', o.contact_email, 'subtotal_minor', o.subtotal_minor, 'discount_minor', o.discount_minor,
           'total_minor', o.total_minor, 'currency', o.currency,
           'paid_at', CASE WHEN o.recovery_state IN ('secured', 'cancellation_pending') THEN o.paid_at END,
           'free_confirmed_at', CASE WHEN o.recovery_state IN ('secured', 'cancellation_pending') THEN o.free_confirmed_at END,
           'free_confirmation', (SELECT pg_catalog.json_build_object(
                                   'state', CASE WHEN r.state <> 'secured' THEN 'processing'
                                                 ELSE r.outcome ->> 'status' END,
                                   'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                                   'requested_at', c.received_at)
                                 FROM business.order_command c
                                 JOIN business.recovery_operation r ON r.id = c.id
                                 WHERE c.order_id = o.id AND c.command = 'free_confirm'
                                 ORDER BY c.created_at DESC, c.id DESC LIMIT 1),
           'cancellation', (SELECT pg_catalog.json_build_object(
                              'state', CASE WHEN r.state <> 'secured' THEN 'processing'
                                            ELSE r.outcome ->> 'status' END,
                              'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                              'rate', CASE WHEN r.state = 'secured' THEN (r.outcome -> 'evidence' ->> 'rate')::integer END,
                              'amount_minor', CASE WHEN r.state = 'secured'
                                                   THEN (r.outcome -> 'evidence' ->> 'refund_target_minor')::bigint END,
                              'requested_by', CASE c.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
                              'requested_at', c.received_at)
                            FROM business.order_command c
                            JOIN business.recovery_operation r ON r.id = c.id
                            LEFT JOIN business.late_payment_decision d ON d.command_id = c.id
                            WHERE c.order_id = o.id AND (c.command = 'cancel' OR d.decision = 'full_return')
                            ORDER BY c.created_at DESC, c.id DESC LIMIT 1),
           'payment', (SELECT pg_catalog.json_build_object(
                         'id', pc.id,
                         'state', CASE WHEN pc.state = 'cancelled' AND pc.mismatch_reason IS NOT NULL THEN 'unknown'
                                       ELSE pc.state END,
                         'amount_minor', pc.amount_minor, 'currency', pc.currency,
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
           'refund', CASE WHEN o.recovery_state <> 'cancellation_pending' AND NOT business.order_handover_pending(o.id)
                          THEN business.order_refund_summary(o.id) END,
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
  FROM business.orders o
  CROSS JOIN LATERAL (SELECT business.order_visible_appointment(o) AS a) va
  WHERE o.id = p_order;
$$;

-- As in 0031, with the visible fulfilment state.
CREATE OR REPLACE FUNCTION business.order_staff_row(p_order business.orders, p_view text)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'id', p_order.id, 'version', p_order.version,
           'lifecycle', business.order_visible_lifecycle(p_order), 'raw_lifecycle', p_order.lifecycle,
           'date', (business.order_visible_appointment(p_order) ->> 'date')::date,
           'fulfillment_mode', p_order.fulfillment_mode,
           'slot', business.order_visible_appointment(p_order) - 'date',
           'placed_at', p_order.placed_at,
           'items', business.order_staff_items(p_order.id),
           'customer_full_name', p_order.customer_full_name,
           'cancel_reason', CASE WHEN p_order.recovery_state <> 'cancellation_pending' THEN p_order.cancel_reason END,
           'production', business.order_production_view(p_order.id),
           'fulfillment_state', business.order_visible_fulfillment(p_order) ->> 'state')
         || CASE WHEN p_view IN ('chef', 'technical_support') THEN
              pg_catalog.jsonb_build_object(
                'attention', pg_catalog.to_jsonb(business.order_attention_flags(p_order)),
                'needs_attention', pg_catalog.cardinality(business.order_attention_flags(p_order)) > 0)
            ELSE '{}'::jsonb END
         || CASE WHEN p_view = 'chef' THEN
              pg_catalog.jsonb_build_object(
                'contact_email', p_order.contact_email,
                'payment_requirement', p_order.payment_requirement,
                'total_minor', p_order.total_minor, 'currency', p_order.currency,
                'payment_state', (SELECT CASE WHEN pc.state = 'cancelled' AND pc.mismatch_reason IS NOT NULL THEN 'unknown'
                                              ELSE pc.state END
                                  FROM business.payment_collection pc WHERE pc.order_id = p_order.id
                                  ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1),
                'paid_at', CASE WHEN p_order.recovery_state IN ('secured', 'cancellation_pending') THEN p_order.paid_at END,
                'free_confirmed_at', CASE WHEN p_order.recovery_state IN ('secured', 'cancellation_pending')
                                          THEN p_order.free_confirmed_at END,
                'refund', CASE WHEN p_order.recovery_state <> 'cancellation_pending'
                                    AND NOT business.order_handover_pending(p_order.id)
                               THEN business.order_refund_summary(p_order.id)::jsonb END)
            ELSE '{}'::jsonb END;
$$;

-- As in 0031, plus the fulfilment block (every view) and history (reasons
-- for chef and fulfilment).
CREATE OR REPLACE FUNCTION business.order_staff_get(p_actor uuid, p_order uuid)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support', 'fulfillment']);
  v_view text := business.order_staff_view_of(v_account);
  o business.orders%ROWTYPE;
  v jsonb;
  v_flags text[];
  v_result jsonb;
BEGIN
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT * INTO o FROM business.orders x WHERE x.id = p_order;
  IF NOT FOUND OR (v_view = 'fulfillment' AND NOT business.order_fulfillment_scope(o.id)) THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v := business.order_view(o.id)::jsonb;
  v_flags := business.order_attention_flags(o);
  IF v_view = 'chef' THEN
    v_result := (v #- '{payment,checkout_url}')
                || pg_catalog.jsonb_build_object(
                     'raw_lifecycle', o.lifecycle, 'recovery_state', o.recovery_state,
                     'attention', pg_catalog.to_jsonb(v_flags), 'needs_attention', pg_catalog.cardinality(v_flags) > 0,
                     'production', business.order_production_view(o.id),
                     'collections', business.order_staff_collections(o.id, true),
                     'refund_goal', business.order_staff_refund_goal(o.id, true),
                     'refund_operations', business.order_staff_refund_operations(o.id, true),
                     'refund_observations', business.order_staff_refund_observations(o.id, true),
                     'commands', business.order_staff_commands(o.id),
                     'appointment_history', business.order_staff_appointments(o.id, true),
                     'fulfillment_history', business.order_staff_fulfillment(o.id, true),
                     'audit', business.order_staff_audit(o.id));
  ELSIF v_view = 'technical_support' THEN
    v_result := pg_catalog.jsonb_build_object(
                  'id', v -> 'id', 'version', v -> 'version', 'lifecycle', v -> 'lifecycle',
                  'raw_lifecycle', o.lifecycle, 'recovery_state', o.recovery_state,
                  'cancel_reason', v -> 'cancel_reason', 'cancelled_at', v -> 'cancelled_at',
                  'payment_requirement', v -> 'payment_requirement', 'placed_at', v -> 'placed_at',
                  'date', v -> 'date', 'fulfillment_mode', v -> 'fulfillment_mode', 'slot', v -> 'slot',
                  'full_refund_deadline_at', v -> 'full_refund_deadline_at', 'policy_version_id', v -> 'policy_version_id',
                  'customer_full_name', v -> 'customer_full_name',
                  'paid_at', v -> 'paid_at', 'free_confirmed_at', v -> 'free_confirmed_at',
                  'free_confirmation', v -> 'free_confirmation',
                  'cancellation', CASE WHEN pg_catalog.jsonb_typeof(v -> 'cancellation') = 'object'
                                       THEN (v -> 'cancellation') - ARRAY['amount_minor', 'rate'] ELSE v -> 'cancellation' END,
                  'payment', CASE WHEN pg_catalog.jsonb_typeof(v -> 'payment') = 'object'
                                  THEN (v -> 'payment') - ARRAY['amount_minor', 'currency', 'checkout_url'] ELSE v -> 'payment' END,
                  'refund', CASE WHEN pg_catalog.jsonb_typeof(v -> 'refund') = 'object'
                                 THEN pg_catalog.jsonb_build_object('state', v -> 'refund' -> 'state',
                                                                    'requested_at', v -> 'refund' -> 'requested_at',
                                                                    'goal_version', v -> 'refund' -> 'goal_version',
                                                                    'reason', v -> 'refund' -> 'reason')
                                 ELSE v -> 'refund' END,
                  'items', business.order_staff_items(o.id),
                  'attention', pg_catalog.to_jsonb(v_flags), 'needs_attention', pg_catalog.cardinality(v_flags) > 0,
                  'production', business.order_production_view(o.id),
                  'collections', business.order_staff_collections(o.id, false),
                  'refund_goal', business.order_staff_refund_goal(o.id, false),
                  'refund_operations', business.order_staff_refund_operations(o.id, false),
                  'refund_observations', business.order_staff_refund_observations(o.id, false),
                  'commands', business.order_staff_commands(o.id),
                  'appointment_change', v -> 'appointment_change',
                  'appointment_history', business.order_staff_appointments(o.id, false),
                  'fulfillment', v -> 'fulfillment',
                  'fulfillment_history', business.order_staff_fulfillment(o.id, false),
                  'audit', business.order_staff_audit(o.id));
  ELSE
    v_result := pg_catalog.jsonb_build_object(
                  'id', v -> 'id', 'version', v -> 'version', 'lifecycle', v -> 'lifecycle',
                  'raw_lifecycle', o.lifecycle, 'cancel_reason', v -> 'cancel_reason', 'cancelled_at', v -> 'cancelled_at',
                  'placed_at', v -> 'placed_at', 'date', v -> 'date', 'fulfillment_mode', v -> 'fulfillment_mode',
                  'slot', v -> 'slot', 'customer_full_name', v -> 'customer_full_name',
                  'items', business.order_staff_items(o.id),
                  'production', business.order_production_view(o.id),
                  'appointment_change', v -> 'appointment_change',
                  'appointment_history', business.order_staff_appointments(o.id, false),
                  'fulfillment', v -> 'fulfillment',
                  'fulfillment_history', business.order_staff_fulfillment(o.id, true));
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.staff_viewed', 'order', o.id, NULL,
                                pg_catalog.json_build_object('view', v_view));
  RETURN v_result::json;
END;
$$;

-- As in 0031, plus the no_show notice (p_facts: the secured outcome's
-- evidence with refund_returned).
CREATE OR REPLACE FUNCTION business.order_notice_render(p_kind text, p_o business.orders, p_facts json, p_origin text,
                                             OUT subject text, OUT body_text text, OUT body_html text)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_text text[] := ARRAY[]::text[];
  v_html text[] := ARRAY[]::text[];
  v_p text;
  v_paid bigint := business.order_paid_minor(p_o.id);
  v_free boolean := p_o.payment_requirement = 'none';
  v_link text := p_origin || '/orders/' || p_o.id::text;
  v_when text := business.notice_day(business.order_handover_at(p_o)) || ', ' || business.notice_time(business.order_handover_at(p_o))
                 || ' to ' || business.notice_time(p_o.handover_ends_at) || ' (London time)';
  v_mode text := CASE p_o.fulfillment_mode WHEN 'collection' THEN 'collection' WHEN 'customer_courier' THEN 'courier collection'
                                           ELSE 'handover' END;
  v_items text;
  v_totals text;
  v_rate integer;
  v_target bigint;
  v_returned boolean;
  v_from text;
  v_to text;
  v_before bigint;
  v_new bigint;
  v_style constant text := 'margin:0 0 16px';
  v_link_style constant text := 'color:#5b1a2e';
BEGIN
  SELECT pg_catalog.string_agg(
           '- ' || CASE WHEN i.quantity > 1 THEN i.quantity::text || ' × ' ELSE '' END || c.name || ' (' || r.flavour_name
           || ', ' || business.notice_size(r.size_code) || CASE WHEN i.physical_lot_id IS NOT NULL THEN ', ready-made' ELSE '' END
           || '): ' || business.notice_money(i.unit_price_minor) || CASE WHEN i.quantity > 1 THEN ' each' ELSE '' END,
           E'\n' ORDER BY i.line_no)
  INTO v_items
  FROM business.order_item i
  JOIN business.variant va ON va.id = i.variant_id
  JOIN business.cake c ON c.id = va.cake_id
  JOIN business.recipe_version r ON r.id = i.recipe_version_id
  WHERE i.order_id = p_o.id;
  v_totals := CASE WHEN p_o.discount_minor > 0
                   THEN 'Cakes: ' || business.notice_money(p_o.subtotal_minor) || E'\nDiscount: −'
                        || business.notice_money(p_o.discount_minor) || E'\n' ELSE '' END;

  IF p_kind = 'confirmed' THEN
    subject := 'Your GK Cakes order is confirmed';
    v_text := v_text || CASE WHEN v_free THEN 'Thank you. Your order is confirmed. It is free, so there is nothing to pay.'
                             ELSE 'Thank you. Your order is confirmed and we have received your payment of '
                                  || business.notice_money(v_paid) || '.' END;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\n' || pg_catalog.upper(pg_catalog.left(v_mode, 1))
                         || pg_catalog.substr(v_mode, 2) || ': ' || v_when);
    v_text := v_text || (E'Your cakes:\n' || coalesce(v_items, '') || E'\n' || v_totals
                         || CASE WHEN v_free THEN 'Total: £0' ELSE 'Total paid: ' || business.notice_money(v_paid) END);
    v_text := v_text || CASE p_o.fulfillment_mode
      WHEN 'collection' THEN 'Collection is from St Katharine Docks, London. The chef will send you the exact address and instructions about two days before.'
      WHEN 'customer_courier' THEN 'Please arrange and pay for your own courier to collect at the time above. The chef will send the exact collection address and instructions about two days before. Once the cake is handed to your courier, it is their responsibility.'
      ELSE 'Delivery by GK Cakes is agreed and paid separately. The chef will contact you about it.' END;
    IF NOT v_free THEN
      -- Against the command's persisted receipt (as the cancellation rate
      -- is), so a replayed transition renders the same bytes.
      v_text := v_text || CASE WHEN p_o.full_refund_deadline_at > (p_facts ->> 'received_at')::timestamptz
        THEN 'If you need to cancel: cancel before ' || business.notice_time(p_o.full_refund_deadline_at) || ' on '
             || business.notice_day(p_o.full_refund_deadline_at)
             || ' (London time) for a full refund of what you paid. From then on, the refund is 50%.'
        ELSE 'If you need to cancel: the refund is now 50% of what you paid.' END;
    END IF;
  ELSIF p_kind = 'paid_awaiting_agreement' THEN
    subject := 'Payment received for your GK Cakes order';
    v_text := v_text || ('We have received your payment of ' || business.notice_money(v_paid)
                         || '. It arrived after the preparation time needed for your chosen date, so your order is not confirmed yet.');
    v_text := v_text || 'The chef will contact you about whether the date can still work. If you do not agree a handover, the full amount you paid will be returned.'::text;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nRequested ' || v_mode || ': ' || v_when);
    v_text := v_text || (E'Your cakes:\n' || coalesce(v_items, '') || E'\n' || v_totals || 'Paid: ' || business.notice_money(v_paid));
  ELSIF p_kind = 'released' THEN
    subject := 'Your GK Cakes order has been released';
    v_text := v_text || CASE WHEN p_o.cancel_reason = 'free_not_confirmed'
      THEN 'Your free order has been released because it was not confirmed in time. The date and cakes are no longer held for you.'
      ELSE 'Your order has been released because its payment was not completed. The date and cakes are no longer held for you.' END;
    IF NOT v_free THEN
      v_text := v_text || 'We have no record of a successful payment for this order. If a payment does arrive later, the full amount will be returned.'::text;
    END IF;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nRequested ' || v_mode || ': ' || v_when);
  ELSIF p_kind = 'cancelled' THEN
    subject := 'Your GK Cakes order is cancelled';
    v_rate := (p_facts ->> 'rate')::integer;
    v_target := coalesce((p_facts ->> 'refund_target_minor')::bigint, 0);
    v_text := v_text || CASE p_facts ->> 'reason'
      WHEN 'chef_cancelled' THEN 'We are sorry: the chef has had to cancel your order.'
      WHEN 'late_payment_returned' THEN 'Your order has been cancelled because a handover could not be agreed after your payment arrived.'
      ELSE 'Your order has been cancelled as you asked.' END;
    v_returned := coalesce((p_facts ->> 'refund_returned')::boolean, false);
    v_text := v_text || CASE
      WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN 'There was no payment for this order, so there is nothing to refund.'
      WHEN v_target = 0 THEN 'No refund is due for this order.'
      ELSE CASE WHEN v_returned THEN 'Refund: ' ELSE 'Refund requested: ' END || business.notice_money(v_target)
           || CASE WHEN v_rate = 100 THEN ', the full amount you paid.'
                   ELSE ', which is ' || v_rate::text || '% of the ' || business.notice_money((p_facts ->> 'paid_minor')::bigint)
                        || ' you paid, because the full-refund deadline had been reached.' END END;
    IF v_target > 0 THEN
      v_text := v_text || CASE WHEN v_returned THEN 'It has already been returned to you.'
                               ELSE 'We have asked for this refund and will email you once it has been returned.' END;
    END IF;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nIt was planned for: ' || v_when);
  ELSIF p_kind = 'refund_returned' THEN
    subject := 'Your GK Cakes refund has been returned';
    v_text := v_text || ('We have returned ' || business.notice_money((p_facts ->> 'target_minor')::bigint)
                         || CASE WHEN (p_facts ->> 'goal_version')::integer > 1 THEN ' in total' ELSE '' END
                         || ' to you for this order. Depending on your bank, it can take a few working days to show on your statement.');
    v_text := v_text || ('Order reference: ' || p_o.id::text);
  ELSIF p_kind = 'no_show' THEN
    subject := CASE WHEN p_o.fulfillment_mode = 'gk_delivery' THEN 'Your GK Cakes order could not be handed over'
                    ELSE 'Your GK Cakes order was not collected' END;
    v_text := v_text || ('Your order was not ' || CASE WHEN p_o.fulfillment_mode = 'gk_delivery' THEN 'handed over'
                                                       ELSE 'collected' END
                         || ' at the agreed time (' || v_when || '), so it has been closed as not collected.');
    -- The amount this no-show newly reserved (earlier refunds count against
    -- the 50%, K05 cumulative goal), never the nominal 50% on its own.
    v_target := coalesce((p_facts ->> 'refund_target_minor')::bigint, 0);
    v_before := coalesce((p_facts ->> 'returned_before_minor')::bigint, 0);
    v_new := coalesce((p_facts ->> 'new_refund_minor')::bigint, 0);
    v_returned := coalesce((p_facts ->> 'refund_returned')::boolean, false);
    v_text := v_text || CASE
      WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN 'There was no payment for this order, so there is nothing to refund.'
      ELSE 'Our terms return 50% of what you paid when an order is not collected: ' || business.notice_money(v_target)
           || ' of the ' || business.notice_money((p_facts ->> 'paid_minor')::bigint) || ' you paid.' END;
    IF v_new > 0 THEN
      v_text := v_text || (CASE WHEN v_returned THEN 'Refund: ' ELSE 'Refund requested: ' END || business.notice_money(v_new)
                           || CASE WHEN v_before > 0 THEN ' (the ' || business.notice_money(v_before)
                                                          || ' already refunded counts towards it).' ELSE '.' END);
      v_text := v_text || CASE WHEN v_returned THEN 'It has already been returned to you.'
                               ELSE 'We have asked for this refund and will email you once it has been returned.' END;
    ELSIF v_target > 0 AND v_before >= v_target THEN
      v_text := v_text || ('The ' || business.notice_money(v_before) || ' already refunded covers this, so nothing more is due.');
    ELSIF v_target > v_before THEN
      v_text := v_text || ('The chef will contact you about the remaining ' || business.notice_money(v_target - v_before) || '.');
    END IF;
    v_text := v_text || ('If you think this is a mistake, please reply to this email.' || E'\nOrder reference: ' || p_o.id::text);
  ELSIF p_kind IN ('pickup_moved', 'appointment_changed') THEN
    v_from := business.notice_day((p_facts ->> 'from_starts_at')::timestamptz) || ', '
              || business.notice_time((p_facts ->> 'from_starts_at')::timestamptz) || ' to '
              || business.notice_time((p_facts ->> 'from_ends_at')::timestamptz) || ' (London time)';
    v_to := business.notice_day((p_facts ->> 'to_starts_at')::timestamptz) || ', '
            || business.notice_time((p_facts ->> 'to_starts_at')::timestamptz) || ' to '
            || business.notice_time((p_facts ->> 'to_ends_at')::timestamptz) || ' (London time)';
    IF p_kind = 'pickup_moved' THEN
      subject := 'Your GK Cakes ' || v_mode || ' time has changed';
      v_text := v_text || ('Your ' || v_mode || ' time has been changed as you asked.')::text;
    ELSE
      subject := 'Your GK Cakes ' || v_mode || ' date or time has changed';
      v_text := v_text || ('As agreed with you, the chef has changed your ' || v_mode || ' date or time.')::text;
    END IF;
    v_text := v_text || ('New time: ' || v_to || E'\nPrevious time: ' || v_from || E'\nOrder reference: ' || p_o.id::text);
    IF p_kind = 'pickup_moved' THEN
      v_text := v_text || ('You can change the time yourself to another available time on the same day until six hours before it starts. '
                           || 'For anything else, please contact the chef.')::text;
    END IF;
    IF NOT v_free THEN
      v_text := v_text || CASE WHEN p_o.full_refund_deadline_at > (p_facts ->> 'received_at')::timestamptz
        THEN 'Changing the time does not change your cancellation terms: cancel before '
             || business.notice_time(p_o.full_refund_deadline_at) || ' on ' || business.notice_day(p_o.full_refund_deadline_at)
             || ' (London time) for a full refund of what you paid. From then on, the refund is 50%.'
        ELSE 'Changing the time does not change your cancellation terms: the refund is 50% of what you paid.' END;
    END IF;
  ELSE
    RAISE EXCEPTION 'unknown order notice %', p_kind USING ERRCODE = 'invalid_parameter_value';
  END IF;

  -- Paragraphs: plain text separated by blank lines; HTML paragraphs with
  -- line breaks; then the link paragraph and the footer.
  FOREACH v_p IN ARRAY v_text LOOP
    v_html := v_html || ('<p style="' || v_style || '">' || pg_catalog.replace(business.notice_html(v_p), E'\n', '<br>') || '</p>');
  END LOOP;
  -- A released order has no order page paragraph (and no invitation to
  -- order again: no marketing).
  IF p_kind <> 'released' THEN
    v_text := v_text || ('Your order page: ' || v_link
                         || E'\nIf you ordered without signing in, open this link on the same device and browser you ordered with.');
    v_html := v_html || ('<p style="' || v_style || '">Your order page: <a href="' || business.notice_html(v_link)
                         || '" style="' || v_link_style || '">' || business.notice_html(v_link)
                         || '</a><br>If you ordered without signing in, open this link on the same device and browser you ordered with.</p>');
  END IF;
  v_p := E'GK Cakes, London\nYou can reply to this email if you have a question. You are receiving it because of your order with GK Cakes.';
  body_text := E'Hello,\n\n' || pg_catalog.array_to_string(v_text, E'\n\n') || E'\n\n' || v_p || E'\n';
  body_html := '<!DOCTYPE html><html lang="en-GB"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width">'
    || '<title>' || business.notice_html(subject) || '</title></head>'
    || '<body style="margin:0;padding:24px;background:#ffffff;color:#141416;font-family:Helvetica,Arial,sans-serif;font-size:16px;line-height:1.5">'
    || '<div style="max-width:560px;margin:0 auto">'
    || '<p style="margin:0 0 24px;font-family:Georgia,serif;font-size:22px;color:#5b1a2e">GK Cakes</p>'
    || '<p style="' || v_style || '">Hello,</p>' || pg_catalog.array_to_string(v_html, '')
    || '<p style="margin:32px 0 0;font-size:13px;color:#55585e">' || pg_catalog.replace(business.notice_html(v_p), E'\n', '<br>')
    || '</p></div></body></html>';
END;
$$;

-- As in 0031, with the no_show template revision.
CREATE OR REPLACE FUNCTION business.order_notice_enqueue(p_order uuid, p_kind text, p_dedupe text, p_transition uuid, p_facts json)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_s business.notice_settings%ROWTYPE;
  v_o business.orders%ROWTYPE;
  v_rev text := CASE p_kind WHEN 'confirmed' THEN 'order-confirmed.v1' WHEN 'paid_awaiting_agreement' THEN 'order-paid-late.v1'
                            WHEN 'released' THEN 'order-released.v1' WHEN 'cancelled' THEN 'order-cancelled.v1'
                            WHEN 'refund_returned' THEN 'refund-returned.v1' WHEN 'pickup_moved' THEN 'pickup-moved.v1'
                            WHEN 'appointment_changed' THEN 'appointment-changed.v1'
                            WHEN 'no_show' THEN 'order-no-show.v1' END;
  v_name text := p_order::text || ':' || p_kind || ':' || p_dedupe;
  v_id uuid;
  v_key uuid;
  v_subject text;
  v_text text;
  v_html text;
  v_payload text;
  v_outcome text;
  v_detail text;
BEGIN
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('gk.order_notice:' || v_name, 0));
  IF EXISTS (SELECT 1 FROM business.order_notice n WHERE n.order_id = p_order AND n.kind = p_kind AND n.dedupe = p_dedupe) THEN
    RETURN 'exists';
  END IF;
  SELECT * INTO v_s FROM business.notice_settings s WHERE s.singleton;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order;
  IF v_s.singleton IS NULL THEN
    v_outcome := 'not_configured';
  ELSIF NOT business.notice_address_ok(v_o.contact_email) THEN
    v_outcome := 'not_queued';
    v_detail := 'recipient';
  ELSIF NOT business.notice_recipient_allowed(v_s, v_o.contact_email) THEN
    -- Outside production, a recipient off the allowlist is never queued, so
    -- no worker build (old, failed or rolled back) can send to it.
    v_outcome := 'not_queued';
    v_detail := 'allowlist';
  ELSE
    v_id := business.notice_uuid(v_s.environment || ':order_notice:intent:' || v_name);
    v_key := business.notice_uuid(v_s.environment || ':order_notice:key:' || v_name);
    BEGIN
      SELECT r.subject, r.body_text, r.body_html INTO v_subject, v_text, v_html
      FROM business.order_notice_render(p_kind, v_o, p_facts, v_s.ui_origin) r;
      -- notifications payload and Request, field for field (canonical form).
      v_payload := '{"provider":"resend","environment":' || business.notice_json(v_s.environment)
        || ',"idempotency_key":' || business.notice_json(v_key::text)
        || ',"request":{"from":' || business.notice_json('GK Cakes <orders@gkcake.com>')
        || ',"to":[' || business.notice_json(v_o.contact_email) || ']'
        || ',"reply_to":[' || business.notice_json('gk.cake.london@gmail.com') || ']'
        || ',"subject":' || business.notice_json(v_subject)
        || ',"text":' || business.notice_json(v_text)
        || ',"html":' || business.notice_json(v_html)
        || ',"tags":[{"name":"intent","value":' || business.notice_json(v_id::text) || '},{"name":"env","value":'
        || business.notice_json(v_s.environment) || '}]}}';
      PERFORM business.notification_enqueue(v_id, p_transition, p_order, v_rev, v_key, v_payload::json, 'system', NULL);
      v_outcome := 'queued';
    EXCEPTION WHEN OTHERS THEN
      v_outcome := 'not_queued';
      v_detail := pg_catalog.lower(SQLSTATE);
    END;
  END IF;
  INSERT INTO business.order_notice (id, order_id, kind, dedupe, transition_id, template_revision, outcome, intent_id, detail)
  VALUES (pg_catalog.gen_random_uuid(), p_order, p_kind, p_dedupe, p_transition, v_rev, v_outcome,
          CASE WHEN v_outcome = 'queued' THEN v_id END, v_detail)
  -- A concurrent transaction that recorded it first (the advisory lock
  -- makes this unlikely) must not fail this transition at commit.
  ON CONFLICT ON CONSTRAINT order_notice_once DO NOTHING;
  IF v_outcome <> 'queued' THEN
    -- Visible to staff (the order's audit summary; attention flag
    -- notice_not_sent), without personal data.
    PERFORM business.audit_record('system', NULL, 'order.notice_not_queued', 'order', p_order, p_transition,
                                  pg_catalog.json_build_object('kind', p_kind, 'outcome', v_outcome, 'detail', v_detail));
  END IF;
  RETURN v_outcome;
END;
$$;

-- As in 0031, plus the no_show notice when a no-show is secured (a
-- handover sends none).
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
  v_evidence json;
BEGIN
  PERFORM 1 FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  v_state := business.recovery_mark_secured(v.id);
  SELECT r.outcome ->> 'status', r.outcome -> 'evidence' INTO v_status, v_evidence
  FROM business.recovery_operation r WHERE r.id = v.id;
  UPDATE business.orders o SET recovery_state = 'secured', updated_at = pg_catalog.now()
  WHERE o.id = v.order_id
    AND ((o.recovery_state = 'confirmation_pending' AND v.command <> 'cancel'
          AND v_status IN ('confirmed', 'paid_awaiting_agreement'))
         OR (o.recovery_state = 'cancellation_pending' AND v.command IN ('cancel', 'late_payment')
             AND v_status = 'cancelled'));
  IF FOUND THEN
    -- A cancellation's refund may already have succeeded before the order
    -- was secured: the email then says so instead of "requested".
    PERFORM business.order_notice_enqueue(v.order_id, v_status, v.id::text, v.id,
      CASE WHEN v_status = 'cancelled'
           THEN (v_evidence::jsonb || pg_catalog.jsonb_build_object('refund_returned',
                   coalesce(business.order_refund_summary(v.order_id) ->> 'state' = 'returned', false)))::json
           ELSE pg_catalog.json_build_object('received_at', v.received_at) END);
  ELSIF v.command IN ('pickup_move', 'appointment_change') AND v_status = 'moved' AND v_state = 'secured' THEN
    -- The moved appointment is visible from now on: its notice, once per
    -- command (order_notice dedupe), rendered from the history row.
    PERFORM business.order_notice_enqueue(v.order_id,
      CASE v.command WHEN 'pickup_move' THEN 'pickup_moved' ELSE 'appointment_changed' END, v.id::text, v.id,
      (SELECT pg_catalog.json_build_object('from_starts_at', c.from_starts_at, 'from_ends_at', c.from_ends_at,
                                           'to_starts_at', c.to_starts_at, 'to_ends_at', c.to_ends_at,
                                           'received_at', c.received_at)
       FROM business.pickup_change c WHERE c.command_id = v.id));
  ELSIF v.command = 'no_show' AND v_status = 'no_show' AND v_state = 'secured' THEN
    PERFORM business.order_notice_enqueue(v.order_id, 'no_show', v.id::text, v.id,
      (v_evidence::jsonb || pg_catalog.jsonb_build_object('refund_returned',
         coalesce(business.order_refund_summary(v.order_id) ->> 'state' = 'returned', false)))::json);
  END IF;
  RETURN v_state;
END;
$$;

-- As in 0031, plus the handover and no-show (business.order_handover_commit;
-- a no-show locks the collections first: it reserves refunds).
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
  IF v.command IN ('paid_confirm', 'cancel', 'late_payment', 'manual_payout', 'manual_payout_outcome', 'no_show') THEN
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

  IF v.command IN ('cancel', 'late_payment', 'manual_payout', 'manual_payout_outcome', 'pickup_move', 'appointment_change',
                   'handover', 'no_show') THEN
    v_outcome := CASE v.command WHEN 'cancel' THEN business.order_cancel_commit(v)
                                WHEN 'handover' THEN business.order_handover_commit(v)
                                WHEN 'no_show' THEN business.order_handover_commit(v)
                                WHEN 'pickup_move' THEN business.order_appointment_commit(v)
                                WHEN 'appointment_change' THEN business.order_appointment_commit(v)
                                WHEN 'late_payment' THEN business.order_late_payment_commit(v)
                                WHEN 'manual_payout' THEN business.manual_payout_commit(v)
                                ELSE business.manual_payout_outcome_commit(v) END;
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

-- As in 0030, and nothing while a no-show is not secured (its own notice
-- comes first; the secured command's trigger rechecks).
CREATE OR REPLACE FUNCTION business.order_notice_refund_check(p_order uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v json;
BEGIN
  IF p_order IS NULL OR NOT EXISTS (SELECT 1 FROM business.orders o
                                    WHERE o.id = p_order AND o.recovery_state <> 'cancellation_pending')
     OR business.order_handover_pending(p_order) THEN
    RETURN;
  END IF;
  v := business.order_refund_summary(p_order);
  IF v ->> 'state' = 'returned' AND (v ->> 'target_minor')::bigint > 0 THEN
    PERFORM business.order_notice_enqueue(p_order, 'refund_returned', v ->> 'goal_version',
              business.notice_uuid('refund_returned:' || p_order::text || ':' || (v ->> 'goal_version')),
              pg_catalog.json_build_object('target_minor', (v ->> 'target_minor')::bigint,
                                           'goal_version', (v ->> 'goal_version')::integer));
  END IF;
END;
$$;

REVOKE ALL ON TABLE business.fulfillment_event FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_fulfillment_state(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_handover_pending(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_visible_fulfillment(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_fulfillment(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.fulfillment_refusal(business.orders, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.fulfillment_target(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.fulfillment_check_input(text, text, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_fulfillment_transition(uuid, uuid, text, text, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_handover_request(uuid, uuid, text, text, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_handover_commit(business.order_command) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_fulfillment_transition(uuid, uuid, text, text, integer, text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_handover_request(uuid, uuid, text, text, integer, text, text, timestamptz) TO gk_api;
