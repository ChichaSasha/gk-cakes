-- GK-026 B1 part 1: customer and chef cancellation of an accepted order
-- (contracts r6.5 K05 "Both cake types", "Target=nearest-penny-half-up",
-- "Current 50% half-penny rounding favours customer", "Production state
-- never causes zero entitlement", "at boundary change refresh preview and
-- require reconfirmation"; K03 code use; K04 clocks; K11 accepted commands;
-- owner decision BO-GK-001.ready_cancellation: strictly before
-- original_handover_at - 120 elapsed hours returns 100%, at equality or
-- later exactly 50%; chef cancellation 100%).
--
-- Preview (business.order_cancellation_preview, API). The owner or a guest
-- capability holder with "cancel" asks what cancelling now would return.
-- The server computes, at the request instant: the rate (100 strictly before
-- full_refund_deadline_at, else 50), the actual paid aggregate (every
-- succeeded collection: after discounts), the amount = nearest-penny half-up
-- of rate x paid (a half penny goes to the customer), the allocation per
-- succeeded collection as business.refund_reserve would reserve it, and a
-- short expiry (10 minutes). The preview is stored immutably with the order
-- version it was computed at and a SHA-256 of its canonical body.
--
-- Cancel (business.order_cancel_request, API; business.order_chef_cancel_request,
-- chef). A new K11 order command 'cancel' through the A3a machinery: the
-- request records the command with its persisted receipt instant and exact
-- payload, reserves the recovery operation and enqueues the job (202). The
-- customer must name the preview and its hash; under the order lock, at the
-- receipt instant, the preview must be unexpired, of the current version,
-- and still give the same rate and amount: a preview that said 100% when
-- the receipt is at or after the deadline (the boundary was crossed since)
-- is refused 409 preview_stale (refresh and reconfirm). Eligible
-- lifecycles: confirmed, in_production, ready (production never reduces
-- the entitlement); anything else is 409 cancellation_not_eligible
-- (awaiting payment has its own release paths; paid_awaiting_agreement is
-- the chef's late-payment decision: part 2). The chef's cancellation
-- returns 100% and carries a bounded reason note.
--
-- Commit (business.order_command_apply, worker, after the prepared envelope
-- is acknowledged). Under the order lock the commit re-checks the version,
-- lifecycle and that the order is secured; then sets lifecycle cancelled,
-- cancel_reason customer_cancelled | chef_cancelled, cancelled_at = the
-- persisted receipt instant, recovery_state cancellation_pending (the order
-- reads "cancellation_pending", without its reason or refund, until the
-- outcome envelope is acknowledged: business.order_command_secured), version
-- + 1; releases the day place (capacity_release) and never restores the
-- code use (K03: it stays consumed); records the order's refund goal
-- (target = the rate applied at the receipt instant to the paid aggregate,
-- reason and rate kept) and reserves the refund operations through the A3c2
-- functions (business.refund_reserve: within C - S - R and the aggregate
-- remainder, one unresolved operation per collection). An order that changed
-- since the request is refused (outcome refused, reason order_changed; only
-- its version changes). The refund dispatch then runs as in 0016.
--
-- Expand-only (K09): new table, nullable columns, replaced CHECK
-- constraints and new functions; the replaced functions keep their
-- signatures and grants. Lock order (K00): the order, its collections by
-- ID, the recovery operation, the goal and refund operations, the day.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------- schema

ALTER TABLE business.orders DROP CONSTRAINT orders_cancel_reason_check;
ALTER TABLE business.orders
  ADD CONSTRAINT orders_cancel_reason_check CHECK (cancel_reason IN (
    'payment_not_completed', 'free_not_confirmed', 'customer_cancelled', 'chef_cancelled'));
ALTER TABLE business.orders DROP CONSTRAINT orders_recovery_state_check;
ALTER TABLE business.orders
  ADD CONSTRAINT orders_recovery_state_check CHECK (recovery_state IN (
    'prepared', 'confirmation_pending', 'cancellation_pending', 'secured'));

ALTER TABLE business.order_command DROP CONSTRAINT order_command_command_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_command_check CHECK (command IN ('free_confirm', 'paid_confirm', 'cancel'));
ALTER TABLE business.order_command DROP CONSTRAINT order_command_actor_kind_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_actor_kind_check CHECK (actor_kind IN ('customer', 'guest', 'system', 'staff'));
ALTER TABLE business.order_command DROP CONSTRAINT order_command_actor;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_actor CHECK ((actor_kind IN ('customer', 'staff')) = (actor_account_id IS NOT NULL)
                                           AND (actor_kind = 'guest') = (guest_capability_id IS NOT NULL));
-- The chef's cancellation reason (bounded single-line text) and the
-- customer's confirmed preview.
ALTER TABLE business.order_command
  ADD COLUMN note text NULL CHECK (note = btrim(note) AND char_length(note) BETWEEN 1 AND 500
                                   AND note !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  ADD COLUMN preview_id uuid NULL;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_cancel CHECK ((command = 'cancel' OR (note IS NULL AND preview_id IS NULL))
                                            AND (note IS NULL OR actor_kind = 'staff')
                                            AND (command <> 'cancel' OR actor_kind IN ('customer', 'guest', 'staff'))
                                            AND (command <> 'cancel' OR (actor_kind = 'staff') = (preview_id IS NULL)));

ALTER TABLE business.refund_goal DROP CONSTRAINT refund_goal_reason_check;
ALTER TABLE business.refund_goal
  ADD CONSTRAINT refund_goal_reason_check CHECK (reason IN ('late_capture_terminal', 'customer_cancelled', 'chef_cancelled'));
ALTER TABLE business.refund_goal DROP CONSTRAINT refund_goal_source_check;
ALTER TABLE business.refund_goal
  ADD CONSTRAINT refund_goal_source_check CHECK (source IN ('system', 'chef', 'customer'));
-- The policy rate and paid basis the target came from (K05: keep the reason
-- and original policy target visible). NULL for the late-capture full return.
ALTER TABLE business.refund_goal
  ADD COLUMN policy_rate smallint NULL CHECK (policy_rate IN (50, 100)),
  ADD COLUMN paid_basis_minor bigint NULL CHECK (paid_basis_minor > 0),
  ADD CONSTRAINT refund_goal_policy CHECK ((policy_rate IS NULL) = (paid_basis_minor IS NULL)
                                          AND (policy_rate IS NULL) = (reason = 'late_capture_terminal'));

CREATE TABLE business.cancellation_preview (
  id                  uuid PRIMARY KEY,
  order_id            uuid NOT NULL REFERENCES business.orders (id),
  order_version       integer NOT NULL CHECK (order_version >= 1),
  actor_account_id    uuid NULL REFERENCES business.account (id),
  guest_capability_id uuid NULL REFERENCES business.order_guest_capability (id),
  rate                smallint NOT NULL CHECK (rate IN (50, 100)),
  paid_minor          bigint NOT NULL CHECK (paid_minor >= 0),
  amount_minor        bigint NOT NULL CHECK (amount_minor >= 0 AND amount_minor <= paid_minor),
  allocations         json NOT NULL CHECK (octet_length(allocations::text) <= 4096),
  full_refund_deadline_at timestamptz NOT NULL,
  computed_at         timestamptz NOT NULL,
  expires_at          timestamptz NOT NULL,
  body_sha256         char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  created_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT cancellation_preview_actor CHECK ((actor_account_id IS NULL) <> (guest_capability_id IS NULL)),
  CONSTRAINT cancellation_preview_expiry CHECK (expires_at = computed_at + interval '10 minutes')
);
CREATE INDEX cancellation_preview_order_idx ON business.cancellation_preview (order_id, computed_at);
CREATE TRIGGER cancellation_preview_immutable BEFORE UPDATE OR DELETE ON business.cancellation_preview
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER cancellation_preview_no_truncate BEFORE TRUNCATE ON business.cancellation_preview
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_preview_fk FOREIGN KEY (preview_id) REFERENCES business.cancellation_preview (id);

-- ------------------------------------------------------------ helpers

-- Owner-internal: the K05 rate of a cancellation at instant p_at: a chef's
-- is 100; a customer's is 100 strictly before the deadline, 50 at equality
-- or later.
CREATE FUNCTION business.cancellation_rate(p_deadline timestamptz, p_at timestamptz, p_chef boolean)
RETURNS smallint
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN p_chef OR p_at < p_deadline THEN 100 ELSE 50 END::smallint;
$$;

-- Owner-internal: nearest-penny half-up of rate% x paid (both
-- non-negative: a half penny goes to the customer).
CREATE FUNCTION business.cancellation_amount(p_paid bigint, p_rate smallint)
RETURNS bigint
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT (p_paid * p_rate + 50) / 100;
$$;

-- Owner-internal: the order's actual paid aggregate (every succeeded
-- collection; amounts are after discounts).
CREATE FUNCTION business.order_paid_minor(p_order uuid)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(sum(pc.amount_minor), 0)::bigint
  FROM business.payment_collection pc WHERE pc.order_id = p_order AND pc.state = 'succeeded';
$$;

-- Owner-internal: what business.refund_reserve would reserve per succeeded
-- collection (by ID) for a goal of p_target: each takes min(its room, what
-- the target still needs beyond everything already returned or reserved).
CREATE FUNCTION business.cancellation_allocations(p_order uuid, p_target bigint)
RETURNS json
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_left bigint;
  v_take bigint;
  v_out json[] := ARRAY[]::json[];
  c record;
BEGIN
  SELECT GREATEST(0, p_target - coalesce(sum(b.succeeded_minor + b.reserved_minor), 0)) INTO v_left
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b
  WHERE pc.order_id = p_order;
  FOR c IN SELECT pc.id, b.room_minor FROM business.payment_collection pc
           CROSS JOIN LATERAL business.refund_balance(pc.id) b
           WHERE pc.order_id = p_order AND pc.state = 'succeeded' ORDER BY pc.id LOOP
    v_take := LEAST(c.room_minor, v_left);
    CONTINUE WHEN v_take <= 0;
    v_out := v_out || pg_catalog.json_build_object('collection_id', c.id, 'amount_minor', v_take);
    v_left := v_left - v_take;
  END LOOP;
  RETURN pg_catalog.array_to_json(v_out);
END;
$$;

-- Owner-internal: whether an order may be cancelled by the customer or chef
-- now (K05: unfulfilled accepted lifecycles; production never removes it).
CREATE FUNCTION business.cancellation_eligible(p_lifecycle text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_lifecycle IN ('confirmed', 'in_production', 'ready');
$$;

-- Owner-internal: record an accepted cancel command on order p_order (the
-- caller holds the order lock and has checked it): the exact payload (order
-- snapshot plus the cancellation it asks for), the K11 command operation
-- and its job, in the caller's transaction (as order_free_confirm_request).
CREATE FUNCTION business.order_cancel_record(
  p_order uuid, p_version integer, p_actor_kind text, p_account uuid, p_capability uuid, p_key text, p_body_sha256 text,
  p_now timestamptz, p_env text, p_reason text, p_rate smallint, p_paid bigint, p_amount bigint, p_note text,
  p_preview uuid)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid := pg_catalog.gen_random_uuid();
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
BEGIN
  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(p_version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.cancel', 'operation_id', v_id, 'order_id', p_order,
    'prior_version', p_version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', p_actor_kind, 'account_id', p_account),
    'cancellation', pg_catalog.json_build_object('reason', p_reason, 'rate', p_rate, 'paid_minor', p_paid,
                                                 'amount_minor', p_amount, 'note', p_note, 'preview_id', p_preview),
    'order', business.order_recovery_snapshot(p_order))::text;
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_payload, 'UTF8')), 'hex');
  SELECT r.result INTO v_reserve
  FROM business.recovery_reserve(v_id, 'command', p_order, p_version, v_hash, v_key, NULL) r;
  IF v_reserve = 'busy' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  ELSIF v_reserve <> 'reserved' THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  v_job := business.job_enqueue(v_id, NULL);
  INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, guest_capability_id,
                                      idempotency_key, body_sha256, prior_version, received_at, payload, payload_sha256,
                                      envelope_key, job_id, note, preview_id)
  VALUES (v_id, p_order, 'cancel', p_actor_kind, p_account, p_capability, p_key, p_body_sha256, p_version, p_now,
          v_payload, v_hash, v_key, v_job, p_note, p_preview);
  PERFORM business.audit_record(CASE p_actor_kind WHEN 'guest' THEN 'system' ELSE p_actor_kind END, p_account,
                                'order.cancellation_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id, 'reason', p_reason, 'rate', p_rate,
                                                             'amount_minor', p_amount));
  RETURN v_id;
