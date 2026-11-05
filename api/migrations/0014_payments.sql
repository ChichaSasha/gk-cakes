-- GK-025 A3b: Stripe sandbox Checkout under the K11 recovery protocol, a
-- signed webhook inbox, reconciliation and paid confirmation through the A3a
-- order-command path (contracts r6.5 K00, K03, K04 clocks/Q2/Stripe rules,
-- K05 payment states and lifecycle, K11).
--
-- Collection (cake, full amount). POST /api/v1/orders/{id}/payment calls
-- business.payment_initiate: under the order lock it checks the order
-- (awaiting payment, full payment, prepared, holds intact, expected version),
-- the quote's 24 h window and the 192 h lead time at the request instant,
-- that no restore freeze is on, and that no other collection of the order
-- may still succeed; then it stores the collection (amount, environment,
-- Stripe account and mode, idempotency key = collection ID, the UI origin
-- the customer returns to), its dispatch job and an audit event in one
-- transaction.
-- The same actor (account or guest capability) and key replays the same
-- collection (K00); another key while one may succeed is
-- payment_in_progress; after a definitive failure, expiry or cancellation a
-- new collection is allowed.
--
-- Dispatch (worker). The dispatch is a K11 recovery operation of kind
-- 'dispatch' with aggregate = collection, prior version 0 and operation ID =
-- collection ID. The exact Checkout Session request is fixed at the first
-- dispatch, once the worker has its Stripe key and journal:
-- payment_dispatch_fix stores the payload (environment, account, key, the
-- form-encoded request with expires_at = the first dispatch second + 2 hours
-- on the database clock; Stripe allows 30 minutes to 24 hours) and only then
-- fixes first_dispatch_at and replay_until = first dispatch + 23 hours
-- (Stripe keeps idempotency keys 24 hours). The worker then reserves the
-- recovery operation with that payload's hash and writes the prepared
-- envelope, so the body is persisted before the external create (K04) and
-- never rebuilt. Right before each call payment_dispatch_admit re-checks the
-- freeze and the window on the database clock and records
-- dispatch_started_at; a first call with less than 31 minutes of session
-- lifetime left is refused and recorded as a definitive failure without
-- calling Stripe. Every call is appended to payment_attempt. Outcomes:
-- 'created' (session ID, URL, provider expiry, livemode) makes the
-- collection pending and schedules a reconcile 15 minutes later (a lost
-- webhook is covered); 'rejected' (a definitive Stripe refusal, or expired
-- before any call) makes it failed; an idempotency conflict goes to
-- reconciliation and the collection is unknown.
--
-- Webhook (API). business.payment_event_record stores each verified event
-- once per (account, livemode, event ID) and, when its metadata names a
-- collection of this environment and account, schedules that collection's
-- reconcile job (at most one waiting job per collection; it is brought
-- forward). The API acknowledges only after this commits.
--
-- Reconcile (worker). The worker retrieves the session (with its payment
-- intent and latest charge) and decides a verdict; the database records it:
-- 'paid' (all funds checks passed; the success instant is the charge's
-- provider time, observed_at the local observation), 'pending' (anything not
-- yet paid), 'cancelled' (the session expired with no payment intent or a
-- canceled one: recorded only; holds are released by A3c; a decided
-- collection never moves back) or 'mismatch' (wrong amount, currency,
-- account, mode or metadata, a refunded or disputed charge: the collection
-- becomes unknown, never confirmed; a succeeded one keeps its paid evidence
-- and records the observation beside it). A paid
-- verdict, in the same transaction, makes the collection succeeded and
-- records the order command 'paid_confirm' (receipt instant = the success
-- instant, actor system, exact payload = order snapshot + collection
-- evidence), reserves its K11 command operation on the order and enqueues
-- its job, at most once per collection however many events arrive.
--
-- Paid confirmation (worker, A3a path). business.order_command_apply now
-- also applies paid_confirm: an order still awaiting payment with its holds
-- intact is committed (allocation committed, lots sold, code consumed,
-- paid_at = the success instant) as confirmed when the handover is at least
-- 192 h after the success instant, otherwise as paid_awaiting_agreement (K04
-- Q2: the money is real; no fulfillment; the chef case is A3c/GK-026). An
-- order that changed or lost its holds is refused (the collection stays
-- succeeded; the worker moves the job to attention: full return is A3c).
-- Both stay confirmation_pending until the outcome envelope is secured.
--
-- Expand-only (K09): new tables, columns, constraints and functions; the
-- replaced functions keep their signatures. Lock order (K00): orders, then
-- collections, then the recovery operation and the engines.

