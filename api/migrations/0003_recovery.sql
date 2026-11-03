-- GK-024: minimal K11 recovery coordinator state (contracts r6.5 K11, schema
-- r1.5 recovery_operation). The application writes the independent encrypted
-- prepared/outcome envelopes to B2; this table records each protected
-- operation's phase so that a crash at any point resumes without repeating
-- or losing an external effect:
--   reserved   -> prepared envelope acknowledged -> prepared
--   prepared   -> effect outcome recorded locally -> committed
--   committed  -> outcome envelope acknowledged   -> secured
--   reserved/prepared -> reconcile (manager attention; never a blind retry)
-- One unresolved operation per aggregate serializes revisions.
--
-- Owner-internal: domain gateway functions call these inside their own
-- transactions so a domain state change and its recovery phase commit
-- together. No runtime role reads the table or executes these directly.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.recovery_operation (
  id               uuid PRIMARY KEY,
  kind             text NOT NULL CHECK (kind IN ('dispatch', 'command')),
  aggregate_id     uuid NOT NULL,
  prior_version    integer NOT NULL CHECK (prior_version >= 0),
  payload_hash     char(64) NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
  envelope_key     text NOT NULL UNIQUE CHECK (length(envelope_key) BETWEEN 1 AND 512),
  -- Dispatch only: last instant an identical replay is provider-idempotent.
  replay_until     timestamptz NULL,
  state            text NOT NULL DEFAULT 'reserved'
                   CHECK (state IN ('reserved', 'prepared', 'committed', 'secured', 'reconcile')),
  outcome          jsonb NULL CHECK (outcome IS NULL OR octet_length(outcome::text) <= 4096),
  outcome_hash     char(64) NULL CHECK (outcome_hash ~ '^[0-9a-f]{64}$'),
  reconcile_reason text NULL CHECK (length(reconcile_reason) BETWEEN 1 AND 200),
  prepared_ack_at  timestamptz NULL,
  outcome_ack_at   timestamptz NULL,
  version          integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recovery_dispatch_replay CHECK ((kind = 'dispatch') = (replay_until IS NOT NULL)),
  CONSTRAINT recovery_prepared_ack CHECK (state IN ('reserved', 'reconcile') OR prepared_ack_at IS NOT NULL),
  CONSTRAINT recovery_outcome CHECK ((state IN ('committed', 'secured')) = (outcome IS NOT NULL AND outcome_hash IS NOT NULL)),
  CONSTRAINT recovery_secured CHECK ((state = 'secured') = (outcome_ack_at IS NOT NULL)),
  CONSTRAINT recovery_reconcile CHECK ((state = 'reconcile') = (reconcile_reason IS NOT NULL))
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
-- current state/outcome) or 'busy' (another unresolved operation holds the
-- aggregate). A different operation reusing p_id is an error.
CREATE FUNCTION business.recovery_reserve(
  p_id uuid, p_kind text, p_aggregate_id uuid, p_prior_version integer,
  p_payload_hash text, p_envelope_key text, p_replay_until timestamptz)
RETURNS TABLE (result text, op_state text, op_outcome jsonb, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  r business.recovery_operation;
BEGIN
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
  INSERT INTO business.recovery_operation AS o
    (id, kind, aggregate_id, prior_version, payload_hash, envelope_key, replay_until)
  VALUES (p_id, p_kind, p_aggregate_id, p_prior_version, p_payload_hash, p_envelope_key, p_replay_until)
  ON CONFLICT (aggregate_id) WHERE state <> 'secured' DO NOTHING;
  IF NOT FOUND THEN
    RETURN QUERY SELECT 'busy'::text, NULL::text, NULL::jsonb, NULL::text;
    RETURN;
  END IF;
  RETURN QUERY SELECT 'reserved'::text, 'reserved'::text, NULL::jsonb, NULL::text;
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

-- prepared -> committed with the effect's outcome. Repeating with the same
-- outcome is a no-op; a different outcome for a recorded one is an error.
CREATE FUNCTION business.recovery_mark_committed(p_id uuid, p_outcome jsonb, p_outcome_hash text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  r business.recovery_operation;
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
    RETURN r.state;
  END IF;
  IF r.state <> 'prepared' THEN
    RAISE EXCEPTION 'recovery operation % is %, not prepared', p_id, r.state
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.recovery_operation
  SET state = 'committed', outcome = p_outcome, outcome_hash = p_outcome_hash,
      version = version + 1, updated_at = pg_catalog.now()
  WHERE id = p_id;
  RETURN 'committed';
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

-- reserved/prepared -> reconcile when the outcome cannot be established safely.
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
  WHERE id = p_id AND state IN ('reserved', 'prepared')
  RETURNING state INTO v_state;
  IF FOUND THEN
    RETURN v_state;
  END IF;
  SELECT state INTO v_state FROM business.recovery_operation WHERE id = p_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'unknown recovery operation %', p_id USING ERRCODE = 'no_data_found';
  ELSIF v_state <> 'reconcile' THEN
    RAISE EXCEPTION 'recovery operation % is %; its outcome is already recorded', p_id, v_state
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
REVOKE ALL ON FUNCTION business.recovery_mark_prepared(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_mark_committed(uuid, jsonb, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_mark_secured(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_mark_reconcile(uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_frozen() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.recovery_set_freeze(boolean, text) FROM PUBLIC;
