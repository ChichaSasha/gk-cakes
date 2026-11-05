-- GK-026 B2 follow-ups of B1 (contracts r6.5 K05; review of PR #38).
--
-- 1. order_get: a refund reads needs_attention after a failed operation only
--    when the goal still lacks money beyond what is returned, reserved and
--    the delivery share visibly waiting for the chef
--    (delivery_pending_minor). A pending chef delivery action is not a
--    failure needing a person (K05 "cake cancellation records delivery
--    obligation immediately and makes it visible pending chef action").
-- 2. refund_goal_apply: raising only the policy target (a cancellation
--    whose policy amount is already covered by a floor or an earlier goal)
--    is audited as refund.policy_target_raised (K05 "Keep reason and
--    original policy target visible"; K08 audit).
-- 3. order_late_payment_commit: the commit-time honour_too_late refusal is
--    audited like the other refusals (order.late_payment_refused).
--
-- Expand-only (K09): functions replaced with unchanged signatures.

SET LOCAL ROLE gk_owner;

-- As in 0022, plus the audit of a policy target raised alone.
CREATE OR REPLACE FUNCTION business.refund_goal_apply(p_order uuid, p_reason text, p_source text, p_rate smallint, p_paid bigint,
                                           p_policy bigint, p_actor_kind text, p_account uuid)
RETURNS business.refund_goal
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_g business.refund_goal%ROWTYPE;
  v_policy bigint;
  v_target bigint;
  v_prior_policy bigint;
BEGIN
  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    v_policy := coalesce(p_policy, 0);
    v_target := business.refund_goal_compute(p_order, v_policy);
    IF v_target <= 0 THEN
      RETURN NULL;
    END IF;
    INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source, policy_rate, paid_basis_minor,
                                      policy_target_minor)
    VALUES (pg_catalog.gen_random_uuid(), p_order, v_target, coalesce(p_reason, 'delivery_failure'),
            CASE WHEN p_policy IS NULL THEN 'chef' ELSE p_source END,
            CASE WHEN p_policy IS NULL THEN NULL ELSE p_rate END, CASE WHEN p_policy IS NULL THEN NULL ELSE p_paid END,
            v_policy)
    RETURNING * INTO v_g;
    PERFORM business.audit_record(p_actor_kind, p_account, 'refund.goal_recorded', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_target, 'policy_target_minor', v_policy,
                                                               'reason', v_g.reason, 'rate', v_g.policy_rate));
    RETURN v_g;
  END IF;
  v_policy := coalesce(v_g.policy_target_minor, v_g.target_minor);
  v_prior_policy := v_policy;
  IF p_policy IS NOT NULL AND p_policy > v_policy THEN
    v_policy := p_policy;
    UPDATE business.refund_goal g
    SET reason = p_reason, source = p_source, policy_rate = p_rate, paid_basis_minor = p_paid, policy_target_minor = v_policy,
        updated_at = pg_catalog.now()
    WHERE g.id = v_g.id RETURNING * INTO v_g;
  ELSIF v_g.policy_target_minor IS NULL THEN
    UPDATE business.refund_goal g SET policy_target_minor = v_policy WHERE g.id = v_g.id RETURNING * INTO v_g;
  END IF;
  v_target := business.refund_goal_compute(p_order, v_policy);
  -- A raised policy target is audited even when the goal itself does not
  -- move (a floor or an earlier exception already covers it): the reason
  -- and original policy target stay visible (K05).
  IF v_policy > v_prior_policy AND v_target <= v_g.target_minor THEN
    PERFORM business.audit_record(p_actor_kind, p_account, 'refund.policy_target_raised', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_g.target_minor,
                                                               'policy_target_minor', v_policy,
                                                               'prior_policy_target_minor', v_prior_policy,
                                                               'reason', v_g.reason, 'rate', v_g.policy_rate));
  END IF;
  IF v_target > v_g.target_minor THEN
    UPDATE business.refund_goal g SET target_minor = v_target, version = g.version + 1, updated_at = pg_catalog.now()
    WHERE g.id = v_g.id RETURNING * INTO v_g;
    PERFORM business.audit_record(p_actor_kind, p_account, 'refund.goal_raised', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_target, 'policy_target_minor', v_policy,
                                                               'reason', v_g.reason));
  END IF;
  RETURN v_g;
END;
$$;

-- As in 0022, plus the audit of the commit-time honour_too_late refusal.
CREATE OR REPLACE FUNCTION business.order_late_payment_commit(v business.order_command)
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
  IF v_o.original_handover_at <= v.received_at THEN
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.late_payment_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', 'honour_too_late'));
    RETURN '{"status":"refused","evidence":{"reason":"honour_too_late"}}';
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

-- As in 0022, except that a pending chef delivery share is not a reason for
-- needs_attention after a failed operation.
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
           -- confirmation being secured"), is confirmation_pending; an
           -- unresolved or unsecured cancellation is cancellation_pending.
           'lifecycle', CASE WHEN o.recovery_state = 'confirmation_pending'
                               OR (o.lifecycle = 'awaiting_payment'
                                   AND EXISTS (SELECT 1 FROM business.order_command pcmd
                                               JOIN business.recovery_operation pr ON pr.id = pcmd.id
                                               WHERE pcmd.order_id = o.id AND pcmd.command = 'paid_confirm'
                                                 AND pr.state <> 'secured'))
                             THEN 'confirmation_pending'
                             WHEN o.recovery_state = 'cancellation_pending'
                               OR EXISTS (SELECT 1 FROM business.order_command ccmd
                                          JOIN business.recovery_operation cr ON cr.id = ccmd.id
                                          LEFT JOIN business.late_payment_decision cd ON cd.command_id = ccmd.id
                                          WHERE ccmd.order_id = o.id AND cr.state <> 'secured'
                                            AND (ccmd.command = 'cancel' OR cd.decision = 'full_return'))
                             THEN 'cancellation_pending'
                             ELSE o.lifecycle END,
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
           -- The return (K05): requested until Stripe has it, pending while
           -- it is being processed or checked, returned only when every
           -- penny of the goal succeeded; never returned while unresolved.
           -- Hidden while the cancellation that recorded it is not secured.
           'refund', (SELECT pg_catalog.json_build_object(
                        'state', CASE WHEN rf.attention THEN 'needs_attention'
                                      WHEN rf.succeeded >= g.target_minor THEN 'returned'
                                      WHEN rf.succeeded > 0 THEN 'partly_returned'
                                      WHEN rf.sent THEN 'pending'
                                      ELSE 'requested' END,
                        'target_minor', g.target_minor, 'succeeded_minor', rf.succeeded,
                        'in_progress_minor', rf.reserved, 'currency', 'GBP', 'requested_at', g.created_at,
                        'goal_version', g.version, 'reason', g.reason, 'policy_rate', g.policy_rate,
                        'delivery_pending_minor', business.refund_delivery_pending(g.order_id, g.target_minor))
                      FROM business.refund_goal g
                      CROSS JOIN LATERAL (
                        SELECT coalesce(sum(b.succeeded_minor), 0) AS succeeded,
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
                      WHERE g.order_id = o.id AND o.recovery_state <> 'cancellation_pending'),
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
