-- GK-017 r02b state 19: a failed GK delivery (design
-- docs/design/sessions/checkout-recovery/gk-017-r02 state 19 "Delivery
-- failed · our side (choose, collection chosen, redelivery chosen)" and
-- "Delivery failed · nobody available"; Creator decisions 2026-11-05: Q17
-- "GK-caused delivery failure: redelivery or collection, or 100% of the
-- delivery back (K05)" (r02b reading: two choices, redelivery or collection
-- with the delivery refunded in full; question 24 is open), Q18
-- "Customer-caused failure: treated like a no-show (50% of the cake;
-- delivery payment kept)", Q10 "After a failed delivery: any opening time
-- that day" (state 19: the times still ahead today), Q19 (the no-show refund
-- is requested when the chef records it); contracts r6.5 K05 "GK-caused
-- unperformed delivery raises its target to 100% delivery ... explicit
-- collection floor", no-show 50%, cumulative goal, delivery refunds; K06
-- "GK Delivery responsibility through customer handover ... Delivery failure
-- is actionable, not automatic completed"; K08 audit without personal data;
-- K09 expand-only; K11 accepted order commands).
--
-- Chef: the delivery failed (business.order_delivery_failed, chef only:
-- recording a failure opens a money-relevant outcome, K00 "fulfillment can
-- prepare/ready/dispatch/handover only"; fulfilment records what it saw as a
-- delivery_exception). Only while the GK delivery is on its way: the order
-- is ready, fulfilment out_for_delivery or exception, its latest dispatch
-- has no our-side failure recorded yet and no chef reschedule followed it
-- (business.delivery_on_its_way); outside the fulfilment scope 404.
--   our_side (GK caused it): a plain versioned command (order version + 1,
--   409 stale_version; the same key replays, also during a restore freeze;
--   GK503 otherwise while frozen; 409 state_conflict while a K11 command is
--   unresolved; 409 transition_not_allowed when the delivery is not on its
--   way). Records one immutable business.delivery_failure row (the dispatch it
--   ends, the current quote, its paid matched delivery collection and amount
--   (the 100% delivery obligation, K05), the staff-only reason, the chef, the
--   receipt), a fulfilment event delivery_failed (to exception) and audit
--   delivery.failed (no reason). It refunds nothing by itself and records no
--   business.refund_floor (that row is immutable and unique per collection,
--   and a redelivery must leave no floor behind): the customer's choice
--   decides. From then on the order is not dispatched until the customer
--   chose redelivery and the chef set the new time (409
--   delivery_failure_open, redelivery_time_not_set), no no-show is recorded
--   for the failed dispatch, and the chef's appointment change waits for the
--   choice (409 move_not_eligible).
--   nobody_available (Q18): the existing chef no_show K11 command (0032,
--   business.order_handover_enqueue: one implementation behind both routes)
--   with a recorded cause: a GK delivery on its way, at or after the
--   appointment start (409 handover_not_due). Its commit marks the paid
--   delivery kept (business.delivery_kept) and records the no-show goal of
--   50% of the cake (the paid aggregate without the kept delivery; the kept
--   delivery stays out of every allocation, business.refund_shares), so
--   nothing of the delivery is refunded or left pending for the chef. The
--   no_show notice says the delivery payment is kept; every no-show notice
--   states a stray delivery payment returned with it (unmatched_minor).
--
-- Customer: the choice after an our-side failure
-- (business.order_delivery_failure_choice_request, API). The owner, or a
-- guest capability with "move_pickup" for both choices (each decides how and
-- when the cake is handed over: the same-day move's access). A K11 order
-- command 'delivery_failure_choice' (prepared envelope before the commit,
-- outcome envelope before it is visible, exactly one outcome, the same actor
-- and key replay, another body is key_reused, another protected change is
-- 409 state_conflict, a restore freeze refuses new requests and defers the
-- commit). Only while the choice is open (409 choice_not_open), at the
-- version the customer saw (409 stale_version). No deadline (as Q8: the case
-- stays open with the chef; attention flag delivery_failure_open).
--   redelivery: the order stays a GK delivery and its time and money do not
--   change; nothing more to pay and nothing refunded. A cancellation before
--   the redelivery is dispatched records the delivery's 100% floor and
--   returns it in full (business.delivery_failure_floor_record, from the
--   terminal return; the preview and request compute it in a rolled-back
--   block). The
--   chef then agrees the new time through the existing appointment change
--   (0031); a dispatch is accepted again once such a change, requested after
--   the choice, is secured (business.delivery_failure_refusal; flag
--   redelivery_time_needed until then).
--   collection: target_slot_id must be a published start of the current
--   schedule on the order's handover day (the failed delivery's day; the day
--   place stays), after the receipt instant, within the ready cakes' shelf
--   life (409 slot_unavailable). The commit changes the mode and the
--   appointment; the original handover, refund deadline and production
--   anchor never move. A paid delivery gets its 100% floor
--   (business.refund_floor, reason gk_delivery_failure, recorded by the chef
--   who recorded the failure), the goal raised and the refund reserved, as
--   0022/0036: the delivery comes back in full, once; the cake is not
--   refunded. The refund stays invisible (no refund block, no email) until
--   the choice is secured (business.order_refund_hidden).
-- The commit re-checks the version, that this failure's choice is still
-- open, the paid delivery and the target slot (decided by the persisted
-- receipt instant, as every order command); otherwise a refusal
-- (order_changed, not_open, slot_unavailable; version + 1 only).
--
-- Customer cancellation after a dispatch (K05/K06, Q18; design state 19 shows
-- no cancel control once the delivery is on its way): the preview and the
-- request refuse with 409 cancellation_not_eligible while a GK delivery is
-- dispatched (out for delivery, an exception after the dispatch, or an open
-- or pending our-side failure choice), until a secured redelivery choice
-- returns the order to a not-yet-dispatched state (business.delivery_dispatched).
-- The chef's 100% cancellation stays available. A secured chef reschedule
-- after the latest dispatch also un-dispatches the order
-- (business.order_rescheduled_since_dispatch); 0037 refuses such a
-- reschedule while the delivery is on its way (the chef records the
-- failure's cause first), so this covers one made before 0037.
--
-- Read models. order_view adds delivery_failure (the latest failure; NULL
-- when none): {cause, at, state, open, pending, choices, times, delivery_paid_minor
-- (our side, the paid delivery at stake), chosen {choice, state, refusal_reason,
-- requested_at, to, delivery_refund_minor (collection with a paid delivery,
-- once secured)}, redelivery_time_set (a secured redelivery), cake_refund_minor
-- and delivery_kept_minor (nobody available, once secured)}. times are the
-- day's published starts while open; the API keeps those after its clock.
-- While a collection choice is committed but unsecured the mode and
-- appointment read as before (order_visible_mode, order_visible_appointment),
-- and the delivery block reads cancelled_by_choice once it is secured. Staff:
-- the chef also gets every failure with its reason and choices, technical
-- support the same without money or reason, fulfilment the state.
--
-- Expand-only (K09): new tables, replaced CHECK constraints, new functions;
-- replaced functions keep their signatures, results, security and grants
-- (appointment_change_eligible becomes STABLE: it reads the failure). Lock
-- order (K00): the order, its collections by ID, the recovery operation, the
-- goal and refund operations, history/audit.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------- schema

ALTER TABLE business.order_command DROP CONSTRAINT order_command_command_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_command_check CHECK (command IN ('free_confirm', 'paid_confirm', 'cancel', 'late_payment',
                                                                'manual_payout', 'manual_payout_outcome', 'pickup_move',
                                                                'appointment_change', 'handover', 'no_show',
                                                                'delivery_choice', 'delivery_failure_choice'));
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_delivery_failure_choice CHECK (command <> 'delivery_failure_choice'
                                                              OR actor_kind IN ('customer', 'guest'));

ALTER TABLE business.fulfillment_event DROP CONSTRAINT fulfillment_event_action_check;
ALTER TABLE business.fulfillment_event
  ADD CONSTRAINT fulfillment_event_action_check CHECK (action IN ('ready', 'dispatch', 'delivery_exception', 'handover',
                                                                  'no_show', 'delivery_failed'));

-- A failed GK delivery recorded by the chef: one immutable row per our-side
-- failure of a dispatch, or per nobody-available no-show request (whose
-- outcome is its K11 command's).
CREATE TABLE business.delivery_failure (
  id                     uuid PRIMARY KEY,
  order_id               uuid NOT NULL REFERENCES business.orders (id),
  cause                  text NOT NULL CHECK (cause IN ('our_side', 'nobody_available')),
  -- The dispatch whose delivery failed.
  dispatch_event_id      uuid NOT NULL REFERENCES business.fulfillment_event (id),
  -- The current quote and, when it was paid, its matched collection and
  -- amount (our side: the 100% delivery obligation; nobody available: the
  -- delivery payment kept).
  quote_id               uuid NULL REFERENCES business.delivery_quote (id),
  delivery_collection_id uuid NULL REFERENCES business.payment_collection (id),
  delivery_minor         bigint NULL CHECK (delivery_minor > 0),
  -- Staff only (the chef's view): never audited, never shown to the customer.
  reason                 text NULL CHECK (reason = btrim(reason) AND char_length(reason) BETWEEN 1 AND 500
                                          AND reason !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  -- Nobody available: the chef's no_show command.
  no_show_command_id     uuid NULL UNIQUE REFERENCES business.order_command (id),
  order_version          integer NOT NULL CHECK (order_version >= 1),
  recorded_by            uuid NOT NULL REFERENCES business.account (id),
  -- The persisted receipt instant.
  recorded_at            timestamptz NOT NULL,
  created_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT delivery_failure_cause CHECK ((cause = 'nobody_available') = (no_show_command_id IS NOT NULL)),
  CONSTRAINT delivery_failure_delivery CHECK ((delivery_collection_id IS NULL) = (delivery_minor IS NULL))
);
CREATE INDEX delivery_failure_order_idx ON business.delivery_failure (order_id, created_at);
CREATE UNIQUE INDEX delivery_failure_our_side_idx ON business.delivery_failure (dispatch_event_id) WHERE cause = 'our_side';
CREATE TRIGGER delivery_failure_immutable BEFORE UPDATE OR DELETE ON business.delivery_failure
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER delivery_failure_no_truncate BEFORE TRUNCATE ON business.delivery_failure
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- The customer's choice after an our-side failure, one row per accepted
-- request (its K11 command); the outcome is the command's recovery
-- operation. Also the history of the explicit, agreed mode change (K06).
CREATE TABLE business.delivery_failure_choice (
  command_id             uuid PRIMARY KEY REFERENCES business.order_command (id),
  order_id               uuid NOT NULL REFERENCES business.orders (id),
  failure_id             uuid NOT NULL REFERENCES business.delivery_failure (id),
  choice                 text NOT NULL CHECK (choice IN ('redelivery', 'collection')),
  -- The paid matched delivery collection at the receipt.
  delivery_collection_id uuid NULL REFERENCES business.payment_collection (id),
  -- The order's mode and appointment at the receipt.
  from_mode              text NOT NULL CHECK (from_mode = 'gk_delivery'),
  from_day               date NOT NULL,
  from_slot_id           uuid NOT NULL,
  from_starts_at         timestamptz NOT NULL,
  from_ends_at           timestamptz NOT NULL,
  -- A collection's time on the same day, and its schedule revision.
  to_slot_id             uuid NULL,
  to_starts_at           timestamptz NULL,
  to_ends_at             timestamptz NULL,
  schedule_revision      integer NULL CHECK (schedule_revision >= 1),
  actor_kind             text NOT NULL CHECK (actor_kind IN ('customer', 'guest')),
  actor_account_id       uuid NULL REFERENCES business.account (id),
  guest_capability_id    uuid NULL REFERENCES business.order_guest_capability (id),
  idempotency_key        text NOT NULL CHECK (idempotency_key ~ '^[\x21-\x7e]{1,128}$'),
  body_sha256            char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  order_version          integer NOT NULL CHECK (order_version >= 1),
  received_at            timestamptz NOT NULL,
  created_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT delivery_failure_choice_actor CHECK ((actor_kind = 'customer') = (actor_account_id IS NOT NULL)
                                                  AND (actor_kind = 'guest') = (guest_capability_id IS NOT NULL)),
  CONSTRAINT delivery_failure_choice_target CHECK (
    (choice = 'redelivery' AND to_slot_id IS NULL AND to_starts_at IS NULL AND to_ends_at IS NULL
     AND schedule_revision IS NULL)
    OR (choice = 'collection' AND to_slot_id IS NOT NULL AND to_starts_at IS NOT NULL
        AND to_ends_at = to_starts_at + interval '60 minutes' AND schedule_revision IS NOT NULL))
);
CREATE INDEX delivery_failure_choice_order_idx ON business.delivery_failure_choice (order_id, created_at);
CREATE INDEX delivery_failure_choice_failure_idx ON business.delivery_failure_choice (failure_id, created_at);
CREATE UNIQUE INDEX delivery_failure_choice_key_idx
  ON business.delivery_failure_choice (order_id, idempotency_key, actor_account_id, guest_capability_id) NULLS NOT DISTINCT;
CREATE TRIGGER delivery_failure_choice_immutable BEFORE UPDATE OR DELETE ON business.delivery_failure_choice
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER delivery_failure_choice_no_truncate BEFORE TRUNCATE ON business.delivery_failure_choice
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- A delivery payment kept after a nobody-available failure (Q18): recorded
-- by the no-show's commit; the collection stays out of every refund
-- allocation (business.refund_shares) unless the chef records a floor on it.
CREATE TABLE business.delivery_kept (
  collection_id uuid PRIMARY KEY REFERENCES business.payment_collection (id),
  order_id      uuid NOT NULL REFERENCES business.orders (id),
  failure_id    uuid NOT NULL UNIQUE REFERENCES business.delivery_failure (id),
  amount_minor  bigint NOT NULL CHECK (amount_minor > 0),
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER delivery_kept_immutable BEFORE UPDATE OR DELETE ON business.delivery_kept
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER delivery_kept_no_truncate BEFORE TRUNCATE ON business.delivery_kept
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- ------------------------------------------------------------ helpers

-- Owner-internal: the order's latest dispatch (fulfilment event), or NULL.
CREATE FUNCTION business.order_last_dispatch(p_order uuid)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT e.id FROM business.fulfillment_event e
  WHERE e.order_id = p_order AND e.action = 'dispatch'
  ORDER BY e.created_at DESC, e.id DESC LIMIT 1;
$$;

-- Owner-internal: the our-side failure recorded for the order's latest
-- dispatch, or NULL.
CREATE FUNCTION business.delivery_failure_current(p_order uuid)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT f.id FROM business.delivery_failure f
  WHERE f.order_id = p_order AND f.cause = 'our_side' AND f.dispatch_event_id = business.order_last_dispatch(p_order);
$$;

-- Owner-internal: what the customer chose after our-side failure p_failure
-- (redelivery | collection), from a choice whose outcome is chosen (secured,
-- or with p_secured false also committed); NULL while none.
CREATE FUNCTION business.delivery_failure_chosen(p_failure uuid, p_secured boolean)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT c.choice FROM business.delivery_failure_choice c JOIN business.recovery_operation r ON r.id = c.command_id
  WHERE c.failure_id = p_failure AND r.outcome ->> 'status' = 'chosen'
    AND (r.state = 'secured' OR (NOT p_secured AND r.state = 'committed'))
  ORDER BY c.created_at DESC, c.command_id DESC LIMIT 1;
$$;

-- Owner-internal: whether order p_o's choice after an our-side failure is
-- open (unresolved commands are the caller's): a GK delivery, ready and
-- secured, whose latest dispatch failed on our side and nothing was chosen.
CREATE FUNCTION business.delivery_failure_open(p_o business.orders)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_o.fulfillment_mode = 'gk_delivery' AND p_o.lifecycle = 'ready' AND p_o.recovery_state = 'secured'
     AND coalesce((SELECT business.delivery_failure_chosen(x.f, false) IS NULL
                   FROM (SELECT business.delivery_failure_current(p_o.id) AS f) x WHERE x.f IS NOT NULL), false);
$$;

-- Owner-internal: whether a secured chef appointment change (0031, outcome
-- moved) was requested after the order's latest dispatch: the order then
-- waits for a new dispatch (0037 refuses such a change while a delivery is
-- on its way; this also covers one made before 0037).
CREATE FUNCTION business.order_rescheduled_since_dispatch(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.fulfillment_event e
                 JOIN business.pickup_change p ON p.order_id = e.order_id AND p.kind = 'appointment_change'
                                              AND p.created_at > e.created_at
                 JOIN business.recovery_operation r ON r.id = p.command_id
                 WHERE e.id = business.order_last_dispatch(p_order)
                   AND r.state = 'secured' AND r.outcome ->> 'status' = 'moved');
$$;

-- Owner-internal: whether order p_o's GK delivery is on its way: ready,
-- dispatched (out for delivery, or an exception after the dispatch), no
-- our-side failure recorded for that dispatch and not rescheduled since.
CREATE FUNCTION business.delivery_on_its_way(p_o business.orders)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_o.fulfillment_mode = 'gk_delivery' AND p_o.lifecycle = 'ready'
     AND business.order_fulfillment_state(p_o) IN ('out_for_delivery', 'exception')
     AND business.order_last_dispatch(p_o.id) IS NOT NULL AND business.delivery_failure_current(p_o.id) IS NULL
     AND NOT business.order_rescheduled_since_dispatch(p_o.id);
$$;

-- Owner-internal: whether order p_o's GK delivery counts as dispatched for a
-- customer cancellation: it was dispatched, not rescheduled since, and no
-- secured redelivery choice returned it to waiting for a new dispatch.
CREATE FUNCTION business.delivery_dispatched(p_o business.orders)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_o.fulfillment_mode = 'gk_delivery' AND business.order_last_dispatch(p_o.id) IS NOT NULL
     AND NOT business.order_rescheduled_since_dispatch(p_o.id)
     AND business.delivery_failure_chosen(business.delivery_failure_current(p_o.id), true) IS DISTINCT FROM 'redelivery';
$$;

-- Owner-internal: once the latest dispatch failed on our side and before a
-- redelivery is dispatched, record the paid delivery's 100% floor
-- (business.refund_floor, reason gk_delivery_failure, recorded by the chef
-- who recorded the failure; K05 "GK-caused unperformed delivery raises its
-- target to 100% delivery"), once. Called by a cancellation's terminal
-- return (the order is cancelled, so no redelivery can follow) and, rolled
-- back, by the cancellation preview and request to compute the same amount.
-- The caller holds the order and its collections. p_actor_kind NULL: no
-- audit (inside a rolled-back computation). Returns the floor recorded now,
-- or NULL.
CREATE FUNCTION business.delivery_failure_floor_record(p_order uuid, p_actor_kind text, p_account uuid)
RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  f business.delivery_failure%ROWTYPE;
  v_quote uuid;
BEGIN
  SELECT * INTO f FROM business.delivery_failure x WHERE x.id = business.delivery_failure_current(p_order);
  IF f.id IS NULL OR f.delivery_collection_id IS NULL
     OR NOT EXISTS (SELECT 1 FROM business.orders o WHERE o.id = p_order AND o.fulfillment_mode = 'gk_delivery')
     OR EXISTS (SELECT 1 FROM business.refund_floor rf WHERE rf.collection_id = f.delivery_collection_id) THEN
    RETURN NULL;
  END IF;
  v_quote := business.delivery_current_quote(p_order);
  IF v_quote IS NULL OR business.delivery_paid_collection(v_quote) IS DISTINCT FROM f.delivery_collection_id THEN
    RETURN NULL;
  END IF;
  INSERT INTO business.refund_floor (id, order_id, collection_id, floor_minor, reason, recorded_by)
  VALUES (pg_catalog.gen_random_uuid(), p_order, f.delivery_collection_id, f.delivery_minor, 'gk_delivery_failure',
          f.recorded_by);
  IF p_actor_kind IS NOT NULL THEN
    PERFORM business.audit_record(p_actor_kind, p_account, 'refund.floor_recorded', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('collection_id', f.delivery_collection_id,
                                                               'floor_minor', f.delivery_minor, 'failure_id', f.id));
  END IF;
  RETURN f.delivery_minor;
END;
$$;

-- Owner-internal: whether the chef set the new time after the customer
-- chose redelivery after our-side failure p_failure: a secured chef
-- appointment change (0031, outcome moved) requested after the secured
-- redelivery choice.
CREATE FUNCTION business.redelivery_time_set(p_failure uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.delivery_failure_choice c
                 JOIN business.recovery_operation cr ON cr.id = c.command_id
                 JOIN business.pickup_change p ON p.order_id = c.order_id AND p.kind = 'appointment_change'
                                              AND p.created_at > c.created_at
                 JOIN business.recovery_operation pr ON pr.id = p.command_id
                 WHERE c.failure_id = p_failure AND c.choice = 'redelivery'
                   AND cr.state = 'secured' AND cr.outcome ->> 'status' = 'chosen'
                   AND pr.state = 'secured' AND pr.outcome ->> 'status' = 'moved');
$$;

-- Owner-internal: why order p_o may not be dispatched after an our-side
-- failure of its latest dispatch (NULL when there is none or the redelivery
-- is ready): delivery_failure_open (nothing chosen yet, or the choice is not
-- secured) or redelivery_time_not_set (the chef has not set the new time).
CREATE FUNCTION business.delivery_failure_refusal(p_o business.orders)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN x.f IS NULL THEN NULL
              WHEN business.delivery_failure_chosen(x.f, true) IS DISTINCT FROM 'redelivery' THEN 'delivery_failure_open'
              WHEN NOT business.redelivery_time_set(x.f) THEN 'redelivery_time_not_set' END
  FROM (SELECT business.delivery_failure_current(p_o.id) AS f) x;
$$;

-- Owner-internal: a choice after an our-side failure as the order page
-- shows it: state processing (not secured), chosen or refused
-- (refusal_reason); to (collection: the new mode and time); and
-- delivery_refund_minor, the paid delivery returned in full (collection
-- with a paid delivery, only once secured: K11).
CREATE FUNCTION business.delivery_failure_choice_json(c business.delivery_failure_choice)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'choice', c.choice,
           'state', CASE WHEN r.state <> 'secured' THEN 'processing' ELSE r.outcome ->> 'status' END,
           'refusal_reason', CASE WHEN r.state = 'secured' AND r.outcome ->> 'status' = 'refused'
                                  THEN r.outcome -> 'evidence' ->> 'reason' END,
           'requested_at', c.received_at,
           'to', CASE WHEN c.choice = 'collection' THEN pg_catalog.jsonb_build_object(
                   'fulfillment_mode', 'collection', 'date', c.from_day, 'id', c.to_slot_id, 'starts_at', c.to_starts_at,
                   'ends_at', c.to_ends_at) END)
         || CASE WHEN c.choice = 'collection' AND c.delivery_collection_id IS NOT NULL AND r.state = 'secured'
                      AND r.outcome ->> 'status' = 'chosen' THEN pg_catalog.jsonb_build_object(
              'delivery_refund_minor', (SELECT pc.amount_minor FROM business.payment_collection pc
                                        WHERE pc.id = c.delivery_collection_id))
            ELSE '{}'::jsonb END
  FROM business.recovery_operation r WHERE r.id = c.command_id;
$$;

-- Owner-internal: the delivery_failure block of an order (see the header):
-- its latest failure, or NULL. times are the handover day's published
-- starts within shelf life while the choice is open (the API keeps those
-- after its clock); no database-clock read.
CREATE FUNCTION business.order_delivery_failure_view(p_order business.orders)
RETURNS jsonb
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  f business.delivery_failure%ROWTYPE;
  c business.delivery_failure_choice%ROWTYPE;
  r business.recovery_operation%ROWTYPE;
  v_pending boolean := business.order_command_unresolved(p_order.id);
  v_open boolean;
  v jsonb;
BEGIN
  SELECT * INTO f FROM business.delivery_failure x WHERE x.order_id = p_order.id
  ORDER BY x.created_at DESC, x.id DESC LIMIT 1;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  IF f.cause = 'nobody_available' THEN
    SELECT * INTO r FROM business.recovery_operation x WHERE x.id = f.no_show_command_id;
    v := pg_catalog.jsonb_build_object(
      'cause', f.cause, 'at', f.recorded_at,
      'state', CASE WHEN r.state <> 'secured' THEN 'processing' ELSE r.outcome ->> 'status' END,
      'open', false, 'pending', v_pending, 'choices', '[]'::jsonb, 'times', '[]'::jsonb, 'chosen', NULL);
    -- The no-show's money only once it is secured (K11).
    IF r.state = 'secured' AND r.outcome ->> 'status' = 'no_show' THEN
      v := v || pg_catalog.jsonb_build_object(
        'cake_refund_minor', (r.outcome -> 'evidence' ->> 'refund_target_minor')::bigint,
        'delivery_kept_minor', coalesce((r.outcome -> 'evidence' ->> 'delivery_kept_minor')::bigint, 0));
    END IF;
    RETURN v;
  END IF;
  v_open := f.id = business.delivery_failure_current(p_order.id) AND business.delivery_failure_open(p_order)
            AND NOT v_pending;
  SELECT * INTO c FROM business.delivery_failure_choice x WHERE x.failure_id = f.id
  ORDER BY x.created_at DESC, x.command_id DESC LIMIT 1;
  v := pg_catalog.jsonb_build_object(
    'cause', f.cause, 'at', f.recorded_at, 'state', 'recorded', 'open', v_open, 'pending', v_pending,
    'choices', CASE WHEN v_open THEN '["redelivery", "collection"]'::jsonb ELSE '[]'::jsonb END,
    'times', CASE WHEN v_open THEN business.delivery_choice_times(p_order) ELSE '[]'::jsonb END,
    'delivery_paid_minor', f.delivery_minor,
    'chosen', CASE WHEN c.command_id IS NOT NULL THEN business.delivery_failure_choice_json(c) END);
  IF business.delivery_failure_chosen(f.id, true) = 'redelivery' THEN
    v := v || pg_catalog.jsonb_build_object('redelivery_time_set', business.redelivery_time_set(f.id));
  END IF;
  RETURN v;
END;
$$;

-- Owner-internal: the staff copy of a delivery_failure block p_block of
-- order p_order for view p_view. chef: the block and every failure oldest
-- first with its reason, amounts and choices; technical support: the same
-- without money or the reason (K00 no money; K09 necessary support only);
-- fulfilment: the cause, state and the latest choice without money.
CREATE FUNCTION business.order_staff_delivery_failure(p_block jsonb, p_order uuid, p_view text)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE p_view
    WHEN 'fulfillment' THEN
      pg_catalog.jsonb_build_object(
        'cause', p_block -> 'cause', 'at', p_block -> 'at', 'state', p_block -> 'state', 'open', p_block -> 'open',
        'pending', p_block -> 'pending',
        'chosen', CASE WHEN pg_catalog.jsonb_typeof(p_block -> 'chosen') = 'object'
                       THEN (p_block -> 'chosen') - 'delivery_refund_minor' ELSE p_block -> 'chosen' END)
    ELSE
      (CASE WHEN p_view = 'chef' THEN p_block
            ELSE (p_block - ARRAY['delivery_paid_minor', 'cake_refund_minor', 'delivery_kept_minor'])
                 || pg_catalog.jsonb_build_object(
                      'chosen', CASE WHEN pg_catalog.jsonb_typeof(p_block -> 'chosen') = 'object'
                                     THEN (p_block -> 'chosen') - 'delivery_refund_minor' ELSE p_block -> 'chosen' END) END)
      || pg_catalog.jsonb_build_object(
           'history', (SELECT coalesce(pg_catalog.jsonb_agg(
                                pg_catalog.jsonb_build_object(
                                  'id', f.id, 'cause', f.cause, 'at', f.recorded_at, 'order_version', f.order_version,
                                  'dispatch_event_id', f.dispatch_event_id, 'quote_id', f.quote_id,
                                  'no_show_command_id', f.no_show_command_id,
                                  'choices', (SELECT coalesce(pg_catalog.jsonb_agg(
                                                       (CASE WHEN p_view = 'chef' THEN business.delivery_failure_choice_json(c)
                                                             ELSE business.delivery_failure_choice_json(c) - 'delivery_refund_minor' END)
                                                       || pg_catalog.jsonb_build_object(
                                                            'command_id', c.command_id,
                                                            'access', CASE WHEN c.actor_kind = 'guest' THEN 'guest_link' ELSE 'account' END,
                                                            'from', pg_catalog.jsonb_build_object(
                                                                      'fulfillment_mode', c.from_mode, 'date', c.from_day,
                                                                      'id', c.from_slot_id, 'starts_at', c.from_starts_at,
                                                                      'ends_at', c.from_ends_at))
                                                       ORDER BY c.created_at, c.command_id), '[]'::jsonb)
                                              FROM business.delivery_failure_choice c WHERE c.failure_id = f.id))
                                || CASE WHEN p_view = 'chef' THEN pg_catalog.jsonb_build_object(
                                     'reason', f.reason, 'delivery_collection_id', f.delivery_collection_id,
                                     'delivery_minor', f.delivery_minor) ELSE '{}'::jsonb END
                                ORDER BY f.created_at, f.id), '[]'::jsonb)
                       FROM business.delivery_failure f WHERE f.order_id = p_order))
  END;
$$;

-- ---------------------------------------------------------------- API

-- Owner-internal: a handover or no-show as a K11 order command (0032's
-- business.order_handover_request, now its one implementation). p_cause
-- nobody_available (with an optional staff-only p_reason) is the chef's
-- "delivery failed, nobody available" (Q18): a GK delivery on its way; it
-- also records the business.delivery_failure row and the cause in the
-- payload. Returns the command and whether it existed.
CREATE FUNCTION business.order_handover_enqueue(p_actor uuid, p_order uuid, p_key text, p_body_sha256 text,
                                                p_expected_version integer, p_action text, p_env text, p_now timestamptz,
                                                p_cause text, p_reason text)
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
  v_quote uuid;
  v_coll uuid;
  v_amount bigint;
BEGIN
  PERFORM business.fulfillment_check_input(p_key, p_body_sha256, p_expected_version, p_action, NULL, p_now);
  IF p_action NOT IN ('handover', 'no_show') OR p_order IS NULL THEN
    RAISE EXCEPTION 'action_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_cause IS NOT NULL AND (p_cause <> 'nobody_available' OR p_action <> 'no_show') THEN
    RAISE EXCEPTION 'failure_cause_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_reason IS NOT NULL AND (p_cause IS NULL OR p_reason <> pg_catalog.btrim(p_reason)
                               OR pg_catalog.char_length(p_reason) NOT BETWEEN 1 AND 500
                               OR p_reason ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') THEN
    RAISE EXCEPTION 'failure_reason_invalid' USING ERRCODE = 'GK422';
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
  IF p_cause IS NOT NULL AND v_o.fulfillment_mode <> 'gk_delivery' THEN
    RAISE EXCEPTION 'transition_not_allowed' USING ERRCODE = 'GK409';
  END IF;
  v_refusal := business.fulfillment_refusal(v_o, p_action, p_now);
  IF v_refusal IS NOT NULL THEN
    RAISE EXCEPTION '%', v_refusal USING ERRCODE = 'GK409';
  END IF;
  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  IF p_cause IS NULL THEN
    v_payload := pg_catalog.json_build_object(
      'v', 1, 'environment', p_env, 'command', 'order.' || p_action, 'operation_id', v_id, 'order_id', p_order,
      'prior_version', v_o.version,
      'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      'actor', pg_catalog.json_build_object('kind', 'staff', 'account_id', v_account),
      'fulfillment', pg_catalog.json_build_object('action', p_action, 'from', business.order_fulfillment_state(v_o),
                                                  'to', business.fulfillment_target(p_action),
                                                  'rate', CASE WHEN p_action = 'no_show' THEN 50 END),
      'order', business.order_recovery_snapshot(p_order))::text;
  ELSE
    v_quote := business.delivery_current_quote(p_order);
    IF v_quote IS NOT NULL THEN
      v_coll := business.delivery_paid_collection(v_quote);
    END IF;
    IF v_coll IS NOT NULL THEN
      SELECT pc.amount_minor INTO v_amount FROM business.payment_collection pc WHERE pc.id = v_coll;
    END IF;
    -- Q18: 50% of the cake; the paid delivery is kept.
    v_payload := pg_catalog.json_build_object(
      'v', 1, 'environment', p_env, 'command', 'order.' || p_action, 'operation_id', v_id, 'order_id', p_order,
      'prior_version', v_o.version,
      'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      'actor', pg_catalog.json_build_object('kind', 'staff', 'account_id', v_account),
      'fulfillment', pg_catalog.json_build_object('action', p_action, 'from', business.order_fulfillment_state(v_o),
                                                  'to', business.fulfillment_target(p_action), 'rate', 50,
                                                  'cause', p_cause, 'delivery_kept_collection_id', v_coll,
                                                  'delivery_kept_minor', v_amount),
      'order', business.order_recovery_snapshot(p_order))::text;
  END IF;
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
  IF p_cause IS NOT NULL THEN
    INSERT INTO business.delivery_failure (id, order_id, cause, dispatch_event_id, quote_id, delivery_collection_id,
                                           delivery_minor, reason, no_show_command_id, order_version, recorded_by,
                                           recorded_at)
    VALUES (pg_catalog.gen_random_uuid(), p_order, p_cause, business.order_last_dispatch(p_order), v_quote, v_coll,
            v_amount, p_reason, v_id, v_o.version, v_account, p_now);
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.' || p_action || '_requested', 'order', p_order, NULL,
                                CASE WHEN p_cause IS NULL THEN pg_catalog.json_build_object('operation_id', v_id)
                                     ELSE pg_catalog.json_build_object('operation_id', v_id, 'cause', p_cause) END);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- As in 0032, through business.order_handover_enqueue (no cause).
CREATE OR REPLACE FUNCTION business.order_handover_request(p_actor uuid, p_order uuid, p_key text, p_body_sha256 text,
                                                p_expected_version integer, p_action text, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  RETURN QUERY SELECT h.result_id, h.replayed
               FROM business.order_handover_enqueue(p_actor, p_order, p_key, p_body_sha256, p_expected_version, p_action,
                                                    p_env, p_now, NULL, NULL) h;
END;
$$;

-- API: the chef records that order p_order's GK delivery failed (see the
-- header): cause our_side (a plain versioned command; the result is the
-- failure) or nobody_available (the no-show K11 command; the result is the
-- command). Returns the result, whether it already existed (the same key)
-- and the cause.
CREATE FUNCTION business.order_delivery_failed(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_cause text, p_reason text,
  p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean, cause text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  v_frozen boolean;
  v_o business.orders%ROWTYPE;
  v_id uuid;
  v_quote uuid;
  v_coll uuid;
  v_amount bigint;
  v_dispatch uuid;
BEGIN
  PERFORM business.delivery_check_input(p_key, p_body_sha256, p_expected_version, p_now);
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_cause IS NULL OR p_cause NOT IN ('our_side', 'nobody_available') THEN
    RAISE EXCEPTION 'failure_cause_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_reason IS NOT NULL AND (p_reason <> pg_catalog.btrim(p_reason) OR pg_catalog.char_length(p_reason) NOT BETWEEN 1 AND 500
                               OR p_reason ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') THEN
    RAISE EXCEPTION 'failure_reason_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- The same key replays (the failure or the no-show command), also during
  -- a restore freeze.
  v_prior := business.command_claim(v_account, 'order.delivery_failed', p_order::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true, (SELECT f.cause FROM business.delivery_failure f
                                        WHERE f.id = v_prior OR f.no_show_command_id = v_prior);
    RETURN;
  END IF;
  IF p_cause = 'nobody_available' THEN
    -- Q18: the existing no-show command with its cause.
    SELECT h.result_id INTO v_id
    FROM business.order_handover_enqueue(p_actor, p_order, p_key, p_body_sha256, p_expected_version, 'no_show', p_env,
                                         p_now, 'nobody_available', p_reason) h;
    PERFORM business.command_finish(v_account, 'order.delivery_failed', p_order::text, p_key, v_id);
    RETURN QUERY SELECT v_id, false, 'nobody_available'::text;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  -- Lock order (K00): the order, its collections by ID.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND OR NOT business.order_fulfillment_scope(p_order) THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  IF business.order_command_unresolved(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured' OR NOT business.delivery_on_its_way(v_o) THEN
    RAISE EXCEPTION 'transition_not_allowed' USING ERRCODE = 'GK409';
  END IF;
  v_dispatch := business.order_last_dispatch(p_order);
  v_quote := business.delivery_current_quote(p_order);
  IF v_quote IS NOT NULL THEN
    v_coll := business.delivery_paid_collection(v_quote);
  END IF;
  IF v_coll IS NOT NULL THEN
    SELECT pc.amount_minor INTO v_amount FROM business.payment_collection pc WHERE pc.id = v_coll;
  END IF;
  v_id := pg_catalog.gen_random_uuid();
  INSERT INTO business.delivery_failure (id, order_id, cause, dispatch_event_id, quote_id, delivery_collection_id,
                                         delivery_minor, reason, order_version, recorded_by, recorded_at)
  VALUES (v_id, p_order, 'our_side', v_dispatch, v_quote, v_coll, v_amount, p_reason, v_o.version, v_account, p_now);
  INSERT INTO business.fulfillment_event (id, order_id, action, from_state, to_state, actor_account_id, received_at)
  VALUES (pg_catalog.gen_random_uuid(), p_order, 'delivery_failed', business.order_fulfillment_state(v_o), 'exception',
          v_account, p_now);
  UPDATE business.orders o SET fulfillment_state = 'exception', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = p_order;
  PERFORM business.audit_record('staff', v_account, 'delivery.failed', 'order', p_order, NULL,
                                pg_catalog.json_build_object('failure_id', v_id, 'cause', 'our_side', 'quote_id', v_quote,
                                                             'delivery_collection_id', v_coll));
  PERFORM business.command_finish(v_account, 'order.delivery_failed', p_order::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false, 'our_side'::text;
END;
$$;

-- API: the customer's choice after an our-side failure on order p_order
-- (see the header): the K11 command, its choice row, the job and the audit.
-- Returns the command (the recovery operation) and whether it existed.
CREATE FUNCTION business.order_delivery_failure_choice_request(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_choice text, p_target_slot uuid, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_l record;
  v_o business.orders%ROWTYPE;
  v_kind text;
  v_prior business.delivery_failure_choice%ROWTYPE;
  v_frozen boolean;
  v_failure uuid;
  v_quote uuid;
  v_paid uuid;
  v_to_slot uuid;
  v_to_starts timestamptz;
  v_to_ends timestamptz;
  v_revision integer;
  v_from_starts timestamptz;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
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
  IF p_choice IS NULL OR p_choice NOT IN ('redelivery', 'collection') THEN
    RAISE EXCEPTION 'failure_choice_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF (p_choice = 'collection') <> (p_target_slot IS NOT NULL) THEN
    RAISE EXCEPTION 'failure_target_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  -- The same-day move's access for both choices: the owner, or a guest
  -- capability with "move_pickup".
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order, 'move_pickup') l;
  v_o := v_l.o_order;
  v_kind := CASE WHEN v_l.o_account IS NULL THEN 'guest' ELSE 'customer' END;
  -- The same actor and key replay the recorded choice, also while frozen.
  SELECT * INTO v_prior FROM business.delivery_failure_choice c
  WHERE c.order_id = p_order AND c.idempotency_key = p_key
    AND c.actor_account_id IS NOT DISTINCT FROM v_l.o_account AND c.guest_capability_id IS NOT DISTINCT FROM v_l.o_capability;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior.command_id, true;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  -- Lock order (K00): the order (held), its collections by ID.
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  IF business.order_command_unresolved(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF NOT business.delivery_failure_open(v_o) THEN
    RAISE EXCEPTION 'choice_not_open' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  v_failure := business.delivery_failure_current(p_order);
  v_from_starts := business.order_handover_at(v_o);
  IF p_choice = 'collection' THEN
    -- Q10 / state 19: an opening time of the failed delivery's day still
    -- ahead (after the receipt), no six-hour cutoff.
    SELECT s.slot_id, s.starts_at, s.ends_at, s.schedule_revision INTO v_to_slot, v_to_starts, v_to_ends, v_revision
    FROM business.schedule_slots_on(v_o.handover_day) s WHERE s.slot_id = p_target_slot;
    IF v_to_slot IS NULL OR v_to_starts <= p_now OR NOT business.order_lots_last_until(p_order, v_to_ends) THEN
      RAISE EXCEPTION 'slot_unavailable' USING ERRCODE = 'GK409';
    END IF;
  END IF;
  v_quote := business.delivery_current_quote(p_order);
  IF v_quote IS NOT NULL THEN
    v_paid := business.delivery_paid_collection(v_quote);
  END IF;

  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.delivery_failure_choice', 'operation_id', v_id, 'order_id', p_order,
    'prior_version', v_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', v_kind, 'account_id', v_l.o_account),
    'delivery_failure_choice', pg_catalog.json_build_object(
      'choice', p_choice, 'failure_id', v_failure, 'quote_id', v_quote, 'delivery_collection_id', v_paid,
      'from', pg_catalog.json_build_object('fulfillment_mode', v_o.fulfillment_mode, 'date', v_o.handover_day,
                                           'slot_id', v_o.slot_id, 'starts_at', v_from_starts, 'ends_at', v_o.handover_ends_at),
      'to', CASE WHEN p_choice = 'collection' THEN pg_catalog.json_build_object(
              'fulfillment_mode', 'collection', 'date', v_o.handover_day, 'slot_id', v_to_slot, 'starts_at', v_to_starts,
              'ends_at', v_to_ends) END,
      'schedule_revision', v_revision),
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
  INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, guest_capability_id,
                                      idempotency_key, body_sha256, prior_version, received_at, payload, payload_sha256,
                                      envelope_key, job_id)
  VALUES (v_id, p_order, 'delivery_failure_choice', v_kind, v_l.o_account, v_l.o_capability, p_key, p_body_sha256,
          v_o.version, p_now, v_payload, v_hash, v_key, v_job);
  INSERT INTO business.delivery_failure_choice (command_id, order_id, failure_id, choice, delivery_collection_id, from_mode,
                                                from_day, from_slot_id, from_starts_at, from_ends_at, to_slot_id,
                                                to_starts_at, to_ends_at, schedule_revision, actor_kind, actor_account_id,
                                                guest_capability_id, idempotency_key, body_sha256, order_version,
                                                received_at)
  VALUES (v_id, p_order, v_failure, p_choice, v_paid, v_o.fulfillment_mode, v_o.handover_day, v_o.slot_id, v_from_starts,
          v_o.handover_ends_at, v_to_slot, v_to_starts, v_to_ends, v_revision, v_kind, v_l.o_account, v_l.o_capability,
          p_key, p_body_sha256, v_o.version, p_now);
  PERFORM business.audit_record(CASE v_kind WHEN 'guest' THEN 'system' ELSE v_kind END, v_l.o_account,
                                'order.delivery_failure_choice_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id, 'choice', p_choice,
                                                             'failure_id', v_failure));
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- ------------------------------------------------------------- commit

-- Owner-internal: the domain commit of a prepared delivery_failure_choice
-- command (the caller, business.order_command_apply, holds the order, its
-- collections and the recovery operation and has checked the freeze; see
-- the header). Returns the outcome text.
CREATE FUNCTION business.order_delivery_failure_choice_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  c business.delivery_failure_choice%ROWTYPE;
  f business.delivery_failure%ROWTYPE;
  v_quote uuid;
  v_paid uuid;
  v_amount bigint;
  v_reason text;
  v_actor text := CASE v.actor_kind WHEN 'guest' THEN 'system' ELSE v.actor_kind END;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  SELECT * INTO c FROM business.delivery_failure_choice x WHERE x.command_id = v.id;
  SELECT * INTO f FROM business.delivery_failure x WHERE x.id = c.failure_id;
  v_quote := business.delivery_current_quote(v.order_id);
  IF v_quote IS NOT NULL THEN
    v_paid := business.delivery_paid_collection(v_quote);
  END IF;
  IF v_o.version <> v.prior_version OR v_o.recovery_state <> 'secured' OR v_paid IS DISTINCT FROM c.delivery_collection_id THEN
    v_reason := 'order_changed';
  ELSIF business.delivery_failure_current(v.order_id) IS DISTINCT FROM c.failure_id
        OR NOT business.delivery_failure_open(v_o) THEN
    v_reason := 'not_open';
  ELSIF c.choice = 'collection'
        AND (NOT EXISTS (SELECT 1 FROM business.schedule_slots_on(c.from_day) s
                         WHERE s.slot_id = c.to_slot_id AND s.starts_at = c.to_starts_at)
             -- Decided by the persisted receipt instant (K00), as every order
             -- command, also when a restore freeze delayed the commit.
             OR c.to_starts_at <= v.received_at
             OR NOT business.order_lots_last_until(v.order_id, c.to_ends_at)) THEN
    v_reason := 'slot_unavailable';
  END IF;
  IF v_reason IS NOT NULL THEN
    -- Nothing changes but the version: the choice is open again.
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.delivery_failure_choice_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason));
    RETURN pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', v_reason);
  END IF;

  IF c.choice = 'redelivery' THEN
    -- The order stays a GK delivery at its time; the chef agrees the new
    -- time (0031); nothing more to pay and nothing refunded.
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record(v_actor, v.actor_account_id, 'order.delivery_failure_choice_made', 'order', v.order_id,
                                  NULL, pg_catalog.json_build_object('operation_id', v.id, 'choice', c.choice,
                                                                     'failure_id', c.failure_id));
    RETURN pg_catalog.json_build_object(
      'status', 'chosen',
      'evidence', pg_catalog.json_build_object('order_version', v_o.version + 1, 'choice', c.choice))::text;
  END IF;

  -- Collection: the explicit, agreed mode and time change (K06): same day
  -- and place; the original handover, refund deadline and production
  -- anchor stay.
  UPDATE business.orders o
  SET fulfillment_mode = 'collection', slot_id = c.to_slot_id, handover_starts_at = c.to_starts_at,
      handover_ends_at = c.to_ends_at, version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = v.order_id;
  IF c.delivery_collection_id IS NOT NULL THEN
    -- K05: the GK-caused unperformed delivery's 100% floor, recorded by the
    -- chef who recorded the failure; the goal combines it and the refund is
    -- reserved (0022's delivery-failure floor).
    SELECT pc.amount_minor INTO v_amount FROM business.payment_collection pc WHERE pc.id = c.delivery_collection_id;
    IF NOT EXISTS (SELECT 1 FROM business.refund_floor rf WHERE rf.collection_id = c.delivery_collection_id) THEN
      INSERT INTO business.refund_floor (id, order_id, collection_id, floor_minor, reason, recorded_by)
      VALUES (pg_catalog.gen_random_uuid(), v.order_id, c.delivery_collection_id, v_amount, 'gk_delivery_failure',
              f.recorded_by);
      PERFORM business.audit_record(v_actor, v.actor_account_id, 'refund.floor_recorded', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('collection_id', c.delivery_collection_id,
                                                                 'floor_minor', v_amount, 'failure_id', c.failure_id));
    END IF;
    PERFORM business.refund_goal_apply(v.order_id, NULL, NULL, NULL, NULL, NULL, v_actor, v.actor_account_id);
    PERFORM business.refund_reserve(v.order_id);
  END IF;
  PERFORM business.audit_record(v_actor, v.actor_account_id, 'order.delivery_failure_choice_made', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'choice', c.choice,
                                                             'failure_id', c.failure_id, 'delivery_refund_minor', v_amount));
  RETURN pg_catalog.json_build_object(
    'status', 'chosen',
    'evidence', pg_catalog.json_build_object('order_version', v_o.version + 1, 'choice', c.choice,
                                             'to_slot_id', c.to_slot_id, 'delivery_refund_minor', v_amount))::text;
END;
$$;

-- As in 0032, plus a nobody-available no-show (Q18; the delivery_failure row
-- of the command): the paid delivery is kept (business.delivery_kept, unless
-- the chef recorded a floor on it) and the 50% goal is of the cake only
-- (the paid aggregate without the kept delivery); the outcome states the
-- cause and the delivery kept. Every no-show's outcome also states a stray
-- delivery payment returned with it (unmatched_minor, only when there is
-- one). Every other handover and no-show is as before.
CREATE OR REPLACE FUNCTION business.order_handover_commit(v business.order_command)
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
  f business.delivery_failure%ROWTYPE;
  v_kept bigint := 0;
  v_stray bigint := 0;
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
  SELECT * INTO f FROM business.delivery_failure x WHERE x.no_show_command_id = v.id;
  IF f.id IS NOT NULL AND f.delivery_collection_id IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM business.refund_floor rf WHERE rf.collection_id = f.delivery_collection_id) THEN
    -- Q18: the delivery was attempted, so its payment is kept.
    INSERT INTO business.delivery_kept (collection_id, order_id, failure_id, amount_minor)
    VALUES (f.delivery_collection_id, v.order_id, f.id, f.delivery_minor);
    v_kept := f.delivery_minor;
  END IF;
  -- No-show (K05): 50% of the actual paid aggregate (Q18: without a kept
  -- delivery), a policy goal (the cumulative order obligation; earlier
  -- returns count) and its refunds. What the customer was already returned
  -- or has on its way (S + R over every collection) before and after: the
  -- email states only what this no-show newly reserved.
  v_paid := business.order_paid_minor(v.order_id) - v_kept;
  v_target := business.cancellation_amount(v_paid, 50::smallint);
  SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_before
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b WHERE pc.order_id = v.order_id;
  -- A stray delivery payment (0035) not returned yet: the goal returns it in
  -- full beside the 50%, so the outcome (and the notice) states it.
  v_stray := CASE WHEN v_target > 0 THEN business.delivery_unmatched_minor(v.order_id) ELSE 0 END;
  IF v_target > 0 THEN
    PERFORM business.refund_goal_apply(v.order_id, 'no_show', 'chef', 50::smallint, v_paid, v_target, 'staff',
                                       v.actor_account_id);
    PERFORM business.refund_reserve(v.order_id);
  END IF;
  SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_after
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b WHERE pc.order_id = v.order_id;
  IF f.id IS NOT NULL THEN
    PERFORM business.audit_record('staff', v.actor_account_id, 'order.no_show', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'rate', 50, 'refund_target_minor', v_target,
                                                               'new_refund_minor', v_after - v_before,
                                                               'cause', f.cause, 'delivery_kept_minor', v_kept));
    RETURN pg_catalog.format(
      '{"status":"no_show","evidence":{"order_version":%s,"rate":50,"paid_minor":%s,"refund_target_minor":%s,'
      '"returned_before_minor":%s,"new_refund_minor":%s,"cause":"%s","delivery_kept_minor":%s%s}}',
      v_o.version + 1, v_paid, v_target, v_before, v_after - v_before, f.cause, v_kept,
      CASE WHEN v_stray > 0 THEN ',"unmatched_minor":' || v_stray::text ELSE '' END);
  END IF;
  PERFORM business.audit_record('staff', v.actor_account_id, 'order.no_show', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'rate', 50, 'refund_target_minor', v_target,
                                                             'new_refund_minor', v_after - v_before));
  RETURN pg_catalog.format(
    '{"status":"no_show","evidence":{"order_version":%s,"rate":50,"paid_minor":%s,"refund_target_minor":%s,'
    '"returned_before_minor":%s,"new_refund_minor":%s%s}}',
    v_o.version + 1, v_paid, v_target, v_before, v_after - v_before,
    CASE WHEN v_stray > 0 THEN ',"unmatched_minor":' || v_stray::text ELSE '' END);
END;
$$;

-- ------------------------------------------------- replaced functions

-- As in 0036, plus: a dispatch after an our-side failure of the latest
-- dispatch waits for a secured redelivery choice and the chef's new time
-- (409 delivery_failure_open / redelivery_time_not_set), and a GK delivery
-- no-show needs a delivery on its way (not a dispatch that failed on our
-- side).
CREATE OR REPLACE FUNCTION business.fulfillment_refusal(p_o business.orders, p_action text, p_at timestamptz)
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
           WHEN EXISTS (SELECT 1 FROM business.delivery_not_possible n WHERE n.order_id = p_o.id) THEN 'delivery_not_possible'
           WHEN business.delivery_failure_refusal(p_o) IS NOT NULL THEN business.delivery_failure_refusal(p_o)
           WHEN NOT business.delivery_settled(p_o.id) THEN 'delivery_not_paid' END
    WHEN p_action = 'delivery_exception' THEN
      CASE WHEN business.order_fulfillment_state(p_o) IN ('awaiting_handover', 'out_for_delivery', 'exception') THEN NULL
           ELSE 'transition_not_allowed' END
    WHEN p_action = 'handover' THEN
      CASE WHEN (p_o.fulfillment_mode = 'gk_delivery' AND business.order_fulfillment_state(p_o) = 'out_for_delivery')
                OR (p_o.fulfillment_mode <> 'gk_delivery'
                    AND business.order_fulfillment_state(p_o) IN ('awaiting_handover', 'exception')) THEN NULL
           ELSE 'transition_not_allowed' END
    WHEN p_action = 'no_show' THEN
      CASE WHEN (p_o.fulfillment_mode <> 'gk_delivery'
                 AND business.order_fulfillment_state(p_o) NOT IN ('awaiting_handover', 'exception'))
                OR (p_o.fulfillment_mode = 'gk_delivery' AND NOT business.delivery_on_its_way(p_o))
             THEN 'transition_not_allowed'
           WHEN p_at < business.order_handover_at(p_o) THEN 'handover_not_due' END
    ELSE 'transition_not_allowed' END
$$;

-- As in 0032, and not while a GK delivery is on its way (an exception after
-- a dispatch: the chef records the failure's cause first, then a redelivery
-- time after the customer chose it; K06, Q17/Q18) or the customer's choice
-- after an our-side failure is open. STABLE now: it reads the failure.
CREATE OR REPLACE FUNCTION business.appointment_change_eligible(p_order business.orders)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_order.lifecycle IN ('confirmed', 'in_production', 'ready')
         AND coalesce(p_order.fulfillment_state, 'awaiting_handover') IN ('awaiting_handover', 'exception')
         AND NOT business.delivery_failure_open(p_order)
         AND NOT business.delivery_on_its_way(p_order)
$$;

-- As in 0036, plus a choice after a delivery failure that is not secured.
CREATE OR REPLACE FUNCTION business.order_refund_hidden(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT business.order_handover_pending(p_order)
      OR EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                 WHERE c.order_id = p_order AND c.command IN ('delivery_choice', 'delivery_failure_choice')
                   AND r.state <> 'secured');
$$;

-- As in 0036, plus an unresolved choice after a delivery failure
-- (production and the chef's refunds wait for it).
CREATE OR REPLACE FUNCTION business.order_terminal_pending(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                 WHERE c.order_id = p_order
                   AND c.command IN ('cancel', 'late_payment', 'pickup_move', 'appointment_change', 'delivery_choice',
                                     'delivery_failure_choice')
                   AND r.state <> 'secured');
$$;

-- As in 0036, plus the GK delivery a committed but unsecured collection
-- choice after a delivery failure replaces (K11).
CREATE OR REPLACE FUNCTION business.order_visible_mode(p_order business.orders)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce((SELECT dc.from_mode FROM business.delivery_choice dc
                   JOIN business.recovery_operation r ON r.id = dc.command_id
                   WHERE dc.order_id = p_order.id AND r.state = 'committed' AND r.outcome ->> 'status' = 'chosen'
                   ORDER BY dc.created_at DESC, dc.command_id DESC LIMIT 1),
                  (SELECT fc.from_mode FROM business.delivery_failure_choice fc
                   JOIN business.recovery_operation r ON r.id = fc.command_id
                   WHERE fc.order_id = p_order.id AND fc.choice = 'collection' AND r.state = 'committed'
                     AND r.outcome ->> 'status' = 'chosen'
                   ORDER BY fc.created_at DESC, fc.command_id DESC LIMIT 1),
                  p_order.fulfillment_mode);
$$;

-- As in 0036, plus the delivery_failure_choice command
-- (business.order_delivery_failure_choice_commit; it locks the collections
-- first: a collection may record a floor and reserve the delivery refund).
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
  IF v.command IN ('paid_confirm', 'cancel', 'late_payment', 'manual_payout', 'manual_payout_outcome', 'no_show',
                   'delivery_choice', 'delivery_failure_choice') THEN
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
                   'handover', 'no_show', 'delivery_choice', 'delivery_failure_choice') THEN
    v_outcome := CASE v.command WHEN 'cancel' THEN business.order_cancel_commit(v)
                                WHEN 'delivery_choice' THEN business.order_delivery_choice_commit(v)
                                WHEN 'delivery_failure_choice' THEN business.order_delivery_failure_choice_commit(v)
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
-- As in 0036, and while a collection choice after a delivery failure is
-- committed but its outcome is not secured, the GK delivery appointment it
-- replaces (K11).
CREATE OR REPLACE FUNCTION business.order_visible_appointment(p_order business.orders)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(
    (SELECT pg_catalog.jsonb_build_object('date', c.from_day, 'id', c.from_slot_id, 'starts_at', c.from_starts_at,
                                          'ends_at', c.from_ends_at)
     FROM business.pickup_change c JOIN business.recovery_operation r ON r.id = c.command_id
     WHERE c.order_id = p_order.id AND r.state = 'committed' AND r.outcome ->> 'status' = 'moved'
     ORDER BY c.created_at DESC, c.command_id DESC LIMIT 1),
    (SELECT pg_catalog.jsonb_build_object('date', a.from_day, 'id', a.from_slot_id, 'starts_at', a.from_starts_at,
                                          'ends_at', a.from_ends_at)
     FROM business.late_payment_answer a
     JOIN business.late_payment_offer f ON f.id = a.offer_id
     JOIN business.recovery_operation r ON r.id = a.command_id
     WHERE a.order_id = p_order.id AND f.kind = 'other' AND r.state = 'committed' AND r.outcome ->> 'status' = 'confirmed'
     ORDER BY a.created_at DESC, a.id DESC LIMIT 1),
    (SELECT pg_catalog.jsonb_build_object('date', dc.from_day, 'id', dc.from_slot_id, 'starts_at', dc.from_starts_at,
                                          'ends_at', dc.from_ends_at)
     FROM business.delivery_choice dc JOIN business.recovery_operation r ON r.id = dc.command_id
     WHERE dc.order_id = p_order.id AND r.state = 'committed' AND r.outcome ->> 'status' = 'chosen'
     ORDER BY dc.created_at DESC, dc.command_id DESC LIMIT 1),
    (SELECT pg_catalog.jsonb_build_object('date', fc.from_day, 'id', fc.from_slot_id, 'starts_at', fc.from_starts_at,
                                          'ends_at', fc.from_ends_at)
     FROM business.delivery_failure_choice fc JOIN business.recovery_operation r ON r.id = fc.command_id
     WHERE fc.order_id = p_order.id AND fc.choice = 'collection' AND r.state = 'committed'
       AND r.outcome ->> 'status' = 'chosen'
     ORDER BY fc.created_at DESC, fc.command_id DESC LIMIT 1),
    pg_catalog.jsonb_build_object('date', p_order.handover_day, 'id', p_order.slot_id,
                                  'starts_at', business.order_handover_at(p_order), 'ends_at', p_order.handover_ends_at));
$$;
-- As in 0036, with the visible fulfilment mode (a committed but unsecured
-- collection choice after a delivery failure still reads as a GK delivery).
CREATE OR REPLACE FUNCTION business.order_delivery_choice_view(p_order business.orders)
RETURNS jsonb
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  c business.delivery_choice%ROWTYPE;
  v_reason text := business.delivery_choice_basis(p_order, pg_catalog.now());
  v_opens timestamptz := business.delivery_choice_opens_at(p_order);
  v_pending boolean := business.order_command_unresolved(p_order.id);
  v_open boolean;
BEGIN
  SELECT * INTO c FROM business.delivery_choice x WHERE x.order_id = p_order.id
  ORDER BY x.created_at DESC, x.command_id DESC LIMIT 1;
  IF business.order_visible_mode(p_order) <> 'gk_delivery' AND c.command_id IS NULL THEN
    RETURN NULL;
  END IF;
  v_open := v_reason IS NOT NULL AND NOT v_pending;
  RETURN pg_catalog.jsonb_build_object(
    'open', v_open, 'reason', v_reason, 'opens_at', v_opens, 'pending', v_pending,
    'choices', CASE WHEN v_open THEN '["collection", "customer_courier", "full_refund"]'::jsonb ELSE '[]'::jsonb END,
    'times', CASE WHEN v_reason IS NOT NULL OR v_opens IS NOT NULL
                  THEN business.delivery_choice_times(p_order) ELSE '[]'::jsonb END,
    'chosen', CASE WHEN c.command_id IS NOT NULL THEN business.delivery_choice_json(c) END);
END;
$$;
-- As in 0036, and a collection chosen after a delivery failure reads like a
-- Q5 time choice: once secured the block's state is cancelled_by_choice (the
-- delivery is not happening; a paid one keeps paid_minor and its full refund
-- is in the refund block).
CREATE OR REPLACE FUNCTION business.order_delivery_view(p_order business.orders)
RETURNS jsonb
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  q business.delivery_quote%ROWTYPE;
  w business.delivery_waiver%ROWTYPE;
  l business.payment_collection%ROWTYPE;
  v_paid uuid;
  v_state text;
  v jsonb;
  v_chosen text;
BEGIN
  SELECT r.state INTO v_chosen
  FROM (SELECT dc.command_id, dc.created_at FROM business.delivery_choice dc WHERE dc.order_id = p_order.id
        UNION ALL
        SELECT fc.command_id, fc.created_at FROM business.delivery_failure_choice fc
        WHERE fc.order_id = p_order.id AND fc.choice = 'collection') x
  JOIN business.recovery_operation r ON r.id = x.command_id
  WHERE r.state IN ('committed', 'secured') AND r.outcome ->> 'status' = 'chosen'
  ORDER BY x.created_at DESC, x.command_id DESC LIMIT 1;
  IF p_order.fulfillment_mode <> 'gk_delivery' AND v_chosen IS NULL THEN
    RETURN NULL;
  END IF;
  SELECT * INTO q FROM business.delivery_quote x WHERE x.id = business.delivery_current_quote(p_order.id);
  IF q.id IS NULL THEN
    RETURN pg_catalog.jsonb_build_object('state', CASE WHEN v_chosen = 'secured' THEN 'cancelled_by_choice'
                                                       ELSE 'awaiting_quote' END,
                                         'quote', NULL, 'payable', false, 'payment', NULL)
           || business.order_delivery_unmatched(p_order.id);
  END IF;
  SELECT * INTO w FROM business.delivery_waiver x WHERE x.quote_id = q.id;
  v_paid := business.delivery_paid_collection(q.id);
  SELECT * INTO l FROM business.payment_collection pc
  WHERE pc.order_id = p_order.id AND pc.kind = 'delivery' AND pc.delivery_quote_id = q.id
  ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1;
  v_state := CASE WHEN w.id IS NOT NULL THEN 'waived'
                  WHEN v_paid IS NOT NULL THEN 'paid'
                  WHEN business.delivery_in_flight(p_order.id) THEN 'payment_checking'
                  WHEN pg_catalog.now() >= q.pay_by THEN 'pay_by_passed'
                  ELSE 'quoted' END;
  IF v_chosen = 'secured' THEN
    v_state := 'cancelled_by_choice';
  END IF;
  v := pg_catalog.jsonb_build_object(
    'state', v_state,
    'quote', pg_catalog.jsonb_build_object('id', q.id, 'amount_minor', q.amount_minor, 'currency', q.currency,
                                           'quoted_at', q.quoted_at, 'pay_by', q.pay_by),
    'payable', v_state IN ('quoted', 'pay_by_passed') AND business.delivery_order_open(p_order),
    'payment', CASE WHEN l.id IS NOT NULL THEN pg_catalog.jsonb_build_object(
                 'id', l.id,
                 'state', CASE WHEN l.state = 'cancelled' AND l.mismatch_reason IS NOT NULL THEN 'unknown' ELSE l.state END,
                 'amount_minor', l.amount_minor, 'currency', l.currency, 'requested_at', l.initiated_at,
                 'checkout_url', CASE WHEN l.state = 'pending' AND l.session_status = 'open'
                                           AND l.provider_expires_at > pg_catalog.now() AND l.expire_requested_at IS NULL
                                           AND l.mismatch_reason IS NULL AND business.delivery_order_open(p_order)
                                      THEN l.checkout_url END,
                 'expires_at', l.provider_expires_at) END);
  IF v_paid IS NOT NULL THEN
    v := v || pg_catalog.jsonb_build_object('paid_minor', (SELECT pc.amount_minor FROM business.payment_collection pc
                                                            WHERE pc.id = v_paid));
  END IF;
  IF w.id IS NOT NULL THEN
    v := v || pg_catalog.jsonb_build_object('waived', pg_catalog.jsonb_build_object('at', w.approved_at));
  END IF;
  RETURN v || business.order_delivery_unmatched(p_order.id);
END;
$$;
-- As in 0036, plus delivery_failure (the latest failed GK delivery; see the
-- header).
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
           'placed_at', o.placed_at, 'date', (va.a ->> 'date')::date, 'fulfillment_mode', business.order_visible_mode(o),
           'slot', va.a - 'date',
           'fulfillment', business.order_visible_fulfillment(o),
           'pickup_move_until', CASE WHEN business.order_visible_mode(o) IN ('collection', 'customer_courier')
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
           'cancellation', (SELECT CASE WHEN r.state = 'secured' AND (r.outcome -> 'evidence' ->> 'unmatched_minor') IS NOT NULL
                              -- A stray delivery payment returned in full beside
                              -- amount_minor (0035), only when the outcome states
                              -- it (other cancellations render as in 0034).
                              THEN pg_catalog.json_build_object(
                                'state', r.outcome ->> 'status',
                                'reason', r.outcome -> 'evidence' ->> 'reason',
                                'rate', (r.outcome -> 'evidence' ->> 'rate')::integer,
                                'amount_minor', (r.outcome -> 'evidence' ->> 'refund_target_minor')::bigint,
                                'unmatched_minor', (r.outcome -> 'evidence' ->> 'unmatched_minor')::bigint,
                                'requested_by', CASE c.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
                                'requested_at', c.received_at)
                              ELSE pg_catalog.json_build_object(
                              'state', CASE WHEN r.state <> 'secured' THEN 'processing'
                                            ELSE r.outcome ->> 'status' END,
                              'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                              'rate', CASE WHEN r.state = 'secured' THEN (r.outcome -> 'evidence' ->> 'rate')::integer END,
                              'amount_minor', CASE WHEN r.state = 'secured'
                                                   THEN (r.outcome -> 'evidence' ->> 'refund_target_minor')::bigint END,
                              'requested_by', CASE c.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
                              'requested_at', c.received_at) END
                            FROM business.order_command c
                            JOIN business.recovery_operation r ON r.id = c.id
                            LEFT JOIN business.late_payment_decision d ON d.command_id = c.id
                            LEFT JOIN business.delivery_choice dc ON dc.command_id = c.id
                            WHERE c.order_id = o.id AND (c.command = 'cancel' OR d.decision = 'full_return'
                                                         OR dc.choice = 'full_refund')
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
                       WHERE pc.order_id = o.id AND pc.kind = 'cake'
                       ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1),
           'delivery', business.order_delivery_view(o),
           'delivery_choice', business.order_delivery_choice_view(o),
           'delivery_failure', business.order_delivery_failure_view(o),
           'refund', CASE WHEN o.recovery_state <> 'cancellation_pending' AND NOT business.order_refund_hidden(o.id)
                          THEN business.order_refund_summary(o.id) END,
           'late_payment', business.order_late_payment_view(o),
           'agreed', business.order_late_payment_agreed(o.id),
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
-- As in 0036, plus delivery_failure (business.order_staff_delivery_failure:
-- the chef also sees every failure with its reason and money, technical
-- support the same without money or reason, fulfilment the state) on orders
-- whose customer view has the block.
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
  v_delivery jsonb;
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
  v_delivery := v -> 'delivery';
  IF v_view = 'chef' THEN
    v_result := (v #- '{payment,checkout_url}' #- '{delivery,payment,checkout_url}')
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
                     'late_payment_offers', business.order_staff_late_payment_offers(o.id),
                     'audit', business.order_staff_audit(o.id))
                || CASE WHEN pg_catalog.jsonb_typeof(v_delivery) = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_history', business.order_staff_delivery(o.id, true))
                        ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_choice') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_choice',
                               business.order_staff_delivery_choice(v -> 'delivery_choice', o.id, 'chef'))
                        ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_failure') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_failure',
                               business.order_staff_delivery_failure(v -> 'delivery_failure', o.id, 'chef'))
                        ELSE '{}'::jsonb END;
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
                                       THEN (v -> 'cancellation') - ARRAY['amount_minor', 'rate', 'unmatched_minor']
                                       ELSE v -> 'cancellation' END,
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
                  -- No money for technical support: the refund amount shown to
                  -- the customer is dropped.
                  'late_payment', CASE WHEN pg_catalog.jsonb_typeof(v -> 'late_payment') = 'object'
                                       THEN (v -> 'late_payment') - ARRAY['paid_minor', 'currency']
                                       ELSE v -> 'late_payment' END,
                  'agreed', v -> 'agreed',
                  'late_payment_offers', business.order_staff_late_payment_offers(o.id),
                  'audit', business.order_staff_audit(o.id))
                || CASE WHEN pg_catalog.jsonb_typeof(v_delivery) = 'object' THEN pg_catalog.jsonb_build_object(
                     -- Delivery without money (amounts, currency, payment link).
                     'delivery', (v_delivery - ARRAY['paid_minor', 'unmatched_minor', 'quote', 'payment'])
                                 || pg_catalog.jsonb_build_object(
                                      'quote', CASE WHEN pg_catalog.jsonb_typeof(v_delivery -> 'quote') = 'object'
                                                    THEN (v_delivery -> 'quote') - ARRAY['amount_minor', 'currency']
                                                    ELSE v_delivery -> 'quote' END,
                                      'payment', CASE WHEN pg_catalog.jsonb_typeof(v_delivery -> 'payment') = 'object'
                                                      THEN (v_delivery -> 'payment') - ARRAY['amount_minor', 'currency', 'checkout_url']
                                                      ELSE v_delivery -> 'payment' END),
                     'delivery_history', business.order_staff_delivery(o.id, false))
                   ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_choice') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_choice',
                               business.order_staff_delivery_choice(v -> 'delivery_choice', o.id, 'technical_support'))
                        ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_failure') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_failure',
                               business.order_staff_delivery_failure(v -> 'delivery_failure', o.id, 'technical_support'))
                        ELSE '{}'::jsonb END;
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
                  'fulfillment_history', business.order_staff_fulfillment(o.id, true))
                -- Fulfilment: whether a GK delivery may be dispatched, nothing else.
                -- pay_by is internal: the API decides quoted / pay_by_passed at its
                -- own clock from it and removes it (withDeliveryClock), so the
                -- state never follows the database's real clock.
                || CASE WHEN pg_catalog.jsonb_typeof(v_delivery) = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery',
                               pg_catalog.jsonb_build_object('state', v_delivery -> 'state')
                               || CASE WHEN pg_catalog.jsonb_typeof(v_delivery -> 'quote') = 'object'
                                       THEN pg_catalog.jsonb_build_object('pay_by', v_delivery -> 'quote' -> 'pay_by')
                                       ELSE '{}'::jsonb END)
                        ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_choice') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_choice',
                               business.order_staff_delivery_choice(v -> 'delivery_choice', o.id, 'fulfillment'))
                        ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_failure') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_failure',
                               business.order_staff_delivery_failure(v -> 'delivery_failure', o.id, 'fulfillment'))
                        ELSE '{}'::jsonb END;
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.staff_viewed', 'order', o.id, NULL,
                                pg_catalog.json_build_object('view', v_view));
  RETURN v_result::json;
