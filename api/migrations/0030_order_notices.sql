-- GK-024/025/026: transactional order emails through the K08 engine
-- (contracts r6.5 K08 "Transaction inserts notice intent+job+audit
-- atomically, unique transition/recipient/template revision", "Financial
-- notices distinguish requested/pending from successful return", "No
-- marketing"; K11 "append outcome/version acknowledgment independently
-- before exposing a confirmed response or confirmation email"; K09 minimal
-- personal data).
--
-- 1. Staging recipient allowlist (EMAIL_RECIPIENT_ALLOWLIST, worker).
--    business.notification_dispatch_suppress (worker, lease-fenced, freeze
--    first): the dispatcher found the stored recipient outside this
--    environment's allowlist, so the intent is suspended with a fixed reason
--    (the existing suspended_reason; no new state) and audited as
--    'notification.suppressed'. Answers 'suppressed' (never dispatched:
--    the job completes), 'suppressed_after_dispatch' (an earlier attempt
--    may have been sent: manager attention), 'settled' (nothing to stop),
--    'already' (suspended before) or 'frozen' (restore freeze: nothing
--    changes). The provider is never called for a suppressed intent
--    (notification_dispatch_admit answers 'suspended').
--
-- Expand-only (K09): new tables, functions and triggers; replaced
-- functions keep their signatures and grants.

SET LOCAL ROLE gk_owner;

CREATE FUNCTION business.notification_dispatch_suppress(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_intent(p_job_id, p_worker, p_generation);
  v_frozen boolean;
  v_result text;
BEGIN
  IF v.id IS NULL THEN
    RAISE EXCEPTION 'job % is not a notification job', p_job_id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Freeze first (0020): the leased rows are locked, then the freeze row.
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN 'frozen';
  ELSIF v.suspended_reason IS NOT NULL THEN
    RETURN 'already';
  ELSIF v.state NOT IN ('queued', 'sending', 'unknown') THEN
    RETURN 'settled';
  END IF;
  v_result := CASE WHEN v.state = 'queued'
                        AND NOT EXISTS (SELECT 1 FROM business.notification_attempt a WHERE a.intent_id = v.id)
                   THEN 'suppressed' ELSE 'suppressed_after_dispatch' END;
  UPDATE business.notification_intent
  SET suspended_reason = 'recipient not on this environment''s email allowlist (EMAIL_RECIPIENT_ALLOWLIST): not sent',
      version = version + 1, updated_at = pg_catalog.now()
  WHERE id = v.id;
  PERFORM business.audit_record('system', NULL, 'notification.suppressed', 'notification_intent', v.id, v.transition_id,
                                pg_catalog.json_build_object('intent_state', v.state, 'result', v_result));
  RETURN v_result;
END;
$$;

REVOKE ALL ON FUNCTION business.notification_dispatch_suppress(uuid, uuid, bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION business.notification_dispatch_suppress(uuid, uuid, bigint) TO gk_worker;
