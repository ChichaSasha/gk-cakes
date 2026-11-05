-- GK-026 B2 part 2b: the Q5 manual alternative payout (contracts r6.5 K05
-- "Q5 accepted: chef may arrange a verified alternative payout only after
-- definitive provider unavailability, with evidence. Reuse refund_operation
-- and the same collection/order goal: channel provider or
-- manual_alternative, never a parallel unbounded ledger. Before showing an
-- instruction to pay externally, lock the order/collection, reconcile all
-- known outcomes, prove no prior unresolved operation, reserve the whole
-- remaining allocation, and protect the prepared operation under K11 ...
-- A submitted/manual transfer is pending, not succeeded; unknown transfer
-- outcome blocks every retry/channel until definitive reconciliation.
-- Require unique bank/transfer evidence identity and verified successful
-- outcome before reducing liability once. Protect outcome under K11 before
-- publishing success"; K11; K00).
--
-- Eligibility (definitive provider unavailability). The collection's latest
-- refund operation is a failed provider refund (Stripe's definitive failure
-- or rejection, or a manager's complete-list decision) without
-- contradicting evidence, nothing is unresolved on it (409
-- refund_in_progress), the order needs no reconciliation (409
-- reconciliation_required); otherwise 409 manual_payout_not_eligible. The
-- chef also names the unavailability evidence and the verified recipient
-- evidence by REFERENCE only (1-200 visible characters, no run of six or
-- more digits: never raw bank details; 422 evidence_ref_invalid).
--
-- Preview (business.manual_payout_preview, chef). Immutable, 10 minutes,
-- for the order version: the whole remaining allocation of that failed
-- operation = min(its amount, the collection's room, what the goal still
-- needs) (409 no_refund_pending at 0), GBP, both references, a SHA-256 of
-- its body. An unexpired identical preview is returned again. Any change
-- of recipient reference or amount is a new preview.
--
-- Reservation (business.manual_payout_request -> K11 order command
-- 'manual_payout'). With expected_version, preview_id + preview_hash and an
-- Idempotency-Key; the same checks again at the receipt instant (a
-- changed amount, version or expired preview is 409 preview_stale). The
-- commit (after the prepared envelope is acknowledged) re-checks everything
-- under the order and collection locks and reserves one refund operation
-- with channel manual_alternative for the whole amount under the current
-- goal version (no dispatch job: the provider worker never touches it, and
-- the one-unresolved-per-collection index blocks provider refunds and other
-- manual payouts), or refuses it (order_changed, not_eligible,
-- amount_changed). The transfer instructions (amount, currency, recipient
-- evidence reference, payment reference = the operation ID) are shown only
-- once that command's outcome is secured (business.manual_payout_list).
--
-- Outcome (business.manual_payout_outcome_request -> K11 order command
-- 'manual_payout_outcome'): submitted (reserved -> pending), succeeded
-- (pending or unknown -> succeeded; the transfer evidence identity unique
-- across all manual payouts (409 duplicate), the evidence reference, the
-- exact amount and GBP), failed (pending or unknown -> failed, with
-- evidence: the remainder reopens, never re-sent automatically), unknown
-- (pending -> unknown: blocks every retry and channel on the collection),
-- withdrawn (reserved -> cancelled: provably nothing was paid). Recorded in
-- business.manual_payout_event. A success reduces the liability once (the
-- operation's S); until its command is secured, order_get reports it as
-- in progress, never returned (business.manual_payout_unpublished). No
-- Stripe refund ID is invented: refund_operation's provider checks are
-- relaxed for this channel only.
--
-- Deferred: the UI, emails/notifications, protected evidence storage
-- (references point to evidence kept outside this system), foreign-currency
-- payouts (GBP only: liability stays open).
--
-- Expand-only (K09): new tables and functions, broader CHECK constraints,
-- functions replaced with unchanged signatures. Lock order (K00): the
-- order, its collections by ID, the recovery operation, the goal, refund
-- operations.

SET LOCAL ROLE gk_owner;

ALTER TABLE business.refund_operation DROP CONSTRAINT refund_operation_sent;
ALTER TABLE business.refund_operation
  ADD CONSTRAINT refund_operation_sent CHECK ((state <> 'reserved' OR dispatch_started_at IS NULL)
                                              AND (state IN ('reserved', 'failed', 'cancelled')
                                                   OR dispatch_started_at IS NOT NULL OR channel = 'manual_alternative'));
ALTER TABLE business.refund_operation DROP CONSTRAINT refund_operation_succeeded;
ALTER TABLE business.refund_operation
  ADD CONSTRAINT refund_operation_succeeded CHECK (state <> 'succeeded' OR provider_refund_id IS NOT NULL
                                                   OR channel = 'manual_alternative');
ALTER TABLE business.refund_operation
  ADD CONSTRAINT refund_operation_manual CHECK (channel <> 'manual_alternative'
                                                OR (provider_refund_id IS NULL AND payload IS NULL
                                                    AND dispatch_started_at IS NULL));

ALTER TABLE business.order_command DROP CONSTRAINT order_command_command_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_command_check CHECK (command IN ('free_confirm', 'paid_confirm', 'cancel', 'late_payment',
                                                                'manual_payout', 'manual_payout_outcome'));
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_manual_payout CHECK (command NOT IN ('manual_payout', 'manual_payout_outcome')
                                                    OR actor_kind = 'staff');

-- Owner-internal: whether p_ref is an evidence reference (never raw bank
-- details: no run of six or more digits).
CREATE FUNCTION business.evidence_ref_valid(p_ref text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_ref IS NOT NULL AND p_ref ~ '^[\x21-\x7e]([\x20-\x7e]{0,198}[\x21-\x7e])?$' AND p_ref !~ '[0-9]{6,}';
$$;

CREATE TABLE business.manual_payout_preview (
  id                    uuid PRIMARY KEY,
  order_id              uuid NOT NULL REFERENCES business.orders (id),
  order_version         integer NOT NULL CHECK (order_version >= 1),
  collection_id         uuid NOT NULL REFERENCES business.payment_collection (id),
  failed_operation_id   uuid NOT NULL REFERENCES business.refund_operation (id),
  unavailable_evidence_ref text NOT NULL CHECK (business.evidence_ref_valid(unavailable_evidence_ref)),
  recipient_evidence_ref   text NOT NULL CHECK (business.evidence_ref_valid(recipient_evidence_ref)),
  amount_minor          bigint NOT NULL CHECK (amount_minor > 0),
  currency              text NOT NULL DEFAULT 'GBP' CHECK (currency = 'GBP'),
  actor_account_id      uuid NOT NULL REFERENCES business.account (id),
  computed_at           timestamptz NOT NULL,
  expires_at            timestamptz NOT NULL,
  body_sha256           char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  created_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT manual_payout_preview_expiry CHECK (expires_at = computed_at + interval '10 minutes')
);
CREATE INDEX manual_payout_preview_order_idx ON business.manual_payout_preview (order_id, computed_at);
CREATE TRIGGER manual_payout_preview_immutable BEFORE UPDATE OR DELETE ON business.manual_payout_preview
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER manual_payout_preview_no_truncate BEFORE TRUNCATE ON business.manual_payout_preview
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- What each manual payout command asked for (immutable).
CREATE TABLE business.manual_payout_command (
  command_id          uuid PRIMARY KEY REFERENCES business.order_command (id),
  kind                text NOT NULL CHECK (kind IN ('reserve', 'outcome')),
  preview_id          uuid NULL REFERENCES business.manual_payout_preview (id),
  refund_operation_id uuid NULL REFERENCES business.refund_operation (id),
  outcome             text NULL CHECK (outcome IN ('submitted', 'succeeded', 'failed', 'unknown', 'withdrawn')),
  transfer_identity   text NULL CHECK (transfer_identity IS NULL OR business.evidence_ref_valid(transfer_identity)),
  evidence_ref        text NULL CHECK (evidence_ref IS NULL OR business.evidence_ref_valid(evidence_ref)),
  amount_minor        bigint NULL CHECK (amount_minor > 0),
  currency            text NULL CHECK (currency = 'GBP'),
  CONSTRAINT manual_payout_command_kind CHECK ((kind = 'reserve') = (preview_id IS NOT NULL)
                                               AND (kind = 'outcome') = (refund_operation_id IS NOT NULL)
                                               AND (kind = 'outcome') = (outcome IS NOT NULL)
                                               AND (outcome = 'succeeded') = (transfer_identity IS NOT NULL)
                                               AND (outcome IS DISTINCT FROM 'succeeded'
                                                    OR (amount_minor IS NOT NULL AND currency IS NOT NULL
                                                        AND evidence_ref IS NOT NULL))
                                               AND (outcome IS DISTINCT FROM 'failed' OR evidence_ref IS NOT NULL))
);
CREATE TRIGGER manual_payout_command_immutable BEFORE UPDATE OR DELETE ON business.manual_payout_command
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER manual_payout_command_no_truncate BEFORE TRUNCATE ON business.manual_payout_command
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- One reserved manual payout (its refund operation) and its recorded
-- evidence; changed only by the outcome commit.
CREATE TABLE business.manual_payout (
  refund_operation_id uuid PRIMARY KEY REFERENCES business.refund_operation (id),
  order_id            uuid NOT NULL REFERENCES business.orders (id),
  collection_id       uuid NOT NULL REFERENCES business.payment_collection (id),
  preview_id          uuid NOT NULL REFERENCES business.manual_payout_preview (id),
  reserve_command_id  uuid NOT NULL UNIQUE REFERENCES business.order_command (id),
  failed_operation_id uuid NOT NULL REFERENCES business.refund_operation (id),
  amount_minor        bigint NOT NULL CHECK (amount_minor > 0),
  currency            text NOT NULL DEFAULT 'GBP' CHECK (currency = 'GBP'),
  submitted_at        timestamptz NULL,
  -- The unique bank/transfer evidence identity of a verified success.
  transfer_identity   text NULL UNIQUE CHECK (transfer_identity IS NULL OR business.evidence_ref_valid(transfer_identity)),
  evidence_ref        text NULL CHECK (evidence_ref IS NULL OR business.evidence_ref_valid(evidence_ref)),
  decided_at          timestamptz NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX manual_payout_order_idx ON business.manual_payout (order_id, created_at);

-- Every recorded outcome, in order (immutable).
CREATE TABLE business.manual_payout_event (
  id                  uuid PRIMARY KEY,
  refund_operation_id uuid NOT NULL REFERENCES business.refund_operation (id),
  command_id          uuid NOT NULL UNIQUE REFERENCES business.order_command (id),
  outcome             text NOT NULL CHECK (outcome IN ('submitted', 'succeeded', 'failed', 'unknown', 'withdrawn')),
  prior_state         text NOT NULL,
  recorded_by         uuid NOT NULL REFERENCES business.account (id),
  received_at         timestamptz NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER manual_payout_event_immutable BEFORE UPDATE OR DELETE ON business.manual_payout_event
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER manual_payout_event_no_truncate BEFORE TRUNCATE ON business.manual_payout_event
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: the eligibility of a manual payout from collection
-- p_collection of order p_order now (the caller holds the order and
-- collection locks): the failed operation whose remainder it repays and
-- the whole remaining allocation, or the refusal (see the header).
CREATE FUNCTION business.manual_payout_check(p_order uuid, p_collection uuid)
RETURNS TABLE (failed_operation_id uuid, amount_minor bigint, refusal text)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  o business.refund_operation%ROWTYPE;
  v_amount bigint;
BEGIN
  IF business.refund_reconciliation_required(p_order) THEN
    RETURN QUERY SELECT NULL::uuid, 0::bigint, 'reconciliation_required'::text;
    RETURN;
  END IF;
  IF EXISTS (SELECT 1 FROM business.refund_operation x
             WHERE x.collection_id = p_collection
               AND x.state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action')) THEN
    RETURN QUERY SELECT NULL::uuid, 0::bigint, 'refund_in_progress'::text;
    RETURN;
  END IF;
  -- The latest decided operation (a withdrawn manual payout changes
  -- nothing) must be a definitive failure, and the provider must have
  -- failed definitively on this collection.
  SELECT * INTO o FROM business.refund_operation x
  WHERE x.collection_id = p_collection AND x.state <> 'cancelled'
  ORDER BY x.created_at DESC, x.id DESC LIMIT 1;
  IF o.id IS NULL OR o.state <> 'failed' OR o.mismatch_reason IS NOT NULL
     OR NOT EXISTS (SELECT 1 FROM business.refund_operation x
                    WHERE x.collection_id = p_collection AND x.channel = 'provider' AND x.state = 'failed'
                      AND x.mismatch_reason IS NULL)
     OR NOT EXISTS (SELECT 1 FROM business.payment_collection pc WHERE pc.id = p_collection AND pc.state = 'succeeded') THEN
    RETURN QUERY SELECT NULL::uuid, 0::bigint, 'manual_payout_not_eligible'::text;
    RETURN;
  END IF;
  v_amount := business.refund_remainder_amount(o.id);
  IF coalesce(v_amount, 0) <= 0 THEN
    RETURN QUERY SELECT o.id, 0::bigint, 'no_refund_pending'::text;
    RETURN;
  END IF;
  RETURN QUERY SELECT o.id, v_amount, NULL::text;
END;
$$;

-- Owner-internal: the canonical body of a preview (its hash covers it).
CREATE FUNCTION business.manual_payout_preview_body(p business.manual_payout_preview)
RETURNS json
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.json_build_object(
    'preview_id', p.id, 'order_id', p.order_id, 'order_version', p.order_version, 'collection_id', p.collection_id,
    'failed_refund_operation_id', p.failed_operation_id, 'amount_minor', p.amount_minor, 'currency', p.currency,
    'unavailable_evidence_ref', p.unavailable_evidence_ref, 'recipient_evidence_ref', p.recipient_evidence_ref,
    'computed_at', pg_catalog.to_char(p.computed_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'expires_at', pg_catalog.to_char(p.expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'));
$$;

-- API (chef): preview a manual payout (see the header).
CREATE FUNCTION business.manual_payout_preview(p_actor uuid, p_order uuid, p_collection uuid, p_unavailable_ref text,
                                               p_recipient_ref text, p_now timestamptz)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_o business.orders%ROWTYPE;
  v_check record;
  p business.manual_payout_preview%ROWTYPE;
  v_body text;
BEGIN
  IF p_now IS NULL OR p_order IS NULL OR p_collection IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF NOT business.evidence_ref_valid(p_unavailable_ref) OR NOT business.evidence_ref_valid(p_recipient_ref) THEN
    RAISE EXCEPTION 'evidence_ref_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  IF NOT EXISTS (SELECT 1 FROM business.payment_collection pc WHERE pc.id = p_collection AND pc.order_id = p_order) THEN
    RAISE EXCEPTION 'collection not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT * INTO v_check FROM business.manual_payout_check(p_order, p_collection);
  IF v_check.refusal IS NOT NULL THEN
    RAISE EXCEPTION '%', v_check.refusal USING ERRCODE = 'GK409';
  END IF;
  SELECT * INTO p FROM business.manual_payout_preview x
  WHERE x.order_id = p_order AND x.actor_account_id = v_account AND x.order_version = v_o.version
    AND x.collection_id = p_collection AND x.failed_operation_id = v_check.failed_operation_id
    AND x.amount_minor = v_check.amount_minor AND x.unavailable_evidence_ref = p_unavailable_ref
    AND x.recipient_evidence_ref = p_recipient_ref AND x.computed_at <= p_now AND x.expires_at > p_now
  ORDER BY x.computed_at DESC LIMIT 1;
  IF NOT FOUND THEN
    p.id := pg_catalog.gen_random_uuid();
    p.order_id := p_order;
    p.order_version := v_o.version;
    p.collection_id := p_collection;
    p.failed_operation_id := v_check.failed_operation_id;
    p.unavailable_evidence_ref := p_unavailable_ref;
    p.recipient_evidence_ref := p_recipient_ref;
    p.amount_minor := v_check.amount_minor;
    p.currency := 'GBP';
    p.actor_account_id := v_account;
    p.computed_at := pg_catalog.date_trunc('milliseconds', p_now);
    p.expires_at := p.computed_at + interval '10 minutes';
    v_body := business.manual_payout_preview_body(p)::text;
    p.body_sha256 := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_body, 'UTF8')), 'hex');
    INSERT INTO business.manual_payout_preview (id, order_id, order_version, collection_id, failed_operation_id,
                                                unavailable_evidence_ref, recipient_evidence_ref, amount_minor, currency,
                                                actor_account_id, computed_at, expires_at, body_sha256)
    VALUES (p.id, p.order_id, p.order_version, p.collection_id, p.failed_operation_id, p.unavailable_evidence_ref,
            p.recipient_evidence_ref, p.amount_minor, p.currency, p.actor_account_id, p.computed_at, p.expires_at,
            p.body_sha256);
    PERFORM business.audit_record('staff', v_account, 'refund.manual_payout_previewed', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('preview_id', p.id, 'collection_id', p_collection,
                                                               'amount_minor', p.amount_minor));
  END IF;
  RETURN (business.manual_payout_preview_body(p)::jsonb || pg_catalog.jsonb_build_object('preview_hash', p.body_sha256))::json;
END;
$$;

-- Owner-internal: the common admission of the two manual payout command
-- requests (the caller locked the order and validated its input): replay,
-- one command at a time, the version (not yet the recovery reservation or
-- the command row. Returns the earlier command of the same key (a replay),
-- or NULL when the caller continues with its own checks.
CREATE FUNCTION business.manual_payout_command_admit(v_o business.orders, p_account uuid, p_command text, p_key text,
                                                     p_body_sha256 text, p_expected_version integer)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_prior business.order_command%ROWTYPE;
BEGIN
  SELECT * INTO v_prior FROM business.order_command c
  WHERE c.order_id = v_o.id AND c.command = p_command AND c.idempotency_key = p_key AND c.actor_account_id = p_account;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN v_prior.id;
  END IF;
  IF EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
             WHERE c.order_id = v_o.id AND r.state <> 'secured')
     OR v_o.recovery_state IN ('confirmation_pending', 'cancellation_pending') THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  RETURN NULL;
END;
$$;

-- Owner-internal: reserve the K11 recovery operation of a manual payout
-- command and record the command (the caller holds the order lock).
CREATE FUNCTION business.manual_payout_command_record(v_o business.orders, p_account uuid, p_command text, p_key text,
                                                      p_body_sha256 text, p_env text, p_now timestamptz, p_detail json)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid := pg_catalog.gen_random_uuid();
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
BEGIN
  v_key := 'recovery/' || p_env || '/op/' || v_o.id::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.' || p_command, 'operation_id', v_id, 'order_id', v_o.id,
    'prior_version', v_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', 'staff', 'account_id', p_account),
    'manual_payout', p_detail,
    'order', business.order_recovery_snapshot(v_o.id))::text;
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_payload, 'UTF8')), 'hex');
  SELECT r.result INTO v_reserve
  FROM business.recovery_reserve(v_id, 'command', v_o.id, v_o.version, v_hash, v_key, NULL) r;
  IF v_reserve = 'busy' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  ELSIF v_reserve <> 'reserved' THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  v_job := business.job_enqueue(v_id, NULL);
  INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, idempotency_key, body_sha256,
                                      prior_version, received_at, payload, payload_sha256, envelope_key, job_id)
  VALUES (v_id, v_o.id, p_command, 'staff', p_account, p_key, p_body_sha256, v_o.version, p_now, v_payload, v_hash,
          v_key, v_job);
  RETURN v_id;
END;
$$;

-- API (chef): reserve a previewed manual payout as a K11 order command
-- (see the header). Returns the command and whether it is a replay.
CREATE FUNCTION business.manual_payout_request(p_actor uuid, p_order uuid, p_key text, p_body_sha256 text,
                                               p_expected_version integer, p_preview uuid, p_preview_hash text,
                                               p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_o business.orders%ROWTYPE;
  v_prior uuid;
  p business.manual_payout_preview%ROWTYPE;
  v_check record;
  v_id uuid;
BEGIN
  IF p_now IS NULL OR p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_preview IS NULL OR p_preview_hash IS NULL THEN
    RAISE EXCEPTION 'payout_preview_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v_prior := business.manual_payout_command_admit(v_o, v_account, 'manual_payout', p_key, p_body_sha256, p_expected_version);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  SELECT * INTO p FROM business.manual_payout_preview x
  WHERE x.id = p_preview AND x.order_id = p_order AND x.actor_account_id = v_account;
  IF NOT FOUND OR p.body_sha256 <> p_preview_hash THEN
    RAISE EXCEPTION 'payout_preview_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p.order_version <> v_o.version OR p_now >= p.expires_at THEN
    RAISE EXCEPTION 'preview_stale' USING ERRCODE = 'GK409';
  END IF;
  SELECT * INTO v_check FROM business.manual_payout_check(p_order, p.collection_id);
  IF v_check.refusal IS NOT NULL THEN
    RAISE EXCEPTION '%', v_check.refusal USING ERRCODE = 'GK409';
  END IF;
  IF v_check.amount_minor <> p.amount_minor OR v_check.failed_operation_id <> p.failed_operation_id THEN
    RAISE EXCEPTION 'preview_stale' USING ERRCODE = 'GK409';
  END IF;
  v_id := business.manual_payout_command_record(v_o, v_account, 'manual_payout', p_key, p_body_sha256, p_env, p_now,
                                                business.manual_payout_preview_body(p));
  INSERT INTO business.manual_payout_command (command_id, kind, preview_id) VALUES (v_id, 'reserve', p.id);
  PERFORM business.audit_record('staff', v_account, 'refund.manual_payout_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id, 'preview_id', p.id,
                                                             'amount_minor', p.amount_minor));
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- Owner-internal: a refused manual payout command (version + 1, audited).
CREATE FUNCTION business.manual_payout_refuse(v business.order_command, p_reason text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
  PERFORM business.audit_record('system', NULL, 'refund.manual_payout_refused', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'command', v.command, 'reason', p_reason));
  RETURN pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', p_reason);
END;
$$;

-- Owner-internal: the domain commit of a manual payout reservation (the
-- caller, business.order_command_apply, holds the order and collection
-- locks and checked the freeze).
CREATE FUNCTION business.manual_payout_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  p business.manual_payout_preview%ROWTYPE;
  c business.payment_collection%ROWTYPE;
  v_g business.refund_goal%ROWTYPE;
  v_check record;
  v_op uuid := pg_catalog.gen_random_uuid();
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  SELECT x.* INTO p FROM business.manual_payout_preview x JOIN business.manual_payout_command mc ON mc.preview_id = x.id
  WHERE mc.command_id = v.id;
  IF v_o.version <> v.prior_version THEN
    RETURN business.manual_payout_refuse(v, 'order_changed');
  END IF;
  SELECT * INTO v_check FROM business.manual_payout_check(v.order_id, p.collection_id);
  IF v_check.refusal IS NOT NULL THEN
    RETURN business.manual_payout_refuse(v, 'not_eligible');
  END IF;
  IF v_check.amount_minor <> p.amount_minor OR v_check.failed_operation_id <> p.failed_operation_id THEN
    RETURN business.manual_payout_refuse(v, 'amount_changed');
  END IF;
  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = v.order_id FOR UPDATE;
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = p.collection_id;
  INSERT INTO business.refund_operation (id, order_id, collection_id, goal_id, goal_version, channel, amount_minor, environment,
                                         provider_account_id, livemode, payment_intent_id)
  VALUES (v_op, v.order_id, c.id, v_g.id, v_g.version, 'manual_alternative', p.amount_minor, c.environment,
          c.provider_account_id, c.livemode, c.payment_intent_id);
  INSERT INTO business.manual_payout (refund_operation_id, order_id, collection_id, preview_id, reserve_command_id,
                                      failed_operation_id, amount_minor)
  VALUES (v_op, v.order_id, c.id, p.id, v.id, p.failed_operation_id, p.amount_minor);
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
  PERFORM business.audit_record('staff', v.actor_account_id, 'refund.manual_payout_reserved', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'refund_operation_id', v_op,
                                                             'collection_id', c.id, 'amount_minor', p.amount_minor,
                                                             'goal_version', v_g.version));
  RETURN pg_catalog.format('{"status":"reserved","evidence":{"order_version":%s,"refund_operation_id":"%s","amount_minor":%s}}',
                           v_o.version + 1, v_op, p.amount_minor);
END;
$$;

-- Owner-internal: whether manual payout outcome p_outcome may follow state
-- p_state.
CREATE FUNCTION business.manual_payout_transition(p_state text, p_outcome text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE p_outcome WHEN 'submitted' THEN p_state = 'reserved'
                        WHEN 'withdrawn' THEN p_state = 'reserved'
                        WHEN 'unknown' THEN p_state = 'pending'
                        WHEN 'succeeded' THEN p_state IN ('pending', 'unknown')
                        WHEN 'failed' THEN p_state IN ('pending', 'unknown')
                        ELSE false END;
$$;

-- API (chef): record a manual payout outcome as a K11 order command (see
-- the header). Returns the command and whether it is a replay.
CREATE FUNCTION business.manual_payout_outcome_request(p_actor uuid, p_order uuid, p_key text, p_body_sha256 text,
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
     OR (p_outcome IN ('succeeded', 'failed') AND p_evidence_ref IS NULL) THEN
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

-- Owner-internal: the domain commit of a manual payout outcome (the
-- caller holds the order and collection locks and checked the freeze).
CREATE FUNCTION business.manual_payout_outcome_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  mc business.manual_payout_command%ROWTYPE;
  o business.refund_operation%ROWTYPE;
  v_state text;
BEGIN
  SELECT * INTO v_o FROM business.orders x WHERE x.id = v.order_id FOR UPDATE;
  SELECT * INTO mc FROM business.manual_payout_command x WHERE x.command_id = v.id;
  IF v_o.version <> v.prior_version THEN
    RETURN business.manual_payout_refuse(v, 'order_changed');
  END IF;
  SELECT * INTO o FROM business.refund_operation ro WHERE ro.id = mc.refund_operation_id FOR UPDATE;
  IF NOT business.manual_payout_transition(o.state, mc.outcome) THEN
    RETURN business.manual_payout_refuse(v, 'state_changed');
  END IF;
  IF mc.outcome = 'succeeded'
     AND (mc.amount_minor <> o.amount_minor
          OR EXISTS (SELECT 1 FROM business.manual_payout m WHERE m.transfer_identity = mc.transfer_identity)) THEN
    RETURN business.manual_payout_refuse(v, 'duplicate_evidence');
  END IF;
  v_state := CASE mc.outcome WHEN 'submitted' THEN 'pending' WHEN 'withdrawn' THEN 'cancelled' ELSE mc.outcome END;
  UPDATE business.refund_operation ro
  SET state = v_state,
      failure_reason = CASE WHEN v_state = 'failed' THEN 'manual_transfer_failed' END,
      settled_at = CASE WHEN v_state IN ('succeeded', 'failed', 'cancelled') THEN pg_catalog.now() END,
      last_observed_at = pg_catalog.now(), version = ro.version + 1, updated_at = pg_catalog.now()
  WHERE ro.id = o.id;
  UPDATE business.manual_payout m
  SET submitted_at = CASE WHEN mc.outcome = 'submitted' THEN v.received_at ELSE m.submitted_at END,
      transfer_identity = coalesce(mc.transfer_identity, m.transfer_identity),
      evidence_ref = coalesce(mc.evidence_ref, m.evidence_ref),
      decided_at = CASE WHEN v_state IN ('succeeded', 'failed', 'cancelled') THEN v.received_at ELSE m.decided_at END,
      updated_at = pg_catalog.now()
  WHERE m.refund_operation_id = o.id;
  INSERT INTO business.manual_payout_event (id, refund_operation_id, command_id, outcome, prior_state, recorded_by, received_at)
  VALUES (pg_catalog.gen_random_uuid(), o.id, v.id, mc.outcome, o.state, v.actor_account_id, v.received_at);
  UPDATE business.orders x SET version = x.version + 1, updated_at = pg_catalog.now() WHERE x.id = v.order_id;
  PERFORM business.audit_record('staff', v.actor_account_id, 'refund.manual_payout_' || mc.outcome, 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'refund_operation_id', o.id,
                                                             'amount_minor', o.amount_minor, 'prior_state', o.state));
  RETURN pg_catalog.format('{"status":"recorded","evidence":{"order_version":%s,"refund_operation_id":"%s","outcome":"%s"}}',
                           v_o.version + 1, o.id, mc.outcome);
END;
$$;

-- Owner-internal: manual payout successes of the order whose outcome is
-- not yet secured under K11 (never published as returned).
CREATE FUNCTION business.manual_payout_unpublished(p_order uuid)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(sum(ro.amount_minor), 0)::bigint
  FROM business.refund_operation ro
  WHERE ro.order_id = p_order AND ro.channel = 'manual_alternative' AND ro.state = 'succeeded'
    AND EXISTS (SELECT 1 FROM business.manual_payout_command mc JOIN business.recovery_operation r ON r.id = mc.command_id
                WHERE mc.refund_operation_id = ro.id AND mc.outcome = 'succeeded' AND r.state <> 'secured');
$$;

-- API (chef): the order's manual payouts. The transfer instructions appear
-- only once the reservation's K11 outcome is secured.
CREATE FUNCTION business.manual_payout_list(p_actor uuid, p_order uuid)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_items json;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM business.orders o WHERE o.id = p_order) THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT coalesce(pg_catalog.json_agg(pg_catalog.json_build_object(
           'refund_operation_id', m.refund_operation_id, 'collection_id', m.collection_id,
           'state', CASE WHEN r.state <> 'secured' THEN 'protecting' ELSE ro.state END,
           'amount_minor', m.amount_minor, 'currency', m.currency,
           'unavailable_evidence_ref', p.unavailable_evidence_ref,
           'instructions', CASE WHEN r.state = 'secured' THEN pg_catalog.json_build_object(
                                  'amount_minor', m.amount_minor, 'currency', m.currency,
                                  'recipient_evidence_ref', p.recipient_evidence_ref,
                                  'payment_reference', m.refund_operation_id) END,
           'submitted_at', m.submitted_at, 'transfer_identity', m.transfer_identity, 'decided_at', m.decided_at,
           'outcome_pending', EXISTS (SELECT 1 FROM business.manual_payout_command mc
                                      JOIN business.recovery_operation rr ON rr.id = mc.command_id
                                      WHERE mc.refund_operation_id = m.refund_operation_id AND rr.state <> 'secured'))
         ORDER BY m.created_at), '[]'::json)
  INTO v_items
  FROM business.manual_payout m
  JOIN business.refund_operation ro ON ro.id = m.refund_operation_id
  JOIN business.recovery_operation r ON r.id = m.reserve_command_id
  JOIN business.manual_payout_preview p ON p.id = m.preview_id
  WHERE m.order_id = p_order;
  RETURN v_items;
END;
$$;

-- As in 0022, plus the manual payout commands (their commits:
-- business.manual_payout_commit, business.manual_payout_outcome_commit).
CREATE OR REPLACE FUNCTION business.order_command_apply(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS TABLE (op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_o business.orders%ROWTYPE;
  v_c business.payment_collection%ROWTYPE;
  v_r business.recovery_operation%ROWTYPE;
  v_expires timestamptz;
  v_reason text;
  v_lot record;
  v_outcome text;
  v_lifecycle text;
  v_state text;
  v_frozen boolean;
BEGIN
  -- Lock order (K00): the order, its collections by ID (all of them: a
  -- refused confirmation or a cancellation may record the return across
  -- them), then its recovery operation, then the goal and refund
  -- operations, then the day and lots inside the engines.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  IF v.command IN ('paid_confirm', 'cancel', 'late_payment', 'manual_payout', 'manual_payout_outcome') THEN
    PERFORM 1 FROM business.payment_collection c WHERE c.order_id = v.order_id ORDER BY c.id FOR UPDATE;
    SELECT * INTO v_c FROM business.payment_collection c WHERE c.id = v.collection_id;
  END IF;
  SELECT * INTO v_r FROM business.recovery_operation r WHERE r.id = v.id FOR UPDATE;
  IF v_r.state IN ('committed', 'secured') THEN
    RETURN QUERY SELECT v_r.state, v_r.outcome, v_r.outcome_hash::text;
    RETURN;
  ELSIF v_r.state <> 'prepared' THEN
    RAISE EXCEPTION 'recovery operation % is %, not prepared', v.id, v_r.state USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- A restore freeze stops the commit even if it landed after the worker's
  -- last check: the share lock makes a concurrent freeze wait for this
  -- commit, or this commit see the freeze.
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'frozen' USING ERRCODE = 'GK002';
  END IF;

  IF v.command IN ('cancel', 'late_payment', 'manual_payout', 'manual_payout_outcome') THEN
    v_outcome := CASE v.command WHEN 'cancel' THEN business.order_cancel_commit(v)
                                WHEN 'late_payment' THEN business.order_late_payment_commit(v)
                                WHEN 'manual_payout' THEN business.manual_payout_commit(v)
                                ELSE business.manual_payout_outcome_commit(v) END;
    SELECT c.op_state INTO v_state
    FROM business.recovery_mark_committed(v.id, v_outcome::json,
           pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_outcome, 'UTF8')), 'hex'), false) c;
    SELECT * INTO v_r FROM business.recovery_operation r WHERE r.id = v.id;
    RETURN QUERY SELECT v_r.state, v_r.outcome, v_r.outcome_hash::text;
    RETURN;
  END IF;

  IF v.command = 'paid_confirm' THEN
    IF v_o.version <> v.prior_version OR v_o.lifecycle <> 'awaiting_payment' OR v_o.recovery_state <> 'prepared'
       OR v_o.payment_requirement <> 'full' OR v_c.state IS DISTINCT FROM 'succeeded' THEN
      v_reason := 'order_changed';
    ELSIF NOT business.order_holds_intact(v.order_id) THEN
      v_reason := 'holds_released';
    END IF;
  ELSE
    SELECT q.expires_at INTO v_expires FROM business.checkout_quote q WHERE q.id = v_o.quote_id;
    IF v_o.version <> v.prior_version OR v_o.lifecycle <> 'awaiting_payment' OR v_o.recovery_state <> 'prepared'
       OR v_o.payment_requirement <> 'none' THEN
      v_reason := 'order_changed';
    ELSIF NOT business.order_holds_intact(v.order_id) THEN
      v_reason := 'holds_released';
    ELSIF v.received_at >= v_expires OR v_o.original_handover_at - v.received_at < business.schedule_lead_time() THEN
      v_reason := 'window_closed';
    END IF;
  END IF;

  IF v_reason IS NULL THEN
    IF EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = v.order_id) THEN
      PERFORM business.capacity_commit(v.order_id, v_o.handover_day);
    END IF;
    FOR v_lot IN SELECT r.lot_id, r.quantity FROM business.order_lot_reservation r
                 WHERE r.order_id = v.order_id ORDER BY r.lot_id LOOP
      PERFORM business.lot_settle(v_lot.lot_id, v_lot.quantity, true);
      UPDATE business.order_lot_reservation r SET state = 'sold' WHERE r.order_id = v.order_id AND r.lot_id = v_lot.lot_id;
    END LOOP;
    UPDATE business.code_application c SET state = 'consumed', updated_at = pg_catalog.now()
    WHERE c.order_id = v.order_id;
    IF v.command = 'paid_confirm' THEN
      -- K04: 192 elapsed hours from the authoritative success to the
      -- handover, equality allowed; otherwise Q2 paid_awaiting_agreement.
      v_lifecycle := CASE WHEN v_o.original_handover_at - v.received_at >= business.schedule_lead_time()
                          THEN 'confirmed' ELSE 'paid_awaiting_agreement' END;
      UPDATE business.orders o
      SET lifecycle = v_lifecycle, paid_at = v.received_at, recovery_state = 'confirmation_pending',
          version = o.version + 1, updated_at = pg_catalog.now()
      WHERE o.id = v.order_id;
      v_outcome := pg_catalog.format('{"status":"%s","evidence":{"order_version":%s,"collection_id":"%s"}}',
                                     v_lifecycle, v_o.version + 1, v.collection_id);
      PERFORM business.audit_record('system', NULL,
                                    CASE WHEN v_lifecycle = 'confirmed' THEN 'order.paid_confirmed'
                                         ELSE 'order.paid_awaiting_agreement' END,
                                    'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id, 'collection_id', v.collection_id));
    ELSE
      UPDATE business.orders o
      SET lifecycle = 'confirmed', free_confirmed_at = v.received_at, recovery_state = 'confirmation_pending',
          version = o.version + 1, updated_at = pg_catalog.now()
      WHERE o.id = v.order_id;
      v_outcome := pg_catalog.format('{"status":"confirmed","evidence":{"order_version":%s}}', v_o.version + 1);
      PERFORM business.audit_record(CASE WHEN v.actor_account_id IS NULL THEN 'system' ELSE 'customer' END,
                                    v.actor_account_id, 'order.free_confirmed', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id));
    END IF;
  ELSE
    -- Nothing changes but the version: later commands reserve the next one.
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    IF v.command = 'paid_confirm' THEN
      v_outcome := pg_catalog.format('{"status":"refused","evidence":{"reason":"%s","collection_id":"%s"}}',
                                     v_reason, v.collection_id);
      PERFORM business.audit_record('system', NULL, 'order.paid_confirmation_refused', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason,
                                                                 'collection_id', v.collection_id));
      IF v_o.lifecycle = 'cancelled' THEN
        PERFORM business.refund_late_capture(v.order_id);
      END IF;
    ELSE
      v_outcome := pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', v_reason);
      PERFORM business.audit_record('system', NULL, 'order.free_confirmation_refused', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason));
    END IF;
  END IF;
  SELECT c.op_state INTO v_state
  FROM business.recovery_mark_committed(v.id, v_outcome::json,
         pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_outcome, 'UTF8')), 'hex'), false) c;
  SELECT * INTO v_r FROM business.recovery_operation r WHERE r.id = v.id;
  RETURN QUERY SELECT v_r.state, v_r.outcome, v_r.outcome_hash::text;
