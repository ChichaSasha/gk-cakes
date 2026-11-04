-- GK-022 A1: cake catalog core (contracts r6.5 K00, K01, K07; schema r1.5
-- recipe_version and the unexpanded variant/inspiration relations).
--
-- A cake is a listing: a photo-led design that customers browse. It may have
-- purchasable variants (one flavour in one size, made to order or from ready
-- stock); a cake without a purchasable variant is inspiration only ("by
-- request"). A variant's price, servings, coating, composition, allergens and
-- recipe lines live in an immutable recipe version: a new revision is a new
-- row and becomes the variant's current one, so an order that pins a version
-- keeps exactly what was bought (K01, K07). There is no automatic scaling
-- between sizes: each size has its own recipe (K07). Referenced rows are
-- archived, never deleted. Missing allergen disclosure disables buying: a
-- variant without a current recipe version, which always carries a non-empty
-- allergen declaration and the shared-kitchen notice, is not purchasable.
--
-- Every change is a chef command through a gateway function below. Commands
-- carry an Idempotency-Key (K00): the same key and body return the original
-- result; a changed body is refused (key_reused). Existing objects also need
-- their expected version. Each change is audited. Reads for customers return
-- active cakes only; staff reads (chef, technical support) see every state.
--
-- Refusals use SQLSTATE GK403 (no capability), GK404 (missing), GK409
-- (conflict: the message is the code) and GK422 (invalid input), so a missing
-- grant (42501) or a constraint failure is never mistaken for one of them.

SET LOCAL ROLE gk_owner;

-- K00 command identity: one row per (account, command, resource, key). The API hashes
-- the command's canonical input (path resource and body). result_id is set
-- inside the same transaction as the effect. Retention (30 days) is a later
-- maintenance job; business uniqueness prevents duplicate effects afterwards.
CREATE TABLE business.command_receipt (
  account_id      uuid NOT NULL REFERENCES business.account (id),
  command         text NOT NULL CHECK (command ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$' AND length(command) <= 80),
  -- The path resource the command acts on ('' for a create without one).
  resource        text NOT NULL CHECK (resource = '' OR resource ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
  idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[\x21-\x7e]{1,128}$'),
  body_sha256     char(64) NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
  result_id       uuid NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, command, resource, idempotency_key)
);

CREATE INDEX command_receipt_created_idx ON business.command_receipt (created_at);

CREATE TABLE business.flavour (
  id          uuid PRIMARY KEY,
  name        text NOT NULL CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 80 AND name !~ '[[:cntrl:]]'),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid NOT NULL REFERENCES business.account (id),
  archived_at timestamptz NULL,
  version     integer NOT NULL DEFAULT 1 CHECK (version >= 1)
);

CREATE UNIQUE INDEX flavour_active_name_idx ON business.flavour (lower(name)) WHERE archived_at IS NULL;

-- The shared-kitchen allergen notice, versioned separately from each
-- recipe's "contains" declaration (K01). Immutable; a recipe version pins one.
CREATE TABLE business.allergen_notice (
  id         uuid PRIMARY KEY,
  body       text NOT NULL CHECK (body = btrim(body) AND char_length(body) BETWEEN 1 AND 2000
                                  AND body !~ '[\x01-\x09\x0b-\x1f\x7f]'),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL REFERENCES business.account (id)
);

CREATE INDEX allergen_notice_created_idx ON business.allergen_notice (created_at DESC, id DESC);

CREATE TABLE business.cake (
  id           uuid PRIMARY KEY,
  name         text NOT NULL CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 120 AND name !~ '[[:cntrl:]]'),
  description  text NOT NULL DEFAULT '' CHECK (description = btrim(description) AND char_length(description) <= 4000
                                              AND description !~ '[\x01-\x09\x0b-\x1f\x7f]'),
  state        text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'active', 'archived')),
  -- First activation; the public listing order. Kept when re-activated.
  -- A draft archived before it was ever published keeps NULL.
  published_at timestamptz NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  CONSTRAINT cake_published CHECK (state <> 'active' OR published_at IS NOT NULL)
);

-- The public page query: active cakes, newest first, keyset cursor.
CREATE INDEX cake_public_page_idx ON business.cake (published_at DESC, id DESC) WHERE state = 'active';
CREATE INDEX cake_admin_page_idx ON business.cake (created_at DESC, id DESC);

