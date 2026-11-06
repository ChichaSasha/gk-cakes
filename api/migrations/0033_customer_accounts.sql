-- GK-016 r02b: customer accounts (Creator-approved 2026-11-05; contracts r6.5
-- K00 "Claims require the authenticated customer branch and chef approval",
-- "Claim approval revokes outstanding guest capabilities and audits the
-- transition", "No automatic account association by email, payer, address or
-- Instagram", "Pending claim reveals no order details", "Store safe evidence
-- reference, approving chef and immutable association audit", "generic404 on
-- absent/expired/foreign proof"; K02 "GET /me/orders and detail are
-- owner-only snapshots", "Profile Instagram optional private plain text, not
-- identity proof"; K09 "Account/profile/favourites/basic history: 24 months
-- inactive since login/new order, warning then 30 days to return",
-- "Deletion revokes capabilities ... and carries tombstones", "Preserve
-- accounting facts through anonymization"; K10 no analytics on account
-- routes, which is a UI rule: nothing here records browsing).
--
-- Expand only: new nullable/defaulted columns, new tables and functions; the
-- deployed API keeps calling identity_resolve and order_get unchanged.
--
-- 1. Identity. identity_resolve_account replaces identity_resolve for this
--    API revision: it also says whether an account was deleted (the API
--    answers 403 ACCOUNT_DELETED) and records sign-in activity for the
--    inactivity rule (at most once an hour per account; any authenticated
--    request counts, a conservative reading of "since login": an account in
--    use is never warned). A deleted account is also disabled, so the
--    previous API revision refuses it too.
-- 2. GET /me/orders: account_order_page lists the account's orders (newest
--    placed first, keyset) as summaries built from business.order_view, the
--    renderer of order_get, so the list and the order page never disagree.
--    Each row says how the order came to the account: checkout, an approved
--    claim, or the chef.
-- 3. Profile: optional full name, phone and Instagram name on the account
--    with their own profile_version (K00 expected_version, command
--    receipts); audited with the changed field names only.
-- 4. Claims (K00): a signed-in customer asks to add an older order, by
--    reference + how they ordered + roughly when + a note (path c), or with
--    the order's guest capability held in this browser (path a; the emailed
--    capability link does not exist yet). Both wait for the chef. Approving
--    links the order to the account, revokes every outstanding guest
--    capability of the order and appends an immutable association event;
--    declining reveals nothing. The chef may also add an order to an account
--    directly (with an evidence note). Chef only (K00 "chef manages ...
--    claims").
-- 5. Deletion (K09): a preview binds the facts the customer saw (orders with
--    unsettled money block; confirmed upcoming orders warn; active staff
--    accounts are refused: the operator revokes their grants first);
--    deletion needs an email-link or Google sign-in within the last 10
--    minutes (the API passes that instant from the token), the unchanged
--    preview, no blocking order after the locks; it unlinks every order
--    (order details keep their own retention), revokes the orders' guest
--    capabilities, closes and erases claims, anonymises the profile, records
--    a tombstone and a pending provider deletion (the auth user is removed
--    by a later operator/worker step, never by the API), and audits counts
--    only. The same key reads a lost answer back before any other check.
--    account_owner_guard (triggers on orders, checkout_quote and
--    code_application) refuses attaching anything to a deleted account, so
--    a checkout racing a deletion (any API build) cannot link its order to
--    the deleted account.
-- 6. Inactivity (K09, BO-GK-005.retention_orders_accounts): account_retention_due
--    lists, for the worker, accounts due a warning (24 London months after
--    the last activity: sign-in or a new order), a reminder (7 days before
--    deletion) or deletion (30 days after the warning was recorded and 7
--    days after the reminder, never before either); notices are recorded
--    per activity anchor so a returning customer starts a new cycle.
--    Recording a notice is owner-only until the email step exists; the
--    emails and the deletion run are later steps.
--
-- Lock order (K00, extended): the actor's account, then the claimant/owner
-- account, then claim rows, then orders by UUID, then history/audit. Order
-- changes (claim approval, chef add, deletion) take the restore freeze first
-- (GK503), as other order commands. An account's own commands and its
-- deletion lock it FOR NO KEY UPDATE (rows that only reference it take KEY
-- SHARE and pass, so order commands never deadlock with a deletion);
-- attaching an order, quote or code use to it takes FOR SHARE and waits.

SET LOCAL ROLE gk_owner;

ALTER TABLE business.account
  ADD COLUMN profile_full_name  text NULL CHECK (profile_full_name IS NULL OR (profile_full_name = btrim(profile_full_name)
                                   AND char_length(profile_full_name) BETWEEN 1 AND 120
                                   AND profile_full_name !~ '[[:cntrl:]]')),
  ADD COLUMN profile_phone      text NULL CHECK (profile_phone IS NULL OR (profile_phone ~ '^\+?[0-9][0-9 ()-]{5,22}[0-9]$'
                                   AND char_length(pg_catalog.regexp_replace(profile_phone, '[^0-9]', '', 'g')) BETWEEN 7 AND 15)),
  ADD COLUMN profile_instagram  text NULL CHECK (profile_instagram IS NULL OR profile_instagram ~ '^[A-Za-z0-9._]{1,30}$'),
  ADD COLUMN profile_version    integer NOT NULL DEFAULT 1 CHECK (profile_version >= 1),
  ADD COLUMN profile_updated_at timestamptz NULL,
  -- Existing accounts are backfilled below; NULL (an account created by
  -- the previous API revision during the deploy) reads as created_at.
  ADD COLUMN last_active_at     timestamptz NULL,
  ADD COLUMN deleted_at         timestamptz NULL,
  ADD CONSTRAINT account_deleted_disabled CHECK (deleted_at IS NULL OR disabled_at IS NOT NULL),
  ADD CONSTRAINT account_deleted_anonymous CHECK (deleted_at IS NULL OR (profile_full_name IS NULL AND profile_phone IS NULL
                                                                        AND profile_instagram IS NULL));

-- Data only: an existing account's last sign-in is unknown, so its 24
-- inactive months start at this migration, never earlier (K09).
UPDATE business.account SET last_active_at = pg_catalog.now() WHERE last_active_at IS NULL;

-- A request to add an older order to the account (K00 claims).
CREATE TABLE business.order_claim (
  id                  uuid PRIMARY KEY,
  account_id          uuid NOT NULL REFERENCES business.account (id),
  -- evidence: reference + channel + roughly when + note (path c);
  -- email_link: the order's guest capability held by the customer (path a).
  path                text NOT NULL CHECK (path IN ('evidence', 'email_link')),
  channel             text NOT NULL CHECK (channel IN ('instagram', 'offline', 'website_email_link')),
  reference           text NULL CHECK (reference IS NULL OR (reference = btrim(reference)
                                         AND char_length(reference) BETWEEN 1 AND 64 AND reference !~ '[[:cntrl:]]')),
  approximate_date    text NULL CHECK (approximate_date IS NULL
                                       OR approximate_date ~ '^[0-9]{4}-(0[1-9]|1[0-2])(-(0[1-9]|[12][0-9]|3[01]))?$'),
  note                text NULL CHECK (note IS NULL OR (note = btrim(note) AND char_length(note) BETWEEN 1 AND 500
                                         AND note !~ '[\x01-\x09\x0b-\x1f\x7f]')),
  capability_order_id uuid NULL REFERENCES business.orders (id),
  capability_id       uuid NULL REFERENCES business.order_guest_capability (id),
  state               text NOT NULL DEFAULT 'pending'
                      CHECK (state IN ('pending', 'approved', 'declined', 'withdrawn', 'closed')),
  decided_order_id    uuid NULL REFERENCES business.orders (id),
  decided_by          uuid NULL REFERENCES business.account (id),
  decided_at          timestamptz NULL,
  -- Internal: never shown to the customer (K09 internal review notes).
  decision_note       text NULL CHECK (decision_note IS NULL OR (decision_note = btrim(decision_note)
                                         AND char_length(decision_note) BETWEEN 1 AND 500
                                         AND decision_note !~ '[[:cntrl:]]')),
  received_at         timestamptz NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  -- Set when the account is deleted: the customer's free text is erased.
  erased_at           timestamptz NULL,
  version             integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  CONSTRAINT order_claim_path CHECK (
    (path = 'email_link' AND channel = 'website_email_link' AND capability_order_id IS NOT NULL
       AND capability_id IS NOT NULL AND reference IS NULL AND approximate_date IS NULL)
    OR (path = 'evidence' AND capability_order_id IS NULL AND capability_id IS NULL
        AND (erased_at IS NOT NULL OR (reference IS NOT NULL AND note IS NOT NULL)))),
  CONSTRAINT order_claim_erased CHECK (erased_at IS NULL OR (reference IS NULL AND note IS NULL AND approximate_date IS NULL)),
  CONSTRAINT order_claim_decision CHECK ((state IN ('pending', 'closed')) = (decided_at IS NULL)
                                         AND (state = 'approved') = (decided_order_id IS NOT NULL)
                                         AND (state IN ('approved', 'declined')) = (decided_by IS NOT NULL)
                                         AND (decision_note IS NULL OR state IN ('approved', 'declined')))
);

CREATE INDEX order_claim_account_idx ON business.order_claim (account_id, created_at DESC);
-- The chef's queue pages by (received_at, id) within a state.
CREATE INDEX order_claim_queue_idx ON business.order_claim (state, received_at, id);
-- One pending email-link claim per account and order.
CREATE UNIQUE INDEX order_claim_link_pending_idx ON business.order_claim (account_id, capability_order_id)
  WHERE state = 'pending' AND path = 'email_link';

-- Immutable association history (K00): every link of an order to an
-- account and every unlink, with its source and actor. The chef's evidence
-- note is the only field that may change, and only to NULL (erased when the
-- account is deleted).
CREATE TABLE business.order_owner_event (
  id               uuid PRIMARY KEY,
  order_id         uuid NOT NULL REFERENCES business.orders (id),
  account_id       uuid NOT NULL REFERENCES business.account (id),
  kind             text NOT NULL CHECK (kind IN ('linked', 'unlinked')),
  source           text NOT NULL CHECK (source IN ('claim', 'chef', 'account_deleted')),
  claim_id         uuid NULL REFERENCES business.order_claim (id),
  evidence         text NULL CHECK (evidence IS NULL OR (evidence = btrim(evidence)
                                      AND char_length(evidence) BETWEEN 1 AND 500 AND evidence !~ '[[:cntrl:]]')),
  actor_account_id uuid NOT NULL REFERENCES business.account (id),
  occurred_at      timestamptz NOT NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT order_owner_event_source CHECK (
    (kind = 'linked' AND source = 'claim' AND claim_id IS NOT NULL AND evidence IS NULL)
    OR (kind = 'linked' AND source = 'chef' AND claim_id IS NULL)
    OR (kind = 'unlinked' AND source = 'account_deleted' AND claim_id IS NULL AND evidence IS NULL))
);

CREATE INDEX order_owner_event_order_idx ON business.order_owner_event (order_id, occurred_at DESC);
CREATE INDEX order_owner_event_account_idx ON business.order_owner_event (account_id);

-- A deletion preview: the facts the customer saw (order IDs and reasons
-- only, no personal data), bound by their hash for 15 minutes.
CREATE TABLE business.account_deletion_preview (
  id           uuid PRIMARY KEY,
  account_id   uuid NOT NULL REFERENCES business.account (id),
  facts        jsonb NOT NULL,
  facts_sha256 char(64) NOT NULL CHECK (facts_sha256 ~ '^[0-9a-f]{64}$'),
  created_at   timestamptz NOT NULL,
  expires_at   timestamptz NOT NULL,
  CONSTRAINT account_deletion_preview_ttl CHECK (expires_at = created_at + interval '15 minutes')
);

CREATE INDEX account_deletion_preview_account_idx ON business.account_deletion_preview (account_id);
CREATE INDEX account_deletion_preview_expires_idx ON business.account_deletion_preview (expires_at);

-- K09 tombstone: that an account was deleted, when and why, without
-- personal data; restore/recovery replay must carry it (README gap).
CREATE TABLE business.account_tombstone (
  account_id      uuid PRIMARY KEY REFERENCES business.account (id),
  deleted_at      timestamptz NOT NULL,
  reason          text NOT NULL CHECK (reason IN ('customer_request', 'inactivity')),
  orders_unlinked integer NOT NULL CHECK (orders_unlinked >= 0),
  created_at      timestamptz NOT NULL DEFAULT now()
);

-- The identity provider's user is deleted outside the API (operator or a
-- later worker step); until then the provider subject stays on the deleted
-- account so a still-valid token resolves to it and is refused.
CREATE TABLE business.account_provider_deletion (
  account_id   uuid PRIMARY KEY REFERENCES business.account (id),
  requested_at timestamptz NOT NULL,
  completed_at timestamptz NULL CHECK (completed_at IS NULL OR completed_at >= requested_at)
);

