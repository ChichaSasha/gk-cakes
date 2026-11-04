-- GK-025 A3a: K11 accepted-command protection and free confirmation
-- (contracts r6.5 K03 "zero cake amount", K04 clocks, K05 lifecycle, K11).
--
-- An accepted order command is a recovery operation of kind 'command' on its
-- order (aggregate = order, prior version = the order's version). The API
-- records the request, the exact accepted payload (the order snapshot it
-- commits) and the command's receipt instant, reserves the operation and
-- enqueues a worker job, all in one transaction, and answers 202. The worker
-- then runs the K11 protocol with the independent journal:
--   reserved   -> prepared envelope acknowledged -> prepared
--   prepared   -> domain commit + outcome, one transaction -> committed
--   committed  -> outcome envelope acknowledged -> secured
-- The order shows "confirmation_pending" from the commit until the outcome
-- envelope is acknowledged; only then is it confirmed (K11: a commit without
-- the independent outcome acknowledgment is not exposed as confirmed). A
-- command has no provider replay deadline: its commit is local and
-- idempotent for the operation forever.
--
-- Free confirmation (K03): a zero-amount order is confirmed by the
-- customer's explicit command, without any payment row. Its anchor is the
-- persisted receipt instant (K04 "free confirmation"): the 24-hour quote
-- window must still be open at that instant (equality expires) and the
-- handover at least 192 elapsed hours after it (equality allowed;
-- BO-GK-001.lead_time; no chef-approved shorter lead exists yet). The commit
-- makes the day allocation committed, sells the reserved ready cakes and
-- consumes the code use; a refusal (the order changed, or a hold was
-- released meanwhile) is recorded as the outcome and changes nothing but the
-- order's version (so later commands on the order reserve the next version).
-- At most one free confirmation per order is pending or confirmed; after a
-- refusal the customer may ask again. A restore freeze is re-checked inside
-- the commit's transaction (the freeze row is share-locked), so a freeze
-- that lands after the worker's last check still stops the commit.

SET LOCAL ROLE gk_owner;

ALTER TABLE business.recovery_operation DROP CONSTRAINT recovery_operation_kind_check;
ALTER TABLE business.recovery_operation
  ADD CONSTRAINT recovery_operation_kind_check CHECK (kind IN ('dispatch', 'command'));
ALTER TABLE business.recovery_operation ALTER COLUMN replay_until DROP NOT NULL;
-- A dispatch has its provider idempotency window; a command has none.
ALTER TABLE business.recovery_operation
  ADD CONSTRAINT recovery_replay_kind CHECK ((kind = 'dispatch') = (replay_until IS NOT NULL));

CREATE TABLE business.order_command (
  -- The recovery operation's ID.
  id               uuid PRIMARY KEY,
  order_id         uuid NOT NULL REFERENCES business.orders (id),
  command          text NOT NULL CHECK (command IN ('free_confirm')),
  actor_kind       text NOT NULL CHECK (actor_kind IN ('customer', 'guest')),
  actor_account_id uuid NULL REFERENCES business.account (id),
  -- The guest capability that made the request (idempotency is scoped to
  -- it, K00).
  guest_capability_id uuid NULL REFERENCES business.order_guest_capability (id),
  idempotency_key  text NOT NULL CHECK (idempotency_key ~ '^[\x21-\x7e]{1,128}$'),
  body_sha256      char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  prior_version    integer NOT NULL CHECK (prior_version >= 1),
  -- The persisted command receipt instant (K04 free-confirmation anchor).
  received_at      timestamptz NOT NULL,
  -- The exact accepted payload bytes written to the prepared envelope.
  payload          text NOT NULL CHECK (octet_length(payload) BETWEEN 2 AND 65536),
  payload_sha256   char(64) NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  envelope_key     text NOT NULL UNIQUE CHECK (length(envelope_key) BETWEEN 1 AND 512),
  job_id           uuid NOT NULL UNIQUE REFERENCES business.job (id),
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT order_command_actor CHECK ((actor_kind = 'customer') = (actor_account_id IS NOT NULL)
                                       AND (actor_kind = 'guest') = (guest_capability_id IS NOT NULL))
);

-- Requests on one order are serialized by the order row lock; the request
-- function enforces "at most one pending or confirmed free confirmation".
CREATE INDEX order_command_order_idx ON business.order_command (order_id, command, created_at);

CREATE TRIGGER order_command_immutable BEFORE UPDATE OR DELETE ON business.order_command
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER order_command_no_truncate BEFORE TRUNCATE ON business.order_command
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: everything needed to rebuild an order after a primary
-- database loss (K11 "exact accepted payload/snapshots"): the order, its
-- immutable items, holds, code use and guest capability digests.
CREATE FUNCTION business.order_recovery_snapshot(p_order uuid)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.json_build_object(
    'id', o.id, 'quote_id', o.quote_id, 'version', o.version, 'owner_account_id', o.owner_account_id,
    'customer_full_name', o.customer_full_name, 'different_payer_name', o.different_payer_name,
    'contact_email', o.contact_email, 'lifecycle', o.lifecycle, 'payment_requirement', o.payment_requirement,
    'fulfillment_mode', o.fulfillment_mode, 'handover_day', o.handover_day, 'slot_id', o.slot_id,
    'original_handover_at', o.original_handover_at, 'handover_ends_at', o.handover_ends_at,
    'full_refund_deadline_at', o.full_refund_deadline_at, 'policy_version_id', o.policy_version_id,
    'subtotal_minor', o.subtotal_minor, 'discount_minor', o.discount_minor, 'total_minor', o.total_minor,
    'currency', o.currency, 'placed_at', o.placed_at,
    'items', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                'id', i.id, 'line_no', i.line_no, 'variant_id', i.variant_id, 'recipe_version_id', i.recipe_version_id,
                'lot_id', i.physical_lot_id, 'quantity', i.quantity, 'unit_price_minor', i.unit_price_minor,
                'discount_minor', i.discount_minor, 'net_paid_minor', i.net_paid_minor) ORDER BY i.line_no)
              FROM business.order_item i WHERE i.order_id = o.id),
    'allocations', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                      'id', a.id, 'day', a.day, 'cake_quantity', a.cake_quantity,
                      'classification', a.classification, 'state', a.state) ORDER BY a.day)
                    FROM business.capacity_allocation a WHERE a.order_ref = o.id),
    'lot_reservations', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                           'lot_id', r.lot_id, 'quantity', r.quantity, 'state', r.state) ORDER BY r.lot_id)
                         FROM business.order_lot_reservation r WHERE r.order_id = o.id),
    'code_application', (SELECT pg_catalog.json_build_object(
                           'id', c.id, 'code_id', c.code_id, 'code_version_id', c.code_version_id,
                           'account_id', c.account_id, 'state', c.state, 'discount_minor', c.discount_minor)
                         FROM business.code_application c WHERE c.order_id = o.id),
    'guest_capabilities', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                             'id', g.id, 'digest', pg_catalog.encode(g.capability_digest, 'hex'),
                             'allowed_commands', g.allowed_commands, 'revoked_at', g.revoked_at) ORDER BY g.created_at, g.id)
                           FROM business.order_guest_capability g WHERE g.order_id = o.id))
  FROM business.orders o WHERE o.id = p_order;
