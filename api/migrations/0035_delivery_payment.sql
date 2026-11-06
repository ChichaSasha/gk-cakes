-- GK delivery quote, separate delivery payment and free-delivery waiver
-- (contracts r6.5 K03 "GK Delivery individually quoted by chef from £100 ...
-- separate collection attached to the same order ... free-delivery code
-- waives the whole eligible GK fee within per-code maximum driving distance;
-- chef records measured distance/source and approves eligibility ... Chef may
-- attach a later delivery quote to the same versioned order ... Changed amount
-- requires agreement; no double use ... A payable separate delivery remains
-- awaiting separate payment and cannot be dispatched unpaid; its payment never
-- restarts cake preparation/deadline"; K04 payment authority and clocks; K05
-- delivery refunds chef-only; K06 "chef quote and paid/waived separate
-- collection required before dispatch"; K08 audit without personal data; K09
-- expand-only; K11; design GK-017 r02b state 15 and Creator decision Q9:
-- "Unpaid delivery quote: deadline 48 h before handover, then the same
-- choice").
--
-- Chef quote (business.order_delivery_quote, chef). On a gk_delivery order
-- whose cake is visibly confirmed (lifecycle confirmed, in_production or
-- ready, secured, never dispatched, no unresolved K11 command), with no
-- delivery collection in flight and the current quote not paid: amount in
-- whole pence from 10000 (K03 "from £100") to 99999999 (Stripe's GBP
-- maximum for one charge), an optional chef
-- note (staff only). The pay-by instant is the original handover - 48 h
-- (Q9); quoting at or after it is refused (409 delivery_pay_by_passed). A new
-- quote replaces the current one (also a waived one, which then needs a new
-- distance review); quotes are immutable rows, the current one is the
-- latest. The customer's payment of the current quote is the agreement to its
-- amount. Versioned (order version + 1), idempotent (K00 command receipt; the
-- same key replays, also during a restore freeze), refused during a restore
-- freeze (GK503), audited without the note.
--
-- Free-delivery waiver (business.order_delivery_waiver, chef). Only when the
-- order's code application is a consumed free_delivery code (the one
-- order-level use; nothing is consumed again) and the current quote is
-- unpaid, not waived and has no payment in flight: records the measured
-- driving distance (metres) and its source and the chef's eligibility
-- approval, immutably; the current quote is then waived (nothing to pay). No
-- per-code maximum distance exists in the data model yet (no invented
-- radius): the chef's approval is the eligibility decision. No other waiver
-- kind exists. Versioned, idempotent, freeze-refused, audited without the
-- distance or its source.
--
-- Customer payment (business.delivery_payment_initiate, API). The order's
-- owner or a guest capability with "pay" (the cake payment's access) pays the
-- current quote at the version they saw: a payment_collection of kind
-- 'delivery' for exactly the quote amount, linked to the quote, dispatched by
-- the existing payment worker (Stripe Checkout under K11, lease fencing,
-- freeze-first, the same session lifetime and expire rules). Refused at or
-- after pay-by (409 delivery_pay_by_passed), for a quote that is not the
-- current one (409 quote_not_current), a waived or paid quote, or while a
-- delivery collection is in flight (409 payment_in_progress: do not pay
-- again).
--
-- Settlement. The webhook and reconcile settle a delivery collection exactly
-- like a cake collection, by its own ID. A paid verdict records the
-- collection succeeded and, instead of the cake's paid confirmation command,
-- its delivery outcome (business.delivery_payment_outcome, immutable): 'paid'
-- when the collection's quote is still the order's current quote, not waived,
-- not already paid by another collection, and the order is confirmed, in
-- production or ready; otherwise 'unmatched' (reason quote_replaced,
-- quote_waived, already_paid, order_not_open or no_quote). Real money is
-- always kept (K04): an unmatched payment raises the chef attention flag
-- delivery_payment_unmatched until it is returned (the chef's agreed-exception
-- refund on that collection; delivery refunds are chef-only, K05). A payment
-- started before pay-by that succeeds after it is still 'paid' (K04: a valid
-- payment underway is honoured; delayed observation is not lateness). A
-- delivery payment never touches the order's lifecycle, paid_at, production
-- anchor or refund deadline.
--
-- Policy basis (K05). An unmatched payment is not what the order cost:
-- order_paid_minor (the basis of every percentage: cancellation 100%/50%,
-- no-show 50%, previews, notices) leaves it out, and it is an implicit floor
-- of its full amount in refund_shares and refund_goal_compute, so a goal is
-- the policy target plus the stray in full (capped by every succeeded
-- collection) and the stray's own return never counts against the cake's
-- share. The chef's exception limit counts every succeeded collection. A
-- stray recorded on an order that already has a refund goal raises that
-- goal by the stray and reserves it at once (K05 cumulative goal). A
-- cancellation that returns a stray states it beside the policy amount
-- (preview, outcome evidence, cancellation block and notice:
-- unmatched_minor).
--
-- Never dispatched. A delivery collection still created whose Stripe call
-- was never admitted 15 minutes after its request holds no money (no
-- session can exist): it is not in flight, is flagged delivery_payment_stuck
-- and the next quote, waiver or payment closes it as never dispatched, as
-- the cake's hold review does (payment_dispatch_fix/admit then answer
-- closed).
--
-- Line item. payment_dispatch_kind (worker, lease-fenced) gives the worker
-- the collection's kind, so a delivery's Checkout Session line item reads
-- "GK Cakes delivery".
--
-- Dispatch. fulfillment_refusal accepts a 'paid' collection of the current
-- quote (succeeded, no recorded mismatch) or a waiver of it; otherwise 409
-- delivery_not_paid.
--
-- Read models. order_view adds delivery on gk_delivery orders: {state
-- awaiting_quote | quoted | payment_checking | paid | waived | pay_by_passed,
-- quote {id, amount_minor, currency, quoted_at, pay_by} | null, payable,
-- paid_minor (when paid), waived {at} (when waived), payment (the current
-- quote's latest delivery collection, with its checkout link only while it can
-- be used)}. payable and pay_by_passed depend on the clock: the database clock
-- here, the API's clock in the API (as late_payment's acceptable). payment
-- (the cake's) now reads only cake collections. Staff: the chef gets
-- delivery_history (quotes with note, waivers with distance and source,
-- payments with outcome); technical support the same without money, note or
-- distance source; fulfilment delivery.state only.
--
-- Expand-only (K09): new tables, a nullable column, a new CHECK, new
-- functions; replaced functions keep their signatures, results, security and
-- grants. Lock order (K00): the order, its collections by ID, quotes and
-- waivers, then history and audit.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------- schema

CREATE TABLE business.delivery_quote (
  id            uuid PRIMARY KEY,
  order_id      uuid NOT NULL REFERENCES business.orders (id),
  amount_minor  bigint NOT NULL CHECK (amount_minor BETWEEN 10000 AND 99999999),
  currency      text NOT NULL DEFAULT 'GBP' CHECK (currency = 'GBP'),
  -- The chef's note (staff only, never audited).
  note          text NULL CHECK (note = btrim(note) AND char_length(note) BETWEEN 1 AND 500
                                 AND note !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  -- The order version the quote was made at (the quote bumps it).
  order_version integer NOT NULL CHECK (order_version >= 1),
  -- Q9: the original handover - 48 hours.
  pay_by        timestamptz NOT NULL,
  quoted_by     uuid NOT NULL REFERENCES business.account (id),
  -- The persisted receipt instant.
  quoted_at     timestamptz NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (order_id, order_version)
);
CREATE TRIGGER delivery_quote_immutable BEFORE UPDATE OR DELETE ON business.delivery_quote
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER delivery_quote_no_truncate BEFORE TRUNCATE ON business.delivery_quote
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- The chef's free-delivery eligibility review of one quote (K03).
CREATE TABLE business.delivery_waiver (
  id                   uuid PRIMARY KEY,
  order_id             uuid NOT NULL REFERENCES business.orders (id),
  quote_id             uuid NOT NULL UNIQUE REFERENCES business.delivery_quote (id),
  -- The order's one code application (a free_delivery code); never a second use.
  code_application_id  uuid NOT NULL REFERENCES business.code_application (id),
  distance_m           integer NOT NULL CHECK (distance_m BETWEEN 1 AND 1000000),
  distance_source      text NOT NULL CHECK (distance_source = btrim(distance_source)
                                            AND char_length(distance_source) BETWEEN 1 AND 200
                                            AND distance_source !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  eligibility_approved boolean NOT NULL CHECK (eligibility_approved),
  order_version        integer NOT NULL CHECK (order_version >= 1),
  approved_by          uuid NOT NULL REFERENCES business.account (id),
  approved_at          timestamptz NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX delivery_waiver_order_idx ON business.delivery_waiver (order_id);
CREATE TRIGGER delivery_waiver_immutable BEFORE UPDATE OR DELETE ON business.delivery_waiver
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER delivery_waiver_no_truncate BEFORE TRUNCATE ON business.delivery_waiver
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- A delivery collection pays one quote.
ALTER TABLE business.payment_collection ADD COLUMN delivery_quote_id uuid NULL REFERENCES business.delivery_quote (id);
ALTER TABLE business.payment_collection
  ADD CONSTRAINT payment_collection_delivery_quote CHECK (delivery_quote_id IS NULL OR kind = 'delivery');
CREATE INDEX payment_collection_delivery_quote_idx ON business.payment_collection (delivery_quote_id)
  WHERE delivery_quote_id IS NOT NULL;

-- What a succeeded delivery collection settled (see the header).
CREATE TABLE business.delivery_payment_outcome (
  collection_id uuid PRIMARY KEY REFERENCES business.payment_collection (id),
  order_id      uuid NOT NULL REFERENCES business.orders (id),
  quote_id      uuid NULL REFERENCES business.delivery_quote (id),
  outcome       text NOT NULL CHECK (outcome IN ('paid', 'unmatched')),
  reason        text NULL CHECK (reason IN ('no_quote', 'quote_replaced', 'quote_waived', 'already_paid', 'order_not_open')),
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT delivery_payment_outcome_reason CHECK ((outcome = 'paid') = (reason IS NULL)
                                                   AND (outcome <> 'paid' OR quote_id IS NOT NULL))
);
CREATE UNIQUE INDEX delivery_payment_outcome_paid_idx ON business.delivery_payment_outcome (quote_id) WHERE outcome = 'paid';
CREATE INDEX delivery_payment_outcome_order_idx ON business.delivery_payment_outcome (order_id);
CREATE TRIGGER delivery_payment_outcome_immutable BEFORE UPDATE OR DELETE ON business.delivery_payment_outcome
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER delivery_payment_outcome_no_truncate BEFORE TRUNCATE ON business.delivery_payment_outcome
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- ------------------------------------------------------------ helpers

-- Owner-internal: the order's current delivery quote (the latest; NULL
-- before the first).
CREATE FUNCTION business.delivery_current_quote(p_order uuid)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT q.id FROM business.delivery_quote q WHERE q.order_id = p_order ORDER BY q.order_version DESC LIMIT 1;
$$;

-- Owner-internal: a delivery collection whose Stripe call was never
-- admitted 15 minutes after it was requested (the later of the request
-- instant and its insertion, as the cake's never-dispatched cleanup, 0015):
-- no Checkout Session can exist for it (admission records
-- dispatch_started_at under the collection lock before any call), so it can
-- hold no money. It is not in flight, and the next delivery command closes
-- it (business.delivery_close_never_dispatched).
CREATE FUNCTION business.delivery_collection_stuck(p_c business.payment_collection)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_c.kind = 'delivery' AND p_c.state = 'created' AND p_c.dispatch_started_at IS NULL
     AND pg_catalog.now() >= GREATEST(p_c.initiated_at, p_c.created_at) + interval '15 minutes';
$$;

-- Owner-internal: whether a delivery collection of the order may still
-- succeed or is under reconciliation (K04: no second collection; a recorded
-- mismatch, even on a succeeded one, is checked before anything else). A
-- stuck collection (never admitted to Stripe) is not in flight.
CREATE FUNCTION business.delivery_in_flight(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.payment_collection pc
                 WHERE pc.order_id = p_order AND pc.kind = 'delivery'
                   AND (pc.state IN ('created', 'pending', 'unknown') OR pc.mismatch_reason IS NOT NULL)
                   AND NOT business.delivery_collection_stuck(pc));
$$;

-- Owner-internal: close order p_order's stuck delivery collections as never
-- dispatched (cancelled, cancel_reason never_dispatched: payment_dispatch_fix
-- and payment_dispatch_admit then answer closed, so no Stripe call is ever
-- made). The caller holds the order and its collections (checked under the
-- collection lock, as the hold review does for the cake). Returns how many.
CREATE FUNCTION business.delivery_close_never_dispatched(p_order uuid)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid;
  v_count integer := 0;
BEGIN
  FOR v_id IN SELECT pc.id FROM business.payment_collection pc
              WHERE pc.order_id = p_order AND business.delivery_collection_stuck(pc)
              ORDER BY pc.id FOR UPDATE LOOP
    UPDATE business.payment_collection pc
    SET state = 'cancelled', cancel_reason = 'never_dispatched', version = pc.version + 1, updated_at = pg_catalog.now()
    WHERE pc.id = v_id;
    PERFORM business.audit_record('system', NULL, 'payment.cancelled', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('collection_id', v_id, 'reason', 'never_dispatched'));
    v_count := v_count + 1;
  END LOOP;
  RETURN v_count;
END;
$$;

-- Owner-internal: delivery money held for order p_order that paid no
-- current quote (outcome unmatched) and is not returned yet (the
-- collection's succeeded refunds deducted).
CREATE FUNCTION business.delivery_unmatched_minor(p_order uuid)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(sum(GREATEST(0, b.paid_minor - b.succeeded_minor)), 0)::bigint
  FROM business.delivery_payment_outcome d
  CROSS JOIN LATERAL business.refund_balance(d.collection_id) b
  WHERE d.order_id = p_order AND d.outcome = 'unmatched';
$$;

-- Owner-internal: the collection that paid quote p_quote (outcome paid,
-- succeeded, no recorded mismatch), or NULL.
CREATE FUNCTION business.delivery_paid_collection(p_quote uuid)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pc.id FROM business.delivery_payment_outcome d
  JOIN business.payment_collection pc ON pc.id = d.collection_id
  WHERE d.quote_id = p_quote AND d.outcome = 'paid' AND pc.state = 'succeeded' AND pc.mismatch_reason IS NULL;
$$;

-- Owner-internal: whether the order's delivery is settled for dispatch: its
-- current quote is waived or paid (K06).
CREATE FUNCTION business.delivery_settled(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce((SELECT EXISTS (SELECT 1 FROM business.delivery_waiver w WHERE w.quote_id = x.q)
                          OR business.delivery_paid_collection(x.q) IS NOT NULL
                   FROM (SELECT business.delivery_current_quote(p_order) AS q) x WHERE x.q IS NOT NULL), false);
$$;

-- Owner-internal: whether the order accepts delivery quotes, waivers and
-- payments now: GK delivery, cake visibly confirmed (confirmed,
-- in_production or ready; secured), never dispatched, no unresolved K11
-- command.
CREATE FUNCTION business.delivery_order_open(p_o business.orders)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_o.fulfillment_mode = 'gk_delivery'
     AND p_o.lifecycle IN ('confirmed', 'in_production', 'ready') AND p_o.recovery_state = 'secured'
     AND (p_o.paid_at IS NOT NULL OR p_o.free_confirmed_at IS NOT NULL)
     AND NOT EXISTS (SELECT 1 FROM business.fulfillment_event e WHERE e.order_id = p_o.id AND e.action = 'dispatch')
     AND NOT business.order_command_unresolved(p_o.id);
$$;

-- Owner-internal: the common input checks of the chef's delivery commands.
CREATE FUNCTION business.delivery_check_input(p_key text, p_body_sha256 text, p_expected_version integer, p_now timestamptz)
RETURNS void
LANGUAGE plpgsql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
END;
$$;

-- Owner-internal: record what succeeded delivery collection p_c settled (see
-- the header), once; the order is locked by the caller. An unmatched payment
-- on an order that already has a refund goal raises that goal by the stray
-- and reserves it (K05 cumulative goal). Returns the outcome.
CREATE FUNCTION business.delivery_payment_settle(p_o business.orders, p_c business.payment_collection)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_outcome text;
  v_reason text;
BEGIN
  SELECT d.outcome INTO v_outcome FROM business.delivery_payment_outcome d WHERE d.collection_id = p_c.id;
  IF FOUND THEN
    RETURN v_outcome;
  END IF;
  v_reason := CASE
    WHEN p_c.delivery_quote_id IS NULL THEN 'no_quote'
    WHEN p_o.lifecycle NOT IN ('confirmed', 'in_production', 'ready') THEN 'order_not_open'
    WHEN p_c.delivery_quote_id IS DISTINCT FROM business.delivery_current_quote(p_o.id) THEN 'quote_replaced'
    WHEN EXISTS (SELECT 1 FROM business.delivery_waiver w WHERE w.quote_id = p_c.delivery_quote_id) THEN 'quote_waived'
    WHEN EXISTS (SELECT 1 FROM business.delivery_payment_outcome d
                 WHERE d.quote_id = p_c.delivery_quote_id AND d.outcome = 'paid') THEN 'already_paid'
  END;
  v_outcome := CASE WHEN v_reason IS NULL THEN 'paid' ELSE 'unmatched' END;
  INSERT INTO business.delivery_payment_outcome (collection_id, order_id, quote_id, outcome, reason)
  VALUES (p_c.id, p_o.id, p_c.delivery_quote_id, v_outcome, v_reason);
  IF v_outcome = 'unmatched' AND EXISTS (SELECT 1 FROM business.refund_goal g WHERE g.order_id = p_o.id) THEN
    -- K05 cumulative goal: a goal recorded before this money arrived did
    -- not count it. Raise it now by the stray's implicit floor (the stray on
    -- top of what the goal already owes, never taken from the cake's share)
    -- and reserve the new allocation, as a recorded floor does (0022
    -- refund_delivery_failure_record). An unresolved refund elsewhere keeps
    -- its own course; one that later settles under this raised goal
    -- reserves the remainder once (refund_operation_settled).
    PERFORM business.refund_goal_apply(p_o.id, NULL, NULL, NULL, NULL, NULL, 'system', NULL);
    PERFORM business.refund_reserve(p_o.id);
  END IF;
  IF v_outcome = 'paid' THEN
    PERFORM business.audit_record('system', NULL, 'delivery.paid', 'order', p_o.id, NULL,
                                  pg_catalog.json_build_object('collection_id', p_c.id, 'quote_id', p_c.delivery_quote_id));
  ELSE
    PERFORM business.audit_record('system', NULL, 'delivery.payment_unmatched', 'order', p_o.id, NULL,
                                  pg_catalog.json_build_object('collection_id', p_c.id, 'quote_id', p_c.delivery_quote_id,
                                                               'reason', v_reason));
  END IF;
  RETURN v_outcome;
END;
$$;

-- Owner-internal: {unmatched_minor} while the order holds delivery money
-- that paid no current quote and is not returned yet ("a payment was
-- received; the chef will return it"; do not pay that quote again), else {}.
CREATE FUNCTION business.order_delivery_unmatched(p_order uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN x.m > 0 THEN pg_catalog.jsonb_build_object('unmatched_minor', x.m) ELSE '{}'::jsonb END
  FROM (SELECT business.delivery_unmatched_minor(p_order) AS m) x;
$$;

-- Owner-internal: the delivery block of a gk_delivery order (NULL
-- otherwise; see the header). payable ignores the clock (the API applies
-- its own: payable and now before pay_by); state uses the database clock.
CREATE FUNCTION business.order_delivery_view(p_order business.orders)
RETURNS jsonb
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  q business.delivery_quote%ROWTYPE;
  w business.delivery_waiver%ROWTYPE;
  l business.payment_collection%ROWTYPE;
  v_paid uuid;
  v_state text;
  v jsonb;
BEGIN
  IF p_order.fulfillment_mode <> 'gk_delivery' THEN
    RETURN NULL;
  END IF;
  SELECT * INTO q FROM business.delivery_quote x WHERE x.id = business.delivery_current_quote(p_order.id);
  IF q.id IS NULL THEN
    RETURN pg_catalog.jsonb_build_object('state', 'awaiting_quote', 'quote', NULL, 'payable', false, 'payment', NULL)
           || business.order_delivery_unmatched(p_order.id);
  END IF;
  SELECT * INTO w FROM business.delivery_waiver x WHERE x.quote_id = q.id;
  v_paid := business.delivery_paid_collection(q.id);
  SELECT * INTO l FROM business.payment_collection pc
  WHERE pc.order_id = p_order.id AND pc.kind = 'delivery' AND pc.delivery_quote_id = q.id
  ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1;
  v_state := CASE WHEN w.id IS NOT NULL THEN 'waived'
                  WHEN v_paid IS NOT NULL THEN 'paid'
                  WHEN business.delivery_in_flight(p_order.id) THEN 'payment_checking'
                  WHEN pg_catalog.now() >= q.pay_by THEN 'pay_by_passed'
                  ELSE 'quoted' END;
  v := pg_catalog.jsonb_build_object(
    'state', v_state,
    'quote', pg_catalog.jsonb_build_object('id', q.id, 'amount_minor', q.amount_minor, 'currency', q.currency,
                                           'quoted_at', q.quoted_at, 'pay_by', q.pay_by),
    'payable', v_state IN ('quoted', 'pay_by_passed') AND business.delivery_order_open(p_order),
    'payment', CASE WHEN l.id IS NOT NULL THEN pg_catalog.jsonb_build_object(
                 'id', l.id,
                 'state', CASE WHEN l.state = 'cancelled' AND l.mismatch_reason IS NOT NULL THEN 'unknown' ELSE l.state END,
                 'amount_minor', l.amount_minor, 'currency', l.currency, 'requested_at', l.initiated_at,
                 'checkout_url', CASE WHEN l.state = 'pending' AND l.session_status = 'open'
                                           AND l.provider_expires_at > pg_catalog.now() AND l.expire_requested_at IS NULL
                                           AND l.mismatch_reason IS NULL AND business.delivery_order_open(p_order)
                                      THEN l.checkout_url END,
                 'expires_at', l.provider_expires_at) END);
  IF v_paid IS NOT NULL THEN
    v := v || pg_catalog.jsonb_build_object('paid_minor', (SELECT pc.amount_minor FROM business.payment_collection pc
                                                            WHERE pc.id = v_paid));
  END IF;
  IF w.id IS NOT NULL THEN
    v := v || pg_catalog.jsonb_build_object('waived', pg_catalog.jsonb_build_object('at', w.approved_at));
  END IF;
  RETURN v || business.order_delivery_unmatched(p_order.id);
END;
$$;

-- Owner-internal: the order's delivery quotes for staff, oldest first, with
-- their waiver and payments. p_chef adds the money, the chef's note and the
-- distance source (free text); technical support gets neither.
CREATE FUNCTION business.order_staff_delivery(p_order uuid, p_chef boolean)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(
           pg_catalog.jsonb_build_object(
             'id', q.id, 'order_version', q.order_version, 'quoted_at', q.quoted_at, 'pay_by', q.pay_by,
             'current', q.id = business.delivery_current_quote(p_order),
             'waiver', (SELECT pg_catalog.jsonb_build_object('id', w.id, 'at', w.approved_at, 'distance_m', w.distance_m,
                                                             'eligibility_approved', w.eligibility_approved,
                                                             'code_application_id', w.code_application_id)
                               || CASE WHEN p_chef THEN pg_catalog.jsonb_build_object('distance_source', w.distance_source)
                                       ELSE '{}'::jsonb END
                        FROM business.delivery_waiver w WHERE w.quote_id = q.id),
             'payments', (SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
                                   'collection_id', pc.id, 'state', pc.state, 'outcome', d.outcome, 'reason', d.reason)
                                   ORDER BY pc.created_at, pc.id), '[]'::jsonb)
                          FROM business.payment_collection pc
                          LEFT JOIN business.delivery_payment_outcome d ON d.collection_id = pc.id
                          WHERE pc.delivery_quote_id = q.id))
           || CASE WHEN p_chef THEN pg_catalog.jsonb_build_object('amount_minor', q.amount_minor, 'currency', q.currency,
                                                                  'note', q.note)
                   ELSE '{}'::jsonb END
           ORDER BY q.order_version), '[]'::jsonb)
  FROM business.delivery_quote q
  WHERE q.order_id = p_order;
$$;

-- ---------------------------------------------------------------- API

-- API: the chef's delivery quote on order p_order (see the header). Returns
-- the quote and whether it already existed.
CREATE FUNCTION business.order_delivery_quote(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_amount bigint, p_note text,
  p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  v_frozen boolean;
  v_o business.orders%ROWTYPE;
  v_current uuid;
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  PERFORM business.delivery_check_input(p_key, p_body_sha256, p_expected_version, p_now);
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_amount IS NULL OR p_amount < 10000 OR p_amount > 99999999 THEN
    RAISE EXCEPTION 'delivery_amount_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_note IS NOT NULL AND (p_note <> pg_catalog.btrim(p_note) OR pg_catalog.char_length(p_note) NOT BETWEEN 1 AND 500
                             OR p_note ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') THEN
    RAISE EXCEPTION 'note_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- The same key replays (the quote), also during a restore freeze.
  v_prior := business.command_claim(v_account, 'order.delivery_quote', p_order::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  -- Lock order (K00): the order, its collections by ID.
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  PERFORM business.delivery_close_never_dispatched(p_order);
  IF business.order_command_unresolved(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF NOT business.delivery_order_open(v_o) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF business.delivery_in_flight(p_order) THEN
    RAISE EXCEPTION 'payment_in_progress' USING ERRCODE = 'GK409';
  END IF;
  v_current := business.delivery_current_quote(p_order);
  IF v_current IS NOT NULL AND business.delivery_paid_collection(v_current) IS NOT NULL THEN
    -- Paid: the customer agreed to that amount; no retrospective edit.
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF p_now >= v_o.original_handover_at - interval '48 hours' THEN
    RAISE EXCEPTION 'delivery_pay_by_passed' USING ERRCODE = 'GK409';
  END IF;
  INSERT INTO business.delivery_quote (id, order_id, amount_minor, note, order_version, pay_by, quoted_by, quoted_at)
  VALUES (v_id, p_order, p_amount, p_note, v_o.version, v_o.original_handover_at - interval '48 hours', v_account, p_now);
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order;
  PERFORM business.audit_record('staff', v_account, 'delivery.quoted', 'order', p_order, NULL,
                                pg_catalog.json_build_object('quote_id', v_id, 'amount_minor', p_amount,
                                                             'replaced_quote_id', v_current));
  PERFORM business.command_finish(v_account, 'order.delivery_quote', p_order::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- API: the chef's free-delivery waiver of order p_order's current quote (see
-- the header). Returns the waiver and whether it already existed.
CREATE FUNCTION business.order_delivery_waiver(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_distance_m integer,
  p_distance_source text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  v_frozen boolean;
  v_o business.orders%ROWTYPE;
  v_quote uuid;
  v_code uuid;
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  PERFORM business.delivery_check_input(p_key, p_body_sha256, p_expected_version, p_now);
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_distance_m IS NULL OR p_distance_m < 1 OR p_distance_m > 1000000 THEN
    RAISE EXCEPTION 'distance_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_distance_source IS NULL OR p_distance_source <> pg_catalog.btrim(p_distance_source)
     OR pg_catalog.char_length(p_distance_source) NOT BETWEEN 1 AND 200
     OR p_distance_source ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]' THEN
    RAISE EXCEPTION 'distance_source_invalid' USING ERRCODE = 'GK422';
  END IF;
  v_prior := business.command_claim(v_account, 'order.delivery_waiver', p_order::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  PERFORM business.delivery_close_never_dispatched(p_order);
  IF business.order_command_unresolved(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF NOT business.delivery_order_open(v_o) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- K03: only the order's own free-delivery code waives (one order-level
  -- code application, already consumed at confirmation).
  SELECT ca.id INTO v_code FROM business.code_application ca
  JOIN business.discount_code_version cv ON cv.id = ca.code_version_id
  WHERE ca.order_id = p_order AND ca.state = 'consumed' AND cv.kind = 'free_delivery';
  IF v_code IS NULL THEN
    RAISE EXCEPTION 'waiver_not_eligible' USING ERRCODE = 'GK409';
  END IF;
  v_quote := business.delivery_current_quote(p_order);
  IF v_quote IS NULL THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF business.delivery_in_flight(p_order) THEN
    RAISE EXCEPTION 'payment_in_progress' USING ERRCODE = 'GK409';
  END IF;
  IF business.delivery_paid_collection(v_quote) IS NOT NULL
     OR EXISTS (SELECT 1 FROM business.delivery_waiver w WHERE w.quote_id = v_quote) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  INSERT INTO business.delivery_waiver (id, order_id, quote_id, code_application_id, distance_m, distance_source,
                                        eligibility_approved, order_version, approved_by, approved_at)
  VALUES (v_id, p_order, v_quote, v_code, p_distance_m, p_distance_source, true, v_o.version, v_account, p_now);
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order;
  PERFORM business.audit_record('staff', v_account, 'delivery.waived', 'order', p_order, NULL,
                                pg_catalog.json_build_object('quote_id', v_quote, 'waiver_id', v_id,
                                                             'code_application_id', v_code));
  PERFORM business.command_finish(v_account, 'order.delivery_waiver', p_order::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- API: the customer's payment of quote p_quote of order p_order (see the
-- header): a delivery collection and its dispatch job, as payment_initiate.
-- Returns the collection and whether it already existed (a replay).
CREATE FUNCTION business.delivery_payment_initiate(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_quote uuid, p_collection uuid, p_return_origin text, p_env text, p_account text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_l record;
  v_o business.orders%ROWTYPE;
  v_prior business.payment_collection%ROWTYPE;
  v_frozen boolean;
  q business.delivery_quote%ROWTYPE;
  v_job uuid;
BEGIN
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_quote IS NULL THEN
    RAISE EXCEPTION 'quote_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' OR p_account IS NULL OR p_account !~ '^acct_[A-Za-z0-9]{1,64}$'
     OR p_collection IS NULL OR p_return_origin IS NULL OR p_return_origin !~ '^https?://[^/?#[:space:]]+$' THEN
    RAISE EXCEPTION 'payment environment, account, collection and return origin are required';
  END IF;
  -- The cake payment's access: the owner or a guest capability with "pay".
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order, 'pay') l;
  v_o := v_l.o_order;
  -- The same actor (account, or guest capability) and key replays.
  SELECT * INTO v_prior FROM business.payment_collection c
  WHERE c.order_id = p_order AND c.request_key = p_key
    AND c.actor_account_id IS NOT DISTINCT FROM v_l.o_account AND c.guest_capability_id IS NOT DISTINCT FROM v_l.o_capability;
  IF FOUND THEN
    IF v_prior.request_sha256 <> p_body_sha256 OR v_prior.kind <> 'delivery' THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior.id, true;
    RETURN;
  END IF;
  -- K11: a restore freezes new collections.
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  PERFORM business.delivery_close_never_dispatched(p_order);
  IF v_o.fulfillment_mode <> 'gk_delivery' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- K04: no second collection while an earlier one may still succeed.
  IF business.delivery_in_flight(p_order) THEN
    RAISE EXCEPTION 'payment_in_progress' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF NOT business.delivery_order_open(v_o) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  SELECT * INTO q FROM business.delivery_quote x WHERE x.id = business.delivery_current_quote(p_order);
  IF q.id IS NULL OR q.id <> p_quote THEN
    RAISE EXCEPTION 'quote_not_current' USING ERRCODE = 'GK409';
  END IF;
  IF EXISTS (SELECT 1 FROM business.delivery_waiver w WHERE w.quote_id = q.id)
     OR business.delivery_paid_collection(q.id) IS NOT NULL THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- Q9: pay by the original handover - 48 h (equality has passed).
  IF p_now >= q.pay_by THEN
    RAISE EXCEPTION 'delivery_pay_by_passed' USING ERRCODE = 'GK409';
  END IF;

  v_job := business.job_enqueue(p_collection, NULL);
  INSERT INTO business.payment_collection
    (id, order_id, kind, delivery_quote_id, amount_minor, environment, provider_account_id, livemode, actor_kind,
     actor_account_id, guest_capability_id, request_key, request_sha256, return_origin, initiated_at)
  VALUES (p_collection, p_order, 'delivery', q.id, q.amount_minor, p_env, p_account, p_env = 'production',
          CASE WHEN v_l.o_account IS NULL THEN 'guest' ELSE 'customer' END, v_l.o_account, v_l.o_capability, p_key,
          p_body_sha256, p_return_origin, p_now);
  INSERT INTO business.payment_job (job_id, collection_id, purpose) VALUES (v_job, p_collection, 'dispatch');
  PERFORM business.audit_record(CASE WHEN v_l.o_account IS NULL THEN 'system' ELSE 'customer' END, v_l.o_account,
                                'payment.collection_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('collection_id', p_collection, 'amount_minor', q.amount_minor,
                                                             'kind', 'delivery', 'quote_id', q.id));
  RETURN QUERY SELECT p_collection, false;
END;
$$;

-- Worker: the kind of the leased dispatch's collection (cake or delivery),
-- for the Checkout Session line item it builds at the first dispatch.
-- VOLATILE: the lease check (payment_leased) locks the job row.
CREATE FUNCTION business.payment_dispatch_kind(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.payment_job := business.payment_leased_dispatch(p_job_id, p_worker, p_generation, p_operation_id);
BEGIN
  RETURN (SELECT pc.kind FROM business.payment_collection pc WHERE pc.id = v.collection_id);
END;
$$;

-- ------------------------------------------------- replaced functions

-- As in 0017, except that a paid delivery collection records its delivery
-- outcome (business.delivery_payment_settle) instead of the cake's paid
-- confirmation: no order command, no change to the order.
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
    -- the contradicting observation is kept beside it.
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
    WHERE pc.id = c.id
    RETURNING * INTO c;
    PERFORM business.audit_record('system', NULL, 'payment.succeeded', 'order', c.order_id, NULL,
                                  pg_catalog.json_build_object('collection_id', c.id, 'amount_minor', c.amount_minor));
    IF c.kind = 'delivery' THEN
      -- K03: a delivery payment never confirms, restarts or moves the cake.
      PERFORM business.delivery_payment_settle(v_o, c);
    ELSIF v_cmd IS NULL THEN
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

-- As in 0021, except that it is the K05 policy basis: an unmatched
-- delivery payment (paid no current quote, see the header) is not part of
-- what the order costs, so a percentage (50% after the deadline, no-show)
-- never takes from it and its own return never counts against the cake's
-- share. Every caller is a policy, notice or preview amount; the goal cap
-- and the chef's exception limit count every succeeded collection.
CREATE OR REPLACE FUNCTION business.order_paid_minor(p_order uuid)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(sum(pc.amount_minor), 0)::bigint
  FROM business.payment_collection pc
  WHERE pc.order_id = p_order AND pc.state = 'succeeded'
    AND NOT EXISTS (SELECT 1 FROM business.delivery_payment_outcome d
                    WHERE d.collection_id = pc.id AND d.outcome = 'unmatched');
$$;

-- As in 0022, plus: an unmatched delivery payment is an implicit floor of
-- its full amount (K05 "explicit collection floor", net its own returns): a
-- floored share with p_floors (refund_plan reserves it automatically, like a
-- recorded floor), and outside the proportional policy pool without
-- p_floors. Without unmatched payments the result is unchanged.
CREATE OR REPLACE FUNCTION business.refund_shares(p_order uuid, p_target bigint, p_floors boolean)
RETURNS TABLE (collection_id uuid, ord integer, share_minor bigint, exposure_minor bigint, room_minor bigint, blocked boolean,
               floored boolean)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH c AS (
    SELECT pc.id, CASE pc.kind WHEN 'cake' THEN 0 ELSE 1 END AS ord, b.paid_minor AS paid,
           d.collection_id IS NOT NULL AS um,
           CASE WHEN p_floors THEN LEAST(GREATEST(coalesce(f.floor_minor, 0),
                                                  CASE WHEN d.collection_id IS NOT NULL THEN b.paid_minor ELSE 0 END),
                                         b.paid_minor)
                ELSE 0 END AS fl,
           b.exposure_minor AS ex, b.room_minor AS room,
           (b.unresolved OR b.quarantined OR EXISTS (SELECT 1 FROM business.refund_operation ro
                                                    WHERE ro.collection_id = pc.id AND ro.mismatch_reason IS NOT NULL)) AS blocked,
           (f.id IS NOT NULL OR d.collection_id IS NOT NULL) AS floored
    FROM business.payment_collection pc
    CROSS JOIN LATERAL business.refund_balance(pc.id) b
    LEFT JOIN business.refund_floor f ON f.collection_id = pc.id
    LEFT JOIN business.delivery_payment_outcome d ON d.collection_id = pc.id AND d.outcome = 'unmatched'
    WHERE pc.order_id = p_order AND pc.state = 'succeeded'
  ), w AS (
    SELECT c.*, CASE WHEN p_floors THEN c.paid - c.fl WHEN c.um THEN 0 ELSE c.paid END AS wt,
           CASE WHEN p_floors OR NOT c.um THEN c.paid ELSE 0 END AS basis
    FROM c
  ), t AS (
    SELECT GREATEST(0, LEAST(p_target, coalesce(sum(w.basis), 0)) - coalesce(sum(w.fl), 0))::bigint AS tp,
           coalesce(sum(w.wt), 0)::bigint AS pw
    FROM w
  ), a AS (
    SELECT w.*, t.tp,
           CASE WHEN t.pw > 0 THEN (t.tp * w.wt) / t.pw ELSE 0 END AS base,
           CASE WHEN t.pw > 0 THEN (t.tp * w.wt) % t.pw ELSE 0 END AS rem
    FROM w, t
  ), r AS (
    SELECT a.*, row_number() OVER (ORDER BY a.rem DESC, a.ord, a.id) AS rn, a.tp - sum(a.base) OVER () AS leftover
    FROM a
  )
  SELECT r.id, r.ord, (r.fl + r.base + CASE WHEN r.rn <= r.leftover THEN 1 ELSE 0 END)::bigint, r.ex, r.room, r.blocked,
         r.floored
  FROM r ORDER BY r.ord, r.id;
$$;

-- As in 0022, plus: an unmatched delivery payment counts as a floor of its
-- full amount, and the cap is every succeeded collection (order_paid_minor
-- is the policy basis since 0035). So a 50% target over cake 10000p with an
-- unmatched 12000p is 5000p + 12000p = 17000p, whether or not the 12000p was
-- returned first, and never 50% of 22000p.
CREATE OR REPLACE FUNCTION business.refund_goal_compute(p_order uuid, p_policy bigint)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH s AS (SELECT * FROM business.refund_shares(p_order, p_policy, false)),
  f AS (SELECT f.collection_id, f.floor_minor FROM business.refund_floor f WHERE f.order_id = p_order
        UNION ALL
        SELECT d.collection_id, pc.amount_minor
        FROM business.delivery_payment_outcome d
        JOIN business.payment_collection pc ON pc.id = d.collection_id
        WHERE d.order_id = p_order AND d.outcome = 'unmatched'
          AND NOT EXISTS (SELECT 1 FROM business.refund_floor rf WHERE rf.collection_id = d.collection_id))
  SELECT LEAST((SELECT coalesce(sum(pc.amount_minor), 0) FROM business.payment_collection pc
                WHERE pc.order_id = p_order AND pc.state = 'succeeded'),
               GREATEST(p_policy,
                        CASE WHEN EXISTS (SELECT 1 FROM f JOIN s ON s.collection_id = f.collection_id)
                             THEN coalesce((SELECT sum(CASE WHEN f.collection_id IS NOT NULL
                                                            THEN GREATEST(f.floor_minor, s.exposure_minor)
                                                            ELSE GREATEST(s.share_minor, s.exposure_minor) END)
                                            FROM s LEFT JOIN f ON f.collection_id = s.collection_id), 0)
                             ELSE 0 END))::bigint;
$$;

-- As in 0024, except that the limit and the goal cap count every succeeded
-- collection (an unmatched delivery payment included; see order_paid_minor).
CREATE OR REPLACE FUNCTION business.refund_exception_record(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                                 p_expected_version integer, p_amount bigint, p_currency text,
                                                 p_collections uuid[], p_amounts bigint[], p_reason text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid;
  v_o business.orders%ROWTYPE;
  v_g business.refund_goal%ROWTYPE;
  v_paid bigint;
  v_returned bigint;
  v_target bigint;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_allocs json;
  v_ops jsonb := '[]'::jsonb;
  v_op uuid;
  a record;
BEGIN
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_amount IS NULL OR p_amount < 1 OR p_amount > 100000000 THEN
    RAISE EXCEPTION 'amount_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_currency IS DISTINCT FROM 'GBP' THEN
    RAISE EXCEPTION 'currency_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_collections IS NULL OR p_amounts IS NULL OR pg_catalog.cardinality(p_collections) NOT BETWEEN 1 AND 10
     OR pg_catalog.cardinality(p_collections) <> pg_catalog.cardinality(p_amounts)
     OR pg_catalog.array_position(p_collections, NULL) IS NOT NULL OR pg_catalog.array_position(p_amounts, NULL) IS NOT NULL
     OR EXISTS (SELECT 1 FROM pg_catalog.unnest(p_amounts) x WHERE x < 1)
     OR (SELECT count(DISTINCT x) FROM pg_catalog.unnest(p_collections) x) <> pg_catalog.cardinality(p_collections)
     OR (SELECT sum(x) FROM pg_catalog.unnest(p_amounts) x) <> p_amount THEN
    RAISE EXCEPTION 'allocations_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_reason IS NULL OR p_reason <> pg_catalog.btrim(p_reason) OR pg_catalog.char_length(p_reason) NOT BETWEEN 1 AND 500
     OR p_reason ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]' THEN
    RAISE EXCEPTION 'reason_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- The same key replays even during a restore freeze: it changes nothing.
  v_prior := business.command_claim(v_account, 'refund.exception', p_order::text, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  v_o := business.refund_chef_admit(p_order, p_expected_version);
  IF EXISTS (SELECT 1 FROM pg_catalog.unnest(p_collections) x
             WHERE NOT EXISTS (SELECT 1 FROM business.payment_collection pc WHERE pc.id = x AND pc.order_id = p_order)) THEN
    RAISE EXCEPTION 'allocations_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF EXISTS (SELECT 1 FROM business.refund_operation ro
             WHERE ro.order_id = p_order AND ro.state IN ('reserved', 'submitting', 'pending', 'unknown', 'requires_action')) THEN
    RAISE EXCEPTION 'refund_in_progress' USING ERRCODE = 'GK409';
  END IF;
  -- Every succeeded collection, an unmatched delivery payment included:
  -- order_paid_minor is the policy basis since 0035 and leaves it out, but
  -- the chef may return it (the per-collection room still bounds each
  -- allocation, so nothing is returned twice).
  SELECT coalesce(sum(pc.amount_minor), 0)::bigint INTO v_paid
  FROM business.payment_collection pc WHERE pc.order_id = p_order AND pc.state = 'succeeded';
  SELECT coalesce(sum(b.succeeded_minor + b.reserved_minor), 0) INTO v_returned
  FROM business.payment_collection pc CROSS JOIN LATERAL business.refund_balance(pc.id) b
  WHERE pc.order_id = p_order;
  IF p_amount > v_paid - v_returned
     OR EXISTS (SELECT 1 FROM ROWS FROM (pg_catalog.unnest(p_collections), pg_catalog.unnest(p_amounts)) AS x(cid, amt)
                CROSS JOIN LATERAL business.refund_balance(x.cid) b
                WHERE x.amt > b.room_minor) THEN
    RAISE EXCEPTION 'refund_limit' USING ERRCODE = 'GK409';
  END IF;

  SELECT * INTO v_g FROM business.refund_goal g WHERE g.order_id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    v_target := LEAST(v_paid, v_returned + p_amount);
    INSERT INTO business.refund_goal (id, order_id, target_minor, reason, source, policy_target_minor)
    VALUES (pg_catalog.gen_random_uuid(), p_order, v_target, 'chef_exception', 'chef', 0)
    RETURNING * INTO v_g;
    PERFORM business.audit_record('staff', v_account, 'refund.goal_recorded', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_target, 'policy_target_minor', 0,
                                                               'reason', v_g.reason, 'exception_id', v_id));
  ELSE
    v_target := LEAST(v_paid, GREATEST(v_g.target_minor, v_returned) + p_amount);
    UPDATE business.refund_goal g SET target_minor = v_target, version = g.version + 1, updated_at = pg_catalog.now()
    WHERE g.id = v_g.id RETURNING * INTO v_g;
    PERFORM business.audit_record('staff', v_account, 'refund.goal_raised', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('goal_id', v_g.id, 'version', v_g.version,
                                                               'target_minor', v_target,
                                                               'policy_target_minor', v_g.policy_target_minor,
                                                               'reason', v_g.reason, 'exception_id', v_id));
  END IF;
  FOR a IN SELECT x.cid, x.amt FROM ROWS FROM (pg_catalog.unnest(p_collections), pg_catalog.unnest(p_amounts)) AS x(cid, amt)
           JOIN business.payment_collection pc ON pc.id = x.cid
           ORDER BY CASE pc.kind WHEN 'cake' THEN 0 ELSE 1 END, pc.id LOOP
    v_op := business.refund_reserve_one(p_order, a.cid, v_g, a.amt);
    v_ops := v_ops || pg_catalog.jsonb_build_array(pg_catalog.jsonb_build_object(
               'collection_id', a.cid, 'amount_minor', a.amt, 'refund_operation_id', v_op));
  END LOOP;
  v_allocs := v_ops::json;
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order;
  INSERT INTO business.refund_exception (id, order_id, goal_id, goal_version, target_minor, amount_minor, allocations, reason,
                                         order_version, recorded_by)
  VALUES (v_id, p_order, v_g.id, v_g.version, v_target, p_amount, v_allocs, p_reason, v_o.version + 1, v_account);
  PERFORM business.audit_record('staff', v_account, 'refund.exception_recorded', 'order', p_order, NULL,
                                pg_catalog.json_build_object('exception_id', v_id, 'goal_version', v_g.version,
                                                             'target_minor', v_target, 'amount_minor', p_amount,
                                                             'allocations', v_allocs));
  PERFORM business.command_finish(v_account, 'refund.exception', p_order::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;


-- As in 0022, plus unmatched_minor (outside the hash, as
-- delivery_pending_minor): a stray delivery payment (0035) this cancellation
-- returns in full beside amount_minor (the policy amount, unchanged); the
-- allocations already include it.
CREATE OR REPLACE FUNCTION business.order_cancellation_preview(p_owner uuid, p_guest_digest bytea, p_order uuid, p_now timestamptz)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_l record;
  v_account uuid;
  v_capability uuid;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_rate smallint;
  v_paid bigint;
  v_amount bigint;
  v_alloc json;
  v_body json;
  v_hash text;
  v_p business.cancellation_preview%ROWTYPE;
  v_pending bigint;
  v_stray bigint;
BEGIN
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order, 'cancel') l;
  v_o := v_l.o_order;
  v_account := v_l.o_account;
  v_capability := v_l.o_capability;
  IF EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
             WHERE c.order_id = p_order AND c.command = 'cancel'
               AND NOT (r.state = 'secured' AND r.outcome ->> 'status' = 'refused'))
     OR NOT business.cancellation_eligible(v_o.lifecycle) THEN
    RAISE EXCEPTION 'cancellation_not_eligible' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  v_rate := business.cancellation_rate(v_o.full_refund_deadline_at, p_now, false);
  v_paid := business.order_paid_minor(p_order);
  v_amount := business.cancellation_amount(v_paid, v_rate);
  -- Allocated as the goal would be (combined with any recorded floor).
  v_alloc := business.cancellation_allocations(p_order, business.refund_goal_compute(p_order, v_amount));
  v_pending := business.refund_delivery_pending(p_order, business.refund_goal_compute(p_order, v_amount));
  -- A stray delivery payment (0035) returned in full with this cancellation,
  -- beside the policy amount (it is in the allocations); none without a goal.
  v_stray := CASE WHEN v_amount > 0 THEN business.delivery_unmatched_minor(p_order) ELSE 0 END;
  -- An unexpired identical preview of this actor and version is reused.
  SELECT * INTO v_p FROM business.cancellation_preview p
  WHERE p.order_id = p_order AND p.order_version = v_o.version
    AND p.actor_account_id IS NOT DISTINCT FROM v_account AND p.guest_capability_id IS NOT DISTINCT FROM v_capability
    AND p.rate = v_rate AND p.paid_minor = v_paid AND p.amount_minor = v_amount
    AND p.allocations::text = v_alloc::text AND p.full_refund_deadline_at = v_o.full_refund_deadline_at
    AND p.computed_at <= p_now AND p_now < p.expires_at
  ORDER BY p.computed_at DESC, p.id DESC LIMIT 1;
  IF FOUND THEN
    v_body := pg_catalog.json_build_object(
      'preview_id', v_p.id, 'order_id', p_order, 'order_version', v_p.order_version, 'rate', v_p.rate,
      'paid_minor', v_p.paid_minor, 'amount_minor', v_p.amount_minor, 'currency', 'GBP', 'allocations', v_p.allocations,
      'full_refund_deadline_at', v_p.full_refund_deadline_at, 'computed_at', v_p.computed_at, 'expires_at', v_p.expires_at);
    RETURN (v_body::jsonb || pg_catalog.jsonb_build_object('preview_hash', v_p.body_sha256,
                                                           'delivery_pending_minor', v_pending,
                                                           'unmatched_minor', v_stray))::json;
  END IF;
  v_body := pg_catalog.json_build_object(
    'preview_id', v_id, 'order_id', p_order, 'order_version', v_o.version, 'rate', v_rate,
    'paid_minor', v_paid, 'amount_minor', v_amount, 'currency', 'GBP', 'allocations', v_alloc,
    'full_refund_deadline_at', v_o.full_refund_deadline_at,
    'computed_at', p_now, 'expires_at', p_now + interval '10 minutes');
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_body::text, 'UTF8')), 'hex');
  INSERT INTO business.cancellation_preview (id, order_id, order_version, actor_account_id, guest_capability_id, rate,
                                             paid_minor, amount_minor, allocations, full_refund_deadline_at,
                                             computed_at, expires_at, body_sha256)
  VALUES (v_id, p_order, v_o.version, v_account, v_capability, v_rate, v_paid, v_amount, v_alloc,
          v_o.full_refund_deadline_at, p_now, p_now + interval '10 minutes', v_hash);
  RETURN (v_body::jsonb || pg_catalog.jsonb_build_object('preview_hash', v_hash, 'delivery_pending_minor', v_pending,
                                                         'unmatched_minor', v_stray))::json;
END;
$$;


-- As in 0022, plus: the outcome evidence (and audit) states unmatched_minor,
-- the stray delivery payment (0035) the goal returns in full beside the
-- policy refund_target_minor, when there is one.
CREATE OR REPLACE FUNCTION business.order_terminal_return(p_order uuid, p_version integer, p_reason text, p_rate smallint,
                                               p_source text, p_actor_kind text, p_account uuid, p_operation uuid,
                                               p_at timestamptz)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_paid bigint := business.order_paid_minor(p_order);
  v_target bigint := business.cancellation_amount(v_paid, p_rate);
  -- A stray delivery payment (0035) not returned yet: the goal returns it in
  -- full beside the policy target, so the outcome states it.
  v_stray bigint := CASE WHEN v_target > 0 THEN business.delivery_unmatched_minor(p_order) ELSE 0 END;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  UPDATE business.orders o
  SET lifecycle = 'cancelled', cancel_reason = p_reason, cancelled_at = p_at,
      recovery_state = 'cancellation_pending', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = p_order;
  IF v_target > 0 THEN
    PERFORM business.refund_goal_apply(p_order, p_reason, p_source, p_rate, v_paid, v_target, 'system', NULL);
    PERFORM business.refund_reserve(p_order);
  END IF;
  IF EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = p_order AND a.day = v_o.handover_day) THEN
    PERFORM business.capacity_release(p_order, v_o.handover_day);
  END IF;
  PERFORM business.audit_record(CASE p_actor_kind WHEN 'guest' THEN 'system' ELSE p_actor_kind END, p_account,
                                'order.cancelled', 'order', p_order, NULL,
                                CASE WHEN v_stray > 0
                                     THEN pg_catalog.json_build_object('operation_id', p_operation, 'reason', p_reason,
                                                                       'rate', p_rate, 'refund_target_minor', v_target,
                                                                       'unmatched_minor', v_stray)
                                     ELSE pg_catalog.json_build_object('operation_id', p_operation, 'reason', p_reason,
                                                                       'rate', p_rate, 'refund_target_minor', v_target) END);
  IF v_stray > 0 THEN
    RETURN pg_catalog.format(
      '{"status":"cancelled","evidence":{"order_version":%s,"reason":"%s","rate":%s,"paid_minor":%s,"refund_target_minor":%s,'
      '"unmatched_minor":%s}}',
      p_version + 1, p_reason, p_rate, v_paid, v_target, v_stray);
  END IF;
  RETURN pg_catalog.format(
    '{"status":"cancelled","evidence":{"order_version":%s,"reason":"%s","rate":%s,"paid_minor":%s,"refund_target_minor":%s}}',
    p_version + 1, p_reason, p_rate, v_paid, v_target);
END;
$$;


-- As in 0032, plus one sentence in the cancelled notice when its outcome
-- states a stray delivery payment returned in full (unmatched_minor, 0035):
-- the customer is told the total actually returned.
CREATE OR REPLACE FUNCTION business.order_notice_render(p_kind text, p_o business.orders, p_facts json, p_origin text,
                                             OUT subject text, OUT body_text text, OUT body_html text)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_text text[] := ARRAY[]::text[];
  v_html text[] := ARRAY[]::text[];
  v_p text;
  v_paid bigint := business.order_paid_minor(p_o.id);
  v_free boolean := p_o.payment_requirement = 'none';
  v_link text := p_origin || '/orders/' || p_o.id::text;
  v_when text := business.notice_day(business.order_handover_at(p_o)) || ', ' || business.notice_time(business.order_handover_at(p_o))
                 || ' to ' || business.notice_time(p_o.handover_ends_at) || ' (London time)';
  v_mode text := CASE p_o.fulfillment_mode WHEN 'collection' THEN 'collection' WHEN 'customer_courier' THEN 'courier collection'
                                           ELSE 'handover' END;
  v_items text;
  v_totals text;
  v_rate integer;
  v_target bigint;
  v_returned boolean;
  v_from text;
  v_to text;
  v_before bigint;
  v_new bigint;
  v_style constant text := 'margin:0 0 16px';
  v_link_style constant text := 'color:#5b1a2e';
BEGIN
  SELECT pg_catalog.string_agg(
           '- ' || CASE WHEN i.quantity > 1 THEN i.quantity::text || ' × ' ELSE '' END || c.name || ' (' || r.flavour_name
           || ', ' || business.notice_size(r.size_code) || CASE WHEN i.physical_lot_id IS NOT NULL THEN ', ready-made' ELSE '' END
           || '): ' || business.notice_money(i.unit_price_minor) || CASE WHEN i.quantity > 1 THEN ' each' ELSE '' END,
           E'\n' ORDER BY i.line_no)
  INTO v_items
  FROM business.order_item i
  JOIN business.variant va ON va.id = i.variant_id
  JOIN business.cake c ON c.id = va.cake_id
  JOIN business.recipe_version r ON r.id = i.recipe_version_id
  WHERE i.order_id = p_o.id;
  v_totals := CASE WHEN p_o.discount_minor > 0
                   THEN 'Cakes: ' || business.notice_money(p_o.subtotal_minor) || E'\nDiscount: −'
                        || business.notice_money(p_o.discount_minor) || E'\n' ELSE '' END;

  IF p_kind = 'confirmed' THEN
    subject := 'Your GK Cakes order is confirmed';
    v_text := v_text || CASE WHEN v_free THEN 'Thank you. Your order is confirmed. It is free, so there is nothing to pay.'
                             ELSE 'Thank you. Your order is confirmed and we have received your payment of '
                                  || business.notice_money(v_paid) || '.' END;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\n' || pg_catalog.upper(pg_catalog.left(v_mode, 1))
                         || pg_catalog.substr(v_mode, 2) || ': ' || v_when);
    v_text := v_text || (E'Your cakes:\n' || coalesce(v_items, '') || E'\n' || v_totals
                         || CASE WHEN v_free THEN 'Total: £0' ELSE 'Total paid: ' || business.notice_money(v_paid) END);
    v_text := v_text || CASE p_o.fulfillment_mode
      WHEN 'collection' THEN 'Collection is from St Katharine Docks, London. The chef will send you the exact address and instructions about two days before.'
      WHEN 'customer_courier' THEN 'Please arrange and pay for your own courier to collect at the time above. The chef will send the exact collection address and instructions about two days before. Once the cake is handed to your courier, it is their responsibility.'
      ELSE 'Delivery by GK Cakes is agreed and paid separately. The chef will contact you about it.' END;
    IF NOT v_free THEN
      -- Against the command's persisted receipt (as the cancellation rate
      -- is), so a replayed transition renders the same bytes.
      v_text := v_text || CASE WHEN p_o.full_refund_deadline_at > (p_facts ->> 'received_at')::timestamptz
        THEN 'If you need to cancel: cancel before ' || business.notice_time(p_o.full_refund_deadline_at) || ' on '
             || business.notice_day(p_o.full_refund_deadline_at)
             || ' (London time) for a full refund of what you paid. From then on, the refund is 50%.'
        ELSE 'If you need to cancel: the refund is now 50% of what you paid.' END;
    END IF;
  ELSIF p_kind = 'paid_awaiting_agreement' THEN
    subject := 'Payment received for your GK Cakes order';
    v_text := v_text || ('We have received your payment of ' || business.notice_money(v_paid)
                         || '. It arrived after the preparation time needed for your chosen date, so your order is not confirmed yet.');
    v_text := v_text || 'The chef will contact you about whether the date can still work. If you do not agree a handover, the full amount you paid will be returned.'::text;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nRequested ' || v_mode || ': ' || v_when);
    v_text := v_text || (E'Your cakes:\n' || coalesce(v_items, '') || E'\n' || v_totals || 'Paid: ' || business.notice_money(v_paid));
  ELSIF p_kind = 'released' THEN
    subject := 'Your GK Cakes order has been released';
    v_text := v_text || CASE WHEN p_o.cancel_reason = 'free_not_confirmed'
      THEN 'Your free order has been released because it was not confirmed in time. The date and cakes are no longer held for you.'
      ELSE 'Your order has been released because its payment was not completed. The date and cakes are no longer held for you.' END;
    IF NOT v_free THEN
      v_text := v_text || 'We have no record of a successful payment for this order. If a payment does arrive later, the full amount will be returned.'::text;
    END IF;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nRequested ' || v_mode || ': ' || v_when);
  ELSIF p_kind = 'cancelled' THEN
    subject := 'Your GK Cakes order is cancelled';
    v_rate := (p_facts ->> 'rate')::integer;
    v_target := coalesce((p_facts ->> 'refund_target_minor')::bigint, 0);
    v_text := v_text || CASE p_facts ->> 'reason'
      WHEN 'chef_cancelled' THEN 'We are sorry: the chef has had to cancel your order.'
      WHEN 'late_payment_returned' THEN 'Your order has been cancelled because a handover could not be agreed after your payment arrived.'
      ELSE 'Your order has been cancelled as you asked.' END;
    v_returned := coalesce((p_facts ->> 'refund_returned')::boolean, false);
    v_text := v_text || CASE
      WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN 'There was no payment for this order, so there is nothing to refund.'
      WHEN v_target = 0 THEN 'No refund is due for this order.'
      ELSE CASE WHEN v_returned THEN 'Refund: ' ELSE 'Refund requested: ' END || business.notice_money(v_target)
           || CASE WHEN v_rate = 100 THEN ', the full amount you paid.'
                   ELSE ', which is ' || v_rate::text || '% of the ' || business.notice_money((p_facts ->> 'paid_minor')::bigint)
                        || ' you paid, because the full-refund deadline had been reached.' END END;
    IF v_target > 0 THEN
      v_text := v_text || CASE WHEN v_returned THEN 'It has already been returned to you.'
                               ELSE 'We have asked for this refund and will email you once it has been returned.' END;
    END IF;
    -- A stray delivery payment (0035): returned in full beside the refund
    -- above (only outcomes that carry it; earlier notices render as before).
    IF coalesce((p_facts ->> 'unmatched_minor')::bigint, 0) > 0 THEN
      v_text := v_text || ('We are also returning in full a delivery payment of '
                           || business.notice_money((p_facts ->> 'unmatched_minor')::bigint)
                           || ' that could not be used for your order, so ' || business.notice_money(v_target
                              + (p_facts ->> 'unmatched_minor')::bigint)
                           || CASE WHEN v_returned THEN ' has been returned to you in total.'
                                   ELSE ' is being returned to you in total.' END);
    END IF;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nIt was planned for: ' || v_when);
  ELSIF p_kind = 'refund_returned' THEN
    subject := 'Your GK Cakes refund has been returned';
    v_text := v_text || ('We have returned ' || business.notice_money((p_facts ->> 'target_minor')::bigint)
                         || CASE WHEN (p_facts ->> 'goal_version')::integer > 1 THEN ' in total' ELSE '' END
                         || ' to you for this order. Depending on your bank, it can take a few working days to show on your statement.');
    v_text := v_text || ('Order reference: ' || p_o.id::text);
  ELSIF p_kind = 'no_show' THEN
    subject := CASE WHEN p_o.fulfillment_mode = 'gk_delivery' THEN 'Your GK Cakes order could not be handed over'
                    ELSE 'Your GK Cakes order was not collected' END;
    v_text := v_text || ('Your order was not ' || CASE WHEN p_o.fulfillment_mode = 'gk_delivery' THEN 'handed over'
                                                       ELSE 'collected' END
                         || ' at the agreed time (' || v_when || '), so it has been closed as not collected.');
    -- The amount this no-show newly reserved (earlier refunds count against
    -- the 50%, K05 cumulative goal), never the nominal 50% on its own.
    v_target := coalesce((p_facts ->> 'refund_target_minor')::bigint, 0);
    v_before := coalesce((p_facts ->> 'returned_before_minor')::bigint, 0);
    v_new := coalesce((p_facts ->> 'new_refund_minor')::bigint, 0);
    v_returned := coalesce((p_facts ->> 'refund_returned')::boolean, false);
    v_text := v_text || CASE
      WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN 'There was no payment for this order, so there is nothing to refund.'
      ELSE 'Our terms return 50% of what you paid when an order is not collected: ' || business.notice_money(v_target)
           || ' of the ' || business.notice_money((p_facts ->> 'paid_minor')::bigint) || ' you paid.' END;
    IF v_new > 0 THEN
      v_text := v_text || (CASE WHEN v_returned THEN 'Refund: ' ELSE 'Refund requested: ' END || business.notice_money(v_new)
                           || CASE WHEN v_before > 0 THEN ' (the ' || business.notice_money(v_before)
                                                          || ' already refunded counts towards it).' ELSE '.' END);
      v_text := v_text || CASE WHEN v_returned THEN 'It has already been returned to you.'
                               ELSE 'We have asked for this refund and will email you once it has been returned.' END;
    ELSIF v_target > 0 AND v_before >= v_target THEN
      v_text := v_text || ('The ' || business.notice_money(v_before) || ' already refunded covers this, so nothing more is due.');
    ELSIF v_target > v_before THEN
      v_text := v_text || ('The chef will contact you about the remaining ' || business.notice_money(v_target - v_before) || '.');
    END IF;
    v_text := v_text || ('If you think this is a mistake, please reply to this email.' || E'\nOrder reference: ' || p_o.id::text);
  ELSIF p_kind IN ('pickup_moved', 'appointment_changed') THEN
    v_from := business.notice_day((p_facts ->> 'from_starts_at')::timestamptz) || ', '
              || business.notice_time((p_facts ->> 'from_starts_at')::timestamptz) || ' to '
              || business.notice_time((p_facts ->> 'from_ends_at')::timestamptz) || ' (London time)';
    v_to := business.notice_day((p_facts ->> 'to_starts_at')::timestamptz) || ', '
            || business.notice_time((p_facts ->> 'to_starts_at')::timestamptz) || ' to '
            || business.notice_time((p_facts ->> 'to_ends_at')::timestamptz) || ' (London time)';
    IF p_kind = 'pickup_moved' THEN
      subject := 'Your GK Cakes ' || v_mode || ' time has changed';
      v_text := v_text || ('Your ' || v_mode || ' time has been changed as you asked.')::text;
    ELSE
      subject := 'Your GK Cakes ' || v_mode || ' date or time has changed';
      v_text := v_text || ('As agreed with you, the chef has changed your ' || v_mode || ' date or time.')::text;
    END IF;
    v_text := v_text || ('New time: ' || v_to || E'\nPrevious time: ' || v_from || E'\nOrder reference: ' || p_o.id::text);
    IF p_kind = 'pickup_moved' THEN
      v_text := v_text || ('You can change the time yourself to another available time on the same day until six hours before it starts. '
                           || 'For anything else, please contact the chef.')::text;
    END IF;
    IF NOT v_free THEN
      v_text := v_text || CASE WHEN p_o.full_refund_deadline_at > (p_facts ->> 'received_at')::timestamptz
        THEN 'Changing the time does not change your cancellation terms: cancel before '
             || business.notice_time(p_o.full_refund_deadline_at) || ' on ' || business.notice_day(p_o.full_refund_deadline_at)
             || ' (London time) for a full refund of what you paid. From then on, the refund is 50%.'
        ELSE 'Changing the time does not change your cancellation terms: the refund is 50% of what you paid.' END;
    END IF;
  ELSE
    RAISE EXCEPTION 'unknown order notice %', p_kind USING ERRCODE = 'invalid_parameter_value';
  END IF;

  -- Paragraphs: plain text separated by blank lines; HTML paragraphs with
  -- line breaks; then the link paragraph and the footer.
  FOREACH v_p IN ARRAY v_text LOOP
    v_html := v_html || ('<p style="' || v_style || '">' || pg_catalog.replace(business.notice_html(v_p), E'\n', '<br>') || '</p>');
  END LOOP;
  -- A released order has no order page paragraph (and no invitation to
  -- order again: no marketing).
  IF p_kind <> 'released' THEN
    v_text := v_text || ('Your order page: ' || v_link
                         || E'\nIf you ordered without signing in, open this link on the same device and browser you ordered with.');
    v_html := v_html || ('<p style="' || v_style || '">Your order page: <a href="' || business.notice_html(v_link)
                         || '" style="' || v_link_style || '">' || business.notice_html(v_link)
                         || '</a><br>If you ordered without signing in, open this link on the same device and browser you ordered with.</p>');
  END IF;
  v_p := E'GK Cakes, London\nYou can reply to this email if you have a question. You are receiving it because of your order with GK Cakes.';
  body_text := E'Hello,\n\n' || pg_catalog.array_to_string(v_text, E'\n\n') || E'\n\n' || v_p || E'\n';
  body_html := '<!DOCTYPE html><html lang="en-GB"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width">'
    || '<title>' || business.notice_html(subject) || '</title></head>'
    || '<body style="margin:0;padding:24px;background:#ffffff;color:#141416;font-family:Helvetica,Arial,sans-serif;font-size:16px;line-height:1.5">'
    || '<div style="max-width:560px;margin:0 auto">'
    || '<p style="margin:0 0 24px;font-family:Georgia,serif;font-size:22px;color:#5b1a2e">GK Cakes</p>'
    || '<p style="' || v_style || '">Hello,</p>' || pg_catalog.array_to_string(v_html, '')
    || '<p style="margin:32px 0 0;font-size:13px;color:#55585e">' || pg_catalog.replace(business.notice_html(v_p), E'\n', '<br>')
    || '</p></div></body></html>';
END;
$$;


-- As in 0032, except that a dispatch needs the current delivery quote paid
-- or waived (K06), not merely any succeeded delivery collection.
CREATE OR REPLACE FUNCTION business.fulfillment_refusal(p_o business.orders, p_action text, p_at timestamptz)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE
    WHEN p_action = 'ready' THEN
      CASE WHEN p_o.lifecycle = 'in_production' THEN NULL ELSE 'transition_not_allowed' END
    WHEN p_o.lifecycle <> 'ready' THEN 'transition_not_allowed'
    WHEN p_action = 'dispatch' THEN
      CASE WHEN p_o.fulfillment_mode <> 'gk_delivery'
                OR business.order_fulfillment_state(p_o) NOT IN ('awaiting_handover', 'exception') THEN 'transition_not_allowed'
           WHEN NOT business.delivery_settled(p_o.id) THEN 'delivery_not_paid' END
    WHEN p_action = 'delivery_exception' THEN
      CASE WHEN business.order_fulfillment_state(p_o) IN ('awaiting_handover', 'out_for_delivery', 'exception') THEN NULL
           ELSE 'transition_not_allowed' END
    WHEN p_action = 'handover' THEN
      CASE WHEN (p_o.fulfillment_mode = 'gk_delivery' AND business.order_fulfillment_state(p_o) = 'out_for_delivery')
                OR (p_o.fulfillment_mode <> 'gk_delivery'
                    AND business.order_fulfillment_state(p_o) IN ('awaiting_handover', 'exception')) THEN NULL
           ELSE 'transition_not_allowed' END
    WHEN p_action = 'no_show' THEN
      CASE WHEN (p_o.fulfillment_mode <> 'gk_delivery'
                 AND business.order_fulfillment_state(p_o) NOT IN ('awaiting_handover', 'exception'))
                OR (p_o.fulfillment_mode = 'gk_delivery'
                    AND NOT (business.order_fulfillment_state(p_o) = 'out_for_delivery'
                             OR (business.order_fulfillment_state(p_o) = 'exception'
                                 AND EXISTS (SELECT 1 FROM business.fulfillment_event e
                                             WHERE e.order_id = p_o.id AND e.action = 'dispatch'))))
             THEN 'transition_not_allowed'
           WHEN p_at < business.order_handover_at(p_o) THEN 'handover_not_due' END
    ELSE 'transition_not_allowed' END
$$;

-- As in 0030, plus delivery_payment_unmatched (a delivery payment that did
-- not pay the current quote, see the header, not fully returned yet) and
-- delivery_payment_stuck (a delivery collection whose Stripe call was never
-- admitted 15 minutes after it was requested; the next delivery command
-- closes it).
CREATE OR REPLACE FUNCTION business.order_attention_flags(p_order business.orders)
RETURNS text[]
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.array_remove(ARRAY[
           CASE WHEN business.order_refund_summary(p_order.id) ->> 'state' = 'needs_attention'
                THEN 'refund_needs_attention' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                             WHERE pc.order_id = p_order.id AND pc.state = 'unknown')
                THEN 'payment_unknown' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                             WHERE pc.order_id = p_order.id AND pc.mismatch_reason IS NOT NULL)
                THEN 'payment_mismatch' END,
           CASE WHEN p_order.lifecycle = 'paid_awaiting_agreement' THEN 'paid_awaiting_agreement' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.refund_observation b
                             JOIN business.payment_collection pc ON pc.id = b.collection_id
                             WHERE pc.order_id = p_order.id AND b.quarantined AND b.resolved_at IS NULL)
                THEN 'refund_quarantine' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.order_notice n
                             WHERE n.order_id = p_order.id
                               AND ((n.outcome = 'not_queued' AND n.detail <> 'allowlist')
                                    OR (n.outcome = 'not_configured'
                                        AND EXISTS (SELECT 1 FROM business.notice_settings s WHERE s.singleton))))
                THEN 'notice_not_sent' END,
           CASE WHEN business.delivery_unmatched_minor(p_order.id) > 0 THEN 'delivery_payment_unmatched' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                             WHERE pc.order_id = p_order.id AND business.delivery_collection_stuck(pc))
                THEN 'delivery_payment_stuck' END
         ]::text[], NULL);
