-- GK-024: transactional email engine (contracts r6.5 K08, schema r1.5
-- notification_intent/job; K11 dispatch protection from 0003).
--
-- A domain transaction commits its state change, the notification intent, its
-- job and an audit event together (business.notification_enqueue, called by
-- the owning domain gateway function). The worker dispatches outside any
-- database transaction under the K11 recovery coordinator, calling only the
-- lease-fenced gateway functions below: each verifies that the caller holds
-- the job's live lease (owner, generation, unexpired) and that the recovery
-- operation is exactly the intent's (operation = idempotency key, aggregate =
-- intent, payload hash = the stored dispatch payload), then changes the
-- recovery phase and the intent in one transaction.
--
-- Intent states: queued -> sending (dispatch prepared) -> accepted (provider
-- message ID) | failed (definitive provider rejection) | unknown (a response
-- was lost or ambiguous; replayed with the same key and body until the
-- provider window ends, then manager reconciliation, never a new send).
-- delivered_to_server and bounced are provider evidence (signed webhook,
-- GK-024 A3); no inbox delivery or read is ever claimed. Every provider call
-- is recorded in the append-only notification_attempt.
--
-- Schema mapping: "key" is idempotency_key; "body_hash" is payload_hash, the
-- SHA-256 of the exact stored dispatch payload (provider, environment,
-- idempotency key and the provider request with recipient and body).
--
-- Jobs gain bounded retries: business.job_fail schedules retries after 1m,
-- 5m, 15m and 1h, then moves the job to attention (manager); job_attention
-- escalates at once; job_requeue (owner tooling) re-admits it.

SET LOCAL ROLE gk_owner;

