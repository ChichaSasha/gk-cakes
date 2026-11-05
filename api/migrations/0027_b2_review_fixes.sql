-- GK-026 B2 review fixes (PR #40; contracts r6.5 K05 "Match identities
-- using complete provider pages and definitive nonexecution; audit
-- correction/unfreeze", "No raw bank details", Q5; K11 "unresolved facts go
-- to chef queue", "Independent completion evidence records outcomes").
--
-- 1. One proof of non-execution (business.refund_not_executed_proven) for
--    every manager "failed" on an unknown provider refund: the replay window
--    ended, a complete reconcile listing after it found nothing
--    (refund_operation.not_found_at, new: set only by the reconcile's
--    not_found verdict, which follows a complete list; last_observed_at is
--    also touched by other paths), nothing observed names, matches or links
--    the operation, it records no refund ID and its contradicting evidence
--    carries no refund object. Used by refund_operation_decide and by
--    refund_mismatch_clear ('failed' on an unknown refund: 409 window_open,
--    then evidence_required). 'failed' on a succeeded refund needs Stripe's
--    record of that same refund failed or canceled.
-- 2. A manager's succeeded decision matches an observed refund at once only
--    when Stripe's record proves the identity (its metadata names the
--    operation, or the operation already records that ID). Otherwise the ID
--    is recorded and the reconcile verifies it at Stripe; an unmatched
--    observation of that ID is matched there (refund_observation_record),
--    so its money is counted once.
-- 3. Every recovery decision completes the order's refund jobs in attention
--    that it settled (a dispatch whose operation is now decided without
--    contradiction; a reconcile whose collection has nothing unresolved,
--    quarantined or contradicted), and the attention list never shows such
--    jobs.
-- 4. A decision that settles an operation whose dispatch K11 record is
--    still prepared or in reconcile enqueues a refund_resolve job: the
--    worker records the manager outcome (status manager_failed or
--    manager_succeeded, evidence: the decision ID and state) in the
--    independent journal with recovery.Coordinator.Resolve, outside any
--    transaction, then secures the record. A record already committed with
--    another outcome is left alone (done).
-- 5. Evidence references refuse six or more digits once spaces, hyphens and
--    dots are removed, and anything IBAN-like (two letters, two digits, 11
--    to 30 letters or digits).
-- 6. Withdrawing a manual payout reservation needs an evidence reference.
--
-- Expand-only (K09): a new column, table and functions; functions replaced
-- with unchanged signatures.

SET LOCAL ROLE gk_owner;

ALTER TABLE business.refund_operation ADD COLUMN not_found_at timestamptz NULL;