-- K09 inactivity notices, one per kind and activity anchor (the last
-- activity they were computed from): a returning customer has a new anchor.
CREATE TABLE business.account_retention_notice (
  id              uuid PRIMARY KEY,
  account_id      uuid NOT NULL REFERENCES business.account (id),
  kind            text NOT NULL CHECK (kind IN ('warning', 'reminder')),
  activity_anchor timestamptz NOT NULL,
  deletion_due_at timestamptz NOT NULL,
  recorded_at     timestamptz NOT NULL,
  UNIQUE (account_id, kind, activity_anchor)
);

CREATE FUNCTION business.order_owner_event_guard()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.id, NEW.order_id, NEW.account_id, NEW.kind, NEW.source, NEW.claim_id, NEW.actor_account_id,
                          NEW.occurred_at, NEW.created_at)
                         IS DISTINCT FROM (OLD.id, OLD.order_id, OLD.account_id, OLD.kind, OLD.source, OLD.claim_id,
                                           OLD.actor_account_id, OLD.occurred_at, OLD.created_at)
     OR (NEW.evidence IS NOT NULL AND NEW.evidence IS DISTINCT FROM OLD.evidence) THEN
    RAISE EXCEPTION 'association history is immutable' USING ERRCODE = 'GK409';
  END IF;
  RETURN NEW;
END;
$$;

-- Nothing is attached to a deleted account (K09). Every writer of an
-- account reference that means ownership or a customer's use (orders and
-- quotes owned by it, code uses counted for it) takes the account FOR
-- SHARE here, so it waits for a deletion in progress (FOR NO KEY UPDATE)
-- and then sees deleted_at: GK403 account_deleted. This protects every API
-- build, including one that reads the account without a lock (checkout).
CREATE FUNCTION business.account_owner_guard()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
  v_old uuid;
  v_deleted timestamptz;
BEGIN
  IF TG_TABLE_NAME = 'code_application' THEN
    v_account := NEW.account_id;
    IF TG_OP = 'UPDATE' THEN
      v_old := OLD.account_id;
    END IF;
  ELSE
    v_account := NEW.owner_account_id;
    IF TG_OP = 'UPDATE' THEN
      v_old := OLD.owner_account_id;
    END IF;
  END IF;
  IF v_account IS NULL OR v_account IS NOT DISTINCT FROM v_old THEN
    RETURN NEW;
  END IF;
  SELECT a.deleted_at INTO v_deleted FROM business.account a WHERE a.id = v_account FOR SHARE;
  IF v_deleted IS NOT NULL THEN
    RAISE EXCEPTION 'account_deleted' USING ERRCODE = 'GK403';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER orders_account_owner_guard BEFORE INSERT OR UPDATE OF owner_account_id ON business.orders
  FOR EACH ROW EXECUTE FUNCTION business.account_owner_guard();
CREATE TRIGGER checkout_quote_account_owner_guard BEFORE INSERT OR UPDATE OF owner_account_id ON business.checkout_quote
  FOR EACH ROW EXECUTE FUNCTION business.account_owner_guard();
