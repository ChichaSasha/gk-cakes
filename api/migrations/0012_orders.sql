-- GK-025 A2: policy versions, orders and checkout holds (contracts r6.5 K00,
-- K02, K03, K04, K05; schema r1.5 policy_version, orders, order_item,
-- code_application, order_guest_capability; owner decisions GK-003
-- gk019-owner-r6 and GK-007 interview-r1).
--
-- Checkout turns one valid quote into one order awaiting payment, in one
-- transaction: it verifies the quote's owner or guest proof, that the quote
-- has not expired (24 h from first return; equality expires), that every
-- price, recipe version, lot price and code version is still current
-- (otherwise 409 price_changed: the customer reviews a new quote) and that
-- the appointment is still offered; then it inserts the order and its items,
-- admits the made-to-order cakes to the day (soft capacity, a 15-minute
-- tentative hold that is a reconcile trigger, not proof of failure), reserves
-- ready cakes exclusively, reserves the code's use within its limits, and
-- binds the order to its owner or to the quote's guest proof digest with
-- order-scoped commands. A repeated checkout of the same quote returns the
-- same order. Nothing is charged or confirmed here: payment and free
-- confirmation, which must first pass the K11 recovery gate, come with the
-- Stripe sandbox milestone. Lock order (K00): the new order row, handover
-- days, finished-cake lots by ID, the discount code, then reservations.
--
-- The refund deadline is fixed at placement as the original handover (the
-- appointment start) minus 120 elapsed hours and never moves (K05;
-- BO-GK-003.cancellation_early and cancellation_late).
--
-- Deviations recorded for the next milestone: the 202 answer carries the
-- order ID (there is no payment operation yet, so no operation ID); a 409
-- price_changed asks the client to request a new quote with a fresh guest
-- proof instead of returning one; the appointment is the quote's (no
-- pickup_slot_id at checkout).

SET LOCAL ROLE gk_owner;

-- Accepted customer policy versions (refund and cancellation terms). The
-- chef records a version citing its owner decision and source hash; the
-- newest effective one is what customers consent to at checkout. Final
-- legal text is reviewed before launch (GK-037).
CREATE TABLE business.policy_version (
  id                     uuid PRIMARY KEY,
  source_decision_id     text NOT NULL CHECK (source_decision_id ~ '^[A-Za-z0-9._:-]{1,120}$'),
  source_revision_sha256 char(64) NOT NULL CHECK (source_revision_sha256 ~ '^[0-9a-f]{64}$'),
  predecessor_id         uuid NULL REFERENCES business.policy_version (id),
  summary                text NOT NULL CHECK (summary = btrim(summary) AND char_length(summary) BETWEEN 1 AND 4000
                                              AND summary !~ '[\x01-\x09\x0b-\x1f\x7f]'),
  -- Where the Creator approved it (for example a Trello comment link).
  approver_evidence      text NOT NULL CHECK (approver_evidence = btrim(approver_evidence)
                                              AND char_length(approver_evidence) BETWEEN 1 AND 500
                                              AND approver_evidence !~ '[[:cntrl:]]'),
  recorded_at            timestamptz NOT NULL DEFAULT now(),
  effective_at           timestamptz NOT NULL,
  recorded_by            uuid NOT NULL REFERENCES business.account (id)
);

CREATE INDEX policy_version_effective_idx ON business.policy_version (effective_at DESC, recorded_at DESC);

