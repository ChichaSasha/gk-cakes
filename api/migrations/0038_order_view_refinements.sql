-- Order view refinements for the order page (GK-030 states 10-19, design
-- GK-017 r02b; expand only). Additive read-model fields the UI asked for, so
-- it never computes money or offers a button the API refuses (contracts K00
-- additive API v1 and capability scope, K05 allocation and floors, K09
-- expand-only, K11 nothing visible before it is secured).
--
-- 1. Customer actions (business.order_customer_actions, API). The API's
--    GET /orders/{id} adds actions: what this caller may do now. The
--    database answers the caller's grants (the owner: every customer
--    command; a guest: its capability's allowed_commands) and the state
--    facts of the commands the view does not show (pay, free confirmation,
--    cancel, same-day move), with the instants the API compares with its own
--    clock (the quote window, the lead time, the six-hour move cutoff). The
--    API derives the others from the blocks it already shows at its clock
--    (late_payment, delivery, delivery_choice, delivery_failure). The same
--    access rule as order_get (NULL otherwise). Reads only.
--
-- 2. Labelled allocations. The cancellation preview's allocations carry
--    kind cake | delivery | stray (stray: a delivery payment whose outcome
--    is unmatched, 0035) in the answer only: the stored allocations and the
--    preview hash are unchanged (like delivery_pending_minor and
--    unmatched_minor), so previews made before this migration stay
--    reusable and cancellable.
--
-- 3. Refund parts (business.order_refund_parts). order_view's refund block
--    adds parts [{kind, target_minor, succeeded_minor, in_progress_minor,
--    pending_chef_minor}] in the order cake, delivery, stray; parts whose
--    amounts are all 0 are left out (a kept delivery, Q18). Invariant: the
--    parts add up to the block's target_minor, succeeded_minor,
--    in_progress_minor and delivery_pending_minor exactly. Per kind:
--    succeeded and in progress are the block's own sums over that kind's
--    collections (an unpublished manual payout counted in progress, as the
--    block does); pending_chef is delivery_pending_minor (only unfloored
--    matched delivery collections wait for the chef, K05); the target is
--    the larger of the kind's K05 share of the goal (business.refund_shares
--    with floors: recorded floors and strays first, then the proportional
--    rest) and what is accounted for on it (succeeded + in progress +
--    pending chef + what refund_plan would reserve now). When those differ
--    from the goal (prior actuals skewed by a chef exception), the
--    difference is taken first from shares not yet received, then, only if
--    more went back or is in flight than the goal (an external refund), from
--    the parts in order; a goal that no part accounts for goes to the first
--    part (cake). The block is order_view's only: the staff list, attention
--    flags, notices and account summaries keep business.order_refund_summary
--    unchanged; the chef's staff detail (order_view) gets parts, technical
--    support's picks its keys (no money).
--
-- 4. Account summaries (business.account_order_summaries, API). The rows of
--    account_order_page (unchanged, for the deployed API) plus each order's
--    late_payment, delivery, delivery_choice and delivery_failure blocks
--    under order_blocks, which the API reads at its own clock to add
--    delivery_state, needs_answer and attention and then removes (never in a
--    response). order_customer_summary now renders through
--    business.order_customer_summary_from (identical output) so the page
--    reads order_view once per row.
--
-- 5. No-show notice and a stray refund reserved earlier. The no-show's
--    outcome now also records, when the order has a stray delivery payment,
--    stray_before_minor (what was already returned or on its way on the
--    strays before the no-show) and stray_new_minor (what the no-show newly
--    reserved for them). The notice's refund line is the 50% part: newly
--    reserved minus stray_new_minor (previously minus the stray's whole
--    unreturned amount, which under-reported it when a stray refund was
--    reserved but unsettled before the no-show), and "already refunded"
--    leaves out stray returns (they never count against the 50%, 0035).
--    Outcomes without these keys render as before; orders without a stray
--    record nothing new (bytes unchanged). The cancelled notice states the
--    policy amount from the outcome and the stray beside it without
--    subtracting, so it has no such gap.

