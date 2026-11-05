-- GK-025 follow-ups (review nits of A3a, A3b and A3c1; contracts r6.5 K03
-- "unknown remains reserved", K04, K05).
--
-- Expire admission. payment_expire_admit also skips once Stripe has expired
-- the session by itself (now() >= provider_expires_at): there is nothing to
-- ask; the following retrieve-and-verify reconcile decides as before.
--
-- Audit. A dispatch Stripe rejected audits payment.failed on the order with
-- the collection in the data, as every other payment audit does (earlier
-- events keep subject type payment_collection).
--
-- Mismatch on a cancelled collection. A stale mismatch verdict (a reconcile
-- that read Stripe before the collection was decided) used to move a
-- cancelled collection to unknown. It now keeps 'cancelled' and its
-- evidence and records only mismatch_reason, mismatch_evidence and the
-- observation time (audit payment.mismatch; the worker puts the job in
-- manager attention). The money is never hidden: a collection under
-- reconciliation (mismatch_reason set, not succeeded) keeps the order's
-- holds in the hold review exactly as unknown does, refuses a second
-- collection (GK409 payment_in_progress) and reads 'unknown' in order_get
-- ("still checking payment; do not pay again"). An order already released
-- before the mismatch stays cancelled; the manager reconciles it.
--
-- Expand-only (K09): the replaced functions keep their signatures and
-- grants.

SET LOCAL ROLE gk_owner;