CREATE TABLE business.orders (
  id                      uuid PRIMARY KEY,
  quote_id                uuid NOT NULL UNIQUE REFERENCES business.checkout_quote (id),
  body_sha256             char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  version                 integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  owner_account_id        uuid NULL REFERENCES business.account (id),
  customer_full_name      text NOT NULL CHECK (customer_full_name = btrim(customer_full_name)
                                               AND char_length(customer_full_name) BETWEEN 1 AND 120
                                               AND customer_full_name !~ '[[:cntrl:]]'),
  different_payer_name    text NULL CHECK (different_payer_name IS NULL OR (different_payer_name = btrim(different_payer_name)
                                               AND char_length(different_payer_name) BETWEEN 1 AND 120
                                               AND different_payer_name !~ '[[:cntrl:]]')),
  contact_email           text NOT NULL CHECK (char_length(contact_email) BETWEEN 3 AND 254
                                               AND contact_email ~ '^[^@[:space:][:cntrl:]]+@[^@[:space:][:cntrl:]]+\.[^@[:space:][:cntrl:]]+$'),
  lifecycle               text NOT NULL DEFAULT 'awaiting_payment' CHECK (lifecycle IN (
                            'awaiting_payment', 'paid_awaiting_agreement', 'confirmed', 'in_production', 'ready',
                            'completed', 'cancelled')),
  payment_requirement     text NOT NULL CHECK (payment_requirement IN ('full', 'none')),
  fulfillment_mode        text NOT NULL CHECK (fulfillment_mode IN ('collection', 'customer_courier', 'gk_delivery')),
  handover_day            date NOT NULL,
  slot_id                 uuid NOT NULL,
  original_handover_at    timestamptz NOT NULL,
  handover_ends_at        timestamptz NOT NULL,
  full_refund_deadline_at timestamptz NOT NULL,
  policy_version_id       uuid NOT NULL REFERENCES business.policy_version (id),
  subtotal_minor          bigint NOT NULL CHECK (subtotal_minor > 0),
  discount_minor          bigint NOT NULL CHECK (discount_minor >= 0),
  total_minor             bigint NOT NULL,
  currency                text NOT NULL DEFAULT 'GBP' CHECK (currency = 'GBP'),
  placed_at               timestamptz NOT NULL,
  paid_at                 timestamptz NULL,
  free_confirmed_at       timestamptz NULL,
  recovery_state          text NOT NULL DEFAULT 'prepared' CHECK (recovery_state IN ('prepared', 'confirmation_pending', 'secured')),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT orders_total CHECK (total_minor = subtotal_minor - discount_minor AND total_minor >= 0),
  CONSTRAINT orders_payment_requirement CHECK ((payment_requirement = 'none') = (total_minor = 0)),
  -- K05: the full-refund deadline is the original handover minus 120 h.
  CONSTRAINT orders_refund_deadline CHECK (full_refund_deadline_at = original_handover_at - interval '120 hours'),
  CONSTRAINT orders_single_anchor CHECK (paid_at IS NULL OR free_confirmed_at IS NULL)
);

CREATE INDEX orders_owner_idx ON business.orders (owner_account_id, placed_at DESC) WHERE owner_account_id IS NOT NULL;
CREATE INDEX orders_day_idx ON business.orders (handover_day, lifecycle);

-- The refund deadline, original handover, totals and anchors never change
-- once set; the lifecycle moves forward through domain commands only.
CREATE FUNCTION business.orders_guard()
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
     -- Terminal history is never reopened.
     OR (OLD.lifecycle IN ('cancelled', 'completed') AND NEW.lifecycle IS DISTINCT FROM OLD.lifecycle) THEN
    RAISE EXCEPTION 'order snapshot fields are immutable' USING ERRCODE = 'GK409';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER orders_guard BEFORE UPDATE OR DELETE ON business.orders
  FOR EACH ROW EXECUTE FUNCTION business.orders_guard();

-- Immutable purchase snapshot per line (K01, K07): what was bought and what
-- was paid for it after the line's discount share.
CREATE TABLE business.order_item (
  id                uuid PRIMARY KEY,
  order_id          uuid NOT NULL REFERENCES business.orders (id),
  line_no           integer NOT NULL CHECK (line_no BETWEEN 1 AND 20),
  variant_id        uuid NOT NULL REFERENCES business.variant (id),
  recipe_version_id uuid NOT NULL REFERENCES business.recipe_version (id),
  physical_lot_id   uuid NULL REFERENCES business.finished_cake_lot (id),
  quantity          integer NOT NULL CHECK (quantity BETWEEN 1 AND 20),
  unit_price_minor  bigint NOT NULL CHECK (unit_price_minor >= 1),
  discount_minor    bigint NOT NULL CHECK (discount_minor >= 0),
  net_paid_minor    bigint NOT NULL,
  CONSTRAINT order_item_net CHECK (net_paid_minor = unit_price_minor * quantity - discount_minor AND net_paid_minor >= 0),
  UNIQUE (order_id, line_no)
);

CREATE TRIGGER order_item_immutable BEFORE UPDATE OR DELETE ON business.order_item
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();