CREATE TRIGGER code_application_account_owner_guard BEFORE INSERT OR UPDATE OF account_id ON business.code_application
  FOR EACH ROW EXECUTE FUNCTION business.account_owner_guard();

CREATE TRIGGER order_owner_event_guard BEFORE UPDATE OR DELETE ON business.order_owner_event
  FOR EACH ROW EXECUTE FUNCTION business.order_owner_event_guard();
CREATE TRIGGER order_owner_event_no_truncate BEFORE TRUNCATE ON business.order_owner_event
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER account_tombstone_immutable BEFORE UPDATE OR DELETE ON business.account_tombstone
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER account_tombstone_no_truncate BEFORE TRUNCATE ON business.account_tombstone
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER order_claim_no_delete BEFORE DELETE ON business.order_claim
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER order_claim_no_truncate BEFORE TRUNCATE ON business.order_claim
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER account_retention_notice_immutable BEFORE UPDATE OR DELETE ON business.account_retention_notice
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER account_retention_notice_no_truncate BEFORE TRUNCATE ON business.account_retention_notice
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- ------------------------------------------------------------------ identity

-- The API calls this for every authenticated request after verifying the
-- token: the subject's account (created on first use), its state (active,
-- disabled or deleted) and active staff capabilities in name order. It
-- records activity for the inactivity rule at most once an hour.
CREATE FUNCTION business.identity_resolve_account(p_subject uuid)
RETURNS TABLE (account_id uuid, account_state text, account_capabilities text[])
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid := business.account_ensure(p_subject, false);
BEGIN
  UPDATE business.account a SET last_active_at = pg_catalog.now()
  WHERE a.id = v_id AND a.deleted_at IS NULL AND a.disabled_at IS NULL
    AND (a.last_active_at IS NULL OR a.last_active_at < pg_catalog.now() - interval '1 hour');
  RETURN QUERY
  SELECT a.id,
         CASE WHEN a.deleted_at IS NOT NULL THEN 'deleted' WHEN a.disabled_at IS NOT NULL THEN 'disabled' ELSE 'active' END,
         coalesce((SELECT pg_catalog.array_agg(c.capability ORDER BY c.capability)
                   FROM business.staff_capability c
                   WHERE c.account_id = a.id AND c.revoked_at IS NULL), '{}'::text[])
  FROM business.account a
  WHERE a.id = v_id;
END;
$$;

-- Owner-internal: the active customer account of a verified subject (any
-- account may act as a customer; K00 needs no staff membership), without a
-- lock. A missing, disabled or deleted account is GK403.
CREATE FUNCTION business.account_customer_id(p_subject uuid)
RETURNS uuid
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid;
BEGIN
  SELECT a.id INTO v_id FROM business.account a
  WHERE a.provider_subject = p_subject AND a.disabled_at IS NULL AND a.deleted_at IS NULL;
  IF v_id IS NULL THEN
    RAISE EXCEPTION 'account_unavailable' USING ERRCODE = 'GK403';
  END IF;
  RETURN v_id;
END;
$$;

-- Owner-internal: the same, locked for the account's own commands. FOR NO
-- KEY UPDATE, not FOR UPDATE: rows that merely reference the account (order
-- commands, receipts, audit) take KEY SHARE, which this lock lets through,
-- so a command that locked an order first never deadlocks with it; writers
-- that attach something to the account take FOR SHARE (account_owner_guard),
-- which waits.
CREATE FUNCTION business.account_customer_lock(p_subject uuid)
RETURNS business.account
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  a business.account%ROWTYPE;
BEGIN
  SELECT * INTO a FROM business.account x WHERE x.provider_subject = p_subject FOR NO KEY UPDATE;
  IF NOT FOUND OR a.disabled_at IS NOT NULL OR a.deleted_at IS NOT NULL THEN
    RAISE EXCEPTION 'account_unavailable' USING ERRCODE = 'GK403';
  END IF;
  RETURN a;
END;
$$;

-- Owner-internal: a restore freeze refuses order changes (GK503).
CREATE FUNCTION business.account_order_freeze_check()
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_frozen boolean;
BEGIN
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
END;
$$;

-- --------------------------------------------------------------- my orders

-- Owner-internal: how the order came to its current owner: checkout, an
-- approved claim or the chef (the latest link event for that owner).
CREATE FUNCTION business.order_owner_source(p_order business.orders)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(
    (SELECT pg_catalog.jsonb_build_object('added_by', e.source, 'added_at', e.occurred_at)
     FROM business.order_owner_event e
     WHERE e.order_id = p_order.id AND e.kind = 'linked' AND e.account_id = p_order.owner_account_id
     ORDER BY e.occurred_at DESC, e.id DESC LIMIT 1),
    pg_catalog.jsonb_build_object('added_by', 'checkout', 'added_at', p_order.placed_at));
$$;

-- Owner-internal: the owner's summary row of an order, from order_view (the
-- order_get renderer): visible lifecycle and states, the date and slot,
-- the total, the payment, refund and change blocks without links or
-- policy detail, and the item names. No contact details.
CREATE FUNCTION business.order_customer_summary(p_order business.orders)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'id', v -> 'id', 'version', v -> 'version', 'lifecycle', v -> 'lifecycle',
           'cancel_reason', v -> 'cancel_reason', 'cancelled_at', v -> 'cancelled_at',
           'payment_requirement', v -> 'payment_requirement', 'placed_at', v -> 'placed_at',
           'date', v -> 'date', 'fulfillment_mode', v -> 'fulfillment_mode', 'slot', v -> 'slot',
           'fulfillment_state', v -> 'fulfillment' -> 'state',
           'total_minor', v -> 'total_minor', 'currency', v -> 'currency',
           'payment', CASE WHEN pg_catalog.jsonb_typeof(v -> 'payment') = 'object' THEN pg_catalog.jsonb_build_object(
                        'state', v -> 'payment' -> 'state', 'amount_minor', v -> 'payment' -> 'amount_minor',
                        'expires_at', v -> 'payment' -> 'expires_at',
                        'payment_page_open', pg_catalog.jsonb_typeof(v -> 'payment' -> 'checkout_url') = 'string')
                      ELSE 'null'::jsonb END,
           'free_confirmation', CASE WHEN pg_catalog.jsonb_typeof(v -> 'free_confirmation') = 'object'
                                     THEN (v -> 'free_confirmation') - 'requested_at' ELSE 'null'::jsonb END,
           'cancellation', CASE WHEN pg_catalog.jsonb_typeof(v -> 'cancellation') = 'object'
                                THEN pg_catalog.jsonb_build_object('state', v -> 'cancellation' -> 'state',
                                       'reason', v -> 'cancellation' -> 'reason',
                                       'amount_minor', v -> 'cancellation' -> 'amount_minor')
                                ELSE 'null'::jsonb END,
           'appointment_change', CASE WHEN pg_catalog.jsonb_typeof(v -> 'appointment_change') = 'object'
                                      THEN pg_catalog.jsonb_build_object('kind', v -> 'appointment_change' -> 'kind',
                                             'state', v -> 'appointment_change' -> 'state')
                                      ELSE 'null'::jsonb END,
           'refund', CASE WHEN pg_catalog.jsonb_typeof(v -> 'refund') = 'object'
                          THEN pg_catalog.jsonb_build_object('state', v -> 'refund' -> 'state',
                                 'target_minor', v -> 'refund' -> 'target_minor',
                                 'succeeded_minor', v -> 'refund' -> 'succeeded_minor',
                                 'in_progress_minor', v -> 'refund' -> 'in_progress_minor',
                                 'currency', v -> 'refund' -> 'currency')
                          ELSE 'null'::jsonb END,
           'items', coalesce((SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
                                'cake_name', i -> 'cake_name', 'flavour', i -> 'flavour', 'size_code', i -> 'size_code',
                                'quantity', i -> 'quantity') ORDER BY (i ->> 'line_no')::integer)
                              FROM pg_catalog.jsonb_array_elements(v -> 'items') i), '[]'::jsonb))
         || business.order_owner_source(p_order)
  FROM (SELECT business.order_view(p_order.id)::jsonb AS v) x;
