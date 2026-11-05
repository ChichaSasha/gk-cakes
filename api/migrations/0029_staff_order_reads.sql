-- Staff order reads: the order list and the order detail the chef UI needs
-- (contracts r6.5 K00 "Capabilities: chef manages policy/catalog/date
-- exceptions/cancellations/refunds/claims; fulfillment can prepare/ready/
-- dispatch/handover only; technical support gets bounded audited access and
-- no routine money/content authority", "staff require current provider
-- session/account plus active locally granted capability and object scope",
-- "404 missing/not-owned"; K09 "Access Elina; Sasha necessary support
-- only"; K11 visible lifecycle; K05 refund state).
--
-- 1. business.order_view(order): the order as business.order_get renders
--    it, without the access check. order_get now checks access and returns
--    it (identical JSON); the visible lifecycle (K11:
--    confirmation_pending / cancellation_pending) and the refund block are
--    owner-internal helpers shared with the staff reads
--    (business.order_visible_lifecycle, business.order_refund_summary).
-- 2. business.order_attention_flags(order): refund_needs_attention (the
--    refund block's needs_attention, even while it is hidden from the
--    customer), payment_unknown (a collection whose dispatch outcome is
--    unknown), payment_mismatch (a collection with contradicting evidence),
--    paid_awaiting_agreement (K04 Q2), refund_quarantine (an unresolved
--    quarantined refund observation).
-- 3. business.order_staff_list (chef, technical_support, fulfillment):
--    orders whose London handover day is in [from, to] (at most 93 days),
--    optionally only some visible lifecycles and only those that need (or
--    do not need) attention, by slot start, placement and ID, keyset after
--    (handover, placed, id), 1 to 100 per page.
-- 4. business.order_staff_get (same roles): the order view plus raw
--    lifecycle and recovery state, attention flags, production state,
--    payment collections, the refund goal, refund operations and
--    observations, order commands (kind, requester kind, receipt, record
--    state, outcome status; never notes or evidence) and an audit summary
--    (action, time, actor kind; never details or actors).
--
-- Redaction by view (the staff member's most privileged capability; chef,
-- then technical_support, then fulfillment). Keys a view may not see are
-- absent, never null.
--   chef: everything above, contact email, different payer name, amounts,
--     provider references (Stripe session, payment intent and refund IDs),
--     resolution notes. Never the customer's checkout URL (a payment link).
--   technical_support (K00 bounded, no money; K09 necessary support only):
--     states, reasons, times, our IDs and the customer's name; no amounts
--     or rates of money, no contact email or payer name, no provider
--     references, no resolution notes.
--   fulfillment (K00 prepare/ready/dispatch/handover only): only orders
--     that were ever visibly confirmed (their production timer was
--     scheduled or production started); other orders are 404 / absent.
--     Name, handover, items (no prices), lifecycle, cancel reason,
--     production and the version for the start command; no money, no
--     contact, no attention flags (the needs_attention filter is 403), no
--     collections, refunds, commands or audit.
--
-- Every read is audited without personal data, as the attention list
-- audits support reads (here every staff view, chef included): the list
-- as 'order.staff_listed' on the reader's account (view, dates, number of
-- items), the detail as 'order.staff_viewed' on the order (view). The
-- detail's audit summary leaves out these read events. Reads are VOLATILE
-- (the audit, and the capability check's row locks).
--
-- Expand-only (K09): new functions; order_get replaced with an unchanged
-- signature and output.

SET LOCAL ROLE gk_owner;

-- Owner-internal: the lifecycle customers see (K11; as in order_get 0026).
CREATE FUNCTION business.order_visible_lifecycle(p_order business.orders)
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
              ELSE p_order.lifecycle END;
$$;

-- Owner-internal: the order's refund block (K05; as in order_get 0026),
-- NULL without a goal. order_view hides it while a cancellation is not
-- secured; the staff attention flag reads it regardless.
CREATE FUNCTION business.order_refund_summary(p_order uuid)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.json_build_object(
           'state', CASE WHEN rf.attention THEN 'needs_attention'
                         WHEN rf.succeeded - rf.unpublished >= g.target_minor THEN 'returned'
                         WHEN rf.succeeded - rf.unpublished > 0 THEN 'partly_returned'
                         WHEN rf.sent OR rf.unpublished > 0 THEN 'pending'
                         ELSE 'requested' END,
           'target_minor', g.target_minor, 'succeeded_minor', rf.succeeded - rf.unpublished,
           'in_progress_minor', rf.reserved + rf.unpublished, 'currency', 'GBP', 'requested_at', g.created_at,
           'goal_version', g.version, 'reason', g.reason, 'policy_rate', g.policy_rate,
           'delivery_pending_minor', business.refund_delivery_pending(g.order_id, g.target_minor))
  FROM business.refund_goal g
  CROSS JOIN LATERAL (
    SELECT coalesce(sum(b.succeeded_minor), 0) AS succeeded,
           business.manual_payout_unpublished(g.order_id) AS unpublished,
           coalesce(sum(b.reserved_minor), 0) AS reserved,
           EXISTS (SELECT 1 FROM business.refund_operation ro
                   WHERE ro.order_id = g.order_id AND ro.state IN ('submitting', 'pending', 'unknown'))
             AS sent,
           (EXISTS (SELECT 1 FROM business.refund_operation ro
                    WHERE ro.order_id = g.order_id
                      AND (ro.state = 'requires_action' OR ro.mismatch_reason IS NOT NULL
                           OR (ro.state IN ('submitting', 'pending', 'unknown')
                               AND EXISTS (SELECT 1 FROM business.recovery_operation r
                                           WHERE r.id = ro.id AND r.state = 'reconcile'))))
            OR coalesce(bool_or(b.quarantined), false)
            OR (EXISTS (SELECT 1 FROM business.refund_operation ro
                        WHERE ro.order_id = g.order_id AND ro.state = 'failed')
                AND coalesce(sum(b.succeeded_minor + b.reserved_minor), 0)
                    + business.refund_delivery_pending(g.order_id, g.target_minor) < g.target_minor))
             AS attention
    FROM business.payment_collection pc
    CROSS JOIN LATERAL business.refund_balance(pc.id) b
    WHERE pc.order_id = g.order_id) rf
  WHERE g.order_id = p_order;
$$;

-- Owner-internal: the order as order_get renders it, without any access
-- check (NULL for an unknown order). Callers check access first.
CREATE FUNCTION business.order_view(p_order uuid)
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
           'placed_at', o.placed_at, 'date', o.handover_day, 'fulfillment_mode', o.fulfillment_mode,
           'slot', pg_catalog.json_build_object('id', o.slot_id, 'starts_at', o.original_handover_at, 'ends_at', o.handover_ends_at),
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
           'refund', CASE WHEN o.recovery_state <> 'cancellation_pending' THEN business.order_refund_summary(o.id) END,
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
  WHERE o.id = p_order;
$$;

-- As in 0026 (identical output), through business.order_view.
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
  SELECT business.order_view(o.id)
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

-- Owner-internal: the order's attention flags (see the header, 2), in a
-- fixed order.
CREATE FUNCTION business.order_attention_flags(p_order business.orders)
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
                THEN 'refund_quarantine' END
         ]::text[], NULL);
