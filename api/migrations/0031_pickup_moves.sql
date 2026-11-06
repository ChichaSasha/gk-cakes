-- GK-031 B1 part 1: same-day pickup moves by the customer and date/time
-- amendments by the chef (contracts r6.5 K06 "POST /orders/{id}/pickup
-- accepts same-day target_slot_id,version,key. Require at least 6h before
-- current slot start, equality allowed", "Lock order+old/new slots
-- canonically, move atomically or keep original. No per-slot count
-- rejection; still require open fitting slot. Date changes only by chef
-- after customer agreement, feasibility/shelf-life review; no automatic
-- next-day/weekend skip. Preserve original refund deadline and preparation
-- anchor, keep old/new history", "For an authorized same-day pickup move on
-- an existing eligible order, reuse that order's day/physical allocation and
-- original accepted lead/payment promise ... do not reapply fresh 192h lead,
-- new-purchase stock or day-admission filters"; K05 "Moves never change
-- original deadline"; K11 accepted order-change protection; K08 notices;
-- owner decisions BO-GK-001.move_time 7f8b0043, BO-GK-001.move_date b0fe9fea,
-- BO-GK-001.refund_follows_move 31d16f84; owner data contract "move":
-- pickup_change(order, old/new slots, actor, agreement_ref, command/recorded_at,
-- reason)).
--
-- The current appointment. orders.original_handover_at stays the immutable
-- placement anchor of the refund deadline (K05); the appointment the order
-- now has is handover_day, slot_id, handover_starts_at (NULL: still the
-- original start, every order before this migration) and handover_ends_at.
-- The production anchor (payment or free confirmation + 48 h) never reads
-- the appointment, so no move resets it.
--
-- Customer move (business.order_pickup_move_request, API): the owner or a
-- guest capability holder with "move_pickup", collection and
-- customer_courier orders only (GK delivery has no self-service change,
-- K06), lifecycle confirmed / in_production / ready and secured (anything
-- else 409 move_not_eligible). The receipt instant is persisted with the
-- command; at it the current appointment start must be at least 6 hours
-- away (equality allowed, else 409 move_too_late), and the target must be a
-- published start of the current schedule on the same London day that is
-- not a closed date, starting after the receipt instant, whose 60 minutes
-- end before any ready cake of the order expires (else 409
-- slot_unavailable). A minimum preparation gap before the new slot is an
-- open question for the Creator (BO-GK-001.move_time sets the cutoff
-- against the current slot only) and is not applied. The day place and
-- ready cakes the order holds are reused: no lead time, day capacity, booking
-- horizon or new stock check, and no per-slot count. While any order command
-- is unresolved a new move is refused (409 state_conflict), so the no-op
-- check never compares against an appointment the customer cannot see yet;
-- otherwise moving to the slot the order already has is a no-op (no
-- command; 200 unchanged). A restore freeze
-- refuses new requests (GK503); the same key replays.
--
-- Chef amendment (business.order_appointment_change_request, chef): another
-- date and/or time for an order of any fulfilment mode in the same
-- lifecycles, only with the customer's recorded agreement (channel,
-- agreed_at after placement and not after the receipt, evidence reference;
-- silence never counts) and a feasibility note (shelf life, preparation):
-- K06 "Date changes only by chef after customer agreement,
-- feasibility/shelf-life review". The target is a published start of the
-- current schedule on a non-closed day, in the future at the receipt, within
-- the order's ready cakes' shelf life; no six-hour rule (the chef decides).
-- A new day is admitted as the chef's decision: the order's allocation on
-- the old day is released and committed on the new one, as standard when the
-- new day's committed standard cakes plus the order's fit its limit,
-- otherwise as a recorded chef_extra allocation whose evidence is the
-- feasibility note (0010's classification; soft limit, K04). The allocation
-- moves in the same commit as the appointment (the domain commit of the K11
-- command; securing only acknowledges it and never rolls it back), so the
-- staff day counts can show the new day for the short time before the
-- outcome is secured, while the order itself still shows its old day.
-- Accepted: deferring the allocation to the securing step would split one
-- domain change over two transactions.
--
-- Both are K11 order commands ('pickup_move', 'appointment_change') through
-- the A3a machinery: the request records the command, its exact payload
-- (order snapshot, from and to appointment), the business.pickup_change
-- history row and the worker job, and answers 202. The commit
-- (business.order_appointment_commit, from business.order_command_apply)
-- locks the order, then the handover day(s) in date order (pickup slots are
-- derived and have no rows: there is no per-slot cap and no separate slot
-- table, owner data contract "no separate slot-capacity table"), re-checks
-- version, state, the receipt-time cutoff and that the target is still
-- published, and either moves the appointment (and allocation) with the
-- version + 1, or records a refusal (only the version changes) so the
-- original appointment is kept. Until the outcome envelope is acknowledged
-- (secured) customers and staff see the original appointment and the change
-- as "processing" (business.order_visible_appointment). Securing queues the
-- pickup_moved / appointment_changed notice in the same transaction (0030
-- machinery, once per command). While a move is unresolved, production
-- start (manual and timer) waits, as it does for a cancellation.
--
-- Expand-only (K09): a nullable column, a new table, replaced CHECK
-- constraints, new functions; replaced functions keep their signatures,
-- results and grants. Lock order (K00): the order, the recovery operation,
-- the handover days by date, the allocations, history/audit/intents.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------- schema

ALTER TABLE business.orders ADD COLUMN handover_starts_at timestamptz NULL;

ALTER TABLE business.order_command DROP CONSTRAINT order_command_command_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_command_check CHECK (command IN ('free_confirm', 'paid_confirm', 'cancel', 'late_payment',
                                                                'manual_payout', 'manual_payout_outcome', 'pickup_move',
                                                                'appointment_change'));
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_appointment CHECK ((command <> 'pickup_move' OR actor_kind IN ('customer', 'guest'))
                                                 AND (command <> 'appointment_change' OR actor_kind = 'staff'));

ALTER TABLE business.order_notice DROP CONSTRAINT order_notice_kind_check;
ALTER TABLE business.order_notice
  ADD CONSTRAINT order_notice_kind_check CHECK (kind IN ('confirmed', 'paid_awaiting_agreement', 'released', 'cancelled',
                                                         'refund_returned', 'pickup_moved', 'appointment_changed'));

-- One row per requested appointment change (owner data contract "move"),
-- written with the command and never changed; its outcome is the command's
-- recovery operation (moved or refused). The history is these rows.
CREATE TABLE business.pickup_change (
  command_id        uuid PRIMARY KEY REFERENCES business.order_command (id),
  order_id          uuid NOT NULL REFERENCES business.orders (id),
  kind              text NOT NULL CHECK (kind IN ('pickup_move', 'appointment_change')),
  from_day          date NOT NULL,
  from_slot_id      uuid NOT NULL,
  from_starts_at    timestamptz NOT NULL,
  from_ends_at      timestamptz NOT NULL,
  to_day            date NOT NULL,
  to_slot_id        uuid NOT NULL,
  to_starts_at      timestamptz NOT NULL,
  to_ends_at        timestamptz NOT NULL,
  -- The schedule revision the target was published in at the receipt.
  schedule_revision integer NOT NULL CHECK (schedule_revision >= 1),
  actor_kind        text NOT NULL CHECK (actor_kind IN ('customer', 'guest', 'staff')),
  actor_account_id  uuid NULL REFERENCES business.account (id),
  -- Chef amendments: the customer's agreement and the feasibility note.
  agreement_channel text NULL CHECK (agreement_channel IN ('email', 'phone', 'instagram', 'whatsapp', 'in_person', 'other')),
  agreed_at         timestamptz NULL,
  agreement_ref     text NULL CHECK (agreement_ref = btrim(agreement_ref) AND char_length(agreement_ref) BETWEEN 1 AND 200
                                     AND agreement_ref !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  reason            text NULL CHECK (reason = btrim(reason) AND char_length(reason) BETWEEN 1 AND 500
                                     AND reason !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  -- The persisted command receipt instant (K00 time rules).
  received_at       timestamptz NOT NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pickup_change_length CHECK (from_ends_at = from_starts_at + interval '60 minutes'
                                         AND to_ends_at = to_starts_at + interval '60 minutes'),
  CONSTRAINT pickup_change_actor CHECK ((actor_kind IN ('customer', 'staff')) = (actor_account_id IS NOT NULL)),
  CONSTRAINT pickup_change_kind CHECK (
    (kind = 'pickup_move' AND actor_kind IN ('customer', 'guest') AND from_day = to_day
     AND agreement_channel IS NULL AND agreed_at IS NULL AND agreement_ref IS NULL AND reason IS NULL)
    OR (kind = 'appointment_change' AND actor_kind = 'staff' AND agreement_channel IS NOT NULL
        AND agreed_at IS NOT NULL AND agreed_at <= received_at AND agreement_ref IS NOT NULL AND reason IS NOT NULL))
);
CREATE INDEX pickup_change_order_idx ON business.pickup_change (order_id, created_at);
CREATE TRIGGER pickup_change_immutable BEFORE UPDATE OR DELETE ON business.pickup_change
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER pickup_change_no_truncate BEFORE TRUNCATE ON business.pickup_change
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- ------------------------------------------------------------ helpers

-- Owner-internal: whether a protected (K11) command of the order is not
-- secured yet.
CREATE FUNCTION business.order_command_unresolved(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.recovery_operation r
                 WHERE r.aggregate_id = p_order AND r.kind = 'command' AND r.state <> 'secured');
$$;

-- The customer's same-day move cutoff before the current appointment start
-- (BO-GK-001.move_time: 6 hours, equality allowed).
CREATE FUNCTION business.pickup_move_cutoff()
RETURNS interval
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$ SELECT interval '6 hours' $$;

-- Owner-internal: the published appointment starts of the current schedule
-- on p_day (slot IDs as business.schedule_public_slots derives them), when
-- p_day is an opening weekday that is not a closed date. No lead time, day
-- capacity, horizon or stock: those admit new purchases only (K06).
CREATE FUNCTION business.schedule_slots_on(p_day date)
RETURNS TABLE (slot_id uuid, starts_at timestamptz, ends_at timestamptz, schedule_revision integer)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.md5(cur.version_id::text || '|' || p_day::text || '|' || st.starts::text)::uuid,
         (p_day + st.starts) AT TIME ZONE 'Europe/London',
         ((p_day + st.starts) AT TIME ZONE 'Europe/London') + interval '60 minutes',
         cur.version_revision
  FROM business.schedule_current() cur
  JOIN business.schedule_start st
    ON st.schedule_version_id = cur.version_id AND st.weekday = extract(isodow FROM p_day)::smallint
  WHERE NOT EXISTS (SELECT 1 FROM business.schedule_closed_date c
                    WHERE c.schedule_version_id = cur.version_id AND c.day = p_day)
  ORDER BY 2;
$$;

-- Owner-internal: whether every ready cake of the order lasts until p_ends
-- (K06 shelf life: never moved past a physical cake's expiry).
CREATE FUNCTION business.order_lots_last_until(p_order uuid, p_ends timestamptz)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT NOT EXISTS (SELECT 1 FROM business.order_item i JOIN business.finished_cake_lot l ON l.id = i.physical_lot_id
                     WHERE i.order_id = p_order AND l.expires_at < p_ends);
$$;

-- Owner-internal: the order's current appointment start.
CREATE FUNCTION business.order_handover_at(p_order business.orders)
RETURNS timestamptz
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$ SELECT coalesce(p_order.handover_starts_at, p_order.original_handover_at) $$;

-- Owner-internal: the appointment customers and staff see (K11): while an
-- appointment change is committed but its outcome is not secured, the
-- appointment it replaces. {date, id, starts_at, ends_at}.
CREATE FUNCTION business.order_visible_appointment(p_order business.orders)
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
    pg_catalog.jsonb_build_object('date', p_order.handover_day, 'id', p_order.slot_id,
                                  'starts_at', business.order_handover_at(p_order), 'ends_at', p_order.handover_ends_at));
$$;

-- Owner-internal: whether the order is in a lifecycle whose appointment may
-- change (K06; K05 lifecycle before handover).
CREATE FUNCTION business.appointment_change_eligible(p_order business.orders)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$ SELECT p_order.lifecycle IN ('confirmed', 'in_production', 'ready') $$;

-- Owner-internal: record an accepted appointment change command on the
-- locked, checked order (as business.order_cancel_record): the exact
-- payload, the K11 operation, its job, the command, the history row and
-- the audit, in the caller's transaction.
CREATE FUNCTION business.order_appointment_record(
  p_o business.orders, p_command text, p_actor_kind text, p_account uuid, p_capability uuid, p_key text,
  p_body_sha256 text, p_now timestamptz, p_env text, p_to_day date, p_to_slot uuid, p_to_starts timestamptz,
  p_to_ends timestamptz, p_revision integer, p_channel text, p_agreed_at timestamptz, p_evidence_ref text, p_note text)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid := pg_catalog.gen_random_uuid();
  v_from_starts timestamptz := business.order_handover_at(p_o);
  v_key text;
  v_payload text;
  v_hash text;
  v_reserve text;
  v_job uuid;
BEGIN
  v_key := 'recovery/' || p_env || '/op/' || p_o.id::text || '/' || pg_catalog.lpad(p_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.' || p_command, 'operation_id', v_id, 'order_id', p_o.id,
    'prior_version', p_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', p_actor_kind, 'account_id', p_account),
    'appointment', pg_catalog.json_build_object(
      'from', pg_catalog.json_build_object('date', p_o.handover_day, 'slot_id', p_o.slot_id, 'starts_at', v_from_starts,
                                           'ends_at', p_o.handover_ends_at),
      'to', pg_catalog.json_build_object('date', p_to_day, 'slot_id', p_to_slot, 'starts_at', p_to_starts, 'ends_at', p_to_ends),
      'schedule_revision', p_revision,
      'agreement', CASE WHEN p_channel IS NOT NULL THEN pg_catalog.json_build_object(
                     'channel', p_channel, 'agreed_at', p_agreed_at, 'evidence_ref', p_evidence_ref) END,
      'feasibility_note', p_note),
    'order', business.order_recovery_snapshot(p_o.id))::text;
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_payload, 'UTF8')), 'hex');
  SELECT r.result INTO v_reserve
  FROM business.recovery_reserve(v_id, 'command', p_o.id, p_o.version, v_hash, v_key, NULL) r;
  IF v_reserve = 'busy' THEN
    -- Another protected operation on this order is unresolved.
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  ELSIF v_reserve <> 'reserved' THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  v_job := business.job_enqueue(v_id, NULL);
  INSERT INTO business.order_command (id, order_id, command, actor_kind, actor_account_id, guest_capability_id,
                                      idempotency_key, body_sha256, prior_version, received_at, payload, payload_sha256,
                                      envelope_key, job_id)
  VALUES (v_id, p_o.id, p_command, p_actor_kind, p_account, p_capability, p_key, p_body_sha256, p_o.version, p_now,
          v_payload, v_hash, v_key, v_job);
  INSERT INTO business.pickup_change (command_id, order_id, kind, from_day, from_slot_id, from_starts_at, from_ends_at,
                                      to_day, to_slot_id, to_starts_at, to_ends_at, schedule_revision, actor_kind,
                                      actor_account_id, agreement_channel, agreed_at, agreement_ref, reason, received_at)
  VALUES (v_id, p_o.id, p_command, p_o.handover_day, p_o.slot_id, v_from_starts, p_o.handover_ends_at, p_to_day, p_to_slot,
          p_to_starts, p_to_ends, p_revision, p_actor_kind, p_account, p_channel, p_agreed_at, p_evidence_ref, p_note, p_now);
  PERFORM business.audit_record(CASE p_actor_kind WHEN 'guest' THEN 'system' ELSE p_actor_kind END, p_account,
                                'order.' || p_command || '_requested', 'order', p_o.id, NULL,
                                pg_catalog.json_build_object('operation_id', v_id));
  RETURN v_id;