--
-- Expand-only (K09): new functions; replaced functions keep their
-- signatures, results, security and grants. Reads only, except the no-show
-- commit's two new outcome keys.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------ helpers

-- Owner-internal: how a collection's refund is labelled for the customer:
-- cake, delivery (a delivery collection that paid its quote, or not settled
-- yet) or stray (a delivery payment whose outcome is unmatched, 0035).
CREATE FUNCTION business.collection_refund_kind(p_collection uuid)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN pc.kind = 'cake' THEN 'cake'
              WHEN EXISTS (SELECT 1 FROM business.delivery_payment_outcome d
                           WHERE d.collection_id = pc.id AND d.outcome = 'unmatched') THEN 'stray'
              ELSE 'delivery' END
  FROM business.payment_collection pc WHERE pc.id = p_collection;
$$;

-- Owner-internal: allocations [{collection_id, amount_minor}] with each
-- collection's kind, in the same order.
CREATE FUNCTION business.cancellation_allocations_labelled(p_alloc json)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.json_agg(pg_catalog.json_build_object(
                    'collection_id', (e.v ->> 'collection_id')::uuid,
                    'amount_minor', (e.v ->> 'amount_minor')::bigint,
                    'kind', business.collection_refund_kind((e.v ->> 'collection_id')::uuid)) ORDER BY e.n), '[]'::json)
  FROM pg_catalog.json_array_elements(p_alloc) WITH ORDINALITY e(v, n);
$$;

-- Owner-internal: the refund block's parts per kind (see the header, 3);
-- NULL without a goal.
CREATE FUNCTION business.order_refund_parts(p_order uuid)
RETURNS json
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_target bigint;
  v_kinds constant text[] := ARRAY['cake', 'delivery', 'stray'];
  v_present boolean[] := ARRAY[false, false, false];
  v_s bigint[] := ARRAY[0, 0, 0];
  v_r bigint[] := ARRAY[0, 0, 0];
  v_p bigint[] := ARRAY[0, 0, 0];
  v_plan bigint[] := ARRAY[0, 0, 0];
  v_share bigint[] := ARRAY[0, 0, 0];
  v_t bigint[] := ARRAY[0, 0, 0];
  v_diff bigint;
  v_take bigint;
  v_out jsonb := '[]'::jsonb;
  v_k integer;
  r record;