$$;

-- Owner-internal: the production start (K05): started (the one
-- transition), scheduled (the timer is waiting) or not_scheduled.
CREATE FUNCTION business.order_production_view(p_order uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'state', CASE WHEN t.order_id IS NOT NULL THEN 'started'
                         WHEN j.state IN ('queued', 'retry', 'running', 'attention') THEN 'scheduled'
                         ELSE 'not_scheduled' END,
           'scheduled_at', coalesce(t.scheduled_at, pj.scheduled_at),
           'started_at', t.effective_at, 'observed_at', t.observed_at, 'source', t.source)
  FROM (SELECT p_order AS id) x
  LEFT JOIN business.production_transition t ON t.order_id = x.id
  LEFT JOIN business.production_timer_job pj ON pj.order_id = x.id
  LEFT JOIN business.job j ON j.id = pj.job_id;
$$;

-- Owner-internal: whether the order was ever visibly confirmed (the
-- fulfillment scope): its production timer was scheduled (when it became
-- confirmed and secured) or production started.
CREATE FUNCTION business.order_fulfillment_scope(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.production_timer_job pj WHERE pj.order_id = p_order)
      OR EXISTS (SELECT 1 FROM business.production_transition t WHERE t.order_id = p_order);
$$;

-- Owner-internal: the staff view of an account: chef, technical_support or
-- fulfillment (its most privileged active capability), NULL without one.
CREATE FUNCTION business.order_staff_view_of(p_account uuid)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN bool_or(c.capability = 'chef') THEN 'chef'
              WHEN bool_or(c.capability = 'technical_support') THEN 'technical_support'
              WHEN bool_or(c.capability = 'fulfillment') THEN 'fulfillment' END
  FROM business.staff_capability c
  WHERE c.account_id = p_account AND c.revoked_at IS NULL;