END;
$$;

-- Owner-internal: a replay of the same actor and key returns the earlier
-- command (another body is key_reused); NULL otherwise.
CREATE FUNCTION business.order_appointment_replay(p_order uuid, p_command text, p_account uuid, p_capability uuid,
                                                  p_key text, p_body_sha256 text)
RETURNS uuid
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_prior business.order_command%ROWTYPE;
BEGIN
  SELECT * INTO v_prior FROM business.order_command c
  WHERE c.order_id = p_order AND c.command = p_command AND c.idempotency_key = p_key
    AND c.actor_account_id IS NOT DISTINCT FROM p_account AND c.guest_capability_id IS NOT DISTINCT FROM p_capability;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  IF v_prior.body_sha256 <> p_body_sha256 THEN
    RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
  END IF;
  RETURN v_prior.id;
END;
$$;

CREATE FUNCTION business.appointment_check_common(p_key text, p_body_sha256 text, p_expected_version integer,
                                                  p_slot uuid, p_env text, p_now timestamptz)
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
  IF p_slot IS NULL THEN
    RAISE EXCEPTION 'slot_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
END;
$$;

-- ---------------------------------------------------------------- API

-- API: the customer's same-day move of order p_order to p_target_slot (see
-- the header). Returns the command (the recovery operation) and whether it
-- already existed; unchanged (with the order's ID) when the order already
-- has that appointment.
CREATE FUNCTION business.order_pickup_move_request(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_target_slot uuid, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean, unchanged boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_l record;
  v_o business.orders%ROWTYPE;
  v_prior uuid;
  v_t record;
  v_frozen boolean;
BEGIN
  PERFORM business.appointment_check_common(p_key, p_body_sha256, p_expected_version, p_target_slot, p_env, p_now);
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order, 'move_pickup') l;
  v_o := v_l.o_order;
  -- The same key replays, also during a restore freeze.
  v_prior := business.order_appointment_replay(p_order, 'pickup_move', v_l.o_account, v_l.o_capability, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true, false;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  IF NOT business.appointment_change_eligible(v_o) OR v_o.fulfillment_mode NOT IN ('collection', 'customer_courier') THEN
    RAISE EXCEPTION 'move_not_eligible' USING ERRCODE = 'GK409';
  END IF;
  IF business.order_command_unresolved(p_order) THEN
    -- Another protected change is in progress (K11): wait for it.
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  SELECT * INTO v_t FROM business.schedule_slots_on(v_o.handover_day) s WHERE s.slot_id = p_target_slot;
  IF v_o.slot_id = p_target_slot OR v_t.starts_at = business.order_handover_at(v_o) THEN
    -- Already this appointment: nothing to move.
    RETURN QUERY SELECT p_order, true, true;
    RETURN;
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- At the persisted receipt instant: at least 6 hours before the current
  -- start, equality allowed (BO-GK-001.move_time).
  IF p_now > business.order_handover_at(v_o) - business.pickup_move_cutoff() THEN
    RAISE EXCEPTION 'move_too_late' USING ERRCODE = 'GK409';
  END IF;
  IF v_t.slot_id IS NULL OR v_t.starts_at <= p_now OR NOT business.order_lots_last_until(p_order, v_t.ends_at) THEN
    RAISE EXCEPTION 'slot_unavailable' USING ERRCODE = 'GK409';
  END IF;
  RETURN QUERY SELECT business.order_appointment_record(v_o, 'pickup_move',
                        CASE WHEN v_l.o_account IS NULL THEN 'guest' ELSE 'customer' END, v_l.o_account, v_l.o_capability,
                        p_key, p_body_sha256, p_now, p_env, v_o.handover_day, v_t.slot_id, v_t.starts_at, v_t.ends_at,
                        v_t.schedule_revision, NULL, NULL, NULL, NULL),
                      false, false;
END;
$$;

-- API: the chef's date/time amendment of order p_order with the customer's
-- recorded agreement (see the header).
CREATE FUNCTION business.order_appointment_change_request(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_target_day date,
  p_target_slot uuid, p_channel text, p_agreed_at timestamptz, p_evidence_ref text, p_note text, p_env text,
  p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean, unchanged boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_o business.orders%ROWTYPE;
  v_prior uuid;
  v_t record;
  v_frozen boolean;
BEGIN
  PERFORM business.appointment_check_common(p_key, p_body_sha256, p_expected_version, p_target_slot, p_env, p_now);
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_target_day IS NULL THEN
    RAISE EXCEPTION 'date_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- Silence never counts: the customer's agreement, fully evidenced.
  IF p_channel IS NULL OR p_channel NOT IN ('email', 'phone', 'instagram', 'whatsapp', 'in_person', 'other')
     OR p_agreed_at IS NULL OR p_agreed_at > p_now
     OR p_evidence_ref IS NULL OR p_evidence_ref <> pg_catalog.btrim(p_evidence_ref)
     OR pg_catalog.char_length(p_evidence_ref) NOT BETWEEN 1 AND 200
     OR p_evidence_ref ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]' THEN
    RAISE EXCEPTION 'appointment_agreement_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_note IS NULL OR p_note <> pg_catalog.btrim(p_note) OR pg_catalog.char_length(p_note) NOT BETWEEN 1 AND 500
     OR p_note ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]' THEN
    RAISE EXCEPTION 'feasibility_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v_prior := business.order_appointment_replay(p_order, 'appointment_change', v_account, NULL, p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true, false;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  IF NOT business.appointment_change_eligible(v_o) THEN
    RAISE EXCEPTION 'move_not_eligible' USING ERRCODE = 'GK409';
  END IF;
  IF business.order_command_unresolved(p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF p_agreed_at < v_o.placed_at THEN
    RAISE EXCEPTION 'appointment_agreement_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT * INTO v_t FROM business.schedule_slots_on(p_target_day) s WHERE s.slot_id = p_target_slot;
  IF p_target_day = v_o.handover_day AND (v_o.slot_id = p_target_slot OR v_t.starts_at = business.order_handover_at(v_o)) THEN
    RETURN QUERY SELECT p_order, true, true;
    RETURN;
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.recovery_state <> 'secured' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_t.slot_id IS NULL OR v_t.starts_at <= p_now OR NOT business.order_lots_last_until(p_order, v_t.ends_at) THEN
    RAISE EXCEPTION 'slot_unavailable' USING ERRCODE = 'GK409';
  END IF;
  RETURN QUERY SELECT business.order_appointment_record(v_o, 'appointment_change', 'staff', v_account, NULL, p_key,
                        p_body_sha256, p_now, p_env, p_target_day, v_t.slot_id, v_t.starts_at, v_t.ends_at,
                        v_t.schedule_revision, p_channel, p_agreed_at, p_evidence_ref, p_note),
                      false, false;
END;
$$;

-- API: the same-day move choices of order p_order (GET /schedule
-- purpose=pickup_move), for its owner or a guest capability holder with
-- "move_pickup" (404 otherwise). One row per published start of the order's
-- day (or one row without a slot when there is none). day_status: open |
-- not_eligible (lifecycle, GK delivery, not secured) | pending (an order
-- command is unresolved) | too_late (the six-hour cutoff passed) | closed
-- (no published start on the day). availability available, or unavailable
-- with reason_code current_slot | past (starts at or before now) | shelf_life
-- | the day status.
-- No lead time, day capacity, horizon or new stock (K06): the order's own
-- allocation is reused, so a full day never hides a valid move.
CREATE FUNCTION business.schedule_move_slots(p_owner uuid, p_guest_digest bytea, p_order uuid, p_now timestamptz)
RETURNS TABLE (order_day date, order_mode text, current_slot_id uuid, current_starts_at timestamptz,
               current_ends_at timestamptz, move_until timestamptz, day_status text, schedule_revision integer,
               slot_id uuid, starts_at timestamptz, ends_at timestamptz, availability text, reason_code text)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_a jsonb;
  v_current timestamptz;
  v_status text;
BEGIN
  IF (p_owner IS NULL) = (p_guest_digest IS NULL) OR p_order IS NULL OR p_now IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT * INTO v_o FROM business.orders o
  WHERE o.id = p_order
    AND ((p_owner IS NOT NULL AND o.owner_account_id = (SELECT a.id FROM business.account a
                                                       WHERE a.provider_subject = p_owner AND a.disabled_at IS NULL))
         OR (p_guest_digest IS NOT NULL AND EXISTS (
               SELECT 1 FROM business.order_guest_capability g
               WHERE g.order_id = o.id AND g.capability_digest = p_guest_digest AND g.revoked_at IS NULL
                 AND 'move_pickup' = ANY (g.allowed_commands))));
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  v_a := business.order_visible_appointment(v_o);
  v_current := (v_a ->> 'starts_at')::timestamptz;
  v_status := CASE
    WHEN NOT business.appointment_change_eligible(v_o) OR v_o.fulfillment_mode NOT IN ('collection', 'customer_courier')
         OR v_o.recovery_state <> 'secured' THEN 'not_eligible'
    WHEN business.order_command_unresolved(v_o.id) THEN 'pending'
    WHEN p_now > v_current - business.pickup_move_cutoff() THEN 'too_late'
    WHEN NOT EXISTS (SELECT 1 FROM business.schedule_slots_on(v_o.handover_day)) THEN 'closed'
    ELSE 'open' END;
  RETURN QUERY
  SELECT v_o.handover_day, v_o.fulfillment_mode, (v_a ->> 'id')::uuid, v_current, (v_a ->> 'ends_at')::timestamptz,
         CASE WHEN v_o.fulfillment_mode IN ('collection', 'customer_courier') THEN v_current - business.pickup_move_cutoff() END,
         v_status, coalesce(s.schedule_revision, (SELECT c.version_revision FROM business.schedule_current() c)),
         s.slot_id, s.starts_at, s.ends_at,
         CASE WHEN s.slot_id IS NULL THEN NULL
              WHEN v_status = 'open' AND s.starts_at <> v_current AND s.starts_at > p_now
                   AND business.order_lots_last_until(v_o.id, s.ends_at) THEN 'available'
              ELSE 'unavailable' END,
         CASE WHEN s.slot_id IS NULL THEN NULL
              WHEN s.starts_at = v_current THEN 'current_slot'
              WHEN v_status <> 'open' THEN v_status
              WHEN s.starts_at <= p_now THEN 'past'
              WHEN NOT business.order_lots_last_until(v_o.id, s.ends_at) THEN 'shelf_life' END
  FROM (SELECT 1) one
  LEFT JOIN business.schedule_slots_on(v_o.handover_day) s ON true
  ORDER BY s.starts_at;
END;
$$;

-- ------------------------------------------------------------- commit

-- Owner-internal: the domain commit of a prepared pickup_move or
-- appointment_change command (the caller, business.order_command_apply,
-- holds the order and the recovery operation and has checked the freeze).
-- Returns the outcome text.
CREATE FUNCTION business.order_appointment_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  c business.pickup_change%ROWTYPE;
  v_reason text;
  v_limit integer;
  v_alloc business.capacity_allocation%ROWTYPE;
  v_committed bigint;
  v_class text;
  v_outcome text;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id;
  SELECT * INTO c FROM business.pickup_change p WHERE p.command_id = v.id;
  -- Lock order (K00): the handover days by date (slots have no rows).
  IF c.from_day < c.to_day THEN
    PERFORM business.capacity_day_lock(c.from_day);
  END IF;
  v_limit := business.capacity_day_lock(c.to_day);
  IF c.from_day > c.to_day THEN
    PERFORM business.capacity_day_lock(c.from_day);
  END IF;

  IF v_o.version <> v.prior_version OR v_o.handover_day <> c.from_day OR v_o.slot_id <> c.from_slot_id
     OR business.order_handover_at(v_o) <> c.from_starts_at THEN
    v_reason := 'order_changed';
  ELSIF NOT business.appointment_change_eligible(v_o) OR v_o.recovery_state <> 'secured'
        OR (v.command = 'pickup_move' AND v_o.fulfillment_mode NOT IN ('collection', 'customer_courier')) THEN
    v_reason := 'not_eligible';
  ELSIF v.command = 'pickup_move' AND v.received_at > c.from_starts_at - business.pickup_move_cutoff() THEN
    v_reason := 'too_late';
  ELSIF NOT EXISTS (SELECT 1 FROM business.schedule_slots_on(c.to_day) s
                    WHERE s.slot_id = c.to_slot_id AND s.starts_at = c.to_starts_at)
        OR NOT business.order_lots_last_until(v.order_id, c.to_ends_at) THEN
    v_reason := 'slot_unavailable';
  END IF;

  IF v_reason IS NOT NULL THEN
    -- The original appointment stays; later commands reserve the next version.
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.' || v.command || '_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason));
    RETURN pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', v_reason);
  END IF;

  IF c.from_day <> c.to_day THEN
    -- The chef's decision admits the new day (K04 soft limit; 0010
    -- classification): standard within the limit, otherwise a recorded
    -- chef_extra allocation. The old day's place is released.
    SELECT * INTO v_alloc FROM business.capacity_allocation a
    WHERE a.order_ref = v.order_id AND a.day = c.from_day AND a.state <> 'released' FOR UPDATE;
    IF FOUND THEN
      SELECT coalesce(sum(a.cake_quantity), 0) INTO v_committed FROM business.capacity_allocation a
      WHERE a.day = c.to_day AND a.classification = 'standard' AND a.state = 'committed' AND a.order_ref <> v.order_id;
      v_class := CASE WHEN v_committed + v_alloc.cake_quantity <= v_limit THEN 'standard' ELSE 'chef_extra' END;
      UPDATE business.capacity_allocation a SET state = 'released', expires_at = NULL, updated_at = pg_catalog.now()
      WHERE a.id = v_alloc.id;
      UPDATE business.capacity_allocation a
      SET state = 'committed', classification = v_class, cake_quantity = v_alloc.cake_quantity, expires_at = NULL,
          chef_evidence = CASE WHEN v_class = 'chef_extra' THEN c.reason END, updated_at = pg_catalog.now()
      WHERE a.order_ref = v.order_id AND a.day = c.to_day;
      IF NOT FOUND THEN
        INSERT INTO business.capacity_allocation (id, order_ref, day, cake_quantity, classification, state, chef_evidence)
        VALUES (pg_catalog.gen_random_uuid(), v.order_id, c.to_day, v_alloc.cake_quantity, v_class, 'committed',
                CASE WHEN v_class = 'chef_extra' THEN c.reason END);
      END IF;
    END IF;
  END IF;
  UPDATE business.orders o
  SET handover_day = c.to_day, slot_id = c.to_slot_id, handover_starts_at = c.to_starts_at, handover_ends_at = c.to_ends_at,
      version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = v.order_id;
  v_outcome := pg_catalog.json_build_object(
    'status', 'moved',
    'evidence', pg_catalog.json_build_object('order_version', v_o.version + 1, 'from_slot_id', c.from_slot_id,
                                             'to_slot_id', c.to_slot_id, 'to_date', c.to_day, 'allocation', v_class))::text;
  PERFORM business.audit_record(CASE v.actor_kind WHEN 'guest' THEN 'system' ELSE v.actor_kind END, v.actor_account_id,
                                CASE v.command WHEN 'pickup_move' THEN 'order.pickup_moved' ELSE 'order.appointment_changed' END,
                                'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'allocation', v_class));
  RETURN v_outcome;
END;
$$;

-- Owner-internal: the order's appointment history for staff, oldest
-- first; the agreement and feasibility note for the chef only.
CREATE FUNCTION business.order_staff_appointments(p_order uuid, p_chef boolean)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
           'command_id', c.command_id, 'kind', c.kind,
           'requested_by', CASE c.actor_kind WHEN 'staff' THEN 'chef' ELSE 'customer' END,
           'received_at', c.received_at,
           'state', CASE WHEN r.state <> 'secured' THEN 'processing' ELSE r.outcome ->> 'status' END,
           'record_state', r.state,
           'reason', CASE WHEN r.state IN ('committed', 'secured') THEN r.outcome -> 'evidence' ->> 'reason' END,
           'allocation', CASE WHEN r.state IN ('committed', 'secured') THEN r.outcome -> 'evidence' ->> 'allocation' END,
           'from', pg_catalog.jsonb_build_object('date', c.from_day, 'id', c.from_slot_id, 'starts_at', c.from_starts_at,
                                                 'ends_at', c.from_ends_at),
           'to', pg_catalog.jsonb_build_object('date', c.to_day, 'id', c.to_slot_id, 'starts_at', c.to_starts_at,
                                               'ends_at', c.to_ends_at))
           || CASE WHEN p_chef AND c.kind = 'appointment_change' THEN pg_catalog.jsonb_build_object(
                'agreement', pg_catalog.jsonb_build_object('channel', c.agreement_channel, 'agreed_at', c.agreed_at,
                                                           'evidence_ref', c.agreement_ref),
                'feasibility_note', c.reason) ELSE '{}'::jsonb END
           ORDER BY c.created_at, c.command_id), '[]'::jsonb)
  FROM business.pickup_change c
  JOIN business.recovery_operation r ON r.id = c.command_id
  WHERE c.order_id = p_order;