BEGIN
  SELECT g.target_minor INTO v_target FROM business.refund_goal g WHERE g.order_id = p_order;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  -- Succeeded and in progress, as order_refund_summary sums them (every
  -- collection; an unpublished manual payout success is still in progress).
  FOR r IN
    SELECT pg_catalog.array_position(v_kinds, business.collection_refund_kind(pc.id)) AS k,
           pc.state = 'succeeded' AS paid, b.succeeded_minor AS s, b.reserved_minor AS rv,
           (SELECT coalesce(sum(ro.amount_minor), 0)
            FROM business.refund_operation ro
            WHERE ro.collection_id = pc.id AND ro.channel = 'manual_alternative' AND ro.state = 'succeeded'
              AND EXISTS (SELECT 1 FROM business.manual_payout_command mc
                          JOIN business.recovery_operation x ON x.id = mc.command_id
                          WHERE mc.refund_operation_id = ro.id AND mc.outcome = 'succeeded' AND x.state <> 'secured'))::bigint
             AS unpub
    FROM business.payment_collection pc
    CROSS JOIN LATERAL business.refund_balance(pc.id) b
    WHERE pc.order_id = p_order
  LOOP
    v_s[r.k] := v_s[r.k] + r.s - r.unpub;
    v_r[r.k] := v_r[r.k] + r.rv + r.unpub;
    v_present[r.k] := v_present[r.k] OR r.paid OR r.s > 0 OR r.rv > 0;
  END LOOP;
  -- Only unfloored matched delivery collections wait for the chef (K05).
  v_p[2] := business.refund_delivery_pending(p_order, v_target);
  FOR r IN SELECT pg_catalog.array_position(v_kinds, business.collection_refund_kind(s.collection_id)) AS k,
                  s.share_minor
           FROM business.refund_shares(p_order, v_target, true) s LOOP
    v_share[r.k] := v_share[r.k] + r.share_minor;
  END LOOP;
  FOR r IN SELECT pg_catalog.array_position(v_kinds, business.collection_refund_kind(p.collection_id)) AS k,
                  p.amount_minor
           FROM business.refund_plan(p_order, v_target) p LOOP
    v_plan[r.k] := v_plan[r.k] + r.amount_minor;
  END LOOP;
  FOR v_k IN 1..3 LOOP
    v_t[v_k] := GREATEST(v_share[v_k], v_s[v_k] + v_r[v_k] + v_p[v_k] + v_plan[v_k]);
  END LOOP;
  v_diff := v_target - (v_t[1] + v_t[2] + v_t[3]);
  IF v_diff > 0 THEN
    -- A goal no part accounts for: the first part present (cake first).
    v_k := coalesce(pg_catalog.array_position(v_present, true), 1);
    v_t[v_k] := v_t[v_k] + v_diff;
  ELSIF v_diff < 0 THEN
    -- First the shares a part has not received (another part received more
    -- than its share, for example a chef exception before a cancellation).
    FOR v_k IN 1..3 LOOP
      EXIT WHEN v_diff >= 0;
      v_take := LEAST(-v_diff, GREATEST(0, v_t[v_k] - (v_s[v_k] + v_r[v_k] + v_p[v_k] + v_plan[v_k])));
      v_t[v_k] := v_t[v_k] - v_take;
      v_diff := v_diff + v_take;
    END LOOP;
    -- Then, only when more went back or is in flight than the goal (an
    -- external refund), from the parts in order.
    FOR v_k IN 1..3 LOOP
      EXIT WHEN v_diff >= 0;
      v_take := LEAST(-v_diff, v_t[v_k]);
      v_t[v_k] := v_t[v_k] - v_take;
      v_diff := v_diff + v_take;
    END LOOP;
  END IF;
  FOR v_k IN 1..3 LOOP
    CONTINUE WHEN v_t[v_k] = 0 AND v_s[v_k] = 0 AND v_r[v_k] = 0 AND v_p[v_k] = 0;
    v_out := v_out || pg_catalog.jsonb_build_array(pg_catalog.jsonb_build_object(
               'kind', v_kinds[v_k], 'target_minor', v_t[v_k], 'succeeded_minor', v_s[v_k],
               'in_progress_minor', v_r[v_k], 'pending_chef_minor', v_p[v_k]));
  END LOOP;
  -- Each part's keys in this order (jsonb would sort them).
  RETURN (SELECT coalesce(pg_catalog.json_agg(pg_catalog.json_build_object(
                   'kind', e ->> 'kind', 'target_minor', (e ->> 'target_minor')::bigint,
                   'succeeded_minor', (e ->> 'succeeded_minor')::bigint,
                   'in_progress_minor', (e ->> 'in_progress_minor')::bigint,
                   'pending_chef_minor', (e ->> 'pending_chef_minor')::bigint) ORDER BY n), '[]'::json)
          FROM pg_catalog.jsonb_array_elements(v_out) WITH ORDINALITY x(e, n));
END;
$$;

-- Owner-internal: the customer's refund block: order_refund_summary
-- (unchanged, same keys in the same order) with parts appended.
CREATE FUNCTION business.order_refund_customer(p_order uuid)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  -- The summary is a json object (json_build_object): its text without the
  -- closing brace, then parts, keeps every existing key and its order.
  SELECT CASE WHEN x.s IS NULL THEN NULL
              ELSE (pg_catalog.left(x.s::text, -1) || ', "parts" : '
                    || coalesce(business.order_refund_parts(p_order)::text, '[]') || '}')::json END
  FROM (SELECT business.order_refund_summary(p_order) AS s) x;