CREATE TABLE business.variant (
  id                        uuid PRIMARY KEY,
  cake_id                   uuid NOT NULL REFERENCES business.cake (id),
  flavour_id                uuid NOT NULL REFERENCES business.flavour (id),
  -- GK-002 sizes (single tiers and tiered "from" sizes). Servings and price
  -- are per recipe version, entered by the chef.
  size_code                 text NOT NULL CHECK (size_code IN (
                              'mini', 's', 'm', 'l', 'xl', 'xxl',
                              'tiers_4_7', 'tiers_6_8', 'tiers_8_10', 'tiers_4_6_8', 'tiers_6_9_11')),
  stock_mode                text NOT NULL CHECK (stock_mode IN ('made_to_order', 'ready_stock')),
  current_recipe_version_id uuid NULL,
  created_at                timestamptz NOT NULL DEFAULT now(),
  archived_at               timestamptz NULL,
  version                   integer NOT NULL DEFAULT 1 CHECK (version >= 1)
);

CREATE UNIQUE INDEX variant_active_idx ON business.variant (cake_id, flavour_id, size_code, stock_mode) WHERE archived_at IS NULL;
CREATE INDEX variant_cake_idx ON business.variant (cake_id, created_at, id);

CREATE TABLE business.recipe_version (
  id                 uuid PRIMARY KEY,
  variant_id         uuid NOT NULL REFERENCES business.variant (id),
  revision           integer NOT NULL CHECK (revision >= 1),
  -- What was bought, copied from the variant and flavour at creation, so the
  -- snapshot never depends on live rows.
  flavour_name       text NOT NULL,
  size_code          text NOT NULL,
  stock_mode         text NOT NULL,
  price_minor        bigint NOT NULL CHECK (price_minor BETWEEN 1 AND 10000000),
  servings_min       integer NOT NULL CHECK (servings_min BETWEEN 1 AND 500),
  servings_max       integer NOT NULL,
  coating            text NOT NULL CHECK (coating = btrim(coating) AND char_length(coating) BETWEEN 1 AND 200 AND coating !~ '[[:cntrl:]]'),
  composition        text[] NOT NULL CHECK (cardinality(composition) BETWEEN 1 AND 20),
  allergen_notice_id uuid NOT NULL REFERENCES business.allergen_notice (id),
  -- SHA-256 of the canonical version content, computed by the API.
  source_sha256      char(64) NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
  created_at         timestamptz NOT NULL DEFAULT now(),
  created_by         uuid NOT NULL REFERENCES business.account (id),
  CONSTRAINT recipe_version_servings CHECK (servings_max BETWEEN servings_min AND 500),
  UNIQUE (variant_id, revision),
  UNIQUE (id, variant_id)
);

-- A variant's current version must be one of its own.
ALTER TABLE business.variant
  ADD CONSTRAINT variant_current_recipe FOREIGN KEY (current_recipe_version_id, id)
  REFERENCES business.recipe_version (id, variant_id);

-- The "contains" declaration: UK FIC Annex II allergen codes with the exact
-- label the chef approved (for example "CEREALS CONTAINING GLUTEN (WHEAT)").
CREATE TABLE business.recipe_allergen (
  recipe_version_id uuid NOT NULL REFERENCES business.recipe_version (id),
  code              text NOT NULL CHECK (code IN (
                      'celery', 'cereals_containing_gluten', 'crustaceans', 'eggs', 'fish', 'lupin',
                      'milk', 'molluscs', 'mustard', 'tree_nuts', 'peanuts', 'sesame', 'soybeans', 'sulphites')),
  label             text NOT NULL CHECK (label = btrim(label) AND char_length(label) BETWEEN 1 AND 80 AND label !~ '[[:cntrl:]]'),
  prominent         boolean NOT NULL DEFAULT false,
  PRIMARY KEY (recipe_version_id, code)
);

-- Planned recipe quantities (K07): exact decimals at 0.01 g/ml, whole counts.
-- Linking lines to inventory items and batches comes with GK-032.
CREATE TABLE business.recipe_line (
  recipe_version_id uuid NOT NULL REFERENCES business.recipe_version (id),
  position          integer NOT NULL CHECK (position BETWEEN 1 AND 100),
  component         text NOT NULL CHECK (component = btrim(component) AND char_length(component) BETWEEN 1 AND 120
                                       AND component !~ '[[:cntrl:]]'),
  quantity          numeric(14,2) NOT NULL CHECK (quantity > 0),
  unit              text NOT NULL CHECK (unit IN ('g', 'ml', 'count')),
  CONSTRAINT recipe_line_whole_count CHECK (unit <> 'count' OR quantity = trunc(quantity)),
  PRIMARY KEY (recipe_version_id, position)
);

-- Purchased snapshots and the notice are immutable, even for the owner role
-- outside migrations.
CREATE FUNCTION business.catalog_immutable()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME USING ERRCODE = 'GK409';
END;
$$;