-- As in 0015, plus: skip once Stripe has expired the session by itself
-- (now() >= provider_expires_at; the retrieve decides).
CREATE OR REPLACE FUNCTION business.payment_expire_admit(p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_reconcile(p_job_id, p_worker, p_generation, p_collection);
  c business.payment_collection%ROWTYPE;
BEGIN
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  IF business.recovery_frozen() THEN
    RETURN 'frozen';
  END IF;
  IF c.state <> 'pending' OR c.mismatch_reason IS NOT NULL OR c.provider_session_id IS NULL
     OR coalesce(c.session_status, 'open') <> 'open' OR c.dispatch_started_at IS NULL
     OR pg_catalog.now() < c.dispatch_started_at + interval '15 minutes'
     OR (c.provider_expires_at IS NOT NULL AND pg_catalog.now() >= c.provider_expires_at)
     OR (c.expire_requested_at IS NOT NULL AND c.expire_requested_at > pg_catalog.now() - interval '30 seconds')
     OR EXISTS (SELECT 1 FROM business.payment_attempt a
                WHERE a.collection_id = c.id AND a.kind = 'expire' AND a.result IN ('expired', 'not_open', 'rejected')) THEN
    RETURN 'skip';
  END IF;
  UPDATE business.payment_collection pc
  SET expire_requested_at = pg_catalog.now(), version = pc.version + 1, updated_at = pg_catalog.now()
  WHERE pc.id = c.id;
  RETURN 'ok';
END;
$$;

-- As in 0015; payment.failed is audited on the order (collection in data).
CREATE OR REPLACE FUNCTION business.payment_recovery_committed(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_outcome json, p_outcome_hash text)
RETURNS TABLE (op_state text, changed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  v_status text := p_outcome ->> 'status';
  v_ev json := p_outcome -> 'evidence';
  v_livemode boolean;
  v_state text;
  v_changed boolean;
  v_cancel_reason text;
  v_order uuid;
BEGIN
  IF v_status IS NULL OR v_status NOT IN ('created', 'rejected') THEN
    RAISE EXCEPTION 'unsupported payment dispatch outcome %', v_status USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT pc.cancel_reason, pc.order_id INTO v_cancel_reason, v_order FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  SELECT c.op_state, c.changed INTO v_state, v_changed
  FROM business.recovery_mark_committed(p_operation_id, p_outcome, p_outcome_hash, false) c;
  IF v_status = 'created' THEN
    v_livemode := (v_ev ->> 'livemode')::boolean;
    UPDATE business.payment_collection pc
    SET state = CASE WHEN v_livemode IS DISTINCT FROM pc.livemode THEN 'unknown' ELSE 'pending' END,
        mismatch_reason = CASE WHEN v_livemode IS DISTINCT FROM pc.livemode
                               THEN 'session created in the wrong Stripe mode' END,
        provider_session_id = v_ev ->> 'session_id',
        checkout_url = v_ev ->> 'url',
        provider_expires_at = pg_catalog.to_timestamp((v_ev ->> 'expires_at')::bigint),
        provider_livemode = v_livemode,
        payment_intent_id = coalesce(pc.payment_intent_id, v_ev ->> 'payment_intent_id'),
        session_status = coalesce(v_ev ->> 'session_status', pc.session_status),
        version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = v.collection_id AND pc.state IN ('created', 'unknown') AND pc.mismatch_reason IS NULL;
    IF v_changed THEN
      PERFORM business.payment_schedule_reconcile(v.collection_id, pg_catalog.now() + interval '15 minutes');
    END IF;
  ELSE
    UPDATE business.payment_collection pc
    SET state = 'failed', version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = v.collection_id AND pc.state IN ('created', 'unknown') AND pc.mismatch_reason IS NULL;
    IF v_changed AND v_cancel_reason IS DISTINCT FROM 'never_dispatched' THEN
      PERFORM business.audit_record('system', NULL, 'payment.failed', 'order', v_order, NULL,
                                    pg_catalog.json_build_object('collection_id', v.collection_id,
                                                                 'reason', coalesce(v_ev ->> 'reason', 'rejected_by_stripe')));
    END IF;
  END IF;
  RETURN QUERY SELECT v_state, v_changed;
END;
$$;

-- As in 0014, plus: a mismatch on a cancelled collection keeps it
-- cancelled (see the header).
CREATE OR REPLACE FUNCTION business.payment_reconcile_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid, p_verdict text, p_session_id text,
  p_session_status text, p_payment_status text, p_payment_intent_id text, p_evidence json, p_success_at timestamptz,
  p_reason text)
RETURNS TABLE (collection_state text, command_id uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased(p_job_id, p_worker, p_generation);
  v_order uuid;
  v_o business.orders%ROWTYPE;
  c business.payment_collection%ROWTYPE;
  v_frozen boolean;
  v_cmd uuid;
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
BEGIN
  IF v.job_id IS NULL OR v.purpose <> 'reconcile' OR v.collection_id IS DISTINCT FROM p_collection THEN
    RAISE EXCEPTION 'collection % is not the reconcile of job %', p_collection, p_job_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF p_verdict IS NULL OR p_verdict NOT IN ('paid', 'pending', 'cancelled', 'mismatch')
     OR (p_verdict = 'paid') <> (p_success_at IS NOT NULL) OR (p_verdict = 'mismatch') <> (p_reason IS NOT NULL) THEN
    RAISE EXCEPTION 'invalid reconcile verdict %', p_verdict USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Lock order (K00): the order, then the collection.
  SELECT pc.order_id INTO v_order FROM business.payment_collection pc WHERE pc.id = p_collection;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v_order FOR UPDATE;
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = p_collection FOR UPDATE;
  IF c.provider_session_id IS NOT NULL AND c.provider_session_id IS DISTINCT FROM p_session_id THEN
    RAISE EXCEPTION 'session % is not the session of collection %', p_session_id, c.id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT oc.id INTO v_cmd FROM business.order_command oc WHERE oc.collection_id = c.id;

  IF p_verdict = 'mismatch' THEN
    -- A succeeded or cancelled collection keeps its state and evidence;
    -- the contradicting observation is kept beside it (a cancelled one
    -- under reconciliation still keeps the holds, refuses a new collection
    -- and reads unknown: see the header).
    UPDATE business.payment_collection pc
    SET state = CASE WHEN pc.state IN ('succeeded', 'cancelled') THEN pc.state ELSE 'unknown' END,
        evidence = CASE WHEN pc.state IN ('succeeded', 'cancelled') THEN pc.evidence ELSE p_evidence END,
        mismatch_reason = p_reason, mismatch_evidence = p_evidence, last_observed_at = pg_catalog.now(),
        version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = c.id;
    PERFORM business.audit_record('system', NULL, 'payment.mismatch', 'order', c.order_id, NULL,
                                  pg_catalog.json_build_object('collection_id', c.id, 'reason', p_reason));
  ELSIF c.state IN ('succeeded', 'failed') OR c.mismatch_reason IS NOT NULL
        OR (c.state = 'cancelled' AND p_verdict <> 'paid') THEN
    -- Decided already (or under manager reconciliation; a stale observation
    -- never reopens a cancelled collection): only the observation time
    -- moves. Real funds after a cancellation are still recorded as paid.
    UPDATE business.payment_collection pc SET last_observed_at = pg_catalog.now() WHERE pc.id = c.id;
  ELSIF p_verdict = 'pending' THEN
    UPDATE business.payment_collection pc
    SET state = 'pending', provider_session_id = p_session_id, session_status = p_session_status,
        payment_status = p_payment_status, payment_intent_id = coalesce(p_payment_intent_id, pc.payment_intent_id),
        evidence = p_evidence, last_observed_at = pg_catalog.now(), version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = c.id;
  ELSIF p_verdict = 'cancelled' THEN
    UPDATE business.payment_collection pc
    SET state = 'cancelled', provider_session_id = p_session_id, session_status = p_session_status,
        payment_status = p_payment_status, payment_intent_id = coalesce(p_payment_intent_id, pc.payment_intent_id),
        evidence = p_evidence, last_observed_at = pg_catalog.now(), version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = c.id;
    PERFORM business.audit_record('system', NULL, 'payment.cancelled', 'order', c.order_id, NULL,
                                  pg_catalog.json_build_object('collection_id', c.id));
  ELSE
    -- Paid. A restore freeze stops it under its own lock.
    SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
    IF v_frozen THEN
      RAISE EXCEPTION 'frozen' USING ERRCODE = 'GK002';
    END IF;
    UPDATE business.payment_collection pc
    SET state = 'succeeded', succeeded_at = p_success_at, observed_at = pg_catalog.now(),
        provider_session_id = p_session_id, session_status = p_session_status, payment_status = p_payment_status,
        payment_intent_id = coalesce(p_payment_intent_id, pc.payment_intent_id), evidence = p_evidence,
        last_observed_at = pg_catalog.now(), version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = c.id;
    PERFORM business.audit_record('system', NULL, 'payment.succeeded', 'order', c.order_id, NULL,
                                  pg_catalog.json_build_object('collection_id', c.id, 'amount_minor', c.amount_minor));
    IF v_cmd IS NULL THEN
      v_cmd := pg_catalog.gen_random_uuid();
      v_key := 'recovery/' || c.environment || '/op/' || v_o.id::text || '/'
               || pg_catalog.lpad(v_o.version::text, 10, '0') || '-' || v_cmd::text;
      v_payload := pg_catalog.json_build_object(
        'v', 1, 'environment', c.environment, 'command', 'order.paid_confirm', 'operation_id', v_cmd,
        'order_id', v_o.id, 'prior_version', v_o.version,
        'received_at', pg_catalog.to_char(p_success_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
        'actor', pg_catalog.json_build_object('kind', 'system'),
        'collection', pg_catalog.json_build_object(
          'id', c.id, 'amount_minor', c.amount_minor, 'currency', c.currency, 'account', c.provider_account_id,
          'livemode', c.livemode, 'session_id', p_session_id, 'payment_intent_id', p_payment_intent_id,
          'succeeded_at', pg_catalog.to_char(p_success_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
          'evidence', p_evidence),
        'order', business.order_recovery_snapshot(v_o.id))::text;
      v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_payload, 'UTF8')), 'hex');
      SELECT r.result INTO v_reserve
      FROM business.recovery_reserve(v_cmd, 'command', v_o.id, v_o.version, v_hash, v_key, NULL) r;
      IF v_reserve = 'busy' THEN
        -- Another protected operation on the order is unresolved: nothing
        -- is recorded; the reconcile retries (counted, then attention).
        RAISE EXCEPTION 'order_busy' USING ERRCODE = 'GK409';
      ELSIF v_reserve <> 'reserved' THEN
        -- The order already has an operation at this or a later version:
        -- this can never succeed; manager attention, nothing recorded.
        RAISE EXCEPTION 'order_stale' USING ERRCODE = 'GK409';
      END IF;
      v_job := business.job_enqueue(v_cmd, NULL);
      INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, guest_capability_id,
                                          idempotency_key, body_sha256, prior_version, received_at, payload,
                                          payload_sha256, envelope_key, job_id, collection_id)
      VALUES (v_cmd, v_o.id, 'paid_confirm', 'system', NULL, NULL, 'payment:' || c.id::text, v_hash, v_o.version,
              p_success_at, v_payload, v_hash, v_key, v_job, c.id);
      PERFORM business.audit_record('system', NULL, 'order.paid_confirmation_requested', 'order', v_o.id, NULL,
                                    pg_catalog.json_build_object('operation_id', v_cmd, 'collection_id', c.id));
    END IF;
  END IF;
  RETURN QUERY SELECT pc.state, v_cmd FROM business.payment_collection pc WHERE pc.id = c.id;