END;
$$;
-- As in 0036, plus the no_show notice of a nobody-available GK delivery
-- (0037, Q18): the 50% is of the cake price and the delivery payment is kept
-- (wording for Creator review); and every no-show notice whose outcome
-- states a stray delivery payment returned with it (unmatched_minor) says so
-- with the cancelled notice's sentence, its refund line stating the 50% part.
-- Every other notice renders exactly as before.
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
  v_stray bigint;
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
      WHEN 'delivery_not_possible' THEN
        'We are sorry that we could not deliver your order. As you chose, it has been cancelled'
        || CASE WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN '.'
                WHEN coalesce((p_facts ->> 'refund_returned')::boolean, false) THEN ' and everything you paid has been returned.'
                ELSE ' and everything you paid is being returned.' END
      ELSE 'Your order has been cancelled as you asked.' END;
    v_returned := coalesce((p_facts ->> 'refund_returned')::boolean, false);
    v_text := v_text || CASE
      WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN 'There was no payment for this order, so there is nothing to refund.'
      WHEN v_target = 0 THEN 'No refund is due for this order.'
      ELSE CASE WHEN v_returned THEN 'Refund: ' ELSE 'Refund requested: ' END || business.notice_money(v_target)
           || CASE WHEN v_rate = 100 THEN ', the full amount you paid.'
                   -- After "GK can't deliver" (0036): the rate on the cakes and
                   -- the delivery payment in full.
                   WHEN coalesce((p_facts ->> 'delivery_full_minor')::bigint, 0) > 0 THEN
                     ': ' || v_rate::text || '% of the '
                     || business.notice_money((p_facts ->> 'paid_minor')::bigint - (p_facts ->> 'delivery_full_minor')::bigint)
                     || ' you paid for your cakes, because the full-refund deadline had been reached, and your delivery payment of '
                     || business.notice_money((p_facts ->> 'delivery_full_minor')::bigint)
                     || ' in full, because we could not deliver your order.'
                   ELSE ', which is ' || v_rate::text || '% of the ' || business.notice_money((p_facts ->> 'paid_minor')::bigint)
                        || ' you paid, because the full-refund deadline had been reached.' END END;
    IF v_target > 0 THEN
      v_text := v_text || CASE WHEN v_returned THEN 'It has already been returned to you.'
                               ELSE 'We have asked for this refund and will email you once it has been returned.' END;
    END IF;
    -- A stray delivery payment (0035): returned in full beside the refund
    -- above (only outcomes that carry it; earlier notices render as before).
    IF coalesce((p_facts ->> 'unmatched_minor')::bigint, 0) > 0 THEN
      v_text := v_text || ('We are also returning in full a delivery payment of '
                           || business.notice_money((p_facts ->> 'unmatched_minor')::bigint)
                           || ' that could not be used for your order, so ' || business.notice_money(v_target
                              + (p_facts ->> 'unmatched_minor')::bigint)
                           || CASE WHEN v_returned THEN ' has been returned to you in total.'
                                   ELSE ' is being returned to you in total.' END);
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
    -- A stray delivery payment returned with the no-show (0037): stated on
    -- its own below; the refund line is the 50% part.
    v_stray := coalesce((p_facts ->> 'unmatched_minor')::bigint, 0);
    v_new := GREATEST(0, v_new - v_stray);
    v_returned := coalesce((p_facts ->> 'refund_returned')::boolean, false);
    v_text := v_text || CASE
      -- Nobody available for a GK delivery (0037, Q18): paid_minor is what
      -- was paid for the cakes; the delivery payment is kept.
      WHEN p_facts ->> 'cause' = 'nobody_available' AND coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0
        THEN 'There was no payment for your cakes, so there is nothing to refund for them.'
      WHEN p_facts ->> 'cause' = 'nobody_available'
        THEN 'Our terms return 50% of the cake price when nobody is available to receive a delivery: '
             || business.notice_money(v_target) || ' of the ' || business.notice_money((p_facts ->> 'paid_minor')::bigint)
             || ' you paid for your cakes.'
      WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN 'There was no payment for this order, so there is nothing to refund.'
      ELSE 'Our terms return 50% of what you paid when an order is not collected: ' || business.notice_money(v_target)
           || ' of the ' || business.notice_money((p_facts ->> 'paid_minor')::bigint) || ' you paid.' END;
    IF p_facts ->> 'cause' = 'nobody_available' AND coalesce((p_facts ->> 'delivery_kept_minor')::bigint, 0) > 0 THEN
      v_text := v_text || ('Your delivery payment of ' || business.notice_money((p_facts ->> 'delivery_kept_minor')::bigint)
                           || ' is not refunded, because the delivery was attempted.');
    END IF;
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
    IF v_stray > 0 THEN
      v_text := v_text || ('We are also returning in full a delivery payment of ' || business.notice_money(v_stray)
                           || ' that could not be used for your order, so ' || business.notice_money(v_new + v_stray)
                           || CASE WHEN v_returned THEN ' has been returned to you in total.'
                                   ELSE ' is being returned to you in total.' END);
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
-- As in 0036, plus delivery_failure_open (the customer's choice after an
-- our-side delivery failure is open; no deadline, the case stays with the
-- chef) and redelivery_time_needed (redelivery chosen, the chef has not set
-- the new time yet). Neither reads a clock.
CREATE OR REPLACE FUNCTION business.order_attention_flags(p_order business.orders)
RETURNS text[]
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.array_remove(ARRAY[
           CASE WHEN business.order_refund_summary(p_order.id) ->> 'state' = 'needs_attention'
                THEN 'refund_needs_attention' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                             WHERE pc.order_id = p_order.id AND pc.state = 'unknown')
                THEN 'payment_unknown' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                             WHERE pc.order_id = p_order.id AND pc.mismatch_reason IS NOT NULL)
                THEN 'payment_mismatch' END,
           CASE WHEN p_order.lifecycle = 'paid_awaiting_agreement' THEN 'paid_awaiting_agreement' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.refund_observation b
                             JOIN business.payment_collection pc ON pc.id = b.collection_id
                             WHERE pc.order_id = p_order.id AND b.quarantined AND b.resolved_at IS NULL)
                THEN 'refund_quarantine' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.order_notice n
                             WHERE n.order_id = p_order.id
                               AND ((n.outcome = 'not_queued' AND n.detail <> 'allowlist')
                                    OR (n.outcome = 'not_configured'
                                        AND EXISTS (SELECT 1 FROM business.notice_settings s WHERE s.singleton))))
                THEN 'notice_not_sent' END,
           CASE WHEN business.delivery_unmatched_minor(p_order.id) > 0 THEN 'delivery_payment_unmatched' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                             WHERE pc.order_id = p_order.id AND business.delivery_collection_stuck(pc))
                THEN 'delivery_payment_stuck' END,
           CASE WHEN business.delivery_choice_basis(p_order, pg_catalog.now()) IS NOT NULL THEN 'delivery_choice_open' END,
           CASE WHEN business.delivery_failure_open(p_order) THEN 'delivery_failure_open' END,
           CASE WHEN p_order.fulfillment_mode = 'gk_delivery' AND p_order.lifecycle = 'ready'
                     AND business.delivery_failure_refusal(p_order) = 'redelivery_time_not_set'
                THEN 'redelivery_time_needed' END
         ]::text[], NULL);
