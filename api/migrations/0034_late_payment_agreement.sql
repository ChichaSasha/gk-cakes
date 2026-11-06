-- GK-017 r02b Q6: the late-payment agreement on the order page (Creator
-- decision 2026-11-05: "Paid late: the customer agrees or declines on the
-- order page; the buttons record explicit agreement as evidence. The chef may
-- propose a different date, only as an offer the customer must accept.";
-- design docs/design/sessions/checkout-recovery/gk-017-r02 state 10;
-- contracts r6.5 K04 Q2 "Honour only when chef records feasibility and the
-- customer explicitly agrees to that handover ... Silence never counts as
-- agreement", K05 refund and preparation anchors, K06 moves keep the original
-- deadline, K08 audit without personal data, K09 expand-only, K11 accepted
-- order commands).
--
-- Chef offer (business.order_late_payment_offer, chef). On a secured
-- paid_awaiting_agreement order with no unresolved K11 command: kind
-- 'requested' offers the original appointment (refused honour_too_late once
-- the original handover is not after the receipt instant, as the chef's
-- honour); kind 'other' offers a published start of the current schedule on a
-- non-closed day (the chef appointment change's validation: in the future at
-- the receipt, within the ready cakes' shelf life) that can take the order's
-- day place: on another day the committed standard cakes of other orders plus
-- the order's must fit the day's limit (a customer acceptance never creates a
-- chef_extra place), otherwise 409 slot_unavailable. The chef records
-- feasibility, so the 192-hour lead time does not apply. An offer changes no
-- commitment or money and holds no capacity: not a K11 command, but versioned
-- (order version + 1), idempotent (K00 command receipt; the same key replays,
-- also during a restore freeze), audited without personal data and refused
-- during a restore freeze (GK503). Offers are immutable rows
-- (business.late_payment_offer); a newer offer withdraws the open one.
--
-- Customer answer (business.order_late_payment_answer, API). The order's
-- owner or a guest capability holder with "cancel" (the cancellation's
-- access) answers the current open offer only (else 409 offer_not_current;
-- while waiting there is no offer to answer). The answer is a K11 order
-- command 'late_payment' through the existing late-payment path, with its
-- decision row: accept = honour with agreement evidence {channel order_page,
-- agreed_at = the receipt instant, evidence_ref = the answer ID}; decline =
-- full_return (cancel_reason late_payment_returned, 100% of the actual paid
-- aggregate). The answer is stored immutably (business.late_payment_answer:
-- offer, answer, receipt instant, the access used as account or guest
-- capability ID, never tokens or personal data). For kind 'other' the
-- accepted command moves the order to the offered appointment in the same
-- commit, with the place checked at that moment: if the slot can no longer
-- take it, the offer lapses and nothing else changes (409 slot_unavailable
-- at the request, recorded as a lapsed answer without a command; or the
-- commit's refusal slot_unavailable). The chef must then offer again. A
-- restore freeze refuses new answers (GK503; the same key replays) and
-- defers the commit, as for the other customer order commands. The feasibility
-- recorder on the decision row is the chef who made the offer.
--
-- Read models. order_view adds late_payment {state waiting | offered |
-- processing, offer, answer} on a paid_awaiting_agreement order and agreed
-- {at, date, start, end, kind, offer_id} once an accepted answer is secured;
-- while an accepted 'other' offer is committed but not secured, the visible
-- appointment is still the one it replaces (business.order_visible_appointment).
-- Staff: the chef sees everything order_view has plus late_payment_offers;
-- technical support late_payment, agreed and late_payment_offers (no money);
-- fulfilment none of it. Notices: no new template; the confirmed/cancelled
-- notices fire once through order_command_secured as for the chef's decision.
--
-- Expand-only (K09): new tables, replaced CHECK constraints, new functions;
-- replaced functions keep their signatures, results and grants. Lock order
-- (K00): the order, its collections, the recovery operation, goal and refund
-- operations, the handover days by date, history/audit.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------- schema

ALTER TABLE business.order_command DROP CONSTRAINT order_command_late_payment;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_late_payment CHECK (command <> 'late_payment' OR actor_kind IN ('staff', 'customer', 'guest'));

-- order_page: the customer's own button press (only the answer function
-- writes it; the chef's route keeps its channel list).
ALTER TABLE business.late_payment_decision DROP CONSTRAINT late_payment_decision_channel_check;
ALTER TABLE business.late_payment_decision
  ADD CONSTRAINT late_payment_decision_channel_check CHECK (channel IN ('email', 'phone', 'instagram', 'whatsapp',
                                                                         'in_person', 'other', 'order_page'));

CREATE TABLE business.late_payment_offer (
  id                uuid PRIMARY KEY,
  order_id          uuid NOT NULL REFERENCES business.orders (id),
  kind              text NOT NULL CHECK (kind IN ('requested', 'other')),
  handover_day      date NOT NULL,
  slot_id           uuid NOT NULL,
  starts_at         timestamptz NOT NULL,
  ends_at           timestamptz NOT NULL,
  -- The schedule revision an 'other' slot was published in at the receipt.
  schedule_revision integer NULL CHECK (schedule_revision >= 1),
  -- The order version the offer was made at (the offer bumps it).
  order_version     integer NOT NULL CHECK (order_version >= 1),
  -- The chef who recorded feasibility, at the persisted receipt instant.
  offered_by        uuid NOT NULL REFERENCES business.account (id),
  offered_at        timestamptz NOT NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT late_payment_offer_window CHECK (ends_at > starts_at),
  CONSTRAINT late_payment_offer_revision CHECK ((kind = 'other') = (schedule_revision IS NOT NULL)),
  UNIQUE (order_id, order_version)
);
CREATE TRIGGER late_payment_offer_immutable BEFORE UPDATE OR DELETE ON business.late_payment_offer
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER late_payment_offer_no_truncate BEFORE TRUNCATE ON business.late_payment_offer
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- The customer's answer to an offer (agreement evidence), one per offer.
CREATE TABLE business.late_payment_answer (
  -- Also the decision's evidence_ref.
  id                  uuid PRIMARY KEY,
  offer_id            uuid NOT NULL UNIQUE REFERENCES business.late_payment_offer (id),
  order_id            uuid NOT NULL REFERENCES business.orders (id),
  answer              text NOT NULL CHECK (answer IN ('accept', 'decline')),
  -- The K11 late_payment command; NULL when the offer lapsed at the receipt.
  command_id          uuid NULL UNIQUE REFERENCES business.order_command (id),
  lapsed              boolean NOT NULL,
  actor_kind          text NOT NULL CHECK (actor_kind IN ('customer', 'guest')),
  actor_account_id    uuid NULL REFERENCES business.account (id),
  guest_capability_id uuid NULL REFERENCES business.order_guest_capability (id),
  idempotency_key     text NOT NULL CHECK (idempotency_key ~ '^[\x21-\x7e]{1,128}$'),
  body_sha256         char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  order_version       integer NOT NULL CHECK (order_version >= 1),
  -- The order's appointment at the receipt (what an accepted 'other' offer
  -- replaces).
  from_day            date NOT NULL,
  from_slot_id        uuid NOT NULL,
  from_starts_at      timestamptz NOT NULL,
  from_ends_at        timestamptz NOT NULL,
  -- The persisted receipt instant (= agreed_at of an acceptance).
  received_at         timestamptz NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT late_payment_answer_actor CHECK ((actor_kind = 'customer') = (actor_account_id IS NOT NULL)
                                              AND (actor_kind = 'guest') = (guest_capability_id IS NOT NULL)),
  CONSTRAINT late_payment_answer_lapse CHECK ((command_id IS NULL) = lapsed AND (NOT lapsed OR answer = 'accept'))
);
CREATE INDEX late_payment_answer_order_idx ON business.late_payment_answer (order_id, created_at);
CREATE UNIQUE INDEX late_payment_answer_key_idx
  ON business.late_payment_answer (order_id, idempotency_key, actor_account_id, guest_capability_id) NULLS NOT DISTINCT;
CREATE TRIGGER late_payment_answer_immutable BEFORE UPDATE OR DELETE ON business.late_payment_answer
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER late_payment_answer_no_truncate BEFORE TRUNCATE ON business.late_payment_answer
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- On an order-page answer, late_payment_decision.recorded_by is the chef who
-- made the offer (the feasibility recorder; the column is NOT NULL and a
-- guest has no account): the decision's real actor is the order_command
-- (actor_kind, actor_account_id or guest_capability_id) and
-- late_payment_answer. A decline's row is therefore the customer's decision.
COMMENT ON COLUMN business.late_payment_decision.recorded_by IS
  'The chef who recorded feasibility. For an order-page answer (0034) the chef who made the offer; the actor is order_command / late_payment_answer.';

-- ------------------------------------------------------------ helpers

-- Owner-internal: the state of an offer: open, withdrawn (a newer offer),
-- closed (the order left paid_awaiting_agreement otherwise), processing (an
-- answer's command is not secured), accepted, declined, lapsed (the slot
-- could no longer take the order) or refused (the answer's command was
-- refused for another reason).
CREATE FUNCTION business.late_payment_offer_state(p_offer uuid)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE
    WHEN a.id IS NOT NULL AND a.lapsed THEN 'lapsed'
    WHEN a.id IS NOT NULL AND r.state <> 'secured' THEN 'processing'
    WHEN a.id IS NOT NULL AND r.outcome ->> 'status' = 'confirmed' THEN 'accepted'
    WHEN a.id IS NOT NULL AND r.outcome ->> 'status' = 'cancelled' THEN 'declined'
    WHEN a.id IS NOT NULL AND r.outcome -> 'evidence' ->> 'reason' = 'slot_unavailable' THEN 'lapsed'
    WHEN a.id IS NOT NULL THEN 'refused'
    WHEN EXISTS (SELECT 1 FROM business.late_payment_offer n
                 WHERE n.order_id = f.order_id AND n.order_version > f.order_version) THEN 'withdrawn'
    WHEN o.lifecycle <> 'paid_awaiting_agreement' THEN 'closed'
    ELSE 'open' END
  FROM business.late_payment_offer f
  JOIN business.orders o ON o.id = f.order_id
  LEFT JOIN business.late_payment_answer a ON a.offer_id = f.id
  LEFT JOIN business.recovery_operation r ON r.id = a.command_id
  WHERE f.id = p_offer;
$$;

-- Owner-internal: the order's open offer (NULL while waiting).
CREATE FUNCTION business.late_payment_open_offer(p_order uuid)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT f.id FROM business.late_payment_offer f
  WHERE f.order_id = p_order AND business.late_payment_offer_state(f.id) = 'open'
  ORDER BY f.order_version DESC LIMIT 1;
$$;

-- Owner-internal: whether order p_o can take the published start p_slot of
-- p_day (with start p_starts when given) at instant p_at: published on a
-- non-closed day of the current schedule, after p_at, within the ready
-- cakes' shelf life, and on another day the order's live day place fits the
-- day's standard limit beside the other orders' committed standard cakes
-- (the day is locked: lock order K00, handover days after the order).
-- Returns the slot or nothing.
CREATE FUNCTION business.late_payment_slot_fits(p_o business.orders, p_day date, p_slot uuid, p_starts timestamptz,
                                                p_at timestamptz)
RETURNS TABLE (slot_id uuid, starts_at timestamptz, ends_at timestamptz, schedule_revision integer)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_t record;
  v_qty integer;
  v_limit integer;
  v_committed bigint;
BEGIN
  SELECT * INTO v_t FROM business.schedule_slots_on(p_day) s
  WHERE s.slot_id = p_slot AND (p_starts IS NULL OR s.starts_at = p_starts);
  IF NOT FOUND OR v_t.starts_at <= p_at OR NOT business.order_lots_last_until(p_o.id, v_t.ends_at) THEN
    RETURN;
  END IF;
  IF p_day <> p_o.handover_day THEN
    SELECT a.cake_quantity INTO v_qty FROM business.capacity_allocation a
    WHERE a.order_ref = p_o.id AND a.day = p_o.handover_day AND a.state <> 'released';
    IF FOUND THEN
      v_limit := business.capacity_day_lock(p_day);
      SELECT coalesce(sum(a.cake_quantity), 0) INTO v_committed FROM business.capacity_allocation a
      WHERE a.day = p_day AND a.classification = 'standard' AND a.state = 'committed' AND a.order_ref <> p_o.id;
      IF v_committed + v_qty > v_limit THEN
        RETURN;
      END IF;
    END IF;
  END IF;
  RETURN QUERY SELECT v_t.slot_id, v_t.starts_at, v_t.ends_at, v_t.schedule_revision;
END;
$$;

-- Owner-internal: an offer as the order page shows it. accept_until: the
-- instant from which it can no longer be accepted (the original handover
-- for requested, as the honour rule; the offered start for other); the API
-- adds acceptable at its own clock. Declining has no such limit.
CREATE FUNCTION business.late_payment_offer_json(f business.late_payment_offer)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN f.id IS NOT NULL THEN pg_catalog.jsonb_build_object(
    'id', f.id, 'kind', f.kind, 'date', f.handover_day, 'slot_id', f.slot_id, 'start', f.starts_at, 'end', f.ends_at,
    'offered_at', f.offered_at,
    'accept_until', CASE f.kind WHEN 'requested'
                      THEN (SELECT o.original_handover_at FROM business.orders o WHERE o.id = f.order_id)
                      ELSE f.starts_at END) END;
$$;

-- Owner-internal: the late_payment block of a paid_awaiting_agreement order
-- (NULL otherwise): waiting (no open offer: only "contact the chef"),
-- offered (the open offer can be answered) or processing (a protected
-- command of the order is unresolved: the customer's answer, with its offer,
-- or the chef's own decision or another change, with no offer to answer);
-- answer: the latest offer's answer when it did not resolve the order
-- (processing, lapsed or refused, with the reason); paid_minor and currency:
-- the actual paid aggregate a decline returns in full (server-computed, as
-- the cancellation preview).
CREATE FUNCTION business.order_late_payment_view(p_order business.orders)
RETURNS jsonb
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_latest business.late_payment_offer%ROWTYPE;
  v_open business.late_payment_offer%ROWTYPE;
  v_a business.late_payment_answer%ROWTYPE;
  v_state text;
  v_reason text;
  v_busy boolean := business.order_command_unresolved(p_order.id);
BEGIN
  IF p_order.lifecycle <> 'paid_awaiting_agreement' THEN
    RETURN NULL;
  END IF;
  SELECT * INTO v_latest FROM business.late_payment_offer f WHERE f.order_id = p_order.id
  ORDER BY f.order_version DESC LIMIT 1;
  SELECT * INTO v_open FROM business.late_payment_offer f WHERE f.id = business.late_payment_open_offer(p_order.id);
  IF v_latest.id IS NOT NULL THEN
    SELECT * INTO v_a FROM business.late_payment_answer a WHERE a.offer_id = v_latest.id;
  END IF;
  IF v_a.id IS NOT NULL THEN
    v_state := business.late_payment_offer_state(v_latest.id);
    SELECT r.outcome -> 'evidence' ->> 'reason' INTO v_reason
    FROM business.recovery_operation r WHERE r.id = v_a.command_id AND r.state = 'secured';
    IF v_a.lapsed THEN
      v_reason := 'slot_unavailable';
    END IF;
  END IF;
  RETURN pg_catalog.jsonb_build_object(
    'state', CASE WHEN v_state = 'processing' OR v_busy THEN 'processing'
                  WHEN v_open.id IS NOT NULL THEN 'offered' ELSE 'waiting' END,
    'offer', CASE WHEN v_state = 'processing' THEN business.late_payment_offer_json(v_latest)
                  WHEN v_busy THEN NULL
                  ELSE business.late_payment_offer_json(v_open) END,
    'answer', CASE WHEN v_a.id IS NOT NULL AND v_state IN ('processing', 'lapsed', 'refused') THEN
                pg_catalog.jsonb_build_object('offer_id', v_a.offer_id, 'answer', v_a.answer,
                                              'received_at', v_a.received_at, 'state', v_state, 'reason', v_reason) END,
    'paid_minor', business.order_paid_minor(p_order.id), 'currency', p_order.currency);
END;
$$;

-- Owner-internal: what the customer agreed on the order page, once that
-- acceptance is secured (K11); NULL otherwise.
CREATE FUNCTION business.order_late_payment_agreed(p_order uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object('at', a.received_at, 'kind', f.kind, 'date', f.handover_day, 'start', f.starts_at,
                                       'end', f.ends_at, 'offer_id', f.id)
  FROM business.late_payment_answer a
  JOIN business.late_payment_offer f ON f.id = a.offer_id
  JOIN business.recovery_operation r ON r.id = a.command_id
  WHERE a.order_id = p_order AND a.answer = 'accept' AND r.state = 'secured' AND r.outcome ->> 'status' = 'confirmed'
  ORDER BY a.created_at DESC LIMIT 1;
$$;

-- Owner-internal: the order's offers and answers for staff, oldest first
-- (no personal data, no money).
CREATE FUNCTION business.order_staff_late_payment_offers(p_order uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(business.late_payment_offer_json(f)
           || pg_catalog.jsonb_build_object(
                'state', business.late_payment_offer_state(f.id),
                'answer', CASE WHEN a.id IS NOT NULL THEN pg_catalog.jsonb_build_object(
                            'id', a.id, 'answer', a.answer, 'received_at', a.received_at,
                            'access', CASE a.actor_kind WHEN 'guest' THEN 'guest_link' ELSE 'account' END,
                            'command_id', a.command_id, 'lapsed', a.lapsed) END)
           ORDER BY f.order_version), '[]'::jsonb)
  FROM business.late_payment_offer f
  LEFT JOIN business.late_payment_answer a ON a.offer_id = f.id
  WHERE f.order_id = p_order;
$$;

-- ---------------------------------------------------------------- API

-- API: the chef's offer on a paid_awaiting_agreement order (see the
-- header). Returns the offer and whether it already existed.
CREATE FUNCTION business.order_late_payment_offer(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_kind text,
  p_target_day date, p_target_slot uuid, p_now timestamptz)
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
  v_open uuid;
  v_t record;
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_kind IS NULL OR p_kind NOT IN ('requested', 'other') THEN
    RAISE EXCEPTION 'offer_kind_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_kind = 'requested' AND (p_target_day IS NOT NULL OR p_target_slot IS NOT NULL) THEN
    RAISE EXCEPTION 'offer_target_invalid' USING ERRCODE = 'GK422';
  ELSIF p_kind = 'other' AND p_target_day IS NULL THEN
    RAISE EXCEPTION 'date_invalid' USING ERRCODE = 'GK422';
  ELSIF p_kind = 'other' AND p_target_slot IS NULL THEN
    RAISE EXCEPTION 'slot_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- The same key replays (the offer), also during a restore freeze.
  v_prior := business.command_claim(v_account, 'order.late_payment_offer', p_order::text, p_key, p_body_sha256);
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
  IF business.order_command_unresolved(p_order) THEN
    -- A decision, answer or other protected change is in progress (K11).
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.lifecycle <> 'paid_awaiting_agreement' OR v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  v_open := business.late_payment_open_offer(p_order);
  IF p_kind = 'requested' THEN
    -- The customer's own date, under the honour rule (K04 Q2).
    IF v_o.original_handover_at <= p_now THEN
      RAISE EXCEPTION 'honour_too_late' USING ERRCODE = 'GK409';
    END IF;
    INSERT INTO business.late_payment_offer (id, order_id, kind, handover_day, slot_id, starts_at, ends_at,
                                             schedule_revision, order_version, offered_by, offered_at)
    VALUES (v_id, p_order, 'requested', v_o.handover_day, v_o.slot_id, business.order_handover_at(v_o),
            v_o.handover_ends_at, NULL, v_o.version, v_account, p_now);
  ELSE
    IF p_target_day = v_o.handover_day
       AND (p_target_slot = v_o.slot_id
            OR EXISTS (SELECT 1 FROM business.schedule_slots_on(p_target_day) s
                       WHERE s.slot_id = p_target_slot AND s.starts_at = business.order_handover_at(v_o))) THEN
      -- The requested appointment itself: kind requested.
      RAISE EXCEPTION 'offer_target_invalid' USING ERRCODE = 'GK422';
    END IF;
    SELECT * INTO v_t FROM business.late_payment_slot_fits(v_o, p_target_day, p_target_slot, NULL, p_now) s;
    IF v_t.slot_id IS NULL THEN
      RAISE EXCEPTION 'slot_unavailable' USING ERRCODE = 'GK409';
    END IF;
    INSERT INTO business.late_payment_offer (id, order_id, kind, handover_day, slot_id, starts_at, ends_at,
                                             schedule_revision, order_version, offered_by, offered_at)
    VALUES (v_id, p_order, 'other', p_target_day, v_t.slot_id, v_t.starts_at, v_t.ends_at, v_t.schedule_revision,
            v_o.version, v_account, p_now);
  END IF;
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order;
  IF v_open IS NOT NULL THEN
    PERFORM business.audit_record('staff', v_account, 'order.late_payment_offer_withdrawn', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('offer_id', v_open, 'replaced_by', v_id));
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.late_payment_offered', 'order', p_order, NULL,
                                pg_catalog.json_build_object('offer_id', v_id, 'kind', p_kind));
  PERFORM business.command_finish(v_account, 'order.late_payment_offer', p_order::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- API: the customer's answer to offer p_offer of order p_order (see the
-- header). outcome 'pending' (the command, 202) or 'lapsed' (the offered
-- slot can no longer take the order: the answer is recorded without a
-- command, 409 slot_unavailable). Returns the command (or the lapsed
-- answer), whether it already existed and the outcome.
CREATE FUNCTION business.order_late_payment_answer(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_offer uuid, p_answer text, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean, outcome text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_l record;
  v_o business.orders%ROWTYPE;
  v_kind text;
  v_prior business.late_payment_answer%ROWTYPE;
  v_frozen boolean;
  v_f business.late_payment_offer%ROWTYPE;
  v_t record;
  v_answer uuid := pg_catalog.gen_random_uuid();
  v_id uuid := pg_catalog.gen_random_uuid();
  v_decision text;
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
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
  IF p_offer IS NULL THEN
    RAISE EXCEPTION 'offer_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_answer IS NULL OR p_answer NOT IN ('accept', 'decline') THEN
    RAISE EXCEPTION 'answer_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  -- The cancellation's access: the owner or a guest capability with "cancel".
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order, 'cancel') l;
  v_o := v_l.o_order;
  v_kind := CASE WHEN v_l.o_account IS NULL THEN 'guest' ELSE 'customer' END;
  -- The same actor and key replay the recorded answer, also while frozen.
  SELECT * INTO v_prior FROM business.late_payment_answer a
  WHERE a.order_id = p_order AND a.idempotency_key = p_key
    AND a.actor_account_id IS NOT DISTINCT FROM v_l.o_account AND a.guest_capability_id IS NOT DISTINCT FROM v_l.o_capability;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT coalesce(v_prior.command_id, v_prior.id), true,
                        CASE WHEN v_prior.lapsed THEN 'lapsed' ELSE 'pending' END;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  IF business.order_command_unresolved(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- Only the current open offer (none while waiting; silence is not an
  -- answer and a withdrawn or answered offer cannot be taken up).
  IF v_o.lifecycle <> 'paid_awaiting_agreement' OR v_o.recovery_state <> 'secured'
     OR business.late_payment_open_offer(p_order) IS DISTINCT FROM p_offer THEN
    RAISE EXCEPTION 'offer_not_current' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  SELECT * INTO v_f FROM business.late_payment_offer f WHERE f.id = p_offer;
  IF p_answer = 'accept' AND v_f.kind = 'requested' AND v_o.original_handover_at <= p_now THEN
    -- Too late to honour the requested date; declining stays possible.
    RAISE EXCEPTION 'honour_too_late' USING ERRCODE = 'GK409';
  END IF;
  IF p_answer = 'accept' AND v_f.kind = 'other' THEN
    SELECT * INTO v_t FROM business.late_payment_slot_fits(v_o, v_f.handover_day, v_f.slot_id, v_f.starts_at, p_now) s;
    IF v_t.slot_id IS NULL THEN
      -- The slot can no longer take the order: the offer lapses, nothing
      -- else changes; the chef must offer again.
      INSERT INTO business.late_payment_answer (id, offer_id, order_id, answer, command_id, lapsed, actor_kind,
                                                actor_account_id, guest_capability_id, idempotency_key, body_sha256,
                                                order_version, from_day, from_slot_id, from_starts_at, from_ends_at,
                                                received_at)
      VALUES (v_answer, p_offer, p_order, p_answer, NULL, true, v_kind, v_l.o_account, v_l.o_capability, p_key,
              p_body_sha256, v_o.version, v_o.handover_day, v_o.slot_id, business.order_handover_at(v_o),
              v_o.handover_ends_at, p_now);
      UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order;
      PERFORM business.audit_record(CASE v_kind WHEN 'guest' THEN 'system' ELSE v_kind END, v_l.o_account,
                                    'order.late_payment_offer_lapsed', 'order', p_order, NULL,
                                    pg_catalog.json_build_object('offer_id', p_offer, 'answer_id', v_answer,
                                                                 'reason', 'slot_unavailable'));
      RETURN QUERY SELECT v_answer, false, 'lapsed'::text;
      RETURN;
    END IF;
  END IF;

  -- The K11 late_payment command with the customer's answer as evidence.
  v_decision := CASE p_answer WHEN 'accept' THEN 'honour' ELSE 'full_return' END;
  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.late_payment', 'operation_id', v_id, 'order_id', p_order,
    'prior_version', v_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', v_kind, 'account_id', v_l.o_account),
    'decision', pg_catalog.json_build_object(
      'decision', v_decision,
      'channel', CASE WHEN v_decision = 'honour' THEN 'order_page' END,
      'agreed_at', CASE WHEN v_decision = 'honour' THEN p_now END,
      'evidence_ref', CASE WHEN v_decision = 'honour' THEN v_answer::text END),
    'answer', pg_catalog.json_build_object(
      'answer_id', v_answer, 'offer_id', p_offer, 'answer', p_answer, 'kind', v_f.kind,
      'to', pg_catalog.json_build_object('date', v_f.handover_day, 'slot_id', v_f.slot_id, 'starts_at', v_f.starts_at,
                                         'ends_at', v_f.ends_at),
      'from', pg_catalog.json_build_object('date', v_o.handover_day, 'slot_id', v_o.slot_id,
                                           'starts_at', business.order_handover_at(v_o), 'ends_at', v_o.handover_ends_at)),
    'order', business.order_recovery_snapshot(p_order))::text;
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_payload, 'UTF8')), 'hex');
  SELECT r.result INTO v_reserve
  FROM business.recovery_reserve(v_id, 'command', p_order, v_o.version, v_hash, v_key, NULL) r;
  IF v_reserve = 'busy' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  ELSIF v_reserve <> 'reserved' THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  v_job := business.job_enqueue(v_id, NULL);
  INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, guest_capability_id,
                                      idempotency_key, body_sha256, prior_version, received_at, payload, payload_sha256,
                                      envelope_key, job_id)
  VALUES (v_id, p_order, 'late_payment', v_kind, v_l.o_account, v_l.o_capability, p_key, p_body_sha256, v_o.version, p_now,
          v_payload, v_hash, v_key, v_job);
  -- The chef who made the offer recorded feasibility; the customer's
  -- button press is the agreement (accept) at the receipt instant.
  INSERT INTO business.late_payment_decision (command_id, decision, channel, agreed_at, evidence_ref, recorded_by, recorded_at)
  VALUES (v_id, v_decision, CASE WHEN v_decision = 'honour' THEN 'order_page' END,
          CASE WHEN v_decision = 'honour' THEN p_now END, CASE WHEN v_decision = 'honour' THEN v_answer::text END,
          v_f.offered_by, p_now);
  INSERT INTO business.late_payment_answer (id, offer_id, order_id, answer, command_id, lapsed, actor_kind, actor_account_id,
                                            guest_capability_id, idempotency_key, body_sha256, order_version, from_day,
                                            from_slot_id, from_starts_at, from_ends_at, received_at)
  VALUES (v_answer, p_offer, p_order, p_answer, v_id, false, v_kind, v_l.o_account, v_l.o_capability, p_key, p_body_sha256,
          v_o.version, v_o.handover_day, v_o.slot_id, business.order_handover_at(v_o), v_o.handover_ends_at, p_now);
  PERFORM business.audit_record(CASE v_kind WHEN 'guest' THEN 'system' ELSE v_kind END, v_l.o_account,
                                'order.late_payment_answered', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id, 'offer_id', p_offer, 'answer', p_answer,
                                                             'kind', v_f.kind));
  RETURN QUERY SELECT v_id, false, 'pending'::text;
