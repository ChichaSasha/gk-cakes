-- GK-024 A3b: hardening of business.email_event_record (review follow-up of
-- PR #10). Same signature, owner and grants; the only behaviour change is that
-- the message-only evidence update never replaces a provider message ID that
-- is already recorded (it relied on the row lock alone). Comments name the
-- intent row lock as the primary protection.

SET LOCAL ROLE gk_owner;

CREATE OR REPLACE FUNCTION business.email_event_record(
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
    -- The intent row is locked (FOR UPDATE above): that lock is the primary
    -- protection, so concurrent events for one email are correlated and
    -- applied in turn. The UPDATEs repeat their checks as a second layer.
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
      -- Evidence of the provider message without a state change; never
      -- replaces a recorded one (repeated in the UPDATE, like the rank).
      UPDATE business.notification_intent i
      SET provider_message_id = p_provider_message_id, version = i.version + 1, updated_at = pg_catalog.now()
      WHERE i.id = v.id AND i.provider_message_id IS NULL;
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