$$;
-- As in 0036, and refused (409 cancellation_not_eligible) once a GK delivery
-- is dispatched, until a secured redelivery choice (business.delivery_dispatched;
-- K05/K06, Q18). The chef's cancellation is unchanged. The amount counts the
-- paid delivery's 100% floor after an our-side failure whose redelivery is
-- not dispatched (business.delivery_failure_floor_record, recorded inside a
-- block that is rolled back), as the cancellation will record it.
CREATE OR REPLACE FUNCTION business.order_cancel_request(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_preview_id uuid, p_preview_hash text, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_l record;
  v_account uuid;
  v_capability uuid;
  v_prior uuid;
  v_p business.cancellation_preview%ROWTYPE;
  v_rate smallint;
  v_paid bigint;
  v_amount bigint;
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
  IF p_preview_id IS NULL OR p_preview_hash IS NULL OR p_preview_hash !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'preview_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order, 'cancel') l;
  v_o := v_l.o_order;
  v_account := v_l.o_account;
  v_capability := v_l.o_capability;
  v_prior := business.order_cancel_admissible(v_o, v_account, v_capability, p_key, p_body_sha256, p_expected_version);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  -- Not once the GK delivery is on its way (0037).
  IF business.delivery_dispatched(v_o) THEN
    RAISE EXCEPTION 'cancellation_not_eligible' USING ERRCODE = 'GK409';
  END IF;
  -- The preview this actor saw, exactly.
  SELECT * INTO v_p FROM business.cancellation_preview p
  WHERE p.id = p_preview_id AND p.order_id = p_order
    AND p.actor_account_id IS NOT DISTINCT FROM v_account AND p.guest_capability_id IS NOT DISTINCT FROM v_capability;
  IF NOT FOUND OR v_p.body_sha256 <> p_preview_hash THEN
    RAISE EXCEPTION 'preview_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- At the persisted receipt instant (K05): a boundary crossed since the
  -- preview, an expired preview or another amount needs a new preview.
  v_rate := business.cancellation_rate(v_o.full_refund_deadline_at, p_now, false);
  BEGIN
    PERFORM business.delivery_failure_floor_record(p_order, NULL, NULL);
    v_paid := business.order_paid_minor(p_order);
    v_amount := business.cancellation_amount(v_paid, v_rate);
    v_amount := v_amount + CASE WHEN v_amount > 0 THEN business.cancellation_floor_topup(p_order, v_amount) ELSE 0 END;
    RAISE EXCEPTION 'computed' USING ERRCODE = 'GKR01';
  EXCEPTION WHEN SQLSTATE 'GKR01' THEN
    NULL;
  END;
  IF p_now >= v_p.expires_at OR v_p.order_version <> v_o.version OR v_p.rate <> v_rate
     OR v_p.amount_minor <> v_amount THEN
    RAISE EXCEPTION 'preview_stale' USING ERRCODE = 'GK409';
  END IF;
  RETURN QUERY SELECT business.order_cancel_record(p_order, v_o.version,
                        CASE WHEN v_account IS NULL THEN 'guest' ELSE 'customer' END, v_account, v_capability, p_key,
                        p_body_sha256, p_now, p_env, 'customer_cancelled', v_rate, v_paid, v_amount, NULL, p_preview_id),
                      false;
END;
$$;
-- As in 0036, and refused (409 cancellation_not_eligible) once a GK delivery
-- is dispatched, until a secured redelivery choice (business.delivery_dispatched).
-- After an our-side failure whose redelivery is not dispatched, the amount and
-- allocations count the paid delivery's 100% floor that the cancellation will
-- record (computed with business.delivery_failure_floor_record inside a block
-- that is rolled back, so nothing is recorded by a preview).
CREATE OR REPLACE FUNCTION business.order_cancellation_preview(p_owner uuid, p_guest_digest bytea, p_order uuid, p_now timestamptz)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_l record;
  v_account uuid;
  v_capability uuid;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_rate smallint;
  v_paid bigint;
  v_amount bigint;
  v_alloc json;
  v_body json;
  v_hash text;
  v_p business.cancellation_preview%ROWTYPE;
  v_pending bigint;
  v_stray bigint;
BEGIN
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order, 'cancel') l;
  v_o := v_l.o_order;
  v_account := v_l.o_account;
  v_capability := v_l.o_capability;
  IF EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
             WHERE c.order_id = p_order AND c.command = 'cancel'
               AND NOT (r.state = 'secured' AND r.outcome ->> 'status' = 'refused'))
     OR NOT business.cancellation_eligible(v_o.lifecycle)
     -- Not once the GK delivery is on its way (0037).
     OR business.delivery_dispatched(v_o) THEN
    RAISE EXCEPTION 'cancellation_not_eligible' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  v_rate := business.cancellation_rate(v_o.full_refund_deadline_at, p_now, false);
  BEGIN
    PERFORM business.delivery_failure_floor_record(p_order, NULL, NULL);
    v_paid := business.order_paid_minor(p_order);
    v_amount := business.cancellation_amount(v_paid, v_rate);
    -- Allocated as the goal would be (combined with any recorded floor).
    v_alloc := business.cancellation_allocations(p_order, business.refund_goal_compute(p_order, v_amount));
    v_pending := business.refund_delivery_pending(p_order, business.refund_goal_compute(p_order, v_amount));
    -- A stray delivery payment (0035) returned in full with this cancellation,
    -- beside the policy amount (it is in the allocations); none without a goal.
    v_stray := CASE WHEN v_amount > 0 THEN business.delivery_unmatched_minor(p_order) ELSE 0 END;
    -- What the cancellation returns: the policy amount plus any recorded
    -- delivery floor beyond its policy share (the goal is computed from the
    -- policy amount above, which the floor raises).
    v_amount := v_amount + CASE WHEN v_amount > 0 THEN business.cancellation_floor_topup(p_order, v_amount) ELSE 0 END;
    RAISE EXCEPTION 'computed' USING ERRCODE = 'GKR01';
  EXCEPTION WHEN SQLSTATE 'GKR01' THEN
    NULL;
  END;
  -- An unexpired identical preview of this actor and version is reused.
  SELECT * INTO v_p FROM business.cancellation_preview p
  WHERE p.order_id = p_order AND p.order_version = v_o.version
    AND p.actor_account_id IS NOT DISTINCT FROM v_account AND p.guest_capability_id IS NOT DISTINCT FROM v_capability
    AND p.rate = v_rate AND p.paid_minor = v_paid AND p.amount_minor = v_amount
    AND p.allocations::text = v_alloc::text AND p.full_refund_deadline_at = v_o.full_refund_deadline_at
    AND p.computed_at <= p_now AND p_now < p.expires_at
  ORDER BY p.computed_at DESC, p.id DESC LIMIT 1;
  IF FOUND THEN
    v_body := pg_catalog.json_build_object(
      'preview_id', v_p.id, 'order_id', p_order, 'order_version', v_p.order_version, 'rate', v_p.rate,
      'paid_minor', v_p.paid_minor, 'amount_minor', v_p.amount_minor, 'currency', 'GBP', 'allocations', v_p.allocations,
      'full_refund_deadline_at', v_p.full_refund_deadline_at, 'computed_at', v_p.computed_at, 'expires_at', v_p.expires_at);
    RETURN (v_body::jsonb || pg_catalog.jsonb_build_object('preview_hash', v_p.body_sha256,
                                                           'delivery_pending_minor', v_pending,
                                                           'unmatched_minor', v_stray))::json;
  END IF;
  v_body := pg_catalog.json_build_object(
    'preview_id', v_id, 'order_id', p_order, 'order_version', v_o.version, 'rate', v_rate,
    'paid_minor', v_paid, 'amount_minor', v_amount, 'currency', 'GBP', 'allocations', v_alloc,
    'full_refund_deadline_at', v_o.full_refund_deadline_at,
    'computed_at', p_now, 'expires_at', p_now + interval '10 minutes');
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_body::text, 'UTF8')), 'hex');
  INSERT INTO business.cancellation_preview (id, order_id, order_version, actor_account_id, guest_capability_id, rate,
                                             paid_minor, amount_minor, allocations, full_refund_deadline_at,
                                             computed_at, expires_at, body_sha256)
  VALUES (v_id, p_order, v_o.version, v_account, v_capability, v_rate, v_paid, v_amount, v_alloc,
          v_o.full_refund_deadline_at, p_now, p_now + interval '10 minutes', v_hash);
  RETURN (v_body::jsonb || pg_catalog.jsonb_build_object('preview_hash', v_hash, 'delivery_pending_minor', v_pending,
                                                         'unmatched_minor', v_stray))::json;
