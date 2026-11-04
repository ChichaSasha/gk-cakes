-- GK-025 A1: discount codes and storefront quotes (contracts r6.5 K00, K02,
-- K03, K04; owner decisions GK-007 interview-r1, GK-003 gk019-owner-r6,
-- GK-005 vat and money_precision).
--
-- Discount codes. A code (stored upper case, matched without regard to case
-- or surrounding spaces) has immutable versions. Two kinds exist (GK-007
-- discount_types): a percentage of the cake price (basis points; never
-- delivery) and free delivery (it waives the chef-quoted GK delivery charge
-- later, after the chef's distance review; it changes no cake amount). A
-- version may expire and may limit total and per-customer uses; a
-- per-customer limit needs a signed-in customer (GK-007
-- limited_codes_signin). One code per order (stacking A). Usage is reserved
-- and consumed by checkout (A2), so quotes count no uses.
--
-- Quotes. The server prices a storefront basket for one appointment: lines
-- of made-to-order variants (each variant's current recipe version and
-- price) or ready-made cakes (a released lot's price). A percentage code
-- gives D = round_half_up(subtotal * bps / 10000), allocated to lines by
-- floor of the proportional share, then one penny each by largest remainder,
-- ties by line number (K03). No VAT is added (GK-005 vat: not registered; no
-- exemption is claimed). GK delivery is quoted later by the chef and paid
-- separately, so a quote covers cakes only. A quote is immutable, valid for
-- 24 elapsed hours from when it is first returned (K02), reserves nothing
-- and belongs to its signed-in customer or to the holder of a fresh guest
-- proof bound to that quote alone (only its SHA-256 digest is stored); a
-- guessed quote ID gives no access. A ready-made cake must still be within
-- its safe expiry at the end of the appointment. Expired quotes are removed
-- by an owner-only retention purge; quotes are otherwise immutable.
--
-- Not yet quotable: resale lots (their handover is the original one) and
-- chef-approved shorter lead times; ready cakes need the ordinary 192-hour
-- lead until lead_time_exception exists.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.discount_code (
  id          uuid PRIMARY KEY,
  code        text NOT NULL UNIQUE CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{2,31}$'),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid NOT NULL REFERENCES business.account (id),
  archived_at timestamptz NULL,
  version     integer NOT NULL DEFAULT 1 CHECK (version >= 1)
);

CREATE TABLE business.discount_code_version (
  id                 uuid PRIMARY KEY,
  code_id            uuid NOT NULL REFERENCES business.discount_code (id),
  revision           integer NOT NULL CHECK (revision >= 1),
  kind               text NOT NULL CHECK (kind IN ('percentage', 'free_delivery')),
  basis_points       integer NULL CHECK (basis_points BETWEEN 1 AND 10000),
  expires_at         timestamptz NULL,
  total_limit        integer NULL CHECK (total_limit >= 1),
  per_customer_limit integer NULL CHECK (per_customer_limit >= 1),
  created_at         timestamptz NOT NULL DEFAULT now(),
  created_by         uuid NOT NULL REFERENCES business.account (id),
  CONSTRAINT discount_code_version_kind CHECK ((kind = 'percentage') = (basis_points IS NOT NULL)),
  UNIQUE (code_id, revision)
);

CREATE TABLE business.checkout_quote (
  id                 uuid PRIMARY KEY,
  owner_account_id   uuid NULL REFERENCES business.account (id),
  guest_proof_digest bytea NULL CHECK (guest_proof_digest IS NULL OR octet_length(guest_proof_digest) = 32),
  idempotency_key    text NOT NULL CHECK (idempotency_key ~ '^[\x21-\x7e]{1,128}$'),
  body_sha256        char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  handover_day       date NOT NULL,
  slot_id            uuid NOT NULL,
  slot_starts_at     timestamptz NOT NULL,
  slot_ends_at       timestamptz NOT NULL,
  schedule_revision  integer NOT NULL,
  fulfillment_mode   text NOT NULL CHECK (fulfillment_mode IN ('collection', 'customer_courier', 'gk_delivery')),
  code_version_id    uuid NULL REFERENCES business.discount_code_version (id),
  subtotal_minor     bigint NOT NULL CHECK (subtotal_minor > 0),
  discount_minor     bigint NOT NULL CHECK (discount_minor >= 0),
  total_minor        bigint NOT NULL,
  currency           text NOT NULL DEFAULT 'GBP' CHECK (currency = 'GBP'),
  sent_at            timestamptz NOT NULL,
  expires_at         timestamptz NOT NULL,
  CONSTRAINT checkout_quote_owner CHECK ((owner_account_id IS NULL) <> (guest_proof_digest IS NULL)),
  CONSTRAINT checkout_quote_total CHECK (total_minor = subtotal_minor - discount_minor AND total_minor >= 0),
  CONSTRAINT checkout_quote_expiry CHECK (expires_at = sent_at + interval '24 hours')
);

CREATE UNIQUE INDEX checkout_quote_owner_key_idx ON business.checkout_quote (owner_account_id, idempotency_key)
  WHERE owner_account_id IS NOT NULL;
-- A guest proof is fresh for one quote (K02): its digest is bound to it.
CREATE UNIQUE INDEX checkout_quote_guest_idx ON business.checkout_quote (guest_proof_digest)
  WHERE guest_proof_digest IS NOT NULL;
CREATE INDEX checkout_quote_expires_idx ON business.checkout_quote (expires_at);

CREATE TABLE business.checkout_quote_line (
  quote_id          uuid NOT NULL REFERENCES business.checkout_quote (id),
  line_no           integer NOT NULL CHECK (line_no BETWEEN 1 AND 20),
  variant_id        uuid NOT NULL REFERENCES business.variant (id),
  recipe_version_id uuid NOT NULL REFERENCES business.recipe_version (id),
  lot_id            uuid NULL REFERENCES business.finished_cake_lot (id),
  quantity          integer NOT NULL CHECK (quantity BETWEEN 1 AND 20),
  unit_price_minor  bigint NOT NULL CHECK (unit_price_minor >= 1),
  discount_minor    bigint NOT NULL CHECK (discount_minor >= 0),
  net_minor         bigint NOT NULL,
  CONSTRAINT checkout_quote_line_net CHECK (net_minor = unit_price_minor * quantity - discount_minor AND net_minor >= 0),
  PRIMARY KEY (quote_id, line_no)
);

CREATE TRIGGER discount_code_version_immutable BEFORE UPDATE OR DELETE ON business.discount_code_version
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER discount_code_no_delete BEFORE DELETE ON business.discount_code
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
-- Quotes never change; they are deleted only by quote_purge_expired, which
-- marks its own transaction.
CREATE FUNCTION business.quote_guard()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' AND pg_catalog.current_setting('business.quote_purge', true) = 'on' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME USING ERRCODE = 'GK409';
END;
$$;

CREATE TRIGGER checkout_quote_immutable BEFORE UPDATE OR DELETE ON business.checkout_quote
  FOR EACH ROW EXECUTE FUNCTION business.quote_guard();
CREATE TRIGGER checkout_quote_line_immutable BEFORE UPDATE OR DELETE ON business.checkout_quote_line
  FOR EACH ROW EXECUTE FUNCTION business.quote_guard();
CREATE TRIGGER discount_no_truncate_code BEFORE TRUNCATE ON business.discount_code
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER discount_no_truncate_version BEFORE TRUNCATE ON business.discount_code_version
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER quote_no_truncate BEFORE TRUNCATE ON business.checkout_quote
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER quote_line_no_truncate BEFORE TRUNCATE ON business.checkout_quote_line
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: a code as a customer typed it, normalized.
CREATE FUNCTION business.discount_normalize(p_code text)
RETURNS text
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$ SELECT pg_catalog.upper(pg_catalog.btrim(p_code)) $$;

-- Owner-internal: a percentage discount D of a subtotal (half up) and its
-- allocation to line amounts: floor of the proportional share, then one
-- penny each by largest remainder, ties by position (K03). Returns the
-- allocation per position; sum = D.
CREATE FUNCTION business.discount_allocate(p_amounts bigint[], p_basis_points integer)
RETURNS bigint[]
LANGUAGE plpgsql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_n integer := coalesce(pg_catalog.cardinality(p_amounts), 0);
  v_subtotal bigint := 0;
  v_discount bigint;
  v_alloc bigint[] := '{}';
  v_rem bigint[] := '{}';
  v_left bigint;
  v_best integer;
  i integer;
BEGIN
  IF v_n = 0 OR p_basis_points IS NULL OR p_basis_points NOT BETWEEN 0 AND 10000 THEN
    RAISE EXCEPTION 'discount allocation needs amounts and 0 to 10000 basis points' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  FOR i IN 1 .. v_n LOOP
    IF p_amounts[i] IS NULL OR p_amounts[i] < 0 THEN
      RAISE EXCEPTION 'amounts must be non-negative' USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_subtotal := v_subtotal + p_amounts[i];
  END LOOP;
  IF v_subtotal = 0 THEN
    RETURN pg_catalog.array_fill(0::bigint, ARRAY[v_n]);
  END IF;
  v_discount := (v_subtotal * p_basis_points + 5000) / 10000;
  FOR i IN 1 .. v_n LOOP
    v_alloc := v_alloc || ((v_discount * p_amounts[i]) / v_subtotal);
    v_rem := v_rem || ((v_discount * p_amounts[i]) % v_subtotal);
  END LOOP;
  v_left := v_discount;
  FOR i IN 1 .. v_n LOOP
    v_left := v_left - v_alloc[i];
  END LOOP;
  WHILE v_left > 0 LOOP
    v_best := NULL;
    FOR i IN 1 .. v_n LOOP
      IF v_rem[i] >= 0 AND (v_best IS NULL OR v_rem[i] > v_rem[v_best]) THEN
        v_best := i;
      END IF;
    END LOOP;
    v_alloc[v_best] := v_alloc[v_best] + 1;
    v_rem[v_best] := -1;
    v_left := v_left - 1;
  END LOOP;
  RETURN v_alloc;
END;
$$;

-- ---------------------------------------------------------- code commands

CREATE FUNCTION business.discount_check_version(p_kind text, p_basis_points integer, p_expires_at timestamptz,
                                                p_total_limit integer, p_per_customer_limit integer)
RETURNS void
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_kind IS NULL OR p_kind NOT IN ('percentage', 'free_delivery')
     OR (p_kind = 'percentage') <> (p_basis_points IS NOT NULL)
     OR p_basis_points NOT BETWEEN 1 AND 10000 THEN
    RAISE EXCEPTION 'kind_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expires_at IS NOT NULL AND p_expires_at <= pg_catalog.now() THEN
    RAISE EXCEPTION 'expires_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_total_limit < 1 THEN
    RAISE EXCEPTION 'limit_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_per_customer_limit < 1 THEN
    RAISE EXCEPTION 'per_customer_limit_invalid' USING ERRCODE = 'GK422';
  END IF;
END;
$$;

CREATE FUNCTION business.discount_code_create(p_actor uuid, p_key text, p_body_sha256 text, p_code text, p_kind text,
                                              p_basis_points integer, p_expires_at timestamptz, p_total_limit integer,
                                              p_per_customer_limit integer)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'discount.code_create', '', p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  PERFORM business.discount_check_version(p_kind, p_basis_points, p_expires_at, p_total_limit, p_per_customer_limit);
  BEGIN
    INSERT INTO business.discount_code (id, code, created_by) VALUES (v_id, business.discount_normalize(p_code), v_account);
  EXCEPTION
    WHEN unique_violation THEN RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
    WHEN check_violation OR not_null_violation THEN RAISE EXCEPTION 'code_invalid' USING ERRCODE = 'GK422';
  END;
  INSERT INTO business.discount_code_version (id, code_id, revision, kind, basis_points, expires_at, total_limit,
                                              per_customer_limit, created_by)
  VALUES (pg_catalog.gen_random_uuid(), v_id, 1, p_kind, p_basis_points, p_expires_at, p_total_limit,
          p_per_customer_limit, v_account);
  PERFORM business.audit_record('staff', v_account, 'discount.code_created', 'discount_code', v_id, NULL,
                                pg_catalog.json_build_object('kind', p_kind));
  PERFORM business.command_finish(v_account, 'discount.code_create', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- A new version of a code (new terms apply to new quotes; quotes keep the
-- version they priced with).
CREATE FUNCTION business.discount_code_revise(p_actor uuid, p_key text, p_body_sha256 text, p_code_id uuid,
                                              p_expected_version integer, p_kind text, p_basis_points integer,
                                              p_expires_at timestamptz, p_total_limit integer, p_per_customer_limit integer)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'discount.code_revise', coalesce(p_code_id::text, ''), p_key, p_body_sha256);
  v_version integer;
  v_archived timestamptz;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT c.version, c.archived_at INTO v_version, v_archived FROM business.discount_code c WHERE c.id = p_code_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'code not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  IF v_archived IS NOT NULL THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  PERFORM business.discount_check_version(p_kind, p_basis_points, p_expires_at, p_total_limit, p_per_customer_limit);
  INSERT INTO business.discount_code_version (id, code_id, revision, kind, basis_points, expires_at, total_limit,
                                              per_customer_limit, created_by)
  VALUES (pg_catalog.gen_random_uuid(), p_code_id, v_version + 1, p_kind, p_basis_points, p_expires_at, p_total_limit,
          p_per_customer_limit, v_account);
  UPDATE business.discount_code c SET version = c.version + 1 WHERE c.id = p_code_id;
  PERFORM business.audit_record('staff', v_account, 'discount.code_revised', 'discount_code', p_code_id, NULL,
                                pg_catalog.json_build_object('revision', v_version + 1));
  PERFORM business.command_finish(v_account, 'discount.code_revise', p_code_id::text, p_key, p_code_id);
  RETURN QUERY SELECT p_code_id, false;
END;
$$;

CREATE FUNCTION business.discount_code_archive(p_actor uuid, p_key text, p_body_sha256 text, p_code_id uuid,
                                               p_expected_version integer)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'discount.code_archive', coalesce(p_code_id::text, ''), p_key, p_body_sha256);
  v_version integer;
  v_archived timestamptz;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT c.version, c.archived_at INTO v_version, v_archived FROM business.discount_code c WHERE c.id = p_code_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'code not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  IF v_archived IS NOT NULL THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.discount_code c SET archived_at = pg_catalog.now(), version = c.version + 1 WHERE c.id = p_code_id;
  PERFORM business.audit_record('staff', v_account, 'discount.code_archived', 'discount_code', p_code_id, NULL, NULL);
  PERFORM business.command_finish(v_account, 'discount.code_archive', p_code_id::text, p_key, p_code_id);
  RETURN QUERY SELECT p_code_id, false;
END;
$$;

CREATE FUNCTION business.discount_staff_list(p_actor uuid, p_code uuid)
RETURNS TABLE (code_id uuid, code text, code_version integer, archived_at timestamptz, version_id uuid, kind text,
               basis_points integer, expires_at timestamptz, total_limit integer, per_customer_limit integer,
               version_created_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY
  SELECT c.id, c.code, c.version, c.archived_at, v.id, v.kind, v.basis_points, v.expires_at, v.total_limit,
         v.per_customer_limit, v.created_at
  FROM business.discount_code c
  JOIN LATERAL (SELECT * FROM business.discount_code_version dv WHERE dv.code_id = c.id
                ORDER BY dv.revision DESC LIMIT 1) v ON true
  WHERE p_code IS NULL OR c.id = p_code
  ORDER BY c.archived_at IS NOT NULL, c.code
  LIMIT 500;
END;
$$;

-- ------------------------------------------------------------------ quotes

-- Creates a quote. p_owner (a verified subject) or p_guest_digest (SHA-256
-- of a 256-bit guest proof), exactly one. p_lines: [{"variant_id": uuid,
-- "quantity": n}] for made-to-order cakes or [{"lot_id": uuid, "quantity":
-- n}] for ready-made cakes; 1-20 lines, at most 20 cakes. A repeated
-- (owner or guest, key) with the same body returns the original quote.
CREATE FUNCTION business.quote_create(p_owner uuid, p_guest_digest bytea, p_key text, p_body_sha256 text,
                                      p_day date, p_slot uuid, p_mode text, p_lines json, p_code text,
                                      p_now timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
  v_disabled boolean;
  v_prior uuid;
  v_prior_hash text;
  v_id uuid := pg_catalog.gen_random_uuid();
  v_line json;
  v_n integer := 0;
  v_variants uuid[] := '{}';
  v_recipes uuid[] := '{}';
  v_lots uuid[] := '{}';
  v_qty integer[] := '{}';
  v_price bigint[] := '{}';
  v_amounts bigint[] := '{}';
  v_alloc bigint[];
  v_mto integer := 0;
  v_total_qty integer := 0;
  v_subtotal bigint := 0;
  v_discount bigint := 0;
  v_variant uuid;
  v_lot uuid;
  v_q integer;
  v_recipe uuid;
  v_unit bigint;
  v_code_version uuid;
  v_code_kind text;
  v_bps integer;
  v_code_expires timestamptz;
  v_per_customer integer;
  v_slot record;
  i integer;
BEGIN
  IF (p_owner IS NULL) = (p_guest_digest IS NULL) OR (p_guest_digest IS NOT NULL AND octet_length(p_guest_digest) <> 32) THEN
    RAISE EXCEPTION 'proof_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' OR p_now IS NULL THEN
    RAISE EXCEPTION 'body hash and time are required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF p_owner IS NOT NULL THEN
    SELECT r.account_id, r.account_disabled INTO v_account, v_disabled FROM business.identity_resolve(p_owner) r;
    IF v_disabled THEN
      RAISE EXCEPTION 'account disabled' USING ERRCODE = 'GK403';
    END IF;
  END IF;
  -- Replay: same owner or guest and key. A concurrent first use waits on the
  -- unique index; the transaction-level advisory lock keeps it simple.
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended(
    'quote|' || coalesce(v_account::text || '|' || p_key, pg_catalog.encode(p_guest_digest, 'hex')), 0));
  SELECT q.id, q.body_sha256 INTO v_prior, v_prior_hash FROM business.checkout_quote q
  WHERE q.idempotency_key = p_key
    AND (q.owner_account_id = v_account OR q.guest_proof_digest = p_guest_digest);
  IF FOUND THEN
    IF v_prior_hash <> p_body_sha256 THEN
      RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
    END IF;
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  -- A guest proof belongs to one quote: a new quote needs a fresh proof.
  IF v_account IS NULL AND EXISTS (SELECT 1 FROM business.checkout_quote q WHERE q.guest_proof_digest = p_guest_digest) THEN
    RAISE EXCEPTION 'proof_reused' USING ERRCODE = 'GK422';
  END IF;

  IF p_mode IS NULL OR p_mode NOT IN ('collection', 'customer_courier', 'gk_delivery') THEN
    RAISE EXCEPTION 'mode_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_lines IS NULL OR pg_catalog.json_typeof(p_lines) <> 'array'
     OR pg_catalog.json_array_length(p_lines) NOT BETWEEN 1 AND 20 THEN
    RAISE EXCEPTION 'lines_invalid' USING ERRCODE = 'GK422';
  END IF;

  FOR v_line IN SELECT e FROM pg_catalog.json_array_elements(p_lines) e LOOP
    IF pg_catalog.json_typeof(v_line) <> 'object'
       OR pg_catalog.json_typeof(v_line -> 'quantity') IS DISTINCT FROM 'number'
       OR coalesce((v_line ->> 'quantity') !~ '^([1-9]|1[0-9]|20)$', true)
       OR ((v_line -> 'variant_id') IS NULL) = ((v_line -> 'lot_id') IS NULL)
       OR coalesce(pg_catalog.json_typeof(v_line -> 'variant_id'), 'string') <> 'string'
       OR coalesce(pg_catalog.json_typeof(v_line -> 'lot_id'), 'string') <> 'string'
       OR coalesce(coalesce(v_line ->> 'variant_id', v_line ->> 'lot_id')
                   !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$', true) THEN
      RAISE EXCEPTION 'lines_invalid' USING ERRCODE = 'GK422';
    END IF;
    v_q := (v_line ->> 'quantity')::integer;
    IF v_line ->> 'variant_id' IS NOT NULL THEN
      v_variant := (v_line ->> 'variant_id')::uuid;
      -- A purchasable made-to-order variant: active, of an active cake, with
      -- a current recipe version (which always declares its allergens).
      SELECT v.current_recipe_version_id, r.price_minor INTO v_recipe, v_unit
      FROM business.variant v
      JOIN business.cake c ON c.id = v.cake_id
      JOIN business.recipe_version r ON r.id = v.current_recipe_version_id
      WHERE v.id = v_variant AND v.archived_at IS NULL AND c.state = 'active' AND v.stock_mode = 'made_to_order';
      IF NOT FOUND THEN
        RAISE EXCEPTION 'item_unavailable' USING ERRCODE = 'GK409';
      END IF;
      v_lot := NULL;
      v_mto := v_mto + v_q;
    ELSE
      v_lot := (v_line ->> 'lot_id')::uuid;
      SELECT l.variant_id, l.recipe_version_id, l.price_minor INTO v_variant, v_recipe, v_unit
      FROM business.finished_cake_lot l
      JOIN business.variant v ON v.id = l.variant_id
      JOIN business.cake c ON c.id = v.cake_id
      WHERE l.id = v_lot AND l.kind = 'ready' AND l.released_at IS NOT NULL AND l.withdrawn_at IS NULL
        AND l.expires_at > p_now AND l.produced_quantity - l.reserved_quantity - l.sold_quantity >= v_q
        AND v.archived_at IS NULL AND c.state = 'active';
      IF NOT FOUND THEN
        RAISE EXCEPTION 'item_unavailable' USING ERRCODE = 'GK409';
      END IF;
    END IF;
    -- One line per made-to-order variant and per lot.
    FOR i IN 1 .. v_n LOOP
      IF (v_lot IS NULL AND v_lots[i] IS NULL AND v_variants[i] = v_variant)
         OR (v_lot IS NOT NULL AND v_lots[i] = v_lot) THEN
        RAISE EXCEPTION 'lines_duplicate' USING ERRCODE = 'GK422';
      END IF;
    END LOOP;
    v_n := v_n + 1;
    v_variants := v_variants || v_variant;
    v_recipes := v_recipes || v_recipe;
    v_lots := v_lots || v_lot;
    v_qty := v_qty || v_q;
    v_price := v_price || v_unit;
    v_amounts := v_amounts || (v_unit * v_q);
    v_total_qty := v_total_qty + v_q;
    v_subtotal := v_subtotal + v_unit * v_q;
  END LOOP;
  IF v_total_qty > 20 THEN
    RAISE EXCEPTION 'lines_invalid' USING ERRCODE = 'GK422';
  END IF;

  -- The appointment, as the public schedule offers it now: made-to-order
  -- cakes need their day places; ready-made cakes need none (a full day
  -- does not stop them).
  SELECT s.* INTO v_slot FROM business.schedule_public_slots(p_day, p_mode, p_now, greatest(v_mto, 1)) s
  WHERE s.slot_id = p_slot;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'slot_unavailable' USING ERRCODE = 'GK409';
  END IF;
  IF v_slot.availability NOT IN ('available', 'requestable')
     AND NOT (v_mto = 0 AND v_slot.reason_code = 'full') THEN
    RAISE EXCEPTION 'slot_unavailable' USING ERRCODE = 'GK409';
  END IF;
  -- A ready-made cake must be within its safe expiry when it is handed over
  -- (K01, K04): through the end of the appointment.
  FOR i IN 1 .. v_n LOOP
    IF v_lots[i] IS NOT NULL AND EXISTS (SELECT 1 FROM business.finished_cake_lot l
                                         WHERE l.id = v_lots[i] AND l.expires_at < v_slot.ends_at) THEN
      RAISE EXCEPTION 'item_unavailable' USING ERRCODE = 'GK409';
    END IF;
  END LOOP;

  IF p_code IS NOT NULL THEN
    SELECT v.id, v.kind, v.basis_points, v.expires_at, v.per_customer_limit
    INTO v_code_version, v_code_kind, v_bps, v_code_expires, v_per_customer
    FROM business.discount_code c
    JOIN LATERAL (SELECT * FROM business.discount_code_version dv WHERE dv.code_id = c.id
                  ORDER BY dv.revision DESC LIMIT 1) v ON true
    WHERE c.code = business.discount_normalize(p_code) AND c.archived_at IS NULL;
    IF NOT FOUND OR (v_code_expires IS NOT NULL AND v_code_expires <= p_now) THEN
      RAISE EXCEPTION 'code_unknown' USING ERRCODE = 'GK422';
    END IF;
    IF v_per_customer IS NOT NULL AND v_account IS NULL THEN
      RAISE EXCEPTION 'code_requires_sign_in' USING ERRCODE = 'GK422';
    END IF;
    IF v_code_kind = 'free_delivery' AND p_mode <> 'gk_delivery' THEN
      RAISE EXCEPTION 'code_not_applicable' USING ERRCODE = 'GK422';
    END IF;
  END IF;

  IF v_code_kind = 'percentage' THEN
    v_alloc := business.discount_allocate(v_amounts, v_bps);
  ELSE
    v_alloc := pg_catalog.array_fill(0::bigint, ARRAY[v_n]);
  END IF;
  FOR i IN 1 .. v_n LOOP
    v_discount := v_discount + v_alloc[i];
  END LOOP;

  INSERT INTO business.checkout_quote (id, owner_account_id, guest_proof_digest, idempotency_key, body_sha256,
                                       handover_day, slot_id, slot_starts_at, slot_ends_at, schedule_revision,
                                       fulfillment_mode, code_version_id, subtotal_minor, discount_minor, total_minor,
                                       sent_at, expires_at)
  VALUES (v_id, v_account, CASE WHEN v_account IS NULL THEN p_guest_digest END, p_key, p_body_sha256, p_day, p_slot,
          v_slot.starts_at, v_slot.ends_at, v_slot.schedule_revision, p_mode, v_code_version, v_subtotal, v_discount,
          v_subtotal - v_discount, p_now, p_now + interval '24 hours');
  FOR i IN 1 .. v_n LOOP
    INSERT INTO business.checkout_quote_line (quote_id, line_no, variant_id, recipe_version_id, lot_id, quantity,
                                              unit_price_minor, discount_minor, net_minor)
    VALUES (v_id, i, v_variants[i], v_recipes[i], v_lots[i], v_qty[i], v_price[i], v_alloc[i],
            v_price[i] * v_qty[i] - v_alloc[i]);
  END LOOP;
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- A quote with its lines, for its signed-in owner or the holder of its
-- guest proof; nothing otherwise (a guessed ID gives no access).
CREATE FUNCTION business.quote_get(p_quote uuid, p_owner uuid, p_guest_digest bytea)
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
           'id', q.id, 'version', 1, 'sent_at', q.sent_at, 'expires_at', q.expires_at, 'date', q.handover_day,
           'slot', pg_catalog.json_build_object('id', q.slot_id, 'starts_at', q.slot_starts_at, 'ends_at', q.slot_ends_at),
           'schedule_version', q.schedule_revision, 'fulfillment_mode', q.fulfillment_mode,
           'subtotal_minor', q.subtotal_minor, 'discount_minor', q.discount_minor, 'total_minor', q.total_minor,
           'currency', q.currency,
           'discount', CASE WHEN dv.id IS NULL THEN NULL ELSE pg_catalog.json_build_object(
             'code', dc.code, 'kind', dv.kind, 'basis_points', dv.basis_points, 'version_id', dv.id) END,
           'lines', (SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                       'line_no', l.line_no, 'variant_id', l.variant_id, 'lot_id', l.lot_id,
                       'recipe_version_id', l.recipe_version_id, 'cake_id', v.cake_id, 'cake_name', c.name,
                       'flavour', r.flavour_name, 'size_code', r.size_code, 'quantity', l.quantity,
                       'unit_price_minor', l.unit_price_minor, 'discount_minor', l.discount_minor,
                       'net_minor', l.net_minor) ORDER BY l.line_no)
                     FROM business.checkout_quote_line l
                     JOIN business.variant v ON v.id = l.variant_id
                     JOIN business.cake c ON c.id = v.cake_id
                     JOIN business.recipe_version r ON r.id = l.recipe_version_id
                     WHERE l.quote_id = q.id))
  INTO v_result
  FROM business.checkout_quote q
  LEFT JOIN business.discount_code_version dv ON dv.id = q.code_version_id
  LEFT JOIN business.discount_code dc ON dc.id = dv.code_id
  WHERE q.id = p_quote
    AND ((p_owner IS NOT NULL AND q.owner_account_id = (SELECT a.id FROM business.account a
                                                       WHERE a.provider_subject = p_owner AND a.disabled_at IS NULL))
         OR (p_guest_digest IS NOT NULL AND q.guest_proof_digest = p_guest_digest));
  RETURN v_result;