$$;

-- GET /me/orders: the account's orders, newest placed first; p_after_* is
-- the last row of the previous page. Returns {items, next} where next is
-- the cursor position after this page, or null.
CREATE FUNCTION business.account_order_page(p_subject uuid, p_after_placed timestamptz, p_after_id uuid, p_limit integer)
RETURNS json
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.account_customer_id(p_subject);
  v_items jsonb;
  v_count integer;
  v_last business.orders%ROWTYPE;
BEGIN
  IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 50 THEN
    RAISE EXCEPTION 'page_limit_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF (p_after_placed IS NULL) <> (p_after_id IS NULL) THEN
    RAISE EXCEPTION 'cursor_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT coalesce(pg_catalog.jsonb_agg(business.order_customer_summary(o) ORDER BY o.placed_at DESC, o.id DESC), '[]'::jsonb),
         pg_catalog.count(*)
  INTO v_items, v_count
  FROM (SELECT * FROM business.orders x
        WHERE x.owner_account_id = v_account
          AND (p_after_placed IS NULL OR (x.placed_at, x.id) < (p_after_placed, p_after_id))
        ORDER BY x.placed_at DESC, x.id DESC
        LIMIT p_limit + 1) o;
  IF v_count <= p_limit THEN
    RETURN pg_catalog.json_build_object('items', v_items, 'next', NULL);
  END IF;
  v_items := v_items - p_limit;
  SELECT * INTO v_last FROM business.orders x WHERE x.id = (v_items -> (p_limit - 1) ->> 'id')::uuid;
  RETURN pg_catalog.json_build_object('items', v_items,
                                      'next', pg_catalog.json_build_object('placed_at', v_last.placed_at, 'id', v_last.id));
END;
$$;

-- ------------------------------------------------------------------ profile

CREATE FUNCTION business.account_profile_get(p_subject uuid)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.json_build_object('full_name', a.profile_full_name, 'phone', a.profile_phone,
                                      'instagram', a.profile_instagram, 'version', a.profile_version,
                                      'updated_at', a.profile_updated_at)
  FROM business.account a WHERE a.id = business.account_customer_id(p_subject);
$$;

-- PATCH /me/profile: each p_set_* names a field the request changes (a
-- NULL value clears it). Audited with the changed field names only.
CREATE FUNCTION business.account_profile_update(
  p_subject uuid, p_key text, p_body_sha256 text, p_expected_version integer,
  p_set_full_name boolean, p_full_name text, p_set_phone boolean, p_phone text,
  p_set_instagram boolean, p_instagram text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  a business.account%ROWTYPE := business.account_customer_lock(p_subject);
  v_prior uuid := business.command_claim(a.id, 'account.profile_update', a.id::text, p_key, p_body_sha256);
  v_fields text[] := '{}';
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM business.catalog_expect_version(a.profile_version, p_expected_version);
  IF p_set_full_name AND p_full_name IS DISTINCT FROM a.profile_full_name THEN
    v_fields := v_fields || 'full_name'::text;
  END IF;
  IF p_set_phone AND p_phone IS DISTINCT FROM a.profile_phone THEN
    v_fields := v_fields || 'phone'::text;
  END IF;
  IF p_set_instagram AND p_instagram IS DISTINCT FROM a.profile_instagram THEN
    v_fields := v_fields || 'instagram'::text;
  END IF;
  BEGIN
    UPDATE business.account x
    SET profile_full_name = CASE WHEN p_set_full_name THEN p_full_name ELSE x.profile_full_name END,
        profile_phone = CASE WHEN p_set_phone THEN p_phone ELSE x.profile_phone END,
        profile_instagram = CASE WHEN p_set_instagram THEN p_instagram ELSE x.profile_instagram END,
        profile_version = x.profile_version + 1, profile_updated_at = pg_catalog.now()
    WHERE x.id = a.id;
  EXCEPTION WHEN check_violation THEN
    RAISE EXCEPTION 'profile_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('customer', a.id, 'account.profile_updated', 'account', a.id, NULL,
                                pg_catalog.json_build_object('fields', v_fields));
  PERFORM business.command_finish(a.id, 'account.profile_update', a.id::text, p_key, a.id);
  RETURN QUERY SELECT a.id, false;
END;
$$;

-- ------------------------------------------------------------------- claims

-- Owner-internal: the customer's view of a claim: what they sent and the
-- outcome; the order only once approved; never the chef's note.
CREATE FUNCTION business.order_claim_customer_view(c business.order_claim)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'id', c.id, 'version', c.version, 'state', c.state, 'path', c.path, 'channel', c.channel,
           'reference', c.reference, 'approximate_date', c.approximate_date, 'note', c.note,
           'created_at', c.received_at, 'decided_at', c.decided_at,
           'order_id', CASE WHEN c.state = 'approved' THEN c.decided_order_id END);
$$;

-- Owner-internal: link an unowned, locked order to an account: the owner,
-- the order version, every outstanding guest capability revoked (K00), the
-- association event and the audit. Returns the number of capabilities
-- revoked.
CREATE FUNCTION business.order_owner_link(p_order uuid, p_account uuid, p_source text, p_claim uuid, p_evidence text,
                                          p_actor uuid, p_now timestamptz)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_revoked integer;
  v_event uuid := pg_catalog.gen_random_uuid();
BEGIN
  UPDATE business.orders o SET owner_account_id = p_account, version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.id = p_order AND o.owner_account_id IS NULL;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order_owned' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.order_guest_capability g SET revoked_at = GREATEST(pg_catalog.now(), g.created_at)
  WHERE g.order_id = p_order AND g.revoked_at IS NULL;
  GET DIAGNOSTICS v_revoked = ROW_COUNT;
  BEGIN
    INSERT INTO business.order_owner_event (id, order_id, account_id, kind, source, claim_id, evidence, actor_account_id,
                                            occurred_at)
    VALUES (v_event, p_order, p_account, 'linked', p_source, p_claim, p_evidence, p_actor, p_now);
  EXCEPTION WHEN check_violation THEN
    RAISE EXCEPTION 'evidence_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', p_actor, 'order.owner_linked', 'order', p_order, NULL,
                                pg_catalog.json_build_object('source', p_source, 'claim_id', p_claim,
                                                             'association_id', v_event,
                                                             'guest_capabilities_revoked', v_revoked));
  RETURN v_revoked;
END;
$$;