END;
$$;
-- As in 0036, plus: after an our-side delivery failure whose redelivery is
-- not dispatched, the paid delivery's 100% floor is recorded first
-- (business.delivery_failure_floor_record; K05 GK-caused unperformed
-- delivery), so a customer's or the chef's cancellation returns it in full
-- beside the cake's rate; the outcome then states delivery_full_minor.
CREATE OR REPLACE FUNCTION business.order_terminal_return(p_order uuid, p_version integer, p_reason text, p_rate smallint,
                                               p_source text, p_actor_kind text, p_account uuid, p_operation uuid,
                                               p_at timestamptz)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_paid bigint;
  v_policy bigint;
  v_topup bigint;
  v_target bigint;
  v_floors bigint;
  v_stray bigint;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  PERFORM business.delivery_not_possible_floor(p_order, CASE p_actor_kind WHEN 'guest' THEN 'system' ELSE p_actor_kind END,
                                               p_account);
  PERFORM business.delivery_failure_floor_record(p_order, CASE p_actor_kind WHEN 'guest' THEN 'system' ELSE p_actor_kind END,
                                                 p_account);
  v_paid := business.order_paid_minor(p_order);
  v_policy := business.cancellation_amount(v_paid, p_rate);
  v_topup := CASE WHEN v_policy > 0 THEN business.cancellation_floor_topup(p_order, v_policy) ELSE 0 END;
  v_target := v_policy + v_topup;
  SELECT coalesce(sum(f.floor_minor), 0) INTO v_floors FROM business.refund_floor f WHERE f.order_id = p_order;
  -- A stray delivery payment (0035) not returned yet: the goal returns it in
  -- full beside the policy target, so the outcome states it.
  v_stray := CASE WHEN v_policy > 0 THEN business.delivery_unmatched_minor(p_order) ELSE 0 END;
  UPDATE business.orders o
  SET lifecycle = 'cancelled', cancel_reason = p_reason, cancelled_at = p_at,
      recovery_state = 'cancellation_pending', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = p_order;
  IF v_policy > 0 THEN
    -- The goal from the policy target: recorded floors raise it.
    PERFORM business.refund_goal_apply(p_order, p_reason, p_source, p_rate, v_paid, v_policy, 'system', NULL);
    PERFORM business.refund_reserve(p_order);
  END IF;
  IF EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = p_order AND a.day = v_o.handover_day) THEN
    PERFORM business.capacity_release(p_order, v_o.handover_day);
  END IF;
  PERFORM business.audit_record(CASE p_actor_kind WHEN 'guest' THEN 'system' ELSE p_actor_kind END, p_account,
                                'order.cancelled', 'order', p_order, NULL,
                                CASE WHEN v_stray > 0
                                     THEN pg_catalog.json_build_object('operation_id', p_operation, 'reason', p_reason,
                                                                       'rate', p_rate, 'refund_target_minor', v_target,
                                                                       'unmatched_minor', v_stray)
                                     ELSE pg_catalog.json_build_object('operation_id', p_operation, 'reason', p_reason,
                                                                       'rate', p_rate, 'refund_target_minor', v_target) END);
  IF v_topup > 0 THEN
    RETURN (pg_catalog.json_build_object(
      'status', 'cancelled',
      'evidence', pg_catalog.jsonb_build_object('order_version', p_version + 1, 'reason', p_reason, 'rate', p_rate,
                                                'paid_minor', v_paid, 'refund_target_minor', v_target,
                                                'delivery_full_minor', v_floors)
                  || CASE WHEN v_stray > 0 THEN pg_catalog.jsonb_build_object('unmatched_minor', v_stray)
                          ELSE '{}'::jsonb END))::text;
  END IF;
  IF v_stray > 0 THEN
    RETURN pg_catalog.format(
      '{"status":"cancelled","evidence":{"order_version":%s,"reason":"%s","rate":%s,"paid_minor":%s,"refund_target_minor":%s,'
      '"unmatched_minor":%s}}',
      p_version + 1, p_reason, p_rate, v_paid, v_target, v_stray);
  END IF;
  RETURN pg_catalog.format(
    '{"status":"cancelled","evidence":{"order_version":%s,"reason":"%s","rate":%s,"paid_minor":%s,"refund_target_minor":%s}}',
    p_version + 1, p_reason, p_rate, v_paid, v_target);
