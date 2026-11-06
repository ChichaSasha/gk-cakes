-- GK-017 r02b Q5: the customer's choice when GK can't deliver (design
-- docs/design/sessions/checkout-recovery/gk-017-r02 state 14 "We can't
-- deliver after payment" and state 15 "pay-by time passed ... the same choice
-- with no deadline"; Creator decisions 2026-11-05: Q5 "when GK can't deliver
-- after payment, or the delivery quote isn't paid, the customer chooses
-- collection, own courier or a full refund of everything paid", Q8 "No time
-- limit on the Q5 choice; the case stays open with the chef", Q9 "Unpaid
-- delivery quote: deadline 48 h before handover, then the same choice", Q10
-- "any opening time that day"; contracts r6.5 K03 separate delivery, K05
-- "chef cancellation 100%", "GK-caused unperformed delivery raises its target
-- to 100% delivery ... explicit collection floor", cumulative goal; K06 "No
-- implicit date/delivery-mode change", courier responsibility at handover;
-- K08 audit without personal data; K09 expand-only; K11 accepted order
-- commands).
--
-- Chef: GK can't deliver (business.order_delivery_not_possible, chef). On a
-- gk_delivery order whose cake is visibly confirmed (confirmed, in_production
-- or ready; secured; paid or free-confirmed), never dispatched, with no
-- unresolved K11 command and no delivery collection in flight (409
-- payment_in_progress): records one immutable decision (business.
-- delivery_not_possible: the current quote, an optional one-line reason that
-- only the chef sees, never audited or shown to the customer, the order
-- version, the chef and the receipt instant). Versioned (order version + 1),
-- idempotent (K00 command receipt: the same key replays, also during a
-- restore freeze; another key on a decided order is 409 state_conflict),
-- refused during a restore freeze (GK503), audited delivery.not_possible
-- {decision_id, quote_id}. It refunds nothing by itself: when the delivery
-- was paid it records the delivery's 100% floor (business.refund_floor,
-- reason gk_delivery_failure: an obligation, nothing is reserved until a goal
-- exists), and the customer's choice decides. Any later goal (a time choice,
-- the full refund, an ordinary cancellation: the preview shows it) returns
-- the paid delivery in full beside the cake's own rate (K05 "GK-caused
-- unperformed delivery raises its target to 100% delivery"; state 14). From then on the delivery is never
-- dispatched (409 delivery_not_possible), and no quote, waiver or delivery
-- payment is accepted (business.delivery_order_open): an unpaid quote is
-- closed.
--
-- The choice is open (business.delivery_choice_basis) on a gk_delivery order,
-- visibly confirmed, never dispatched, when the chef recorded that GK can't
-- deliver (reason not_possible), or without any chef action when the current
-- quote is unpaid, not waived, has no payment in flight and its pay-by
-- (original handover - 48 h, 0035) has passed (reason pay_by_passed). No
-- deadline (Q8).
--
-- Customer choice (business.order_delivery_choice_request, API). The owner,
-- or a guest capability with "cancel" for full_refund and "move_pickup" for
-- the time choices (the cancellation's and the same-day move's access). A K11
-- order command 'delivery_choice' (prepared envelope before the commit,
-- outcome envelope before it is visible, exactly one outcome, the same actor
-- and key replay, another body is key_reused, a restore freeze refuses new
-- requests and defers the commit). Only while the choice is open (409
-- choice_not_open), at the version the customer saw (409 stale_version);
-- another protected change in progress is 409 state_conflict. The choice is
-- stored immutably with the command (business.delivery_choice: choice,
-- basis, quote, the paid delivery collection, the appointment it replaces and
-- the target, the access used as account or guest capability, the receipt
-- instant); it is also the mode change's history.
--   collection / customer_courier: target_slot_id must be a published start
--   of the current schedule on the order's own handover day (Q10: every
--   opening time that day, not limited by the six-hour self-move cutoff),
--   after the receipt instant and within the ready cakes' shelf life (409
--   slot_unavailable). The commit changes the fulfilment mode and the
--   appointment; the day place stays (no capacity admission); the original
--   handover, refund deadline and production anchor never move. A paid
--   delivery (the current quote's matched, succeeded collection) has its
--   100% floor (recorded with the chef's decision) and gets the goal and
--   reservation of 0022's delivery-failure floor: it comes back in full. An
--   unpaid quote is closed: nothing is refunded.
--   full_refund: the existing terminal return (business.order_terminal_return)
--   with cancel_reason delivery_not_possible and 100% of everything actually
--   paid (cake and matched delivery; a stray delivery payment in full on top,
--   as 0035 defines), source customer. A paid delivery's floor makes its
--   share reserved automatically (the chef already decided).
--   The cancelled notice (order_command_secured) states the reason.
-- The commit re-checks the version, the open choice at the receipt, the paid
-- delivery and the target slot (published, starting after the receipt and
-- after the commit's own instant: a commit delayed by a restore freeze never
-- sets a time that has started); otherwise it records a refusal
-- (order_changed, not_open, slot_unavailable; version + 1 only) and the
-- choice is open again.
--
-- Read models. order_view adds delivery_choice (gk_delivery orders, and
-- orders a choice changed): {open, reason not_possible | pay_by_passed | null,
-- opens_at (the unpaid quote's pay-by while no chef decision exists; the API
-- applies its clock to open and reason, as 0035's pay_by_passed), pending (a
-- protected change is unresolved), choices, times [{id, starts_at, ends_at}]
-- (the day's published starts within shelf life; the API keeps those after
-- its clock, and empties choices and times unless open), chosen {choice, reason, state processing | chosen | cancelled |
-- refused, refusal_reason, requested_at, to, delivery_refund_minor} | null}.
-- While a choice is not secured, chosen carries no delivery_refund_minor and
-- the order shows no refund block and sends no refund email
-- (order_refund_hidden; the secured command's trigger rechecks); while a
-- time choice is committed but unsecured, the mode and appointment read as
-- before (order_visible_mode, order_visible_appointment). After a
-- secured time choice the delivery block reads cancelled_by_choice. Staff:
-- the chef also gets the not-possible decision with its reason and the
-- history of choices; technical support the same without money or reason;
-- fulfilment the state. Attention flag delivery_choice_open.
--
-- Expand-only (K09): new tables, replaced CHECK constraints, new functions;
-- replaced functions keep their signatures, results, security and grants.
-- Lock order (K00): the order, its collections by ID, the recovery
-- operation, the goal and refund operations, history/audit.

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------- schema

ALTER TABLE business.order_command DROP CONSTRAINT order_command_command_check;
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_command_check CHECK (command IN ('free_confirm', 'paid_confirm', 'cancel', 'late_payment',
                                                                'manual_payout', 'manual_payout_outcome', 'pickup_move',
                                                                'appointment_change', 'handover', 'no_show',
                                                                'delivery_choice'));
ALTER TABLE business.order_command
  ADD CONSTRAINT order_command_delivery_choice CHECK (command <> 'delivery_choice' OR actor_kind IN ('customer', 'guest'));

ALTER TABLE business.orders DROP CONSTRAINT orders_cancel_reason_check;
ALTER TABLE business.orders
  ADD CONSTRAINT orders_cancel_reason_check CHECK (cancel_reason IN (
    'payment_not_completed', 'free_not_confirmed', 'customer_cancelled', 'chef_cancelled', 'late_payment_returned',
    'delivery_not_possible'));

ALTER TABLE business.refund_goal DROP CONSTRAINT refund_goal_reason_check;
ALTER TABLE business.refund_goal
  ADD CONSTRAINT refund_goal_reason_check CHECK (reason IN (
    'late_capture_terminal', 'customer_cancelled', 'chef_cancelled', 'late_payment_returned', 'delivery_failure',
    'chef_exception', 'no_show', 'delivery_not_possible'));

ALTER TABLE business.delivery_payment_outcome DROP CONSTRAINT delivery_payment_outcome_reason_check;
ALTER TABLE business.delivery_payment_outcome
  ADD CONSTRAINT delivery_payment_outcome_reason_check CHECK (reason IN ('no_quote', 'quote_replaced', 'quote_waived',
                                                                         'already_paid', 'order_not_open',
                                                                         'delivery_not_possible'));

-- The chef's decision that GK can't deliver an order (one per order).
CREATE TABLE business.delivery_not_possible (
  id            uuid PRIMARY KEY,
  order_id      uuid NOT NULL UNIQUE REFERENCES business.orders (id),
  -- The order's current quote at the decision (NULL before any quote).
  quote_id      uuid NULL REFERENCES business.delivery_quote (id),
  -- Staff only (the chef's view): never audited, never shown to the customer.
  reason        text NULL CHECK (reason = btrim(reason) AND char_length(reason) BETWEEN 1 AND 500
                                 AND reason !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'),
  -- The order version the decision was made at (it bumps it).
  order_version integer NOT NULL CHECK (order_version >= 1),
  recorded_by   uuid NOT NULL REFERENCES business.account (id),
  -- The persisted receipt instant.
  recorded_at   timestamptz NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER delivery_not_possible_immutable BEFORE UPDATE OR DELETE ON business.delivery_not_possible
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER delivery_not_possible_no_truncate BEFORE TRUNCATE ON business.delivery_not_possible
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- The customer's Q5 choice, one row per accepted request (its K11 command);
-- the outcome is the command's recovery operation. Also the history of the
-- explicit, agreed fulfilment mode change (K06).
CREATE TABLE business.delivery_choice (
  command_id             uuid PRIMARY KEY REFERENCES business.order_command (id),
  order_id               uuid NOT NULL REFERENCES business.orders (id),
  choice                 text NOT NULL CHECK (choice IN ('collection', 'customer_courier', 'full_refund')),
  -- Why the choice was open at the receipt.
  basis                  text NOT NULL CHECK (basis IN ('not_possible', 'pay_by_passed')),
  not_possible_id        uuid NULL REFERENCES business.delivery_not_possible (id),
  -- The current quote at the receipt and, when it was paid, its collection.
  quote_id               uuid NULL REFERENCES business.delivery_quote (id),
  delivery_collection_id uuid NULL REFERENCES business.payment_collection (id),
  -- The order's mode and appointment at the receipt (what a time choice replaces).
  from_mode              text NOT NULL CHECK (from_mode = 'gk_delivery'),
  from_day               date NOT NULL,
  from_slot_id           uuid NOT NULL,
  from_starts_at         timestamptz NOT NULL,
  from_ends_at           timestamptz NOT NULL,
  -- A time choice's target on the same day, and the schedule revision it
  -- was published in at the receipt.
  to_slot_id             uuid NULL,
  to_starts_at           timestamptz NULL,
  to_ends_at             timestamptz NULL,
  schedule_revision      integer NULL CHECK (schedule_revision >= 1),
  actor_kind             text NOT NULL CHECK (actor_kind IN ('customer', 'guest')),
  actor_account_id       uuid NULL REFERENCES business.account (id),
  guest_capability_id    uuid NULL REFERENCES business.order_guest_capability (id),
  idempotency_key        text NOT NULL CHECK (idempotency_key ~ '^[\x21-\x7e]{1,128}$'),
  body_sha256            char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  order_version          integer NOT NULL CHECK (order_version >= 1),
  -- The persisted receipt instant (K00 time rules).
  received_at            timestamptz NOT NULL,
  created_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT delivery_choice_actor CHECK ((actor_kind = 'customer') = (actor_account_id IS NOT NULL)
                                          AND (actor_kind = 'guest') = (guest_capability_id IS NOT NULL)),
  CONSTRAINT delivery_choice_basis CHECK ((basis = 'not_possible') = (not_possible_id IS NOT NULL)
                                          AND (basis <> 'pay_by_passed' OR (quote_id IS NOT NULL
                                                                            AND delivery_collection_id IS NULL))),
  CONSTRAINT delivery_choice_target CHECK (
    (choice = 'full_refund' AND to_slot_id IS NULL AND to_starts_at IS NULL AND to_ends_at IS NULL
     AND schedule_revision IS NULL)
    OR (choice <> 'full_refund' AND to_slot_id IS NOT NULL AND to_starts_at IS NOT NULL
        AND to_ends_at = to_starts_at + interval '60 minutes' AND schedule_revision IS NOT NULL))
);
CREATE INDEX delivery_choice_order_idx ON business.delivery_choice (order_id, created_at);
CREATE UNIQUE INDEX delivery_choice_key_idx
  ON business.delivery_choice (order_id, idempotency_key, actor_account_id, guest_capability_id) NULLS NOT DISTINCT;
CREATE TRIGGER delivery_choice_immutable BEFORE UPDATE OR DELETE ON business.delivery_choice
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER delivery_choice_no_truncate BEFORE TRUNCATE ON business.delivery_choice
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- ------------------------------------------------------------ helpers

-- Owner-internal: whether order p_o could offer the Q5 choice at all, apart
-- from the clock and from unresolved commands: GK delivery, cake visibly
-- confirmed (secured; paid or free-confirmed), never dispatched.
CREATE FUNCTION business.delivery_choice_eligible(p_o business.orders)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_o.fulfillment_mode = 'gk_delivery'
     AND p_o.lifecycle IN ('confirmed', 'in_production', 'ready') AND p_o.recovery_state = 'secured'
     AND (p_o.paid_at IS NOT NULL OR p_o.free_confirmed_at IS NOT NULL)
     AND NOT EXISTS (SELECT 1 FROM business.fulfillment_event e WHERE e.order_id = p_o.id AND e.action = 'dispatch');
$$;

-- Owner-internal: the instant from which an unpaid delivery quote opens the
-- choice (its pay-by), while no chef decision exists: the current quote is
-- not waived, not paid and has no payment in flight; NULL otherwise.
CREATE FUNCTION business.delivery_choice_opens_at(p_o business.orders)
RETURNS timestamptz
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT q.pay_by FROM business.delivery_quote q
  WHERE q.id = business.delivery_current_quote(p_o.id)
    AND business.delivery_choice_eligible(p_o)
    AND NOT EXISTS (SELECT 1 FROM business.delivery_not_possible n WHERE n.order_id = p_o.id)
    AND NOT EXISTS (SELECT 1 FROM business.delivery_waiver w WHERE w.quote_id = q.id)
    AND business.delivery_paid_collection(q.id) IS NULL
    AND NOT business.delivery_in_flight(p_o.id);
$$;

-- Owner-internal: why the Q5 choice is open on order p_o at p_at
-- (not_possible: the chef's decision; pay_by_passed: the current quote unpaid
-- at or after its pay-by), or NULL. Unresolved commands are the caller's.
CREATE FUNCTION business.delivery_choice_basis(p_o business.orders, p_at timestamptz)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN NOT business.delivery_choice_eligible(p_o) THEN NULL
              WHEN EXISTS (SELECT 1 FROM business.delivery_not_possible n WHERE n.order_id = p_o.id) THEN 'not_possible'
              WHEN business.delivery_choice_opens_at(p_o) <= p_at THEN 'pay_by_passed' END;
$$;

-- Owner-internal: the opening times a time choice may take on order p_o's
-- handover day (Q10: every published start of that day of the current
-- schedule, not a closed date, within the ready cakes' shelf life; no
-- six-hour cutoff, lead time or capacity). Only starts after the receipt
-- are accepted: the API drops the others at its clock, the request checks
-- its receipt instant.
CREATE FUNCTION business.delivery_choice_times(p_o business.orders)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object('id', s.slot_id, 'starts_at', s.starts_at,
                                                                     'ends_at', s.ends_at) ORDER BY s.starts_at),
                  '[]'::jsonb)
  FROM business.schedule_slots_on(p_o.handover_day) s
  WHERE business.order_lots_last_until(p_o.id, s.ends_at);
$$;

-- Owner-internal: the fulfilment mode customers and staff see (K11): while a
-- Q5 time choice is committed but its outcome is not secured, the GK
-- delivery it replaces.
CREATE FUNCTION business.order_visible_mode(p_order business.orders)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce((SELECT dc.from_mode FROM business.delivery_choice dc
                   JOIN business.recovery_operation r ON r.id = dc.command_id
                   WHERE dc.order_id = p_order.id AND r.state = 'committed' AND r.outcome ->> 'status' = 'chosen'
                   ORDER BY dc.created_at DESC, dc.command_id DESC LIMIT 1),
                  p_order.fulfillment_mode);
$$;

-- Owner-internal: whether order p_order's refund must stay invisible (K11:
-- not shown, no email): a handover or no-show (0032) or a Q5 choice is not
-- secured yet. The secured command's trigger rechecks the refund email.
CREATE FUNCTION business.order_refund_hidden(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT business.order_handover_pending(p_order)
      OR EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                 WHERE c.order_id = p_order AND c.command = 'delivery_choice' AND r.state <> 'secured');
$$;

-- Owner-internal: what recorded collection floors (business.refund_floor: a
-- GK-caused unperformed delivery, 0022 and 0036) add to the policy target
-- p_policy of order p_order: each floor beyond its collection's proportional
-- policy share. A cancellation returns the policy target plus this: the
-- delivery in full beside the rate on the cake (K05).
CREATE FUNCTION business.cancellation_floor_topup(p_order uuid, p_policy bigint)
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(sum(GREATEST(0, f.floor_minor - s.share_minor)), 0)::bigint
  FROM business.refund_floor f
  JOIN business.refund_shares(p_order, p_policy, false) s ON s.collection_id = f.collection_id
  WHERE f.order_id = p_order;
$$;

-- Owner-internal: once the chef recorded that GK can't deliver order
-- p_order, the 100% floor of its paid delivery (the current quote's matched
-- succeeded collection; Q5 "returned in full whatever they choose", K05),
-- recorded once by the chef who decided. A floor is an obligation, not a
-- refund: nothing is reserved until a goal exists (the customer's choice or
-- a cancellation). The caller holds the order and its collections. Returns
-- the paid delivery's amount, or NULL when there is no decision or no paid
-- delivery.
CREATE FUNCTION business.delivery_not_possible_floor(p_order uuid, p_actor_kind text, p_account uuid)
RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_np business.delivery_not_possible%ROWTYPE;
  v_quote uuid;
  v_coll uuid;
  v_amount bigint;
BEGIN
  SELECT * INTO v_np FROM business.delivery_not_possible n WHERE n.order_id = p_order;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  v_quote := business.delivery_current_quote(p_order);
  IF v_quote IS NOT NULL THEN
    v_coll := business.delivery_paid_collection(v_quote);
  END IF;
  IF v_coll IS NULL THEN
    RETURN NULL;
  END IF;
  SELECT pc.amount_minor INTO v_amount FROM business.payment_collection pc WHERE pc.id = v_coll;
  IF NOT EXISTS (SELECT 1 FROM business.refund_floor f WHERE f.collection_id = v_coll) THEN
    INSERT INTO business.refund_floor (id, order_id, collection_id, floor_minor, reason, recorded_by)
    VALUES (pg_catalog.gen_random_uuid(), p_order, v_coll, v_amount, 'gk_delivery_failure', v_np.recorded_by);
    PERFORM business.audit_record(p_actor_kind, p_account, 'refund.floor_recorded', 'order', p_order, NULL,
                                  pg_catalog.json_build_object('collection_id', v_coll, 'floor_minor', v_amount,
                                                               'decision_id', v_np.id));
  END IF;
  RETURN v_amount;
END;
$$;

-- Owner-internal: a choice as the order page shows it. state processing
-- (not secured), chosen (a time choice), cancelled (full refund) or refused
-- (refusal_reason). delivery_refund_minor: the paid delivery returned in
-- full (only when it was paid, and only once the choice is secured: K11).
CREATE FUNCTION business.delivery_choice_json(c business.delivery_choice)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'choice', c.choice, 'reason', c.basis,
           'state', CASE WHEN r.state <> 'secured' THEN 'processing' ELSE r.outcome ->> 'status' END,
           'refusal_reason', CASE WHEN r.state = 'secured' AND r.outcome ->> 'status' = 'refused'
                                  THEN r.outcome -> 'evidence' ->> 'reason' END,
           'requested_at', c.received_at,
           'to', CASE WHEN c.choice <> 'full_refund' THEN pg_catalog.jsonb_build_object(
                   'fulfillment_mode', c.choice, 'date', c.from_day, 'id', c.to_slot_id, 'starts_at', c.to_starts_at,
                   'ends_at', c.to_ends_at) END)
         -- The refund is visible only once the choice is secured (K11).
         || CASE WHEN c.delivery_collection_id IS NOT NULL AND r.state = 'secured'
                      AND r.outcome ->> 'status' IN ('chosen', 'cancelled') THEN pg_catalog.jsonb_build_object(
              'delivery_refund_minor', (SELECT pc.amount_minor FROM business.payment_collection pc
                                        WHERE pc.id = c.delivery_collection_id))
            ELSE '{}'::jsonb END
  FROM business.recovery_operation r WHERE r.id = c.command_id;
$$;

-- Owner-internal: the delivery_choice block of an order (see the header):
-- GK delivery orders and orders a choice changed; NULL otherwise. open and
-- reason use the database clock (the API applies its own through opens_at);
-- times are the day's starts, listed while the choice is or will become open
-- (the API empties them unless open at its clock and keeps only starts after
-- its clock).
CREATE FUNCTION business.order_delivery_choice_view(p_order business.orders)
RETURNS jsonb
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  c business.delivery_choice%ROWTYPE;
  v_reason text := business.delivery_choice_basis(p_order, pg_catalog.now());
  v_opens timestamptz := business.delivery_choice_opens_at(p_order);
  v_pending boolean := business.order_command_unresolved(p_order.id);
  v_open boolean;
BEGIN
  SELECT * INTO c FROM business.delivery_choice x WHERE x.order_id = p_order.id
  ORDER BY x.created_at DESC, x.command_id DESC LIMIT 1;
  IF p_order.fulfillment_mode <> 'gk_delivery' AND c.command_id IS NULL THEN
    RETURN NULL;
  END IF;
  v_open := v_reason IS NOT NULL AND NOT v_pending;
  RETURN pg_catalog.jsonb_build_object(
    'open', v_open, 'reason', v_reason, 'opens_at', v_opens, 'pending', v_pending,
    'choices', CASE WHEN v_open THEN '["collection", "customer_courier", "full_refund"]'::jsonb ELSE '[]'::jsonb END,
    'times', CASE WHEN v_reason IS NOT NULL OR v_opens IS NOT NULL
                  THEN business.delivery_choice_times(p_order) ELSE '[]'::jsonb END,
    'chosen', CASE WHEN c.command_id IS NOT NULL THEN business.delivery_choice_json(c) END);
END;
$$;

-- Owner-internal: the staff copy of a delivery_choice block p_block of order
-- p_order for view p_view. chef: the block, the not-possible decision with
-- its reason and every choice oldest first with its money; technical
-- support: the same without money or the reason (K00 no money; K09 necessary
-- support only); fulfilment: whether the choice is open and the latest
-- choice's state and target.
CREATE FUNCTION business.order_staff_delivery_choice(p_block jsonb, p_order uuid, p_view text)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE p_view
    WHEN 'fulfillment' THEN
      pg_catalog.jsonb_build_object(
        'open', p_block -> 'open', 'reason', p_block -> 'reason', 'opens_at', p_block -> 'opens_at',
        'pending', p_block -> 'pending',
        'chosen', CASE WHEN pg_catalog.jsonb_typeof(p_block -> 'chosen') = 'object'
                       THEN (p_block -> 'chosen') - 'delivery_refund_minor' ELSE p_block -> 'chosen' END)
    ELSE
      (CASE WHEN p_view = 'chef' THEN p_block
            ELSE p_block || pg_catalog.jsonb_build_object(
                   'chosen', CASE WHEN pg_catalog.jsonb_typeof(p_block -> 'chosen') = 'object'
                                  THEN (p_block -> 'chosen') - 'delivery_refund_minor' ELSE p_block -> 'chosen' END) END)
      || pg_catalog.jsonb_build_object(
           'not_possible', (SELECT pg_catalog.jsonb_build_object('id', n.id, 'at', n.recorded_at, 'quote_id', n.quote_id,
                                                                 'order_version', n.order_version)
                                   || CASE WHEN p_view = 'chef' THEN pg_catalog.jsonb_build_object('reason', n.reason)
                                           ELSE '{}'::jsonb END
                            FROM business.delivery_not_possible n WHERE n.order_id = p_order),
           'history', (SELECT coalesce(pg_catalog.jsonb_agg(
                                (CASE WHEN p_view = 'chef' THEN business.delivery_choice_json(c)
                                      ELSE business.delivery_choice_json(c) - 'delivery_refund_minor' END)
                                || pg_catalog.jsonb_build_object(
                                     'command_id', c.command_id,
                                     'access', CASE WHEN c.actor_kind = 'guest' THEN 'guest_link' ELSE 'account' END,
                                     'from', pg_catalog.jsonb_build_object('fulfillment_mode', c.from_mode, 'date', c.from_day,
                                                                           'id', c.from_slot_id, 'starts_at', c.from_starts_at,
                                                                           'ends_at', c.from_ends_at))
                                ORDER BY c.created_at, c.command_id), '[]'::jsonb)
                       FROM business.delivery_choice c WHERE c.order_id = p_order))
  END;
$$;

-- ---------------------------------------------------------------- API

-- API: the chef's decision that GK can't deliver order p_order (see the
-- header). Returns the decision and whether it already existed (the same
-- key, or a decision already recorded on the order).
CREATE FUNCTION business.order_delivery_not_possible(
  p_actor uuid, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer, p_reason text,
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
  v_id uuid := pg_catalog.gen_random_uuid();
  v_quote uuid;
BEGIN
  PERFORM business.delivery_check_input(p_key, p_body_sha256, p_expected_version, p_now);
  IF p_order IS NULL THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  IF p_reason IS NOT NULL AND (p_reason <> pg_catalog.btrim(p_reason) OR pg_catalog.char_length(p_reason) NOT BETWEEN 1 AND 500
                               OR p_reason ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') THEN
    RAISE EXCEPTION 'not_possible_reason_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- The same key replays (the decision), also during a restore freeze.
  v_prior := business.command_claim(v_account, 'order.delivery_not_possible', p_order::text, p_key, p_body_sha256);
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
  -- One decision per order: another key on a decided order is a conflict
  -- (K00: only the same key replays).
  IF EXISTS (SELECT 1 FROM business.delivery_not_possible n WHERE n.order_id = p_order) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF NOT business.delivery_choice_eligible(v_o) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  -- K04: a delivery payment that may still succeed is settled first.
  IF business.delivery_in_flight(p_order) THEN
    RAISE EXCEPTION 'payment_in_progress' USING ERRCODE = 'GK409';
  END IF;
  v_quote := business.delivery_current_quote(p_order);
  INSERT INTO business.delivery_not_possible (id, order_id, quote_id, reason, order_version, recorded_by, recorded_at)
  VALUES (v_id, p_order, v_quote, p_reason, v_o.version, v_account, p_now);
  -- A paid delivery's 100% floor (an obligation, not a refund: nothing is
  -- reserved until a goal exists, so the customer's choice still decides).
  PERFORM business.delivery_not_possible_floor(p_order, 'staff', v_account);
  UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = p_order;
  PERFORM business.audit_record('staff', v_account, 'delivery.not_possible', 'order', p_order, NULL,
                                pg_catalog.json_build_object('decision_id', v_id, 'quote_id', v_quote));
  PERFORM business.command_finish(v_account, 'order.delivery_not_possible', p_order::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- API: the customer's Q5 choice on order p_order (see the header): the
-- K11 command, its choice row, the job and the audit. Returns the command
-- (the recovery operation) and whether it already existed.
CREATE FUNCTION business.order_delivery_choice_request(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_choice text, p_target_slot uuid, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_l record;
  v_o business.orders%ROWTYPE;
  v_kind text;
  v_prior business.delivery_choice%ROWTYPE;
  v_frozen boolean;
  v_basis text;
  v_np uuid;
  v_quote uuid;
  v_paid uuid;
  v_to_slot uuid;
  v_to_starts timestamptz;
  v_to_ends timestamptz;
  v_revision integer;
  v_from_starts timestamptz;
  v_id uuid := pg_catalog.gen_random_uuid();
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
  IF p_choice IS NULL OR p_choice NOT IN ('collection', 'customer_courier', 'full_refund') THEN
    RAISE EXCEPTION 'choice_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF (p_choice = 'full_refund') <> (p_target_slot IS NULL) THEN
    RAISE EXCEPTION 'choice_target_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  -- The cancellation's access for a full refund, the same-day move's for a
  -- time: the owner, or a guest capability with "cancel" / "move_pickup".
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order,
                                                      CASE WHEN p_choice = 'full_refund' THEN 'cancel'
                                                           ELSE 'move_pickup' END) l;
  v_o := v_l.o_order;
  v_kind := CASE WHEN v_l.o_account IS NULL THEN 'guest' ELSE 'customer' END;
  -- The same actor and key replay the recorded choice, also while frozen.
  SELECT * INTO v_prior FROM business.delivery_choice c
  WHERE c.order_id = p_order AND c.idempotency_key = p_key
    AND c.actor_account_id IS NOT DISTINCT FROM v_l.o_account AND c.guest_capability_id IS NOT DISTINCT FROM v_l.o_capability;
  IF FOUND THEN
    IF v_prior.body_sha256 <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior.command_id, true;
    RETURN;
  END IF;
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  -- Lock order (K00): the order (held), its collections by ID.
  PERFORM 1 FROM business.payment_collection pc WHERE pc.order_id = p_order ORDER BY pc.id FOR UPDATE;
  PERFORM business.delivery_close_never_dispatched(p_order);
  IF business.order_command_unresolved(p_order) THEN
    -- Another protected change is in progress (K11): wait for it.
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  v_basis := business.delivery_choice_basis(v_o, p_now);
  IF v_basis IS NULL THEN
    RAISE EXCEPTION 'choice_not_open' USING ERRCODE = 'GK409';
  END IF;
  IF v_o.version <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  v_from_starts := business.order_handover_at(v_o);
  IF p_choice <> 'full_refund' THEN
    -- Q10: any opening time that day after the receipt (no six-hour cutoff).
    SELECT s.slot_id, s.starts_at, s.ends_at, s.schedule_revision INTO v_to_slot, v_to_starts, v_to_ends, v_revision
    FROM business.schedule_slots_on(v_o.handover_day) s WHERE s.slot_id = p_target_slot;
    IF v_to_slot IS NULL OR v_to_starts <= p_now OR NOT business.order_lots_last_until(p_order, v_to_ends) THEN
      RAISE EXCEPTION 'slot_unavailable' USING ERRCODE = 'GK409';
    END IF;
  END IF;
  SELECT n.id INTO v_np FROM business.delivery_not_possible n WHERE n.order_id = p_order;
  v_quote := business.delivery_current_quote(p_order);
  IF v_quote IS NOT NULL THEN
    v_paid := business.delivery_paid_collection(v_quote);
  END IF;

  v_key := 'recovery/' || p_env || '/op/' || p_order::text || '/' || pg_catalog.lpad(v_o.version::text, 10, '0')
           || '-' || v_id::text;
  v_payload := pg_catalog.json_build_object(
    'v', 1, 'environment', p_env, 'command', 'order.delivery_choice', 'operation_id', v_id, 'order_id', p_order,
    'prior_version', v_o.version,
    'received_at', pg_catalog.to_char(p_now AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'actor', pg_catalog.json_build_object('kind', v_kind, 'account_id', v_l.o_account),
    'delivery_choice', pg_catalog.json_build_object(
      'choice', p_choice, 'basis', v_basis, 'not_possible_id', v_np, 'quote_id', v_quote,
      'delivery_collection_id', v_paid,
      'from', pg_catalog.json_build_object('fulfillment_mode', v_o.fulfillment_mode, 'date', v_o.handover_day,
                                           'slot_id', v_o.slot_id, 'starts_at', v_from_starts, 'ends_at', v_o.handover_ends_at),
      'to', CASE WHEN p_choice <> 'full_refund' THEN pg_catalog.json_build_object(
              'fulfillment_mode', p_choice, 'date', v_o.handover_day, 'slot_id', v_to_slot, 'starts_at', v_to_starts,
              'ends_at', v_to_ends) END,
      'schedule_revision', v_revision),
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
  VALUES (v_id, p_order, 'delivery_choice', v_kind, v_l.o_account, v_l.o_capability, p_key, p_body_sha256, v_o.version,
          p_now, v_payload, v_hash, v_key, v_job);
  INSERT INTO business.delivery_choice (command_id, order_id, choice, basis, not_possible_id, quote_id,
                                        delivery_collection_id, from_mode, from_day, from_slot_id, from_starts_at,
                                        from_ends_at, to_slot_id, to_starts_at, to_ends_at, schedule_revision, actor_kind,
                                        actor_account_id, guest_capability_id, idempotency_key, body_sha256, order_version,
                                        received_at)
  VALUES (v_id, p_order, p_choice, v_basis, CASE WHEN v_basis = 'not_possible' THEN v_np END, v_quote, v_paid,
          v_o.fulfillment_mode, v_o.handover_day, v_o.slot_id, v_from_starts, v_o.handover_ends_at, v_to_slot,
          v_to_starts, v_to_ends, v_revision, v_kind, v_l.o_account, v_l.o_capability, p_key, p_body_sha256,
          v_o.version, p_now);
  PERFORM business.audit_record(CASE v_kind WHEN 'guest' THEN 'system' ELSE v_kind END, v_l.o_account,
                                'order.delivery_choice_requested', 'order', p_order, NULL,
                                pg_catalog.json_build_object('operation_id', v_id, 'choice', p_choice, 'basis', v_basis));
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- ------------------------------------------------------------- commit

-- Owner-internal: the domain commit of a prepared delivery_choice command
-- (the caller, business.order_command_apply, holds the order, its
-- collections and the recovery operation and has checked the freeze; see
-- the header). Returns the outcome text.
CREATE FUNCTION business.order_delivery_choice_commit(v business.order_command)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  c business.delivery_choice%ROWTYPE;
  v_paid uuid;
  v_amount bigint;
  v_reason text;
  v_actor text := CASE v.actor_kind WHEN 'guest' THEN 'system' ELSE v.actor_kind END;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  SELECT * INTO c FROM business.delivery_choice x WHERE x.command_id = v.id;
  IF c.quote_id IS NOT NULL THEN
    v_paid := business.delivery_paid_collection(c.quote_id);
  END IF;
  IF v_o.version <> v.prior_version OR v_o.recovery_state <> 'secured'
     OR business.delivery_current_quote(v.order_id) IS DISTINCT FROM c.quote_id
     OR v_paid IS DISTINCT FROM c.delivery_collection_id THEN
    v_reason := 'order_changed';
  ELSIF business.delivery_choice_basis(v_o, v.received_at) IS DISTINCT FROM c.basis THEN
    v_reason := 'not_open';
  ELSIF c.choice <> 'full_refund'
        AND (NOT EXISTS (SELECT 1 FROM business.schedule_slots_on(c.from_day) s
                         WHERE s.slot_id = c.to_slot_id AND s.starts_at = c.to_starts_at)
             -- A commit delayed (a restore freeze) never sets a time that has
             -- already started.
             OR c.to_starts_at <= v.received_at OR c.to_starts_at <= pg_catalog.now()
             OR NOT business.order_lots_last_until(v.order_id, c.to_ends_at)) THEN
    v_reason := 'slot_unavailable';
  END IF;
  IF v_reason IS NOT NULL THEN
    -- Nothing changes but the version: the choice is open again.
    UPDATE business.orders o SET version = o.version + 1, updated_at = pg_catalog.now() WHERE o.id = v.order_id;
    PERFORM business.audit_record('system', NULL, 'order.delivery_choice_refused', 'order', v.order_id, NULL,
                                  pg_catalog.json_build_object('operation_id', v.id, 'reason', v_reason));
    RETURN pg_catalog.format('{"status":"refused","evidence":{"reason":"%s"}}', v_reason);
  END IF;

  IF c.delivery_collection_id IS NOT NULL THEN
    -- A paid delivery comes back in full whatever the customer chooses
    -- (Q5): the GK-caused unperformed delivery's 100% floor (K05; 0022),
    -- recorded with the chef's decision (ensured here for safety).
    v_amount := business.delivery_not_possible_floor(v.order_id, v_actor, v.actor_account_id);
  END IF;

  IF c.choice = 'full_refund' THEN
    -- K05 "100% if we have to cancel": everything actually paid.
    RETURN business.order_terminal_return(v.order_id, v_o.version, 'delivery_not_possible', 100::smallint, 'customer',
                                          v.actor_kind, v.actor_account_id, v.id, v.received_at);
  END IF;

  -- The explicit, agreed mode and time change (K06): same day and place;
  -- the original handover, refund deadline and production anchor stay.
  UPDATE business.orders o
  SET fulfillment_mode = c.choice, slot_id = c.to_slot_id, handover_starts_at = c.to_starts_at,
      handover_ends_at = c.to_ends_at, version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = v.order_id;
  IF c.delivery_collection_id IS NOT NULL THEN
    PERFORM business.refund_goal_apply(v.order_id, NULL, NULL, NULL, NULL, NULL, v_actor, v.actor_account_id);
    PERFORM business.refund_reserve(v.order_id);
  END IF;
  PERFORM business.audit_record(v_actor, v.actor_account_id, 'order.delivery_choice_made', 'order', v.order_id, NULL,
                                pg_catalog.json_build_object('operation_id', v.id, 'choice', c.choice,
                                                             'delivery_refund_minor', v_amount));
  RETURN pg_catalog.json_build_object(
    'status', 'chosen',
    'evidence', pg_catalog.json_build_object('order_version', v_o.version + 1, 'choice', c.choice,
                                             'to_slot_id', c.to_slot_id, 'delivery_refund_minor', v_amount))::text;
END;
$$;

-- ------------------------------------------------- replaced functions

-- As in 0032, plus the delivery_choice command (business.order_delivery_choice_commit;
-- it locks the collections first: it may record a floor and reserve refunds).
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
  IF v.command IN ('paid_confirm', 'cancel', 'late_payment', 'manual_payout', 'manual_payout_outcome', 'no_show',
                   'delivery_choice') THEN
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

  IF v.command IN ('cancel', 'late_payment', 'manual_payout', 'manual_payout_outcome', 'pickup_move', 'appointment_change',
                   'handover', 'no_show', 'delivery_choice') THEN
    v_outcome := CASE v.command WHEN 'cancel' THEN business.order_cancel_commit(v)
                                WHEN 'delivery_choice' THEN business.order_delivery_choice_commit(v)
                                WHEN 'handover' THEN business.order_handover_commit(v)
                                WHEN 'no_show' THEN business.order_handover_commit(v)
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

-- As in 0032, plus a delivery_choice full refund: secured like a cancellation
-- (the cancelled notice, reason delivery_not_possible). A time choice sends
-- no notice (no new template; Creator review).
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
         OR (o.recovery_state = 'cancellation_pending' AND v.command IN ('cancel', 'late_payment', 'delivery_choice')
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
  ELSIF v.command = 'no_show' AND v_status = 'no_show' AND v_state = 'secured' THEN
    PERFORM business.order_notice_enqueue(v.order_id, 'no_show', v.id::text, v.id,
      (v_evidence::jsonb || pg_catalog.jsonb_build_object('refund_returned',
         coalesce(business.order_refund_summary(v.order_id) ->> 'state' = 'returned', false)))::json);
  END IF;
  RETURN v_state;
END;
$$;

-- As in 0032, plus an unresolved delivery_choice full refund reads
-- cancellation_pending.
CREATE OR REPLACE FUNCTION business.order_visible_lifecycle(p_order business.orders)
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE WHEN p_order.recovery_state = 'confirmation_pending'
                OR (p_order.lifecycle = 'awaiting_payment'
                    AND EXISTS (SELECT 1 FROM business.order_command pcmd
                                JOIN business.recovery_operation pr ON pr.id = pcmd.id
                                WHERE pcmd.order_id = p_order.id AND pcmd.command = 'paid_confirm'
                                  AND pr.state <> 'secured'))
              THEN 'confirmation_pending'
              WHEN p_order.recovery_state = 'cancellation_pending'
                OR EXISTS (SELECT 1 FROM business.order_command ccmd
                           JOIN business.recovery_operation cr ON cr.id = ccmd.id
                           LEFT JOIN business.late_payment_decision cd ON cd.command_id = ccmd.id
                           LEFT JOIN business.delivery_choice cdc ON cdc.command_id = ccmd.id
                           WHERE ccmd.order_id = p_order.id AND cr.state <> 'secured'
                             AND (ccmd.command = 'cancel' OR cd.decision = 'full_return' OR cdc.choice = 'full_refund'))
              THEN 'cancellation_pending'
              WHEN p_order.lifecycle = 'completed' AND business.order_handover_pending(p_order.id) THEN 'ready'
              ELSE p_order.lifecycle END;
$$;

-- As in 0031, plus an unresolved delivery choice (production and the chef's
-- refunds wait for it).
CREATE OR REPLACE FUNCTION business.order_terminal_pending(p_order uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM business.order_command c JOIN business.recovery_operation r ON r.id = c.id
                 WHERE c.order_id = p_order AND c.command IN ('cancel', 'late_payment', 'pickup_move', 'appointment_change', 'delivery_choice')
                   AND r.state <> 'secured');
$$;

-- As in 0035, and never once the chef recorded that GK can't deliver (Q5):
-- no new quote, waiver or payment (an unpaid quote is closed).
CREATE OR REPLACE FUNCTION business.delivery_order_open(p_o business.orders)
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
     AND NOT EXISTS (SELECT 1 FROM business.delivery_not_possible n WHERE n.order_id = p_o.id)
     AND NOT business.order_command_unresolved(p_o.id);
$$;

-- As in 0035, plus reason delivery_not_possible: money that arrives after the
-- chef recorded that GK can't deliver, or after the customer's Q5 choice
-- changed the order away from GK delivery, never pays the delivery; it is a
-- stray (returned with a goal, or flagged for the chef).
CREATE OR REPLACE FUNCTION business.delivery_payment_settle(p_o business.orders, p_c business.payment_collection)
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
    WHEN p_o.fulfillment_mode <> 'gk_delivery'
         OR EXISTS (SELECT 1 FROM business.delivery_not_possible n WHERE n.order_id = p_o.id) THEN 'delivery_not_possible'
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

-- As in 0035, plus: no dispatch once the chef recorded that GK can't deliver
-- (409 delivery_not_possible), whether or not the delivery was paid.
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
           WHEN EXISTS (SELECT 1 FROM business.delivery_not_possible n WHERE n.order_id = p_o.id) THEN 'delivery_not_possible'
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

-- As in 0034, and while a Q5 time choice is committed but its outcome is not
-- secured, the GK delivery appointment it replaces (K11).
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
    (SELECT pg_catalog.jsonb_build_object('date', dc.from_day, 'id', dc.from_slot_id, 'starts_at', dc.from_starts_at,
                                          'ends_at', dc.from_ends_at)
     FROM business.delivery_choice dc JOIN business.recovery_operation r ON r.id = dc.command_id
     WHERE dc.order_id = p_order.id AND r.state = 'committed' AND r.outcome ->> 'status' = 'chosen'
     ORDER BY dc.created_at DESC, dc.command_id DESC LIMIT 1),
    pg_catalog.jsonb_build_object('date', p_order.handover_day, 'id', p_order.slot_id,
                                  'starts_at', business.order_handover_at(p_order), 'ends_at', p_order.handover_ends_at));
$$;

-- As in 0035, plus delivery_choice (the Q5 choice, GK delivery orders and
-- orders changed by a choice; see the header), the visible fulfilment mode
-- (the GK delivery a committed but unsecured time choice replaces), a
-- delivery_choice full refund in the cancellation block, and no refund block
-- while a choice is not secured (business.order_refund_hidden, K11).
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
           'placed_at', o.placed_at, 'date', (va.a ->> 'date')::date, 'fulfillment_mode', business.order_visible_mode(o),
           'slot', va.a - 'date',
           'fulfillment', business.order_visible_fulfillment(o),
           'pickup_move_until', CASE WHEN business.order_visible_mode(o) IN ('collection', 'customer_courier')
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
                            LEFT JOIN business.delivery_choice dc ON dc.command_id = c.id
                            WHERE c.order_id = o.id AND (c.command = 'cancel' OR d.decision = 'full_return'
                                                         OR dc.choice = 'full_refund')
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
           'delivery_choice', business.order_delivery_choice_view(o),
           'refund', CASE WHEN o.recovery_state <> 'cancellation_pending' AND NOT business.order_refund_hidden(o.id)
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

-- As in 0035, with the visible fulfilment mode and no refund while a choice
-- is not secured (business.order_refund_hidden).
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
           'fulfillment_mode', business.order_visible_mode(p_order),
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
                                    AND NOT business.order_refund_hidden(p_order.id)
                               THEN business.order_refund_summary(p_order.id)::jsonb END)
            ELSE '{}'::jsonb END;
$$;

-- As in 0035, plus delivery_choice (business.order_staff_delivery_choice: the
-- chef also sees the not-possible reason and every choice with its money,
-- technical support the same without money or reason, fulfilment the state)
-- on orders whose customer view has the block, and the delivery block and
-- history also after a choice changed the order away from GK delivery.
-- Other orders keep exactly their 0035 keys.
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
                || CASE WHEN pg_catalog.jsonb_typeof(v_delivery) = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_history', business.order_staff_delivery(o.id, true))
                        ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_choice') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_choice',
                               business.order_staff_delivery_choice(v -> 'delivery_choice', o.id, 'chef'))
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
                   ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_choice') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_choice',
                               business.order_staff_delivery_choice(v -> 'delivery_choice', o.id, 'technical_support'))
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
                        ELSE '{}'::jsonb END
                || CASE WHEN pg_catalog.jsonb_typeof(v -> 'delivery_choice') = 'object'
                        THEN pg_catalog.jsonb_build_object('delivery_choice',
                               business.order_staff_delivery_choice(v -> 'delivery_choice', o.id, 'fulfillment'))
                        ELSE '{}'::jsonb END;
  END IF;
  PERFORM business.audit_record('staff', v_account, 'order.staff_viewed', 'order', o.id, NULL,
                                pg_catalog.json_build_object('view', v_view));
  RETURN v_result::json;
END;
$$;

-- As in 0035, plus the cancelled notice's first sentence for reason
-- delivery_not_possible (the customer's Q5 full refund; it follows what was
-- paid and whether it is returned already), and, when the outcome states a
-- delivery returned in full beside a 50% cake rate (delivery_full_minor),
-- that refund line (wording for Creator review). Every other notice renders
-- exactly as before.
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
      WHEN 'delivery_not_possible' THEN
        'We are sorry that we could not deliver your order. As you chose, it has been cancelled'
        || CASE WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN '.'
                WHEN coalesce((p_facts ->> 'refund_returned')::boolean, false) THEN ' and everything you paid has been returned.'
                ELSE ' and everything you paid is being returned.' END
      ELSE 'Your order has been cancelled as you asked.' END;
    v_returned := coalesce((p_facts ->> 'refund_returned')::boolean, false);
    v_text := v_text || CASE
      WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN 'There was no payment for this order, so there is nothing to refund.'
      WHEN v_target = 0 THEN 'No refund is due for this order.'
      ELSE CASE WHEN v_returned THEN 'Refund: ' ELSE 'Refund requested: ' END || business.notice_money(v_target)
           || CASE WHEN v_rate = 100 THEN ', the full amount you paid.'
                   -- After "GK can't deliver" (0036): the rate on the cakes and
                   -- the delivery payment in full.
                   WHEN coalesce((p_facts ->> 'delivery_full_minor')::bigint, 0) > 0 THEN
                     ': ' || v_rate::text || '% of the '
                     || business.notice_money((p_facts ->> 'paid_minor')::bigint - (p_facts ->> 'delivery_full_minor')::bigint)
                     || ' you paid for your cakes, because the full-refund deadline had been reached, and your delivery payment of '
                     || business.notice_money((p_facts ->> 'delivery_full_minor')::bigint)
                     || ' in full, because we could not deliver your order.'
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

-- As in 0035, plus delivery_choice_open: the customer's Q5 choice is open
-- (the chef recorded that GK can't deliver, or the delivery quote's pay-by
-- passed unpaid; database clock) and the case stays with the chef until the
-- customer decides (Q8).
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
                THEN 'delivery_payment_stuck' END,
           CASE WHEN business.delivery_choice_basis(p_order, pg_catalog.now()) IS NOT NULL THEN 'delivery_choice_open' END
         ]::text[], NULL);
$$;

-- As in 0035, and after a secured Q5 time choice (the order is no longer a GK
-- delivery) the block stays with state cancelled_by_choice: the delivery is
-- not happening; a paid delivery keeps paid_minor and its full refund is in
-- the refund block. While the choice is committed but unsecured the block
-- reads as before it (not payable).
CREATE OR REPLACE FUNCTION business.order_delivery_view(p_order business.orders)
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
  v_chosen text;
BEGIN
  SELECT r.state INTO v_chosen FROM business.delivery_choice dc JOIN business.recovery_operation r ON r.id = dc.command_id
  WHERE dc.order_id = p_order.id AND r.state IN ('committed', 'secured') AND r.outcome ->> 'status' = 'chosen'
  ORDER BY dc.created_at DESC, dc.command_id DESC LIMIT 1;
  IF p_order.fulfillment_mode <> 'gk_delivery' AND v_chosen IS NULL THEN
    RETURN NULL;
  END IF;
  SELECT * INTO q FROM business.delivery_quote x WHERE x.id = business.delivery_current_quote(p_order.id);
  IF q.id IS NULL THEN
    RETURN pg_catalog.jsonb_build_object('state', CASE WHEN v_chosen = 'secured' THEN 'cancelled_by_choice'
                                                       ELSE 'awaiting_quote' END,
                                         'quote', NULL, 'payable', false, 'payment', NULL)
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
  IF v_chosen = 'secured' THEN
    v_state := 'cancelled_by_choice';
  END IF;
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


-- As in 0021, with the amount a cancellation returns including recorded
-- delivery floors (business.cancellation_floor_topup), as the preview
-- computes it, so a preview made after "GK can't deliver" stays current.
CREATE OR REPLACE FUNCTION business.order_cancel_request(
  p_owner uuid, p_guest_digest bytea, p_order uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_preview_id uuid, p_preview_hash text, p_env text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_o business.orders%ROWTYPE;
  v_l record;
  v_account uuid;
  v_capability uuid;
  v_prior uuid;
  v_p business.cancellation_preview%ROWTYPE;
  v_rate smallint;
  v_paid bigint;
  v_amount bigint;
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
  IF p_preview_id IS NULL OR p_preview_hash IS NULL OR p_preview_hash !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'preview_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$' THEN
    RAISE EXCEPTION 'journal environment must be a lower-case path segment';
  END IF;
  SELECT * INTO v_l FROM business.order_customer_lock(p_owner, p_guest_digest, p_order, 'cancel') l;
  v_o := v_l.o_order;
  v_account := v_l.o_account;
  v_capability := v_l.o_capability;
  v_prior := business.order_cancel_admissible(v_o, v_account, v_capability, p_key, p_body_sha256, p_expected_version);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  -- The preview this actor saw, exactly.
  SELECT * INTO v_p FROM business.cancellation_preview p
  WHERE p.id = p_preview_id AND p.order_id = p_order
    AND p.actor_account_id IS NOT DISTINCT FROM v_account AND p.guest_capability_id IS NOT DISTINCT FROM v_capability;
  IF NOT FOUND OR v_p.body_sha256 <> p_preview_hash THEN
    RAISE EXCEPTION 'preview_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- At the persisted receipt instant (K05): a boundary crossed since the
  -- preview, an expired preview or another amount needs a new preview.
  v_rate := business.cancellation_rate(v_o.full_refund_deadline_at, p_now, false);
  v_paid := business.order_paid_minor(p_order);
  v_amount := business.cancellation_amount(v_paid, v_rate);
  v_amount := v_amount + CASE WHEN v_amount > 0 THEN business.cancellation_floor_topup(p_order, v_amount) ELSE 0 END;
  IF p_now >= v_p.expires_at OR v_p.order_version <> v_o.version OR v_p.rate <> v_rate
     OR v_p.amount_minor <> v_amount THEN
    RAISE EXCEPTION 'preview_stale' USING ERRCODE = 'GK409';
  END IF;
  RETURN QUERY SELECT business.order_cancel_record(p_order, v_o.version,
                        CASE WHEN v_account IS NULL THEN 'guest' ELSE 'customer' END, v_account, v_capability, p_key,
                        p_body_sha256, p_now, p_env, 'customer_cancelled', v_rate, v_paid, v_amount, NULL, p_preview_id),
                      false;
END;
$$;

-- As in 0035, except that amount_minor (in the hash) also counts recorded
-- delivery floors: after the chef recorded that GK can't deliver, a paid
-- delivery comes back in full beside the cake's rate (K05 "GK-caused
-- unperformed delivery raises its target to 100% delivery"; GK-017 r02b
-- state 14). The allocations already include the floor.
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
  -- What the cancellation returns: the policy amount plus any recorded
  -- delivery floor beyond its policy share (the goal is computed from the
  -- policy amount above, which the floor raises).
  v_amount := v_amount + CASE WHEN v_amount > 0 THEN business.cancellation_floor_topup(p_order, v_amount) ELSE 0 END;
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

-- As in 0035, plus: after the chef recorded that GK can't deliver, the paid
-- delivery's 100% floor is ensured first (business.delivery_not_possible_floor),
-- so the goal returns it in full beside the policy rate on the cake (K05;
-- GK-017 r02b state 14). refund_target_minor then states the whole amount
-- (policy + floor top-up) and delivery_full_minor the delivery returned in
-- full; outcomes without a floor top-up are exactly as before.
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
  v_paid bigint;
  v_policy bigint;
  v_topup bigint;
  v_target bigint;
  v_floors bigint;
  v_stray bigint;
BEGIN
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order FOR UPDATE;
  PERFORM business.delivery_not_possible_floor(p_order, CASE p_actor_kind WHEN 'guest' THEN 'system' ELSE p_actor_kind END,
                                               p_account);
  v_paid := business.order_paid_minor(p_order);
  v_policy := business.cancellation_amount(v_paid, p_rate);
  v_topup := CASE WHEN v_policy > 0 THEN business.cancellation_floor_topup(p_order, v_policy) ELSE 0 END;
  v_target := v_policy + v_topup;
  SELECT coalesce(sum(f.floor_minor), 0) INTO v_floors FROM business.refund_floor f WHERE f.order_id = p_order;
  -- A stray delivery payment (0035) not returned yet: the goal returns it in
  -- full beside the policy target, so the outcome states it.
  v_stray := CASE WHEN v_policy > 0 THEN business.delivery_unmatched_minor(p_order) ELSE 0 END;
  UPDATE business.orders o
  SET lifecycle = 'cancelled', cancel_reason = p_reason, cancelled_at = p_at,
      recovery_state = 'cancellation_pending', version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = p_order;
  IF v_policy > 0 THEN
    -- The goal from the policy target: recorded floors raise it.
    PERFORM business.refund_goal_apply(p_order, p_reason, p_source, p_rate, v_paid, v_policy, 'system', NULL);
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
  IF v_topup > 0 THEN
    RETURN (pg_catalog.json_build_object(
      'status', 'cancelled',
      'evidence', pg_catalog.jsonb_build_object('order_version', p_version + 1, 'reason', p_reason, 'rate', p_rate,
                                                'paid_minor', v_paid, 'refund_target_minor', v_target,
                                                'delivery_full_minor', v_floors)
                  || CASE WHEN v_stray > 0 THEN pg_catalog.jsonb_build_object('unmatched_minor', v_stray)
                          ELSE '{}'::jsonb END))::text;
  END IF;
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

-- As in 0032, and nothing while a Q5 choice is not secured either
-- (business.order_refund_hidden): the secured command's trigger rechecks.
CREATE OR REPLACE FUNCTION business.order_notice_refund_check(p_order uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v json;
BEGIN
  IF p_order IS NULL OR NOT EXISTS (SELECT 1 FROM business.orders o
                                    WHERE o.id = p_order AND o.recovery_state <> 'cancellation_pending')
     OR business.order_refund_hidden(p_order) THEN
    RETURN;
  END IF;
  v := business.order_refund_summary(p_order);
  IF v ->> 'state' = 'returned' AND (v ->> 'target_minor')::bigint > 0 THEN
    PERFORM business.order_notice_enqueue(p_order, 'refund_returned', v ->> 'goal_version',
              business.notice_uuid('refund_returned:' || p_order::text || ':' || (v ->> 'goal_version')),
              pg_catalog.json_build_object('target_minor', (v ->> 'target_minor')::bigint,
                                           'goal_version', (v ->> 'goal_version')::integer));
  END IF;
END;
$$;

REVOKE ALL ON TABLE business.delivery_not_possible FROM PUBLIC;
REVOKE ALL ON TABLE business.delivery_choice FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_choice_eligible(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_choice_opens_at(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_choice_basis(business.orders, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_choice_times(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_visible_mode(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_refund_hidden(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.cancellation_floor_topup(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_not_possible_floor(uuid, text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.delivery_choice_json(business.delivery_choice) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_choice_view(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_staff_delivery_choice(jsonb, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_not_possible(uuid, uuid, text, text, integer, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_choice_request(uuid, bytea, uuid, text, text, integer, text, uuid, text, timestamptz)
  FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_delivery_choice_commit(business.order_command) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.order_delivery_not_possible(uuid, uuid, text, text, integer, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_delivery_choice_request(uuid, bytea, uuid, text, text, integer, text, uuid, text, timestamptz)
  TO gk_api;