$$;

-- As in 0033, plus an unmatched delivery payment not yet returned: money is
-- unsettled (payment_recorded); a stuck delivery collection (never admitted
-- to Stripe 15 minutes after its request) holds no money and does not
-- block.
CREATE OR REPLACE FUNCTION business.account_order_block(p_order business.orders)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE
           WHEN business.order_visible_lifecycle(p_order) IN ('confirmation_pending', 'cancellation_pending')
             THEN 'order_change_in_progress'
           WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                        WHERE pc.order_id = p_order.id
                          AND (pc.state IN ('created', 'pending', 'unknown') OR pc.mismatch_reason IS NOT NULL)
                          AND NOT business.delivery_collection_stuck(pc))
             THEN 'payment_being_checked'
           WHEN p_order.lifecycle = 'paid_awaiting_agreement' THEN 'paid_awaiting_agreement'
           WHEN x.rs IS NOT NULL AND (x.rs ->> 'state' <> 'returned' OR (x.rs ->> 'in_progress_minor')::bigint > 0
                                     OR coalesce((x.rs ->> 'delivery_pending_minor')::bigint, 0) > 0)
             THEN 'refund_in_progress'
           WHEN EXISTS (SELECT 1 FROM business.refund_observation b
                        JOIN business.payment_collection pc ON pc.id = b.collection_id
                        WHERE pc.order_id = p_order.id AND b.quarantined AND b.resolved_at IS NULL)
             THEN 'refund_being_checked'
           WHEN p_order.lifecycle = 'cancelled' AND x.rs IS NULL
                AND EXISTS (SELECT 1 FROM business.payment_collection pc
                            WHERE pc.order_id = p_order.id AND pc.state = 'succeeded')
             THEN 'payment_recorded'
           WHEN business.delivery_unmatched_minor(p_order.id) > 0
             THEN 'payment_recorded'
         END
  FROM (SELECT business.order_refund_summary(p_order.id)::jsonb AS rs) x;