END;
$$;
-- As in 0035, plus: a delivery payment kept after a nobody-available failure
-- (business.delivery_kept, Q18) is outside every allocation (no weight, no
-- basis, no floor: a share of 0, so nothing of it is reserved or left
-- pending for the chef) unless the chef recorded a floor on it. Without kept
-- payments the result is unchanged.
CREATE OR REPLACE FUNCTION business.refund_shares(p_order uuid, p_target bigint, p_floors boolean)
RETURNS TABLE (collection_id uuid, ord integer, share_minor bigint, exposure_minor bigint, room_minor bigint, blocked boolean,
               floored boolean)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH c AS (
    SELECT pc.id, CASE pc.kind WHEN 'cake' THEN 0 ELSE 1 END AS ord, b.paid_minor AS paid,
           d.collection_id IS NOT NULL AS um,
           (k.collection_id IS NOT NULL AND f.id IS NULL) AS kept,
           CASE WHEN p_floors THEN LEAST(GREATEST(coalesce(f.floor_minor, 0),
                                                  CASE WHEN d.collection_id IS NOT NULL THEN b.paid_minor ELSE 0 END),
                                         b.paid_minor)
                ELSE 0 END AS fl,
           b.exposure_minor AS ex, b.room_minor AS room,
           (b.unresolved OR b.quarantined OR EXISTS (SELECT 1 FROM business.refund_operation ro
                                                    WHERE ro.collection_id = pc.id AND ro.mismatch_reason IS NOT NULL)) AS blocked,
           (f.id IS NOT NULL OR d.collection_id IS NOT NULL) AS floored
    FROM business.payment_collection pc
    CROSS JOIN LATERAL business.refund_balance(pc.id) b
    LEFT JOIN business.refund_floor f ON f.collection_id = pc.id
    LEFT JOIN business.delivery_payment_outcome d ON d.collection_id = pc.id AND d.outcome = 'unmatched'
    LEFT JOIN business.delivery_kept k ON k.collection_id = pc.id
    WHERE pc.order_id = p_order AND pc.state = 'succeeded'
  ), w AS (
    SELECT c.*, CASE WHEN c.kept THEN 0 WHEN p_floors THEN c.paid - c.fl WHEN c.um THEN 0 ELSE c.paid END AS wt,
           CASE WHEN c.kept THEN 0 WHEN p_floors OR NOT c.um THEN c.paid ELSE 0 END AS basis
    FROM c
  ), t AS (
    SELECT GREATEST(0, LEAST(p_target, coalesce(sum(w.basis), 0)) - coalesce(sum(w.fl), 0))::bigint AS tp,
           coalesce(sum(w.wt), 0)::bigint AS pw
    FROM w
  ), a AS (
    SELECT w.*, t.tp,
           CASE WHEN t.pw > 0 THEN (t.tp * w.wt) / t.pw ELSE 0 END AS base,
           CASE WHEN t.pw > 0 THEN (t.tp * w.wt) % t.pw ELSE 0 END AS rem
    FROM w, t
  ), r AS (
    SELECT a.*, row_number() OVER (ORDER BY a.rem DESC, a.ord, a.id) AS rn, a.tp - sum(a.base) OVER () AS leftover
    FROM a
  )
  SELECT r.id, r.ord, (r.fl + r.base + CASE WHEN r.rn <= r.leftover THEN 1 ELSE 0 END)::bigint, r.ex, r.room, r.blocked,
         r.floored
  FROM r ORDER BY r.ord, r.id;
$$;
REVOKE ALL ON TABLE business.delivery_failure FROM PUBLIC;
REVOKE ALL ON TABLE business.delivery_failure_choice FROM PUBLIC;
REVOKE ALL ON TABLE business.delivery_kept FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_last_dispatch(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_failure_current(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_failure_chosen(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_failure_open(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_rescheduled_since_dispatch(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_on_its_way(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_failure_floor_record(uuid, text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_dispatched(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.redelivery_time_set(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_failure_refusal(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_failure_choice_json(business.delivery_failure_choice) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_failure_view(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_delivery_failure(jsonb, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_handover_enqueue(uuid, uuid, text, text, integer, text, text, timestamptz, text, text)
  FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_failed(uuid, uuid, text, text, integer, text, text, text, timestamptz)
  FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_failure_choice_request(uuid, bytea, uuid, text, text, integer, text, uuid, text,
  timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_failure_choice_commit(business.order_command) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_delivery_failed(uuid, uuid, text, text, integer, text, text, text, timestamptz)
  TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_delivery_failure_choice_request(uuid, bytea, uuid, text, text, integer, text, uuid, text,
  timestamptz) TO gk_api;
