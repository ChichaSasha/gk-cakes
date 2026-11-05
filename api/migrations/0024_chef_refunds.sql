-- GK-026 B2 part 1: the chef's bounded partial refund goal and the chef's
-- payment of a pending delivery share (contracts r6.5 K05 "chef may create
-- bounded partial refund goal for agreed exception,
-- amount/allocations/reason/version/key mandatory. Separate capability for
-- chef cancellation, refunds and recovery. Additional goodwill cannot
-- exceed actual collected funds through refund API", "Chef-only delivery
-- refund remains separate operation; cake cancellation records delivery
-- obligation immediately and makes it visible pending chef action",
-- "quarantine blocks new dispatch", "At most one unresolved refund
-- operation per collection"; K00 capabilities, lock order; K11).
--
-- Agreed exception (business.refund_exception_record, chef). The chef names
-- an amount A (GBP pence), its allocations over the order's succeeded
-- collections (each > 0, distinct, summing to A) and a reason, at the order
-- version the chef saw, with an Idempotency-Key. Under the order lock:
--   * refused 409 reconciliation_required while the order has an
--     unresolved quarantined refund observation, contradicting evidence on
--     a refund operation or payment collection, a refund operation waiting
--     for a person (requires_action, or unresolved with its K11 dispatch in
--     reconcile), or a payment collection whose outcome is unknown;
--   * refused 409 refund_in_progress while any refund operation of the
--     order is unresolved: the explicit allocation is reserved against
--     settled balances only, so a later automatic reservation never splits
--     it (K05 "No deliberate 20+30 split");
--   * refused 409 refund_limit when A exceeds the order's actual collected
--     funds net of every succeeded return and reservation (paid - S - R) or
--     an allocation exceeds its collection's room (C - S - R).
-- Then the goal is raised by the exception: target = min(paid, max(target,
-- S + R) + A), version + 1 (a new goal has reason chef_exception, source
-- chef); the exception is stored immutably with its reason
-- (business.refund_exception); exactly the named allocations are reserved
-- as refund operations under the new goal version (the existing ledger,
-- K11 dispatch protection per operation as before) and the order version
-- is bumped (a held cancellation preview must be refreshed). An exception
-- is an obligation on top of what is owed: a pending ordinary obligation
-- (for example a delivery share) stays pending. The reason is kept out of
-- the audit detail.
--
-- Pending delivery share (business.refund_delivery_pay, chef). Reserves the
-- delivery share of the current goal that business.refund_delivery_pending
-- reports for one unfloored succeeded delivery collection: min(its share -
-- its S + R, its room, what the goal still needs after everything returned,
-- reserved or automatically reservable). Same reconciliation and
-- in-progress refusals (the latter for that collection); 409
-- no_refund_pending when nothing is pending. The order version is bumped.
--
-- Both: chef only (GK403 otherwise); expected_version (409 stale_version);
-- the same key and body replays (also during a restore freeze), another
-- body is key_reused; a new request during a restore freeze is GK503 and
-- records nothing (freeze first, after the replay); an order with an
-- unsecured K11 command or an unresolved cancellation or late-payment
-- decision is 409 state_conflict. Not K11 order commands themselves: they
-- change no customer commitment, and each reserved operation is protected
-- under K11 before its first Stripe call (0016).
--
-- Expand-only (K09): a new table, a replaced CHECK constraint pair and new
-- functions. Lock order (K00): the order, its collections by ID, the goal,
-- refund operations.

SET LOCAL ROLE gk_owner;

ALTER TABLE business.refund_goal DROP CONSTRAINT refund_goal_reason_check;
ALTER TABLE business.refund_goal
  ADD CONSTRAINT refund_goal_reason_check CHECK (reason IN (
    'late_capture_terminal', 'customer_cancelled', 'chef_cancelled', 'late_payment_returned', 'delivery_failure',
    'chef_exception'));
ALTER TABLE business.refund_goal DROP CONSTRAINT refund_goal_policy;
ALTER TABLE business.refund_goal
  ADD CONSTRAINT refund_goal_policy CHECK ((policy_rate IS NULL) = (paid_basis_minor IS NULL)
                                          AND (policy_rate IS NULL) = (reason IN ('late_capture_terminal', 'delivery_failure',
                                                                                  'chef_exception')));

-- One agreed exception (see the header). Immutable.
CREATE TABLE business.refund_exception (
  id             uuid PRIMARY KEY,
  order_id       uuid NOT NULL REFERENCES business.orders (id),
  goal_id        uuid NOT NULL REFERENCES business.refund_goal (id),
  -- The goal version and target the exception produced.
  goal_version   integer NOT NULL CHECK (goal_version >= 1),
  target_minor   bigint NOT NULL CHECK (target_minor > 0),
  amount_minor   bigint NOT NULL CHECK (amount_minor > 0),
  currency       text NOT NULL DEFAULT 'GBP' CHECK (currency = 'GBP'),
  -- [{collection_id, amount_minor, refund_operation_id}] in collection order.
  allocations    json NOT NULL CHECK (octet_length(allocations::text) <= 4096),
  reason         text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
  order_version  integer NOT NULL CHECK (order_version >= 1),
  recorded_by    uuid NOT NULL REFERENCES business.account (id),
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX refund_exception_order_idx ON business.refund_exception (order_id, created_at);
CREATE TRIGGER refund_exception_immutable BEFORE UPDATE OR DELETE ON business.refund_exception
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER refund_exception_no_truncate BEFORE TRUNCATE ON business.refund_exception
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: whether the order has a refund or payment fact that needs
-- reconciliation by a person before new refund money is committed (see the
-- header).
CREATE FUNCTION business.refund_reconciliation_required(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.refund_observation b
                 JOIN business.payment_collection pc ON pc.id = b.collection_id
                 WHERE pc.order_id = p_order AND b.matched_operation_id IS NULL AND b.resolved_at IS NULL)
      OR EXISTS (SELECT 1 FROM business.refund_operation ro
                 WHERE ro.order_id = p_order
                   AND (ro.mismatch_reason IS NOT NULL OR ro.state = 'requires_action'
                        OR (ro.state IN ('reserved', 'submitting', 'pending', 'unknown')
                            AND EXISTS (SELECT 1 FROM business.recovery_operation r
                                        WHERE r.id = ro.id AND r.state = 'reconcile'))))
      OR EXISTS (SELECT 1 FROM business.payment_collection pc
                 WHERE pc.order_id = p_order AND (pc.mismatch_reason IS NOT NULL OR pc.state = 'unknown'));
$$;

-- Owner-internal: the common admission of the chef's refund commands on
-- order p_order (the caller validated its input and claimed the key):
-- freeze first, the order and its collections locked, the version and
-- state checked, reconciliation refused. Returns the order.
CREATE FUNCTION business.refund_chef_admit(p_order uuid, p_expected_version integer)
RETURNS business.orders
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_frozen boolean;
  v_o business.orders%ROWTYPE;
BEGIN
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured' OR business.order_terminal_pending(p_order)
     OR EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                WHERE c.order_id = p_order AND r.state <> 'secured') THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF business.refund_reconciliation_required(p_order) THEN
    RAISE EXCEPTION 'reconciliation_required' USING ERRCODE = 'GK409';
  END IF;
  RETURN v_o;
END;
$$;

-- Owner-internal: reserve one refund operation of p_amount from collection
-- p_collection under goal p_goal (locked by the caller), with its dispatch
-- job and audit. Returns the operation.
CREATE FUNCTION business.refund_reserve_one(p_order uuid, p_collection uuid, p_goal business.refund_goal, p_amount bigint)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  c business.payment_collection%ROWTYPE;
  v_op uuid := pg_catalog.gen_random_uuid();
  v_job uuid;
BEGIN
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = p_collection;
  INSERT INTO business.refund_operation (id, order_id, collection_id, goal_id, goal_version, amount_minor, environment,
                                         provider_account_id, livemode, payment_intent_id)
  VALUES (v_op, p_order, c.id, p_goal.id, p_goal.version, p_amount, c.environment, c.provider_account_id, c.livemode,
          c.payment_intent_id);
  v_job := business.job_enqueue(v_op, NULL);
  INSERT INTO business.refund_job (job_id, collection_id, refund_operation_id, purpose)
  VALUES (v_job, c.id, v_op, 'dispatch');
  PERFORM business.audit_record('system', NULL, 'refund.reserved', 'order', p_order, NULL,
                                pg_catalog.json_build_object('refund_operation_id', v_op, 'collection_id', c.id,
                                                             'amount_minor', p_amount, 'goal_version', p_goal.version));
  RETURN v_op;
END;
$$;

-- API (chef): record an agreed exception (see the header). Returns the
-- exception and whether it is a replay.
CREATE FUNCTION business.refund_exception_record(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                                 p_expected_version integer, p_amount bigint, p_currency text,
                                                 p_collections uuid[], p_amounts bigint[], p_reason text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  v_o business.orders%ROWTYPE;
  v_g business.refund_goal%ROWTYPE;
  v_paid bigint;
  v_returned bigint;
  v_target bigint;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_allocs json;
  v_ops jsonb := '[]'::jsonb;
  v_op uuid;
  a record;
BEGIN
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_amount IS NULL OR p_amount < 1 OR p_amount > 100000000 THEN
    RAISE EXCEPTION 'amount_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_currency IS DISTINCT FROM 'GBP' THEN
    RAISE EXCEPTION 'currency_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_collections IS NULL OR p_amounts IS NULL OR pg_catalog.cardinality(p_collections) NOT BETWEEN 1 AND 10
     OR pg_catalog.cardinality(p_collections) <> pg_catalog.cardinality(p_amounts)
     OR pg_catalog.array_position(p_collections, NULL) IS NOT NULL OR pg_catalog.array_position(p_amounts, NULL) IS NOT NULL
     OR EXISTS (SELECT 1 FROM pg_catalog.unnest(p_amounts) x WHERE x < 1)
     OR (SELECT count(DISTINCT x) FROM pg_catalog.unnest(p_collections) x) <> pg_catalog.cardinality(p_collections)
     OR (SELECT sum(x) FROM pg_catalog.unnest(p_amounts) x) <> p_amount THEN
    RAISE EXCEPTION 'allocations_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_reason IS NULL OR p_reason <> pg_catalog.btrim(p_reason) OR pg_catalog.char_length(p_reason) NOT BETWEEN 1 AND 500
     OR p_reason ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]' THEN
    RAISE EXCEPTION 'reason_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- The same key replays even during a restore freeze: it changes nothing.
  v_prior := business.command_claim(v_account, 'refund.exception', p_order::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  v_o := business.refund_chef_admit(p_order, p_expected_version);
  IF EXISTS (SELECT 1 FROM pg_catalog.unnest(p_collections) x
             WHERE NOT EXISTS (SELECT 1 FROM business.payment_collection pc WHERE pc.id = x AND pc.order_id = p_order)) THEN
    RAISE EXCEPTION 'allocations_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF EXISTS (SELECT 1 FROM business.refund_operation ro
             WHERE ro.order_id = p_order AND ro.state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action')) THEN
    RAISE EXCEPTION 'refund_in_progress' USING ERRCODE = 'GK409';
  END IF;
  v_paid := business.order_paid_minor(p_order);
  SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_returned
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b
  WHERE pc.order_id = p_order;
  IF p_amount > v_paid - v_returned
     OR EXISTS (SELECT 1 FROM ROWS FROM (pg_catalog.unnest(p_collections), pg_catalog.unnest(p_amounts)) AS x(cid, amt)
                CROSS JOIN LATERAL business.refund_balance(x.cid) b
                WHERE x.amt > b.room_minor) THEN
    RAISE EXCEPTION 'refund_limit' USING ERRCODE = 'GK409';
  END IF;

  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    v_target := LEAST(v_paid, v_returned + p_amount);
    INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source, policy_target_minor)
    VALUES (pg_catalog.gen_random_uuid(), p_order, v_target, 'chef_exception', 'chef', 0)
    RETURNING * INTO v_g;
    PERFORM business.audit_record('staff', v_account, 'refund.goal_recorded', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_target, 'policy_target_minor', 0,
                                                               'reason', v_g.reason, 'exception_id', v_id));
  ELSE
    v_target := LEAST(v_paid, GREATEST(v_g.target_minor, v_returned) + p_amount);
    UPDATE business.refund_goal g SET target_minor = v_target, version = g.version + 1, updated_at = pg_catalog.now()
    WHERE g.id = v_g.id RETURNING * INTO v_g;
    PERFORM business.audit_record('staff', v_account, 'refund.goal_raised', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_target,
                                                               'policy_target_minor', v_g.policy_target_minor,
                                                               'reason', v_g.reason, 'exception_id', v_id));
  END IF;
  FOR a IN SELECT x.cid, x.amt FROM ROWS FROM (pg_catalog.unnest(p_collections), pg_catalog.unnest(p_amounts)) AS x(cid, amt)
           JOIN business.payment_collection pc ON pc.id = x.cid
           ORDER BY CASE pc.kind WHEN 'cake' THEN 0 ELSE 1 END, pc.id LOOP
    v_op := business.refund_reserve_one(p_order, a.cid, v_g, a.amt);
    v_ops := v_ops || pg_catalog.jsonb_build_array(pg_catalog.jsonb_build_object(
               'collection_id', a.cid, 'amount_minor', a.amt, 'refund_operation_id', v_op));
  END LOOP;
  v_allocs := v_ops::json;
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order;
  INSERT INTO business.refund_exception (id, order_id, goal_id, goal_version, target_minor, amount_minor, allocations, reason,
                                         order_version, recorded_by)
  VALUES (v_id, p_order, v_g.id, v_g.version, v_target, p_amount, v_allocs, p_reason, v_o.version + 1, v_account);
  PERFORM business.audit_record('staff', v_account, 'refund.exception_recorded', 'order', p_order, NULL,
                                pg_catalog.json_build_object('exception_id', v_id, 'goal_version', v_g.version,
                                                             'target_minor', v_target, 'amount_minor', p_amount,
                                                             'allocations', v_allocs));
  PERFORM business.command_finish(v_account, 'refund.exception', p_order::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- API (chef): pay the pending delivery share of the order's goal from
-- delivery collection p_collection (see the header). Returns the reserved
-- refund operation and whether it is a replay.
CREATE FUNCTION business.refund_delivery_pay(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                             p_expected_version integer, p_collection uuid)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  v_o business.orders%ROWTYPE;
  v_c business.payment_collection%ROWTYPE;
  v_g business.refund_goal%ROWTYPE;
  v_amount bigint;
  v_op uuid;
BEGIN
  IF p_order IS NULL OR p_collection IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  v_prior := business.command_claim(v_account, 'refund.delivery_pay', p_collection::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  v_o := business.refund_chef_admit(p_order, p_expected_version);
  SELECT * INTO v_c FROM business.payment_collection pc WHERE pc.id = p_collection AND pc.order_id = p_order;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'collection not found' USING ERRCODE = 'GK404';
  END IF;
  IF v_c.kind <> 'delivery' OR v_c.state <> 'succeeded' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF EXISTS (SELECT 1 FROM business.refund_operation ro
             WHERE ro.collection_id = p_collection
               AND ro.state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action')) THEN
    RAISE EXCEPTION 'refund_in_progress' USING ERRCODE = 'GK409';
  END IF;
  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  IF FOUND THEN
    SELECT LEAST(GREATEST(0, s.share_minor - s.exposure_minor), s.room_minor,
                 GREATEST(0, v_g.target_minor
                             - (SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0)
                                FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b
                                WHERE pc.order_id = p_order)
                             - (SELECT coalesce(sum(p.amount_minor), 0) FROM business.refund_plan(p_order, v_g.target_minor) p)))
    INTO v_amount
    FROM business.refund_shares(p_order, v_g.target_minor, true) s
    WHERE s.collection_id = p_collection AND NOT s.floored AND NOT s.blocked;
  END IF;
  IF coalesce(v_amount, 0) <= 0 THEN
    RAISE EXCEPTION 'no_refund_pending' USING ERRCODE = 'GK409';
  END IF;
  v_op := business.refund_reserve_one(p_order, p_collection, v_g, v_amount);
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order;
  PERFORM business.audit_record('staff', v_account, 'refund.delivery_paid', 'order', p_order, NULL,
                                pg_catalog.json_build_object('refund_operation_id', v_op, 'collection_id', p_collection,
                                                             'amount_minor', v_amount, 'goal_version', v_g.version));
  PERFORM business.command_finish(v_account, 'refund.delivery_pay', p_collection::text, p_key, v_op);
  RETURN QUERY SELECT v_op, false;
END;
$$;

REVOKE ALL ON TABLE business.refund_exception FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_reconciliation_required(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_chef_admit(uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_reserve_one(uuid, uuid, business.refund_goal, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_exception_record(uuid, text, text, uuid, integer, bigint, text, uuid[], bigint[], text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_delivery_pay(uuid, text, text, uuid, integer, uuid) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.refund_exception_record(uuid, text, text, uuid, integer, bigint, text, uuid[], bigint[], text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.refund_delivery_pay(uuid, text, text, uuid, integer, uuid) TO gk_api;