-- POST /me/order-claims. Path c (p_guest_digest NULL): reference, channel,
-- optional approximate date and a note. Path a (p_guest_digest set): the
-- active read capability of an order the customer holds; channel
-- website_email_link, an optional note, nothing else; an absent, revoked
-- or foreign capability is GK404 (K00 generic 404), an order already in
-- this account is 409 already_yours. At most 5 pending claims per account
-- (409 claim_limit); one pending email-link claim per order (409
-- claim_pending). Nothing is linked: the chef decides.
CREATE FUNCTION business.order_claim_submit(
  p_subject uuid, p_key text, p_body_sha256 text, p_channel text, p_reference text, p_approximate_date text,
  p_note text, p_guest_digest bytea, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  a business.account%ROWTYPE := business.account_customer_lock(p_subject);
  v_prior uuid := business.command_claim(a.id, 'account.claim_submit', '', p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
  v_order business.orders%ROWTYPE;
  v_capability uuid;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'a receipt instant is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF p_channel IS NULL OR p_channel NOT IN ('instagram', 'offline', 'website_email_link') THEN
    RAISE EXCEPTION 'claim_channel_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF (SELECT pg_catalog.count(*) FROM business.order_claim c WHERE c.account_id = a.id AND c.state = 'pending') >= 5 THEN
    RAISE EXCEPTION 'claim_limit' USING ERRCODE = 'GK409';
  END IF;
  IF p_guest_digest IS NOT NULL THEN
    IF p_channel <> 'website_email_link' OR p_reference IS NOT NULL OR p_approximate_date IS NOT NULL THEN
      RAISE EXCEPTION 'claim_channel_invalid' USING ERRCODE = 'GK422';
    END IF;
    SELECT g.id INTO v_capability
    FROM business.order_guest_capability g
    WHERE g.capability_digest = p_guest_digest AND g.revoked_at IS NULL AND 'read' = ANY (g.allowed_commands);
    IF v_capability IS NULL THEN
      RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
    END IF;
    SELECT o.* INTO STRICT v_order FROM business.orders o
    WHERE o.id = (SELECT g.order_id FROM business.order_guest_capability g WHERE g.id = v_capability);
    IF v_order.owner_account_id = a.id THEN
      RAISE EXCEPTION 'already_yours' USING ERRCODE = 'GK409';
    ELSIF v_order.owner_account_id IS NOT NULL THEN
      RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
    END IF;
    BEGIN
      INSERT INTO business.order_claim (id, account_id, path, channel, note, capability_order_id, capability_id, received_at)
      VALUES (v_id, a.id, 'email_link', 'website_email_link', p_note, v_order.id, v_capability, p_now);
    EXCEPTION
      WHEN unique_violation THEN RAISE EXCEPTION 'claim_pending' USING ERRCODE = 'GK409';
      WHEN check_violation THEN RAISE EXCEPTION 'claim_note_invalid' USING ERRCODE = 'GK422';
    END;
  ELSE
    IF p_reference IS NULL THEN
      RAISE EXCEPTION 'claim_reference_invalid' USING ERRCODE = 'GK422';
    END IF;
    IF p_note IS NULL THEN
      RAISE EXCEPTION 'claim_note_invalid' USING ERRCODE = 'GK422';
    END IF;
    BEGIN
      INSERT INTO business.order_claim (id, account_id, path, channel, reference, approximate_date, note, received_at)
      VALUES (v_id, a.id, 'evidence', p_channel, p_reference, p_approximate_date, p_note, p_now);
    EXCEPTION WHEN check_violation THEN
      RAISE EXCEPTION 'claim_invalid' USING ERRCODE = 'GK422';
    END;
  END IF;
  PERFORM business.audit_record('customer', a.id, 'order_claim.submitted', 'order_claim', v_id, NULL,
                                pg_catalog.json_build_object('path', CASE WHEN p_guest_digest IS NULL THEN 'evidence'
                                                                          ELSE 'email_link' END,
                                                             'channel', p_channel));
  PERFORM business.command_finish(a.id, 'account.claim_submit', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- POST /me/order-claims/{id}/withdraw: the customer withdraws a pending
-- claim of theirs.
CREATE FUNCTION business.order_claim_withdraw(p_subject uuid, p_key text, p_body_sha256 text, p_claim uuid,
                                              p_expected_version integer, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  a business.account%ROWTYPE := business.account_customer_lock(p_subject);
  v_prior uuid := business.command_claim(a.id, 'account.claim_withdraw', coalesce(p_claim::text, ''), p_key, p_body_sha256);
  c business.order_claim%ROWTYPE;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT * INTO c FROM business.order_claim x WHERE x.id = p_claim AND x.account_id = a.id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'claim not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(c.version, p_expected_version);
  IF c.state <> 'pending' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.order_claim x SET state = 'withdrawn', decided_at = p_now, version = x.version + 1 WHERE x.id = c.id;
  PERFORM business.audit_record('customer', a.id, 'order_claim.withdrawn', 'order_claim', c.id, NULL, NULL);
  PERFORM business.command_finish(a.id, 'account.claim_withdraw', p_claim::text, p_key, c.id);
  RETURN QUERY SELECT c.id, false;
END;
$$;

-- The customer's claims, newest first (at most 50; closed ones are gone
-- with the account).
CREATE FUNCTION business.account_claim_list(p_subject uuid)
RETURNS json
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(pg_catalog.json_agg(business.order_claim_customer_view(c) ORDER BY c.received_at DESC, c.id DESC), '[]'::json)
  FROM (SELECT * FROM business.order_claim x
        WHERE x.account_id = business.account_customer_id(p_subject) AND x.state <> 'closed'
        ORDER BY x.received_at DESC, x.id DESC LIMIT 50) c;
$$;

CREATE FUNCTION business.account_claim_get(p_subject uuid, p_claim uuid)
RETURNS json
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.account_customer_id(p_subject);
  v jsonb;
BEGIN
  SELECT business.order_claim_customer_view(c) INTO v FROM business.order_claim c
  WHERE c.id = p_claim AND c.account_id = v_account AND c.state <> 'closed';
  IF v IS NULL THEN
    RAISE EXCEPTION 'claim not found' USING ERRCODE = 'GK404';
  END IF;
  RETURN v::json;
END;
$$;

-- The chef's claim queue: claims in p_state (oldest first, keyset), with
-- the claimant's optional profile to help matching. Chef only. Reads are
-- not audited one by one (as the staff order list); decisions are.
CREATE FUNCTION business.order_claim_staff_list(p_actor uuid, p_state text, p_after_received timestamptz, p_after_id uuid,
                                                p_limit integer)
RETURNS json
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_chef uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_items jsonb;
  v_count integer;
BEGIN
  IF p_state IS NULL OR p_state NOT IN ('pending', 'approved', 'declined', 'withdrawn') THEN
    RAISE EXCEPTION 'claim_state_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 50 THEN
    RAISE EXCEPTION 'page_limit_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF (p_after_received IS NULL) <> (p_after_id IS NULL) THEN
    RAISE EXCEPTION 'cursor_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
           'id', c.id, 'version', c.version, 'state', c.state, 'path', c.path, 'channel', c.channel,
           'reference', c.reference, 'approximate_date', c.approximate_date, 'note', c.note,
           'order_id', coalesce(c.decided_order_id, c.capability_order_id),
           'created_at', c.received_at, 'decided_at', c.decided_at, 'decided_by', c.decided_by,
           'decision_note', c.decision_note,
           'account', pg_catalog.jsonb_build_object('id', a.id, 'full_name', a.profile_full_name,
                                                    'instagram', a.profile_instagram, 'phone', a.profile_phone))
           ORDER BY c.received_at, c.id), '[]'::jsonb),
         pg_catalog.count(*)
  INTO v_items, v_count
  FROM (SELECT * FROM business.order_claim x
        WHERE x.state = p_state
          AND (p_after_received IS NULL OR (x.received_at, x.id) > (p_after_received, p_after_id))
        ORDER BY x.received_at, x.id
        LIMIT p_limit + 1) c
  JOIN business.account a ON a.id = c.account_id;
  IF v_count <= p_limit THEN
    RETURN pg_catalog.json_build_object('items', v_items, 'next', NULL);
  END IF;
  v_items := v_items - p_limit;
  RETURN pg_catalog.json_build_object('items', v_items,
           'next', pg_catalog.json_build_object('received_at', v_items -> (p_limit - 1) -> 'created_at',
                                                'id', v_items -> (p_limit - 1) -> 'id'));
END;
$$;

