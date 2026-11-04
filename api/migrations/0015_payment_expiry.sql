-- GK-025 A3c1: the 15-minute Stripe expire request, never-dispatched
-- cleanup and the release of proven-unpaid holds and abandoned orders
-- (contracts r6.5 K03 "only proven-unpaid abandonment releases use; unknown
-- remains reserved", K04 clocks and "At 15m request expiry only for an open
-- session ...", "Never-dispatched cleanup ...", "Valid payment underway
-- before expiry is not a cancelled order ...", K05 lifecycle, K11).
--
-- Expire request (worker). The collection's reconcile, at or after the
-- first admitted dispatch (dispatch_started_at) + 15 minutes, asks Stripe to
-- expire the session when the collection is still pending, its session was
-- last seen open and no expire request was answered yet:
-- payment_expire_admit records expire_requested_at under the collection
-- lock (a concurrent reconcile within 30 seconds skips), the worker posts
-- /v1/checkout/sessions/{id}/expire with Idempotency-Key <collection>-expire
-- and payment_expire_record appends the answer as a payment attempt of kind
-- 'expire' ('expired', 'not_open', 'rejected' end the expire requests;
-- 'unknown', 'deferred' and 'refused' allow another later). Whatever Stripe
-- answers, only the following retrieve-and-verify reconcile decides the
-- collection: a processing or unknown payment stays pending, never
-- cancelled because the timer elapsed.
--
-- Never-dispatched cleanup (worker, inside the order hold review). A
-- collection still 'created' whose Stripe call was never admitted
-- (dispatch_started_at null) and whose initiation is at least 15 minutes old
-- is closed under the order and collection locks: state 'cancelled' with
-- cancel_reason 'never_dispatched'. payment_dispatch_admit, which records
-- dispatch_started_at under the same collection lock, then answers 'closed'
-- (no Stripe call; the dispatch records a 'rejected' outcome with reason
-- never_dispatched and completes), and payment_dispatch_fix and
-- payment_job_begin answer 'closed' for a request not yet fixed. Admission
-- first: the cleanup leaves the collection alone and the call proceeds.
--
-- Hold review (worker). Every order gets an order_hold_review row and job
-- (AFTER INSERT trigger) due at its hold's end: hold_until = the later of
-- placed_at and the insertion instant, + 15 minutes (at or after it the hold
-- has ended). A collection becoming cancelled or failed brings the review
-- forward (never before hold_until; at most one waiting review job per
-- order). order_hold_review_run, lease-fenced, re-checks everything under
-- the order lock (lock order: the order, its collections by ID, the review
-- row, then the day, lots by ID and the code, as checkout and the order
-- commands do):
--   * not awaiting payment any more: the review ends ('done', closed);
--   * a collection succeeded: the review ends ('done', paid; the money is
--     real: confirmation or a full return decide, never a release);
--   * a restore freeze: nothing changes ('frozen'; the worker defers);
--   * before hold_until: re-scheduled at hold_until ('waiting');
--   * never-dispatched collections are closed (above);
--   * a collection created (admitted), pending or unknown, or an
--     unresolved protected operation on the order (a free or paid
--     confirmation not yet secured): re-scheduled (+15 minutes, or when a
--     never-admitted collection reaches 15 minutes) and never released;
--   * otherwise (every collection failed or cancelled, or none at all, and
--     no unresolved command) the order is abandoned: its day allocation is
--     released, reserved ready cakes returned (lot_settle(..., false),
--     reservation 'returned'), the code use released, and the order
--     cancelled with cancel_reason 'payment_not_completed' (full payment) or
--     'free_not_confirmed' (free order), cancelled_at and version + 1, with
--     an audit event ('released').
-- The storefront sends the customer to pay (or to confirm a free order)
-- right after checkout; coming back later means a new quote and checkout.
-- A late success for a collection of a released order is recorded as
-- succeeded and its paid confirmation is refused (order_changed): the order
-- stays cancelled and the job goes to attention (full return: A3c2).
--
-- Expand-only (K09): new tables, columns, constraints, triggers and
-- functions; the replaced functions keep their signatures.

SET LOCAL ROLE gk_owner;

-- Orders: why and when an order was cancelled (only the hold review cancels
-- yet; later cancellations extend the set).
ALTER TABLE business.orders
  ADD COLUMN cancel_reason text NULL CHECK (cancel_reason IN ('payment_not_completed', 'free_not_confirmed')),
  ADD COLUMN cancelled_at timestamptz NULL,
  ADD CONSTRAINT orders_cancellation CHECK ((cancel_reason IS NULL) = (cancelled_at IS NULL)
                                            AND (cancel_reason IS NULL OR lifecycle = 'cancelled'));

-- Collections: a never-dispatched closure and the expire request.
ALTER TABLE business.payment_collection
  ADD COLUMN cancel_reason text NULL CHECK (cancel_reason = 'never_dispatched'),
  ADD COLUMN expire_requested_at timestamptz NULL,
  ADD CONSTRAINT payment_collection_never_dispatched
    CHECK (cancel_reason IS NULL OR (state = 'cancelled' AND dispatch_started_at IS NULL));
-- A never-dispatched cancellation has no session.
ALTER TABLE business.payment_collection DROP CONSTRAINT payment_collection_session;
ALTER TABLE business.payment_collection
  ADD CONSTRAINT payment_collection_session CHECK (state NOT IN ('pending', 'cancelled') OR provider_session_id IS NOT NULL
                                                   OR cancel_reason = 'never_dispatched');

-- Attempts: create calls and expire requests.
ALTER TABLE business.payment_attempt
  ADD COLUMN kind text NOT NULL DEFAULT 'create' CHECK (kind IN ('create', 'expire'));
ALTER TABLE business.payment_attempt DROP CONSTRAINT payment_attempt_result_check;
ALTER TABLE business.payment_attempt
  ADD CONSTRAINT payment_attempt_result_check CHECK (
    (kind = 'create' AND result IN ('created', 'rejected', 'deferred', 'unknown', 'refused', 'conflict'))
    OR (kind = 'expire' AND result IN ('expired', 'not_open', 'rejected', 'deferred', 'unknown', 'refused')));

-- One hold review per order.
CREATE TABLE business.order_hold_review (
  order_id   uuid PRIMARY KEY REFERENCES business.orders (id),
  -- The hold's end: the later of placed_at and the insertion instant + 15 m.
  hold_until timestamptz NOT NULL,
  -- released: the holds were released and the order cancelled; closed: the
  -- order left awaiting payment otherwise; paid: a collection succeeded.
  outcome    text NULL CHECK (outcome IN ('released', 'closed', 'paid')),
  decided_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT order_hold_review_decided CHECK ((outcome IS NULL) = (decided_at IS NULL))
);

-- The review's jobs (operation = the order); at most one waits.
CREATE TABLE business.order_hold_review_job (
  job_id     uuid PRIMARY KEY REFERENCES business.job (id),
  order_id   uuid NOT NULL REFERENCES business.order_hold_review (order_id),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX order_hold_review_job_order_idx ON business.order_hold_review_job (order_id);

-- Orders guard as in 0012; the cancellation reason and instant never change
-- once set.
CREATE OR REPLACE FUNCTION business.orders_guard()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'orders are never deleted' USING ERRCODE = 'GK409';
  END IF;
  IF (NEW.id, NEW.quote_id, NEW.body_sha256, NEW.original_handover_at, NEW.full_refund_deadline_at, NEW.subtotal_minor,
      NEW.discount_minor, NEW.total_minor, NEW.policy_version_id, NEW.placed_at, NEW.payment_requirement)
     IS DISTINCT FROM
     (OLD.id, OLD.quote_id, OLD.body_sha256, OLD.original_handover_at, OLD.full_refund_deadline_at, OLD.subtotal_minor,
      OLD.discount_minor, OLD.total_minor, OLD.policy_version_id, OLD.placed_at, OLD.payment_requirement)
     OR (OLD.paid_at IS NOT NULL AND NEW.paid_at IS DISTINCT FROM OLD.paid_at)
     OR (OLD.free_confirmed_at IS NOT NULL AND NEW.free_confirmed_at IS DISTINCT FROM OLD.free_confirmed_at)
     OR (OLD.cancel_reason IS NOT NULL AND NEW.cancel_reason IS DISTINCT FROM OLD.cancel_reason)
     OR (OLD.cancelled_at IS NOT NULL AND NEW.cancelled_at IS DISTINCT FROM OLD.cancelled_at)
     -- Terminal history is never reopened.
     OR (OLD.lifecycle IN ('cancelled', 'completed') AND NEW.lifecycle IS DISTINCT FROM OLD.lifecycle) THEN
    RAISE EXCEPTION 'order snapshot fields are immutable' USING ERRCODE = 'GK409';
  END IF;
  RETURN NEW;
END;
$$;

-- Owner-internal: schedule the order's hold review no earlier than its
-- hold's end and no later than p_at: a waiting review job is brought
-- forward, otherwise a new one is enqueued (at most one waiting job per
-- order, also under concurrency: the review row lock). Nothing once the
-- review has decided. Callers hold the order and collection locks or none.
CREATE FUNCTION business.order_hold_review_schedule(p_order uuid, p_at timestamptz)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_r business.order_hold_review%ROWTYPE;
  v_at timestamptz;
  v_job uuid;
BEGIN
  SELECT * INTO v_r FROM business.order_hold_review r WHERE r.order_id = p_order FOR UPDATE;
  IF NOT FOUND OR v_r.outcome IS NOT NULL THEN
    RETURN NULL;
  END IF;
  v_at := GREATEST(coalesce(p_at, pg_catalog.now()), v_r.hold_until, pg_catalog.now());
  SELECT j.id INTO v_job
  FROM business.order_hold_review_job rj JOIN business.job j ON j.id = rj.job_id
  WHERE rj.order_id = p_order AND j.state IN ('queued', 'retry')
  ORDER BY j.next_attempt_at, j.id
  LIMIT 1
  FOR UPDATE OF j;
  IF FOUND THEN
    UPDATE business.job j
    SET next_attempt_at = LEAST(j.next_attempt_at, v_at), version = j.version + 1, updated_at = pg_catalog.now()
    WHERE j.id = v_job;
    RETURN v_job;
  END IF;
  v_job := business.job_enqueue(p_order, v_at);
  INSERT INTO business.order_hold_review_job (job_id, order_id) VALUES (v_job, p_order);
  RETURN v_job;
END;
$$;

-- Trigger: every new order gets its hold review, due at the hold's end.
CREATE FUNCTION business.order_hold_review_start()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  INSERT INTO business.order_hold_review (order_id, hold_until)
  VALUES (NEW.id, GREATEST(NEW.placed_at, pg_catalog.now()) + interval '15 minutes');
  PERFORM business.order_hold_review_schedule(NEW.id, NULL);
  RETURN NULL;
END;
$$;

CREATE TRIGGER orders_hold_review AFTER INSERT ON business.orders
  FOR EACH ROW EXECUTE FUNCTION business.order_hold_review_start();

-- Trigger: a collection that became cancelled or failed brings its order's
-- review forward (a never-dispatched closure is made by the review itself).
CREATE FUNCTION business.payment_collection_closed()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.order_hold_review_schedule(NEW.order_id, pg_catalog.now());
  RETURN NULL;
END;
$$;

CREATE TRIGGER payment_collection_closed AFTER UPDATE OF state ON business.payment_collection
  FOR EACH ROW
  WHEN (OLD.state IS DISTINCT FROM NEW.state AND NEW.state IN ('cancelled', 'failed')
        AND NEW.cancel_reason IS DISTINCT FROM 'never_dispatched')
  EXECUTE FUNCTION business.payment_collection_closed();

-- Orders placed before this migration that still await payment get their
-- review now (their holds end no earlier than now + 15 minutes).
INSERT INTO business.order_hold_review (order_id, hold_until)
SELECT o.id, GREATEST(o.placed_at, pg_catalog.now()) + interval '15 minutes'
FROM business.orders o
WHERE o.lifecycle = 'awaiting_payment'
  AND NOT EXISTS (SELECT 1 FROM business.order_hold_review r WHERE r.order_id = o.id);
SELECT business.order_hold_review_schedule(r.order_id, NULL)
FROM business.order_hold_review r
WHERE r.outcome IS NULL
  AND NOT EXISTS (SELECT 1 FROM business.order_hold_review_job rj WHERE rj.order_id = r.order_id);

-- Owner-internal: the order whose hold review job the caller holds a live
-- lease on. Raises GK001 when the lease is not held; NULL when the job is
-- not a hold review job.
CREATE FUNCTION business.order_hold_review_leased(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_operation uuid;
  v_order uuid;
BEGIN
  SELECT j.operation_id INTO v_operation
  FROM business.job j
  WHERE j.id = p_job_id AND j.state = 'running' AND j.lease_owner = p_worker AND j.generation = p_generation
    AND j.lease_until > pg_catalog.now()
  FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'job % is not leased to this worker generation', p_job_id USING ERRCODE = 'GK001';
  END IF;
  SELECT rj.order_id INTO v_order FROM business.order_hold_review_job rj
  WHERE rj.job_id = p_job_id AND rj.order_id = v_operation;
  RETURN v_order;
END;
$$;

-- Owner-internal: release an abandoned order's holds (the caller holds the
-- order and collection locks): day allocations (day locks by date), reserved
-- ready cakes (lots by ID) and the reserved code use (code lock).
CREATE FUNCTION business.order_holds_release(p_order uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_day date;
  v_lot record;
  v_code uuid;
BEGIN
  FOR v_day IN SELECT DISTINCT a.day FROM business.capacity_allocation a
               WHERE a.order_ref = p_order AND a.state <> 'released' ORDER BY a.day LOOP
    PERFORM business.capacity_release(p_order, v_day);
  END LOOP;
  FOR v_lot IN SELECT r.lot_id, r.quantity FROM business.order_lot_reservation r
               WHERE r.order_id = p_order AND r.state = 'reserved' ORDER BY r.lot_id LOOP
    PERFORM business.lot_settle(v_lot.lot_id, v_lot.quantity, false);
    UPDATE business.order_lot_reservation r SET state = 'returned' WHERE r.order_id = p_order AND r.lot_id = v_lot.lot_id;
  END LOOP;
  SELECT c.code_id INTO v_code FROM business.code_application c WHERE c.order_id = p_order AND c.state = 'reserved';
  IF FOUND THEN
    PERFORM 1 FROM business.discount_code d WHERE d.id = v_code FOR UPDATE;
    UPDATE business.code_application c SET state = 'released', updated_at = pg_catalog.now()
    WHERE c.order_id = p_order AND c.state = 'reserved';
  END IF;
END;
$$;

-- Worker: run the leased hold review (see the header). Returns status
-- 'not_review' (another handler owns the job), 'frozen', 'waiting' (with
-- next_at: the successor review is scheduled), 'released' or 'done'; cleaned
-- counts the never-dispatched collections closed now.
CREATE FUNCTION business.order_hold_review_run(p_job_id uuid, p_worker uuid, p_generation bigint)
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
                AND pg_catalog.now() >= pc.initiated_at + interval '15 minutes'
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
  -- cancellation brings the review forward), a young never-admitted one
  -- (until its 15 minutes), an unresolved command on the order.
  SELECT min(CASE WHEN pc.state = 'created' AND pc.dispatch_started_at IS NULL
                  THEN pc.initiated_at + interval '15 minutes'
                  ELSE pg_catalog.now() + interval '15 minutes' END)
  INTO v_next
  FROM business.payment_collection pc
  WHERE pc.order_id = v_order AND pc.state IN ('created', 'pending', 'unknown');
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

-- Owner-internal: the leased reconcile job of collection p_collection.
CREATE FUNCTION business.payment_leased_reconcile(p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid)
RETURNS business.payment_job
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased(p_job_id, p_worker, p_generation);
BEGIN
  IF v.job_id IS NULL OR v.purpose <> 'reconcile' OR v.collection_id IS DISTINCT FROM p_collection THEN
    RAISE EXCEPTION 'collection % is not the reconcile of job %', p_collection, p_job_id
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN v;
END;
$$;

-- Worker: may the leased reconcile ask Stripe to expire the collection's
-- session now (see the header)? 'ok' (expire_requested_at recorded; call
-- after this commits), 'frozen', or 'skip' (not due, not pending, session
-- not open, already answered, or another reconcile is asking right now).
CREATE FUNCTION business.payment_expire_admit(p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid)
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

-- Worker: record one expire request's answer as a payment attempt of kind
-- 'expire'. Kept even from a worker whose lease has since been lost (the
-- call happened); it never changes the collection's state.
CREATE FUNCTION business.payment_expire_record(
  p_job_id uuid, p_worker uuid, p_generation bigint, p_collection uuid,
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
  JOIN business.payment_job pj ON pj.job_id = j.id AND pj.purpose = 'reconcile' AND pj.collection_id = j.operation_id
  JOIN business.payment_collection pc ON pc.id = pj.collection_id
  WHERE j.id = p_job_id AND pc.id = p_collection AND p_worker IS NOT NULL AND p_generation BETWEEN 1 AND j.generation
  FOR UPDATE OF pc;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'expire request does not belong to job % generation %', p_job_id, p_generation
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF c.expire_requested_at IS NULL THEN
    RAISE EXCEPTION 'collection % was never admitted for an expire request', c.id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT coalesce(max(a.attempt_no), 0) + 1 INTO v_no FROM business.payment_attempt a WHERE a.collection_id = c.id;
  INSERT INTO business.payment_attempt (id, collection_id, attempt_no, job_id, job_generation, kind, result, http_status,
                                        provider_code)
  VALUES (pg_catalog.gen_random_uuid(), c.id, v_no, p_job_id, p_generation, 'expire', p_result, p_http_status, p_provider_code);
  PERFORM business.audit_record('system', NULL, 'payment.expire_requested', 'order', c.order_id, NULL,
                                pg_catalog.json_build_object('collection_id', c.id, 'result', p_result));
  RETURN v_no;
END;
$$;

-- As in 0014; a dispatch whose collection was closed by the never-dispatched
-- cleanup before its request was fixed answers 'closed' (nothing to do).
CREATE OR REPLACE FUNCTION business.payment_job_begin(p_job_id uuid, p_worker uuid, p_generation bigint)
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
    IF c.cancel_reason = 'never_dispatched' AND c.first_dispatch_at IS NULL THEN
      RETURN QUERY SELECT 'closed'::text, v.purpose, c.id, c.order_id, c.state, NULL::text, NULL::timestamptz,
        NULL::timestamptz, NULL::text, pg_catalog.now(), NULL::text, c.amount_minor, c.environment,
        c.provider_account_id, c.livemode;
      RETURN;
    END IF;
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

-- As in 0014; a collection closed by the never-dispatched cleanup before its
-- request was fixed answers 'closed' and fixes nothing.
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
  v_expires timestamptz;
  v_p json;
  v_request text;
BEGIN
  SELECT * INTO c FROM business.payment_collection pc WHERE pc.id = v.collection_id FOR UPDATE;
  IF c.first_dispatch_at IS NOT NULL THEN
    RETURN QUERY SELECT 'ready'::text, c.payload, c.replay_until, c.session_expires_at;
    RETURN;
  END IF;
  IF c.cancel_reason = 'never_dispatched' THEN
    RETURN QUERY SELECT 'closed'::text, NULL::text, NULL::timestamptz, NULL::timestamptz;
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

-- As in 0014, plus 'closed': the never-dispatched cleanup closed the
-- collection first (under this same lock), so no call may ever be made; the
-- worker records a 'rejected' outcome (reason never_dispatched) without
-- calling Stripe.
CREATE OR REPLACE FUNCTION business.payment_dispatch_admit(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
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
  IF c.cancel_reason = 'never_dispatched' THEN
    RETURN 'closed';
  ELSIF business.recovery_frozen() THEN
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

-- As in 0014; a dispatch closed by the never-dispatched cleanup records its
-- 'rejected' outcome without another audit event (the cleanup audited it).
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
    IF v_changed AND (v_ev ->> 'reason') IS DISTINCT FROM 'never_dispatched' THEN
      PERFORM business.audit_record('system', NULL, 'payment.failed', 'payment_collection', v.collection_id, NULL,
                                    pg_catalog.json_build_object('reason', coalesce(v_ev ->> 'reason', 'rejected_by_stripe')));
    END IF;
  END IF;
  RETURN QUERY SELECT v_state, v_changed;
END;
$$;

-- As in 0014, plus cancel_reason and cancelled_at; a paid confirmation that
-- is not secured yet shows confirmation_pending only while the order still
-- awaits payment (a late success for a released order never shows it as
-- being confirmed). A collection past its 15 minutes keeps its own state
-- (pending: "still checking payment; do not pay again").
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
                         'id', pc.id, 'state', pc.state, 'amount_minor', pc.amount_minor, 'currency', pc.currency,
                         'requested_at', pc.initiated_at,
                         'checkout_url', CASE WHEN pc.state = 'pending' AND pc.session_status = 'open'
                                                   AND pc.provider_expires_at > pg_catalog.now()
                                                   AND o.lifecycle = 'awaiting_payment'
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

REVOKE ALL ON TABLE business.order_hold_review FROM PUBLIC;
REVOKE ALL ON TABLE business.order_hold_review_job FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_hold_review_schedule(uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_hold_review_start() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_collection_closed() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_hold_review_leased(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_holds_release(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_hold_review_run(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_leased_reconcile(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_expire_admit(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_expire_record(uuid, uuid, bigint, uuid, text, integer, text) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_hold_review_run(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_expire_admit(uuid, uuid, bigint, uuid) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.payment_expire_record(uuid, uuid, bigint, uuid, text, integer, text) TO gk_worker;