-- Append-only business audit trail. Detail is bounded and carries no
-- personal data (IDs, revisions and reasons only).
CREATE TABLE business.audit_event (
  id            uuid PRIMARY KEY,
  occurred_at   timestamptz NOT NULL DEFAULT now(),
  actor_kind    text NOT NULL CHECK (actor_kind IN ('system', 'staff', 'customer')),
  actor_id      uuid NULL,
  action        text NOT NULL CHECK (action ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$' AND length(action) <= 80),
  subject_kind  text NOT NULL CHECK (subject_kind ~ '^[a-z][a-z0-9_]{0,39}$'),
  subject_id    uuid NOT NULL,
  transition_id uuid NULL,
  detail        json NULL CHECK (detail IS NULL OR octet_length(detail::text) <= 2048),
  CONSTRAINT audit_actor CHECK ((actor_kind = 'system') = (actor_id IS NULL))
);

CREATE INDEX audit_event_subject_idx ON business.audit_event (subject_kind, subject_id, occurred_at);

ALTER TABLE business.job
  ADD COLUMN failures integer NOT NULL DEFAULT 0 CHECK (failures >= 0),
  ADD COLUMN attention_reason text NULL CHECK (length(attention_reason) BETWEEN 1 AND 200),
  ADD CONSTRAINT job_attention_reason CHECK ((state = 'attention') = (attention_reason IS NOT NULL));

CREATE INDEX job_attention_idx ON business.job (updated_at) WHERE state = 'attention';

CREATE TABLE business.notification_intent (
  id                  uuid PRIMARY KEY,
  transition_id       uuid NOT NULL,
  recipient_id        uuid NOT NULL,
  template_revision   text NOT NULL CHECK (template_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
  purpose             text NOT NULL DEFAULT 'transactional' CHECK (purpose = 'transactional'),
  state               text NOT NULL DEFAULT 'queued'
                      CHECK (state IN ('queued', 'sending', 'accepted', 'delivered_to_server', 'bounced', 'failed', 'unknown')),
  idempotency_key     uuid NOT NULL UNIQUE,
  -- json (not jsonb) keeps the exact bytes that payload_hash covers and that
  -- every replay sends.
  payload             json NOT NULL CHECK (octet_length(payload::text) <= 262144),
  payload_hash        char(64) NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
  job_id              uuid NOT NULL UNIQUE REFERENCES business.job (id) DEFERRABLE INITIALLY DEFERRED,
  -- Anchors the provider idempotency window: fixed before the first
  -- dispatch, never moved; replay_until = first_dispatch_at + 23 hours.
  first_dispatch_at   timestamptz NULL,
  replay_until        timestamptz NULL,
  provider_message_id text NULL CHECK (provider_message_id ~ '^[A-Za-z0-9._-]{1,128}$'),
  -- Recipient or contact revoked: no further dispatch, never retargeted.
  suspended_reason    text NULL CHECK (length(suspended_reason) BETWEEN 1 AND 200),
  version             integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT notification_intent_transition_recipient_template UNIQUE (transition_id, recipient_id, template_revision),
  CONSTRAINT notification_dispatch_window CHECK ((first_dispatch_at IS NULL) = (replay_until IS NULL)
                                                 AND (replay_until IS NULL OR replay_until = first_dispatch_at + interval '23 hours')),
  CONSTRAINT notification_dispatched CHECK (state = 'queued' OR first_dispatch_at IS NOT NULL),
  CONSTRAINT notification_provider_reference CHECK (state NOT IN ('accepted', 'delivered_to_server', 'bounced') OR provider_message_id IS NOT NULL)
);

CREATE INDEX notification_intent_state_idx ON business.notification_intent (state, first_dispatch_at);

-- One row per provider call, in order. Never updated or deleted.
CREATE TABLE business.notification_attempt (
  id                  uuid PRIMARY KEY,
  intent_id           uuid NOT NULL REFERENCES business.notification_intent (id),
  attempt_no          integer NOT NULL CHECK (attempt_no >= 1),
  job_id              uuid NOT NULL,
  job_generation      bigint NOT NULL CHECK (job_generation >= 1),
  -- accepted: provider message ID returned; rejected: definitively refused,
  -- not sent; deferred: not processed now (rate limit, concurrent request);
  -- unknown: no or ambiguous response; refused: credential, permission or
  -- sender configuration; conflict: the key was used with a different body.
  result              text NOT NULL CHECK (result IN ('accepted', 'rejected', 'deferred', 'unknown', 'refused', 'conflict')),
  http_status         integer NULL CHECK (http_status BETWEEN 100 AND 599),
  provider_code       text NULL CHECK (provider_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  provider_message_id text NULL CHECK (provider_message_id ~ '^[A-Za-z0-9._-]{1,128}$'),
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT notification_attempt_order UNIQUE (intent_id, attempt_no),
  CONSTRAINT notification_attempt_reference CHECK ((result = 'accepted') = (provider_message_id IS NOT NULL))
);

-- Owner-internal: append an audit event.
CREATE FUNCTION business.audit_record(
  p_actor_kind text, p_actor_id uuid, p_action text, p_subject_kind text, p_subject_id uuid,
  p_transition_id uuid, p_detail json)
RETURNS uuid
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  INSERT INTO business.audit_event (id, actor_kind, actor_id, action, subject_kind, subject_id, transition_id, detail)
  VALUES (pg_catalog.gen_random_uuid(), p_actor_kind, p_actor_id, p_action, p_subject_kind, p_subject_id, p_transition_id, p_detail)
  RETURNING id;
$$;

-- Owner-internal: called by a domain gateway function inside the transaction
-- that commits the business transition, so the intent, its job and the audit
-- event exist exactly when the transition does. The payload is built by the
-- application (notifications.NewNotice): its idempotency_key and intent tag
-- must be this intent's. A second intent for the same transition, recipient
-- and template revision is refused (unique_violation).
CREATE FUNCTION business.notification_enqueue(
  p_intent_id uuid, p_transition_id uuid, p_recipient_id uuid, p_template_revision text,
  p_idempotency_key uuid, p_payload json, p_actor_kind text, p_actor_id uuid)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_job uuid := pg_catalog.gen_random_uuid();
  v_request jsonb;
BEGIN
  IF p_intent_id IS NULL OR p_transition_id IS NULL OR p_recipient_id IS NULL OR p_idempotency_key IS NULL OR p_payload IS NULL THEN
    RAISE EXCEPTION 'notification_enqueue requires intent, transition, recipient, key and payload'
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  v_request := (p_payload -> 'request')::jsonb;
  IF p_payload ->> 'provider' IS DISTINCT FROM 'resend'
     OR p_payload ->> 'idempotency_key' IS DISTINCT FROM p_idempotency_key::text
     OR pg_catalog.jsonb_typeof(v_request) IS DISTINCT FROM 'object'
     OR NOT v_request @> pg_catalog.jsonb_build_object('tags', pg_catalog.jsonb_build_array(
                            pg_catalog.jsonb_build_object('name', 'intent', 'value', p_intent_id::text))) THEN
    RAISE EXCEPTION 'notification payload does not belong to intent %', p_intent_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;

  INSERT INTO business.notification_intent
    (id, transition_id, recipient_id, template_revision, idempotency_key, payload, payload_hash, job_id)
  VALUES (p_intent_id, p_transition_id, p_recipient_id, p_template_revision, p_idempotency_key, p_payload,
          pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(p_payload::text, 'UTF8')), 'hex'), v_job);
  INSERT INTO business.job (id, operation_id, next_attempt_at) VALUES (v_job, p_intent_id, pg_catalog.now());
  PERFORM business.audit_record(p_actor_kind, p_actor_id, 'notification.queued', 'notification_intent', p_intent_id,
                                p_transition_id, pg_catalog.json_build_object('template_revision', p_template_revision));
  RETURN p_intent_id;
END;
$$;

-- Owner-internal: the recipient or contact was revoked. An intent that is not
-- yet settled is never dispatched again (and never retargeted); returns
-- whether this call suspended it.
CREATE FUNCTION business.notification_suspend(p_intent_id uuid, p_reason text, p_actor_kind text, p_actor_id uuid)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_transition uuid;
BEGIN
  UPDATE business.notification_intent
  SET suspended_reason = p_reason, version = version + 1, updated_at = pg_catalog.now()
  WHERE id = p_intent_id AND suspended_reason IS NULL AND state IN ('queued', 'sending', 'unknown')
  RETURNING transition_id INTO v_transition;
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  PERFORM business.audit_record(p_actor_kind, p_actor_id, 'notification.suspended', 'notification_intent', p_intent_id,
                                v_transition, NULL);
  RETURN true;
END;
$$;

-- Fails a leased job: retry after 1m, 5m, 15m, then 1h; the fifth failure
-- moves it to attention with p_reason. Returns the new state, failure count
-- and next attempt, or no row when the caller's lease is stale.
CREATE FUNCTION business.job_fail(p_job_id uuid, p_worker uuid, p_generation bigint, p_reason text)
RETURNS TABLE (job_state text, job_failures integer, job_next_attempt_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_delays constant interval[] := ARRAY[interval '1 minute', interval '5 minutes', interval '15 minutes', interval '1 hour'];
BEGIN
  IF p_reason IS NULL OR length(p_reason) NOT BETWEEN 1 AND 200 THEN
    RAISE EXCEPTION 'job_fail requires a reason of 1 to 200 characters' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.job
  SET failures = failures + 1,
      state = CASE WHEN failures + 1 > pg_catalog.array_length(v_delays, 1) THEN 'attention' ELSE 'retry' END,
      attention_reason = CASE WHEN failures + 1 > pg_catalog.array_length(v_delays, 1) THEN p_reason END,
      next_attempt_at = CASE WHEN failures + 1 > pg_catalog.array_length(v_delays, 1) THEN next_attempt_at
                             ELSE pg_catalog.now() + v_delays[failures + 1] END,
      lease_owner = NULL,
      lease_until = NULL,
      version = version + 1,
      updated_at = pg_catalog.now()
  WHERE id = p_job_id AND state = 'running' AND lease_owner = p_worker AND generation = p_generation
  RETURNING state, failures, next_attempt_at INTO job_state, job_failures, job_next_attempt_at;
  IF FOUND THEN
    RETURN NEXT;
  END IF;
END;
$$;

-- Moves a leased job to attention at once. Returns false when the lease is stale.
CREATE FUNCTION business.job_attention(p_job_id uuid, p_worker uuid, p_generation bigint, p_reason text)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_reason IS NULL OR length(p_reason) NOT BETWEEN 1 AND 200 THEN
    RAISE EXCEPTION 'job_attention requires a reason of 1 to 200 characters' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.job
  SET state = 'attention', attention_reason = p_reason, lease_owner = NULL, lease_until = NULL,
      version = version + 1, updated_at = pg_catalog.now()
  WHERE id = p_job_id AND state = 'running' AND lease_owner = p_worker AND generation = p_generation;
  RETURN FOUND;
END;
$$;

-- Owner tooling (manager decision): re-admit a job in attention, immediately
-- claimable with a fresh retry budget.
CREATE FUNCTION business.job_requeue(p_job_id uuid)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  UPDATE business.job
  SET state = 'queued', attention_reason = NULL, failures = 0, next_attempt_at = pg_catalog.now(),
      version = version + 1, updated_at = pg_catalog.now()
  WHERE id = p_job_id AND state = 'attention';
  RETURN FOUND;
END;
$$;

-- Owner-internal: the intent of the job the caller holds a live lease on,
-- locked. Raises GK001 when the lease is not held (stale generation, another
-- owner, expired or finalized); returns NULL when the job is not a
-- notification job.
CREATE FUNCTION business.notification_leased_intent(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS business.notification_intent
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_operation uuid;
  v_intent business.notification_intent;
BEGIN
  SELECT j.operation_id INTO v_operation
  FROM business.job j
  WHERE j.id = p_job_id AND j.state = 'running' AND j.lease_owner = p_worker AND j.generation = p_generation
    AND j.lease_until > pg_catalog.now()
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'job % is not leased to this worker generation', p_job_id USING ERRCODE = 'GK001';
  END IF;
  SELECT * INTO v_intent FROM business.notification_intent i
  WHERE i.job_id = p_job_id AND i.id = v_operation
  FOR UPDATE;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  RETURN v_intent;
END;
$$;

-- Owner-internal: the leased intent whose recovery operation is p_operation_id.
CREATE FUNCTION business.notification_leased_operation(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS business.notification_intent
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_intent business.notification_intent := business.notification_leased_intent(p_job_id, p_worker, p_generation);
BEGIN
  IF v_intent.id IS NULL OR v_intent.idempotency_key IS DISTINCT FROM p_operation_id THEN
    RAISE EXCEPTION 'recovery operation % is not the dispatch of job %', p_operation_id, p_job_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v_intent;
END;
$$;

-- Worker: start (or resume) the leased job's dispatch. Status 'ready' returns
-- the dispatch identity, fixing first_dispatch_at/replay_until on first use;
-- 'frozen' (restore in progress) and 'suspended' fix nothing and return no
-- payload; 'not_notification' means another handler owns the job.
CREATE FUNCTION business.notification_dispatch_begin(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS TABLE (status text, intent_id uuid, idempotency_key uuid, payload json, replay_until timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_intent(p_job_id, p_worker, p_generation);
BEGIN
  IF v.id IS NULL THEN
    RETURN QUERY SELECT 'not_notification'::text, NULL::uuid, NULL::uuid, NULL::json, NULL::timestamptz;
    RETURN;
  END IF;
  IF v.suspended_reason IS NOT NULL THEN
    RETURN QUERY SELECT 'suspended'::text, v.id, NULL::uuid, NULL::json, NULL::timestamptz;
    RETURN;
  END IF;
  IF business.recovery_frozen() THEN
    RETURN QUERY SELECT 'frozen'::text, v.id, NULL::uuid, NULL::json, NULL::timestamptz;
    RETURN;
  END IF;
  IF v.first_dispatch_at IS NULL THEN
    UPDATE business.notification_intent i
    SET first_dispatch_at = pg_catalog.now(), replay_until = pg_catalog.now() + interval '23 hours',
        version = i.version + 1, updated_at = pg_catalog.now()
    WHERE i.id = v.id
    RETURNING i.replay_until INTO v.replay_until;
  END IF;
  RETURN QUERY SELECT 'ready'::text, v.id, v.idempotency_key, v.payload, v.replay_until;
END;
$$;

-- Worker: recovery_reserve for exactly the leased intent's dispatch.
CREATE FUNCTION business.notification_recovery_reserve(
  p_job_id uuid, p_worker uuid, p_generation bigint,
  p_operation_id uuid, p_kind text, p_aggregate_id uuid, p_prior_version integer,
  p_payload_hash text, p_envelope_key text, p_replay_until timestamptz)
RETURNS TABLE (result text, op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  IF p_kind IS DISTINCT FROM 'dispatch' OR p_aggregate_id IS DISTINCT FROM v.id OR p_prior_version IS DISTINCT FROM 0
     OR p_payload_hash IS DISTINCT FROM v.payload_hash::text OR v.replay_until IS NULL
     OR p_replay_until IS DISTINCT FROM v.replay_until
     OR p_envelope_key !~ ('^recovery/[a-z0-9][a-z0-9-]{0,31}/op/' || v.id::text || '/0000000000-' || v.idempotency_key::text || '$') THEN
    RAISE EXCEPTION 'recovery operation % does not match the dispatch of intent %', p_operation_id, v.id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN QUERY SELECT * FROM business.recovery_reserve(p_operation_id, p_kind, p_aggregate_id, p_prior_version,
                                                      p_payload_hash, p_envelope_key, p_replay_until);
END;
$$;

-- Worker: the prepared envelope is acknowledged; the intent is now sending.
CREATE FUNCTION business.notification_recovery_prepared(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_state text;
BEGIN
  v_state := business.recovery_mark_prepared(p_operation_id);
  UPDATE business.notification_intent
  SET state = 'sending', version = version + 1, updated_at = pg_catalog.now()
  WHERE id = v.id AND state = 'queued';
  RETURN v_state;
END;
$$;

-- Worker: record one provider call. An unknown or conflicting response makes
-- a sending intent unknown. Returns the attempt number.
CREATE FUNCTION business.notification_attempt_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid,
  p_result text, p_http_status integer, p_provider_code text, p_provider_message_id text)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_no integer;
BEGIN
  IF v.state = 'queued' THEN
    RAISE EXCEPTION 'intent % has no prepared dispatch', v.id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT coalesce(max(a.attempt_no), 0) + 1 INTO v_no FROM business.notification_attempt a WHERE a.intent_id = v.id;
  INSERT INTO business.notification_attempt
    (id, intent_id, attempt_no, job_id, job_generation, result, http_status, provider_code, provider_message_id)
  VALUES (pg_catalog.gen_random_uuid(), v.id, v_no, p_job_id, p_generation, p_result, p_http_status, p_provider_code,
          p_provider_message_id);
  IF p_result IN ('unknown', 'conflict') THEN
    UPDATE business.notification_intent
    SET state = 'unknown', version = version + 1, updated_at = pg_catalog.now()
    WHERE id = v.id AND state = 'sending';
  END IF;
  RETURN v_no;
END;
$$;

-- Worker: record the dispatch outcome (recovery committed) and settle the
-- intent: accepted with the provider message ID, or failed after a
-- definitive rejection. Later provider evidence (delivered/bounced) is never
-- overwritten.
CREATE FUNCTION business.notification_recovery_committed(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_outcome json, p_outcome_hash text)
RETURNS TABLE (op_state text, changed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_status text := p_outcome ->> 'status';
  v_state text;
  v_changed boolean;
BEGIN
  IF v_status IS NULL OR v_status NOT IN ('accepted', 'rejected') THEN
    RAISE EXCEPTION 'unsupported dispatch outcome %', v_status USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT c.op_state, c.changed INTO v_state, v_changed
  FROM business.recovery_mark_committed(p_operation_id, p_outcome, p_outcome_hash, false) c;
  IF v_status = 'accepted' THEN
    UPDATE business.notification_intent
    SET state = 'accepted', provider_message_id = p_outcome -> 'evidence' ->> 'provider_message_id',
        version = version + 1, updated_at = pg_catalog.now()
    WHERE id = v.id AND state IN ('sending', 'unknown');
  ELSE
    UPDATE business.notification_intent
    SET state = 'failed', version = version + 1, updated_at = pg_catalog.now()
    WHERE id = v.id AND state IN ('sending', 'unknown');
  END IF;
  RETURN QUERY SELECT v_state, v_changed;
END;
$$;

-- Worker: the outcome envelope is acknowledged.
CREATE FUNCTION business.notification_recovery_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  RETURN business.recovery_mark_secured(v.idempotency_key);
END;
$$;

-- Worker: the outcome cannot be established safely (provider window ended);
-- the intent stays unknown for manager reconciliation.
CREATE FUNCTION business.notification_recovery_reconcile(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_reason text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_state text;
BEGIN
  v_state := business.recovery_mark_reconcile(p_operation_id, p_reason);
  UPDATE business.notification_intent
  SET state = 'unknown', version = version + 1, updated_at = pg_catalog.now()
  WHERE id = v.id AND state IN ('sending', 'unknown');
  RETURN v_state;
END;
$$;

-- Worker: the restore freeze, checked right before each provider call.
CREATE FUNCTION business.notification_recovery_frozen(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_intent(p_job_id, p_worker, p_generation);
BEGIN
  IF v.id IS NULL THEN
    RAISE EXCEPTION 'job % is not a notification job', p_job_id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN business.recovery_frozen();
END;
$$;

REVOKE ALL ON TABLE business.audit_event FROM PUBLIC;
REVOKE ALL ON TABLE business.notification_intent FROM PUBLIC;
REVOKE ALL ON TABLE business.notification_attempt FROM PUBLIC;
REVOKE ALL ON FUNCTION business.audit_record(text, uuid, text, text, uuid, uuid, json) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_enqueue(uuid, uuid, uuid, text, uuid, json, text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_suspend(uuid, text, text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.job_fail(uuid, uuid, bigint, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.job_attention(uuid, uuid, bigint, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.job_requeue(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_leased_intent(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_leased_operation(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_dispatch_begin(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_recovery_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_recovery_prepared(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_attempt_record(uuid, uuid, bigint, uuid, text, integer, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_recovery_committed(uuid, uuid, bigint, uuid, json, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_recovery_secured(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_recovery_reconcile(uuid, uuid, bigint, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_recovery_frozen(uuid, uuid, bigint) FROM PUBLIC;

-- The worker dispatches only through the lease-fenced gateway; no runtime
-- role reads intents or attempts directly.
GRANT EXECUTE ON FUNCTION business.job_fail(uuid, uuid, bigint, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.job_attention(uuid, uuid, bigint, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.notification_dispatch_begin(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.notification_recovery_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamptz) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.notification_recovery_prepared(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.notification_attempt_record(uuid, uuid, bigint, uuid, text, integer, text, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.notification_recovery_committed(uuid, uuid, bigint, uuid, json, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.notification_recovery_secured(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.notification_recovery_reconcile(uuid, uuid, bigint, uuid, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.notification_recovery_frozen(uuid, uuid, bigint) TO gk_worker;
