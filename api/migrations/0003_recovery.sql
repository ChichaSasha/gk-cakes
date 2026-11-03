-- GK-024: minimal K11 recovery coordinator state (contracts r6.5 K11, schema
-- r1.5 recovery_operation). The application writes the independent encrypted
-- prepared/outcome envelopes to B2; this table records each protected
-- dispatch's phase so that a crash at any point resumes without repeating or
-- losing an external effect:
--   reserved   -> prepared envelope acknowledged -> prepared
--   prepared   -> effect outcome recorded locally -> committed
--   committed  -> outcome envelope acknowledged   -> secured
--   prepared   -> reconcile (outcome unknown past the replay deadline, or
--                 unrecordable; manager attention, never a blind retry)
--   reconcile  -> manager-determined outcome      -> committed
-- One unresolved operation per aggregate; prior versions only increase.
-- Only outgoing dispatches (email now; payments/refunds later) are protected
-- here. Accepted-command protection (order confirmation/changes) is designed
-- with GK-025 and will extend kind.
--
-- Owner-internal: domain gateway functions call these inside their own
-- transactions so a domain state change and its recovery phase commit
-- together. No runtime role executes these directly.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.recovery_operation (
  id               uuid PRIMARY KEY,
  kind             text NOT NULL CHECK (kind = 'dispatch'),
  aggregate_id     uuid NOT NULL,
  prior_version    integer NOT NULL CHECK (prior_version >= 0),
  payload_hash     char(64) NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
  envelope_key     text NOT NULL UNIQUE CHECK (length(envelope_key) BETWEEN 1 AND 512),
  -- Last instant an identical replay is provider-idempotent; fixed at the
  -- first reservation, never extended.
  replay_until     timestamptz NOT NULL,
  state            text NOT NULL DEFAULT 'reserved'
                   CHECK (state IN ('reserved', 'prepared', 'committed', 'secured', 'reconcile')),
  -- json (not jsonb) keeps the exact bytes that outcome_hash covers.
  outcome          json NULL CHECK (outcome IS NULL OR octet_length(outcome::text) <= 4096),
  outcome_hash     char(64) NULL CHECK (outcome_hash ~ '^[0-9a-f]{64}$'),
  reconcile_reason text NULL CHECK (length(reconcile_reason) BETWEEN 1 AND 200),
  prepared_ack_at  timestamptz NULL,
  outcome_ack_at   timestamptz NULL,
  version          integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recovery_prepared_ack CHECK ((state = 'reserved') = (prepared_ack_at IS NULL)),
  CONSTRAINT recovery_outcome CHECK ((state IN ('committed', 'secured')) = (outcome IS NOT NULL AND outcome_hash IS NOT NULL)),
  CONSTRAINT recovery_secured CHECK ((state = 'secured') = (outcome_ack_at IS NOT NULL)),
  CONSTRAINT recovery_reconcile CHECK ((state = 'reconcile') = (reconcile_reason IS NOT NULL)),
  CONSTRAINT recovery_aggregate_version UNIQUE (aggregate_id, prior_version)
);

CREATE UNIQUE INDEX recovery_operation_open_aggregate
  ON business.recovery_operation (aggregate_id) WHERE state <> 'secured';

-- Restore freezes every outgoing payment/refund/email effect until reconciled.
CREATE TABLE business.recovery_freeze (
  singleton  boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  frozen     boolean NOT NULL DEFAULT false,
  reason     text NULL CHECK (length(reason) BETWEEN 1 AND 200),
  changed_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recovery_freeze_reason CHECK (NOT frozen OR reason IS NOT NULL)
);
INSERT INTO business.recovery_freeze DEFAULT VALUES;

-- Returns result 'reserved' (new), 'existing' (same operation, with its
-- current state/outcome), 'busy' (another unresolved operation holds the
-- aggregate) or 'stale' (the aggregate already has another operation at this
-- or a later prior version). A different operation reusing p_id is an error.
-- Safe under concurrency: a reserve that loses an insert race re-reads before
-- classifying the conflict.
CREATE FUNCTION business.recovery_reserve(
  p_id uuid, p_kind text, p_aggregate_id uuid, p_prior_version integer,
  p_payload_hash text, p_envelope_key text, p_replay_until timestamptz)