$$;

-- Owner-internal: the order's items without prices (every view).
CREATE FUNCTION business.order_staff_items(p_order uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
           'line_no', i.line_no, 'variant_id', i.variant_id, 'lot_id', i.physical_lot_id,
           'recipe_version_id', i.recipe_version_id, 'cake_name', c.name, 'flavour', r.flavour_name,
           'size_code', r.size_code, 'quantity', i.quantity) ORDER BY i.line_no), '[]'::jsonb)
  FROM business.order_item i
  JOIN business.variant v ON v.id = i.variant_id
  JOIN business.cake c ON c.id = v.cake_id
  JOIN business.recipe_version r ON r.id = i.recipe_version_id
  WHERE i.order_id = p_order;
$$;

-- Owner-internal: one list row for a view (see the header, redaction).
CREATE FUNCTION business.order_staff_row(p_order business.orders, p_view text)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'id', p_order.id, 'version', p_order.version,
           'lifecycle', business.order_visible_lifecycle(p_order), 'raw_lifecycle', p_order.lifecycle,
           'date', p_order.handover_day, 'fulfillment_mode', p_order.fulfillment_mode,
           'slot', pg_catalog.jsonb_build_object('id', p_order.slot_id, 'starts_at', p_order.original_handover_at,
                                                 'ends_at', p_order.handover_ends_at),
           'placed_at', p_order.placed_at,
           'items', business.order_staff_items(p_order.id),
           'customer_full_name', p_order.customer_full_name,
           'cancel_reason', CASE WHEN p_order.recovery_state <> 'cancellation_pending' THEN p_order.cancel_reason END,
           'production', business.order_production_view(p_order.id))
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
                               THEN business.order_refund_summary(p_order.id)::jsonb END)
            ELSE '{}'::jsonb END;
$$;

-- The staff order list (see the header, 3). The keyset is (slot start,
-- placement, ID) after p_after_*; all three or none. Returns
-- {items, next: {handover_at, placed_at, id} | null}.
CREATE FUNCTION business.order_staff_list(p_actor uuid, p_from date, p_to date, p_lifecycles text[],
                                          p_needs_attention boolean, p_after_handover timestamptz,
                                          p_after_placed timestamptz, p_after_id uuid, p_limit integer)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support', 'fulfillment']);
  v_view text := business.order_staff_view_of(v_account);
  v_items jsonb;
  v_next jsonb;
  v_count integer;
BEGIN
  IF p_from IS NULL OR p_to IS NULL OR p_to < p_from OR p_to - p_from > 92 THEN
    RAISE EXCEPTION 'range_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_limit IS NULL OR p_limit < 1 OR p_limit > 100 THEN
    RAISE EXCEPTION 'page_limit_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_lifecycles IS NOT NULL
     AND (pg_catalog.cardinality(p_lifecycles) NOT BETWEEN 1 AND 9
          OR NOT p_lifecycles <@ ARRAY['awaiting_payment', 'confirmation_pending', 'paid_awaiting_agreement', 'confirmed',
                                       'in_production', 'ready', 'completed', 'cancellation_pending', 'cancelled']) THEN
    RAISE EXCEPTION 'lifecycle_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF NOT ((p_after_handover IS NULL AND p_after_placed IS NULL AND p_after_id IS NULL)
          OR (p_after_handover IS NOT NULL AND p_after_placed IS NOT NULL AND p_after_id IS NOT NULL)) THEN
    RAISE EXCEPTION 'cursor_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF v_view = 'fulfillment' AND p_needs_attention IS NOT NULL THEN
    RAISE EXCEPTION 'the needs_attention filter requires the chef or technical_support capability' USING ERRCODE = 'GK403';
  END IF;
  WITH base AS (
    SELECT o, business.order_visible_lifecycle(o) AS shown,
           pg_catalog.cardinality(business.order_attention_flags(o)) > 0 AS attention
    FROM business.orders o
    WHERE o.handover_day BETWEEN p_from AND p_to
      AND (v_view <> 'fulfillment' OR business.order_fulfillment_scope(o.id))
  ), page AS (
    SELECT b.o, pg_catalog.row_number() OVER (ORDER BY (b.o).original_handover_at, (b.o).placed_at, (b.o).id) AS n
    FROM base b
    WHERE (p_lifecycles IS NULL OR b.shown = ANY (p_lifecycles))
      AND (p_needs_attention IS NULL OR b.attention = p_needs_attention)
      AND (p_after_id IS NULL
           OR ((b.o).original_handover_at, (b.o).placed_at, (b.o).id) > (p_after_handover, p_after_placed, p_after_id))
    ORDER BY (b.o).original_handover_at, (b.o).placed_at, (b.o).id
    LIMIT p_limit + 1
  )
  SELECT coalesce(pg_catalog.jsonb_agg(business.order_staff_row(p.o, v_view) ORDER BY p.n)
                    FILTER (WHERE p.n <= p_limit), '[]'::jsonb),
         CASE WHEN pg_catalog.max(p.n) > p_limit THEN
           (SELECT pg_catalog.jsonb_build_object('handover_at', (l.o).original_handover_at, 'placed_at', (l.o).placed_at,
                                                 'id', (l.o).id)
            FROM page l WHERE l.n = p_limit) END
  INTO v_items, v_next
  FROM page p;
  v_count := pg_catalog.jsonb_array_length(v_items);
  PERFORM business.audit_record('staff', v_account, 'order.staff_listed', 'account', v_account, NULL,
                                pg_catalog.json_build_object('view', v_view, 'from', p_from, 'to', p_to, 'items', v_count));
  RETURN pg_catalog.json_build_object('items', v_items, 'next', v_next);
