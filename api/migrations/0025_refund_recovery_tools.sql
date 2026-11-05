-- GK-026 B2 part 2a: manager recovery tools for the refund ledger
-- (contracts r6.5 K05 "External observation table preserves unmatched/
-- over-refund/dispute facts; quarantine blocks new dispatch. Example
-- 10000p collection+unknown10000p local+external5000p return: preserve
-- possible15000p exposure, no clipping or second call. Match identities
-- using complete provider pages and definitive nonexecution; audit
-- correction/unfreeze", "unknown never releases balance on a timer or
-- provider-key expiry", "No automatic payout after contradictory success
-- correction", "Separate capability for chef cancellation, refunds and
-- recovery"; K00 capabilities: technical support gets bounded audited read
-- access and no money authority; K11 "unresolved facts go to chef queue").
--
-- Every decision is the chef's (GK403 otherwise), at the order version the
-- chef saw (409 stale_version; the order version is bumped), with an
-- Idempotency-Key (the same key and body replays, also during a restore
-- freeze; another body is key_reused), refused GK503 during a restore
-- freeze (freeze first after the replay), refused state_conflict while a
-- K11 command of the order is unresolved, recorded immutably with the
-- chef's note in business.refund_recovery_decision and audited
-- (refund.recovery_decided, without the note). None sends money: a
-- reservation is dispatched by the worker under its own K11 protection.
--
-- 1. Resolve a quarantined observation (business.refund_observation_resolve):
--    'match' links it to a local provider operation of the same collection
--    only when the identities prove it (its metadata names the operation, or
--    the operation already records that refund ID; same amount, currency gbp,
--    the collection's payment intent; no other refund matched to the
--    operation; the operation unresolved): the operation is then settled by
--    the refund's own status (409 identity_unproven otherwise).
--    'accept_external' keeps the observation (never deleted) and records the
--    resolution: it no longer blocks dispatch and its money counts as
--    returned by its own status (business.refund_balance); nothing new is
--    reserved (no blind new refund).
-- 2. Decide an unknown provider refund past its replay window
--    (business.refund_operation_decide; 409 window_open before it):
--    'failed' only with complete-provider-list evidence of non-execution: a
--    complete reconcile listing after the window found nothing for it
--    (last_observed_at at or after replay_until, set only by a complete
--    list) and no observation names, matches or carries its refund ID (409
--    evidence_required otherwise). 'succeeded' needs a provider refund ID:
--    a refund already observed for the collection under that ID is matched
--    as in 1 (identities proven by the manager's ID and the provider's
--    record; same amount); otherwise the ID is recorded on the operation and
--    the collection's reconcile is scheduled now, which verifies it at
--    Stripe (list, then GET /v1/refunds/{id}; amount, currency, payment
--    intent) before anything is decided. A refund ID recorded for another
--    operation is 409 duplicate.
-- 3. Clear a contradiction (business.refund_mismatch_clear on a refund
--    operation, business.payment_mismatch_clear on a decided payment
--    collection): 'keep' clears the evidence and keeps the recorded state;
--    'failed' corrects a refund to failed (its remainder reopens; nothing is
--    re-sent: 4 is a separate decision); 'succeeded' corrects it to
--    succeeded only when a refund matched to it is succeeded at Stripe. A
--    payment collection's mismatch is cleared only on a succeeded or failed
--    collection (an unknown payment outcome needs payment reconciliation,
--    not a note).
-- 4. Re-reserve a failed provider refund's remainder
--    (business.refund_remainder_reserve): the latest operation of its
--    collection, failed; none unresolved on the collection (409
--    refund_in_progress); no reconciliation outstanding on the order (409
--    reconciliation_required); min(its amount, the collection's room, what
--    the goal still needs) on the same collection (a failed exception refund
--    returns to the collection the chef chose), 409 no_refund_pending at 0.
-- 5. The attention list (business.refund_attention_list; chef or technical
--    support): jobs in attention with their reason, unresolved quarantined
--    observations, contradictions on refund operations and payment
--    collections, unknown or requires_action refunds (with whether the
--    window ended), failed refunds whose remainder is still owed, pending
--    delivery shares and unresolved manual payouts. Amounts only for the
--    chef; a technical support read is audited (attention.viewed).
--
-- Also: business.refund_chef_admit no longer requires recovery_state
-- 'secured' (a never-confirmed order, for example a late capture on a
-- released order, is 'prepared'); it refuses confirmation_pending and
-- cancellation_pending and any unsecured K11 command instead.
--
-- Expand-only (K09): a new table and functions; a function replaced with
-- its signature unchanged. Lock order (K00): the order, its collections by
-- ID, the goal, refund operations, observations.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.refund_recovery_decision (
  id                  uuid PRIMARY KEY,
  order_id            uuid NOT NULL REFERENCES business.orders (id),
  action              text NOT NULL CHECK (action IN ('observation_match', 'observation_accept_external',
                                                      'operation_failed', 'operation_succeeded',
                                                      'operation_verification', 'mismatch_keep', 'mismatch_failed',
                                                      'mismatch_succeeded', 'payment_mismatch_clear',
                                                      'remainder_reserved')),
  collection_id       uuid NULL REFERENCES business.payment_collection (id),
  refund_operation_id uuid NULL REFERENCES business.refund_operation (id),
  observation_id      uuid NULL REFERENCES business.refund_observation (id),
  -- The operation reserved by a remainder decision.
  reserved_operation_id uuid NULL REFERENCES business.refund_operation (id),
  note                text NULL CHECK (note = btrim(note) AND char_length(note) BETWEEN 1 AND 500
                                       AND note !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  -- Bounded evidence: IDs, states, amounts, times (never personal data).
  evidence            json NULL CHECK (evidence IS NULL OR octet_length(evidence::text) <= 4096),
  order_version       integer NOT NULL CHECK (order_version >= 1),
  decided_by          uuid NOT NULL REFERENCES business.account (id),
  created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX refund_recovery_decision_order_idx ON business.refund_recovery_decision (order_id, created_at);
CREATE TRIGGER refund_recovery_decision_immutable BEFORE UPDATE OR DELETE ON business.refund_recovery_decision
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER refund_recovery_decision_no_truncate BEFORE TRUNCATE ON business.refund_recovery_decision
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: the admission of a chef's refund or recovery decision on
-- order p_order without the reconciliation check (the recovery tools are
-- the reconciliation): freeze first, the order and its collections locked,
-- the version and K11 state checked. Returns the order.
CREATE FUNCTION business.refund_manager_admit(p_order uuid, p_expected_version integer)
RETURNS business.orders
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_frozen boolean;
  v_o business.orders%ROWTYPE;
BEGIN
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state IN ('confirmation_pending', 'cancellation_pending')
     OR EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                WHERE c.order_id = p_order AND r.state <> 'secured') THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  RETURN v_o;
END;
$$;

-- As in 0024, through business.refund_manager_admit.
CREATE OR REPLACE FUNCTION business.refund_chef_admit(p_order uuid, p_expected_version integer)
RETURNS business.orders
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
BEGIN
  v_o := business.refund_manager_admit(p_order, p_expected_version);
  IF business.refund_reconciliation_required(p_order) THEN
    RAISE EXCEPTION 'reconciliation_required' USING ERRCODE = 'GK409';
  END IF;
  RETURN v_o;
END;
$$;

-- Owner-internal: validate a decision note (NULL allowed when p_required is
-- false).
CREATE FUNCTION business.refund_note_check(p_note text, p_required boolean)
RETURNS void
LANGUAGE plpgsql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF (p_note IS NULL AND p_required)
     OR (p_note IS NOT NULL AND (p_note <> pg_catalog.btrim(p_note) OR pg_catalog.char_length(p_note) NOT BETWEEN 1 AND 500
                                 OR p_note ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]')) THEN
    RAISE EXCEPTION 'note_invalid' USING ERRCODE = 'GK422';
  END IF;
END;
$$;

-- Owner-internal: record decision p_action (the caller holds the locks),
-- bump the order version and audit. Returns the decision.
CREATE FUNCTION business.refund_decision_record(p_order uuid, p_account uuid, p_action text, p_collection uuid,
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
  RETURN v_id;
END;
$$;

-- Owner-internal: match quarantined observation p_obs to provider operation
-- p_op (both locked by the caller) when the identities prove it; settles
-- the operation by the refund's own status. p_manager_id: the refund ID the
-- manager supplied (proves the identity together with the provider's
-- record). Raises identity_unproven otherwise. Returns the operation state.
CREATE FUNCTION business.refund_observation_match(p_obs uuid, p_op uuid, p_manager_id text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  b business.refund_observation%ROWTYPE;
  o business.refund_operation%ROWTYPE;
  c business.payment_collection%ROWTYPE;
BEGIN
  SELECT * INTO b FROM business.refund_observation x WHERE x.id = p_obs FOR UPDATE;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_op FOR UPDATE;
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = b.collection_id;
  IF o.id IS NULL OR o.collection_id <> b.collection_id OR o.channel <> 'provider'
     OR NOT (b.metadata_operation_id IS NOT DISTINCT FROM o.id AND b.metadata_operation_id IS NOT NULL
             OR o.provider_refund_id = b.provider_refund_id
             OR (p_manager_id IS NOT NULL AND p_manager_id = b.provider_refund_id AND o.provider_refund_id IS NULL))
     OR b.amount_minor <> o.amount_minor OR b.currency <> 'gbp' OR o.payment_intent_id IS DISTINCT FROM c.payment_intent_id
     OR b.provider_account_id <> o.provider_account_id OR b.livemode <> o.livemode
     OR o.state NOT IN ('submitting', 'pending', 'unknown', 'requires_action')
     OR b.status NOT IN ('succeeded', 'pending', 'requires_action', 'failed', 'canceled')
     OR (o.provider_refund_id IS NOT NULL AND o.provider_refund_id <> b.provider_refund_id)
     OR EXISTS (SELECT 1 FROM business.refund_observation x WHERE x.matched_operation_id = o.id) THEN
    RAISE EXCEPTION 'identity_unproven' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.refund_observation x
  SET matched_operation_id = o.id, quarantined = false, quarantine_reason = NULL, named_operation_id = NULL,
      last_observed_at = pg_catalog.now()
  WHERE x.id = b.id;
  RETURN business.refund_settle(o.id, b.status, b.status, b.provider_refund_id,
                                pg_catalog.json_build_object('refund_id', b.provider_refund_id, 'status', b.status,
                                                             'amount_minor', b.amount_minor, 'source', 'manager_match'),
                                NULL);
END;
$$;

-- API (chef): resolve a quarantined observation (see the header, 1).
CREATE FUNCTION business.refund_observation_resolve(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                                    p_expected_version integer, p_observation uuid, p_resolution text,
                                                    p_operation uuid, p_note text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  b business.refund_observation%ROWTYPE;
  v_id uuid;
  v_state text;
BEGIN
  IF p_order IS NULL OR p_observation IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_resolution IS NULL OR p_resolution NOT IN ('match', 'accept_external')
     OR (p_resolution = 'match') <> (p_operation IS NOT NULL) THEN
    RAISE EXCEPTION 'resolution_invalid' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.refund_note_check(p_note, true);
  v_prior := business.command_claim(v_account, 'refund.observation_resolve', p_observation::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM business.refund_manager_admit(p_order, p_expected_version);
  SELECT x.* INTO b FROM business.refund_observation x JOIN business.payment_collection pc ON pc.id = x.collection_id
  WHERE x.id = p_observation AND pc.order_id = p_order FOR UPDATE OF x;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'observation not found' USING ERRCODE = 'GK404';
  END IF;
  IF NOT b.quarantined OR b.resolved_at IS NOT NULL THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF p_resolution = 'match' THEN
    PERFORM 1 FROM business.refund_operation ro WHERE ro.id = p_operation AND ro.order_id = p_order;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'identity_unproven' USING ERRCODE = 'GK409';
    END IF;
    v_state := business.refund_observation_match(b.id, p_operation, NULL);
    v_id := business.refund_decision_record(p_order, v_account, 'observation_match', b.collection_id, p_operation, b.id,
                                            NULL, p_note,
                                            pg_catalog.json_build_object('refund_id', b.provider_refund_id,
                                                                         'amount_minor', b.amount_minor,
                                                                         'status', b.status, 'operation_state', v_state));
  ELSE
    UPDATE business.refund_observation x
    SET resolved_at = pg_catalog.now(), resolved_by = v_account, resolution_note = p_note
    WHERE x.id = b.id;
    v_id := business.refund_decision_record(p_order, v_account, 'observation_accept_external', b.collection_id, NULL, b.id,
                                            NULL, p_note,
                                            pg_catalog.json_build_object('refund_id', b.provider_refund_id,
                                                                         'amount_minor', b.amount_minor,
                                                                         'status', b.status,
                                                                         'quarantine_reason', b.quarantine_reason));
  END IF;
  PERFORM business.command_finish(v_account, 'refund.observation_resolve', p_observation::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- API (chef): decide an unknown provider refund past its replay window
-- (see the header, 2).
CREATE FUNCTION business.refund_operation_decide(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
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
  v_obs uuid;
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
    IF o.provider_refund_id IS NOT NULL OR o.mismatch_reason IS NOT NULL
       OR o.last_observed_at IS NULL OR o.last_observed_at < o.replay_until
       OR EXISTS (SELECT 1 FROM business.refund_observation x
                  WHERE x.collection_id = o.collection_id
                    AND (x.metadata_operation_id = o.id OR x.matched_operation_id = o.id OR x.named_operation_id = o.id)) THEN
      RAISE EXCEPTION 'evidence_required' USING ERRCODE = 'GK409';
    END IF;
    UPDATE business.refund_operation ro
    SET state = 'failed', failure_reason = 'not_executed_confirmed', settled_at = pg_catalog.now(),
        version = ro.version + 1, updated_at = pg_catalog.now()
    WHERE ro.id = o.id;
    PERFORM business.audit_record('staff', v_account, 'refund.failed', 'order', o.order_id, NULL,
                                  pg_catalog.json_build_object('refund_operation_id', o.id, 'collection_id', o.collection_id,
                                                               'amount_minor', o.amount_minor));
    v_id := business.refund_decision_record(p_order, v_account, 'operation_failed', o.collection_id, o.id, NULL, NULL, p_note,
                                            pg_catalog.json_build_object('replay_until', o.replay_until,
                                                                         'complete_list_at', o.last_observed_at));
  ELSE
    SELECT x.id INTO v_obs FROM business.refund_observation x
    WHERE x.collection_id = o.collection_id AND x.provider_account_id = o.provider_account_id
      AND x.livemode = o.livemode AND x.provider_refund_id = p_refund_id;
    IF FOUND THEN
      IF NOT EXISTS (SELECT 1 FROM business.refund_observation x
                     WHERE x.id = v_obs AND x.quarantined AND x.resolved_at IS NULL) THEN
        RAISE EXCEPTION 'identity_unproven' USING ERRCODE = 'GK409';
      END IF;
      v_state := business.refund_observation_match(v_obs, o.id, p_refund_id);
      v_id := business.refund_decision_record(p_order, v_account, 'operation_succeeded', o.collection_id, o.id, v_obs, NULL,
                                              p_note, pg_catalog.json_build_object('refund_id', p_refund_id,
                                                                                   'operation_state', v_state));
    ELSE
      IF o.provider_refund_id IS NOT NULL AND o.provider_refund_id <> p_refund_id THEN
        RAISE EXCEPTION 'identity_unproven' USING ERRCODE = 'GK409';
      END IF;
      IF EXISTS (SELECT 1 FROM business.refund_operation ro
                 WHERE ro.id <> o.id AND ro.provider_account_id = o.provider_account_id AND ro.livemode = o.livemode
                   AND ro.provider_refund_id = p_refund_id)
         OR EXISTS (SELECT 1 FROM business.refund_observation x
                    WHERE x.provider_account_id = o.provider_account_id AND x.livemode = o.livemode
                      AND x.provider_refund_id = p_refund_id) THEN
        RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
      END IF;
      -- Verified by the reconcile (list, then a direct read) before any
      -- decision: the operation stays unknown until Stripe confirms it.
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

-- API (chef): clear the contradiction recorded on a refund operation (see
-- the header, 3).
CREATE FUNCTION business.refund_mismatch_clear(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
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

-- API (chef): clear the contradiction recorded on a decided payment
-- collection (see the header, 3).
CREATE FUNCTION business.payment_mismatch_clear(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                                p_expected_version integer, p_collection uuid, p_note text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  c business.payment_collection%ROWTYPE;
  v_id uuid;
BEGIN
  IF p_order IS NULL OR p_collection IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.refund_note_check(p_note, true);
  v_prior := business.command_claim(v_account, 'payment.mismatch_clear', p_collection::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM business.refund_manager_admit(p_order, p_expected_version);
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = p_collection AND pc.order_id = p_order;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'collection not found' USING ERRCODE = 'GK404';
  END IF;
  IF c.mismatch_reason IS NULL OR c.state NOT IN ('succeeded', 'failed') THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.payment_collection pc
  SET mismatch_reason = NULL, mismatch_evidence = NULL, updated_at = pg_catalog.now()
  WHERE pc.id = c.id;
  v_id := business.refund_decision_record(p_order, v_account, 'payment_mismatch_clear', c.id, NULL, NULL, NULL, p_note,
                                          pg_catalog.json_build_object('state', c.state, 'mismatch_reason', c.mismatch_reason));
  PERFORM business.command_finish(v_account, 'payment.mismatch_clear', p_collection::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- Owner-internal: what re-reserving failed operation o may take now (the
-- caller holds the locks): min(its amount, the collection's room, what the
-- goal still needs), 0 while the collection is blocked.
CREATE FUNCTION business.refund_remainder_amount(p_op uuid)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN b.unresolved OR b.quarantined
                   OR EXISTS (SELECT 1 FROM business.refund_operation x
                              WHERE x.collection_id = o.collection_id AND x.mismatch_reason IS NOT NULL) THEN 0
              ELSE GREATEST(0, LEAST(o.amount_minor, b.room_minor, business.refund_goal_remaining(o.order_id))) END
  FROM business.refund_operation o CROSS JOIN LATERAL business.refund_balance(o.collection_id) b
  WHERE o.id = p_op;
$$;

-- API (chef): re-reserve a failed provider refund's remainder on its
-- collection (see the header, 4). Returns the decision.
CREATE FUNCTION business.refund_remainder_reserve(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                                  p_expected_version integer, p_operation uuid, p_note text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  o business.refund_operation%ROWTYPE;
  v_g business.refund_goal%ROWTYPE;
  v_amount bigint;
  v_op uuid;
  v_id uuid;
BEGIN
  IF p_order IS NULL OR p_operation IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.refund_note_check(p_note, false);
  v_prior := business.command_claim(v_account, 'refund.remainder_reserve', p_operation::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM business.refund_chef_admit(p_order, p_expected_version);
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = p_operation AND ro.order_id = p_order;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'refund operation not found' USING ERRCODE = 'GK404';
  END IF;
  IF o.state <> 'failed' OR o.channel <> 'provider'
     OR EXISTS (SELECT 1 FROM business.refund_operation x
                WHERE x.collection_id = o.collection_id AND x.id <> o.id AND x.state <> 'cancelled'
                  AND (x.created_at, x.id) > (o.created_at, o.id)) THEN
    -- Not the latest decided operation of its collection: its remainder was
    -- already re-reserved (or superseded).
    IF EXISTS (SELECT 1 FROM business.refund_operation x
               WHERE x.collection_id = o.collection_id
                 AND x.state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action')) THEN
      RAISE EXCEPTION 'refund_in_progress' USING ERRCODE = 'GK409';
    END IF;
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  v_amount := business.refund_remainder_amount(o.id);
  IF v_g.id IS NULL OR coalesce(v_amount, 0) <= 0 THEN
    RAISE EXCEPTION 'no_refund_pending' USING ERRCODE = 'GK409';
  END IF;
  v_op := business.refund_reserve_one(p_order, o.collection_id, v_g, v_amount);
  v_id := business.refund_decision_record(p_order, v_account, 'remainder_reserved', o.collection_id, o.id, NULL, v_op,
                                          p_note, pg_catalog.json_build_object('amount_minor', v_amount,
                                                                               'goal_version', v_g.version));
  PERFORM business.command_finish(v_account, 'refund.remainder_reserve', p_operation::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- API (chef or technical support): the manager attention list (see the
-- header, 5). Volatile: a technical support read is audited.
CREATE FUNCTION business.refund_attention_list(p_actor uuid)
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
    WHERE j.state = 'attention'
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

REVOKE ALL ON TABLE business.refund_recovery_decision FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_manager_admit(uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_note_check(text, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_decision_record(uuid, uuid, text, uuid, uuid, uuid, uuid, text, json) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_observation_match(uuid, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_observation_resolve(uuid, text, text, uuid, integer, uuid, text, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_operation_decide(uuid, text, text, uuid, integer, uuid, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_mismatch_clear(uuid, text, text, uuid, integer, uuid, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_mismatch_clear(uuid, text, text, uuid, integer, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_remainder_amount(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_remainder_reserve(uuid, text, text, uuid, integer, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.refund_attention_list(uuid) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.refund_observation_resolve(uuid, text, text, uuid, integer, uuid, text, uuid, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.refund_operation_decide(uuid, text, text, uuid, integer, uuid, text, text, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.refund_mismatch_clear(uuid, text, text, uuid, integer, uuid, text, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.payment_mismatch_clear(uuid, text, text, uuid, integer, uuid, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.refund_remainder_reserve(uuid, text, text, uuid, integer, uuid, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.refund_attention_list(uuid) TO gk_api;