SET LOCAL ROLE gk_owner;

-- Order commands gain the system's paid confirmation.
ALTER TABLE business.order_command DROP CONSTRAINT order_command_command_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_command_check CHECK (command IN ('free_confirm', 'paid_confirm'));
ALTER TABLE business.order_command DROP CONSTRAINT order_command_actor_kind_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_actor_kind_check CHECK (actor_kind IN ('customer', 'guest', 'system'));

CREATE TABLE business.payment_collection (
  -- Also the Stripe idempotency key and the dispatch's recovery operation.
  id                  uuid PRIMARY KEY,
  order_id            uuid NOT NULL REFERENCES business.orders (id),
  kind                text NOT NULL DEFAULT 'cake' CHECK (kind = 'cake'),
  -- K05 payment states: created (stored, not known to Stripe yet), pending
  -- (session created), unknown (an outcome was lost or contradicts the
  -- collection), succeeded, failed (definitively refused, nothing created),
  -- cancelled (the session expired unpaid).
  state               text NOT NULL DEFAULT 'created'
                      CHECK (state IN ('created', 'pending', 'unknown', 'succeeded', 'failed', 'cancelled')),
  amount_minor        bigint NOT NULL CHECK (amount_minor > 0),
  currency            text NOT NULL DEFAULT 'GBP' CHECK (currency = 'GBP'),
  environment         text NOT NULL CHECK (environment ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
  provider_account_id text NOT NULL CHECK (provider_account_id ~ '^acct_[A-Za-z0-9]{1,64}$'),
  -- The Stripe mode this collection must use (live only in production).
  livemode            boolean NOT NULL,
  actor_kind          text NOT NULL CHECK (actor_kind IN ('customer', 'guest')),
  actor_account_id    uuid NULL REFERENCES business.account (id),
  guest_capability_id uuid NULL REFERENCES business.order_guest_capability (id),
  request_key         text NOT NULL CHECK (request_key ~ '^[\x21-\x7e]{1,128}$'),
  request_sha256      char(64) NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
  -- The exact UI origin the customer returns to (its order page).
  return_origin       text NOT NULL CHECK (return_origin ~ '^https?://[^/?#[:space:]]+$' AND octet_length(return_origin) <= 200),
  -- The exact dispatch payload (JSON text, including the form-encoded
  -- request), fixed at the first dispatch and never rebuilt; its hash is the
  -- recovery payload hash.
  payload             text NULL CHECK (octet_length(payload) BETWEEN 2 AND 16384),
  payload_sha256      char(64) NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  initiated_at        timestamptz NOT NULL,
  -- The expires_at sent to Stripe: the first dispatch second + 2 hours.
  session_expires_at  timestamptz NULL,
  first_dispatch_at   timestamptz NULL,
  replay_until        timestamptz NULL,
  dispatch_started_at timestamptz NULL,
  provider_session_id text NULL CHECK (provider_session_id ~ '^cs_[A-Za-z0-9_]{1,250}$'),
  checkout_url        text NULL CHECK (checkout_url ~ '^https://' AND octet_length(checkout_url) <= 4096),
  provider_expires_at timestamptz NULL,
  provider_livemode   boolean NULL,
  payment_intent_id   text NULL CHECK (payment_intent_id ~ '^pi_[A-Za-z0-9_]{1,250}$'),
  session_status      text NULL CHECK (session_status ~ '^[a-z_]{1,40}$'),
  payment_status      text NULL CHECK (payment_status ~ '^[a-z_]{1,40}$'),
  last_observed_at    timestamptz NULL,
  -- Authoritative success instant (the charge's provider time) and when it
  -- was first observed here (K04: never the webhook arrival).
  succeeded_at        timestamptz NULL,
  observed_at         timestamptz NULL,
  -- Bounded evidence: IDs, statuses, amounts, currency, timestamps only.
  evidence            json NULL CHECK (evidence IS NULL OR octet_length(evidence::text) <= 4096),
  mismatch_reason     text NULL CHECK (length(mismatch_reason) BETWEEN 1 AND 200),
  -- The observation that contradicted the collection (bounded like
  -- evidence); a succeeded collection keeps its paid evidence.
  mismatch_evidence   json NULL CHECK (mismatch_evidence IS NULL OR octet_length(mismatch_evidence::text) <= 4096),
  version             integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT payment_collection_actor CHECK ((actor_kind = 'customer') = (actor_account_id IS NOT NULL)
                                             AND (actor_kind = 'guest') = (guest_capability_id IS NOT NULL)),
  CONSTRAINT payment_collection_window CHECK ((first_dispatch_at IS NULL) = (replay_until IS NULL)
                                              AND (replay_until IS NULL OR replay_until = first_dispatch_at + interval '23 hours')),
  CONSTRAINT payment_collection_body CHECK ((first_dispatch_at IS NULL) = (payload IS NULL)
                                            AND (payload IS NULL) = (payload_sha256 IS NULL)
                                            AND (payload IS NULL) = (session_expires_at IS NULL)),
  CONSTRAINT payment_collection_expiry CHECK (session_expires_at IS NULL
                                              OR session_expires_at = date_trunc('second', first_dispatch_at) + interval '2 hours'),
  CONSTRAINT payment_collection_succeeded CHECK ((state = 'succeeded') = (succeeded_at IS NOT NULL AND observed_at IS NOT NULL)),
  CONSTRAINT payment_collection_session CHECK (state NOT IN ('pending', 'cancelled') OR provider_session_id IS NOT NULL)
);

CREATE INDEX payment_collection_order_idx ON business.payment_collection (order_id, created_at);
CREATE UNIQUE INDEX payment_collection_request_idx
  ON business.payment_collection (order_id, request_key, coalesce(actor_account_id, guest_capability_id));
CREATE UNIQUE INDEX payment_collection_session_idx
  ON business.payment_collection (provider_session_id) WHERE provider_session_id IS NOT NULL;

-- The paid confirmation of a collection, at most one per collection.
ALTER TABLE business.order_command ADD COLUMN collection_id uuid NULL REFERENCES business.payment_collection (id);
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_payment CHECK ((command = 'paid_confirm') = (collection_id IS NOT NULL)
                                             AND (command <> 'paid_confirm' OR actor_kind = 'system'));
CREATE UNIQUE INDEX order_command_collection_idx ON business.order_command (collection_id) WHERE collection_id IS NOT NULL;

-- One row per Stripe Checkout Session create call, in order. Never updated.
CREATE TABLE business.payment_attempt (
  id             uuid PRIMARY KEY,
  collection_id  uuid NOT NULL REFERENCES business.payment_collection (id),
  attempt_no     integer NOT NULL CHECK (attempt_no >= 1),
  job_id         uuid NOT NULL,
  job_generation bigint NOT NULL CHECK (job_generation >= 1),
  -- created: session returned; rejected: definitively refused; deferred:
  -- not processed now (rate limit, concurrent request); unknown: no or
  -- ambiguous response; refused: credential or permission; conflict: the
  -- idempotency key was used with a different request.
  result         text NOT NULL CHECK (result IN ('created', 'rejected', 'deferred', 'unknown', 'refused', 'conflict')),
  http_status    integer NULL CHECK (http_status BETWEEN 100 AND 599),
  provider_code  text NULL CHECK (provider_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT payment_attempt_order UNIQUE (collection_id, attempt_no)
);

CREATE TRIGGER payment_attempt_immutable BEFORE UPDATE OR DELETE ON business.payment_attempt
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();

-- Which collection and purpose each payment job serves.
CREATE TABLE business.payment_job (
  job_id        uuid PRIMARY KEY REFERENCES business.job (id),
  collection_id uuid NOT NULL REFERENCES business.payment_collection (id),
  purpose       text NOT NULL CHECK (purpose IN ('dispatch', 'reconcile')),
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX payment_job_dispatch_idx ON business.payment_job (collection_id) WHERE purpose = 'dispatch';
CREATE INDEX payment_job_collection_idx ON business.payment_job (collection_id, purpose);

-- Verified Stripe events, minimized: IDs, types, mode, times and the hash
-- of the body; never customer or card details.
CREATE TABLE business.stripe_event (
  id                     uuid PRIMARY KEY,
  account                text NOT NULL CHECK (account ~ '^acct_[A-Za-z0-9]{1,64}$'),
  livemode               boolean NOT NULL,
  event_id               text NOT NULL CHECK (event_id ~ '^evt_[A-Za-z0-9_]{1,250}$'),
  event_type             text NOT NULL CHECK (event_type ~ '^[a-z0-9_]{1,40}(\.[a-z0-9_]{1,40}){1,4}$'),
  -- The API environment that received it.
  environment            text NOT NULL CHECK (environment ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
  provider_created_at    timestamptz NULL,
  object_type            text NULL CHECK (object_type ~ '^[a-z0-9_.]{1,60}$'),
  object_id              text NULL CHECK (object_id ~ '^[A-Za-z0-9_]{1,255}$'),
  metadata_collection_id uuid NULL,
  metadata_environment   text NULL CHECK (metadata_environment ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
  -- matched: a collection of this environment and account.
  collection_id          uuid NULL REFERENCES business.payment_collection (id),
  payload_sha256         char(64) NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  received_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT stripe_event_identity UNIQUE (account, livemode, event_id)
);

CREATE INDEX stripe_event_collection_idx ON business.stripe_event (collection_id, received_at) WHERE collection_id IS NOT NULL;

CREATE TRIGGER stripe_event_immutable BEFORE UPDATE OR DELETE ON business.stripe_event
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: whether a form-encoded body has exactly key=value as one
-- of its pairs (both already encoded).
CREATE FUNCTION business.payment_form_has(p_form text, p_key text, p_value text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.strpos('&' || p_form || '&', '&' || p_key || '=' || p_value || '&') > 0;
$$;

-- Owner-internal: schedule the collection's reconcile job no later than
-- p_at: a waiting one is brought forward, otherwise a new one is enqueued
-- (at most one waiting job per collection, also under concurrency).
CREATE FUNCTION business.payment_schedule_reconcile(p_collection uuid, p_at timestamptz)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_job uuid;
BEGIN
  -- Concurrent schedulers of one collection take turns (the collection row
  -- lock; callers hold no order lock, or already hold this one).
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = p_collection FOR UPDATE;
  SELECT j.id INTO v_job
  FROM business.payment_job pj JOIN business.job j ON j.id = pj.job_id
  WHERE pj.collection_id = p_collection AND pj.purpose = 'reconcile' AND j.state IN ('queued', 'retry')
  ORDER BY j.next_attempt_at, j.id
  LIMIT 1
  FOR UPDATE OF j;
  IF FOUND THEN
    UPDATE business.job j
    SET next_attempt_at = LEAST(j.next_attempt_at, GREATEST(p_at, pg_catalog.now())),
        version = j.version + 1, updated_at = pg_catalog.now()
    WHERE j.id = v_job;
    RETURN v_job;
  END IF;
  v_job := business.job_enqueue(p_collection, p_at);
  INSERT INTO business.payment_job (job_id, collection_id, purpose) VALUES (v_job, p_collection, 'reconcile');
  RETURN v_job;
END;
$$;

-- API: start collecting a full-payment order (see the header). p_return_origin
-- is the exact UI origin the Stripe return URLs point at. Returns the
-- collection and whether it already existed (a replay). A restore freeze
-- refuses new collections (GK503 recovery_frozen: retry later).
CREATE FUNCTION business.payment_initiate(
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
  -- K04: no second collection while an earlier one may still succeed.
  IF EXISTS (SELECT 1 FROM business.payment_collection c
             WHERE c.order_id = p_order AND c.state IN ('created', 'pending', 'unknown')) THEN
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

-- API: store one verified Stripe event (see the header). Result 'recorded'
-- or 'duplicate', with the matched collection (if any).
CREATE FUNCTION business.payment_event_record(
  p_env text, p_account text, p_livemode boolean, p_event_id text, p_event_type text, p_created timestamptz,
  p_object_type text, p_object_id text, p_metadata_collection uuid, p_metadata_env text, p_payload_sha256 text)
RETURNS TABLE (result text, event_collection_id uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_seen uuid;
  v_seen_collection uuid;
  v_c business.payment_collection%ROWTYPE;
BEGIN
  -- Serialize deliveries of one event so a concurrent retry is a duplicate.
  PERFORM pg_catalog.pg_advisory_xact_lock(
    pg_catalog.hashtextextended('gk.stripe_event:' || p_account || ':' || p_livemode::text || ':' || p_event_id, 0));
  SELECT e.id, e.collection_id INTO v_seen, v_seen_collection FROM business.stripe_event e
  WHERE e.account = p_account AND e.livemode = p_livemode AND e.event_id = p_event_id;
  IF FOUND THEN
    RETURN QUERY SELECT 'duplicate'::text, v_seen_collection;
    RETURN;
  END IF;
  IF p_metadata_collection IS NOT NULL AND p_metadata_env IS NOT DISTINCT FROM p_env THEN
    -- Locked before the event row references it (its foreign key would
    -- otherwise share-lock it first and concurrent deliveries deadlock when
    -- scheduling the reconcile).
    SELECT * INTO v_c FROM business.payment_collection c
    WHERE c.id = p_metadata_collection AND c.environment = p_env AND c.provider_account_id = p_account
      AND c.livemode = p_livemode
    FOR UPDATE;
  END IF;
  INSERT INTO business.stripe_event
    (id, account, livemode, event_id, event_type, environment, provider_created_at, object_type, object_id,
     metadata_collection_id, metadata_environment, collection_id, payload_sha256)
  VALUES (pg_catalog.gen_random_uuid(), p_account, p_livemode, p_event_id, p_event_type, p_env, p_created, p_object_type,
          p_object_id, p_metadata_collection, p_metadata_env, v_c.id, p_payload_sha256);
  IF v_c.id IS NOT NULL AND v_c.state IN ('created', 'pending', 'unknown') THEN
    PERFORM business.payment_schedule_reconcile(v_c.id, pg_catalog.now());
  END IF;
  RETURN QUERY SELECT 'recorded'::text, v_c.id;
END;
$$;

-- Owner-internal: the payment job the caller holds a live lease on. Raises
-- GK001 when the lease is not held; returns NULL when the job is not a
-- payment job.
CREATE FUNCTION business.payment_leased(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS business.payment_job
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_operation uuid;
  v business.payment_job;
BEGIN
  SELECT j.operation_id INTO v_operation
  FROM business.job j
  WHERE j.id = p_job_id AND j.state = 'running' AND j.lease_owner = p_worker AND j.generation = p_generation
    AND j.lease_until > pg_catalog.now()
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'job % is not leased to this worker generation', p_job_id USING ERRCODE = 'GK001';
  END IF;
  SELECT * INTO v FROM business.payment_job pj WHERE pj.job_id = p_job_id AND pj.collection_id = v_operation;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  RETURN v;
END;
$$;

-- Owner-internal: the leased dispatch job whose recovery operation is
-- p_operation_id (= its collection).
CREATE FUNCTION business.payment_leased_dispatch(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS business.payment_job
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.job_id IS NULL OR v.purpose <> 'dispatch' OR v.collection_id IS DISTINCT FROM p_operation_id THEN
    RAISE EXCEPTION 'recovery operation % is not the dispatch of job %', p_operation_id, p_job_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v;
END;
$$;

-- Worker: start (or resume) the leased payment job. status 'ready' returns
-- what the purpose needs (a dispatch whose body is not fixed yet returns no
-- payload: the worker builds it with db_now and calls payment_dispatch_fix;
-- nothing is fixed here, so a job that waits for a key or journal starts no
-- window); 'frozen' (restore), 'settled' (a reconcile for a collection that
-- is already decided) and 'no_session' (a reconcile before any session is
-- known; the dispatch schedules its own) return no work; 'not_payment'
-- means another handler owns the job.
CREATE FUNCTION business.payment_job_begin(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS TABLE (status text, purpose text, collection_id uuid, order_id uuid, collection_state text, payload text,
               replay_until timestamptz, session_expires_at timestamptz, return_origin text, db_now timestamptz,
               session_id text, amount_minor bigint, environment text, account text, livemode boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased(p_job_id, p_worker, p_generation);
  c business.payment_collection%ROWTYPE;
  v_session text;
BEGIN
  IF v.job_id IS NULL THEN
    RETURN QUERY SELECT 'not_payment'::text, NULL::text, NULL::uuid, NULL::uuid, NULL::text, NULL::text,
      NULL::timestamptz, NULL::timestamptz, NULL::text, NULL::timestamptz, NULL::text, NULL::bigint, NULL::text,
      NULL::text, NULL::boolean;
    RETURN;
  END IF;
  IF business.recovery_frozen() THEN
    RETURN QUERY SELECT 'frozen'::text, v.purpose, v.collection_id, NULL::uuid, NULL::text, NULL::text,
      NULL::timestamptz, NULL::timestamptz, NULL::text, NULL::timestamptz, NULL::text, NULL::bigint, NULL::text,
      NULL::text, NULL::boolean;
    RETURN;
  END IF;
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id;
  IF v.purpose = 'dispatch' THEN
    RETURN QUERY SELECT 'ready'::text, v.purpose, c.id, c.order_id, c.state, c.payload, c.replay_until,
      c.session_expires_at, c.return_origin, pg_catalog.now(), c.provider_session_id, c.amount_minor, c.environment,
      c.provider_account_id, c.livemode;
    RETURN;
  END IF;
  IF c.state IN ('succeeded', 'failed', 'cancelled') THEN
    RETURN QUERY SELECT 'settled'::text, v.purpose, c.id, c.order_id, c.state, NULL::text, NULL::timestamptz,
      NULL::timestamptz, NULL::text, pg_catalog.now(), c.provider_session_id, c.amount_minor, c.environment,
      c.provider_account_id, c.livemode;
    RETURN;
  END IF;
  -- The created session, or one a verified event named for this collection
  -- (its create response may have been lost); the worker verifies the
  -- retrieved session's metadata before trusting it.
  v_session := coalesce(c.provider_session_id,
    (SELECT e.object_id FROM business.stripe_event e
     WHERE e.collection_id = c.id AND e.object_type = 'checkout.session' AND e.object_id ~ '^cs_[A-Za-z0-9_]{1,250}$'
     ORDER BY e.received_at DESC, e.id DESC LIMIT 1));
  IF v_session IS NULL THEN
    RETURN QUERY SELECT 'no_session'::text, v.purpose, c.id, c.order_id, c.state, NULL::text, NULL::timestamptz,
      NULL::timestamptz, NULL::text, pg_catalog.now(), NULL::text, c.amount_minor, c.environment,
      c.provider_account_id, c.livemode;
    RETURN;
  END IF;
  RETURN QUERY SELECT 'ready'::text, v.purpose, c.id, c.order_id, c.state, NULL::text, NULL::timestamptz,
    NULL::timestamptz, NULL::text, pg_catalog.now(), v_session, c.amount_minor, c.environment, c.provider_account_id,
    c.livemode;
END;
$$;

-- Worker: fix the leased dispatch's exact request at its first dispatch
-- (see the header). p_at is the database instant the worker built it from
-- (payment_job_begin's db_now, at most a minute old); the request's
-- expires_at must be its second + 2 hours, and the payload must belong to
-- this collection, order, amount, environment, account and mode (the worker
-- validates the whole request again before every call). Status 'ready'
-- returns the fixed payload, window and expiry (unchanged when already
-- fixed: p_payload is then ignored); 'frozen' fixes nothing.
CREATE FUNCTION business.payment_dispatch_fix(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_payload text, p_at timestamptz)
RETURNS TABLE (status text, payload text, replay_until timestamptz, session_expires_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  c business.payment_collection%ROWTYPE;
  v_expires timestamptz;
  v_p json;
  v_request text;
BEGIN
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  IF c.first_dispatch_at IS NOT NULL THEN
    RETURN QUERY SELECT 'ready'::text, c.payload, c.replay_until, c.session_expires_at;
    RETURN;
  END IF;
  IF business.recovery_frozen() THEN
    RETURN QUERY SELECT 'frozen'::text, NULL::text, NULL::timestamptz, NULL::timestamptz;
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

-- Worker: recovery_reserve for exactly the leased collection's dispatch.
CREATE FUNCTION business.payment_recovery_reserve(
  p_job_id uuid, p_worker uuid, p_generation bigint,
  p_operation_id uuid, p_kind text, p_aggregate_id uuid, p_prior_version integer,
  p_payload_hash text, p_envelope_key text, p_replay_until timestamptz)
RETURNS TABLE (result text, op_state text, op_outcome json, op_outcome_hash text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  c business.payment_collection%ROWTYPE;
BEGIN
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id;
  IF p_kind IS DISTINCT FROM 'dispatch' OR p_aggregate_id IS DISTINCT FROM c.id OR p_prior_version IS DISTINCT FROM 0
     OR c.payload_sha256 IS NULL OR p_payload_hash IS DISTINCT FROM c.payload_sha256::text OR c.replay_until IS NULL
     OR p_replay_until IS DISTINCT FROM c.replay_until
     OR p_envelope_key IS DISTINCT FROM ('recovery/' || c.environment || '/op/' || c.id::text || '/0000000000-' || c.id::text) THEN
    RAISE EXCEPTION 'recovery operation % does not match the dispatch of collection %', p_operation_id, c.id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN QUERY SELECT * FROM business.recovery_reserve(p_operation_id, p_kind, p_aggregate_id, p_prior_version,
                                                      p_payload_hash, p_envelope_key, p_replay_until);
END;
$$;

CREATE FUNCTION business.payment_recovery_prepared(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  RETURN business.recovery_mark_prepared(v.collection_id);
END;
$$;

-- Worker: admission right before the Stripe call, on the database clock:
-- 'ok' (dispatch_started_at recorded), 'frozen', 'expired' (the idempotency
-- window has ended) or 'too_late' (no call was ever admitted and fewer than
-- 31 minutes of session lifetime are left: Stripe would refuse it, so the
-- worker records a definitive failure without calling). Only 'ok' may call
-- Stripe, after this transaction commits.
CREATE FUNCTION business.payment_dispatch_admit(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  c business.payment_collection%ROWTYPE;
BEGIN
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  IF business.recovery_frozen() THEN
    RETURN 'frozen';
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

-- Worker: record one Stripe create call. Evidence is kept even from a
-- worker whose lease has since been lost (the call happened). An unknown or
-- conflicting response makes a created collection unknown.
CREATE FUNCTION business.payment_attempt_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid,
  p_result text, p_http_status integer, p_provider_code text)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  c business.payment_collection%ROWTYPE;
  v_no integer;
BEGIN
  SELECT pc.* INTO c
  FROM business.job j
  JOIN business.payment_job pj ON pj.job_id = j.id AND pj.purpose = 'dispatch' AND pj.collection_id = j.operation_id
  JOIN business.payment_collection pc ON pc.id = pj.collection_id
  WHERE j.id = p_job_id AND pc.id = p_operation_id AND p_worker IS NOT NULL AND p_generation BETWEEN 1 AND j.generation
  FOR UPDATE OF pc;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'attempt does not belong to job % generation %', p_job_id, p_generation
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF c.dispatch_started_at IS NULL THEN
    RAISE EXCEPTION 'collection % was never admitted for dispatch', c.id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT coalesce(max(a.attempt_no), 0) + 1 INTO v_no FROM business.payment_attempt a WHERE a.collection_id = c.id;
  INSERT INTO business.payment_attempt (id, collection_id, attempt_no, job_id, job_generation, result, http_status, provider_code)
  VALUES (pg_catalog.gen_random_uuid(), c.id, v_no, p_job_id, p_generation, p_result, p_http_status, p_provider_code);
  IF p_result IN ('unknown', 'conflict') THEN
    UPDATE business.payment_collection pc
    SET state = 'unknown', version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = c.id AND pc.state = 'created';
  END IF;
  RETURN v_no;
END;
$$;

-- Worker: record the dispatch outcome (recovery committed) and settle the
-- collection: 'created' -> pending with the session (a mode other than the
-- collection's makes it unknown) and a reconcile 15 minutes later;
-- 'rejected' -> failed. Later reconcile evidence is never overwritten.
CREATE FUNCTION business.payment_recovery_committed(
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
BEGIN
  IF v_status IS NULL OR v_status NOT IN ('created', 'rejected') THEN
    RAISE EXCEPTION 'unsupported payment dispatch outcome %', v_status USING ERRCODE = 'invalid_parameter_value';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
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
    IF v_changed THEN
      PERFORM business.audit_record('system', NULL, 'payment.failed', 'payment_collection', v.collection_id, NULL,
                                    pg_catalog.json_build_object('reason', coalesce(v_ev ->> 'reason', 'rejected_by_stripe')));
    END IF;
  END IF;
  RETURN QUERY SELECT v_state, v_changed;
END;
$$;

CREATE FUNCTION business.payment_recovery_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  RETURN business.recovery_mark_secured(v.collection_id);
END;
$$;

-- Worker: the outcome cannot be established safely (idempotency conflict,
-- or the window ended); the collection is unknown for manager
-- reconciliation.
CREATE FUNCTION business.payment_recovery_reconcile(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid, p_reason text)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
  v_state text;
BEGIN
  PERFORM 1 FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  v_state := business.recovery_mark_reconcile(p_operation_id, p_reason);
  UPDATE business.payment_collection pc
  SET state = 'unknown', version = pc.version + 1, updated_at = pg_catalog.now()
  WHERE pc.id = v.collection_id AND pc.state IN ('created', 'unknown');
  RETURN v_state;
END;
$$;

CREATE FUNCTION business.payment_recovery_frozen(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.job_id IS NULL THEN
    RAISE EXCEPTION 'job % is not a payment job', p_job_id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN business.recovery_frozen();
END;
$$;

-- Worker: record a reconcile verdict for the leased reconcile job's
-- collection (see the header). p_session_id is the retrieved session;
-- p_evidence is bounded (IDs, statuses, amounts, currency, times);
-- p_success_at is the charge's provider time (paid only); p_reason names a
-- mismatch. Returns the collection's state afterwards and its paid
-- confirmation command (when one exists). A restore freeze stops a paid
-- record under its own lock (GK002); another unresolved protected operation
-- on the order is GK409 order_busy (nothing is recorded; retry later), an
-- operation at this or a later order version GK409 order_stale (attention).
CREATE FUNCTION business.payment_reconcile_record(
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
    -- A succeeded collection keeps its state and paid evidence; the
    -- contradicting observation is kept beside it.
    UPDATE business.payment_collection pc
    SET state = CASE WHEN pc.state = 'succeeded' THEN pc.state ELSE 'unknown' END,
        evidence = CASE WHEN pc.state = 'succeeded' THEN pc.evidence ELSE p_evidence END,
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

-- As in 0013, now also applying paid_confirm (see the header).
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
  -- Lock order (K00): the order, its collection, then its recovery
  -- operation, then the day and lots inside the engines.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  IF v.command = 'paid_confirm' THEN
    SELECT * INTO v_c FROM business.payment_collection c WHERE c.id = v.collection_id FOR UPDATE;
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
      -- The money is real and stays succeeded: full return is A3c; the
      -- worker moves the job to attention so it is never silent.
      v_outcome := pg_catalog.format('{"status":"refused","evidence":{"reason":"%s","collection_id":"%s"}}',
                                     v_reason, v.collection_id);
      PERFORM business.audit_record('system', NULL, 'order.paid_confirmation_refused', 'order', v.order_id, NULL,
                                    pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason,
                                                                 'collection_id', v.collection_id));
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

-- As in 0013; a paid order awaiting agreement is secured as well.
CREATE OR REPLACE FUNCTION business.order_command_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_state text;
BEGIN
  PERFORM 1 FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  v_state := business.recovery_mark_secured(v.id);
  UPDATE business.orders o SET recovery_state = 'secured', updated_at = pg_catalog.now()
  WHERE o.id = v.order_id AND o.recovery_state = 'confirmation_pending'
    AND (SELECT r.outcome ->> 'status' FROM business.recovery_operation r WHERE r.id = v.id)
        IN ('confirmed', 'paid_awaiting_agreement');
  RETURN v_state;
END;
$$;

-- As in 0013, plus the latest collection ("payment": its state, the
-- checkout URL only while the session is open, the provider expiry) and
-- paid_at only once the paid confirmation is secured.
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
                               OR EXISTS (SELECT 1 FROM business.order_command pcmd
                                          JOIN business.recovery_operation pr ON pr.id = pcmd.id
                                          WHERE pcmd.order_id = o.id AND pcmd.command = 'paid_confirm'
                                            AND pr.state <> 'secured')
                             THEN 'confirmation_pending' ELSE o.lifecycle END,
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
                         'id', pc.id, 'state', pc.state, 'amount_minor', pc.amount_minor, 'currency', pc.currency,
                         'requested_at', pc.initiated_at,
                         'checkout_url', CASE WHEN pc.state = 'pending' AND pc.session_status = 'open'
                                                   AND pc.provider_expires_at > pg_catalog.now()
                                              THEN pc.checkout_url END,
                         'expires_at', pc.provider_expires_at)
                       FROM business.payment_collection pc
                       WHERE pc.order_id = o.id
                       ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1),
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

REVOKE ALL ON TABLE business.payment_collection FROM PUBLIC;
REVOKE ALL ON TABLE business.payment_attempt FROM PUBLIC;
REVOKE ALL ON TABLE business.payment_job FROM PUBLIC;
REVOKE ALL ON TABLE business.stripe_event FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_form_has(text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_schedule_reconcile(uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_initiate(uuid, bytea, uuid, text, text, integer, uuid, text, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_event_record(text, text, boolean, text, text, timestamptz, text, text, uuid, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_leased(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_leased_dispatch(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_job_begin(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_dispatch_fix(uuid, uuid, bigint, uuid, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_recovery_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_recovery_prepared(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_dispatch_admit(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_attempt_record(uuid, uuid, bigint, uuid, text, integer, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_recovery_committed(uuid, uuid, bigint, uuid, json, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_recovery_secured(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_recovery_reconcile(uuid, uuid, bigint, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_recovery_frozen(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_reconcile_record(uuid, uuid, bigint, uuid, text, text, text, text, text, json, timestamptz, text) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.payment_initiate(uuid, bytea, uuid, text, text, integer, uuid, text, text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.payment_event_record(text, text, boolean, text, text, timestamptz, text, text, uuid, text, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.payment_job_begin(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_dispatch_fix(uuid, uuid, bigint, uuid, text, timestamptz) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_recovery_reserve(uuid, uuid, bigint, uuid, text, uuid, integer, text, text, timestamptz) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_recovery_prepared(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_dispatch_admit(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_attempt_record(uuid, uuid, bigint, uuid, text, integer, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_recovery_committed(uuid, uuid, bigint, uuid, json, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_recovery_secured(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_recovery_reconcile(uuid, uuid, bigint, uuid, text) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_recovery_frozen(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_reconcile_record(uuid, uuid, bigint, uuid, text, text, text, text, text, json, timestamptz, text) TO gk_worker;