END;
$$;

-- Owner-internal: the common request checks of a cancel on the locked
-- order: a replay of the same actor and key returns the earlier command;
-- otherwise another cancel that is not refused is a conflict, then the
-- version, the lifecycle and the K11 state are checked.
CREATE FUNCTION business.order_cancel_admissible(p_o business.orders, p_account uuid, p_capability uuid, p_key text,
                                                 p_body_sha256 text, p_expected_version integer)
RETURNS uuid
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_prior business.order_command%ROWTYPE;
BEGIN
  SELECT * INTO v_prior FROM business.order_command c
  WHERE c.order_id = p_o.id AND c.command = 'cancel' AND c.idempotency_key = p_key
    AND c.actor_account_id IS NOT DISTINCT FROM p_account AND c.guest_capability_id IS NOT DISTINCT FROM p_capability;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN v_prior.id;
  END IF;
  IF EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
             WHERE c.order_id = p_o.id AND c.command = 'cancel'
               AND NOT (r.state = 'secured' AND r.outcome ->> 'status' = 'refused')) THEN
    -- One cancellation at a time: pending, or already cancelled.
    IF p_o.lifecycle = 'cancelled' THEN
      RAISE EXCEPTION 'cancellation_not_eligible' USING ERRCODE = 'GK409';
    END IF;
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF p_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF NOT business.cancellation_eligible(p_o.lifecycle) THEN
    RAISE EXCEPTION 'cancellation_not_eligible' USING ERRCODE = 'GK409';
  END IF;
  IF p_o.recovery_state <> 'secured' THEN
    -- The acceptance itself is not protected yet.
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  RETURN NULL;
END;
$$;

