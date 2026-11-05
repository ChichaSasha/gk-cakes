-- GK-034 restore exercise finding (K11): the hold review answered before
-- the restore freeze.
--
-- order_hold_review_run (0015, 0017) read the freeze only after its outcome
-- branches: an already decided review answered 'done', and an undecided one
-- whose order no longer awaited payment, or had a succeeded collection,
-- recorded 'closed' or 'paid' and answered 'done'. On a frozen (restored)
-- database the worker then completed those jobs, and the closing branches
-- changed the review row. A restore freezes every outgoing job; nothing may
-- progress until reconciliation.
--
-- The review now reads the freeze first, right after identifying the leased
-- job as a hold review (a job of another kind still answers 'not_review'
-- for its own handler) and before any lock, outcome branch or audit: a
-- share-lock read of business.recovery_freeze as the other gateways do, so
-- a concurrent freeze waits for this transaction or is seen here. Frozen
-- answers 'frozen' (the worker defers the job) and changes nothing. The
-- freeze row is written only alone (recovery_set_freeze, the restore load
-- into a fresh target), never by a transaction holding order locks, so
-- reading it before the K00 order locks cannot deadlock. Everything else is
-- as in 0017.
--
-- The other worker begin functions already answer 'frozen' right after
-- identifying their job (order_command_begin, payment_job_begin,
-- refund_job_begin; notification_dispatch_begin after the manager hold,
-- which goes to attention, not completion).
--
-- Expand-only (K09): the replaced function keeps its signature and grants.

SET LOCAL ROLE gk_owner;

-- As in 0017, plus: the restore freeze is read first (see the header).
CREATE OR REPLACE FUNCTION business.order_hold_review_run(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS TABLE (status text, order_id uuid, next_at timestamptz, cleaned integer)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_order uuid := business.order_hold_review_leased(p_job_id, p_worker, p_generation);
  v_o business.orders%ROWTYPE;
  v_r business.order_hold_review%ROWTYPE;
  v_frozen boolean;
  v_cleaned integer := 0;
  v_id uuid;
  v_next timestamptz;
  v_reason text;
  v_collections integer;
BEGIN
  IF v_order IS NULL THEN
    RETURN QUERY SELECT 'not_review'::text, NULL::uuid, NULL::timestamptz, 0;
    RETURN;
  END IF;
  -- K11: a restore freeze stops the review before any outcome, decided or
  -- not (share lock: a concurrent freeze waits for this transaction, or it
  -- is seen here).
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN QUERY SELECT 'frozen'::text, v_order, NULL::timestamptz, 0;
    RETURN;
  END IF;
  -- Lock order (K00): the order, its collections by ID, the review row.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v_order FOR UPDATE;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = v_order ORDER BY pc.id FOR UPDATE;
  SELECT * INTO v_r FROM business.order_hold_review r WHERE r.order_id = v_order FOR UPDATE;
  IF v_r.outcome IS NOT NULL THEN
    RETURN QUERY SELECT 'done'::text, v_order, NULL::timestamptz, 0;
    RETURN;
  END IF;
  IF v_o.lifecycle <> 'awaiting_payment' THEN
    UPDATE business.order_hold_review r SET outcome = 'closed', decided_at = pg_catalog.now(), updated_at = pg_catalog.now()
    WHERE r.order_id = v_order;
    RETURN QUERY SELECT 'done'::text, v_order, NULL::timestamptz, 0;
    RETURN;
  END IF;
  IF EXISTS (SELECT 1 FROM business.payment_collection pc WHERE pc.order_id = v_order AND pc.state = 'succeeded') THEN
    -- Real money: a confirmation or a full return decides, never a release.
    UPDATE business.order_hold_review r SET outcome = 'paid', decided_at = pg_catalog.now(), updated_at = pg_catalog.now()
    WHERE r.order_id = v_order;
    RETURN QUERY SELECT 'done'::text, v_order, NULL::timestamptz, 0;
    RETURN;
  END IF;
  IF pg_catalog.now() < v_r.hold_until THEN
    PERFORM business.order_hold_review_schedule(v_order, v_r.hold_until);
    RETURN QUERY SELECT 'waiting'::text, v_order, v_r.hold_until, 0;
    RETURN;
  END IF;

  -- Never-dispatched cleanup: no Stripe call was ever admitted (admission
  -- records dispatch_started_at under the collection lock held here).
  FOR v_id IN SELECT pc.id FROM business.payment_collection pc
              WHERE pc.order_id = v_order AND pc.state = 'created' AND pc.dispatch_started_at IS NULL
                AND pg_catalog.now() >= GREATEST(pc.initiated_at, pc.created_at) + interval '15 minutes'
              ORDER BY pc.id LOOP
    UPDATE business.payment_collection pc
    SET state = 'cancelled', cancel_reason = 'never_dispatched', version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = v_id;
    PERFORM business.audit_record('system', NULL, 'payment.cancelled', 'order', v_order, NULL,
                                  pg_catalog.json_build_object('collection_id', v_id, 'reason', 'never_dispatched'));
    v_cleaned := v_cleaned + 1;
  END LOOP;

  -- Anything that may still succeed keeps the holds: an admitted creation,
  -- a pending or unknown collection (re-checked every 15 minutes; their own
  -- cancellation brings the review forward), a collection under manager
  -- reconciliation (a mismatch, whatever its state), a young never-admitted
  -- one (until its 15 minutes), an unresolved command on the order.
  SELECT min(CASE WHEN pc.state = 'created' AND pc.dispatch_started_at IS NULL
                  THEN GREATEST(pc.initiated_at, pc.created_at) + interval '15 minutes'
                  ELSE pg_catalog.now() + interval '15 minutes' END)
  INTO v_next
  FROM business.payment_collection pc
  WHERE pc.order_id = v_order AND (pc.state IN ('created', 'pending', 'unknown') OR pc.mismatch_reason IS NOT NULL);
  IF v_next IS NULL AND EXISTS (SELECT 1 FROM business.recovery_operation ro
                                WHERE ro.aggregate_id = v_order AND ro.state <> 'secured') THEN
    v_next := pg_catalog.now() + interval '15 minutes';
  END IF;
  IF v_next IS NOT NULL THEN
    PERFORM business.order_hold_review_schedule(v_order, v_next);
    RETURN QUERY SELECT 'waiting'::text, v_order, GREATEST(v_next, pg_catalog.now()), v_cleaned;
    RETURN;
  END IF;

  -- Proven terminal-unpaid (or never paid) and past the hold: release.
  v_reason := CASE WHEN v_o.payment_requirement = 'none' THEN 'free_not_confirmed' ELSE 'payment_not_completed' END;
  SELECT count(*) INTO v_collections FROM business.payment_collection pc WHERE pc.order_id = v_order;
  PERFORM business.order_holds_release(v_order);
  UPDATE business.orders o
  SET lifecycle = 'cancelled', cancel_reason = v_reason, cancelled_at = pg_catalog.now(), version = o.version + 1,
      updated_at = pg_catalog.now()
  WHERE o.id = v_order;
  UPDATE business.order_hold_review r SET outcome = 'released', decided_at = pg_catalog.now(), updated_at = pg_catalog.now()
  WHERE r.order_id = v_order;
  PERFORM business.audit_record('system', NULL, 'order.released_unpaid', 'order', v_order, NULL,
                                pg_catalog.json_build_object('reason', v_reason, 'collections', v_collections,
                                                             'order_version', v_o.version + 1));
  RETURN QUERY SELECT 'released'::text, v_order, NULL::timestamptz, v_cleaned;
END;
$$;