-- One code use per order (K03), reserved at checkout, consumed at paid or
-- free confirmation, released only on proven unpaid abandonment; a
-- cancellation never restores it.
CREATE TABLE business.code_application (
  id              uuid PRIMARY KEY,
  order_id        uuid NOT NULL UNIQUE REFERENCES business.orders (id),
  code_id         uuid NOT NULL REFERENCES business.discount_code (id),
  code_version_id uuid NOT NULL REFERENCES business.discount_code_version (id),
  account_id      uuid NULL REFERENCES business.account (id),
  state           text NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved', 'consumed', 'released')),
  discount_minor  bigint NOT NULL CHECK (discount_minor >= 0),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX code_application_code_idx ON business.code_application (code_id, state);

-- A guest's access to one order: the digest of the secret that created the
-- quote, with order-scoped commands; revocable, replaceable.
CREATE TABLE business.order_guest_capability (
  id                uuid PRIMARY KEY,
  order_id          uuid NOT NULL REFERENCES business.orders (id),
  capability_digest bytea NOT NULL UNIQUE CHECK (octet_length(capability_digest) = 32),
  allowed_commands  text[] NOT NULL CHECK (allowed_commands <@ ARRAY['read', 'pay', 'cancel', 'move_pickup']::text[]),
  created_at        timestamptz NOT NULL DEFAULT now(),
  revoked_at        timestamptz NULL,
  replaced_by       uuid NULL REFERENCES business.order_guest_capability (id)
);

CREATE INDEX order_guest_capability_order_idx ON business.order_guest_capability (order_id);

-- capacity_allocation.order_ref keeps no foreign key yet: chef-entered
-- orders (GK-027, chef extras) will not come from storefront quotes, and the
-- key is added when every allocation has an order row. Checkout is the only
-- writer of standard allocations today.

-- Which lot reservations belong to which order line, so later settlement
-- (paid: sold; proven unpaid: returned) acts on exactly what was reserved.
CREATE TABLE business.order_lot_reservation (
  order_id uuid NOT NULL REFERENCES business.orders (id),
  lot_id   uuid NOT NULL REFERENCES business.finished_cake_lot (id),
  quantity integer NOT NULL CHECK (quantity >= 1),
  state    text NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved', 'sold', 'returned')),
  PRIMARY KEY (order_id, lot_id)
);

-- A code use moves reserved -> consumed or released, never back (K03: a
-- cancellation never restores it); a revoked capability stays revoked.
CREATE FUNCTION business.order_rows_guard()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF TG_TABLE_NAME = 'code_application' THEN
    IF (NEW.id, NEW.order_id, NEW.code_id, NEW.code_version_id, NEW.account_id, NEW.discount_minor)
       IS DISTINCT FROM (OLD.id, OLD.order_id, OLD.code_id, OLD.code_version_id, OLD.account_id, OLD.discount_minor)
       OR (OLD.state <> 'reserved' AND NEW.state IS DISTINCT FROM OLD.state) THEN
      RAISE EXCEPTION 'code use history is immutable' USING ERRCODE = 'GK409';
    END IF;
  ELSIF TG_TABLE_NAME = 'order_guest_capability' THEN
    IF (NEW.id, NEW.order_id, NEW.capability_digest, NEW.created_at)
       IS DISTINCT FROM (OLD.id, OLD.order_id, OLD.capability_digest, OLD.created_at)
       OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
      RAISE EXCEPTION 'capability history is immutable' USING ERRCODE = 'GK409';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER code_application_guard BEFORE UPDATE ON business.code_application
  FOR EACH ROW EXECUTE FUNCTION business.order_rows_guard();
CREATE TRIGGER order_guest_capability_guard BEFORE UPDATE ON business.order_guest_capability
  FOR EACH ROW EXECUTE FUNCTION business.order_rows_guard();

CREATE TRIGGER policy_version_immutable BEFORE UPDATE OR DELETE ON business.policy_version
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER code_application_no_delete BEFORE DELETE ON business.code_application
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER order_guest_capability_no_delete BEFORE DELETE ON business.order_guest_capability
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER order_lot_reservation_no_delete BEFORE DELETE ON business.order_lot_reservation
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER orders_no_truncate BEFORE TRUNCATE ON business.orders
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER order_item_no_truncate BEFORE TRUNCATE ON business.order_item
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER code_application_no_truncate BEFORE TRUNCATE ON business.code_application
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER order_guest_capability_no_truncate BEFORE TRUNCATE ON business.order_guest_capability
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER order_lot_reservation_no_truncate BEFORE TRUNCATE ON business.order_lot_reservation
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER policy_version_no_truncate BEFORE TRUNCATE ON business.policy_version
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: whether qty more made-to-order cakes would be admitted
-- on a day now: committed standard cakes plus qty within the limit (K04;
-- holds do not count). Advisory: checkout decides under the day lock.
CREATE FUNCTION business.capacity_admissible(p_day date, p_quantity integer)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT f.committed_standard + p_quantity <= f.effective_limit FROM business.schedule_day_facts(p_day, p_day) f;
$$;

-- Quotes judge day places as admission does. Replace quote_create's slot
-- check: a slot full only because of other orders' holds can be quoted.
DO $do$
DECLARE
  v_src text;
BEGIN
  SELECT pg_catalog.pg_get_functiondef('business.quote_create(uuid, bytea, text, text, date, uuid, text, json, text, timestamptz)'::regprocedure)
  INTO v_src;
  IF pg_catalog.strpos(v_src, 'AND NOT (v_mto = 0 AND v_slot.reason_code = ''full'')') = 0 THEN
    RAISE EXCEPTION 'quote_create slot check not found';
  END IF;
  v_src := pg_catalog.replace(v_src, 'AND NOT (v_mto = 0 AND v_slot.reason_code = ''full'')',
    'AND NOT (v_slot.reason_code = ''full'' AND (v_mto = 0 OR business.capacity_admissible(p_day, v_mto)))');
  EXECUTE v_src;
END
$do$;

-- Purged quotes would orphan orders: the quote purge must keep converted
-- quotes. Replace the purge so it skips them.
CREATE OR REPLACE FUNCTION business.quote_purge_expired(p_before timestamptz)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_count integer;
BEGIN
  IF p_before IS NULL OR p_before > pg_catalog.now() - interval '30 days' THEN
    RAISE EXCEPTION 'quotes are kept at least 30 days after they expire' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  PERFORM pg_catalog.set_config('business.quote_purge', 'on', true);
  DELETE FROM business.checkout_quote_line l USING business.checkout_quote q
  WHERE l.quote_id = q.id AND q.expires_at < p_before
    AND NOT EXISTS (SELECT 1 FROM business.orders o WHERE o.quote_id = q.id);
  DELETE FROM business.checkout_quote q
  WHERE q.expires_at < p_before AND NOT EXISTS (SELECT 1 FROM business.orders o WHERE o.quote_id = q.id);
  GET DIAGNOSTICS v_count = ROW_COUNT;
  PERFORM pg_catalog.set_config('business.quote_purge', 'off', true);
  RETURN v_count;
END;
$$;

-- --------------------------------------------------------------- policies

CREATE FUNCTION business.policy_record(p_actor uuid, p_key text, p_body_sha256 text, p_source_decision_id text,
                                       p_source_revision_sha256 text, p_summary text, p_effective_at timestamptz,
                                       p_approver_evidence text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'policy.record', '', p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
  v_predecessor uuid;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_effective_at IS NULL OR p_effective_at < pg_catalog.now() - interval '1 minute' THEN
    RAISE EXCEPTION 'effective_invalid' USING ERRCODE = 'GK422';
  END IF;
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('business.policy_version', 0));
  SELECT p.id INTO v_predecessor FROM business.policy_version p ORDER BY p.effective_at DESC, p.recorded_at DESC LIMIT 1;
  BEGIN
    INSERT INTO business.policy_version (id, source_decision_id, source_revision_sha256, predecessor_id, summary,
                                         approver_evidence, effective_at, recorded_by)
    VALUES (v_id, p_source_decision_id, p_source_revision_sha256, v_predecessor, p_summary, p_approver_evidence,
            p_effective_at, v_account);
  EXCEPTION WHEN check_violation OR not_null_violation THEN
    RAISE EXCEPTION 'policy_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'policy.recorded', 'policy_version', v_id, NULL,
                                pg_catalog.json_build_object('source', p_source_decision_id));
  PERFORM business.command_finish(v_account, 'policy.record', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- Public: the policy version customers consent to now (newest effective).
CREATE FUNCTION business.policy_current(p_now timestamptz)
RETURNS TABLE (policy_id uuid, policy_summary text, policy_effective_at timestamptz, policy_source text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p.id, p.summary, p.effective_at, p.source_decision_id
  FROM business.policy_version p
  WHERE p.effective_at <= p_now
  ORDER BY p.effective_at DESC, p.recorded_at DESC
  LIMIT 1;
$$;

-- ---------------------------------------------------------------- checkout

-- Creates the order of a quote (see the header). p_owner or p_guest_digest
-- must match the quote. Returns the order and whether it already existed.
CREATE FUNCTION business.checkout_create(
  p_owner uuid, p_guest_digest bytea, p_key text, p_body_sha256 text, p_quote uuid, p_quote_version integer,
  p_policy uuid, p_full_name text, p_payer_name text, p_email text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
  v_disabled boolean;
  v_q business.checkout_quote%ROWTYPE;
  v_prior uuid;
  v_prior_hash text;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_line business.checkout_quote_line%ROWTYPE;
  v_mto integer := 0;
  v_slot record;
  v_code_id uuid;
  v_code_version uuid;
  v_total_limit integer;
  v_per_customer integer;
  v_used integer;
  v_lot record;
BEGIN
  IF (p_owner IS NULL) = (p_guest_digest IS NULL) OR p_now IS NULL OR p_quote IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_owner IS NOT NULL THEN
    SELECT r.account_id, r.account_disabled INTO v_account, v_disabled FROM business.identity_resolve(p_owner) r;
    IF v_disabled THEN
      RAISE EXCEPTION 'account disabled' USING ERRCODE = 'GK403';
    END IF;
  END IF;

  -- The quote, for its owner or proof holder only (404 otherwise, as for a
  -- guessed ID). Locked so a repeated checkout waits and then replays.
  SELECT * INTO v_q FROM business.checkout_quote q
  WHERE q.id = p_quote
    AND ((v_account IS NOT NULL AND q.owner_account_id = v_account)
         OR (p_guest_digest IS NOT NULL AND q.guest_proof_digest = p_guest_digest))
  FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'quote not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('checkout|' || p_quote::text, 0));
  SELECT o.id, o.body_sha256 INTO v_prior, v_prior_hash FROM business.orders o WHERE o.quote_id = p_quote;
  IF FOUND THEN
    -- A guest replays only while the order's capability for this proof is
    -- active (a revoked or rotated grant answers as if absent).
    IF v_account IS NULL AND NOT EXISTS (
         SELECT 1 FROM business.order_guest_capability g
         WHERE g.order_id = v_prior AND g.capability_digest = p_guest_digest AND g.revoked_at IS NULL) THEN
      RAISE EXCEPTION 'quote not found' USING ERRCODE = 'GK404';
    END IF;
    IF v_prior_hash <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_quote_version IS DISTINCT FROM 1 THEN
    RAISE EXCEPTION 'price_changed' USING ERRCODE = 'GK409';
  END IF;
  -- The 24-hour payment window: equality expires (K02, K04).
  IF p_now >= v_q.expires_at THEN
    RAISE EXCEPTION 'quote_expired' USING ERRCODE = 'GK409';
  END IF;
  IF p_policy IS DISTINCT FROM (SELECT c.policy_id FROM business.policy_current(p_now) c) THEN
    RAISE EXCEPTION 'policy_changed' USING ERRCODE = 'GK409';
  END IF;

  -- Every price is still what the customer reviewed.
  FOR v_line IN SELECT * FROM business.checkout_quote_line l WHERE l.quote_id = p_quote ORDER BY l.line_no LOOP
    IF v_line.lot_id IS NULL THEN
      v_mto := v_mto + v_line.quantity;
      PERFORM 1 FROM business.variant v
      JOIN business.cake c ON c.id = v.cake_id
      JOIN business.recipe_version r ON r.id = v.current_recipe_version_id
      WHERE v.id = v_line.variant_id AND v.archived_at IS NULL AND c.state = 'active'
        AND v.current_recipe_version_id = v_line.recipe_version_id AND r.price_minor = v_line.unit_price_minor;
    ELSE
      PERFORM 1 FROM business.finished_cake_lot l
      JOIN business.variant v ON v.id = l.variant_id
      JOIN business.cake c ON c.id = v.cake_id
      WHERE l.id = v_line.lot_id AND l.price_minor = v_line.unit_price_minor AND v.archived_at IS NULL
        AND c.state = 'active';
    END IF;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'price_changed' USING ERRCODE = 'GK409';
    END IF;
  END LOOP;
  IF v_q.code_version_id IS NOT NULL THEN
    SELECT c.id, v.id, v.total_limit, v.per_customer_limit INTO v_code_id, v_code_version, v_total_limit, v_per_customer
    FROM business.discount_code_version qv
    JOIN business.discount_code c ON c.id = qv.code_id
    JOIN LATERAL (SELECT * FROM business.discount_code_version dv WHERE dv.code_id = c.id
                  ORDER BY dv.revision DESC LIMIT 1) v ON true
    WHERE qv.id = v_q.code_version_id AND c.archived_at IS NULL
      AND (v.expires_at IS NULL OR v.expires_at > p_now);
    IF NOT FOUND OR v_code_version <> v_q.code_version_id THEN
      RAISE EXCEPTION 'price_changed' USING ERRCODE = 'GK409';
    END IF;
    IF v_per_customer IS NOT NULL AND v_account IS NULL THEN
      RAISE EXCEPTION 'code_requires_sign_in' USING ERRCODE = 'GK422';
    END IF;
  END IF;
  -- The appointment is still offered. Day places are not judged here:
  -- other orders' holds do not own the last place (K04), so a slot shown as
  -- full only because of holds stays open, and capacity_admit decides on
  -- committed cakes under the day lock.
  SELECT s.* INTO v_slot FROM business.schedule_public_slots(v_q.handover_day, v_q.fulfillment_mode, p_now, greatest(v_mto, 1)) s
  WHERE s.slot_id = v_q.slot_id;
  IF NOT FOUND OR (v_slot.availability NOT IN ('available', 'requestable') AND v_slot.reason_code IS DISTINCT FROM 'full')
     OR v_slot.starts_at <> v_q.slot_starts_at THEN
    RAISE EXCEPTION 'slot_unavailable' USING ERRCODE = 'GK409';
  END IF;

  BEGIN
    INSERT INTO business.orders (id, quote_id, body_sha256, owner_account_id, customer_full_name, different_payer_name,
                                 contact_email, payment_requirement, fulfillment_mode, handover_day, slot_id,
                                 original_handover_at, handover_ends_at, full_refund_deadline_at, policy_version_id,
                                 subtotal_minor, discount_minor, total_minor, placed_at)
    VALUES (v_id, p_quote, p_body_sha256, v_account, p_full_name, p_payer_name, p_email,
            CASE WHEN v_q.total_minor = 0 THEN 'none' ELSE 'full' END, v_q.fulfillment_mode, v_q.handover_day,
            v_q.slot_id, v_q.slot_starts_at, v_q.slot_ends_at, v_q.slot_starts_at - interval '120 hours', p_policy,
            v_q.subtotal_minor, v_q.discount_minor, v_q.total_minor, p_now);
  EXCEPTION WHEN check_violation OR not_null_violation THEN
    RAISE EXCEPTION 'contact_invalid' USING ERRCODE = 'GK422';
  END;
  -- Resources in lock order (K00): the handover day, then lots by ID, then
  -- the code; rows that reference them (items, reservations) only after
  -- their locks are held, so no foreign-key share lock is taken first.
  IF v_mto > 0 THEN
    PERFORM business.capacity_admit(v_id, v_q.handover_day, v_mto, p_now + interval '15 minutes');
  END IF;
  FOR v_lot IN SELECT l.lot_id, sum(l.quantity)::integer AS qty FROM business.checkout_quote_line l
               WHERE l.quote_id = p_quote AND l.lot_id IS NOT NULL GROUP BY l.lot_id ORDER BY l.lot_id LOOP
    PERFORM business.lot_reserve(v_lot.lot_id, v_lot.qty, p_now);
    -- The cake must still be good at the end of the appointment.
    IF EXISTS (SELECT 1 FROM business.finished_cake_lot fl WHERE fl.id = v_lot.lot_id AND fl.expires_at < v_q.slot_ends_at) THEN
      RAISE EXCEPTION 'item_unavailable' USING ERRCODE = 'GK409';
    END IF;
    INSERT INTO business.order_lot_reservation (order_id, lot_id, quantity) VALUES (v_id, v_lot.lot_id, v_lot.qty);
  END LOOP;
  IF v_code_id IS NOT NULL THEN
    PERFORM 1 FROM business.discount_code c WHERE c.id = v_code_id FOR UPDATE;
    -- Recheck after the lock (K00): still active, unexpired, the reviewed
    -- version, with its current limits.
    SELECT v.id, v.total_limit, v.per_customer_limit INTO v_code_version, v_total_limit, v_per_customer
    FROM business.discount_code c
    JOIN LATERAL (SELECT * FROM business.discount_code_version dv WHERE dv.code_id = c.id
                  ORDER BY dv.revision DESC LIMIT 1) v ON true
    WHERE c.id = v_code_id AND c.archived_at IS NULL AND (v.expires_at IS NULL OR v.expires_at > p_now);
    IF NOT FOUND OR v_code_version <> v_q.code_version_id THEN
      RAISE EXCEPTION 'price_changed' USING ERRCODE = 'GK409';
    END IF;
    -- Uses count reserved and consumed applications of every version of the
    -- code (K03: reserve atomically; only proven unpaid abandonment releases).
    IF v_total_limit IS NOT NULL THEN
      SELECT count(*) INTO v_used FROM business.code_application a WHERE a.code_id = v_code_id AND a.state <> 'released';
      IF v_used >= v_total_limit THEN
        RAISE EXCEPTION 'code_used_up' USING ERRCODE = 'GK409';
      END IF;
    END IF;
    IF v_per_customer IS NOT NULL THEN
      SELECT count(*) INTO v_used FROM business.code_application a
      WHERE a.code_id = v_code_id AND a.account_id = v_account AND a.state <> 'released';
      IF v_used >= v_per_customer THEN
        RAISE EXCEPTION 'code_used_up' USING ERRCODE = 'GK409';
      END IF;
    END IF;
    INSERT INTO business.code_application (id, order_id, code_id, code_version_id, account_id, discount_minor)
    VALUES (pg_catalog.gen_random_uuid(), v_id, v_code_id, v_code_version, v_account, v_q.discount_minor);
  END IF;
  INSERT INTO business.order_item (id, order_id, line_no, variant_id, recipe_version_id, physical_lot_id, quantity,
                                   unit_price_minor, discount_minor, net_paid_minor)
  SELECT pg_catalog.gen_random_uuid(), v_id, l.line_no, l.variant_id, l.recipe_version_id, l.lot_id, l.quantity,
         l.unit_price_minor, l.discount_minor, l.net_minor
  FROM business.checkout_quote_line l WHERE l.quote_id = p_quote;
  IF v_account IS NULL THEN
    INSERT INTO business.order_guest_capability (id, order_id, capability_digest, allowed_commands)
    VALUES (pg_catalog.gen_random_uuid(), v_id, p_guest_digest, ARRAY['read', 'pay', 'cancel', 'move_pickup']);
  END IF;
  PERFORM business.audit_record(CASE WHEN v_account IS NULL THEN 'system' ELSE 'customer' END, v_account,
                                'order.placed', 'order', v_id, NULL,
                                pg_catalog.json_build_object('total_minor', v_q.total_minor, 'cakes', v_mto));
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- An order for its owner or a holder of an active guest capability with
-- "read"; nothing otherwise.
CREATE FUNCTION business.order_get(p_order uuid, p_owner uuid, p_guest_digest bytea)
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
           'id', o.id, 'version', o.version, 'lifecycle', o.lifecycle, 'payment_requirement', o.payment_requirement,
           'placed_at', o.placed_at, 'date', o.handover_day, 'fulfillment_mode', o.fulfillment_mode,
           'slot', pg_catalog.json_build_object('id', o.slot_id, 'starts_at', o.original_handover_at, 'ends_at', o.handover_ends_at),
           'full_refund_deadline_at', o.full_refund_deadline_at, 'policy_version_id', o.policy_version_id,
           'customer_full_name', o.customer_full_name, 'different_payer_name', o.different_payer_name,
           'contact_email', o.contact_email, 'subtotal_minor', o.subtotal_minor, 'discount_minor', o.discount_minor,
           'total_minor', o.total_minor, 'currency', o.currency, 'paid_at', o.paid_at,
           'free_confirmed_at', o.free_confirmed_at,
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

REVOKE ALL ON TABLE business.policy_version, business.orders, business.order_item, business.code_application,
  business.order_guest_capability, business.order_lot_reservation FROM PUBLIC;

REVOKE ALL ON FUNCTION business.orders_guard() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.capacity_admissible(date, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.quote_purge_expired(timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.policy_record(uuid, text, text, text, text, text, timestamptz, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_rows_guard() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.policy_current(timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.checkout_create(uuid, bytea, text, text, uuid, integer, uuid, text, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_get(uuid, uuid, bytea) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.policy_record(uuid, text, text, text, text, text, timestamptz, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.policy_current(timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.checkout_create(uuid, bytea, text, text, uuid, integer, uuid, text, text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_get(uuid, uuid, bytea) TO gk_api;