$$;

-- ------------------------------------------------- replaced functions

-- As in 0029, with the visible appointment (date and slot: the one being
-- replaced while a change is not secured), the latest appointment change
-- (kind, state processing | moved | refused, refusal reason, requester,
-- receipt and target) and pickup_move_until (the customer's six-hour cutoff;
-- collection and courier orders only).
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
           'refund', CASE WHEN o.recovery_state <> 'cancellation_pending' THEN business.order_refund_summary(o.id) END,
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

-- As in 0029, but on the visible appointment (K11): the list filters on
-- the London day the order shows (a committed but unsecured date change
-- keeps it on its old day) and orders and pages by the visible start (the
-- cursor's handover_at is that start), so a secured move re-sorts the order.
CREATE OR REPLACE FUNCTION business.order_staff_list(p_actor uuid, p_from date, p_to date, p_lifecycles text[],
                                          p_needs_attention boolean, p_after_handover timestamptz,
                                          p_after_placed timestamptz, p_after_id uuid, p_limit integer)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support', 'fulfillment']);
  v_view text := business.order_staff_view_of(v_account);
  v_items jsonb;
  v_next jsonb;
  v_count integer;
BEGIN
  IF p_from IS NULL OR p_to IS NULL OR p_to < p_from OR p_to - p_from > 92 THEN
    RAISE EXCEPTION 'range_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_limit IS NULL OR p_limit < 1 OR p_limit > 100 THEN
    RAISE EXCEPTION 'page_limit_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_lifecycles IS NOT NULL
     AND (pg_catalog.cardinality(p_lifecycles) NOT BETWEEN 1 AND 9
          OR NOT p_lifecycles <@ ARRAY['awaiting_payment', 'confirmation_pending', 'paid_awaiting_agreement', 'confirmed',
                                       'in_production', 'ready', 'completed', 'cancellation_pending', 'cancelled']) THEN
    RAISE EXCEPTION 'lifecycle_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF NOT ((p_after_handover IS NULL AND p_after_placed IS NULL AND p_after_id IS NULL)
          OR (p_after_handover IS NOT NULL AND p_after_placed IS NOT NULL AND p_after_id IS NOT NULL)) THEN
    RAISE EXCEPTION 'cursor_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF v_view = 'fulfillment' AND p_needs_attention IS NOT NULL THEN
    RAISE EXCEPTION 'the needs_attention filter requires the chef or technical_support capability' USING ERRCODE = 'GK403';
  END IF;
  WITH cand AS (
    -- The stored day, or the day still shown while a date change is
    -- committed but not secured (the prefilter keeps the day index useful).
    SELECT o, (business.order_visible_appointment(o) ->> 'date')::date AS shown_day,
           (business.order_visible_appointment(o) ->> 'starts_at')::timestamptz AS shown_at
    FROM business.orders o
    WHERE (o.handover_day BETWEEN p_from AND p_to
           OR EXISTS (SELECT 1 FROM business.pickup_change c JOIN business.recovery_operation r ON r.id = c.command_id
                      WHERE c.order_id = o.id AND r.state = 'committed' AND c.from_day BETWEEN p_from AND p_to))
      AND (v_view <> 'fulfillment' OR business.order_fulfillment_scope(o.id))
  ), base AS (
    SELECT c.o, c.shown_at, business.order_visible_lifecycle(c.o) AS shown,
           pg_catalog.cardinality(business.order_attention_flags(c.o)) > 0 AS attention
    FROM cand c
    WHERE c.shown_day BETWEEN p_from AND p_to
  ), page AS (
    SELECT b.o, b.shown_at, pg_catalog.row_number() OVER (ORDER BY b.shown_at, (b.o).placed_at, (b.o).id) AS n
    FROM base b
    WHERE (p_lifecycles IS NULL OR b.shown = ANY (p_lifecycles))
      AND (p_needs_attention IS NULL OR b.attention = p_needs_attention)
      AND (p_after_id IS NULL
           OR (b.shown_at, (b.o).placed_at, (b.o).id) > (p_after_handover, p_after_placed, p_after_id))
    ORDER BY b.shown_at, (b.o).placed_at, (b.o).id
    LIMIT p_limit + 1
  )
  SELECT coalesce(pg_catalog.jsonb_agg(business.order_staff_row(p.o, v_view) ORDER BY p.n)
                    FILTER (WHERE p.n <= p_limit), '[]'::jsonb),
         CASE WHEN pg_catalog.max(p.n) > p_limit THEN
           (SELECT pg_catalog.jsonb_build_object('handover_at', l.shown_at, 'placed_at', (l.o).placed_at,
                                                 'id', (l.o).id)
            FROM page l WHERE l.n = p_limit) END
  INTO v_items, v_next
  FROM page p;
  v_count := pg_catalog.jsonb_array_length(v_items);
  PERFORM business.audit_record('staff', v_account, 'order.staff_listed', 'account', v_account, NULL,
                                pg_catalog.json_build_object('view', v_view, 'from', p_from, 'to', p_to, 'items', v_count));
  RETURN pg_catalog.json_build_object('items', v_items, 'next', v_next);