END;
$$;

-- ------------------------------------------------------------- commit

-- As in 0023, plus the customer's answers: a decline returns everything
-- (source customer); an acceptance of kind 'other' moves the order to the
-- offered appointment and day place in the same commit, or is refused
-- slot_unavailable when the slot can no longer take it (the offer lapses).
-- The chef's own decisions are unchanged.
CREATE OR REPLACE FUNCTION business.order_late_payment_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_d business.late_payment_decision%ROWTYPE;
  v_a business.late_payment_answer%ROWTYPE;
  v_f business.late_payment_offer%ROWTYPE;
  v_snapshot json := (v.payload::json) -> 'order';
  v_t record;
  v_alloc business.capacity_allocation%ROWTYPE;
  v_actor text := CASE v.actor_kind WHEN 'guest' THEN 'system' ELSE v.actor_kind END;
  v_reason text;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  SELECT * INTO v_d FROM business.late_payment_decision d WHERE d.command_id = v.id;
  SELECT * INTO v_a FROM business.late_payment_answer a WHERE a.command_id = v.id;
  IF v_a.id IS NOT NULL THEN
    SELECT * INTO v_f FROM business.late_payment_offer f WHERE f.id = v_a.offer_id;
  END IF;
  IF v_o.version <> v.prior_version OR v_o.lifecycle <> 'paid_awaiting_agreement' OR v_o.recovery_state <> 'secured' THEN
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.late_payment_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', 'order_changed'));
    RETURN '{"status":"refused","evidence":{"reason":"order_changed"}}';
  END IF;
  IF v_d.decision = 'full_return' THEN
    -- Never 50%: the whole actual paid aggregate.
    RETURN business.order_terminal_return(v.order_id, v_o.version, 'late_payment_returned', 100::smallint,
                                          CASE WHEN v.actor_kind = 'staff' THEN 'chef' ELSE 'customer' END,
                                          v.actor_kind, v.actor_account_id, v.id, v.received_at);
  END IF;
  IF coalesce(v_f.kind, 'requested') = 'requested' AND v_o.original_handover_at <= v.received_at THEN
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.late_payment_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', 'honour_too_late'));
    RETURN '{"status":"refused","evidence":{"reason":"honour_too_late"}}';
  END IF;
  IF v_f.kind = 'other' AND v_f.handover_day <> v_o.handover_day THEN
    -- Lock order (K00): the handover days by date.
    PERFORM business.capacity_day_lock(LEAST(v_f.handover_day, v_o.handover_day));
    PERFORM business.capacity_day_lock(GREATEST(v_f.handover_day, v_o.handover_day));
  END IF;
  -- Honour: the original slot is still the order's and its holds are the
  -- committed ones (the place on its day; a released row of another day is
  -- no hold).
  IF v_o.slot_id::text IS DISTINCT FROM v_snapshot ->> 'slot_id'
     OR v_o.handover_day::text IS DISTINCT FROM v_snapshot ->> 'handover_day'
     OR EXISTS (SELECT 1 FROM business.capacity_allocation a WHERE a.order_ref = v.order_id AND a.state <> 'committed'
                  AND a.day = v_o.handover_day)
     OR EXISTS (SELECT 1 FROM business.order_lot_reservation r WHERE r.order_id = v.order_id AND r.state <> 'sold') THEN
    v_reason := 'holds_changed';
  ELSIF v_f.kind = 'other' THEN
    SELECT * INTO v_t FROM business.late_payment_slot_fits(v_o, v_f.handover_day, v_f.slot_id, v_f.starts_at,
                                                           v.received_at) s;
    IF v_t.slot_id IS NULL THEN
      v_reason := 'slot_unavailable';
    END IF;
  END IF;
  IF v_reason IS NOT NULL THEN
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.late_payment_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason));
    RETURN pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', v_reason);
  END IF;
  IF v_f.kind = 'other' THEN
    IF v_f.handover_day <> v_o.handover_day THEN
      -- The day place moves with the appointment (a standard place: the
      -- fit was checked under the day lock).
      SELECT * INTO v_alloc FROM business.capacity_allocation a
      WHERE a.order_ref = v.order_id AND a.day = v_o.handover_day AND a.state <> 'released' FOR UPDATE;
      IF FOUND THEN
        UPDATE business.capacity_allocation a SET state = 'released', expires_at = NULL, updated_at = pg_catalog.now()
        WHERE a.id = v_alloc.id;
        UPDATE business.capacity_allocation a
        SET state = 'committed', classification = 'standard', cake_quantity = v_alloc.cake_quantity, expires_at = NULL,
            chef_evidence = NULL, updated_at = pg_catalog.now()
        WHERE a.order_ref = v.order_id AND a.day = v_f.handover_day;
        IF NOT FOUND THEN
          INSERT INTO business.capacity_allocation (id, order_ref, day, cake_quantity, classification, state)
          VALUES (pg_catalog.gen_random_uuid(), v.order_id, v_f.handover_day, v_alloc.cake_quantity, 'standard', 'committed');
        END IF;
      END IF;
    END IF;
    UPDATE business.orders o
    SET handover_day = v_f.handover_day, slot_id = v_f.slot_id, handover_starts_at = v_f.starts_at,
        handover_ends_at = v_f.ends_at
    WHERE o.id = v.order_id;
  END IF;
  UPDATE business.orders o
  SET lifecycle = 'confirmed', recovery_state = 'confirmation_pending', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = v.order_id;
  PERFORM business.audit_record(v_actor, v.actor_account_id, 'order.late_payment_honoured', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'channel', v_d.channel,
                                                             'kind', coalesce(v_f.kind, 'requested')));
  IF v_f.kind IS NULL THEN
    RETURN pg_catalog.format('{"status":"confirmed","evidence":{"order_version":%s,"decision":"honour"}}', v_o.version + 1);
  END IF;
  RETURN pg_catalog.json_build_object(
    'status', 'confirmed',
    'evidence', pg_catalog.json_build_object('order_version', v_o.version + 1, 'decision', 'honour', 'answer_id', v_a.id,
                                             'offer_id', v_f.id, 'kind', v_f.kind, 'to_date', v_f.handover_day,
                                             'to_slot_id', v_f.slot_id))::text;