$$;

-- API: the state facts and grants behind GET /orders/{id}'s actions (see
-- the header, 1), for the owner (bearer subject) or a guest capability with
-- read, exactly as order_get; NULL otherwise. Instants are for the API's
-- clock: before = allowed while now is earlier (equality has passed),
-- not_after = allowed until and at that instant.
CREATE FUNCTION business.order_customer_actions(p_order uuid, p_owner uuid, p_guest_digest bytea)
RETURNS json
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  o business.orders%ROWTYPE;
  v_grants text[];
  v_quote_expires timestamptz;
  v_unresolved boolean;
  v_open_payment boolean;
BEGIN
  IF p_order IS NULL OR (p_owner IS NULL) = (p_guest_digest IS NULL) THEN
    RETURN NULL;
  END IF;
  SELECT * INTO o FROM business.orders x WHERE x.id = p_order;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  IF p_owner IS NOT NULL THEN
    IF o.owner_account_id IS NULL OR o.owner_account_id IS DISTINCT FROM (
         SELECT a.id FROM business.account a WHERE a.provider_subject = p_owner AND a.disabled_at IS NULL) THEN
      RETURN NULL;
    END IF;
    -- The owner may use every customer command.
    v_grants := ARRAY['read', 'pay', 'cancel', 'move_pickup'];
  ELSE
    SELECT g.allowed_commands INTO v_grants FROM business.order_guest_capability g
    WHERE g.order_id = o.id AND g.capability_digest = p_guest_digest AND g.revoked_at IS NULL
      AND 'read' = ANY (g.allowed_commands);
    IF NOT FOUND THEN
      RETURN NULL;
    END IF;
  END IF;
  v_unresolved := business.order_command_unresolved(o.id);
  SELECT q.expires_at INTO v_quote_expires FROM business.checkout_quote q WHERE q.id = o.quote_id;
  -- payment_initiate and order_free_confirm_request: awaiting payment with
  -- intact holds, nothing in flight, then the quote window and lead time at
  -- the request instant.
  v_open_payment := o.lifecycle = 'awaiting_payment' AND o.recovery_state = 'prepared'
                    AND business.order_holds_intact(o.id) AND v_quote_expires IS NOT NULL;
  RETURN pg_catalog.json_build_object(
    'grants', (SELECT coalesce(pg_catalog.json_agg(g ORDER BY g), '[]'::json)
               FROM pg_catalog.unnest(v_grants) g),
    'pay', CASE WHEN v_open_payment AND o.payment_requirement = 'full'
                     AND NOT EXISTS (SELECT 1 FROM business.payment_collection c
                                     WHERE c.order_id = o.id
                                       AND (c.state IN ('created', 'pending', 'unknown', 'succeeded')
                                            OR c.mismatch_reason IS NOT NULL))
                THEN pg_catalog.json_build_object('before', v_quote_expires,
                                                  'not_after', o.original_handover_at - business.schedule_lead_time()) END,
    'free_confirmation', CASE WHEN v_open_payment AND o.payment_requirement = 'none'
                                   AND NOT EXISTS (SELECT 1 FROM business.order_command c
                                                   JOIN business.recovery_operation r ON r.id = c.id
                                                   WHERE c.order_id = o.id AND c.command = 'free_confirm'
                                                     AND NOT (r.state = 'secured' AND r.outcome ->> 'status' = 'refused'))
                              THEN pg_catalog.json_build_object('before', v_quote_expires,
                                                                'not_after', o.original_handover_at - business.schedule_lead_time())
                              END,
    -- order_cancellation_preview and order_cancel_request: an accepted,
    -- secured order not out for delivery, with no cancellation and no other
    -- protected change in progress.
    'cancel', business.cancellation_eligible(o.lifecycle) AND o.recovery_state = 'secured' AND NOT v_unresolved
              AND NOT business.delivery_dispatched(o)
              AND NOT EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                              WHERE c.order_id = o.id AND c.command = 'cancel'
                                AND NOT (r.state = 'secured' AND r.outcome ->> 'status' = 'refused')),
    -- order_pickup_move_request: collection or own courier, eligible for an
    -- appointment change, secured, nothing in progress, until six hours
    -- before the current start (equality allowed). Whether a later time of
    -- the day is left is the move schedule's (GET /schedule?purpose=pickup_move).
    'move_pickup', CASE WHEN o.fulfillment_mode IN ('collection', 'customer_courier')
                             AND business.appointment_change_eligible(o) AND o.recovery_state = 'secured'
                             AND NOT v_unresolved
                        THEN pg_catalog.json_build_object(
                               'not_after', business.order_handover_at(o) - business.pickup_move_cutoff()) END);