END;
$$;

-- Owner-internal: the order's payment collections for a view.
CREATE FUNCTION business.order_staff_collections(p_order uuid, p_chef boolean)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(
           pg_catalog.jsonb_build_object(
             'id', pc.id, 'kind', pc.kind, 'state', pc.state, 'version', pc.version,
             'initiated_at', pc.initiated_at, 'expires_at', pc.provider_expires_at, 'succeeded_at', pc.succeeded_at,
             'mismatch_reason', pc.mismatch_reason, 'cancel_reason', pc.cancel_reason)
           || CASE WHEN p_chef THEN
                pg_catalog.jsonb_build_object(
                  'amount_minor', pc.amount_minor, 'currency', pc.currency,
                  'refunded_minor', b.succeeded_minor, 'refund_reserved_minor', b.reserved_minor,
                  'provider_session_id', pc.provider_session_id, 'payment_intent_id', pc.payment_intent_id)
              ELSE '{}'::jsonb END
           ORDER BY pc.created_at, pc.id), '[]'::jsonb)
  FROM business.payment_collection pc
  CROSS JOIN LATERAL business.refund_balance(pc.id) b
  WHERE pc.order_id = p_order;
$$;

-- Owner-internal: the order's refund goal for a view (NULL without one).
CREATE FUNCTION business.order_staff_refund_goal(p_order uuid, p_chef boolean)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'id', g.id, 'version', g.version, 'reason', g.reason, 'source', g.source,
           'created_at', g.created_at, 'updated_at', g.updated_at)
         || CASE WHEN p_chef THEN
              pg_catalog.jsonb_build_object(
                'target_minor', g.target_minor, 'policy_rate', g.policy_rate, 'paid_basis_minor', g.paid_basis_minor,
                'policy_target_minor', g.policy_target_minor, 'remaining_minor', business.refund_goal_remaining(g.order_id),
                'delivery_pending_minor', business.refund_delivery_pending(g.order_id, g.target_minor), 'currency', 'GBP')
            ELSE '{}'::jsonb END
  FROM business.refund_goal g
  WHERE g.order_id = p_order;
$$;

-- Owner-internal: the order's refund operations for a view; record_state
-- is the K11 dispatch record's state.
CREATE FUNCTION business.order_staff_refund_operations(p_order uuid, p_chef boolean)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(
           pg_catalog.jsonb_build_object(
             'id', ro.id, 'collection_id', ro.collection_id, 'channel', ro.channel, 'state', ro.state,
             'version', ro.version, 'goal_version', ro.goal_version, 'failure_reason', ro.failure_reason,
             'mismatch_reason', ro.mismatch_reason, 'provider_status', ro.provider_status,
             'record_state', r.state,
             'window_ended', CASE WHEN ro.replay_until IS NOT NULL THEN pg_catalog.now() >= ro.replay_until END,
             'created_at', ro.created_at, 'settled_at', ro.settled_at)
           || CASE WHEN p_chef THEN
                pg_catalog.jsonb_build_object('amount_minor', ro.amount_minor, 'currency', ro.currency,
                                              'provider_refund_id', ro.provider_refund_id)
              ELSE '{}'::jsonb END
           ORDER BY ro.created_at, ro.id), '[]'::jsonb)
  FROM business.refund_operation ro
  LEFT JOIN business.recovery_operation r ON r.id = ro.id
  WHERE ro.order_id = p_order;