CREATE OR REPLACE FUNCTION business.evidence_ref_valid(p_ref text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_ref IS NOT NULL AND p_ref ~ '^[\x21-\x7e]([\x20-\x7e]{0,198}[\x21-\x7e])?$'
         AND pg_catalog.regexp_replace(p_ref, '[ .-]', '', 'g') !~ '[0-9]{6,}'
         AND pg_catalog.regexp_replace(p_ref, '[ .-]', '', 'g') !~* '[a-z]{2}[0-9]{2}[a-z0-9]{11,30}';
$$;

-- Owner-internal: whether unknown provider refund p_op is proven never
-- executed (see the header, 1).
CREATE FUNCTION business.refund_not_executed_proven(p_op uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce((
    SELECT o.channel = 'provider' AND o.state = 'unknown' AND o.provider_refund_id IS NULL
           AND o.replay_until IS NOT NULL AND pg_catalog.now() >= o.replay_until
           AND o.not_found_at IS NOT NULL AND o.not_found_at >= o.replay_until
           AND (o.mismatch_evidence IS NULL OR (o.mismatch_evidence ->> 'refund_id') IS NULL)
           AND NOT EXISTS (SELECT 1 FROM business.refund_observation x
                           WHERE x.collection_id = o.collection_id
                             AND (x.metadata_operation_id = o.id OR x.matched_operation_id = o.id
                                  OR x.named_operation_id = o.id))
    FROM business.refund_operation o WHERE o.id = p_op), false);
$$;

-- Owner-internal: fail unknown refund p_op proven never executed (the
-- caller holds the locks and checked the proof).
CREATE FUNCTION business.refund_fail_not_executed(p_op uuid, p_account uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  o business.refund_operation%ROWTYPE;
BEGIN
  UPDATE business.refund_operation ro
  SET state = 'failed', failure_reason = 'not_executed_confirmed', settled_at = pg_catalog.now(),
      version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = p_op RETURNING * INTO o;
  PERFORM business.audit_record('staff', p_account, 'refund.failed', 'order', o.order_id, NULL,
                                pg_catalog.json_build_object('refund_operation_id', o.id, 'collection_id', o.collection_id,
                                                             'amount_minor', o.amount_minor));
END;
$$;

-- Owner-internal: whether refund job p_job no longer needs a manager: a
-- dispatch whose operation is decided without contradiction, or a reconcile
-- whose collection has nothing unresolved, quarantined or contradicted.
CREATE FUNCTION business.refund_job_settled(p_job uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce((
    SELECT CASE rj.purpose
             WHEN 'dispatch' THEN EXISTS (SELECT 1 FROM business.refund_operation ro
                                          WHERE ro.id = rj.refund_operation_id
                                            AND ro.state IN ('succeeded', 'failed', 'cancelled')
                                            AND ro.mismatch_reason IS NULL)
             ELSE NOT EXISTS (SELECT 1 FROM business.refund_operation ro
                              WHERE ro.collection_id = rj.collection_id
                                AND (ro.mismatch_reason IS NOT NULL
                                     OR ro.state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action')))
                  AND NOT business.refund_quarantined(rj.collection_id)
           END
    FROM business.refund_job rj WHERE rj.job_id = p_job), false);
$$;

-- Owner-internal: complete the order's refund jobs in attention that a
-- manager decision (p_decision) settled (see the header, 3).
CREATE FUNCTION business.refund_attention_settle(p_order uuid, p_decision uuid)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_jobs uuid[];
BEGIN
  WITH done AS (
    UPDATE business.job j
    SET state = 'completed', attention_reason = NULL, version = j.version + 1, updated_at = pg_catalog.now()
    FROM business.refund_job rj JOIN business.payment_collection pc ON pc.id = rj.collection_id
    WHERE rj.job_id = j.id AND pc.order_id = p_order AND j.state = 'attention' AND business.refund_job_settled(j.id)
    RETURNING j.id)
  SELECT pg_catalog.array_agg(done.id) INTO v_jobs FROM done;
  IF v_jobs IS NOT NULL THEN
    PERFORM business.audit_record('system', NULL, 'refund.attention_settled', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('decision_id', p_decision, 'jobs', v_jobs));
  END IF;
  RETURN coalesce(pg_catalog.cardinality(v_jobs), 0);
END;
$$;

-- Which manager decision's outcome each refund_resolve job journals for the
-- refund dispatch's K11 record (job operation = the decision).
CREATE TABLE business.refund_resolve_job (
  job_id              uuid PRIMARY KEY REFERENCES business.job (id),
  refund_operation_id uuid NOT NULL REFERENCES business.refund_operation (id),
  decision_id         uuid NOT NULL UNIQUE REFERENCES business.refund_recovery_decision (id),
  created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX refund_resolve_job_operation_idx ON business.refund_resolve_job (refund_operation_id);

-- Owner-internal: enqueue the journal resolution of p_op's dispatch record
-- after decision p_decision settled it (see the header, 4).
CREATE FUNCTION business.refund_resolve_enqueue(p_op uuid, p_decision uuid)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_job uuid;
BEGIN
  IF p_op IS NULL
     OR NOT EXISTS (SELECT 1 FROM business.refund_operation ro
                    WHERE ro.id = p_op AND ro.channel = 'provider' AND ro.state IN ('succeeded', 'failed')
                      AND ro.payload IS NOT NULL)
     OR NOT EXISTS (SELECT 1 FROM business.recovery_operation r WHERE r.id = p_op AND r.state IN ('prepared', 'reconcile'))
     OR EXISTS (SELECT 1 FROM business.refund_resolve_job rr JOIN business.job j ON j.id = rr.job_id
                WHERE rr.refund_operation_id = p_op AND j.state IN ('queued', 'retry', 'running')) THEN
    RETURN NULL;
  END IF;
  v_job := business.job_enqueue(p_decision, NULL);
  INSERT INTO business.refund_resolve_job (job_id, refund_operation_id, decision_id) VALUES (v_job, p_op, p_decision);
  RETURN v_job;
END;
$$;

-- Owner-internal: the refund_resolve job the caller holds a live lease on
-- (GK001 without the lease; NULL for another kind of job).
CREATE FUNCTION business.refund_resolve_leased(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS business.refund_resolve_job
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_operation uuid;
  v business.refund_resolve_job;
BEGIN
  SELECT j.operation_id INTO v_operation
  FROM business.job j
  WHERE j.id = p_job_id AND j.state = 'running' AND j.lease_owner = p_worker AND j.generation = p_generation
    AND j.lease_until > pg_catalog.now()
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'job % is not leased to this worker generation', p_job_id USING ERRCODE = 'GK001';
  END IF;
  SELECT * INTO v FROM business.refund_resolve_job rr WHERE rr.job_id = p_job_id AND rr.decision_id = v_operation;
  RETURN v;
END;
$$;

-- Owner-internal: the leased resolve job of refund operation p_op.
CREATE FUNCTION business.refund_resolve_leased_op(p_job_id uuid, p_worker uuid, p_generation bigint, p_op uuid)
RETURNS business.refund_resolve_job
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_resolve_job := business.refund_resolve_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.job_id IS NULL OR v.refund_operation_id IS DISTINCT FROM p_op THEN
    RAISE EXCEPTION 'refund operation % is not the resolution of job %', p_op, p_job_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v;
END;
$$;

-- Worker: start the leased resolve job. status: not_resolve (another kind
-- of job), frozen (a restore freeze: retry later), done (the record is
-- already committed or secured, or the operation is no longer decided:
-- nothing to journal) or ready, with the operation's identity, its exact
-- dispatch payload and replay deadline, and the outcome to record. A
-- record still prepared is moved to reconcile first (the manager decided
-- it).
CREATE FUNCTION business.refund_resolve_begin(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS TABLE (status text, operation_id uuid, payload text, replay_until timestamptz, decision_id uuid,
               operation_state text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_resolve_job := business.refund_resolve_leased(p_job_id, p_worker, p_generation);
  o business.refund_operation%ROWTYPE;
  r business.recovery_operation%ROWTYPE;
  v_frozen boolean;
  v_action text;
BEGIN
  IF v.job_id IS NULL THEN
    RETURN QUERY SELECT 'not_resolve'::text, NULL::uuid, NULL::text, NULL::timestamptz, NULL::uuid, NULL::text;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN QUERY SELECT 'frozen'::text, v.refund_operation_id, NULL::text, NULL::timestamptz, v.decision_id, NULL::text;
    RETURN;
  END IF;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = v.refund_operation_id;
  SELECT * INTO r FROM business.recovery_operation x WHERE x.id = o.id FOR UPDATE;
  IF r.id IS NULL OR r.state NOT IN ('prepared', 'reconcile') OR o.state NOT IN ('succeeded', 'failed') THEN
    RETURN QUERY SELECT 'done'::text, o.id, NULL::text, NULL::timestamptz, v.decision_id, o.state;
    RETURN;
  END IF;
  IF r.state = 'prepared' THEN
    SELECT d.action INTO v_action FROM business.refund_recovery_decision d WHERE d.id = v.decision_id;
    PERFORM business.recovery_mark_reconcile(o.id, 'manager decision ' || v_action);
  END IF;
  RETURN QUERY SELECT 'ready'::text, o.id, o.payload, o.replay_until, v.decision_id, o.state;
END;
$$;

-- Worker: the leased resolve job's recovery_lookup.
CREATE FUNCTION business.refund_resolve_lookup(p_job_id uuid, p_worker uuid, p_generation bigint, p_id uuid, p_kind text,
                                               p_aggregate_id uuid, p_prior_version integer, p_payload_hash text,
                                               p_envelope_key text, p_replay_until timestamptz)
RETURNS TABLE (result text, op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.refund_resolve_leased_op(p_job_id, p_worker, p_generation, p_id);
  RETURN QUERY SELECT * FROM business.recovery_lookup(p_id, p_kind, p_aggregate_id, p_prior_version, p_payload_hash,
                                                      p_envelope_key, p_replay_until);
END;
$$;

-- Worker: record the manager outcome from reconcile (committed).
CREATE FUNCTION business.refund_resolve_committed(p_job_id uuid, p_worker uuid, p_generation bigint, p_id uuid,
                                                  p_outcome json, p_outcome_hash text)
RETURNS TABLE (op_state text, changed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_status text := p_outcome ->> 'status';
BEGIN
  PERFORM business.refund_resolve_leased_op(p_job_id, p_worker, p_generation, p_id);
  IF v_status IS NULL OR v_status NOT IN ('manager_failed', 'manager_succeeded') THEN
    RAISE EXCEPTION 'unsupported refund resolution outcome %', v_status USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN QUERY SELECT * FROM business.recovery_mark_committed(p_id, p_outcome, p_outcome_hash, true);
END;
$$;

-- Worker: secure the record after its outcome envelope is acknowledged.
CREATE FUNCTION business.refund_resolve_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.refund_resolve_leased_op(p_job_id, p_worker, p_generation, p_id);
  RETURN business.recovery_mark_secured(p_id);
END;
$$;


-- As in 0016, plus matching an unmatched observation to the provider operation that now
-- records its refund ID (see the header, 2).
CREATE OR REPLACE FUNCTION business.refund_observation_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid, p_refund_id text, p_amount_minor bigint,
  p_currency text, p_status text, p_payment_intent_id text, p_metadata_operation_id uuid,
  p_provider_created_at timestamptz)
RETURNS TABLE (observation_id uuid, matched_operation_id uuid, quarantined boolean, quarantine_reason text, result text,
               resolved boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.refund_job := business.refund_leased_reconcile(p_job_id, p_worker, p_generation, p_collection);
  c business.payment_collection%ROWTYPE;
  b business.refund_observation%ROWTYPE;
  o business.refund_operation%ROWTYPE;
  v_reason text;
  v_result text;
BEGIN
  IF p_refund_id IS NULL OR p_refund_id !~ '^(re|pyr)_[A-Za-z0-9_]{1,250}$' OR p_amount_minor IS NULL OR p_amount_minor < 0
     OR p_currency IS NULL OR p_currency !~ '^[a-z]{3}$' OR p_status IS NULL OR p_status !~ '^[a-z_]{1,40}$' THEN
    RAISE EXCEPTION 'invalid refund observation' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  PERFORM 1 FROM business.orders ord
  WHERE ord.id = (SELECT pc.order_id FROM business.payment_collection pc WHERE pc.id = v.collection_id)
  FOR UPDATE;
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  IF p_payment_intent_id IS DISTINCT FROM c.payment_intent_id THEN
    RAISE EXCEPTION 'refund % is not of the payment intent of collection %', p_refund_id, c.id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT * INTO b FROM business.refund_observation ob
  WHERE ob.provider_account_id = c.provider_account_id AND ob.livemode = c.livemode AND ob.provider_refund_id = p_refund_id
  FOR UPDATE;
  IF FOUND THEN
    IF b.collection_id <> c.id THEN
      RAISE EXCEPTION 'refund % is recorded for another collection', p_refund_id USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_result := 'unchanged';
    IF b.status IS DISTINCT FROM p_status THEN
      v_result := 'updated';
    END IF;
    UPDATE business.refund_observation ob SET status = p_status, last_observed_at = pg_catalog.now()
    WHERE ob.id = b.id;
    -- An unmatched refund whose ID a provider operation of this collection
    -- now records (a manager's succeeded decision, verified here against
    -- Stripe's own record: same amount, gbp, payment intent) is matched to
    -- it, so its money is counted once, through the operation.
    IF b.matched_operation_id IS NULL AND b.quarantined AND b.resolved_at IS NULL AND b.quarantine_reason = 'unmatched' THEN
      SELECT * INTO o FROM business.refund_operation ro
      WHERE ro.collection_id = c.id AND ro.channel = 'provider' AND ro.provider_account_id = c.provider_account_id
        AND ro.livemode = c.livemode AND ro.provider_refund_id = p_refund_id
      FOR UPDATE;
      IF FOUND AND p_amount_minor = o.amount_minor AND p_currency = 'gbp'
         AND o.payment_intent_id IS NOT DISTINCT FROM p_payment_intent_id
         AND (b.metadata_operation_id IS NULL OR b.metadata_operation_id = o.id)
         AND NOT EXISTS (SELECT 1 FROM business.refund_observation ob WHERE ob.matched_operation_id = o.id) THEN
        UPDATE business.refund_observation ob
        SET matched_operation_id = o.id, quarantined = false, quarantine_reason = NULL, named_operation_id = NULL
        WHERE ob.id = b.id;
        PERFORM business.audit_record('system', NULL, 'refund.observation_matched', 'order', c.order_id, NULL,
                                      pg_catalog.json_build_object('observation_id', b.id, 'refund_operation_id', o.id));
        b.matched_operation_id := o.id;
        b.quarantined := false;
        b.quarantine_reason := NULL;
        v_result := 'updated';
      END IF;
    END IF;
    IF business.refund_observation_contradicts(b.matched_operation_id, p_refund_id, p_status, p_amount_minor, p_currency) THEN
      v_result := 'contradiction';
    END IF;
    RETURN QUERY SELECT b.id, b.matched_operation_id, b.quarantined, b.quarantine_reason, v_result, b.resolved_at IS NOT NULL;
    RETURN;
  END IF;

  SELECT * INTO o FROM business.refund_operation ro
  WHERE ro.collection_id = c.id
    AND (ro.id = p_metadata_operation_id
         OR (ro.provider_account_id = c.provider_account_id AND ro.livemode = c.livemode
             AND ro.provider_refund_id = p_refund_id))
  ORDER BY (ro.provider_refund_id = p_refund_id) DESC NULLS LAST
  LIMIT 1
  FOR UPDATE;
  IF NOT FOUND THEN
    v_reason := CASE WHEN p_currency <> 'gbp' THEN 'currency_mismatch' ELSE 'unmatched' END;
  ELSIF o.provider_refund_id IS NOT NULL AND o.provider_refund_id <> p_refund_id
        OR EXISTS (SELECT 1 FROM business.refund_observation ob WHERE ob.matched_operation_id = o.id) THEN
    v_reason := 'operation_already_refunded';
  ELSIF p_currency <> 'gbp' THEN
    v_reason := 'currency_mismatch';
  ELSIF p_amount_minor <> o.amount_minor THEN
    v_reason := 'amount_mismatch';
  ELSIF o.payment_intent_id IS DISTINCT FROM p_payment_intent_id THEN
    v_reason := 'payment_intent_mismatch';
  END IF;
  INSERT INTO business.refund_observation (id, collection_id, provider_account_id, livemode, provider_refund_id, amount_minor,
                                           currency, status, metadata_operation_id, matched_operation_id,
                                           named_operation_id, quarantined, quarantine_reason, source, provider_created_at)
  VALUES (pg_catalog.gen_random_uuid(), c.id, c.provider_account_id, c.livemode, p_refund_id, p_amount_minor, p_currency,
          p_status, p_metadata_operation_id, CASE WHEN v_reason IS NULL THEN o.id END,
          CASE WHEN v_reason IN ('amount_mismatch', 'currency_mismatch', 'payment_intent_mismatch') THEN o.id END,
          v_reason IS NOT NULL, v_reason, 'reconcile', p_provider_created_at)
  RETURNING * INTO b;
  IF v_reason IS NOT NULL THEN
    PERFORM business.audit_record('system', NULL, 'refund.observation_quarantined', 'order', c.order_id, NULL,
                                  pg_catalog.json_build_object('observation_id', b.id, 'collection_id', c.id,
                                                               'amount_minor', p_amount_minor, 'reason', v_reason));
  END IF;
  v_result := 'recorded';
  IF business.refund_observation_contradicts(b.matched_operation_id, p_refund_id, p_status, p_amount_minor, p_currency) THEN
    v_result := 'contradiction';
  END IF;
  RETURN QUERY SELECT b.id, b.matched_operation_id, b.quarantined, b.quarantine_reason, v_result, false;
END;
$$;

-- As in 0016, plus not_found_at on a not_found verdict (see the header, 1).
CREATE OR REPLACE FUNCTION business.refund_reconcile_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_verdict text, p_refund_id text,
  p_provider_status text, p_evidence json, p_reason text)
RETURNS TABLE (operation_state text, window_ended boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  o business.refund_operation%ROWTYPE;
  v business.refund_job;
  v_state text;
BEGIN
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_operation_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'refund operation % not found', p_operation_id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  v := business.refund_leased_reconcile(p_job_id, p_worker, p_generation, o.collection_id);
  IF p_verdict IS NULL OR p_verdict NOT IN ('succeeded', 'pending', 'requires_action', 'failed', 'not_found', 'mismatch')
     OR (p_verdict = 'mismatch' AND (p_reason IS NULL OR length(p_reason) NOT BETWEEN 1 AND 200))
     OR (p_verdict = 'failed' AND p_reason IS NOT NULL AND p_reason !~ '^[a-z][a-z0-9_]{0,63}$')
     OR (p_verdict IN ('succeeded', 'pending', 'requires_action', 'not_found') AND p_reason IS NOT NULL)
     OR (p_verdict IN ('succeeded', 'pending', 'requires_action', 'failed')) <> (p_refund_id IS NOT NULL)
     OR p_refund_id !~ '^(re|pyr)_[A-Za-z0-9_]{1,250}$'
     OR p_provider_status !~ '^[a-z_]{1,40}$' THEN
    RAISE EXCEPTION 'invalid refund reconcile verdict %', p_verdict USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Lock order (K00): the order, the collection, the operation.
  PERFORM 1 FROM business.orders ord WHERE ord.id = o.order_id FOR UPDATE;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = o.collection_id FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_operation_id FOR UPDATE;
  IF o.dispatch_started_at IS NULL THEN
    RAISE EXCEPTION 'refund operation % was never admitted for dispatch', o.id USING ERRCODE = 'invalid_parameter_value';
  END IF;

  IF p_verdict = 'mismatch' THEN
    v_state := business.refund_mismatch(o.id, p_reason, p_evidence, false);
  ELSIF p_verdict = 'not_found' THEN
    UPDATE business.refund_operation ro
    SET state = CASE WHEN ro.state = 'submitting' THEN 'unknown' ELSE ro.state END,
        last_observed_at = pg_catalog.now(), not_found_at = pg_catalog.now(),
        version = ro.version + 1, updated_at = pg_catalog.now()
    WHERE ro.id = o.id
    RETURNING ro.state INTO v_state;
  ELSIF o.state IN ('succeeded', 'failed', 'cancelled') THEN
    IF o.state <> p_verdict
       AND o.mismatch_reason IS DISTINCT FROM 'Stripe now reports ' || p_verdict || ' for a ' || o.state || ' refund' THEN
      -- Never an automatic payout or reopening after a contradiction.
      v_state := business.refund_mismatch(o.id, 'Stripe now reports ' || p_verdict || ' for a ' || o.state || ' refund',
                                          p_evidence, false);
    ELSIF o.state <> p_verdict THEN
      -- The same contradiction is already recorded (by the observation or
      -- an earlier reconcile).
      UPDATE business.refund_operation ro SET last_observed_at = pg_catalog.now() WHERE ro.id = o.id;
      v_state := o.state;
    ELSE
      UPDATE business.refund_operation ro SET last_observed_at = pg_catalog.now() WHERE ro.id = o.id;
      v_state := o.state;
    END IF;
  ELSIF o.provider_refund_id IS NOT NULL AND o.provider_refund_id <> p_refund_id THEN
    v_state := business.refund_mismatch(o.id, 'another refund than the one recorded for this operation', p_evidence, false);
  ELSE
    v_state := business.refund_settle(o.id, p_verdict, coalesce(p_provider_status, p_verdict), p_refund_id, p_evidence,
                                      p_reason);
  END IF;
  RETURN QUERY SELECT v_state, o.replay_until IS NOT NULL AND pg_catalog.now() >= o.replay_until;
END;
$$;

-- As in 0025, plus settling attention jobs and enqueueing the journal
-- resolution (see the header, 3 and 4).
CREATE OR REPLACE FUNCTION business.refund_decision_record(p_order uuid, p_account uuid, p_action text, p_collection uuid,
                                                p_operation uuid, p_observation uuid, p_reserved uuid, p_note text,
                                                p_evidence json)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid := pg_catalog.gen_random_uuid();
  v_version integer;
BEGIN
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order
  RETURNING o.version INTO v_version;
  INSERT INTO business.refund_recovery_decision (id, order_id, action, collection_id, refund_operation_id, observation_id,
                                                 reserved_operation_id, note, evidence, order_version, decided_by)
  VALUES (v_id, p_order, p_action, p_collection, p_operation, p_observation, p_reserved, p_note, p_evidence, v_version,
          p_account);
  PERFORM business.audit_record('staff', p_account, 'refund.recovery_decided', 'order', p_order, NULL,
                                pg_catalog.json_build_object('decision_id', v_id, 'action', p_action,
                                                             'collection_id', p_collection, 'refund_operation_id', p_operation,
                                                             'observation_id', p_observation,
                                                             'reserved_operation_id', p_reserved));
  PERFORM business.refund_attention_settle(p_order, v_id);
  PERFORM business.refund_resolve_enqueue(p_operation, v_id);
  RETURN v_id;
END;
$$;

-- As in 0025, with the shared non-execution proof and the identity rule
-- (see the header, 1 and 2).
CREATE OR REPLACE FUNCTION business.refund_operation_decide(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                                 p_expected_version integer, p_operation uuid, p_decision text,
                                                 p_refund_id text, p_note text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  o business.refund_operation%ROWTYPE;
  b business.refund_observation%ROWTYPE;
  v_id uuid;
  v_state text;
BEGIN
  IF p_order IS NULL OR p_operation IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_decision IS NULL OR p_decision NOT IN ('failed', 'succeeded')
     OR (p_decision = 'succeeded') <> (p_refund_id IS NOT NULL)
     OR p_refund_id !~ '^(re|pyr)_[A-Za-z0-9_]{1,250}$' THEN
    RAISE EXCEPTION 'decision_invalid' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.refund_note_check(p_note, true);
  v_prior := business.command_claim(v_account, 'refund.operation_decide', p_operation::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM business.refund_manager_admit(p_order, p_expected_version);
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_operation AND ro.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'refund operation not found' USING ERRCODE = 'GK404';
  END IF;
  IF o.channel <> 'provider' OR o.state <> 'unknown' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- Never on a timer or a key expiry alone, and never inside the window.
  IF o.replay_until IS NULL OR pg_catalog.now() < o.replay_until THEN
    RAISE EXCEPTION 'window_open' USING ERRCODE = 'GK409';
  END IF;
  IF p_decision = 'failed' THEN
    -- Contradicting evidence is cleared through refund_mismatch_clear (the
    -- same proof applies there).
    IF o.mismatch_reason IS NOT NULL OR NOT business.refund_not_executed_proven(o.id) THEN
      RAISE EXCEPTION 'evidence_required' USING ERRCODE = 'GK409';
    END IF;
    PERFORM business.refund_fail_not_executed(o.id, v_account);
    v_id := business.refund_decision_record(p_order, v_account, 'operation_failed', o.collection_id, o.id, NULL, NULL, p_note,
                                            pg_catalog.json_build_object('replay_until', o.replay_until,
                                                                         'complete_list_at', o.not_found_at));
  ELSE
    SELECT x.* INTO b FROM business.refund_observation x
    WHERE x.collection_id = o.collection_id AND x.provider_account_id = o.provider_account_id
      AND x.livemode = o.livemode AND x.provider_refund_id = p_refund_id;
    IF FOUND AND (b.matched_operation_id IS NOT NULL OR b.resolved_at IS NOT NULL OR NOT b.quarantined) THEN
      RAISE EXCEPTION 'identity_unproven' USING ERRCODE = 'GK409';
    END IF;
    IF FOUND AND (b.metadata_operation_id = o.id OR o.provider_refund_id = p_refund_id) THEN
      -- Identity proven by Stripe's own record (its metadata names the
      -- operation, or the operation already records this refund).
      v_state := business.refund_observation_match(b.id, o.id, NULL);
      v_id := business.refund_decision_record(p_order, v_account, 'operation_succeeded', o.collection_id, o.id, b.id, NULL,
                                              p_note, pg_catalog.json_build_object('refund_id', p_refund_id,
                                                                                   'operation_state', v_state));
    ELSE
      IF (FOUND AND b.quarantine_reason <> 'unmatched')
         OR (o.provider_refund_id IS NOT NULL AND o.provider_refund_id <> p_refund_id) THEN
        RAISE EXCEPTION 'identity_unproven' USING ERRCODE = 'GK409';
      END IF;
      IF EXISTS (SELECT 1 FROM business.refund_operation ro
                 WHERE ro.id <> o.id AND ro.provider_account_id = o.provider_account_id AND ro.livemode = o.livemode
                   AND ro.provider_refund_id = p_refund_id)
         OR EXISTS (SELECT 1 FROM business.refund_observation x
                    WHERE x.provider_account_id = o.provider_account_id AND x.livemode = o.livemode
                      AND x.provider_refund_id = p_refund_id AND x.collection_id <> o.collection_id) THEN
        RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
      END IF;
      -- Only the reconcile decides it, from Stripe's record of this ID
      -- (complete list, then a direct read: amount, currency, payment
      -- intent); an unmatched observation of it is matched there.
      UPDATE business.refund_operation ro
      SET provider_refund_id = p_refund_id, version = ro.version + 1, updated_at = pg_catalog.now()
      WHERE ro.id = o.id;
      PERFORM business.refund_schedule_reconcile(o.collection_id, pg_catalog.now());
      v_id := business.refund_decision_record(p_order, v_account, 'operation_verification', o.collection_id, o.id, NULL,
                                              NULL, p_note, pg_catalog.json_build_object('refund_id', p_refund_id));
    END IF;
  END IF;
  PERFORM business.command_finish(v_account, 'refund.operation_decide', p_operation::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- As in 0025, with the proofs of the header, 1.
CREATE OR REPLACE FUNCTION business.refund_mismatch_clear(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                               p_expected_version integer, p_operation uuid, p_decision text, p_note text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  o business.refund_operation%ROWTYPE;
  v_id uuid;
BEGIN
  IF p_order IS NULL OR p_operation IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_decision IS NULL OR p_decision NOT IN ('keep', 'failed', 'succeeded') THEN
    RAISE EXCEPTION 'decision_invalid' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.refund_note_check(p_note, true);
  v_prior := business.command_claim(v_account, 'refund.mismatch_clear', p_operation::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM business.refund_manager_admit(p_order, p_expected_version);
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_operation AND ro.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'refund operation not found' USING ERRCODE = 'GK404';
  END IF;
  IF o.mismatch_reason IS NULL
     OR (p_decision = 'failed' AND o.state NOT IN ('succeeded', 'unknown'))
     OR (p_decision = 'succeeded' AND o.state NOT IN ('failed', 'unknown')) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- failed on an unknown refund: exactly the proof of a failed decision
  -- (window ended, complete list after it found nothing, nothing names it,
  -- no refund ID and no refund object in the contradicting evidence).
  IF p_decision = 'failed' AND o.state = 'unknown' THEN
    IF o.replay_until IS NULL OR pg_catalog.now() < o.replay_until THEN
      RAISE EXCEPTION 'window_open' USING ERRCODE = 'GK409';
    END IF;
    IF NOT business.refund_not_executed_proven(o.id) THEN
      RAISE EXCEPTION 'evidence_required' USING ERRCODE = 'GK409';
    END IF;
  END IF;
  -- failed on a succeeded refund: Stripe reports that same refund failed or
  -- canceled.
  IF p_decision = 'failed' AND o.state = 'succeeded'
     AND NOT EXISTS (SELECT 1 FROM business.refund_observation x
                     WHERE x.matched_operation_id = o.id AND x.provider_refund_id = o.provider_refund_id
                       AND x.status IN ('failed', 'canceled')) THEN
    RAISE EXCEPTION 'evidence_required' USING ERRCODE = 'GK409';
  END IF;
  IF p_decision = 'succeeded'
     AND (o.provider_refund_id IS NULL
          OR NOT EXISTS (SELECT 1 FROM business.refund_observation x
                         WHERE x.matched_operation_id = o.id AND x.provider_refund_id = o.provider_refund_id
                           AND x.status = 'succeeded' AND x.amount_minor = o.amount_minor)) THEN
    RAISE EXCEPTION 'evidence_required' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.refund_operation ro
  SET state = CASE p_decision WHEN 'keep' THEN ro.state ELSE p_decision END,
      failure_reason = CASE p_decision WHEN 'failed' THEN 'manager_corrected' WHEN 'succeeded' THEN NULL
                                       ELSE ro.failure_reason END,
      settled_at = CASE WHEN p_decision = 'keep' THEN ro.settled_at ELSE coalesce(ro.settled_at, pg_catalog.now()) END,
      mismatch_reason = NULL, mismatch_evidence = NULL, version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = o.id;
  v_id := business.refund_decision_record(p_order, v_account, 'mismatch_' || p_decision, o.collection_id, o.id, NULL, NULL,
                                          p_note, pg_catalog.json_build_object('prior_state', o.state,
                                                                               'mismatch_reason', o.mismatch_reason));
  PERFORM business.command_finish(v_account, 'refund.mismatch_clear', p_operation::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- As in 0025, without jobs a decision settled (see the header, 3).
CREATE OR REPLACE FUNCTION business.refund_attention_list(p_actor uuid)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  v_chef boolean;
  v_items json;
BEGIN
  v_chef := EXISTS (SELECT 1 FROM business.staff_capability c
                    WHERE c.account_id = v_account AND c.capability = 'chef' AND c.revoked_at IS NULL);
  WITH items AS (
    SELECT 'job_attention'::text AS kind, coalesce(oc.order_id, pc.order_id, ptj.order_id, hr.order_id) AS order_id,
           coalesce(rj.collection_id, pj.collection_id) AS collection_id, rj.refund_operation_id AS refund_operation_id,
           NULL::uuid AS observation_id, j.id AS job_id, j.attention_reason AS reason, NULL::bigint AS amount_minor,
           NULL::boolean AS window_ended, j.updated_at AS since
    FROM business.job j
    LEFT JOIN business.refund_job rj ON rj.job_id = j.id
    LEFT JOIN business.payment_job pj ON pj.job_id = j.id
    LEFT JOIN business.payment_collection pc ON pc.id = coalesce(rj.collection_id, pj.collection_id)
    LEFT JOIN business.order_command oc ON oc.job_id = j.id
    LEFT JOIN business.production_timer_job ptj ON ptj.job_id = j.id
    LEFT JOIN business.order_hold_review_job hr ON hr.job_id = j.id
    WHERE j.state = 'attention' AND NOT business.refund_job_settled(j.id)
    UNION ALL
    SELECT 'quarantined_observation', pc.order_id, b.collection_id, b.named_operation_id, b.id, NULL, b.quarantine_reason,
           b.amount_minor, NULL, b.observed_at
    FROM business.refund_observation b JOIN business.payment_collection pc ON pc.id = b.collection_id
    WHERE b.quarantined AND b.resolved_at IS NULL
    UNION ALL
    SELECT 'refund_contradiction', ro.order_id, ro.collection_id, ro.id, NULL, NULL, ro.mismatch_reason, ro.amount_minor,
           NULL, ro.updated_at
    FROM business.refund_operation ro WHERE ro.mismatch_reason IS NOT NULL
    UNION ALL
    SELECT 'payment_contradiction', pc.order_id, pc.id, NULL, NULL, NULL, pc.mismatch_reason, pc.amount_minor, NULL,
           pc.updated_at
    FROM business.payment_collection pc WHERE pc.mismatch_reason IS NOT NULL
    UNION ALL
    SELECT 'refund_' || ro.state, ro.order_id, ro.collection_id, ro.id, NULL, NULL, NULL, ro.amount_minor,
           ro.replay_until IS NOT NULL AND pg_catalog.now() >= ro.replay_until, ro.updated_at
    FROM business.refund_operation ro
    WHERE ro.channel = 'provider' AND ro.state IN ('unknown', 'requires_action') AND ro.mismatch_reason IS NULL
    UNION ALL
    SELECT 'refund_failed_owed', ro.order_id, ro.collection_id, ro.id, NULL, NULL, ro.failure_reason,
           business.refund_remainder_amount(ro.id), NULL, ro.updated_at
    FROM business.refund_operation ro
    WHERE ro.state = 'failed' AND ro.mismatch_reason IS NULL
      AND NOT EXISTS (SELECT 1 FROM business.refund_operation x
                      WHERE x.collection_id = ro.collection_id AND x.id <> ro.id AND x.state <> 'cancelled'
                        AND (x.created_at, x.id) > (ro.created_at, ro.id))
      AND business.refund_goal_remaining(ro.order_id) > 0
    UNION ALL
    SELECT 'delivery_pending', g.order_id, NULL, NULL, NULL, NULL, NULL,
           business.refund_delivery_pending(g.order_id, g.target_minor), NULL, g.updated_at
    FROM business.refund_goal g
    WHERE business.refund_delivery_pending(g.order_id, g.target_minor) > 0
    UNION ALL
    SELECT 'manual_payout_' || ro.state, ro.order_id, ro.collection_id, ro.id, NULL, NULL, NULL, ro.amount_minor, NULL,
           ro.updated_at
    FROM business.refund_operation ro
    WHERE ro.channel = 'manual_alternative' AND ro.state IN ('reserved', 'pending', 'unknown')
  )
  SELECT coalesce(pg_catalog.json_agg(pg_catalog.json_build_object(
           'kind', i.kind, 'order_id', i.order_id, 'collection_id', i.collection_id,
           'refund_operation_id', i.refund_operation_id, 'observation_id', i.observation_id, 'job_id', i.job_id,
           'reason', i.reason, 'amount_minor', CASE WHEN v_chef THEN i.amount_minor END,
           'currency', CASE WHEN v_chef AND i.amount_minor IS NOT NULL THEN 'GBP' END,
           'window_ended', i.window_ended, 'since', i.since)
         ORDER BY i.since, i.kind, i.order_id), '[]'::json)
  INTO v_items FROM items i;
  IF NOT v_chef THEN
    PERFORM business.audit_record('staff', v_account, 'attention.viewed', 'account', v_account, NULL,
                                  pg_catalog.json_build_object('items', pg_catalog.json_array_length(v_items)));
  END IF;
  RETURN v_items;
END;
$$;

-- As in 0026, plus the evidence reference for withdrawn.
CREATE OR REPLACE FUNCTION business.manual_payout_outcome_request(p_actor uuid, p_order uuid, p_key text, p_body_sha256 text,
                                                       p_expected_version integer, p_operation uuid, p_outcome text,
                                                       p_transfer_identity text, p_evidence_ref text, p_amount bigint,
                                                       p_currency text, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_o business.orders%ROWTYPE;
  v_prior uuid;
  o business.refund_operation%ROWTYPE;
  v_id uuid;
BEGIN
  IF p_now IS NULL OR p_order IS NULL OR p_operation IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_outcome IS NULL OR p_outcome NOT IN ('submitted', 'succeeded', 'failed', 'unknown', 'withdrawn')
     OR (p_outcome = 'succeeded') <> (p_transfer_identity IS NOT NULL)
     OR (p_outcome = 'succeeded') <> (p_amount IS NOT NULL) OR (p_outcome = 'succeeded') <> (p_currency IS NOT NULL)
     OR (p_outcome IN ('succeeded', 'failed', 'withdrawn') AND p_evidence_ref IS NULL) THEN
    RAISE EXCEPTION 'outcome_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF (p_transfer_identity IS NOT NULL AND NOT business.evidence_ref_valid(p_transfer_identity))
     OR (p_evidence_ref IS NOT NULL AND NOT business.evidence_ref_valid(p_evidence_ref)) THEN
    RAISE EXCEPTION 'evidence_ref_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_currency IS DISTINCT FROM 'GBP' AND p_currency IS NOT NULL THEN
    RAISE EXCEPTION 'currency_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  SELECT * INTO v_o FROM business.orders x WHERE x.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v_prior := business.manual_payout_command_admit(v_o, v_account, 'manual_payout_outcome', p_key, p_body_sha256,
                                                  p_expected_version);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT ro.* INTO o FROM business.refund_operation ro
  WHERE ro.id = p_operation AND ro.order_id = p_order AND ro.channel = 'manual_alternative';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'refund operation not found' USING ERRCODE = 'GK404';
  END IF;
  IF NOT business.manual_payout_transition(o.state, p_outcome) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF p_outcome = 'succeeded' THEN
    IF p_amount <> o.amount_minor THEN
      RAISE EXCEPTION 'amount_invalid' USING ERRCODE = 'GK422';
    END IF;
    IF EXISTS (SELECT 1 FROM business.manual_payout m WHERE m.transfer_identity = p_transfer_identity) THEN
      RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
    END IF;
  END IF;
  v_id := business.manual_payout_command_record(v_o, v_account, 'manual_payout_outcome', p_key, p_body_sha256, p_env, p_now,
                                                pg_catalog.json_build_object('refund_operation_id', o.id,
                                                                             'outcome', p_outcome,
                                                                             'transfer_identity', p_transfer_identity,
                                                                             'evidence_ref', p_evidence_ref,
                                                                             'amount_minor', p_amount,
                                                                             'currency', p_currency));
  INSERT INTO business.manual_payout_command (command_id, kind, refund_operation_id, outcome, transfer_identity, evidence_ref,
                                              amount_minor, currency)
  VALUES (v_id, 'outcome', o.id, p_outcome, p_transfer_identity, p_evidence_ref, p_amount, p_currency);
  PERFORM business.audit_record('staff', v_account, 'refund.manual_payout_outcome_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id, 'refund_operation_id', o.id,
                                                             'outcome', p_outcome));
  RETURN QUERY SELECT v_id, false;
END;
$$;

REVOKE ALL ON TABLE business.refund_resolve_job FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_not_executed_proven(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_fail_not_executed(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_job_settled(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_attention_settle(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_resolve_enqueue(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_resolve_leased(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_resolve_leased_op(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_resolve_begin(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_resolve_lookup(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_resolve_committed(uuid, uuid, bigint, uuid, json, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_resolve_secured(uuid, uuid, bigint, uuid) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.refund_resolve_begin(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_resolve_lookup(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamptz) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_resolve_committed(uuid, uuid, bigint, uuid, json, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.refund_resolve_secured(uuid, uuid, bigint, uuid) TO gk_worker;