-- POST /admin/order-claims/{id}/decision (chef): approve links p_order to
-- the claimant (an email-link claim only to its own order; an order already
-- in that account is approved without a new link; an order in another
-- account is 409 order_owned) and revokes the order's guest capabilities;
-- decline records the outcome only (no order, nothing linked). The chef's
-- note is internal.
CREATE FUNCTION business.order_claim_decide(p_actor uuid, p_key text, p_body_sha256 text, p_claim uuid,
                                            p_expected_version integer, p_decision text, p_order uuid, p_note text,
                                            p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_chef uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_chef, 'order_claim.decide', coalesce(p_claim::text, ''), p_key, p_body_sha256);
  v_claimant uuid;
  a business.account%ROWTYPE;
  c business.order_claim%ROWTYPE;
  o business.orders%ROWTYPE;
  v_revoked integer := 0;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'a receipt instant is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF p_decision IS NULL OR p_decision NOT IN ('approve', 'decline')
     OR (p_decision = 'approve') <> (p_order IS NOT NULL) THEN
    RAISE EXCEPTION 'claim_decision_invalid' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.account_order_freeze_check();
  SELECT x.account_id INTO v_claimant FROM business.order_claim x WHERE x.id = p_claim;
  IF v_claimant IS NULL THEN
    RAISE EXCEPTION 'claim not found' USING ERRCODE = 'GK404';
  END IF;
  SELECT * INTO a FROM business.account x WHERE x.id = v_claimant FOR SHARE;
  SELECT * INTO c FROM business.order_claim x WHERE x.id = p_claim FOR UPDATE;
  PERFORM business.catalog_expect_version(c.version, p_expected_version);
  IF c.state <> 'pending' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF p_decision = 'approve' THEN
    IF a.disabled_at IS NOT NULL OR a.deleted_at IS NOT NULL THEN
      RAISE EXCEPTION 'account_unavailable' USING ERRCODE = 'GK409';
    END IF;
    IF c.path = 'email_link' AND p_order <> c.capability_order_id THEN
      RAISE EXCEPTION 'claim_order_invalid' USING ERRCODE = 'GK422';
    END IF;
    SELECT * INTO o FROM business.orders x WHERE x.id = p_order FOR UPDATE;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'claim_order_invalid' USING ERRCODE = 'GK422';
    END IF;
    IF o.owner_account_id IS NOT NULL AND o.owner_account_id <> a.id THEN
      RAISE EXCEPTION 'order_owned' USING ERRCODE = 'GK409';
    END IF;
    IF o.owner_account_id IS NULL THEN
      v_revoked := business.order_owner_link(o.id, a.id, 'claim', c.id, NULL, v_chef, p_now);
    END IF;
  END IF;
  BEGIN
    UPDATE business.order_claim x
    SET state = CASE WHEN p_decision = 'approve' THEN 'approved' ELSE 'declined' END,
        decided_order_id = p_order, decided_by = v_chef, decided_at = p_now, decision_note = p_note,
        version = x.version + 1
    WHERE x.id = c.id;
  EXCEPTION WHEN check_violation THEN
    RAISE EXCEPTION 'decision_note_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_chef,
                                CASE WHEN p_decision = 'approve' THEN 'order_claim.approved' ELSE 'order_claim.declined' END,
                                'order_claim', c.id, NULL,
                                pg_catalog.json_build_object('order_id', p_order, 'guest_capabilities_revoked', v_revoked));
  PERFORM business.command_finish(v_chef, 'order_claim.decide', p_claim::text, p_key, c.id);
  RETURN QUERY SELECT c.id, false;
END;
$$;

-- POST /admin/orders/{id}/owner (chef): add an unowned order to an account
-- at the customer's request, with an evidence note; the order's guest
-- capabilities are revoked. p_account or p_account_subject names the
-- account (exactly one); an unknown, disabled or deleted one is 422
-- account_unknown.
CREATE FUNCTION business.order_owner_assign(p_actor uuid, p_key text, p_body_sha256 text, p_order uuid,
                                            p_expected_version integer, p_account uuid, p_account_subject uuid,
                                            p_evidence text, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_chef uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_chef, 'order.owner_assign', coalesce(p_order::text, ''), p_key, p_body_sha256);
  a business.account%ROWTYPE;
  o business.orders%ROWTYPE;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'a receipt instant is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF (p_account IS NULL) = (p_account_subject IS NULL) THEN
    RAISE EXCEPTION 'account_unknown' USING ERRCODE = 'GK422';
  END IF;
  IF p_evidence IS NULL THEN
    RAISE EXCEPTION 'evidence_invalid' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.account_order_freeze_check();
  SELECT * INTO a FROM business.account x
  WHERE (p_account IS NOT NULL AND x.id = p_account) OR (p_account_subject IS NOT NULL AND x.provider_subject = p_account_subject)
  FOR SHARE;
  IF NOT FOUND OR a.disabled_at IS NOT NULL OR a.deleted_at IS NOT NULL THEN
    RAISE EXCEPTION 'account_unknown' USING ERRCODE = 'GK422';
  END IF;
  SELECT * INTO o FROM business.orders x WHERE x.id = p_order FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'order not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(o.version, p_expected_version);
  IF o.owner_account_id = a.id THEN
    RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
  ELSIF o.owner_account_id IS NOT NULL THEN
    RAISE EXCEPTION 'order_owned' USING ERRCODE = 'GK409';
  END IF;
  PERFORM business.order_owner_link(o.id, a.id, 'chef', NULL, p_evidence, v_chef, p_now);
  PERFORM business.command_finish(v_chef, 'order.owner_assign', p_order::text, p_key, o.id);
  RETURN QUERY SELECT o.id, false;
END;
$$;

-- ----------------------------------------------------------------- deletion

-- Owner-internal: why an order's money is not settled (deletion blocks), or
-- NULL. Creator 2026-11-05 (answer 2): blocked only while money is
-- unsettled (a payment being checked, paid awaiting a date, a refund in
-- progress). Of the staff attention flags (order_attention_flags) only the
-- money ones block: refund_needs_attention is a refund block that is not
-- returned (refund_in_progress), payment_unknown and payment_mismatch are
-- collections being checked (payment_being_checked), paid_awaiting_agreement
-- has its own reason and refund_quarantine is refund_being_checked. Other
-- flags (notice_not_sent: an order email was not queued) are staff work,
-- not money, and never block.
CREATE FUNCTION business.account_order_block(p_order business.orders)
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
                          AND (pc.state IN ('created', 'pending', 'unknown') OR pc.mismatch_reason IS NOT NULL))
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
         END
  FROM (SELECT business.order_refund_summary(p_order.id)::jsonb AS rs) x;
$$;

-- Owner-internal: the facts a deletion depends on, canonical (jsonb):
-- blocking orders with their reason, confirmed upcoming orders with their
-- date (warning), every linked order, pending claims and staff grants.
CREATE FUNCTION business.account_deletion_facts(p_account uuid)
RETURNS jsonb
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.jsonb_build_object(
           'blocked', coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object('order_id', y.id, 'reason', y.reason)
                                                    ORDER BY y.id) FILTER (WHERE y.reason IS NOT NULL), '[]'::jsonb),
           'upcoming', coalesce(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object('order_id', y.id, 'date', y.day,
                                                                                  'lifecycle', y.visible)
                                                     ORDER BY y.id)
                                FILTER (WHERE y.reason IS NULL AND y.visible IN ('confirmed', 'in_production', 'ready')),
                                '[]'::jsonb),
           'orders', coalesce(pg_catalog.jsonb_agg(y.id ORDER BY y.id) FILTER (WHERE y.id IS NOT NULL), '[]'::jsonb),
           'pending_claims', (SELECT pg_catalog.count(*) FROM business.order_claim c
                              WHERE c.account_id = p_account AND c.state = 'pending'),
           'staff_capabilities', coalesce((SELECT pg_catalog.jsonb_agg(s.capability ORDER BY s.capability)
                                           FROM business.staff_capability s
                                           WHERE s.account_id = p_account AND s.revoked_at IS NULL), '[]'::jsonb))
  FROM (SELECT 1) one
  LEFT JOIN LATERAL (
    SELECT o.id, business.account_order_block(o) AS reason, business.order_visible_lifecycle(o) AS visible,
           business.order_visible_appointment(o) ->> 'date' AS day
    FROM business.orders o WHERE o.owner_account_id = p_account) y ON true;
$$;

-- POST /me/deletion-preview: the facts, stored and hashed for 15 minutes.
CREATE FUNCTION business.account_deletion_preview_create(p_subject uuid, p_now timestamptz)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.account_customer_id(p_subject);
  v_id uuid := pg_catalog.gen_random_uuid();
  v_facts jsonb;
  v_hash text;
