-- K11 follow-up: the restore freeze is checked before any dispatch outcome.
--
-- payment_dispatch_fix, payment_dispatch_admit, refund_dispatch_fix and
-- refund_dispatch_admit (latest in 0015 and 0016) answered 'ready' (already
-- fixed), 'closed' or 'settled' before they looked at the restore freeze.
-- A freeze set on a live database while a worker was between its begin
-- (which checks the freeze) and fix/admit could therefore still let the job
-- finish: the worker records a never-dispatched or settled outcome for a
-- 'closed'/'settled' admission, and completes on a 'closed' fix.
--
-- Each now reads business.recovery_freeze with a share lock right after its
-- row locks and before any outcome branch, as the other K11 gateways do
-- (a concurrent recovery_set_freeze waits for this transaction, or its
-- freeze is seen here); while frozen it answers 'frozen', which the worker
-- maps to a deferral (FreezeRecheck) without recording anything. After the
-- freeze lifts the same answers as before follow.
--
-- Lock order: the leased rows as before (the collection, then the refund
-- operation), then the freeze row in share mode. The freeze row is written
-- only by recovery_set_freeze, which locks nothing else, so no cycle is
-- possible.
--
-- Expand-only (K09): no new objects; the replaced functions keep their
-- signatures and grants.

SET LOCAL ROLE gk_owner;

-- As in 0015, with the freeze checked first.
CREATE OR REPLACE FUNCTION business.payment_dispatch_fix(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_payload text, p_at timestamptz)
RETURNS TABLE (status text, payload text, replay_until timestamptz, session_expires_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  c business.payment_collection%ROWTYPE;
  v_frozen boolean;
  v_expires timestamptz;
  v_p json;
  v_request text;
BEGIN
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN QUERY SELECT 'frozen'::text, NULL::text, NULL::timestamptz, NULL::timestamptz;
    RETURN;
  END IF;
  IF c.first_dispatch_at IS NOT NULL THEN
    RETURN QUERY SELECT 'ready'::text, c.payload, c.replay_until, c.session_expires_at;
    RETURN;
  END IF;
  IF c.cancel_reason = 'never_dispatched' THEN
    RETURN QUERY SELECT 'closed'::text, NULL::text, NULL::timestamptz, NULL::timestamptz;
    RETURN;
  END IF;
  IF p_at IS NULL OR p_at > pg_catalog.now() OR p_at <= pg_catalog.now() - interval '1 minute' THEN
    RAISE EXCEPTION 'dispatch instant must be the database clock of the last minute' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  v_expires := pg_catalog.date_trunc('second', p_at) + interval '2 hours';
  BEGIN
    v_p := p_payload::json;
  EXCEPTION WHEN others THEN
    v_p := NULL;
  END;
  v_request := v_p ->> 'request';
  IF v_p IS NULL OR pg_catalog.json_typeof(v_p) <> 'object' OR v_request IS NULL OR octet_length(p_payload) > 16384
     OR (v_p ->> 'v') IS DISTINCT FROM '1' OR (v_p ->> 'provider') IS DISTINCT FROM 'stripe'
     OR (v_p ->> 'environment') IS DISTINCT FROM c.environment
     OR (v_p ->> 'account') IS DISTINCT FROM c.provider_account_id
     OR (v_p ->> 'livemode') IS DISTINCT FROM c.livemode::text
     OR (v_p ->> 'idempotency_key') IS DISTINCT FROM c.id::text
     OR (v_p ->> 'order_id') IS DISTINCT FROM c.order_id::text
     OR (v_p ->> 'amount_minor') IS DISTINCT FROM c.amount_minor::text
     OR NOT business.payment_form_has(v_request, 'client_reference_id', c.order_id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Bcollection_id%5D', c.id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Border_id%5D', c.order_id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Benvironment%5D', c.environment)
     OR NOT business.payment_form_has(v_request, 'line_items%5B0%5D%5Bprice_data%5D%5Bunit_amount%5D', c.amount_minor::text)
     OR NOT business.payment_form_has(v_request, 'line_items%5B0%5D%5Bprice_data%5D%5Bcurrency%5D', 'gbp')
     OR NOT business.payment_form_has(v_request, 'expires_at', pg_catalog.date_part('epoch', v_expires)::bigint::text) THEN
    RAISE EXCEPTION 'payment payload does not belong to collection %', c.id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.payment_collection pc
  SET payload = p_payload,
      payload_sha256 = pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(p_payload, 'UTF8')), 'hex'),
      session_expires_at = v_expires, first_dispatch_at = p_at, replay_until = p_at + interval '23 hours',
      version = pc.version + 1, updated_at = pg_catalog.now()
  WHERE pc.id = c.id
  RETURNING pc.payload, pc.replay_until, pc.session_expires_at INTO c.payload, c.replay_until, c.session_expires_at;
  RETURN QUERY SELECT 'ready'::text, c.payload, c.replay_until, c.session_expires_at;
END;
$$;