END;
$$;

-- ------------------------------------------------- replaced functions

-- As in 0022, except that a replay matches only the chef's own decisions
-- (actor_kind staff): customers' order-page answers are late_payment
-- commands too, and a staff member who is also the order's owner must never
-- replay their customer answer on the chef route.
CREATE OR REPLACE FUNCTION business.order_late_payment_request(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_decision text,
  p_channel text, p_agreed_at timestamptz, p_evidence_ref text, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_o business.orders%ROWTYPE;
  v_prior business.order_command%ROWTYPE;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
BEGIN
  IF p_now IS NULL OR p_order IS NULL THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' OR p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_decision IS NULL OR p_decision NOT IN ('honour', 'full_return') THEN
    RAISE EXCEPTION 'decision_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_decision = 'honour' THEN
    -- Silence never counts: the customer's agreement, fully evidenced.
    IF p_channel IS NULL OR p_channel NOT IN ('email', 'phone', 'instagram', 'whatsapp', 'in_person', 'other')
       OR p_agreed_at IS NULL OR p_agreed_at > p_now
       OR p_evidence_ref IS NULL OR p_evidence_ref <> pg_catalog.btrim(p_evidence_ref)
       OR pg_catalog.char_length(p_evidence_ref) NOT BETWEEN 1 AND 200
       OR p_evidence_ref ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]' THEN
      RAISE EXCEPTION 'agreement_invalid' USING ERRCODE = 'GK422';
    END IF;
  ELSIF p_channel IS NOT NULL OR p_agreed_at IS NOT NULL OR p_evidence_ref IS NOT NULL THEN
    RAISE EXCEPTION 'agreement_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT * INTO v_prior FROM business.order_command c
  WHERE c.order_id = p_order AND c.command = 'late_payment' AND c.actor_kind = 'staff' AND c.idempotency_key = p_key
    AND c.actor_account_id = v_account;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior.id, true;
    RETURN;
  END IF;
  IF EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
             WHERE c.order_id = p_order AND c.command IN ('late_payment', 'cancel') AND r.state <> 'secured') THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.lifecycle <> 'paid_awaiting_agreement' OR v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF p_decision = 'honour' THEN
    -- The agreement answers the late payment (never before it), and the
    -- original handover must still be ahead at the receipt instant.
    IF p_agreed_at < v_o.paid_at THEN
      RAISE EXCEPTION 'agreement_invalid' USING ERRCODE = 'GK422';
    END IF;
    IF v_o.original_handover_at <= p_now THEN
      RAISE EXCEPTION 'honour_too_late' USING ERRCODE = 'GK409';
    END IF;
  END IF;

  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.late_payment', 'operation_id', v_id, 'order_id', p_order,
    'prior_version', v_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', 'staff', 'account_id', v_account),
    'decision', pg_catalog.json_build_object('decision', p_decision, 'channel', p_channel, 'agreed_at', p_agreed_at,
                                             'evidence_ref', p_evidence_ref),
    'order', business.order_recovery_snapshot(p_order))::text;
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_payload, 'UTF8')), 'hex');
  SELECT r.result INTO v_reserve
  FROM business.recovery_reserve(v_id, 'command', p_order, v_o.version, v_hash, v_key, NULL) r;
  IF v_reserve = 'busy' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  ELSIF v_reserve <> 'reserved' THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  v_job := business.job_enqueue(v_id, NULL);
  INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, idempotency_key, body_sha256,
                                      prior_version, received_at, payload, payload_sha256, envelope_key, job_id)
  VALUES (v_id, p_order, 'late_payment', 'staff', v_account, p_key, p_body_sha256, v_o.version, p_now, v_payload, v_hash,
          v_key, v_job);
  INSERT INTO business.late_payment_decision (command_id, decision, channel, agreed_at, evidence_ref, recorded_by, recorded_at)
  VALUES (v_id, p_decision, p_channel, p_agreed_at, p_evidence_ref, v_account, p_now);
  PERFORM business.audit_record('staff', v_account, 'order.late_payment_decided', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id, 'decision', p_decision));
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- As in 0031, and while an accepted 'other' offer is committed but its
-- outcome is not secured, the appointment it replaces (K11).
CREATE OR REPLACE FUNCTION business.order_visible_appointment(p_order business.orders)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(
    (SELECT pg_catalog.jsonb_build_object('date', c.from_day, 'id', c.from_slot_id, 'starts_at', c.from_starts_at,
                                          'ends_at', c.from_ends_at)
     FROM business.pickup_change c JOIN business.recovery_operation r ON r.id = c.command_id
     WHERE c.order_id = p_order.id AND r.state = 'committed' AND r.outcome ->> 'status' = 'moved'
     ORDER BY c.created_at DESC, c.command_id DESC LIMIT 1),
    (SELECT pg_catalog.jsonb_build_object('date', a.from_day, 'id', a.from_slot_id, 'starts_at', a.from_starts_at,
                                          'ends_at', a.from_ends_at)
     FROM business.late_payment_answer a
     JOIN business.late_payment_offer f ON f.id = a.offer_id
     JOIN business.recovery_operation r ON r.id = a.command_id
     WHERE a.order_id = p_order.id AND f.kind = 'other' AND r.state = 'committed' AND r.outcome ->> 'status' = 'confirmed'
     ORDER BY a.created_at DESC, a.id DESC LIMIT 1),
    pg_catalog.jsonb_build_object('date', p_order.handover_day, 'id', p_order.slot_id,
                                  'starts_at', business.order_handover_at(p_order), 'ends_at', p_order.handover_ends_at));