$$;

-- Owner-internal: refund observations on the order's collections for a
-- view (quarantine included).
CREATE FUNCTION business.order_staff_refund_observations(p_order uuid, p_chef boolean)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(
           pg_catalog.jsonb_build_object(
             'id', b.id, 'collection_id', b.collection_id, 'status', b.status, 'source', b.source,
             'quarantined', b.quarantined, 'quarantine_reason', b.quarantine_reason,
             'matched_operation_id', b.matched_operation_id, 'named_operation_id', b.named_operation_id,
             'observed_at', b.observed_at, 'resolved_at', b.resolved_at)
           || CASE WHEN p_chef THEN
                pg_catalog.jsonb_build_object('amount_minor', b.amount_minor, 'currency', b.currency,
                                              'provider_refund_id', b.provider_refund_id,
                                              'resolution_note', b.resolution_note)
              ELSE '{}'::jsonb END
           ORDER BY b.observed_at, b.id), '[]'::jsonb)
  FROM business.refund_observation b
  JOIN business.payment_collection pc ON pc.id = b.collection_id
  WHERE pc.order_id = p_order;
$$;

-- Owner-internal: the order's K11 commands: kind, requester kind, receipt,
-- record state, outcome status (and a late-payment decision); never notes,
-- evidence or payloads.
CREATE FUNCTION business.order_staff_commands(p_order uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
           'id', c.id, 'kind', c.command, 'requested_by', c.actor_kind, 'received_at', c.received_at,
           'state', r.state,
           'outcome_status', CASE WHEN r.state IN ('committed', 'secured') THEN r.outcome ->> 'status' END,
           'decision', d.decision)
           ORDER BY c.created_at, c.id), '[]'::jsonb)
  FROM business.order_command c
  JOIN business.recovery_operation r ON r.id = c.id
  LEFT JOIN business.late_payment_decision d ON d.command_id = c.id
  WHERE c.order_id = p_order;
$$;

-- Owner-internal: the order's audit summary (action, time, actor kind; the
-- latest 200 in time order), without the staff read events.
CREATE FUNCTION business.order_staff_audit(p_order uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
           'action', e.action, 'occurred_at', e.occurred_at, 'actor_kind', e.actor_kind)
           ORDER BY e.occurred_at, e.id), '[]'::jsonb)
  FROM (SELECT a.id, a.action, a.occurred_at, a.actor_kind
        FROM business.audit_event a
        WHERE a.subject_kind = 'order' AND a.subject_id = p_order AND a.action <> 'order.staff_viewed'
        ORDER BY a.occurred_at DESC, a.id DESC
        LIMIT 200) e;
$$;

-- The staff order detail (see the header, 4).
CREATE FUNCTION business.order_staff_get(p_actor uuid, p_order uuid)
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
                  'audit', business.order_staff_audit(o.id));
  ELSE
    v_result := pg_catalog.jsonb_build_object(
                  'id', v -> 'id', 'version', v -> 'version', 'lifecycle', v -> 'lifecycle',
                  'raw_lifecycle', o.lifecycle, 'cancel_reason', v -> 'cancel_reason', 'cancelled_at', v -> 'cancelled_at',
                  'placed_at', v -> 'placed_at', 'date', v -> 'date', 'fulfillment_mode', v -> 'fulfillment_mode',
                  'slot', v -> 'slot', 'customer_full_name', v -> 'customer_full_name',
                  'items', business.order_staff_items(o.id),
                  'production', business.order_production_view(o.id));
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.staff_viewed', 'order', o.id, NULL,
                                pg_catalog.json_build_object('view', v_view));
  RETURN v_result::json;
END;
$$;

REVOKE ALL ON FUNCTION business.order_visible_lifecycle(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_refund_summary(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_view(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_attention_flags(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_production_view(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_fulfillment_scope(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_view_of(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_items(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_row(business.orders, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_list(uuid, date, date, text[], boolean, timestamptz, timestamptz, uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_collections(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_refund_goal(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_refund_operations(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_refund_observations(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_commands(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_audit(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_get(uuid, uuid) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_staff_list(uuid, date, date, text[], boolean, timestamptz, timestamptz, uuid, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_staff_get(uuid, uuid) TO gk_api;