END;
$$;

-- Owner-only retention: remove quotes that expired before p_before (at
-- least 30 days ago, K00 key retention). Returns how many were removed.
-- Checkout (A2) keeps what an order needs in its own snapshot rows.
CREATE FUNCTION business.quote_purge_expired(p_before timestamptz)
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
  WHERE l.quote_id = q.id AND q.expires_at < p_before;
  DELETE FROM business.checkout_quote q WHERE q.expires_at < p_before;
  GET DIAGNOSTICS v_count = ROW_COUNT;
  PERFORM pg_catalog.set_config('business.quote_purge', 'off', true);
  RETURN v_count;
END;
$$;

REVOKE ALL ON TABLE business.discount_code, business.discount_code_version, business.checkout_quote,
  business.checkout_quote_line FROM PUBLIC;

REVOKE ALL ON FUNCTION business.discount_normalize(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.discount_allocate(bigint[], integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.discount_check_version(text, integer, timestamptz, integer, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.discount_code_create(uuid, text, text, text, text, integer, timestamptz, integer, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.discount_code_revise(uuid, text, text, uuid, integer, text, integer, timestamptz, integer, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.discount_code_archive(uuid, text, text, uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.discount_staff_list(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.quote_guard() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.quote_purge_expired(timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.quote_create(uuid, bytea, text, text, date, uuid, text, json, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.quote_get(uuid, uuid, bytea) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.discount_code_create(uuid, text, text, text, text, integer, timestamptz, integer, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.discount_code_revise(uuid, text, text, uuid, integer, text, integer, timestamptz, integer, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.discount_code_archive(uuid, text, text, uuid, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.discount_staff_list(uuid, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.quote_create(uuid, bytea, text, text, date, uuid, text, json, text, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.quote_get(uuid, uuid, bytea) TO gk_api;
