-- GK-024 A3: signed Resend webhook inbox (contracts r6.5 K08; provider
-- evidence, never an inbox delivery or read claim).
--
-- The API verifies the Svix signature, then calls business.email_event_record
-- once per delivery, before acknowledging it: the event is stored (deduplicated
-- by svix-id, which Resend keeps across retries), correlated to its
-- notification intent and applied to the intent's state in one transaction.
-- Correlation is by provider message ID, or by the intent tag the engine adds
-- to every request (so an event that arrives before the worker recorded the
-- acceptance, or after the acceptance response was lost, still matches).
-- Evidence only moves an intent forward:
--   sending/unknown < accepted < delivered_to_server < failed = bounced
-- so out-of-order and adverse facts are kept in the inbox and a later, less
-- informative event never overwrites a bounce or failure. A bounce or failure
-- found this way is also audited (notification.adverse_evidence). Only minimized
-- evidence is stored: event type, times, IDs, bounce/failure/suppression
-- categories and the payload hash; never recipients, subjects or provider
-- messages.

SET LOCAL ROLE gk_owner;

-- One intent per provider message.
CREATE UNIQUE INDEX notification_intent_provider_message_idx
  ON business.notification_intent (provider_message_id) WHERE provider_message_id IS NOT NULL;

CREATE TABLE business.email_event (
  id                  uuid PRIMARY KEY,
  svix_id             text NOT NULL UNIQUE CHECK (svix_id ~ '^[A-Za-z0-9_-]{1,128}$'),
  event_type          text NOT NULL CHECK (event_type ~ '^email\.[a-z_]{1,40}$'),
  provider_created_at timestamptz NULL,
  provider_message_id text NULL CHECK (provider_message_id ~ '^[A-Za-z0-9._-]{1,128}$'),
  intent_tag          uuid NULL,
  intent_id           uuid NULL REFERENCES business.notification_intent (id),
  -- matched: applied to (or recorded for) intent_id; unmatched: no intent
  -- known (pending correlation); conflict: the tag and the message ID point
  -- at different intents or messages, so nothing was applied.
  correlation         text NOT NULL CHECK (correlation IN ('matched', 'unmatched', 'conflict')),
  applied_state       text NULL CHECK (applied_state IN ('accepted', 'delivered_to_server', 'bounced', 'failed')),
  detail              json NULL CHECK (detail IS NULL OR octet_length(detail::text) <= 1024),
  payload_hash        char(64) NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
  received_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT email_event_correlated CHECK ((correlation = 'matched') = (intent_id IS NOT NULL)),
  CONSTRAINT email_event_applied CHECK (applied_state IS NULL OR correlation = 'matched')
);

CREATE INDEX email_event_intent_idx ON business.email_event (intent_id, received_at) WHERE intent_id IS NOT NULL;

-- Owner-internal: evidence rank of an intent state.
CREATE FUNCTION business.notification_evidence_rank(p_state text)
RETURNS integer
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE p_state
    WHEN 'queued' THEN 0
    WHEN 'sending' THEN 1
    WHEN 'unknown' THEN 1
    WHEN 'accepted' THEN 2
    WHEN 'delivered_to_server' THEN 3
    WHEN 'failed' THEN 4
    WHEN 'bounced' THEN 4
  END;
$$;

-- API: store one verified provider event and apply it. Result 'recorded' or
-- 'duplicate' (same svix-id seen before; nothing changes), with the
-- correlated intent and its state afterwards.
CREATE FUNCTION business.email_event_record(
  p_svix_id text, p_event_type text, p_provider_created_at timestamptz, p_provider_message_id text,
  p_intent_tag uuid, p_detail json, p_payload_hash text)