END;
$$;

-- As in 0029, with the visible appointment.
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
           'production', business.order_production_view(p_order.id))
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
                                  FROM business.payment_collection pc WHERE pc.order_id = p_order.id
                                  ORDER BY pc.created_at DESC, pc.id DESC LIMIT 1),
                'paid_at', CASE WHEN p_order.recovery_state IN ('secured', 'cancellation_pending') THEN p_order.paid_at END,
                'free_confirmed_at', CASE WHEN p_order.recovery_state IN ('secured', 'cancellation_pending')
                                          THEN p_order.free_confirmed_at END,
                'refund', CASE WHEN p_order.recovery_state <> 'cancellation_pending'
                               THEN business.order_refund_summary(p_order.id)::jsonb END)
            ELSE '{}'::jsonb END;
$$;

-- As in 0029, plus the appointment history (business.order_staff_appointments;
-- agreement evidence for the chef only) and, for technical support and
-- fulfillment, the latest appointment change.
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
                  'appointment_history', business.order_staff_appointments(o.id, false));
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.staff_viewed', 'order', o.id, NULL,
                                pg_catalog.json_build_object('view', v_view));
  RETURN v_result::json;
END;
$$;

-- As in 0030, with the order's current appointment (not the original one
-- after a move) and two new notices. pickup_moved / appointment_changed:
-- p_facts is the history row's from/to appointment and receipt, so a
-- replayed transition renders the same bytes. Moves never change the refund
-- terms (K05; BO-GK-001.refund_follows_move), and the copy says so.
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
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nIt was planned for: ' || v_when);
  ELSIF p_kind = 'refund_returned' THEN
    subject := 'Your GK Cakes refund has been returned';
    v_text := v_text || ('We have returned ' || business.notice_money((p_facts ->> 'target_minor')::bigint)
                         || CASE WHEN (p_facts ->> 'goal_version')::integer > 1 THEN ' in total' ELSE '' END
                         || ' to you for this order. Depending on your bank, it can take a few working days to show on your statement.');
    v_text := v_text || ('Order reference: ' || p_o.id::text);
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