CREATE TRIGGER recipe_version_immutable BEFORE UPDATE OR DELETE ON business.recipe_version
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER recipe_allergen_immutable BEFORE UPDATE OR DELETE ON business.recipe_allergen
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER recipe_line_immutable BEFORE UPDATE OR DELETE ON business.recipe_line
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER allergen_notice_immutable BEFORE UPDATE OR DELETE ON business.allergen_notice
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER flavour_no_delete BEFORE DELETE ON business.flavour
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
-- Row triggers do not fire on TRUNCATE.
CREATE TRIGGER catalog_no_truncate_recipe_version BEFORE TRUNCATE ON business.recipe_version
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER catalog_no_truncate_recipe_allergen BEFORE TRUNCATE ON business.recipe_allergen
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER catalog_no_truncate_recipe_line BEFORE TRUNCATE ON business.recipe_line
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER catalog_no_truncate_allergen_notice BEFORE TRUNCATE ON business.allergen_notice
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER catalog_no_truncate_flavour BEFORE TRUNCATE ON business.flavour
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER catalog_no_truncate_cake BEFORE TRUNCATE ON business.cake
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER catalog_no_truncate_variant BEFORE TRUNCATE ON business.variant
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- A variant's identity (which cake, flavour, size and stock mode) and a
-- flavour's name never change: archive and create a new one instead.
CREATE FUNCTION business.catalog_variant_frozen()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF (NEW.id, NEW.cake_id, NEW.flavour_id, NEW.size_code, NEW.stock_mode)
     IS DISTINCT FROM (OLD.id, OLD.cake_id, OLD.flavour_id, OLD.size_code, OLD.stock_mode) THEN
    RAISE EXCEPTION 'variant identity is immutable' USING ERRCODE = 'GK409';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION business.catalog_flavour_frozen()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF (NEW.id, NEW.name) IS DISTINCT FROM (OLD.id, OLD.name) THEN
    RAISE EXCEPTION 'flavour name is immutable' USING ERRCODE = 'GK409';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER variant_identity_frozen BEFORE UPDATE ON business.variant
  FOR EACH ROW EXECUTE FUNCTION business.catalog_variant_frozen();
CREATE TRIGGER flavour_name_frozen BEFORE UPDATE ON business.flavour
  FOR EACH ROW EXECUTE FUNCTION business.catalog_flavour_frozen();
CREATE TRIGGER cake_no_delete BEFORE DELETE ON business.cake
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER variant_no_delete BEFORE DELETE ON business.variant
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: the account of an enabled staff member holding one of
-- p_capabilities. The account and capability rows are share-locked, so a
-- concurrent revocation or disabling waits for this transaction (or this
-- one sees it).
CREATE FUNCTION business.catalog_staff_account(p_subject uuid, p_capabilities text[])
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
BEGIN
  SELECT a.id INTO v_account
  FROM business.account a
  JOIN business.staff_capability c ON c.account_id = a.id
  WHERE a.provider_subject = p_subject AND a.disabled_at IS NULL
    AND c.capability = ANY (p_capabilities) AND c.revoked_at IS NULL
  ORDER BY c.capability
  LIMIT 1
  FOR SHARE OF a, c;
  IF v_account IS NULL THEN
    RAISE EXCEPTION 'this catalog operation requires an active % capability', pg_catalog.array_to_string(p_capabilities, ' or ')
      USING ERRCODE = 'GK403';
  END IF;
  RETURN v_account;
END;
$$;

-- Owner-internal: claim a command key. Returns the original result when the
-- same key was used with the same body, NULL for a new command (the caller
-- then performs it and calls command_finish). A concurrent first use waits
-- on the primary key until the other transaction ends.
CREATE FUNCTION business.command_claim(p_account uuid, p_command text, p_resource text, p_key text, p_body_sha256 text)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_hash text;
  v_result uuid;
BEGIN
  IF p_key IS NULL OR p_key !~ '^[\x21-\x7e]{1,128}$' THEN
    RAISE EXCEPTION 'idempotency_key_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_body_sha256 IS NULL OR p_body_sha256 !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'body hash must be 64 lowercase hex characters' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  INSERT INTO business.command_receipt (account_id, command, resource, idempotency_key, body_sha256)
  VALUES (p_account, p_command, p_resource, p_key, p_body_sha256)
  ON CONFLICT DO NOTHING;
  IF FOUND THEN
    RETURN NULL;
  END IF;
  SELECT r.body_sha256, r.result_id INTO STRICT v_hash, v_result
  FROM business.command_receipt r
  WHERE r.account_id = p_account AND r.command = p_command AND r.resource = p_resource AND r.idempotency_key = p_key;
  IF v_hash <> p_body_sha256 THEN
    RAISE EXCEPTION 'key_reused' USING ERRCODE = 'GK409';
  END IF;
  IF v_result IS NULL THEN
    RAISE EXCEPTION 'command receipt without a result' USING ERRCODE = 'internal_error';
  END IF;
  RETURN v_result;