END;
$$;

-- As in 0023, except that a manual payout success is in progress until its
-- outcome is secured under K11 (business.manual_payout_unpublished).
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
           -- confirmation being secured"), is confirmation_pending; an
           -- unresolved or unsecured cancellation is cancellation_pending.
           'lifecycle', CASE WHEN o.recovery_state = 'confirmation_pending'
                               OR (o.lifecycle = 'awaiting_payment'
                                   AND EXISTS (SELECT 1 FROM business.order_command pcmd
                                               JOIN business.recovery_operation pr ON pr.id = pcmd.id
                                               WHERE pcmd.order_id = o.id AND pcmd.command = 'paid_confirm'
                                                 AND pr.state <> 'secured'))
                             THEN 'confirmation_pending'
                             WHEN o.recovery_state = 'cancellation_pending'
                               OR EXISTS (SELECT 1 FROM business.order_command ccmd
                                          JOIN business.recovery_operation cr ON cr.id = ccmd.id
                                          LEFT JOIN business.late_payment_decision cd ON cd.command_id = ccmd.id
                                          WHERE ccmd.order_id = o.id AND cr.state <> 'secured'
                                            AND (ccmd.command = 'cancel' OR cd.decision = 'full_return'))
                             THEN 'cancellation_pending'
                             ELSE o.lifecycle END,
           'cancel_reason', CASE WHEN o.recovery_state <> 'cancellation_pending' THEN o.cancel_reason END,
           'cancelled_at', CASE WHEN o.recovery_state <> 'cancellation_pending' THEN o.cancelled_at END,
           'payment_requirement', o.payment_requirement,
           'placed_at', o.placed_at, 'date', o.handover_day, 'fulfillment_mode', o.fulfillment_mode,
           'slot', pg_catalog.json_build_object('id', o.slot_id, 'starts_at', o.original_handover_at, 'ends_at', o.handover_ends_at),
           'full_refund_deadline_at', o.full_refund_deadline_at, 'policy_version_id', o.policy_version_id,
           'customer_full_name', o.customer_full_name, 'different_payer_name', o.different_payer_name,
           'contact_email', o.contact_email, 'subtotal_minor', o.subtotal_minor, 'discount_minor', o.discount_minor,
           'total_minor', o.total_minor, 'currency', o.currency,
           'paid_at', CASE WHEN o.recovery_state IN ('secured', 'cancellation_pending') THEN o.paid_at END,
           'free_confirmed_at', CASE WHEN o.recovery_state IN ('secured', 'cancellation_pending') THEN o.free_confirmed_at END,
           'free_confirmation', (SELECT pg_catalog.json_build_object(
                                   'state', CASE WHEN r.state <> 'secured' THEN 'processing'
                                                 ELSE r.outcome ->> 'status' END,
                                   'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                                   'requested_at', c.received_at)
                                 FROM business.order_command c
                                 JOIN business.recovery_operation r ON r.id = c.id
                                 WHERE c.order_id = o.id AND c.command = 'free_confirm'
                                 ORDER BY c.created_at DESC, c.id DESC LIMIT 1),
           'cancellation', (SELECT pg_catalog.json_build_object(
                              'state', CASE WHEN r.state <> 'secured' THEN 'processing'
                                            ELSE r.outcome ->> 'status' END,
                              'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                              'rate', CASE WHEN r.state = 'secured' THEN (r.outcome -> 'evidence' ->> 'rate')::integer END,
                              'amount_minor', CASE WHEN r.state = 'secured'
                                                   THEN (r.outcome -> 'evidence' ->> 'refund_target_minor')::bigint END,
                              'requested_by', CASE c.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
                              'requested_at', c.received_at)
                            FROM business.order_command c
                            JOIN business.recovery_operation r ON r.id = c.id
                            LEFT JOIN business.late_payment_decision d ON d.command_id = c.id
                            WHERE c.order_id = o.id AND (c.command = 'cancel' OR d.decision = 'full_return')
                            ORDER BY c.created_at DESC, c.id DESC LIMIT 1),
           'payment', (SELECT pg_catalog.json_build_object(
                         'id', pc.id,
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
           -- The return (K05): requested until Stripe has it, pending while
           -- it is being processed or checked, returned only when every
           -- penny of the goal succeeded; never returned while unresolved.
           -- Hidden while the cancellation that recorded it is not secured.
           'refund', (SELECT pg_catalog.json_build_object(
                        'state', CASE WHEN rf.attention THEN 'needs_attention'
                                      WHEN rf.succeeded - rf.unpublished >= g.target_minor THEN 'returned'
                                      WHEN rf.succeeded - rf.unpublished > 0 THEN 'partly_returned'
                                      WHEN rf.sent OR rf.unpublished > 0 THEN 'pending'
                                      ELSE 'requested' END,
                        'target_minor', g.target_minor, 'succeeded_minor', rf.succeeded - rf.unpublished,
                        'in_progress_minor', rf.reserved + rf.unpublished, 'currency', 'GBP', 'requested_at', g.created_at,
                        'goal_version', g.version, 'reason', g.reason, 'policy_rate', g.policy_rate,
                        'delivery_pending_minor', business.refund_delivery_pending(g.order_id, g.target_minor))
                      FROM business.refund_goal g
                      CROSS JOIN LATERAL (
                        SELECT coalesce(sum(b.succeeded_minor), 0) AS succeeded,
                               business.manual_payout_unpublished(g.order_id) AS unpublished,
                               coalesce(sum(b.reserved_minor), 0) AS reserved,
                               EXISTS (SELECT 1 FROM business.refund_operation ro
                                       WHERE ro.order_id = g.order_id AND ro.state IN ('submitting', 'pending', 'unknown'))
                                 AS sent,
                               (EXISTS (SELECT 1 FROM business.refund_operation ro
                                        WHERE ro.order_id = g.order_id
                                          AND (ro.state = 'requires_action' OR ro.mismatch_reason IS NOT NULL
                                               OR (ro.state IN ('submitting', 'pending', 'unknown')
                                                   AND EXISTS (SELECT 1 FROM business.recovery_operation r
                                                               WHERE r.id = ro.id AND r.state = 'reconcile'))))
                                OR coalesce(bool_or(b.quarantined), false)
                                OR (EXISTS (SELECT 1 FROM business.refund_operation ro
                                            WHERE ro.order_id = g.order_id AND ro.state = 'failed')
                                    AND coalesce(sum(b.succeeded_minor + b.reserved_minor), 0)
                                        + business.refund_delivery_pending(g.order_id, g.target_minor) < g.target_minor))
                                 AS attention
                        FROM business.payment_collection pc
                        CROSS JOIN LATERAL business.refund_balance(pc.id) b
                        WHERE pc.order_id = g.order_id) rf
                      WHERE g.order_id = o.id AND o.recovery_state <> 'cancellation_pending'),
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

REVOKE ALL ON TABLE business.manual_payout_preview FROM PUBLIC;
REVOKE ALL ON TABLE business.manual_payout_command FROM PUBLIC;
REVOKE ALL ON TABLE business.manual_payout FROM PUBLIC;
REVOKE ALL ON TABLE business.manual_payout_event FROM PUBLIC;
REVOKE ALL ON FUNCTION business.evidence_ref_valid(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_check(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_preview_body(business.manual_payout_preview) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_preview(uuid, uuid, uuid, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_command_admit(business.orders, uuid, text, text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_command_record(business.orders, uuid, text, text, text, text, timestamptz, json) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_request(uuid, uuid, text, text, integer, uuid, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_refuse(business.order_command, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_commit(business.order_command) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_transition(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_outcome_request(uuid, uuid, text, text, integer, uuid, text, text, text, bigint, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_outcome_commit(business.order_command) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_unpublished(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.manual_payout_list(uuid, uuid) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.manual_payout_preview(uuid, uuid, uuid, text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.manual_payout_request(uuid, uuid, text, text, integer, uuid, text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.manual_payout_outcome_request(uuid, uuid, text, text, integer, uuid, text, text, text, bigint, text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.manual_payout_list(uuid, uuid) TO gk_api;