END;
$$;

-- Owner-internal: the owner's summary row of an order from its order_view
-- representation v (order_customer_summary's rendering since 0033, moved
-- here so a page reads order_view once per row).
CREATE FUNCTION business.order_customer_summary_from(p_order business.orders, v jsonb)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'id', v -> 'id', 'version', v -> 'version', 'lifecycle', v -> 'lifecycle',
           'cancel_reason', v -> 'cancel_reason', 'cancelled_at', v -> 'cancelled_at',
           'payment_requirement', v -> 'payment_requirement', 'placed_at', v -> 'placed_at',
           'date', v -> 'date', 'fulfillment_mode', v -> 'fulfillment_mode', 'slot', v -> 'slot',
           'fulfillment_state', v -> 'fulfillment' -> 'state',
           'total_minor', v -> 'total_minor', 'currency', v -> 'currency',
           'payment', CASE WHEN pg_catalog.jsonb_typeof(v -> 'payment') = 'object' THEN pg_catalog.jsonb_build_object(
                        'state', v -> 'payment' -> 'state', 'amount_minor', v -> 'payment' -> 'amount_minor',
                        'expires_at', v -> 'payment' -> 'expires_at',
                        'payment_page_open', pg_catalog.jsonb_typeof(v -> 'payment' -> 'checkout_url') = 'string')
                      ELSE 'null'::jsonb END,
           'free_confirmation', CASE WHEN pg_catalog.jsonb_typeof(v -> 'free_confirmation') = 'object'
                                     THEN (v -> 'free_confirmation') - 'requested_at' ELSE 'null'::jsonb END,
           'cancellation', CASE WHEN pg_catalog.jsonb_typeof(v -> 'cancellation') = 'object'
                                THEN pg_catalog.jsonb_build_object('state', v -> 'cancellation' -> 'state',
                                       'reason', v -> 'cancellation' -> 'reason',
                                       'amount_minor', v -> 'cancellation' -> 'amount_minor')
                                ELSE 'null'::jsonb END,
           'appointment_change', CASE WHEN pg_catalog.jsonb_typeof(v -> 'appointment_change') = 'object'
                                      THEN pg_catalog.jsonb_build_object('kind', v -> 'appointment_change' -> 'kind',
                                             'state', v -> 'appointment_change' -> 'state')
                                      ELSE 'null'::jsonb END,
           'refund', CASE WHEN pg_catalog.jsonb_typeof(v -> 'refund') = 'object'
                          THEN pg_catalog.jsonb_build_object('state', v -> 'refund' -> 'state',
                                 'target_minor', v -> 'refund' -> 'target_minor',
                                 'succeeded_minor', v -> 'refund' -> 'succeeded_minor',
                                 'in_progress_minor', v -> 'refund' -> 'in_progress_minor',
                                 'currency', v -> 'refund' -> 'currency')
                          ELSE 'null'::jsonb END,
           'items', coalesce((SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
                                'cake_name', i -> 'cake_name', 'flavour', i -> 'flavour', 'size_code', i -> 'size_code',
                                'quantity', i -> 'quantity') ORDER BY (i ->> 'line_no')::integer)
                              FROM pg_catalog.jsonb_array_elements(v -> 'items') i), '[]'::jsonb))
         || business.order_owner_source(p_order);
$$;