BEGIN
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'a receipt instant is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Bounded retention of previews: expired more than a day ago, a few at a
  -- time (they hold order IDs and reasons only).
  DELETE FROM business.account_deletion_preview x
  WHERE x.id IN (SELECT y.id FROM business.account_deletion_preview y
                 WHERE y.expires_at < p_now - interval '1 day' ORDER BY y.expires_at LIMIT 100);
  v_facts := business.account_deletion_facts(v_account);
  v_hash := pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_facts::text, 'UTF8')), 'hex');
  INSERT INTO business.account_deletion_preview (id, account_id, facts, facts_sha256, created_at, expires_at)
  VALUES (v_id, v_account, v_facts, v_hash, p_now, p_now + interval '15 minutes');
  RETURN pg_catalog.json_build_object(
           'preview_id', v_id, 'preview_hash', v_hash, 'expires_at', p_now + interval '15 minutes',
           'can_delete', pg_catalog.jsonb_array_length(v_facts -> 'blocked') = 0
                         AND pg_catalog.jsonb_array_length(v_facts -> 'staff_capabilities') = 0,
           'blocked', v_facts -> 'blocked', 'upcoming', v_facts -> 'upcoming',
           'linked_orders', pg_catalog.jsonb_array_length(v_facts -> 'orders'),
           'pending_claims', v_facts -> 'pending_claims',
           'staff_capabilities', v_facts -> 'staff_capabilities');
END;
$$;

-- POST /me/deletion. p_authenticated_at is the token's latest email-link or
-- Google sign-in instant (the API reads it from the amr claim; password
-- sign-ins do not count): it must be within 10 minutes before the receipt
-- (GK403 reauthentication_required). The same key and body read the
-- deletion back first, without a fresh sign-in, also once the account is
-- deleted and during a freeze. An active staff account is 409
-- staff_account (operator path).
CREATE FUNCTION business.account_delete(p_subject uuid, p_key text, p_body_sha256 text, p_preview uuid,
                                        p_preview_hash text, p_authenticated_at timestamptz, p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean, deleted_at timestamptz, orders_unlinked integer)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  a business.account%ROWTYPE;
  v_prior uuid;
  pv business.account_deletion_preview%ROWTYPE;
  v_facts jsonb;
  v_unlinked integer;
  v_capabilities integer;
  v_claims integer;
  v_frozen boolean;
BEGIN
  IF p_now IS NULL THEN
    RAISE EXCEPTION 'a receipt instant is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Freeze first in the lock order; refused only after the replay lookup.
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  SELECT * INTO a FROM business.account x WHERE x.provider_subject = p_subject FOR NO KEY UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'account_unavailable' USING ERRCODE = 'GK403';
  END IF;
  -- A lost response is read back with the same key and body, without a
  -- new sign-in and also during a freeze.
  v_prior := business.command_claim(a.id, 'account.delete', '', p_key, p_body_sha256);
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT t.account_id, true, t.deleted_at, t.orders_unlinked
                 FROM business.account_tombstone t WHERE t.account_id = v_prior;
    RETURN;
  END IF;
  IF a.deleted_at IS NOT NULL THEN
    RAISE EXCEPTION 'account_deleted' USING ERRCODE = 'GK403';
  ELSIF a.disabled_at IS NOT NULL THEN
    RAISE EXCEPTION 'account_unavailable' USING ERRCODE = 'GK403';
  END IF;
  IF p_authenticated_at IS NULL OR p_authenticated_at > p_now + interval '1 minute'
     OR p_now - p_authenticated_at > interval '10 minutes' THEN
    RAISE EXCEPTION 'reauthentication_required' USING ERRCODE = 'GK403';
  END IF;
  IF v_frozen THEN
    RAISE EXCEPTION 'recovery_frozen' USING ERRCODE = 'GK503';
  END IF;
  -- Staff accounts are not deleted by their holder: the operator revokes
  -- the grants first (gk-identity-admin), then the customer may delete.
  IF EXISTS (SELECT 1 FROM business.staff_capability s WHERE s.account_id = a.id AND s.revoked_at IS NULL) THEN
    RAISE EXCEPTION 'staff_account' USING ERRCODE = 'GK409';
  END IF;
  SELECT * INTO pv FROM business.account_deletion_preview x WHERE x.id = p_preview AND x.account_id = a.id;
  IF NOT FOUND OR p_preview_hash IS NULL OR pv.facts_sha256 <> p_preview_hash THEN
    RAISE EXCEPTION 'deletion_preview_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_now >= pv.expires_at THEN
    RAISE EXCEPTION 'deletion_preview_stale' USING ERRCODE = 'GK409';
  END IF;
  PERFORM 1 FROM business.order_claim c WHERE c.account_id = a.id ORDER BY c.id FOR UPDATE;
  PERFORM 1 FROM business.orders o WHERE o.owner_account_id = a.id ORDER BY o.id FOR UPDATE;
  v_facts := business.account_deletion_facts(a.id);
  IF pg_catalog.jsonb_array_length(v_facts -> 'blocked') > 0 THEN
    RAISE EXCEPTION 'account_deletion_blocked' USING ERRCODE = 'GK409';
  END IF;
  IF pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(v_facts::text, 'UTF8')), 'hex') <> pv.facts_sha256 THEN
    RAISE EXCEPTION 'deletion_preview_stale' USING ERRCODE = 'GK409';
  END IF;

  -- Orders: unlinked (their details keep their own retention, K09), their
  -- guest capabilities revoked, each unlink in the association history.
  INSERT INTO business.order_owner_event (id, order_id, account_id, kind, source, actor_account_id, occurred_at)
  SELECT pg_catalog.gen_random_uuid(), o.id, a.id, 'unlinked', 'account_deleted', a.id, p_now
  FROM business.orders o WHERE o.owner_account_id = a.id;
  UPDATE business.order_guest_capability g SET revoked_at = GREATEST(pg_catalog.now(), g.created_at)
  FROM business.orders o
  WHERE o.owner_account_id = a.id AND g.order_id = o.id AND g.revoked_at IS NULL;
  GET DIAGNOSTICS v_capabilities = ROW_COUNT;
  UPDATE business.orders o SET owner_account_id = NULL, version = o.version + 1, updated_at = pg_catalog.now()
  WHERE o.owner_account_id = a.id;
  GET DIAGNOSTICS v_unlinked = ROW_COUNT;
  -- The chef's evidence notes about this account's orders are erased.
  UPDATE business.order_owner_event e SET evidence = NULL WHERE e.account_id = a.id AND e.evidence IS NOT NULL;
  -- Claims: pending ones closed; every claim's free text (and the chef's
  -- note about it) erased.
  UPDATE business.order_claim c
  SET state = CASE WHEN c.state = 'pending' THEN 'closed' ELSE c.state END,
      reference = NULL, note = NULL, approximate_date = NULL, decision_note = NULL, erased_at = p_now,
      version = c.version + 1
  WHERE c.account_id = a.id;
  GET DIAGNOSTICS v_claims = ROW_COUNT;
  UPDATE business.account x
  SET profile_full_name = NULL, profile_phone = NULL, profile_instagram = NULL,
      profile_version = x.profile_version + 1, profile_updated_at = pg_catalog.now(),
      disabled_at = coalesce(x.disabled_at, pg_catalog.now()), deleted_at = pg_catalog.now(), version = x.version + 1
  WHERE x.id = a.id;
  INSERT INTO business.account_tombstone (account_id, deleted_at, reason, orders_unlinked)
  VALUES (a.id, p_now, 'customer_request', v_unlinked);
  INSERT INTO business.account_provider_deletion (account_id, requested_at) VALUES (a.id, p_now);
  PERFORM business.audit_record('customer', a.id, 'account.deleted', 'account', a.id, NULL,
                                pg_catalog.json_build_object('orders_unlinked', v_unlinked,
                                                             'upcoming_orders', pg_catalog.jsonb_array_length(v_facts -> 'upcoming'),
                                                             'guest_capabilities_revoked', v_capabilities,
                                                             'claims_erased', v_claims));
  PERFORM business.command_finish(a.id, 'account.delete', '', p_key, a.id);
  RETURN QUERY SELECT a.id, false, p_now, v_unlinked;
END;
$$;

-- --------------------------------------------------------------- inactivity

-- Owner-internal: a London calendar shift (K09: anniversaries in
-- Europe/London, a missing day clamps to the month's end, the local clock
-- kept).
CREATE FUNCTION business.account_london_shift(p_at timestamptz, p_by interval)
RETURNS timestamptz
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$ SELECT ((p_at AT TIME ZONE 'Europe/London') + p_by) AT TIME ZONE 'Europe/London' $$;