RETURNS TABLE (result text, op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  r business.recovery_operation;
BEGIN
  FOR attempt IN 1..2 LOOP
    SELECT * INTO r FROM business.recovery_operation o WHERE o.id = p_id FOR UPDATE;
    IF FOUND THEN
      IF r.kind <> p_kind OR r.aggregate_id <> p_aggregate_id OR r.prior_version <> p_prior_version
         OR r.payload_hash <> p_payload_hash OR r.envelope_key <> p_envelope_key
         OR r.replay_until IS DISTINCT FROM p_replay_until THEN
        RAISE EXCEPTION 'recovery operation % already exists with a different identity', p_id
          USING ERRCODE = 'unique_violation';
      END IF;
      RETURN QUERY SELECT 'existing'::text, r.state, r.outcome, r.outcome_hash::text;
      RETURN;
    END IF;
    EXIT WHEN attempt = 2;
    IF EXISTS (SELECT 1 FROM business.recovery_operation o
               WHERE o.aggregate_id = p_aggregate_id AND o.prior_version >= p_prior_version AND o.id <> p_id) THEN
      RETURN QUERY SELECT 'stale'::text, NULL::text, NULL::json, NULL::text;
      RETURN;
    END IF;
    INSERT INTO business.recovery_operation
      (id, kind, aggregate_id, prior_version, payload_hash, envelope_key, replay_until)
    VALUES (p_id, p_kind, p_aggregate_id, p_prior_version, p_payload_hash, p_envelope_key, p_replay_until)
    ON CONFLICT DO NOTHING;
    IF FOUND THEN
      RETURN QUERY SELECT 'reserved'::text, 'reserved'::text, NULL::json, NULL::text;
      RETURN;
    END IF;
    -- A unique conflict: possibly this same operation inserted concurrently.
    -- Look again (new snapshot) before classifying it.
  END LOOP;
  IF EXISTS (SELECT 1 FROM business.recovery_operation o
             WHERE o.aggregate_id = p_aggregate_id AND o.prior_version >= p_prior_version AND o.id <> p_id) THEN
    RETURN QUERY SELECT 'stale'::text, NULL::text, NULL::json, NULL::text;
    RETURN;
  END IF;
  IF EXISTS (SELECT 1 FROM business.recovery_operation o WHERE o.envelope_key = p_envelope_key AND o.id <> p_id) THEN
    RAISE EXCEPTION 'envelope key % belongs to another recovery operation', p_envelope_key
      USING ERRCODE = 'unique_violation';
  END IF;
  RETURN QUERY SELECT 'busy'::text, NULL::text, NULL::json, NULL::text;
END;
$$;

-- Read-only lookup with the same identity check as recovery_reserve; result
-- 'existing' or 'missing'. Used where an operation must already exist.
CREATE FUNCTION business.recovery_lookup(
  p_id uuid, p_kind text, p_aggregate_id uuid, p_prior_version integer,
  p_payload_hash text, p_envelope_key text, p_replay_until timestamptz)
RETURNS TABLE (result text, op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  r business.recovery_operation;
BEGIN
  SELECT * INTO r FROM business.recovery_operation o WHERE o.id = p_id;
  IF NOT FOUND THEN
    RETURN QUERY SELECT 'missing'::text, NULL::text, NULL::json, NULL::text;
    RETURN;
  END IF;
  IF r.kind <> p_kind OR r.aggregate_id <> p_aggregate_id OR r.prior_version <> p_prior_version
     OR r.payload_hash <> p_payload_hash OR r.envelope_key <> p_envelope_key
     OR r.replay_until IS DISTINCT FROM p_replay_until THEN
    RAISE EXCEPTION 'recovery operation % exists with a different identity', p_id
      USING ERRCODE = 'unique_violation';
  END IF;
  RETURN QUERY SELECT 'existing'::text, r.state, r.outcome, r.outcome_hash::text;
END;
$$;

-- reserved -> prepared after the prepared envelope is acknowledged. Returns
-- the resulting state; repeating it on an advanced operation is a no-op.
CREATE FUNCTION business.recovery_mark_prepared(p_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_state text;
BEGIN
  UPDATE business.recovery_operation
  SET state = 'prepared', prepared_ack_at = pg_catalog.now(), version = version + 1, updated_at = pg_catalog.now()
  WHERE id = p_id AND state = 'reserved'
  RETURNING state INTO v_state;
  IF NOT FOUND THEN
    SELECT state INTO v_state FROM business.recovery_operation WHERE id = p_id;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'unknown recovery operation %', p_id USING ERRCODE = 'no_data_found';
    END IF;
  END IF;
  RETURN v_state;
END;
$$;

-- prepared -> committed with the effect's outcome (changed = true), or
-- reconcile -> committed with a manager-determined outcome when
-- p_from_reconcile. Repeating with the same outcome is a no-op (changed =
-- false); a different outcome for a recorded one is an error.
CREATE FUNCTION business.recovery_mark_committed(p_id uuid, p_outcome json, p_outcome_hash text, p_from_reconcile boolean)
RETURNS TABLE (op_state text, changed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  r business.recovery_operation;
  v_expected text := CASE WHEN p_from_reconcile THEN 'reconcile' ELSE 'prepared' END;
BEGIN
  SELECT * INTO r FROM business.recovery_operation WHERE id = p_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'unknown recovery operation %', p_id USING ERRCODE = 'no_data_found';
  END IF;
  IF r.state IN ('committed', 'secured') THEN
    IF r.outcome_hash <> p_outcome_hash THEN
      RAISE EXCEPTION 'recovery operation % already has a different outcome', p_id
        USING ERRCODE = 'unique_violation';
    END IF;
    RETURN QUERY SELECT r.state, false;
    RETURN;
  END IF;
  IF r.state <> v_expected OR r.prepared_ack_at IS NULL THEN
    RAISE EXCEPTION 'recovery operation % is % (prepared acknowledgment %)', p_id, r.state, r.prepared_ack_at
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.recovery_operation
  SET state = 'committed', outcome = p_outcome, outcome_hash = p_outcome_hash, reconcile_reason = NULL,
      version = version + 1, updated_at = pg_catalog.now()
  WHERE id = p_id;
  RETURN QUERY SELECT 'committed'::text, true;
END;
$$;

-- committed -> secured after the outcome envelope is acknowledged.
CREATE FUNCTION business.recovery_mark_secured(p_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_state text;
BEGIN
  UPDATE business.recovery_operation
  SET state = 'secured', outcome_ack_at = pg_catalog.now(), version = version + 1, updated_at = pg_catalog.now()
  WHERE id = p_id AND state = 'committed'
  RETURNING state INTO v_state;
  IF FOUND THEN
    RETURN v_state;
  END IF;
  SELECT state INTO v_state FROM business.recovery_operation WHERE id = p_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'unknown recovery operation %', p_id USING ERRCODE = 'no_data_found';
  ELSIF v_state <> 'secured' THEN
    RAISE EXCEPTION 'recovery operation % is %, not committed', p_id, v_state
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v_state;
END;
$$;

-- prepared -> reconcile when the outcome cannot be established safely. (A
-- reserved operation never ran its effect; it is simply resumed.)
CREATE FUNCTION business.recovery_mark_reconcile(p_id uuid, p_reason text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_state text;
BEGIN
  UPDATE business.recovery_operation
  SET state = 'reconcile', reconcile_reason = p_reason, version = version + 1, updated_at = pg_catalog.now()
  WHERE id = p_id AND state = 'prepared'
  RETURNING state INTO v_state;
  IF FOUND THEN
    RETURN v_state;
  END IF;
  SELECT state INTO v_state FROM business.recovery_operation WHERE id = p_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'unknown recovery operation %', p_id USING ERRCODE = 'no_data_found';
  ELSIF v_state <> 'reconcile' THEN
    RAISE EXCEPTION 'recovery operation % is %, not prepared', p_id, v_state
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v_state;
END;
$$;

CREATE FUNCTION business.recovery_frozen()
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT frozen FROM business.recovery_freeze WHERE singleton;
$$;

-- Restore tooling only (migration credential as gk_owner).
CREATE FUNCTION business.recovery_set_freeze(p_frozen boolean, p_reason text)
RETURNS void
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  UPDATE business.recovery_freeze
  SET frozen = p_frozen, reason = CASE WHEN p_frozen THEN p_reason END, changed_at = pg_catalog.now()
  WHERE singleton;
$$;

REVOKE ALL ON TABLE business.recovery_operation FROM PUBLIC;
REVOKE ALL ON TABLE business.recovery_freeze FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_reserve(uuid, text, uuid, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_lookup(uuid, text, uuid, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_mark_prepared(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_mark_committed(uuid, json, text, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_mark_secured(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_mark_reconcile(uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_frozen() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_set_freeze(boolean, text) FROM PUBLIC;