END;
$$;

CREATE FUNCTION business.command_finish(p_account uuid, p_command text, p_resource text, p_key text, p_result uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  UPDATE business.command_receipt r SET result_id = p_result
  WHERE r.account_id = p_account AND r.command = p_command AND r.resource = p_resource
    AND r.idempotency_key = p_key AND r.result_id IS NULL;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'command receipt missing or already finished' USING ERRCODE = 'internal_error';
  END IF;
END;
$$;

-- Owner-internal: refuse a write when the caller's version is not current.
CREATE FUNCTION business.catalog_expect_version(p_current integer, p_expected integer)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_expected IS NULL OR p_expected < 1 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_current <> p_expected THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
END;
$$;

-- ---------------------------------------------------------------- commands
-- Each returns (result_id, replayed). A replay re-checks the actor's
-- capability first, so a revoked chef cannot replay an earlier command.

CREATE FUNCTION business.catalog_flavour_create(p_actor uuid, p_key text, p_body_sha256 text, p_name text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.flavour_create', '', p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  BEGIN
    INSERT INTO business.flavour (id, name, created_by) VALUES (v_id, p_name, v_account);
  EXCEPTION
    WHEN unique_violation THEN RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
    WHEN check_violation OR not_null_violation THEN RAISE EXCEPTION 'name_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.flavour_created', 'flavour', v_id, NULL, NULL);
  PERFORM business.command_finish(v_account, 'catalog.flavour_create', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

CREATE FUNCTION business.catalog_notice_create(p_actor uuid, p_key text, p_body_sha256 text, p_body text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.notice_create', '', p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  BEGIN
    INSERT INTO business.allergen_notice (id, body, created_by) VALUES (v_id, p_body, v_account);
  EXCEPTION WHEN check_violation OR not_null_violation THEN
    RAISE EXCEPTION 'body_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.allergen_notice_created', 'allergen_notice', v_id, NULL, NULL);
  PERFORM business.command_finish(v_account, 'catalog.notice_create', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

CREATE FUNCTION business.catalog_cake_create(p_actor uuid, p_key text, p_body_sha256 text, p_name text, p_description text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.cake_create', '', p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  BEGIN
    INSERT INTO business.cake (id, name, description) VALUES (v_id, p_name, coalesce(p_description, ''));
  EXCEPTION WHEN check_violation OR not_null_violation THEN
    RAISE EXCEPTION 'cake_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.cake_created', 'cake', v_id, NULL, NULL);
  PERFORM business.command_finish(v_account, 'catalog.cake_create', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- NULL name or description leaves that field unchanged.
CREATE FUNCTION business.catalog_cake_update(p_actor uuid, p_key text, p_body_sha256 text, p_cake uuid,
                                             p_expected_version integer, p_name text, p_description text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.cake_update', coalesce(p_cake::text, ''), p_key, p_body_sha256);
  v_version integer;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_name IS NULL AND p_description IS NULL THEN
    RAISE EXCEPTION 'cake_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT c.version INTO v_version FROM business.cake c WHERE c.id = p_cake FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'cake not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  BEGIN
    UPDATE business.cake c
    SET name = coalesce(p_name, c.name), description = coalesce(p_description, c.description),
        version = c.version + 1, updated_at = pg_catalog.now()
    WHERE c.id = p_cake;
  EXCEPTION WHEN check_violation THEN
    RAISE EXCEPTION 'cake_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.cake_updated', 'cake', p_cake, NULL,
                                pg_catalog.json_build_object('version', v_version + 1));
  PERFORM business.command_finish(v_account, 'catalog.cake_update', coalesce(p_cake::text, ''), p_key, p_cake);
  RETURN QUERY SELECT p_cake, false;
END;
$$;

-- draft -> active, active -> archived, archived -> active, draft -> archived.
CREATE FUNCTION business.catalog_cake_set_state(p_actor uuid, p_key text, p_body_sha256 text, p_cake uuid,
                                                p_expected_version integer, p_state text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.cake_set_state', coalesce(p_cake::text, ''), p_key, p_body_sha256);
  v_version integer;
  v_state text;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_state IS NULL OR p_state NOT IN ('active', 'archived') THEN
    RAISE EXCEPTION 'state_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT c.version, c.state INTO v_version, v_state FROM business.cake c WHERE c.id = p_cake FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'cake not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  IF v_state = p_state THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.cake c
  SET state = p_state, published_at = coalesce(c.published_at, CASE WHEN p_state = 'active' THEN pg_catalog.now() END),
      version = c.version + 1, updated_at = pg_catalog.now()
  WHERE c.id = p_cake;
  PERFORM business.audit_record('staff', v_account, 'catalog.cake_state_changed', 'cake', p_cake, NULL,
                                pg_catalog.json_build_object('from', v_state, 'to', p_state, 'version', v_version + 1));
  PERFORM business.command_finish(v_account, 'catalog.cake_set_state', coalesce(p_cake::text, ''), p_key, p_cake);
  RETURN QUERY SELECT p_cake, false;
END;
$$;

CREATE FUNCTION business.catalog_variant_create(p_actor uuid, p_key text, p_body_sha256 text, p_cake uuid,
                                                p_flavour uuid, p_size_code text, p_stock_mode text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.variant_create', coalesce(p_cake::text, ''), p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
  v_state text;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT c.state INTO v_state FROM business.cake c WHERE c.id = p_cake FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'cake not found' USING ERRCODE = 'GK404';
  END IF;
  IF v_state = 'archived' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  PERFORM 1 FROM business.flavour f WHERE f.id = p_flavour AND f.archived_at IS NULL FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'flavour_unknown' USING ERRCODE = 'GK422';
  END IF;
  BEGIN
    INSERT INTO business.variant (id, cake_id, flavour_id, size_code, stock_mode)
    VALUES (v_id, p_cake, p_flavour, p_size_code, p_stock_mode);
  EXCEPTION
    WHEN unique_violation THEN RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
    WHEN check_violation OR not_null_violation THEN RAISE EXCEPTION 'variant_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.variant_created', 'variant', v_id, NULL,
                                pg_catalog.json_build_object('cake', p_cake));
  PERFORM business.command_finish(v_account, 'catalog.variant_create', coalesce(p_cake::text, ''), p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

CREATE FUNCTION business.catalog_variant_archive(p_actor uuid, p_key text, p_body_sha256 text, p_variant uuid,
                                                 p_expected_version integer)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.variant_archive', coalesce(p_variant::text, ''), p_key, p_body_sha256);
  v_version integer;
  v_archived timestamptz;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT v.version, v.archived_at INTO v_version, v_archived FROM business.variant v WHERE v.id = p_variant FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'variant not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  IF v_archived IS NOT NULL THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.variant v SET archived_at = pg_catalog.now(), version = v.version + 1 WHERE v.id = p_variant;
  PERFORM business.audit_record('staff', v_account, 'catalog.variant_archived', 'variant', p_variant, NULL,
                                pg_catalog.json_build_object('version', v_version + 1));
  PERFORM business.command_finish(v_account, 'catalog.variant_archive', coalesce(p_variant::text, ''), p_key, p_variant);
  RETURN QUERY SELECT p_variant, false;
END;
$$;

-- Creates the next immutable recipe version of a variant and makes it
-- current. p_allergens: [{"code","label","prominent"}], non-empty, unique
-- codes. p_lines: [{"component","quantity" (decimal string),"unit"}].
CREATE FUNCTION business.catalog_recipe_version_create(
  p_actor uuid, p_key text, p_body_sha256 text, p_variant uuid, p_expected_version integer,
  p_price_minor bigint, p_servings_min integer, p_servings_max integer, p_coating text,
  p_composition text[], p_notice uuid, p_allergens json, p_lines json, p_source_sha256 text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.recipe_version_create', coalesce(p_variant::text, ''), p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
  v_version integer;
  v_archived timestamptz;
  v_revision integer;
  v_item text;
  v_flavour text;
  v_size text;
  v_stock text;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT v.version, v.archived_at, f.name, v.size_code, v.stock_mode
  INTO v_version, v_archived, v_flavour, v_size, v_stock
  FROM business.variant v JOIN business.flavour f ON f.id = v.flavour_id
  WHERE v.id = p_variant
  FOR UPDATE OF v;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'variant not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  IF v_archived IS NOT NULL THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  PERFORM 1 FROM business.allergen_notice n WHERE n.id = p_notice;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'notice_unknown' USING ERRCODE = 'GK422';
  END IF;
  IF p_allergens IS NULL OR pg_catalog.json_typeof(p_allergens) <> 'array' OR pg_catalog.json_array_length(p_allergens) = 0 THEN
    RAISE EXCEPTION 'allergens_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_lines IS NULL OR pg_catalog.json_typeof(p_lines) <> 'array' OR pg_catalog.json_array_length(p_lines) > 100 THEN
    RAISE EXCEPTION 'lines_invalid' USING ERRCODE = 'GK422';
  END IF;
  -- Every element must be an object of the expected JSON types (the API
  -- sends exactly that; a bypassing caller gets GK422, not a cast error).
  IF EXISTS (SELECT 1 FROM pg_catalog.json_array_elements(p_allergens) e
             WHERE pg_catalog.json_typeof(e) <> 'object'
                OR pg_catalog.json_typeof(e -> 'code') IS DISTINCT FROM 'string'
                OR pg_catalog.json_typeof(e -> 'label') IS DISTINCT FROM 'string'
                OR coalesce(pg_catalog.json_typeof(e -> 'prominent'), 'boolean') <> 'boolean') THEN
    RAISE EXCEPTION 'allergens_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF EXISTS (SELECT 1 FROM pg_catalog.json_array_elements(p_lines) e
             WHERE pg_catalog.json_typeof(e) <> 'object'
                OR pg_catalog.json_typeof(e -> 'component') IS DISTINCT FROM 'string'
                OR pg_catalog.json_typeof(e -> 'quantity') IS DISTINCT FROM 'string'
                OR pg_catalog.json_typeof(e -> 'unit') IS DISTINCT FROM 'string') THEN
    RAISE EXCEPTION 'lines_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_composition IS NULL OR coalesce(pg_catalog.array_ndims(p_composition), 0) <> 1
     OR pg_catalog.array_position(p_composition, NULL) IS NOT NULL THEN
    RAISE EXCEPTION 'composition_invalid' USING ERRCODE = 'GK422';
  END IF;
  FOREACH v_item IN ARRAY p_composition LOOP
    IF v_item <> pg_catalog.btrim(v_item) OR pg_catalog.char_length(v_item) NOT BETWEEN 1 AND 120
       OR v_item ~ '[[:cntrl:]]' THEN
      RAISE EXCEPTION 'composition_invalid' USING ERRCODE = 'GK422';
    END IF;
  END LOOP;

  -- Quantities are given with at most two decimals; the cast would round more.
  -- A missing quantity is NULL and fails the NOT NULL check below.
  IF EXISTS (SELECT 1 FROM pg_catalog.json_array_elements(p_lines) e
             WHERE (e ->> 'quantity') !~ '^[0-9]{1,12}(\.[0-9]{1,2})?$') THEN
    RAISE EXCEPTION 'recipe_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT coalesce(max(r.revision), 0) + 1 INTO v_revision FROM business.recipe_version r WHERE r.variant_id = p_variant;
  BEGIN
    INSERT INTO business.recipe_version (id, variant_id, revision, flavour_name, size_code, stock_mode, price_minor,
                                         servings_min, servings_max, coating, composition, allergen_notice_id,
                                         source_sha256, created_by)
    VALUES (v_id, p_variant, v_revision, v_flavour, v_size, v_stock, p_price_minor,
            p_servings_min, p_servings_max, p_coating, p_composition, p_notice, p_source_sha256, v_account);
    INSERT INTO business.recipe_allergen (recipe_version_id, code, label, prominent)
    SELECT v_id, a.value ->> 'code', a.value ->> 'label', coalesce((a.value ->> 'prominent')::boolean, false)
    FROM pg_catalog.json_array_elements(p_allergens) AS a(value);
    INSERT INTO business.recipe_line (recipe_version_id, position, component, quantity, unit)
    SELECT v_id, e.ord::integer, e.value ->> 'component', (e.value ->> 'quantity')::numeric(14,2), e.value ->> 'unit'
    FROM pg_catalog.json_array_elements(p_lines) WITH ORDINALITY AS e(value, ord);
  EXCEPTION
    WHEN unique_violation THEN RAISE EXCEPTION 'allergens_duplicate' USING ERRCODE = 'GK422';
    WHEN check_violation OR not_null_violation OR invalid_text_representation OR numeric_value_out_of_range
         OR array_subscript_error OR invalid_parameter_value THEN
      RAISE EXCEPTION 'recipe_invalid' USING ERRCODE = 'GK422';
  END;
  UPDATE business.variant v SET current_recipe_version_id = v_id, version = v.version + 1 WHERE v.id = p_variant;
  PERFORM business.audit_record('staff', v_account, 'catalog.recipe_version_created', 'variant', p_variant, NULL,
                                pg_catalog.json_build_object('recipe_version', v_id, 'revision', v_revision));
  PERFORM business.command_finish(v_account, 'catalog.recipe_version_create', coalesce(p_variant::text, ''), p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- ------------------------------------------------------------------- reads

-- Public: a page of active cakes, newest first, after the keyset cursor
-- (published_at, id) when given.
CREATE FUNCTION business.catalog_public_cake_page(p_after_published timestamptz, p_after_id uuid, p_limit integer)
RETURNS TABLE (cake_id uuid, cake_name text, cake_description text, cake_published_at timestamptz)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT c.id, c.name, c.description, c.published_at
  FROM business.cake c
  WHERE c.state = 'active'
    -- No OR: the first page uses sentinels, so the planner can always seek
    -- on cake_public_page_idx (a generic plan cannot for "IS NULL OR ...").
    AND (c.published_at, c.id) < (coalesce(p_after_published, 'infinity'::timestamptz),
                                  coalesce(p_after_id, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
  ORDER BY c.published_at DESC, c.id DESC
  LIMIT least(greatest(coalesce(p_limit, 20), 1), 51);
$$;

CREATE FUNCTION business.catalog_public_cake(p_cake uuid)
RETURNS TABLE (cake_id uuid, cake_name text, cake_description text, cake_published_at timestamptz)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT c.id, c.name, c.description, c.published_at
  FROM business.cake c
  WHERE c.id = p_cake AND c.state = 'active';
$$;

-- Variant rows with their current (or, for staff, any) recipe version.
-- Internal helper shared by the public and staff reads.
CREATE FUNCTION business.catalog_variant_rows(p_cakes uuid[], p_include_archived boolean)
RETURNS TABLE (variant_id uuid, variant_cake_id uuid, variant_flavour text, variant_size_code text,
               variant_stock_mode text, variant_archived_at timestamptz, variant_version integer,
               recipe_version_id uuid, recipe_revision integer, recipe_price_minor bigint,
               recipe_servings_min integer, recipe_servings_max integer, recipe_coating text,
               recipe_composition text[], recipe_allergens json, recipe_notice_id uuid, recipe_notice text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT v.id, v.cake_id, f.name, v.size_code, v.stock_mode, v.archived_at, v.version,
         r.id, r.revision, r.price_minor, r.servings_min, r.servings_max, r.coating, r.composition,
         (SELECT pg_catalog.json_agg(pg_catalog.json_build_object('code', a.code, 'label', a.label, 'prominent', a.prominent)
                                     ORDER BY a.prominent DESC, a.code)
          FROM business.recipe_allergen a WHERE a.recipe_version_id = r.id),
         n.id, n.body
  FROM business.variant v
  JOIN business.flavour f ON f.id = v.flavour_id
  LEFT JOIN business.recipe_version r ON r.id = v.current_recipe_version_id
  LEFT JOIN business.allergen_notice n ON n.id = r.allergen_notice_id
  WHERE v.cake_id = ANY (p_cakes) AND (p_include_archived OR v.archived_at IS NULL)
  ORDER BY v.cake_id, v.created_at, v.id;
$$;

-- Public: active variants of the given cakes, only where the cake is active.
CREATE FUNCTION business.catalog_public_variants(p_cakes uuid[])
RETURNS TABLE (variant_id uuid, variant_cake_id uuid, variant_flavour text, variant_size_code text,
               variant_stock_mode text, variant_archived_at timestamptz, variant_version integer,
               recipe_version_id uuid, recipe_revision integer, recipe_price_minor bigint,
               recipe_servings_min integer, recipe_servings_max integer, recipe_coating text,
               recipe_composition text[], recipe_allergens json, recipe_notice_id uuid, recipe_notice text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT r.* FROM business.catalog_variant_rows(
    ARRAY(SELECT c.id FROM business.cake c WHERE c.id = ANY (p_cakes) AND c.state = 'active'), false) r;
$$;

-- Staff (chef, technical support): any cake page, newest created first.
CREATE FUNCTION business.catalog_staff_cake_page(p_actor uuid, p_after_created timestamptz, p_after_id uuid, p_limit integer)
RETURNS TABLE (cake_id uuid, cake_name text, cake_description text, cake_state text, cake_published_at timestamptz,
               cake_created_at timestamptz, cake_updated_at timestamptz, cake_version integer)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY
  SELECT c.id, c.name, c.description, c.state, c.published_at, c.created_at, c.updated_at, c.version
  FROM business.cake c
  WHERE (c.created_at, c.id) < (coalesce(p_after_created, 'infinity'::timestamptz),
                                coalesce(p_after_id, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
  ORDER BY c.created_at DESC, c.id DESC
  LIMIT least(greatest(coalesce(p_limit, 20), 1), 51);
END;
$$;

CREATE FUNCTION business.catalog_staff_cake(p_actor uuid, p_cake uuid)
RETURNS TABLE (cake_id uuid, cake_name text, cake_description text, cake_state text, cake_published_at timestamptz,
               cake_created_at timestamptz, cake_updated_at timestamptz, cake_version integer)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY
  SELECT c.id, c.name, c.description, c.state, c.published_at, c.created_at, c.updated_at, c.version
  FROM business.cake c WHERE c.id = p_cake;
END;
$$;

CREATE FUNCTION business.catalog_staff_variants(p_actor uuid, p_cakes uuid[])
RETURNS TABLE (variant_id uuid, variant_cake_id uuid, variant_flavour text, variant_size_code text,
               variant_stock_mode text, variant_archived_at timestamptz, variant_version integer,
               recipe_version_id uuid, recipe_revision integer, recipe_price_minor bigint,
               recipe_servings_min integer, recipe_servings_max integer, recipe_coating text,
               recipe_composition text[], recipe_allergens json, recipe_notice_id uuid, recipe_notice text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY SELECT r.* FROM business.catalog_variant_rows(p_cakes, true) r;
END;
$$;

-- Staff: one immutable recipe version (the purchase snapshot an order pins).
CREATE FUNCTION business.catalog_staff_recipe_version(p_actor uuid, p_recipe_version uuid)
RETURNS TABLE (recipe_version_id uuid, recipe_variant_id uuid, recipe_cake_id uuid, recipe_flavour text,
               recipe_size_code text, recipe_revision integer, recipe_price_minor bigint,
               recipe_servings_min integer, recipe_servings_max integer, recipe_coating text,
               recipe_composition text[], recipe_allergens json, recipe_lines json, recipe_notice_id uuid,
               recipe_notice text, recipe_source_sha256 text, recipe_created_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY
  SELECT r.id, r.variant_id, v.cake_id, r.flavour_name, r.size_code, r.revision, r.price_minor, r.servings_min,
         r.servings_max, r.coating, r.composition,
         (SELECT pg_catalog.json_agg(pg_catalog.json_build_object('code', a.code, 'label', a.label, 'prominent', a.prominent)
                                     ORDER BY a.prominent DESC, a.code)
          FROM business.recipe_allergen a WHERE a.recipe_version_id = r.id),
         coalesce((SELECT pg_catalog.json_agg(pg_catalog.json_build_object('component', l.component,
                                                'quantity', l.quantity::text, 'unit', l.unit) ORDER BY l.position)
                   FROM business.recipe_line l WHERE l.recipe_version_id = r.id), '[]'::json),
         n.id, n.body, r.source_sha256::text, r.created_at
  FROM business.recipe_version r
  JOIN business.variant v ON v.id = r.variant_id
  JOIN business.allergen_notice n ON n.id = r.allergen_notice_id
  WHERE r.id = p_recipe_version;
END;
$$;

CREATE FUNCTION business.catalog_staff_flavours(p_actor uuid)
RETURNS TABLE (flavour_id uuid, flavour_name text, flavour_created_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY
  SELECT f.id, f.name, f.created_at FROM business.flavour f WHERE f.archived_at IS NULL ORDER BY lower(f.name), f.id;
END;
$$;

CREATE FUNCTION business.catalog_staff_notice_current(p_actor uuid)
RETURNS TABLE (notice_id uuid, notice_body text, notice_created_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY
  SELECT n.id, n.body, n.created_at FROM business.allergen_notice n ORDER BY n.created_at DESC, n.id DESC LIMIT 1;
END;
$$;

REVOKE ALL ON TABLE business.command_receipt, business.flavour, business.allergen_notice, business.cake,
  business.variant, business.recipe_version, business.recipe_allergen, business.recipe_line FROM PUBLIC;

REVOKE ALL ON FUNCTION business.catalog_immutable() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_variant_frozen() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_flavour_frozen() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_account(uuid, text[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.command_claim(uuid, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.command_finish(uuid, text, text, text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_expect_version(integer, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_flavour_create(uuid, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_notice_create(uuid, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_cake_create(uuid, text, text, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_cake_update(uuid, text, text, uuid, integer, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_cake_set_state(uuid, text, text, uuid, integer, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_variant_create(uuid, text, text, uuid, uuid, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_variant_archive(uuid, text, text, uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_recipe_version_create(uuid, text, text, uuid, integer, bigint, integer, integer,
  text, text[], uuid, json, json, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_public_cake_page(timestamptz, uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_public_cake(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_variant_rows(uuid[], boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_public_variants(uuid[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_cake_page(uuid, timestamptz, uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_cake(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_variants(uuid, uuid[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_recipe_version(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_flavours(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_notice_current(uuid) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.catalog_flavour_create(uuid, text, text, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_notice_create(uuid, text, text, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_cake_create(uuid, text, text, text, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_cake_update(uuid, text, text, uuid, integer, text, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_cake_set_state(uuid, text, text, uuid, integer, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_variant_create(uuid, text, text, uuid, uuid, text, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_variant_archive(uuid, text, text, uuid, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_recipe_version_create(uuid, text, text, uuid, integer, bigint, integer, integer,
  text, text[], uuid, json, json, text) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_public_cake_page(timestamptz, uuid, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_public_cake(uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_public_variants(uuid[]) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_staff_cake_page(uuid, timestamptz, uuid, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_staff_cake(uuid, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_staff_variants(uuid, uuid[]) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_staff_recipe_version(uuid, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_staff_flavours(uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_staff_notice_current(uuid) TO gk_api;