-- As in 0033 (identical output), through business.order_customer_summary_from.
CREATE OR REPLACE FUNCTION business.order_customer_summary(p_order business.orders)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT business.order_customer_summary_from(p_order, business.order_view(p_order.id)::jsonb);
$$;

-- API: GET /me/orders for the API of this release (see the header, 4):
-- account_order_page's rows and paging, each row with order_blocks (the
-- order's late_payment, delivery, delivery_choice and delivery_failure
-- blocks) that the API reads at its own clock and removes.
CREATE FUNCTION business.account_order_summaries(p_subject uuid, p_after_placed timestamptz, p_after_id uuid, p_limit integer)
RETURNS json
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.account_customer_id(p_subject);
  v_items jsonb;
  v_count integer;
  v_last business.orders%ROWTYPE;
BEGIN
  IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 50 THEN
    RAISE EXCEPTION 'page_limit_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF (p_after_placed IS NULL) <> (p_after_id IS NULL) THEN
    RAISE EXCEPTION 'cursor_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT coalesce(pg_catalog.jsonb_agg(
           business.order_customer_summary_from(o, w.v)
           || pg_catalog.jsonb_build_object('order_blocks', pg_catalog.jsonb_build_object(
                'late_payment', w.v -> 'late_payment', 'delivery', w.v -> 'delivery',
                'delivery_choice', w.v -> 'delivery_choice', 'delivery_failure', w.v -> 'delivery_failure'))
           ORDER BY o.placed_at DESC, o.id DESC), '[]'::jsonb),
         pg_catalog.count(*)
  INTO v_items, v_count
  FROM (SELECT * FROM business.orders x
        WHERE x.owner_account_id = v_account
          AND (p_after_placed IS NULL OR (x.placed_at, x.id) < (p_after_placed, p_after_id))
        ORDER BY x.placed_at DESC, x.id DESC
        LIMIT p_limit + 1) o
  CROSS JOIN LATERAL (SELECT business.order_view(o.id)::jsonb AS v) w;
  IF v_count <= p_limit THEN
    RETURN pg_catalog.json_build_object('items', v_items, 'next', NULL);
  END IF;
  v_items := v_items - p_limit;
  SELECT * INTO v_last FROM business.orders x WHERE x.id = (v_items -> (p_limit - 1) ->> 'id')::uuid;
  RETURN pg_catalog.json_build_object('items', v_items,
                                      'next', pg_catalog.json_build_object('placed_at', v_last.placed_at, 'id', v_last.id));
END;
$$;

-- ------------------------------------------------- replaced functions

-- As in 0037, with the refund block's parts (business.order_refund_customer:
-- order_refund_summary unchanged, parts appended).
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
                     WHERE i.order_id = o.id))
  FROM business.orders o
  CROSS JOIN LATERAL (SELECT business.order_visible_appointment(o) AS a) va
  WHERE o.id = p_order;
$$;

-- As in 0037, and each allocation in the answer carries its kind (cake,
-- delivery, stray). The stored allocations, the reuse comparison and the
-- preview hash are as before (kind is outside the hash, like
-- delivery_pending_minor and unmatched_minor).
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
                                                           'unmatched_minor', v_stray,
                                                           'allocations',
                                                           business.cancellation_allocations_labelled(v_p.allocations)))::json;
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
                                                         'unmatched_minor', v_stray,
                                                         'allocations', business.cancellation_allocations_labelled(v_alloc)))::json;
END;
$$;