$$;

-- Owner-internal: whether the order's holds are still those checkout made:
-- its day allocation not released, its ready cakes still reserved, its code
-- use still reserved.
CREATE FUNCTION business.order_holds_intact(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT NOT EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = p_order AND a.state <> 'tentative')
     AND NOT EXISTS (SELECT 1 FROM business.order_lot_reservation r WHERE r.order_id = p_order AND r.state <> 'reserved')
     AND NOT EXISTS (SELECT 1 FROM business.code_application c WHERE c.order_id = p_order AND c.state <> 'reserved');
$$;

-- API: the customer's explicit free confirmation of a zero-amount order, by
-- its owner or a guest capability holder with "pay". Records the command
-- and enqueues its worker job (see the header); returns the command (the
-- recovery operation) and whether it already existed. p_env names the
-- journal environment (recovery/<env>/...).
CREATE FUNCTION business.order_free_confirm_request(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
  v_disabled boolean;
  v_o business.orders%ROWTYPE;
  v_prior business.order_command%ROWTYPE;
  v_capability uuid;
  v_expires timestamptz;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
BEGIN
  IF (p_owner IS NULL) = (p_guest_digest IS NULL) OR p_now IS NULL OR p_order IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  IF p_owner IS NOT NULL THEN
    SELECT r.account_id, r.account_disabled INTO v_account, v_disabled FROM business.identity_resolve(p_owner) r;
    IF v_disabled THEN
      RAISE EXCEPTION 'account disabled' USING ERRCODE = 'GK403';
    END IF;
  END IF;

  -- The order, for its owner or a "pay" capability holder only (404 as for
  -- a guessed ID). Locked: commands on one order are serialized.
  SELECT * INTO v_o FROM business.orders o
  WHERE o.id = p_order
    AND ((v_account IS NOT NULL AND o.owner_account_id = v_account)
         OR (p_guest_digest IS NOT NULL AND EXISTS (
               SELECT 1 FROM business.order_guest_capability g
               WHERE g.order_id = o.id AND g.capability_digest = p_guest_digest AND g.revoked_at IS NULL
                 AND 'pay' = ANY (g.allowed_commands))))
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF v_account IS NULL THEN
    SELECT g.id INTO v_capability FROM business.order_guest_capability g
    WHERE g.order_id = p_order AND g.capability_digest = p_guest_digest;
  END IF;

  -- The same actor (account, or guest capability) and key replays.
  SELECT * INTO v_prior FROM business.order_command c
  WHERE c.order_id = p_order AND c.command = 'free_confirm' AND c.idempotency_key = p_key
    AND c.actor_account_id IS NOT DISTINCT FROM v_account AND c.guest_capability_id IS NOT DISTINCT FROM v_capability;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior.id, true;
    RETURN;
  END IF;
  -- Another free confirmation that is pending or confirmed (not refused).
  IF EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
             WHERE c.order_id = p_order AND c.command = 'free_confirm'
               AND NOT (r.state = 'secured' AND r.outcome ->> 'status' = 'refused')) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;

  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.payment_requirement <> 'none' THEN
    RAISE EXCEPTION 'payment_required' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.lifecycle <> 'awaiting_payment' OR v_o.recovery_state <> 'prepared'
     OR NOT business.order_holds_intact(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- The 24-hour window at the receipt instant: equality expires (K04).
  SELECT q.expires_at INTO v_expires FROM business.checkout_quote q WHERE q.id = v_o.quote_id;
  IF p_now >= v_expires THEN
    RAISE EXCEPTION 'quote_expired' USING ERRCODE = 'GK409';
  END IF;
  -- 192 elapsed hours from free confirmation to handover, equality allowed.
  IF v_o.original_handover_at - p_now < business.schedule_lead_time() THEN
    RAISE EXCEPTION 'lead_time' USING ERRCODE = 'GK409';
  END IF;

  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.free_confirm', 'operation_id', v_id, 'order_id', p_order,
    'prior_version', v_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', CASE WHEN v_account IS NULL THEN 'guest' ELSE 'customer' END,
                                          'account_id', v_account),
    'order', business.order_recovery_snapshot(p_order))::text;
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_payload, 'UTF8')), 'hex');

  SELECT r.result INTO v_reserve
  FROM business.recovery_reserve(v_id, 'command', p_order, v_o.version, v_hash, v_key, NULL) r;
  IF v_reserve = 'busy' THEN
    -- Another protected operation on this order is unresolved.
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  ELSIF v_reserve <> 'reserved' THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  -- Due at once by the database clock (p_now may be a test clock).
  v_job := business.job_enqueue(v_id, NULL);
  INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, guest_capability_id,
                                      idempotency_key, body_sha256, prior_version, received_at, payload, payload_sha256,
                                      envelope_key, job_id)
  VALUES (v_id, p_order, 'free_confirm', CASE WHEN v_account IS NULL THEN 'guest' ELSE 'customer' END, v_account,
          v_capability, p_key, p_body_sha256, v_o.version, p_now, v_payload, v_hash, v_key, v_job);
  PERFORM business.audit_record(CASE WHEN v_account IS NULL THEN 'system' ELSE 'customer' END, v_account,
                                'order.free_confirmation_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id));
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- Owner-internal: the order command of the job the caller holds a live
-- lease on. Raises GK001 when the lease is not held; returns NULL when the
-- job is not an order-command job.
CREATE FUNCTION business.order_command_leased(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS business.order_command
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_operation uuid;
  v_cmd business.order_command;
BEGIN
  SELECT j.operation_id INTO v_operation
  FROM business.job j
  WHERE j.id = p_job_id AND j.state = 'running' AND j.lease_owner = p_worker AND j.generation = p_generation
    AND j.lease_until > pg_catalog.now()
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'job % is not leased to this worker generation', p_job_id USING ERRCODE = 'GK001';
  END IF;
  SELECT * INTO v_cmd FROM business.order_command c WHERE c.job_id = p_job_id AND c.id = v_operation;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  RETURN v_cmd;
END;
$$;

-- Owner-internal: the leased command whose recovery operation is p_operation_id.
CREATE FUNCTION business.order_command_leased_operation(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS business.order_command
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_cmd business.order_command := business.order_command_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v_cmd.id IS NULL OR v_cmd.id IS DISTINCT FROM p_operation_id THEN
    RAISE EXCEPTION 'recovery operation % is not the command of job %', p_operation_id, p_job_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v_cmd;
END;
$$;

-- Worker: the leased job's command. Status 'ready' returns its identity and
-- exact payload; 'frozen' (restore in progress) returns none;
-- 'not_order_command' means another handler owns the job.
CREATE FUNCTION business.order_command_begin(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS TABLE (status text, operation_id uuid, order_id uuid, prior_version integer, payload text, envelope_key text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.id IS NULL THEN
    RETURN QUERY SELECT 'not_order_command'::text, NULL::uuid, NULL::uuid, NULL::integer, NULL::text, NULL::text;
  ELSIF business.recovery_frozen() THEN
    RETURN QUERY SELECT 'frozen'::text, NULL::uuid, NULL::uuid, NULL::integer, NULL::text, NULL::text;
  ELSE
    RETURN QUERY SELECT 'ready'::text, v.id, v.order_id, v.prior_version, v.payload, v.envelope_key;
  END IF;
END;
$$;

-- Worker: the command's recovery operation (identity-checked; it was
-- reserved with the request).
CREATE FUNCTION business.order_command_reserve(
  p_job_id uuid, p_worker uuid, p_generation bigint,
  p_operation_id uuid, p_kind text, p_aggregate_id uuid, p_prior_version integer,
  p_payload_hash text, p_envelope_key text)
RETURNS TABLE (result text, op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  IF p_kind <> 'command' OR p_aggregate_id <> v.order_id OR p_prior_version <> v.prior_version
     OR p_payload_hash <> v.payload_sha256 OR p_envelope_key <> v.envelope_key THEN
    RAISE EXCEPTION 'recovery operation % does not match the command of order %', p_operation_id, v.order_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN QUERY SELECT * FROM business.recovery_reserve(p_operation_id, 'command', v.order_id, v.prior_version,
                                                       v.payload_sha256, v.envelope_key, NULL);
END;
$$;

CREATE FUNCTION business.order_command_prepared(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  RETURN business.recovery_mark_prepared(v.id);
END;
$$;

-- Worker: the domain commit of a prepared command, in one transaction with
-- its committed phase (see the header). Idempotent: an operation already
-- committed returns its recorded outcome unchanged.
CREATE FUNCTION business.order_command_apply(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS TABLE (op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_o business.orders%ROWTYPE;
  v_r business.recovery_operation%ROWTYPE;
  v_expires timestamptz;
  v_reason text;
  v_lot record;
  v_outcome text;
  v_state text;
  v_frozen boolean;
BEGIN
  -- Lock order (K00): the order, then its recovery operation, then the day
  -- and lots inside the engines.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
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

  SELECT q.expires_at INTO v_expires FROM business.checkout_quote q WHERE q.id = v_o.quote_id;
  IF v_o.version <> v.prior_version OR v_o.lifecycle <> 'awaiting_payment' OR v_o.recovery_state <> 'prepared'
     OR v_o.payment_requirement <> 'none' THEN
    v_reason := 'order_changed';
  ELSIF NOT business.order_holds_intact(v.order_id) THEN
    v_reason := 'holds_released';
  ELSIF v.received_at >= v_expires OR v_o.original_handover_at - v.received_at < business.schedule_lead_time() THEN
    v_reason := 'window_closed';
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
    UPDATE business.orders o
    SET lifecycle = 'confirmed', free_confirmed_at = v.received_at, recovery_state = 'confirmation_pending',
        version = o.version + 1, updated_at = pg_catalog.now()
    WHERE o.id = v.order_id;
    v_outcome := pg_catalog.format('{"status":"confirmed","evidence":{"order_version":%s}}', v_o.version + 1);
    PERFORM business.audit_record(CASE WHEN v.actor_account_id IS NULL THEN 'system' ELSE 'customer' END,
                                  v.actor_account_id, 'order.free_confirmed', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id));
  ELSE
    -- Nothing changes but the version: later commands reserve the next one.
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    v_outcome := pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', v_reason);
    PERFORM business.audit_record('system', NULL, 'order.free_confirmation_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason));
  END IF;
  SELECT c.op_state INTO v_state
  FROM business.recovery_mark_committed(v.id, v_outcome::json,
         pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_outcome, 'UTF8')), 'hex'), false) c;
  SELECT * INTO v_r FROM business.recovery_operation r WHERE r.id = v.id;
  RETURN QUERY SELECT v_r.state, v_r.outcome, v_r.outcome_hash::text;
END;
$$;

-- Worker: committed -> secured after the outcome envelope is acknowledged;
-- a confirmed order becomes visible as confirmed.
CREATE FUNCTION business.order_command_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_state text;
BEGIN
  PERFORM 1 FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  v_state := business.recovery_mark_secured(v.id);
  UPDATE business.orders o SET recovery_state = 'secured', updated_at = pg_catalog.now()
  WHERE o.id = v.order_id AND o.recovery_state = 'confirmation_pending'
    AND (SELECT r.outcome ->> 'status' FROM business.recovery_operation r WHERE r.id = v.id) = 'confirmed';
  RETURN v_state;
END;
$$;

CREATE FUNCTION business.order_command_frozen(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.id IS NULL THEN
    RAISE EXCEPTION 'job % is not an order-command job', p_job_id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN business.recovery_frozen();
END;
$$;

-- Orders as customers see them: an order whose commit is not yet secured in
-- the independent journal is "confirmation_pending", never confirmed (K11),
-- and free_confirmation reports the customer's latest command.
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
           'lifecycle', CASE WHEN o.recovery_state = 'confirmation_pending' THEN 'confirmation_pending' ELSE o.lifecycle END,
           'payment_requirement', o.payment_requirement,
           'placed_at', o.placed_at, 'date', o.handover_day, 'fulfillment_mode', o.fulfillment_mode,
           'slot', pg_catalog.json_build_object('id', o.slot_id, 'starts_at', o.original_handover_at, 'ends_at', o.handover_ends_at),
           'full_refund_deadline_at', o.full_refund_deadline_at, 'policy_version_id', o.policy_version_id,
           'customer_full_name', o.customer_full_name, 'different_payer_name', o.different_payer_name,
           'contact_email', o.contact_email, 'subtotal_minor', o.subtotal_minor, 'discount_minor', o.discount_minor,
           'total_minor', o.total_minor, 'currency', o.currency, 'paid_at', o.paid_at,
           'free_confirmed_at', CASE WHEN o.recovery_state = 'secured' THEN o.free_confirmed_at END,
           'free_confirmation', (SELECT pg_catalog.json_build_object(
                                   'state', CASE WHEN r.state <> 'secured' THEN 'processing'
                                                 ELSE r.outcome ->> 'status' END,
                                   'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                                   'requested_at', c.received_at)
                                 FROM business.order_command c
                                 JOIN business.recovery_operation r ON r.id = c.id
                                 WHERE c.order_id = o.id AND c.command = 'free_confirm'
                                 ORDER BY c.created_at DESC, c.id DESC LIMIT 1),
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

REVOKE ALL ON TABLE business.order_command FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_recovery_snapshot(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_holds_intact(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_free_confirm_request(uuid, bytea, uuid, text, text, integer, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_leased(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_leased_operation(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_begin(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_prepared(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_apply(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_secured(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_frozen(uuid, uuid, bigint) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_free_confirm_request(uuid, bytea, uuid, text, text, integer, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_command_begin(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.order_command_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.order_command_prepared(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.order_command_apply(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.order_command_secured(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.order_command_frozen(uuid, uuid, bigint) TO gk_worker;