-- As in 0030, with the template revisions of the two new notices.
CREATE OR REPLACE FUNCTION business.order_notice_enqueue(p_order uuid, p_kind text, p_dedupe text, p_transition uuid, p_facts json)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_s business.notice_settings%ROWTYPE;
  v_o business.orders%ROWTYPE;
  v_rev text := CASE p_kind WHEN 'confirmed' THEN 'order-confirmed.v1' WHEN 'paid_awaiting_agreement' THEN 'order-paid-late.v1'
                            WHEN 'released' THEN 'order-released.v1' WHEN 'cancelled' THEN 'order-cancelled.v1'
                            WHEN 'refund_returned' THEN 'refund-returned.v1' WHEN 'pickup_moved' THEN 'pickup-moved.v1'
                            WHEN 'appointment_changed' THEN 'appointment-changed.v1' END;
  v_name text := p_order::text || ':' || p_kind || ':' || p_dedupe;
  v_id uuid;
  v_key uuid;
  v_subject text;
  v_text text;
  v_html text;
  v_payload text;
  v_outcome text;
  v_detail text;
BEGIN
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('gk.order_notice:' || v_name, 0));
  IF EXISTS (SELECT 1 FROM business.order_notice n WHERE n.order_id = p_order AND n.kind = p_kind AND n.dedupe = p_dedupe) THEN
    RETURN 'exists';
  END IF;
  SELECT * INTO v_s FROM business.notice_settings s WHERE s.singleton;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order;
  IF v_s.singleton IS NULL THEN
    v_outcome := 'not_configured';
  ELSIF NOT business.notice_address_ok(v_o.contact_email) THEN
    v_outcome := 'not_queued';
    v_detail := 'recipient';
  ELSIF NOT business.notice_recipient_allowed(v_s, v_o.contact_email) THEN
    -- Outside production, a recipient off the allowlist is never queued, so
    -- no worker build (old, failed or rolled back) can send to it.
    v_outcome := 'not_queued';
    v_detail := 'allowlist';
  ELSE
    v_id := business.notice_uuid(v_s.environment || ':order_notice:intent:' || v_name);
    v_key := business.notice_uuid(v_s.environment || ':order_notice:key:' || v_name);
    BEGIN
      SELECT r.subject, r.body_text, r.body_html INTO v_subject, v_text, v_html
      FROM business.order_notice_render(p_kind, v_o, p_facts, v_s.ui_origin) r;
      -- notifications payload and Request, field for field (canonical form).
      v_payload := '{"provider":"resend","environment":' || business.notice_json(v_s.environment)
        || ',"idempotency_key":' || business.notice_json(v_key::text)
        || ',"request":{"from":' || business.notice_json('GK Cakes <orders@gkcake.com>')
        || ',"to":[' || business.notice_json(v_o.contact_email) || ']'
        || ',"reply_to":[' || business.notice_json('gk.cake.london@gmail.com') || ']'
        || ',"subject":' || business.notice_json(v_subject)
        || ',"text":' || business.notice_json(v_text)
        || ',"html":' || business.notice_json(v_html)
        || ',"tags":[{"name":"intent","value":' || business.notice_json(v_id::text) || '},{"name":"env","value":'
        || business.notice_json(v_s.environment) || '}]}}';
      PERFORM business.notification_enqueue(v_id, p_transition, p_order, v_rev, v_key, v_payload::json, 'system', NULL);
      v_outcome := 'queued';
    EXCEPTION WHEN OTHERS THEN
      v_outcome := 'not_queued';
      v_detail := pg_catalog.lower(SQLSTATE);
    END;
  END IF;
  INSERT INTO business.order_notice (id, order_id, kind, dedupe, transition_id, template_revision, outcome, intent_id, detail)
  VALUES (pg_catalog.gen_random_uuid(), p_order, p_kind, p_dedupe, p_transition, v_rev, v_outcome,
          CASE WHEN v_outcome = 'queued' THEN v_id END, v_detail)
  -- A concurrent transaction that recorded it first (the advisory lock
  -- makes this unlikely) must not fail this transition at commit.
  ON CONFLICT ON CONSTRAINT order_notice_once DO NOTHING;
  IF v_outcome <> 'queued' THEN
    -- Visible to staff (the order's audit summary; attention flag
    -- notice_not_sent), without personal data.
    PERFORM business.audit_record('system', NULL, 'order.notice_not_queued', 'order', p_order, p_transition,
                                  pg_catalog.json_build_object('kind', p_kind, 'outcome', v_outcome, 'detail', v_detail));
  END IF;
  RETURN v_outcome;