-- Owner-internal: the order of a customer command, for its owner or a
-- guest capability holder with p_command (404 otherwise, as for a guessed
-- ID), locked; with the capability used.
CREATE FUNCTION business.order_customer_lock(p_owner uuid, p_guest_digest bytea, p_order uuid, p_command text,
                                             OUT o_order business.orders, OUT o_account uuid, OUT o_capability uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_disabled boolean;
BEGIN
  IF (p_owner IS NULL) = (p_guest_digest IS NULL) OR p_order IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_owner IS NOT NULL THEN
    SELECT r.account_id, r.account_disabled INTO o_account, v_disabled FROM business.identity_resolve(p_owner) r;
    IF v_disabled THEN
      RAISE EXCEPTION 'account disabled' USING ERRCODE = 'GK403';
    END IF;
  END IF;
  SELECT * INTO o_order FROM business.orders o
  WHERE o.id = p_order
    AND ((o_account IS NOT NULL AND o.owner_account_id = o_account)
         OR (p_guest_digest IS NOT NULL AND EXISTS (
               SELECT 1 FROM business.order_guest_capability g
               WHERE g.order_id = o.id AND g.capability_digest = p_guest_digest AND g.revoked_at IS NULL
                 AND p_command = ANY (g.allowed_commands))))
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF o_account IS NULL THEN
    SELECT g.id INTO o_capability FROM business.order_guest_capability g
    WHERE g.order_id = p_order AND g.capability_digest = p_guest_digest;
  END IF;
END;
$$;

-- ---------------------------------------------------------------- API

-- API: what cancelling order p_order now would return (see the header), for
-- its owner or a guest capability holder with "cancel". Records the preview
-- and returns it with its hash.
CREATE FUNCTION business.order_cancellation_preview(p_owner uuid, p_guest_digest bytea, p_order uuid, p_now timestamptz)
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
     OR NOT business.cancellation_eligible(v_o.lifecycle) THEN
    RAISE EXCEPTION 'cancellation_not_eligible' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  v_rate := business.cancellation_rate(v_o.full_refund_deadline_at, p_now, false);
  v_paid := business.order_paid_minor(p_order);
  v_amount := business.cancellation_amount(v_paid, v_rate);
  v_alloc := business.cancellation_allocations(p_order, v_amount);
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
  RETURN (v_body::jsonb || pg_catalog.jsonb_build_object('preview_hash', v_hash))::json;
END;
$$;

-- API: the customer's cancellation of order p_order, confirming preview
-- p_preview_id (see the header). Returns the command (the recovery
-- operation) and whether it already existed.
CREATE FUNCTION business.order_cancel_request(
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
  v_paid := business.order_paid_minor(p_order);
  v_amount := business.cancellation_amount(v_paid, v_rate);
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

-- API: the chef's cancellation of order p_order: 100% of the paid
-- aggregate, with a reason note (see the header).
CREATE FUNCTION business.order_chef_cancel_request(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_reason text,
  p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_o business.orders%ROWTYPE;
  v_prior uuid;
  v_paid bigint;
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
  IF p_reason IS NULL OR p_reason <> pg_catalog.btrim(p_reason) OR pg_catalog.char_length(p_reason) NOT BETWEEN 1 AND 500
     OR p_reason ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]' THEN
    RAISE EXCEPTION 'reason_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v_prior := business.order_cancel_admissible(v_o, v_account, NULL, p_key, p_body_sha256, p_expected_version);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  v_paid := business.order_paid_minor(p_order);
  RETURN QUERY SELECT business.order_cancel_record(p_order, v_o.version, 'staff', v_account, NULL, p_key, p_body_sha256,
                                                   p_now, p_env, 'chef_cancelled', 100::smallint, v_paid, v_paid,
                                                   p_reason, NULL),
                      false;
END;
$$;

-- ------------------------------------------------------------- commit

-- Owner-internal: the domain commit of a prepared cancel command (the
-- caller, business.order_command_apply, holds the order, its collections
-- and the recovery operation, and has checked the freeze). Returns the
-- outcome text.
CREATE FUNCTION business.order_cancel_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_reason text := CASE WHEN v.actor_kind = 'staff' THEN 'chef_cancelled' ELSE 'customer_cancelled' END;
  v_rate smallint;
  v_paid bigint;
  v_target bigint;
  v_g business.refund_goal%ROWTYPE;
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
  v_rate := business.cancellation_rate(v_o.full_refund_deadline_at, v.received_at, v.actor_kind = 'staff');
  v_paid := business.order_paid_minor(v.order_id);
  v_target := business.cancellation_amount(v_paid, v_rate);

  -- K03: the code use stays consumed (never restored).
  UPDATE business.orders o
  SET lifecycle = 'cancelled', cancel_reason = v_reason, cancelled_at = v.received_at,
      recovery_state = 'cancellation_pending', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = v.order_id;

  IF v_target > 0 THEN
    SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = v.order_id FOR UPDATE;
    IF NOT FOUND THEN
      INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source, policy_rate, paid_basis_minor)
      VALUES (pg_catalog.gen_random_uuid(), v.order_id, v_target, v_reason,
              CASE WHEN v.actor_kind = 'staff' THEN 'chef' ELSE 'customer' END, v_rate, v_paid)
      RETURNING * INTO v_g;
      PERFORM business.audit_record('system', NULL, 'refund.goal_recorded', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                                 'target_minor', v_target, 'reason', v_reason,
                                                                 'rate', v_rate));
    ELSIF v_g.target_minor < v_target THEN
      -- Defensive: an eligible order has no goal yet. Never lowered.
      UPDATE business.refund_goal g
      SET target_minor = v_target, version = g.version + 1, updated_at = pg_catalog.now()
      WHERE g.id = v_g.id RETURNING * INTO v_g;
      PERFORM business.audit_record('system', NULL, 'refund.goal_raised', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                                 'target_minor', v_target, 'reason', v_reason));
    END IF;
    PERFORM business.refund_reserve(v.order_id);
  END IF;
  -- The day last (lock order K00: goal and refund operations before the day).
  IF EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = v.order_id AND a.day = v_o.handover_day) THEN
    PERFORM business.capacity_release(v.order_id, v_o.handover_day);
  END IF;
  PERFORM business.audit_record(CASE v.actor_kind WHEN 'guest' THEN 'system' ELSE v.actor_kind END, v.actor_account_id,
                                'order.cancelled', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason, 'rate', v_rate,
                                                             'refund_target_minor', v_target));
  RETURN pg_catalog.format(
    '{"status":"cancelled","evidence":{"order_version":%s,"reason":"%s","rate":%s,"paid_minor":%s,"refund_target_minor":%s}}',
    v_o.version + 1, v_reason, v_rate, v_paid, v_target);