-- As in 0037, and a no-show on an order with a stray delivery payment also
-- records stray_before_minor and stray_new_minor (see the header, 5). Other
-- outcomes are byte for byte as before.
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
  v_has_stray boolean;
  v_sbefore bigint := 0;
  v_safter bigint := 0;
  v_strays text := '';
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
  -- What the strays had returned or on its way before (0038): the notice's
  -- refund line is the 50% part, so it leaves out what this no-show newly
  -- reserved for a stray (a stray refund reserved earlier is not new).
  v_has_stray := EXISTS (SELECT 1 FROM business.delivery_payment_outcome d
                         WHERE d.order_id = v.order_id AND d.outcome = 'unmatched');
  IF v_has_stray THEN
    SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_sbefore
    FROM business.delivery_payment_outcome d CROSS JOIN LATERAL business.refund_balance(d.collection_id) b
    WHERE d.order_id = v.order_id AND d.outcome = 'unmatched';
  END IF;
  IF v_target > 0 THEN
    PERFORM business.refund_goal_apply(v.order_id, 'no_show', 'chef', 50::smallint, v_paid, v_target, 'staff',
                                       v.actor_account_id);
    PERFORM business.refund_reserve(v.order_id);
  END IF;
  SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_after
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b WHERE pc.order_id = v.order_id;
  IF v_has_stray THEN
    SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_safter
    FROM business.delivery_payment_outcome d CROSS JOIN LATERAL business.refund_balance(d.collection_id) b
    WHERE d.order_id = v.order_id AND d.outcome = 'unmatched';
    v_strays := ',"stray_before_minor":' || v_sbefore::text || ',"stray_new_minor":' || (v_safter - v_sbefore)::text;
  END IF;
  IF f.id IS NOT NULL THEN
    PERFORM business.audit_record('staff', v.actor_account_id, 'order.no_show', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'rate', 50, 'refund_target_minor', v_target,
                                                               'new_refund_minor', v_after - v_before,
                                                               'cause', f.cause, 'delivery_kept_minor', v_kept));
    RETURN pg_catalog.format(
      '{"status":"no_show","evidence":{"order_version":%s,"rate":50,"paid_minor":%s,"refund_target_minor":%s,'
      '"returned_before_minor":%s,"new_refund_minor":%s,"cause":"%s","delivery_kept_minor":%s%s}}',
      v_o.version + 1, v_paid, v_target, v_before, v_after - v_before, f.cause, v_kept,
      CASE WHEN v_stray > 0 THEN ',"unmatched_minor":' || v_stray::text ELSE '' END || v_strays);
  END IF;
  PERFORM business.audit_record('staff', v.actor_account_id, 'order.no_show', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'rate', 50, 'refund_target_minor', v_target,
                                                             'new_refund_minor', v_after - v_before));
  RETURN pg_catalog.format(
    '{"status":"no_show","evidence":{"order_version":%s,"rate":50,"paid_minor":%s,"refund_target_minor":%s,'
    '"returned_before_minor":%s,"new_refund_minor":%s%s}}',
    v_o.version + 1, v_paid, v_target, v_before, v_after - v_before,
    CASE WHEN v_stray > 0 THEN ',"unmatched_minor":' || v_stray::text ELSE '' END || v_strays);
END;
$$;

-- As in 0037, except the no-show refund line when the outcome records
-- stray_new_minor (0038): the newly reserved amount without what was newly
-- reserved for a stray, and "already refunded" without stray returns. Every
-- other notice, and every outcome without those keys, renders as before.
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
    IF (p_facts ->> 'stray_new_minor') IS NOT NULL THEN
      -- Since 0038 the outcome records what the strays had returned or on
      -- its way before and what this no-show newly reserved for them: the
      -- line is the 50% part, and stray returns never count towards it.
      v_new := GREATEST(0, v_new - (p_facts ->> 'stray_new_minor')::bigint);
      v_before := GREATEST(0, v_before - coalesce((p_facts ->> 'stray_before_minor')::bigint, 0));
    ELSE
      v_new := GREATEST(0, v_new - v_stray);
    END IF;
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

REVOKE ALL ON FUNCTION business.collection_refund_kind(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.cancellation_allocations_labelled(json) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_refund_parts(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_refund_customer(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_customer_actions(uuid, uuid, bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_customer_summary_from(business.orders, jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_order_summaries(uuid, timestamptz, uuid, integer) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_customer_actions(uuid, uuid, bytea) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_order_summaries(uuid, timestamptz, uuid, integer) TO gk_api;