END;
$$;

-- As in 0030, plus the pickup_moved / appointment_changed notice when a
-- moved appointment is secured.
CREATE OR REPLACE FUNCTION business.order_command_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_state text;
  v_status text;
  v_evidence json;
BEGIN
  PERFORM 1 FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  v_state := business.recovery_mark_secured(v.id);
  SELECT r.outcome ->> 'status', r.outcome -> 'evidence' INTO v_status, v_evidence
  FROM business.recovery_operation r WHERE r.id = v.id;
  UPDATE business.orders o SET recovery_state = 'secured', updated_at = pg_catalog.now()
  WHERE o.id = v.order_id
    AND ((o.recovery_state = 'confirmation_pending' AND v.command <> 'cancel'
          AND v_status IN ('confirmed', 'paid_awaiting_agreement'))
         OR (o.recovery_state = 'cancellation_pending' AND v.command IN ('cancel', 'late_payment')
             AND v_status = 'cancelled'));
  IF FOUND THEN
    -- A cancellation's refund may already have succeeded before the order
    -- was secured: the email then says so instead of "requested".
    PERFORM business.order_notice_enqueue(v.order_id, v_status, v.id::text, v.id,
      CASE WHEN v_status = 'cancelled'
           THEN (v_evidence::jsonb || pg_catalog.jsonb_build_object('refund_returned',
                   coalesce(business.order_refund_summary(v.order_id) ->> 'state' = 'returned', false)))::json
           ELSE pg_catalog.json_build_object('received_at', v.received_at) END);
  ELSIF v.command IN ('pickup_move', 'appointment_change') AND v_status = 'moved' AND v_state = 'secured' THEN
    -- The moved appointment is visible from now on: its notice, once per
    -- command (order_notice dedupe), rendered from the history row.
    PERFORM business.order_notice_enqueue(v.order_id,
      CASE v.command WHEN 'pickup_move' THEN 'pickup_moved' ELSE 'appointment_changed' END, v.id::text, v.id,
      (SELECT pg_catalog.json_build_object('from_starts_at', c.from_starts_at, 'from_ends_at', c.from_ends_at,
                                           'to_starts_at', c.to_starts_at, 'to_ends_at', c.to_ends_at,
                                           'received_at', c.received_at)
       FROM business.pickup_change c WHERE c.command_id = v.id));
  END IF;
  RETURN v_state;