END;
$$;

-- As in 0015, plus: a collection under reconciliation keeps the holds.
CREATE OR REPLACE FUNCTION business.order_hold_review_run(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS TABLE (status text, order_id uuid, next_at timestamptz, cleaned integer)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_order uuid := business.order_hold_review_leased(p_job_id, p_worker, p_generation);
  v_o business.orders%ROWTYPE;
  v_r business.order_hold_review%ROWTYPE;
  v_frozen boolean;
  v_cleaned integer := 0;
  v_id uuid;
  v_next timestamptz;
  v_reason text;
  v_collections integer;
BEGIN
  IF v_order IS NULL THEN
    RETURN QUERY SELECT 'not_review'::text, NULL::uuid, NULL::timestamptz, 0;
    RETURN;
  END IF;
  -- Lock order (K00): the order, its collections by ID, the review row.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v_order FOR UPDATE;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = v_order ORDER BY pc.id FOR UPDATE;
  SELECT * INTO v_r FROM business.order_hold_review r WHERE r.order_id = v_order FOR UPDATE;
  IF v_r.outcome IS NOT NULL THEN
    RETURN QUERY SELECT 'done'::text, v_order, NULL::timestamptz, 0;
    RETURN;
  END IF;
  IF v_o.lifecycle <> 'awaiting_payment' THEN
    UPDATE business.order_hold_review r SET outcome = 'closed', decided_at = pg_catalog.now(), updated_at = pg_catalog.now()
    WHERE r.order_id = v_order;
    RETURN QUERY SELECT 'done'::text, v_order, NULL::timestamptz, 0;
    RETURN;
  END IF;
  IF EXISTS (SELECT 1 FROM business.payment_collection pc WHERE pc.order_id = v_order AND pc.state = 'succeeded') THEN
    -- Real money: a confirmation or a full return decides, never a release.
    UPDATE business.order_hold_review r SET outcome = 'paid', decided_at = pg_catalog.now(), updated_at = pg_catalog.now()
    WHERE r.order_id = v_order;
    RETURN QUERY SELECT 'done'::text, v_order, NULL::timestamptz, 0;
    RETURN;
  END IF;
  -- K11: a restore freeze stops every order change (share lock: a
  -- concurrent freeze waits for this transaction, or it is seen here).
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN QUERY SELECT 'frozen'::text, v_order, NULL::timestamptz, 0;
    RETURN;
  END IF;
  IF pg_catalog.now() < v_r.hold_until THEN
    PERFORM business.order_hold_review_schedule(v_order, v_r.hold_until);
    RETURN QUERY SELECT 'waiting'::text, v_order, v_r.hold_until, 0;
    RETURN;
  END IF;

  -- Never-dispatched cleanup: no Stripe call was ever admitted (admission
  -- records dispatch_started_at under the collection lock held here).
  FOR v_id IN SELECT pc.id FROM business.payment_collection pc
              WHERE pc.order_id = v_order AND pc.state = 'created' AND pc.dispatch_started_at IS NULL
                AND pg_catalog.now() >= GREATEST(pc.initiated_at, pc.created_at) + interval '15 minutes'
              ORDER BY pc.id LOOP
    UPDATE business.payment_collection pc
    SET state = 'cancelled', cancel_reason = 'never_dispatched', version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = v_id;
    PERFORM business.audit_record('system', NULL, 'payment.cancelled', 'order', v_order, NULL,
                                  pg_catalog.json_build_object('collection_id', v_id, 'reason', 'never_dispatched'));
    v_cleaned := v_cleaned + 1;
  END LOOP;

  -- Anything that may still succeed keeps the holds: an admitted creation,
  -- a pending or unknown collection (re-checked every 15 minutes; their own
  -- cancellation brings the review forward), a collection under manager
  -- reconciliation (a mismatch, whatever its state), a young never-admitted
  -- one (until its 15 minutes), an unresolved command on the order.
  SELECT min(CASE WHEN pc.state = 'created' AND pc.dispatch_started_at IS NULL
                  THEN GREATEST(pc.initiated_at, pc.created_at) + interval '15 minutes'
                  ELSE pg_catalog.now() + interval '15 minutes' END)
  INTO v_next
  FROM business.payment_collection pc
  WHERE pc.order_id = v_order AND (pc.state IN ('created', 'pending', 'unknown') OR pc.mismatch_reason IS NOT NULL);
  IF v_next IS NULL AND EXISTS (SELECT 1 FROM business.recovery_operation ro
                                WHERE ro.aggregate_id = v_order AND ro.state <> 'secured') THEN
    v_next := pg_catalog.now() + interval '15 minutes';
  END IF;
  IF v_next IS NOT NULL THEN
    PERFORM business.order_hold_review_schedule(v_order, v_next);
    RETURN QUERY SELECT 'waiting'::text, v_order, GREATEST(v_next, pg_catalog.now()), v_cleaned;
    RETURN;
  END IF;

  -- Proven terminal-unpaid (or never paid) and past the hold: release.
  v_reason := CASE WHEN v_o.payment_requirement = 'none' THEN 'free_not_confirmed' ELSE 'payment_not_completed' END;
  SELECT count(*) INTO v_collections FROM business.payment_collection pc WHERE pc.order_id = v_order;
  PERFORM business.order_holds_release(v_order);
  UPDATE business.orders o
  SET lifecycle = 'cancelled', cancel_reason = v_reason, cancelled_at = pg_catalog.now(), version = o.version + 1,
      updated_at = pg_catalog.now()
  WHERE o.id = v_order;
  UPDATE business.order_hold_review r SET outcome = 'released', decided_at = pg_catalog.now(), updated_at = pg_catalog.now()
  WHERE r.order_id = v_order;
  PERFORM business.audit_record('system', NULL, 'order.released_unpaid', 'order', v_order, NULL,
                                pg_catalog.json_build_object('reason', v_reason, 'collections', v_collections,
                                                             'order_version', v_o.version + 1));
  RETURN QUERY SELECT 'released'::text, v_order, NULL::timestamptz, v_cleaned;
END;
$$;

-- As in 0014, plus: no new collection beside one under reconciliation.
CREATE OR REPLACE FUNCTION business.payment_initiate(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_collection uuid, p_return_origin text, p_env text, p_account text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
  v_disabled boolean;
  v_o business.orders%ROWTYPE;
  v_prior business.payment_collection%ROWTYPE;
  v_capability uuid;
  v_quote_expires timestamptz;
  v_frozen boolean;
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
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' OR p_account IS NULL OR p_account !~ '^acct_[A-Za-z0-9]{1,64}$'
     OR p_collection IS NULL OR p_return_origin IS NULL OR p_return_origin !~ '^https?://[^/?#[:space:]]+$' THEN
    RAISE EXCEPTION 'payment environment, account, collection and return origin are required';
  END IF;
  IF p_owner IS NOT NULL THEN
    SELECT r.account_id, r.account_disabled INTO v_account, v_disabled FROM business.identity_resolve(p_owner) r;
    IF v_disabled THEN
      RAISE EXCEPTION 'account disabled' USING ERRCODE = 'GK403';
    END IF;
  END IF;

  -- The order, for its owner or a "pay" capability holder only; locked.
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
  SELECT * INTO v_prior FROM business.payment_collection c
  WHERE c.order_id = p_order AND c.request_key = p_key
    AND c.actor_account_id IS NOT DISTINCT FROM v_account AND c.guest_capability_id IS NOT DISTINCT FROM v_capability;
  IF FOUND THEN
    IF v_prior.request_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior.id, true;
    RETURN;
  END IF;
  -- K11: a restore freezes new collections (share lock: a concurrent freeze
  -- waits for this request, or this request sees it).
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  -- K04: no second collection while an earlier one may still succeed (or
  -- is under manager reconciliation).
  IF EXISTS (SELECT 1 FROM business.payment_collection c
             WHERE c.order_id = p_order AND (c.state IN ('created', 'pending', 'unknown')
                                             OR (c.state <> 'succeeded' AND c.mismatch_reason IS NOT NULL))) THEN
    RAISE EXCEPTION 'payment_in_progress' USING ERRCODE = 'GK409';
  END IF;
  IF EXISTS (SELECT 1 FROM business.payment_collection c WHERE c.order_id = p_order AND c.state = 'succeeded') THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;

  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.payment_requirement <> 'full' OR v_o.lifecycle <> 'awaiting_payment' OR v_o.recovery_state <> 'prepared'
     OR NOT business.order_holds_intact(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- The 24-hour window at the request instant: equality expires (K04).
  SELECT q.expires_at INTO v_quote_expires FROM business.checkout_quote q WHERE q.id = v_o.quote_id;
  IF p_now >= v_quote_expires THEN
    RAISE EXCEPTION 'quote_expired' USING ERRCODE = 'GK409';
  END IF;
  -- Initiation cannot use the late-payment path to bypass a missed lead
  -- time: 192 elapsed hours to the handover, equality allowed.
  IF v_o.original_handover_at - p_now < business.schedule_lead_time() THEN
    RAISE EXCEPTION 'lead_time' USING ERRCODE = 'GK409';
  END IF;

  -- Due at once by the database clock (p_now may be a test clock).
  v_job := business.job_enqueue(p_collection, NULL);
  INSERT INTO business.payment_collection
    (id, order_id, amount_minor, environment, provider_account_id, livemode, actor_kind, actor_account_id,
     guest_capability_id, request_key, request_sha256, return_origin, initiated_at)
  VALUES (p_collection, p_order, v_o.total_minor, p_env, p_account, p_env = 'production',
          CASE WHEN v_account IS NULL THEN 'guest' ELSE 'customer' END, v_account, v_capability, p_key, p_body_sha256,
          p_return_origin, p_now);
  INSERT INTO business.payment_job (job_id, collection_id, purpose) VALUES (v_job, p_collection, 'dispatch');
  PERFORM business.audit_record(CASE WHEN v_account IS NULL THEN 'system' ELSE 'customer' END, v_account,
                                'payment.collection_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('collection_id', p_collection, 'amount_minor', v_o.total_minor));
  RETURN QUERY SELECT p_collection, false;
END;
$$;

-- As in 0016, plus: a collection under reconciliation reads unknown.
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
           -- confirmation being secured"), is confirmation_pending.
           'lifecycle', CASE WHEN o.recovery_state = 'confirmation_pending'
                               OR (o.lifecycle = 'awaiting_payment'
                                   AND EXISTS (SELECT 1 FROM business.order_command pcmd
                                               JOIN business.recovery_operation pr ON pr.id = pcmd.id
                                               WHERE pcmd.order_id = o.id AND pcmd.command = 'paid_confirm'
                                                 AND pr.state <> 'secured'))
                             THEN 'confirmation_pending' ELSE o.lifecycle END,
           'cancel_reason', o.cancel_reason, 'cancelled_at', o.cancelled_at,
           'payment_requirement', o.payment_requirement,
           'placed_at', o.placed_at, 'date', o.handover_day, 'fulfillment_mode', o.fulfillment_mode,
           'slot', pg_catalog.json_build_object('id', o.slot_id, 'starts_at', o.original_handover_at, 'ends_at', o.handover_ends_at),
           'full_refund_deadline_at', o.full_refund_deadline_at, 'policy_version_id', o.policy_version_id,
           'customer_full_name', o.customer_full_name, 'different_payer_name', o.different_payer_name,
           'contact_email', o.contact_email, 'subtotal_minor', o.subtotal_minor, 'discount_minor', o.discount_minor,
           'total_minor', o.total_minor, 'currency', o.currency,
           'paid_at', CASE WHEN o.recovery_state = 'secured' THEN o.paid_at END,
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
           'payment', (SELECT pg_catalog.json_build_object(
                         'id', pc.id,
                         -- Under manager reconciliation reads unknown ("still
                         -- checking payment; do not pay again"), never cancelled.
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
           -- The full return (K05): requested until Stripe has it, pending
           -- while it is being processed or checked, returned only when
           -- every penny of the goal succeeded; never returned while
           -- unresolved.
           'refund', (SELECT pg_catalog.json_build_object(
                        'state', CASE WHEN rf.attention THEN 'needs_attention'
                                      WHEN rf.succeeded >= g.target_minor THEN 'returned'
                                      WHEN rf.succeeded > 0 THEN 'partly_returned'
                                      WHEN rf.sent THEN 'pending'
                                      ELSE 'requested' END,
                        'target_minor', g.target_minor, 'succeeded_minor', rf.succeeded,
                        'in_progress_minor', rf.reserved, 'currency', 'GBP', 'requested_at', g.created_at,
                        'goal_version', g.version)
                      FROM business.refund_goal g
                      CROSS JOIN LATERAL (
                        SELECT coalesce(sum(b.succeeded_minor), 0) AS succeeded,
                               coalesce(sum(b.reserved_minor), 0) AS reserved,
                               EXISTS (SELECT 1 FROM business.refund_operation ro
                                       WHERE ro.order_id = g.order_id AND ro.state IN ('submitting', 'pending', 'unknown'))
                                 AS sent,
                               -- requires action, a contradiction, a quarantined
                               -- observation, a dispatch under reconciliation, or a
                               -- failed return nothing replaced yet.
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
                      WHERE g.order_id = o.id),
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