$$;

-- As in 0032, plus late_payment (a paid_awaiting_agreement order: waiting,
-- offered or processing, the open offer and the latest offer's unresolved
-- answer) and agreed (an acceptance on the order page, once secured).
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

-- As in 0032, plus late_payment_offers (chef and technical support) and,
-- for technical support, late_payment and agreed (no money); fulfilment
-- sees none of it.
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
  IF v_view = 'chef' THEN
    v_result := (v #- '{payment,checkout_url}')
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
                     'audit', business.order_staff_audit(o.id));
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
                                       THEN (v -> 'cancellation') - ARRAY['amount_minor', 'rate'] ELSE v -> 'cancellation' END,
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
                  'late_payment', v -> 'late_payment', 'agreed', v -> 'agreed',
                  'late_payment_offers', business.order_staff_late_payment_offers(o.id),
                  'audit', business.order_staff_audit(o.id));
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
                  'fulfillment_history', business.order_staff_fulfillment(o.id, true));
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.staff_viewed', 'order', o.id, NULL,
                                pg_catalog.json_build_object('view', v_view));
  RETURN v_result::json;
END;
$$;

REVOKE ALL ON TABLE business.late_payment_offer FROM PUBLIC;
REVOKE ALL ON TABLE business.late_payment_answer FROM PUBLIC;
REVOKE ALL ON FUNCTION business.late_payment_offer_state(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.late_payment_open_offer(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.late_payment_slot_fits(business.orders, date, uuid, timestamptz, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.late_payment_offer_json(business.late_payment_offer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_late_payment_view(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_late_payment_agreed(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_late_payment_offers(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_late_payment_offer(uuid, uuid, text, text, integer, text, date, uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_late_payment_answer(uuid, bytea, uuid, text, text, integer, uuid, text, text, timestamptz)
  FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_late_payment_offer(uuid, uuid, text, text, integer, text, date, uuid, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_late_payment_answer(uuid, bytea, uuid, text, text, integer, uuid, text, text, timestamptz)
  TO gk_api;