END;
$$;

-- As in 0013, with the current appointment start.
CREATE OR REPLACE FUNCTION business.order_recovery_snapshot(p_order uuid)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.json_build_object(
    'id', o.id, 'quote_id', o.quote_id, 'version', o.version, 'owner_account_id', o.owner_account_id,
    'customer_full_name', o.customer_full_name, 'different_payer_name', o.different_payer_name,
    'contact_email', o.contact_email, 'lifecycle', o.lifecycle, 'payment_requirement', o.payment_requirement,
    'fulfillment_mode', o.fulfillment_mode, 'handover_day', o.handover_day, 'slot_id', o.slot_id,
    'original_handover_at', o.original_handover_at, 'handover_starts_at', o.handover_starts_at,
    'handover_ends_at', o.handover_ends_at,
    'full_refund_deadline_at', o.full_refund_deadline_at, 'policy_version_id', o.policy_version_id,
    'subtotal_minor', o.subtotal_minor, 'discount_minor', o.discount_minor, 'total_minor', o.total_minor,
    'currency', o.currency, 'placed_at', o.placed_at,
    'items', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                'id', i.id, 'line_no', i.line_no, 'variant_id', i.variant_id, 'recipe_version_id', i.recipe_version_id,
                'lot_id', i.physical_lot_id, 'quantity', i.quantity, 'unit_price_minor', i.unit_price_minor,
                'discount_minor', i.discount_minor, 'net_paid_minor', i.net_paid_minor) ORDER BY i.line_no)
              FROM business.order_item i WHERE i.order_id = o.id),
    'allocations', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                      'id', a.id, 'day', a.day, 'cake_quantity', a.cake_quantity,
                      'classification', a.classification, 'state', a.state) ORDER BY a.day)
                    FROM business.capacity_allocation a WHERE a.order_ref = o.id),
    'lot_reservations', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                           'lot_id', r.lot_id, 'quantity', r.quantity, 'state', r.state) ORDER BY r.lot_id)
                         FROM business.order_lot_reservation r WHERE r.order_id = o.id),
    'code_application', (SELECT pg_catalog.json_build_object(
                           'id', c.id, 'code_id', c.code_id, 'code_version_id', c.code_version_id,
                           'account_id', c.account_id, 'state', c.state, 'discount_minor', c.discount_minor)
                         FROM business.code_application c WHERE c.order_id = o.id),
    'guest_capabilities', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                             'id', g.id, 'digest', pg_catalog.encode(g.capability_digest, 'hex'),
                             'allowed_commands', g.allowed_commands, 'revoked_at', g.revoked_at) ORDER BY g.created_at, g.id)
                           FROM business.order_guest_capability g WHERE g.order_id = o.id))
  FROM business.orders o WHERE o.id = p_order;
