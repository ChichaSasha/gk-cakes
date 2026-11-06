-- Read gaps the customer order page and the chef order page found (GK-030 /
-- GK-031 on the Creator-approved GK-017 r02b designs; expand only). Every
-- field is additive and no existing field changes its meaning (contracts
-- K09 "API v1 additive"); the staff views keep their redaction (K00
-- technical support has no money authority, fulfilment prepare/ready/
-- dispatch/handover only; K09 "Sasha necessary support only"); nothing is
-- shown before it is secured (K11).
--
-- 1. Lifecycle times (business.order_lifecycle_times). order_view (the
--    customer's GET /orders/{id}, and so the chef's staff detail) adds
--    confirmed_at (the receipt instant of the paid or free confirmation, or
--    of the late payment's honour, once that command is secured with outcome
--    confirmed), production_started_at (the production transition's
--    effective instant: an operational step, visible once committed, as the
--    lifecycle in_production), ready_at (the ready step's receipt), and
--    dispatched_at (the latest dispatch's receipt; GK delivery), handed_over_at
--    (the handover's receipt once its K11 command is secured) and
--    delivery_paid_at (the provider success instant of the collection that
--    paid the current delivery quote, exactly when the delivery block shows
--    paid_minor). NULL until then. Technical support gets the same six; fulfilment
--    the first five (no payment time).
--
-- 2. Staff detail. Chef and technical support add original_handover_at (the
--    immutable placement anchor of the refund deadline and the delivery
--    pay-by), free_delivery_code (true when the order's code application is
--    a consumed free_delivery code, the waiver's own rule; never the code)
--    and updated_at (when the order row last changed: the database's commit
--    clock, not a business instant); fulfilment adds updated_at only. The
--    production block and the fulfilment history entries add
--    actor_account_id (the staff account already recorded: the manual
--    production start's starter, each step's actor; NULL for the timer) for
--    the chef and technical support, never for fulfilment (K00 grants it no
--    view of other staff). Fulfilment's delivery_failure block adds
--    redelivery_time_set when the order page's block has it (after a
--    secured redelivery choice), so it is not offered a dispatch the API
--    refuses (409 redelivery_time_not_set).
--
-- 3. Staff schedule choices (business.schedule_staff_choices, gk_api,
--    chef). The published starts of one London day for one order, judged
--    as the chef's appointment change (0031, business.order_appointment_change_request)
--    or, for a paid_awaiting_agreement order, the late-payment offer of
--    another date (0034, business.late_payment_slot_fits) would judge them
--    at the API's receipt instant: day_status open | pending (an order
--    command is unresolved: K11, wait for it) | not_eligible (lifecycle,
--    not secured, a GK delivery on its way or a failed delivery's open
--    choice) | closed (no published start that day: not an opening weekday,
--    a closed date, or no schedule); each start available or unavailable
--    with reason_code current_slot | past | shelf_life | day_full (an offer
--    on another day whose standard places cannot take the order; a customer
--    acceptance never creates chef_extra) | the day status; allocation on
--    another day for an order holding a day place: standard, or chef_extra
--    (the appointment change's classification: the chef's decision admits it
--    beyond the limit with the feasibility note as evidence). The day's
--    standard limit and the other orders' committed standard cakes are read
--    without locking the day (no handover_day row is created, no hold);
--    the commands still re-check everything under their locks (and a
--    restore freeze, which this read does not reflect).
--
-- Not here: the delivery address (K00/K09 privacy review pending; the
-- Creator's r02b Q6 needs a contract check first).
--
-- Expand-only (K09): new functions; replaced functions keep their
-- signatures, results, security and grants. Reads only; no function here
-- reads the database clock for a decision (the choices compare the API's
-- instant).

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------ helpers

-- Owner-internal: the order's lifecycle times as customers may see them
-- (see the header). p_delivery is the order's delivery block
-- (business.order_delivery_view), so delivery_paid_at follows its paid_minor.
CREATE FUNCTION business.order_lifecycle_times(p_o business.orders, p_delivery jsonb,
                                               OUT confirmed_at timestamptz, OUT production_started_at timestamptz,
                                               OUT ready_at timestamptz, OUT dispatched_at timestamptz,
                                               OUT handed_over_at timestamptz, OUT delivery_paid_at timestamptz)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT (SELECT c.received_at FROM business.order_command c
          JOIN business.recovery_operation r ON r.id = c.id
          WHERE c.order_id = p_o.id AND c.command IN ('free_confirm', 'paid_confirm', 'late_payment')
            AND r.state = 'secured' AND r.outcome ->> 'status' = 'confirmed'
          ORDER BY c.created_at, c.id LIMIT 1),
         (SELECT t.effective_at FROM business.production_transition t WHERE t.order_id = p_o.id),
         (SELECT e.received_at FROM business.fulfillment_event e
          WHERE e.order_id = p_o.id AND e.action = 'ready'
          ORDER BY e.created_at DESC, e.id DESC LIMIT 1),
         (SELECT e.received_at FROM business.fulfillment_event e WHERE e.id = business.order_last_dispatch(p_o.id)),
         (SELECT e.received_at FROM business.fulfillment_event e
          JOIN business.recovery_operation r ON r.id = e.command_id
          WHERE e.order_id = p_o.id AND e.action = 'handover' AND r.state = 'secured'
          ORDER BY e.created_at DESC, e.id DESC LIMIT 1),
         CASE WHEN (p_delivery -> 'paid_minor') IS NOT NULL THEN
           (SELECT pc.succeeded_at FROM business.payment_collection pc
            WHERE pc.id = business.delivery_paid_collection(business.delivery_current_quote(p_o.id))) END;
$$;

-- Owner-internal: whether the order's code application is a consumed
-- free_delivery code (the delivery waiver's eligibility rule, 0035).
CREATE FUNCTION business.order_free_delivery_code(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.code_application ca
                 JOIN business.discount_code_version cv ON cv.id = ca.code_version_id
                 WHERE ca.order_id = p_order AND ca.state = 'consumed' AND cv.kind = 'free_delivery');
$$;

-- Owner-internal: the lifecycle times of customer view v for a staff view
-- that picks its keys (p_payment: with delivery_paid_at).
CREATE FUNCTION business.order_staff_times(v jsonb, p_payment boolean)
RETURNS jsonb
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'confirmed_at', v -> 'confirmed_at', 'production_started_at', v -> 'production_started_at',
           'ready_at', v -> 'ready_at', 'dispatched_at', v -> 'dispatched_at', 'handed_over_at', v -> 'handed_over_at')
         || CASE WHEN p_payment THEN pg_catalog.jsonb_build_object('delivery_paid_at', v -> 'delivery_paid_at')
                 ELSE '{}'::jsonb END;
$$;

-- Owner-internal: the chef's and technical support's order facts beside the
-- customer view: the original handover anchor, whether a free-delivery code
-- was used and when the order last changed.
CREATE FUNCTION business.order_staff_extras(p_o business.orders)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object('original_handover_at', p_o.original_handover_at,
                                       'free_delivery_code', business.order_free_delivery_code(p_o.id),
                                       'updated_at', p_o.updated_at);
$$;

-- Owner-internal: the production block for the chef and technical support:
-- business.order_production_view plus the manual start's staff account
-- (NULL for the timer or before a start).
CREATE FUNCTION business.order_staff_production(p_order uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT business.order_production_view(p_order)
         || pg_catalog.jsonb_build_object('actor_account_id',
              (SELECT t.actor_account_id FROM business.production_transition t WHERE t.order_id = p_order));
$$;

-- Owner-internal: business.order_staff_fulfillment with each step's staff
-- account (chef and technical support views; fulfilment keeps 0032's).
CREATE FUNCTION business.order_staff_fulfillment_events(p_order uuid, p_reasons boolean)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
           'action', e.action, 'from', e.from_state, 'to', e.to_state, 'received_at', e.received_at,
           'command_id', e.command_id,
           'state', CASE WHEN e.command_id IS NULL OR r.state = 'secured' THEN 'done' ELSE 'processing' END,
           'actor_account_id', e.actor_account_id)
           || CASE WHEN p_reasons THEN pg_catalog.jsonb_build_object('reason', e.reason) ELSE '{}'::jsonb END
           ORDER BY e.created_at, e.id), '[]'::jsonb)
  FROM business.fulfillment_event e
  LEFT JOIN business.recovery_operation r ON r.id = e.command_id
  WHERE e.order_id = p_order;
$$;

-- ---------------------------------------------------------------- API

-- API: the chef's schedule choices for order p_order on London day p_day at
-- the API's instant p_now (GET /admin/schedule/choices; see the header).
-- One row per published start of the day (one row without a slot when there
-- is none). purpose: late_payment_offer for a paid_awaiting_agreement order,
-- appointment_change otherwise. The current appointment is the visible one
-- (K11). standard_limit, committed_standard (other orders) and order_cakes
-- are set when the day is open, differs from the order's day and the order
-- holds a day place. Reads only: no hold, no handover_day row, no lock.
CREATE FUNCTION business.schedule_staff_choices(p_actor uuid, p_order uuid, p_day date, p_now timestamptz)
RETURNS TABLE (purpose text, day_status text, order_mode text, schedule_revision integer, current_day date,
               current_slot_id uuid, current_starts_at timestamptz, current_ends_at timestamptz,
               standard_limit integer, committed_standard bigint, order_cakes integer,
               slot_id uuid, starts_at timestamptz, ends_at timestamptz, availability text, reason_code text,
               allocation text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_a jsonb;
  v_day date;
  v_current timestamptz;
  v_purpose text;
  v_status text;
  v_qty integer;
  v_limit integer;
  v_committed bigint;
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef']);
  IF p_order IS NULL OR p_now IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_day IS NULL THEN
    RAISE EXCEPTION 'day_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v_a := business.order_visible_appointment(v_o);
  v_day := (v_a ->> 'date')::date;
  v_current := (v_a ->> 'starts_at')::timestamptz;
  v_purpose := CASE WHEN v_o.lifecycle = 'paid_awaiting_agreement' THEN 'late_payment_offer' ELSE 'appointment_change' END;
  -- An unresolved command first: the stored state may not be visible yet.
  v_status := CASE
    WHEN business.order_command_unresolved(v_o.id) THEN 'pending'
    WHEN v_o.recovery_state <> 'secured'
         OR (v_purpose = 'appointment_change' AND NOT business.appointment_change_eligible(v_o)) THEN 'not_eligible'
    WHEN NOT EXISTS (SELECT 1 FROM business.schedule_slots_on(p_day)) THEN 'closed'
    ELSE 'open' END;
  IF v_status = 'open' AND p_day <> v_o.handover_day THEN
    -- The order's live day place (0031 / 0034: the commit moves it).
    SELECT a.cake_quantity INTO v_qty FROM business.capacity_allocation a
    WHERE a.order_ref = v_o.id AND a.day = v_o.handover_day AND a.state <> 'released';
    IF v_qty IS NOT NULL THEN
      -- capacity_day_lock's limit, read without the lock or a new day row.
      SELECT coalesce(h.standard_limit, (SELECT c.version_limit FROM business.schedule_current() c), 0) INTO v_limit
      FROM (SELECT 1) one LEFT JOIN business.handover_day h ON h.day = p_day;
      SELECT coalesce(pg_catalog.sum(a.cake_quantity), 0) INTO v_committed FROM business.capacity_allocation a
      WHERE a.day = p_day AND a.classification = 'standard' AND a.state = 'committed' AND a.order_ref <> v_o.id;
    END IF;
  END IF;
  RETURN QUERY
  SELECT v_purpose, v_status, business.order_visible_mode(v_o),
         coalesce(s.schedule_revision, (SELECT c.version_revision FROM business.schedule_current() c)),
         v_day, (v_a ->> 'id')::uuid, v_current, (v_a ->> 'ends_at')::timestamptz,
         v_limit, v_committed, v_qty,
         s.slot_id, s.starts_at, s.ends_at,
         CASE WHEN s.slot_id IS NULL THEN NULL WHEN x.reason IS NULL THEN 'available' ELSE 'unavailable' END,
         x.reason,
         CASE WHEN s.slot_id IS NOT NULL AND x.reason IS NULL AND v_qty IS NOT NULL THEN
           CASE WHEN v_committed + v_qty <= v_limit THEN 'standard' ELSE 'chef_extra' END END
  FROM (SELECT 1) one
  LEFT JOIN business.schedule_slots_on(p_day) s ON true
  LEFT JOIN LATERAL (SELECT CASE
      WHEN s.slot_id IS NULL THEN NULL
      -- The appointment itself: the change answers unchanged; an offer of
      -- it is kind requested.
      WHEN p_day = v_day AND (s.slot_id = (v_a ->> 'id')::uuid OR s.starts_at = v_current) THEN 'current_slot'
      WHEN v_status <> 'open' THEN v_status
      WHEN s.starts_at <= p_now THEN 'past'
      WHEN NOT business.order_lots_last_until(v_o.id, s.ends_at) THEN 'shelf_life'
      WHEN v_purpose = 'late_payment_offer' AND v_qty IS NOT NULL AND v_committed + v_qty > v_limit THEN 'day_full'
    END AS reason) x ON true
  ORDER BY s.starts_at;
END;
$$;

-- ------------------------------------------------- replaced functions

-- As in 0038, plus the lifecycle times (business.order_lifecycle_times) as
-- its last keys; the delivery block is read once for both.
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
           'delivery', dv.d,
           'delivery_choice', business.order_delivery_choice_view(o),
           'delivery_failure', business.order_delivery_failure_view(o),
           'refund', CASE WHEN o.recovery_state <> 'cancellation_pending' AND NOT business.order_refund_hidden(o.id)
                          THEN business.order_refund_customer(o.id) END,
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
                     WHERE i.order_id = o.id),
           'confirmed_at', lt.confirmed_at, 'production_started_at', lt.production_started_at,
           'ready_at', lt.ready_at, 'dispatched_at', lt.dispatched_at, 'handed_over_at', lt.handed_over_at,
           'delivery_paid_at', lt.delivery_paid_at)
  FROM business.orders o
  CROSS JOIN LATERAL (SELECT business.order_visible_appointment(o) AS a) va
  CROSS JOIN LATERAL (SELECT business.order_delivery_view(o) AS d) dv
  CROSS JOIN LATERAL business.order_lifecycle_times(o, dv.d) lt
  WHERE o.id = p_order;
$$;

-- As in 0037, plus the lifecycle times and, for the chef and technical
-- support, the original handover, free_delivery_code, updated_at and the
-- staff accounts of production and fulfilment steps; fulfilment adds the
-- times without the payment's and updated_at (see the header).
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
                     'production', business.order_staff_production(o.id),
                     'collections', business.order_staff_collections(o.id, true),
                     'refund_goal', business.order_staff_refund_goal(o.id, true),
                     'refund_operations', business.order_staff_refund_operations(o.id, true),
                     'refund_observations', business.order_staff_refund_observations(o.id, true),
                     'commands', business.order_staff_commands(o.id),
                     'appointment_history', business.order_staff_appointments(o.id, true),
                     'fulfillment_history', business.order_staff_fulfillment_events(o.id, true),
                     'late_payment_offers', business.order_staff_late_payment_offers(o.id),
                     'audit', business.order_staff_audit(o.id))
                || business.order_staff_extras(o)
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
                  'production', business.order_staff_production(o.id),
                  'collections', business.order_staff_collections(o.id, false),
                  'refund_goal', business.order_staff_refund_goal(o.id, false),
                  'refund_operations', business.order_staff_refund_operations(o.id, false),
                  'refund_observations', business.order_staff_refund_observations(o.id, false),
                  'commands', business.order_staff_commands(o.id),
                  'appointment_change', v -> 'appointment_change',
                  'appointment_history', business.order_staff_appointments(o.id, false),
                  'fulfillment', v -> 'fulfillment',
                  'fulfillment_history', business.order_staff_fulfillment_events(o.id, false),
                  -- No money for technical support: the refund amount shown to
                  -- the customer is dropped.
                  'late_payment', CASE WHEN pg_catalog.jsonb_typeof(v -> 'late_payment') = 'object'
                                       THEN (v -> 'late_payment') - ARRAY['paid_minor', 'currency']
                                       ELSE v -> 'late_payment' END,
                  'agreed', v -> 'agreed',
                  'late_payment_offers', business.order_staff_late_payment_offers(o.id),
                  'audit', business.order_staff_audit(o.id))
                || business.order_staff_times(v, true)
                || business.order_staff_extras(o)
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
                -- Lifecycle times without the delivery payment's, and when the
                -- order last changed; no actors, anchors or money.
                || business.order_staff_times(v, false)
                || pg_catalog.jsonb_build_object('updated_at', o.updated_at)
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

-- As in 0037, and fulfilment's copy keeps redelivery_time_set.
CREATE OR REPLACE FUNCTION business.order_staff_delivery_failure(p_block jsonb, p_order uuid, p_view text)
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
      -- 0039: whether the chef set the redelivery time (only after a secured
      -- redelivery choice, as on the order page), so fulfilment is not
      -- offered a dispatch the API refuses (redelivery_time_not_set).
      || CASE WHEN (p_block -> 'redelivery_time_set') IS NOT NULL
              THEN pg_catalog.jsonb_build_object('redelivery_time_set', p_block -> 'redelivery_time_set')
              ELSE '{}'::jsonb END
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

REVOKE ALL ON FUNCTION business.order_lifecycle_times(business.orders, jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_free_delivery_code(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_times(jsonb, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_extras(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_production(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_fulfillment_events(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_staff_choices(uuid, uuid, date, timestamptz) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.schedule_staff_choices(uuid, uuid, date, timestamptz) TO gk_api;