$$;

-- As in 0034, plus delivery (gk_delivery orders, see the header); payment
-- (the cake's) reads cake collections only.
CREATE OR REPLACE FUNCTION business.order_view(p_order uuid)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.json_build_object(
           'id', o.id, 'version', o.version,
           'lifecycle', business.order_visible_lifecycle(o),
           'cancel_reason', CASE WHEN o.recovery_state <> 'cancellation_pending' THEN o.cancel_reason END,
           'cancelled_at', CASE WHEN o.recovery_state <> 'cancellation_pending' THEN o.cancelled_at END,
           'payment_requirement', o.payment_requirement,
           'placed_at', o.placed_at, 'date', (va.a ->> 'date')::date, 'fulfillment_mode', o.fulfillment_mode,
           'slot', va.a - 'date',
           'fulfillment', business.order_visible_fulfillment(o),
           'pickup_move_until', CASE WHEN o.fulfillment_mode IN ('collection', 'customer_courier')
                                     THEN (va.a ->> 'starts_at')::timestamptz - business.pickup_move_cutoff() END,
           'appointment_change', (SELECT pg_catalog.json_build_object(
                                    'kind', ch.kind,
                                    'state', CASE WHEN r.state <> 'secured' THEN 'processing' ELSE r.outcome ->> 'status' END,
                                    'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                                    'requested_by', CASE ch.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
                                    'requested_at', ch.received_at,
                                    'to', pg_catalog.json_build_object('date', ch.to_day, 'id', ch.to_slot_id,
                                                                       'starts_at', ch.to_starts_at, 'ends_at', ch.to_ends_at))
                                  FROM business.pickup_change ch
                                  JOIN business.recovery_operation r ON r.id = ch.command_id
                                  WHERE ch.order_id = o.id
                                  ORDER BY ch.created_at DESC, ch.command_id DESC LIMIT 1),
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
           'cancellation', (SELECT CASE WHEN r.state = 'secured' AND (r.outcome -> 'evidence' ->> 'unmatched_minor') IS NOT NULL
                              -- A stray delivery payment returned in full beside
                              -- amount_minor (0035), only when the outcome states
                              -- it (other cancellations render as in 0034).
                              THEN pg_catalog.json_build_object(
                                'state', r.outcome ->> 'status',
                                'reason', r.outcome -> 'evidence' ->> 'reason',
                                'rate', (r.outcome -> 'evidence' ->> 'rate')::integer,
                                'amount_minor', (r.outcome -> 'evidence' ->> 'refund_target_minor')::bigint,
                                'unmatched_minor', (r.outcome -> 'evidence' ->> 'unmatched_minor')::bigint,
                                'requested_by', CASE c.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
                                'requested_at', c.received_at)
                              ELSE pg_catalog.json_build_object(
                              'state', CASE WHEN r.state <> 'secured' THEN 'processing'
                                            ELSE r.outcome ->> 'status' END,
                              'reason', CASE WHEN r.state = 'secured' THEN r.outcome -> 'evidence' ->> 'reason' END,
                              'rate', CASE WHEN r.state = 'secured' THEN (r.outcome -> 'evidence' ->> 'rate')::integer END,
                              'amount_minor', CASE WHEN r.state = 'secured'
                                                   THEN (r.outcome -> 'evidence' ->> 'refund_target_minor')::bigint END,
                              'requested_by', CASE c.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
                              'requested_at', c.received_at) END
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
                       WHERE pc.order_id = o.id AND pc.kind = 'cake'
                       ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1),
           'delivery', business.order_delivery_view(o),
           'refund', CASE WHEN o.recovery_state <> 'cancellation_pending' AND NOT business.order_handover_pending(o.id)
                          THEN business.order_refund_summary(o.id) END,
           'late_payment', business.order_late_payment_view(o),
           'agreed', business.order_late_payment_agreed(o.id),
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
  FROM business.orders o
  CROSS JOIN LATERAL (SELECT business.order_visible_appointment(o) AS a) va
  WHERE o.id = p_order;
$$;

-- As in 0032, except that payment_state reads the latest cake collection
-- (a delivery collection is the delivery block's).
CREATE OR REPLACE FUNCTION business.order_staff_row(p_order business.orders, p_view text)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'id', p_order.id, 'version', p_order.version,
           'lifecycle', business.order_visible_lifecycle(p_order), 'raw_lifecycle', p_order.lifecycle,
           'date', (business.order_visible_appointment(p_order) ->> 'date')::date,
           'fulfillment_mode', p_order.fulfillment_mode,
           'slot', business.order_visible_appointment(p_order) - 'date',
           'placed_at', p_order.placed_at,
           'items', business.order_staff_items(p_order.id),
           'customer_full_name', p_order.customer_full_name,
           'cancel_reason', CASE WHEN p_order.recovery_state <> 'cancellation_pending' THEN p_order.cancel_reason END,
           'production', business.order_production_view(p_order.id),
           'fulfillment_state', business.order_visible_fulfillment(p_order) ->> 'state')
         || CASE WHEN p_view IN ('chef', 'technical_support') THEN
              pg_catalog.jsonb_build_object(
                'attention', pg_catalog.to_jsonb(business.order_attention_flags(p_order)),
                'needs_attention', pg_catalog.cardinality(business.order_attention_flags(p_order)) > 0)
            ELSE '{}'::jsonb END
         || CASE WHEN p_view = 'chef' THEN
              pg_catalog.jsonb_build_object(
                'contact_email', p_order.contact_email,
                'payment_requirement', p_order.payment_requirement,
                'total_minor', p_order.total_minor, 'currency', p_order.currency,
                'payment_state', (SELECT CASE WHEN pc.state = 'cancelled' AND pc.mismatch_reason IS NOT NULL THEN 'unknown'
                                              ELSE pc.state END
                                  FROM business.payment_collection pc WHERE pc.order_id = p_order.id AND pc.kind = 'cake'
                                  ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1),
                'paid_at', CASE WHEN p_order.recovery_state IN ('secured', 'cancellation_pending') THEN p_order.paid_at END,
                'free_confirmed_at', CASE WHEN p_order.recovery_state IN ('secured', 'cancellation_pending')
                                          THEN p_order.free_confirmed_at END,
                'refund', CASE WHEN p_order.recovery_state <> 'cancellation_pending'
                                    AND NOT business.order_handover_pending(p_order.id)
                               THEN business.order_refund_summary(p_order.id)::jsonb END)
            ELSE '{}'::jsonb END;
$$;

-- As in 0034, plus, on gk_delivery orders only, delivery: the chef gets the
-- block (without the payment link) and delivery_history; technical support
-- both without money, note or distance source; fulfilment only the delivery
-- state. Other orders keep exactly their 0034 keys (the chef's copy of the
-- customer view carries delivery: null, as the customer's does).
CREATE OR REPLACE FUNCTION business.order_staff_get(p_actor uuid, p_order uuid)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support', 'fulfillment']);
  v_view text := business.order_staff_view_of(v_account);
  o business.orders%ROWTYPE;
  v jsonb;
  v_flags text[];
  v_result jsonb;
  v_delivery jsonb;
BEGIN
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT * INTO o FROM business.orders x WHERE x.id = p_order;
  IF NOT FOUND OR (v_view = 'fulfillment' AND NOT business.order_fulfillment_scope(o.id)) THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v := business.order_view(o.id)::jsonb;
  v_flags := business.order_attention_flags(o);
  v_delivery := v -> 'delivery';
  IF v_view = 'chef' THEN
    v_result := (v #- '{payment,checkout_url}' #- '{delivery,payment,checkout_url}')
                || pg_catalog.jsonb_build_object(
                     'raw_lifecycle', o.lifecycle, 'recovery_state', o.recovery_state,
                     'attention', pg_catalog.to_jsonb(v_flags), 'needs_attention', pg_catalog.cardinality(v_flags) > 0,
                     'production', business.order_production_view(o.id),
                     'collections', business.order_staff_collections(o.id, true),
                     'refund_goal', business.order_staff_refund_goal(o.id, true),
                     'refund_operations', business.order_staff_refund_operations(o.id, true),
                     'refund_observations', business.order_staff_refund_observations(o.id, true),
                     'commands', business.order_staff_commands(o.id),
                     'appointment_history', business.order_staff_appointments(o.id, true),
                     'fulfillment_history', business.order_staff_fulfillment(o.id, true),
                     'late_payment_offers', business.order_staff_late_payment_offers(o.id),
                     'audit', business.order_staff_audit(o.id))
                || CASE WHEN o.fulfillment_mode = 'gk_delivery'
                        THEN pg_catalog.jsonb_build_object('delivery_history', business.order_staff_delivery(o.id, true))
                        ELSE '{}'::jsonb END;
  ELSIF v_view = 'technical_support' THEN
    v_result := pg_catalog.jsonb_build_object(
                  'id', v -> 'id', 'version', v -> 'version', 'lifecycle', v -> 'lifecycle',
                  'raw_lifecycle', o.lifecycle, 'recovery_state', o.recovery_state,
                  'cancel_reason', v -> 'cancel_reason', 'cancelled_at', v -> 'cancelled_at',
                  'payment_requirement', v -> 'payment_requirement', 'placed_at', v -> 'placed_at',
                  'date', v -> 'date', 'fulfillment_mode', v -> 'fulfillment_mode', 'slot', v -> 'slot',
                  'full_refund_deadline_at', v -> 'full_refund_deadline_at', 'policy_version_id', v -> 'policy_version_id',
                  'customer_full_name', v -> 'customer_full_name',
                  'paid_at', v -> 'paid_at', 'free_confirmed_at', v -> 'free_confirmed_at',
                  'free_confirmation', v -> 'free_confirmation',
                  'cancellation', CASE WHEN pg_catalog.jsonb_typeof(v -> 'cancellation') = 'object'
                                       THEN (v -> 'cancellation') - ARRAY['amount_minor', 'rate', 'unmatched_minor']
                                       ELSE v -> 'cancellation' END,
                  'payment', CASE WHEN pg_catalog.jsonb_typeof(v -> 'payment') = 'object'
                                  THEN (v -> 'payment') - ARRAY['amount_minor', 'currency', 'checkout_url'] ELSE v -> 'payment' END,
                  'refund', CASE WHEN pg_catalog.jsonb_typeof(v -> 'refund') = 'object'
                                 THEN pg_catalog.jsonb_build_object('state', v -> 'refund' -> 'state',
                                                                    'requested_at', v -> 'refund' -> 'requested_at',
                                                                    'goal_version', v -> 'refund' -> 'goal_version',
                                                                    'reason', v -> 'refund' -> 'reason')
                                 ELSE v -> 'refund' END,
                  'items', business.order_staff_items(o.id),
                  'attention', pg_catalog.to_jsonb(v_flags), 'needs_attention', pg_catalog.cardinality(v_flags) > 0,
                  'production', business.order_production_view(o.id),
                  'collections', business.order_staff_collections(o.id, false),
                  'refund_goal', business.order_staff_refund_goal(o.id, false),
                  'refund_operations', business.order_staff_refund_operations(o.id, false),
                  'refund_observations', business.order_staff_refund_observations(o.id, false),
                  'commands', business.order_staff_commands(o.id),
                  'appointment_change', v -> 'appointment_change',
                  'appointment_history', business.order_staff_appointments(o.id, false),
                  'fulfillment', v -> 'fulfillment',
                  'fulfillment_history', business.order_staff_fulfillment(o.id, false),
                  -- No money for technical support: the refund amount shown to
                  -- the customer is dropped.
                  'late_payment', CASE WHEN pg_catalog.jsonb_typeof(v -> 'late_payment') = 'object'
                                       THEN (v -> 'late_payment') - ARRAY['paid_minor', 'currency']
                                       ELSE v -> 'late_payment' END,
                  'agreed', v -> 'agreed',
                  'late_payment_offers', business.order_staff_late_payment_offers(o.id),
                  'audit', business.order_staff_audit(o.id))
                || CASE WHEN pg_catalog.jsonb_typeof(v_delivery) = 'object' THEN pg_catalog.jsonb_build_object(
                     -- Delivery without money (amounts, currency, payment link).
                     'delivery', (v_delivery - ARRAY['paid_minor', 'unmatched_minor', 'quote', 'payment'])
                                 || pg_catalog.jsonb_build_object(
                                      'quote', CASE WHEN pg_catalog.jsonb_typeof(v_delivery -> 'quote') = 'object'
                                                    THEN (v_delivery -> 'quote') - ARRAY['amount_minor', 'currency']
                                                    ELSE v_delivery -> 'quote' END,
                                      'payment', CASE WHEN pg_catalog.jsonb_typeof(v_delivery -> 'payment') = 'object'
                                                      THEN (v_delivery -> 'payment') - ARRAY['amount_minor', 'currency', 'checkout_url']
                                                      ELSE v_delivery -> 'payment' END),
                     'delivery_history', business.order_staff_delivery(o.id, false))
                   ELSE '{}'::jsonb END;
  ELSE
    v_result := pg_catalog.jsonb_build_object(
                  'id', v -> 'id', 'version', v -> 'version', 'lifecycle', v -> 'lifecycle',
                  'raw_lifecycle', o.lifecycle, 'cancel_reason', v -> 'cancel_reason', 'cancelled_at', v -> 'cancelled_at',
                  'placed_at', v -> 'placed_at', 'date', v -> 'date', 'fulfillment_mode', v -> 'fulfillment_mode',
                  'slot', v -> 'slot', 'customer_full_name', v -> 'customer_full_name',
                  'items', business.order_staff_items(o.id),
                  'production', business.order_production_view(o.id),
                  'appointment_change', v -> 'appointment_change',
                  'appointment_history', business.order_staff_appointments(o.id, false),
                  'fulfillment', v -> 'fulfillment',
                  'fulfillment_history', business.order_staff_fulfillment(o.id, true))
                -- Fulfilment: whether a GK delivery may be dispatched, nothing else.
                || CASE WHEN pg_catalog.jsonb_typeof(v_delivery) = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery', pg_catalog.jsonb_build_object('state', v_delivery -> 'state'))
                        ELSE '{}'::jsonb END;
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.staff_viewed', 'order', o.id, NULL,
                                pg_catalog.json_build_object('view', v_view));
  RETURN v_result::json;
END;
$$;

REVOKE ALL ON TABLE business.delivery_quote FROM PUBLIC;
REVOKE ALL ON TABLE business.delivery_waiver FROM PUBLIC;
REVOKE ALL ON TABLE business.delivery_payment_outcome FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_current_quote(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_in_flight(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_collection_stuck(business.payment_collection) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_close_never_dispatched(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_unmatched_minor(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_unmatched(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.payment_dispatch_kind(uuid, uuid, bigint, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_paid_collection(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_settled(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_order_open(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_check_input(text, text, integer, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_payment_settle(business.orders, business.payment_collection) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_view(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_delivery(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_quote(uuid, uuid, text, text, integer, bigint, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_waiver(uuid, uuid, text, text, integer, integer, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_payment_initiate(uuid, bytea, uuid, text, text, integer, uuid, uuid, text, text, text,
  timestamptz) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_delivery_quote(uuid, uuid, text, text, integer, bigint, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_delivery_waiver(uuid, uuid, text, text, integer, integer, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.delivery_payment_initiate(uuid, bytea, uuid, text, text, integer, uuid, uuid, text, text, text,
  timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.payment_dispatch_kind(uuid, uuid, bigint, uuid) TO gk_worker;