END;
$$;

-- As in 0016, plus the cancel command (see the header): its collections are
-- locked after the order (the goal and refund operations follow).
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
  IF v.command IN ('paid_confirm', 'cancel') THEN
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

  IF v.command = 'cancel' THEN
    v_outcome := business.order_cancel_commit(v);
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

-- As in 0014; a cancellation is secured as well (the order then reads
-- cancelled, with its reason and refund).
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
         OR (o.recovery_state = 'cancellation_pending' AND v.command = 'cancel' AND v_status = 'cancelled'));
  RETURN v_state;
END;
$$;

-- As in 0017, plus the cancellation (K05, K11): while a cancel command is
-- unresolved or its commit is not secured the order reads
-- cancellation_pending, without its reason, instant or refund; then
-- cancelled with them. "cancellation" reports the latest cancel command.
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
                                          WHERE ccmd.order_id = o.id AND ccmd.command = 'cancel' AND cr.state <> 'secured')
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
                            WHERE c.order_id = o.id AND c.command = 'cancel'
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
                        'goal_version', g.version, 'reason', g.reason, 'policy_rate', g.policy_rate)
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
                                    AND coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) < g.target_minor))
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

REVOKE ALL ON TABLE business.cancellation_preview FROM PUBLIC;
REVOKE ALL ON FUNCTION business.cancellation_rate(timestamptz, timestamptz, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.cancellation_amount(bigint, smallint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_paid_minor(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.cancellation_allocations(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.cancellation_eligible(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_cancel_record(uuid, integer, text, uuid, uuid, text, text, timestamptz, text, text,
                                                    smallint, bigint, bigint, text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_cancel_admissible(business.orders, uuid, uuid, text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_customer_lock(uuid, bytea, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_cancellation_preview(uuid, bytea, uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_cancel_request(uuid, bytea, uuid, text, text, integer, uuid, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_chef_cancel_request(uuid, uuid, text, text, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_cancel_commit(business.order_command) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_cancellation_preview(uuid, bytea, uuid, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_cancel_request(uuid, bytea, uuid, text, text, integer, uuid, text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_chef_cancel_request(uuid, uuid, text, text, integer, text, text, timestamptz) TO gk_api;