-- Owner-internal: the account's retention step at p_now: the next notice
-- due ('warning', 'reminder' or 'deletion'), its activity anchor and the
-- deletion instant, or no row. Deleted, disabled and staff accounts are
-- never due; deletion is not due while an order's money is unsettled. The
-- reminder comes 7 days before deletion: a reminder recorded later than
-- that (a worker outage) moves deletion to 7 London days after it, never
-- earlier than the warning's 30 days.
CREATE FUNCTION business.account_retention_step(p_account business.account, p_now timestamptz)
RETURNS TABLE (notice text, activity_anchor timestamptz, deletion_due_at timestamptz)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH anchor AS (
    SELECT GREATEST(coalesce(p_account.last_active_at, p_account.created_at),
                    coalesce((SELECT max(o.placed_at) FROM business.orders o WHERE o.owner_account_id = p_account.id),
                             '-infinity'::timestamptz)) AS at
  ), state AS (
    SELECT n.at, business.account_london_shift(n.at, interval '24 months') AS warn_at,
           w.deletion_due_at AS due, w.recorded_at AS warned_at, r.recorded_at AS reminded_at,
           GREATEST(w.deletion_due_at, business.account_london_shift(r.recorded_at, interval '7 days')) AS delete_at
    FROM anchor n
    LEFT JOIN business.account_retention_notice w
      ON w.account_id = p_account.id AND w.kind = 'warning' AND w.activity_anchor = n.at
    LEFT JOIN business.account_retention_notice r
      ON r.account_id = p_account.id AND r.kind = 'reminder' AND r.activity_anchor = n.at
  )
  SELECT CASE WHEN s.warned_at IS NULL THEN 'warning' WHEN s.reminded_at IS NULL THEN 'reminder' ELSE 'deletion' END,
         s.at,
         CASE WHEN s.warned_at IS NULL THEN business.account_london_shift(GREATEST(s.warn_at, p_now), interval '30 days')
              WHEN s.reminded_at IS NULL THEN GREATEST(s.due, business.account_london_shift(p_now, interval '7 days'))
              ELSE s.delete_at END
  FROM state s
  WHERE p_account.deleted_at IS NULL AND p_account.disabled_at IS NULL
    AND NOT EXISTS (SELECT 1 FROM business.staff_capability c WHERE c.account_id = p_account.id AND c.revoked_at IS NULL)
    AND ((s.warned_at IS NULL AND p_now >= s.warn_at)
         OR (s.warned_at IS NOT NULL AND s.reminded_at IS NULL
             AND p_now >= business.account_london_shift(s.due, interval '-7 days'))
         OR (s.reminded_at IS NOT NULL AND p_now >= s.delete_at
             AND NOT EXISTS (SELECT 1 FROM business.orders o
                             WHERE o.owner_account_id = p_account.id AND business.account_order_block(o) IS NOT NULL)));
$$;

-- Worker: accounts with a retention step due at p_now (at most p_limit,
-- longest inactive first). Read only.
CREATE FUNCTION business.account_retention_due(p_now timestamptz, p_limit integer)
RETURNS TABLE (account_id uuid, notice text, activity_anchor timestamptz, deletion_due_at timestamptz)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_now IS NULL OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN
    RAISE EXCEPTION 'page_limit_invalid' USING ERRCODE = 'GK422';
  END IF;
  RETURN QUERY
  SELECT a.id, s.notice, s.activity_anchor, s.deletion_due_at
  FROM business.account a
  CROSS JOIN LATERAL business.account_retention_step(a, p_now) s
  ORDER BY s.activity_anchor, a.id
  LIMIT p_limit;
END;
$$;

-- Owner only for now: record that a warning or reminder for p_anchor was
-- sent; false when it is no longer due (the customer came back, or it was
-- recorded already). A warning fixes the deletion instant at 30 London days
-- after it is recorded; a reminder records max(that, 7 London days after
-- it). No runtime role may call it: a warning starts the deletion clock, so
-- it must be recorded with its email (notification intent) in one
-- lease-fenced worker transaction, which the email step will add.
CREATE FUNCTION business.account_retention_notice_record(p_account uuid, p_kind text, p_anchor timestamptz,
                                                         p_now timestamptz)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  a business.account%ROWTYPE;
  s record;
  v_due timestamptz;
BEGIN
  IF p_kind IS NULL OR p_kind NOT IN ('warning', 'reminder') OR p_anchor IS NULL OR p_now IS NULL THEN
    RAISE EXCEPTION 'retention notice needs a kind, an anchor and an instant' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT * INTO a FROM business.account x WHERE x.id = p_account FOR NO KEY UPDATE;
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  SELECT * INTO s FROM business.account_retention_step(a, p_now) st;
  IF s.notice IS DISTINCT FROM p_kind OR s.activity_anchor IS DISTINCT FROM p_anchor THEN
    RETURN false;
  END IF;
  v_due := CASE WHEN p_kind = 'warning' THEN business.account_london_shift(p_now, interval '30 days') ELSE s.deletion_due_at END;
  INSERT INTO business.account_retention_notice (id, account_id, kind, activity_anchor, deletion_due_at, recorded_at)
  VALUES (pg_catalog.gen_random_uuid(), a.id, p_kind, p_anchor, v_due, p_now)
  ON CONFLICT DO NOTHING;
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  PERFORM business.audit_record('system', NULL, 'account.retention_notice_recorded', 'account', a.id, NULL,
                                pg_catalog.json_build_object('kind', p_kind, 'deletion_due_at', v_due));
  RETURN true;
END;
$$;

REVOKE ALL ON TABLE business.order_claim FROM PUBLIC;
REVOKE ALL ON TABLE business.order_owner_event FROM PUBLIC;
REVOKE ALL ON TABLE business.account_deletion_preview FROM PUBLIC;
REVOKE ALL ON TABLE business.account_tombstone FROM PUBLIC;
REVOKE ALL ON TABLE business.account_provider_deletion FROM PUBLIC;
REVOKE ALL ON TABLE business.account_retention_notice FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_owner_event_guard() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_owner_guard() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.identity_resolve_account(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_customer_id(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_customer_lock(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_order_freeze_check() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_owner_source(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_customer_summary(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_order_page(uuid, timestamptz, uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_profile_get(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_profile_update(uuid, text, text, integer, boolean, text, boolean, text, boolean, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_claim_customer_view(business.order_claim) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_owner_link(uuid, uuid, text, uuid, text, uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_claim_submit(uuid, text, text, text, text, text, text, bytea, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_claim_withdraw(uuid, text, text, uuid, integer, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_claim_list(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_claim_get(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_claim_staff_list(uuid, text, timestamptz, uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_claim_decide(uuid, text, text, uuid, integer, text, uuid, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_owner_assign(uuid, text, text, uuid, integer, uuid, uuid, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_order_block(business.orders) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_deletion_facts(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_deletion_preview_create(uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_delete(uuid, text, text, uuid, text, timestamptz, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_london_shift(timestamptz, interval) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_retention_step(business.account, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_retention_due(timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_retention_notice_record(uuid, text, timestamptz, timestamptz) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.identity_resolve_account(uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_order_page(uuid, timestamptz, uuid, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_profile_get(uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_profile_update(uuid, text, text, integer, boolean, text, boolean, text, boolean, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_claim_submit(uuid, text, text, text, text, text, text, bytea, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_claim_withdraw(uuid, text, text, uuid, integer, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_claim_list(uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_claim_get(uuid, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_claim_staff_list(uuid, text, timestamptz, uuid, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_claim_decide(uuid, text, text, uuid, integer, text, uuid, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.order_owner_assign(uuid, text, text, uuid, integer, uuid, uuid, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_deletion_preview_create(uuid, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_delete(uuid, text, text, uuid, text, timestamptz, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.account_retention_due(timestamptz, integer) TO gk_worker;
-- account_retention_notice_record: no grant (owner only) until the email
-- step records the notice with its notification intent.