$$;

-- As in 0022, plus an unresolved appointment change: production start
-- (manual and timer) waits for it too, so a move commits against the version
-- it was accepted at (K11: other conflicting commands wait while a recovery
-- phase is incomplete).
CREATE OR REPLACE FUNCTION business.order_terminal_pending(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                 WHERE c.order_id = p_order AND c.command IN ('cancel', 'late_payment', 'pickup_move', 'appointment_change')
                   AND r.state <> 'secured');
$$;

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

  IF v.command IN ('cancel', 'late_payment', 'manual_payout', 'manual_payout_outcome', 'pickup_move', 'appointment_change') THEN
    v_outcome := CASE v.command WHEN 'cancel' THEN business.order_cancel_commit(v)
                                WHEN 'pickup_move' THEN business.order_appointment_commit(v)
                                WHEN 'appointment_change' THEN business.order_appointment_commit(v)
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

REVOKE ALL ON TABLE business.pickup_change FROM PUBLIC;
REVOKE ALL ON FUNCTION business.pickup_move_cutoff() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_command_unresolved(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_slots_on(date) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_lots_last_until(uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_handover_at(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_visible_appointment(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.appointment_change_eligible(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_appointment_record(business.orders, text, text, uuid, uuid, text, text, timestamptz, text, date,
  uuid, timestamptz, timestamptz, integer, text, timestamptz, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_appointment_replay(uuid, text, uuid, uuid, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.appointment_check_common(text, text, integer, uuid, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_pickup_move_request(uuid, bytea, uuid, text, text, integer, uuid, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_appointment_change_request(uuid, uuid, text, text, integer, date, uuid, text, timestamptz, text,
  text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_move_slots(uuid, bytea, uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_appointment_commit(business.order_command) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_appointments(uuid, boolean) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_pickup_move_request(uuid, bytea, uuid, text, text, integer, uuid, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_appointment_change_request(uuid, uuid, text, text, integer, date, uuid, text, timestamptz, text,
  text, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.schedule_move_slots(uuid, bytea, uuid, timestamptz) TO gk_api;