RETURNS TABLE (result text, event_intent_id uuid, event_intent_state text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent;
  v_by_tag business.notification_intent;
  v_correlation text := 'unmatched';
  v_target text;
  v_applied text;
  v_seen_intent uuid;
BEGIN
  -- Serialize deliveries of one svix-id so a concurrent retry is a duplicate.
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('gk.email_event:' || p_svix_id, 0));
  SELECT e.intent_id INTO v_seen_intent FROM business.email_event e WHERE e.svix_id = p_svix_id;
  IF FOUND THEN
    RETURN QUERY SELECT 'duplicate'::text, v_seen_intent,
      (SELECT i.state FROM business.notification_intent i WHERE i.id = v_seen_intent);
    RETURN;
  END IF;

  IF p_provider_message_id IS NOT NULL THEN
    SELECT * INTO v FROM business.notification_intent i WHERE i.provider_message_id = p_provider_message_id FOR UPDATE;
  END IF;
  IF p_intent_tag IS NOT NULL THEN
    SELECT * INTO v_by_tag FROM business.notification_intent i WHERE i.id = p_intent_tag FOR UPDATE;
  END IF;
  IF v.id IS NOT NULL AND p_intent_tag IS NOT NULL AND v.id <> p_intent_tag THEN
    v_correlation := 'conflict';
    v := NULL;
  ELSIF v.id IS NULL AND v_by_tag.id IS NOT NULL THEN
    IF v_by_tag.provider_message_id IS NOT NULL AND v_by_tag.provider_message_id IS DISTINCT FROM p_provider_message_id THEN
      v_correlation := 'conflict';
    ELSE
      v := v_by_tag;
    END IF;
  END IF;

  IF v.id IS NOT NULL THEN
    v_correlation := 'matched';
    v_target := CASE p_event_type
      WHEN 'email.sent' THEN 'accepted'
      WHEN 'email.delivered' THEN 'delivered_to_server'
      WHEN 'email.complained' THEN 'delivered_to_server'
      WHEN 'email.bounced' THEN 'bounced'
      WHEN 'email.failed' THEN 'failed'
      WHEN 'email.suppressed' THEN 'failed'
    END;
    -- A queued intent was never dispatched; a state that needs a provider
    -- reference is applied only with one.
    -- The intent row is locked (FOR UPDATE above), so concurrent events for
    -- one email apply in turn; the rank guard repeats the check in the
    -- UPDATE itself.
    IF v_target IS NOT NULL AND v.state <> 'queued'
       AND business.notification_evidence_rank(v_target) > business.notification_evidence_rank(v.state)
       AND (v_target = 'failed' OR coalesce(v.provider_message_id, p_provider_message_id) IS NOT NULL) THEN
      UPDATE business.notification_intent i
      SET state = v_target, provider_message_id = coalesce(i.provider_message_id, p_provider_message_id),
          version = i.version + 1, updated_at = pg_catalog.now()
      WHERE i.id = v.id AND i.state <> 'queued'
        AND business.notification_evidence_rank(i.state) < business.notification_evidence_rank(v_target)
      RETURNING i.state INTO v.state;
      IF FOUND THEN
        v_applied := v_target;
        IF v_target IN ('bounced', 'failed') THEN
          -- Adverse evidence after dispatch: the recipient did not get it.
          PERFORM business.audit_record('system', NULL, 'notification.adverse_evidence', 'notification_intent', v.id,
                                        v.transition_id, pg_catalog.json_build_object('state', v_target, 'event_type', p_event_type));
        END IF;
      ELSE
        SELECT i.state INTO v.state FROM business.notification_intent i WHERE i.id = v.id;
      END IF;
    ELSIF v.provider_message_id IS NULL AND p_provider_message_id IS NOT NULL AND v.state <> 'queued' THEN
      -- Evidence of the provider message without a state change.
      UPDATE business.notification_intent i
      SET provider_message_id = p_provider_message_id, version = i.version + 1, updated_at = pg_catalog.now()
      WHERE i.id = v.id;
    END IF;
  END IF;

  INSERT INTO business.email_event
    (id, svix_id, event_type, provider_created_at, provider_message_id, intent_tag, intent_id, correlation,
     applied_state, detail, payload_hash)
  VALUES (pg_catalog.gen_random_uuid(), p_svix_id, p_event_type, p_provider_created_at, p_provider_message_id, p_intent_tag, v.id,
          v_correlation, v_applied, p_detail, p_payload_hash);
  RETURN QUERY SELECT 'recorded'::text, v.id, v.state;
END;
$$;

-- Enqueue now also requires the env tag (equal to the payload environment)
-- that the webhook filters on; the rest is unchanged from 0004.
CREATE OR REPLACE FUNCTION business.notification_enqueue(
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
     OR coalesce(p_payload ->> 'environment', '') !~ '^[a-z0-9][a-z0-9-]{0,31}$'
     OR pg_catalog.jsonb_typeof(v_request) IS DISTINCT FROM 'object'
     OR NOT v_request @> pg_catalog.jsonb_build_object('tags', pg_catalog.jsonb_build_array(
                            pg_catalog.jsonb_build_object('name', 'intent', 'value', p_intent_id::text),
                            pg_catalog.jsonb_build_object('name', 'env', 'value', p_payload ->> 'environment'))) THEN
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

REVOKE ALL ON TABLE business.email_event FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_evidence_rank(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.email_event_record(text, text, timestamptz, text, uuid, json, text) FROM PUBLIC;

-- The API records verified events only through this function.
GRANT EXECUTE ON FUNCTION business.email_event_record(text, text, timestamptz, text, uuid, json, text) TO gk_api;