-- As in 0015, with the freeze checked first.
CREATE OR REPLACE FUNCTION business.payment_dispatch_admit(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  c business.payment_collection%ROWTYPE;
  v_frozen boolean;
BEGIN
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN 'frozen';
  ELSIF c.cancel_reason = 'never_dispatched' THEN
    RETURN 'closed';
  ELSIF c.replay_until IS NULL OR pg_catalog.now() >= c.replay_until THEN
    RETURN 'expired';
  ELSIF c.dispatch_started_at IS NULL AND c.session_expires_at - pg_catalog.now() < interval '31 minutes' THEN
    RETURN 'too_late';
  END IF;
  IF c.dispatch_started_at IS NULL THEN
    UPDATE business.payment_collection pc
    SET dispatch_started_at = pg_catalog.now(), version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = c.id;
  END IF;
  RETURN 'ok';
END;
$$;

-- As in 0016, with the freeze checked first.
CREATE OR REPLACE FUNCTION business.refund_dispatch_fix(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_payload text, p_at timestamptz)
RETURNS TABLE (status text, payload text, replay_until timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  o business.refund_operation%ROWTYPE;
  v_frozen boolean;
  v_pi text;
  v_p json;
  v_request text;
BEGIN
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id FOR UPDATE;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN QUERY SELECT 'frozen'::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF o.first_dispatch_at IS NOT NULL THEN
    RETURN QUERY SELECT 'ready'::text, o.payload, o.replay_until;
    RETURN;
  END IF;
  IF o.state IN ('cancelled', 'failed', 'succeeded') THEN
    RETURN QUERY SELECT 'closed'::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF business.refund_quarantined(o.collection_id) THEN
    RETURN QUERY SELECT 'blocked'::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  v_pi := coalesce(o.payment_intent_id, (SELECT pc.payment_intent_id FROM business.payment_collection pc
                                         WHERE pc.id = o.collection_id));
  IF v_pi IS NULL THEN
    RETURN QUERY SELECT 'no_payment_intent'::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF p_at IS NULL OR p_at > pg_catalog.now() OR p_at <= pg_catalog.now() - interval '1 minute' THEN
    RAISE EXCEPTION 'dispatch instant must be the database clock of the last minute' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  BEGIN
    v_p := p_payload::json;
  EXCEPTION WHEN others THEN
    v_p := NULL;
  END;
  IF v_p IS NOT NULL AND pg_catalog.json_typeof(v_p) = 'object' THEN
    v_request := v_p ->> 'request';
  END IF;
  IF v_request IS NULL OR octet_length(p_payload) > 16384
     OR (v_p ->> 'v') IS DISTINCT FROM '1' OR (v_p ->> 'provider') IS DISTINCT FROM 'stripe'
     OR (v_p ->> 'kind') IS DISTINCT FROM 'refund'
     OR (v_p ->> 'environment') IS DISTINCT FROM o.environment
     OR (v_p ->> 'account') IS DISTINCT FROM o.provider_account_id
     OR (v_p ->> 'livemode') IS DISTINCT FROM o.livemode::text
     OR (v_p ->> 'idempotency_key') IS DISTINCT FROM o.id::text
     OR (v_p ->> 'refund_operation_id') IS DISTINCT FROM o.id::text
     OR (v_p ->> 'order_id') IS DISTINCT FROM o.order_id::text
     OR (v_p ->> 'collection_id') IS DISTINCT FROM o.collection_id::text
     OR (v_p ->> 'payment_intent_id') IS DISTINCT FROM v_pi
     OR (v_p ->> 'amount_minor') IS DISTINCT FROM o.amount_minor::text
     OR NOT business.payment_form_has(v_request, 'payment_intent', v_pi)
     OR NOT business.payment_form_has(v_request, 'amount', o.amount_minor::text)
     OR NOT business.payment_form_has(v_request, 'reason', 'requested_by_customer')
     OR NOT business.payment_form_has(v_request, 'metadata%5Border_id%5D', o.order_id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Bcollection_id%5D', o.collection_id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Brefund_operation_id%5D', o.id::text)
     OR NOT business.payment_form_has(v_request, 'metadata%5Benvironment%5D', o.environment) THEN
    RAISE EXCEPTION 'refund payload does not belong to refund operation %', o.id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.refund_operation ro
  SET payload = p_payload,
      payload_sha256 = pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(p_payload, 'UTF8')), 'hex'),
      payment_intent_id = v_pi, first_dispatch_at = p_at, replay_until = p_at + interval '23 hours',
      version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = o.id
  RETURNING ro.payload, ro.replay_until INTO o.payload, o.replay_until;
  RETURN QUERY SELECT 'ready'::text, o.payload, o.replay_until;
END;
$$;

-- As in 0016, with the freeze checked first.
CREATE OR REPLACE FUNCTION business.refund_dispatch_admit(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  o business.refund_operation%ROWTYPE;
  v_frozen boolean;
BEGIN
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id FOR UPDATE;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN 'frozen';
  ELSIF o.state = 'cancelled' THEN
    RETURN 'closed';
  ELSIF o.state IN ('succeeded', 'failed') THEN
    RETURN 'settled';
  ELSIF business.refund_quarantined(o.collection_id) OR o.mismatch_reason IS NOT NULL THEN
    RETURN 'blocked';
  ELSIF o.replay_until IS NULL OR pg_catalog.now() >= o.replay_until THEN
    RETURN 'expired';
  END IF;
  UPDATE business.refund_operation ro
  SET dispatch_started_at = coalesce(ro.dispatch_started_at, pg_catalog.now()),
      state = CASE WHEN ro.state = 'reserved' THEN 'submitting' ELSE ro.state END,
      version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = o.id;
  RETURN 'ok';
END;
$$;
