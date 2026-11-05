-- GK-028 shop colour filter: a chef-managed colour list and one optional
-- colour per cake (contracts r6.5 K01 amendment 2026-11-05; source: the
-- Creator-approved GK-016 r01c sketch, shop page "Colour" filter).
--
-- A colour is public catalogue content, never personal data: a code (a slug
-- the shop filter uses, unique among all colours including archived ones and
-- immutable once created), a label the chef chooses (1 to 40 characters,
-- trimmed, single line) and a position (the filter order, 0 to 10000).
-- Colours are archived, never deleted; archiving is reversible. A cake has at
-- most one colour (cake.colour_id). Customers see a cake's colour only while
-- the colour is active: an archived colour reads as no colour. Nothing is
-- seeded: the sketch's starting list (Pink, Silver, Red, Chocolate, White) is
-- the chef's to create.
--
-- Every change is a chef command with an Idempotency-Key (K00), audited, and
-- an existing colour needs its expected version, like the rest of the catalog
-- (0009_catalog.sql). Refusals use the catalog's SQLSTATEs (GK403, GK404,
-- GK409 with the code as message, GK422).
--
-- Expand-only (K09): a new table, a nullable column, new functions (the cake
-- create/update overloads take the colour; the existing functions stay, so a
-- deployed build keeps working and leaves the colour unchanged) and grants.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.colour (
  id          uuid PRIMARY KEY,
  -- 'all' is the shop filter's "All colours" value.
  code        text NOT NULL CHECK (code ~ '^[a-z0-9][a-z0-9-]{0,39}$' AND code <> 'all'),
  -- Trimmed of every Unicode space (and U+FEFF) the shop's own check trims;
  -- no control characters or line/paragraph separators (as 0018).
  label       text NOT NULL CHECK (pg_catalog.char_length(label) BETWEEN 1 AND 40
                                   AND label !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'
                                   AND label !~ '^[ \u00a0\u1680\u2000-\u200a\u202f\u205f\u3000\ufeff]'
                                   AND label !~ '[ \u00a0\u1680\u2000-\u200a\u202f\u205f\u3000\ufeff]$'),
  position    integer NOT NULL CHECK (position BETWEEN 0 AND 10000),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid NOT NULL REFERENCES business.account (id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  archived_at timestamptz NULL,
  version     integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  -- Unique among all colours, archived ones included: a code never changes
  -- meaning.
  CONSTRAINT colour_code_unique UNIQUE (code)
);

ALTER TABLE business.cake ADD COLUMN colour_id uuid NULL REFERENCES business.colour (id);

CREATE INDEX cake_colour_idx ON business.cake (colour_id) WHERE colour_id IS NOT NULL;

-- A colour's code never changes; colours are never deleted or truncated.
CREATE FUNCTION business.catalog_colour_frozen()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF (NEW.id, NEW.code) IS DISTINCT FROM (OLD.id, OLD.code) THEN
    RAISE EXCEPTION 'colour code is immutable' USING ERRCODE = 'GK409';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER colour_code_frozen BEFORE UPDATE ON business.colour
  FOR EACH ROW EXECUTE FUNCTION business.catalog_colour_frozen();
CREATE TRIGGER colour_no_delete BEFORE DELETE ON business.colour
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER catalog_no_truncate_colour BEFORE TRUNCATE ON business.colour
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: refuse an unknown or archived colour for a cake. The row
-- is share-locked so a concurrent archive waits for this transaction.
CREATE FUNCTION business.catalog_colour_usable(p_colour uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM 1 FROM business.colour k WHERE k.id = p_colour AND k.archived_at IS NULL FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'colour_unknown' USING ERRCODE = 'GK422';
  END IF;
END;
$$;

-- Owner-internal: the input rules of the table, checked first so a refusal
-- names its field.
CREATE FUNCTION business.catalog_colour_check(p_code text, p_label text, p_position integer)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_code IS NOT NULL AND (p_code !~ '^[a-z0-9][a-z0-9-]{0,39}$' OR p_code = 'all') THEN
    RAISE EXCEPTION 'code_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_label IS NOT NULL AND (pg_catalog.char_length(p_label) NOT BETWEEN 1 AND 40
                              OR p_label ~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]'
                              OR p_label ~ '^[ \u00a0\u1680\u2000-\u200a\u202f\u205f\u3000\ufeff]'
                              OR p_label ~ '[ \u00a0\u1680\u2000-\u200a\u202f\u205f\u3000\ufeff]$') THEN
    RAISE EXCEPTION 'label_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_position IS NOT NULL AND p_position NOT BETWEEN 0 AND 10000 THEN
    RAISE EXCEPTION 'position_invalid' USING ERRCODE = 'GK422';
  END IF;
END;
$$;

-- ---------------------------------------------------------------- commands

CREATE FUNCTION business.catalog_colour_create(p_actor uuid, p_key text, p_body_sha256 text, p_code text,
                                               p_label text, p_position integer)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.colour_create', '', p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_code IS NULL THEN
    RAISE EXCEPTION 'code_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_label IS NULL THEN
    RAISE EXCEPTION 'label_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_position IS NULL THEN
    RAISE EXCEPTION 'position_invalid' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.catalog_colour_check(p_code, p_label, p_position);
  BEGIN
    INSERT INTO business.colour (id, code, label, position, created_by) VALUES (v_id, p_code, p_label, p_position, v_account);
  EXCEPTION
    WHEN unique_violation THEN RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
    WHEN check_violation OR not_null_violation THEN RAISE EXCEPTION 'colour_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.colour_created', 'colour', v_id, NULL,
                                pg_catalog.json_build_object('code', p_code));
  PERFORM business.command_finish(v_account, 'catalog.colour_create', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- NULL label, position or archived leaves that field unchanged. archived
-- true archives (an archived colour keeps its first archive time), false
-- restores.
CREATE FUNCTION business.catalog_colour_update(p_actor uuid, p_key text, p_body_sha256 text, p_colour uuid,
                                               p_expected_version integer, p_label text, p_position integer,
                                               p_archived boolean)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.colour_update', coalesce(p_colour::text, ''), p_key, p_body_sha256);
  v_version integer;
  v_archived timestamptz;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_label IS NULL AND p_position IS NULL AND p_archived IS NULL THEN
    RAISE EXCEPTION 'colour_invalid' USING ERRCODE = 'GK422';
  END IF;
  PERFORM business.catalog_colour_check(NULL, p_label, p_position);
  SELECT k.version INTO v_version FROM business.colour k WHERE k.id = p_colour FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'colour not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  BEGIN
    UPDATE business.colour k
    SET label = coalesce(p_label, k.label), position = coalesce(p_position, k.position),
        archived_at = CASE WHEN p_archived IS NULL THEN k.archived_at
                           WHEN p_archived THEN coalesce(k.archived_at, pg_catalog.now()) END,
        version = k.version + 1, updated_at = pg_catalog.now()
    WHERE k.id = p_colour
    RETURNING k.archived_at INTO v_archived;
  EXCEPTION WHEN check_violation THEN
    RAISE EXCEPTION 'colour_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.colour_updated', 'colour', p_colour, NULL,
                                pg_catalog.json_build_object('version', v_version + 1, 'archived', v_archived IS NOT NULL));
  PERFORM business.command_finish(v_account, 'catalog.colour_update', coalesce(p_colour::text, ''), p_key, p_colour);
  RETURN QUERY SELECT p_colour, false;
END;
$$;

-- catalog_cake_create with a colour (NULL: none). Same command identity as
-- the 0009 function.
CREATE FUNCTION business.catalog_cake_create(p_actor uuid, p_key text, p_body_sha256 text, p_name text,
                                             p_description text, p_colour uuid)
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
  IF p_colour IS NOT NULL THEN
    PERFORM business.catalog_colour_usable(p_colour);
  END IF;
  BEGIN
    INSERT INTO business.cake (id, name, description, colour_id) VALUES (v_id, p_name, coalesce(p_description, ''), p_colour);
  EXCEPTION WHEN check_violation OR not_null_violation THEN
    RAISE EXCEPTION 'cake_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.cake_created', 'cake', v_id, NULL,
                                CASE WHEN p_colour IS NOT NULL THEN pg_catalog.json_build_object('colour', p_colour) END);
  PERFORM business.command_finish(v_account, 'catalog.cake_create', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- catalog_cake_update with a colour: p_set_colour true sets the colour to
-- p_colour (NULL clears it); false leaves it unchanged. NULL name or
-- description leaves that field unchanged. Same command identity as the 0009
-- function.
CREATE FUNCTION business.catalog_cake_update(p_actor uuid, p_key text, p_body_sha256 text, p_cake uuid,
                                             p_expected_version integer, p_name text, p_description text,
                                             p_set_colour boolean, p_colour uuid)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'catalog.cake_update', coalesce(p_cake::text, ''), p_key, p_body_sha256);
  v_version integer;
  v_set boolean := coalesce(p_set_colour, false);
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_name IS NULL AND p_description IS NULL AND NOT v_set THEN
    RAISE EXCEPTION 'cake_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT c.version INTO v_version FROM business.cake c WHERE c.id = p_cake FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'cake not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  IF v_set AND p_colour IS NOT NULL THEN
    PERFORM business.catalog_colour_usable(p_colour);
  END IF;
  BEGIN
    UPDATE business.cake c
    SET name = coalesce(p_name, c.name), description = coalesce(p_description, c.description),
        colour_id = CASE WHEN v_set THEN p_colour ELSE c.colour_id END,
        version = c.version + 1, updated_at = pg_catalog.now()
    WHERE c.id = p_cake;
  EXCEPTION WHEN check_violation THEN
    RAISE EXCEPTION 'cake_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'catalog.cake_updated', 'cake', p_cake, NULL,
                                CASE WHEN v_set THEN pg_catalog.json_build_object('version', v_version + 1, 'colour', p_colour)
                                     ELSE pg_catalog.json_build_object('version', v_version + 1) END);
  PERFORM business.command_finish(v_account, 'catalog.cake_update', coalesce(p_cake::text, ''), p_key, p_cake);
  RETURN QUERY SELECT p_cake, false;
END;
$$;

-- ------------------------------------------------------------------- reads

-- Public: the colour of each given cake that is active and has an active
-- colour (no row: no colour).
CREATE FUNCTION business.catalog_public_cake_colours(p_cakes uuid[])
RETURNS TABLE (cake_id uuid, colour_code text, colour_label text, colour_position integer)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT c.id, k.code, k.label, k.position
  FROM business.cake c
  JOIN business.colour k ON k.id = c.colour_id
  WHERE c.id = ANY (p_cakes) AND c.state = 'active' AND k.archived_at IS NULL;
$$;

-- Staff (chef, technical support): the colour of each given cake, archived
-- colours included.
CREATE FUNCTION business.catalog_staff_cake_colours(p_actor uuid, p_cakes uuid[])
RETURNS TABLE (cake_id uuid, colour_id uuid, colour_code text, colour_label text, colour_position integer,
               colour_archived_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY
  SELECT c.id, k.id, k.code, k.label, k.position, k.archived_at
  FROM business.cake c
  JOIN business.colour k ON k.id = c.colour_id
  WHERE c.id = ANY (p_cakes);
END;
$$;

-- Staff: every colour (p_colour NULL) or one, archived included, in the
-- filter order.
CREATE FUNCTION business.catalog_staff_colours(p_actor uuid, p_colour uuid)
RETURNS TABLE (colour_id uuid, colour_code text, colour_label text, colour_position integer,
               colour_archived_at timestamptz, colour_version integer)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'technical_support']);
  RETURN QUERY
  SELECT k.id, k.code, k.label, k.position, k.archived_at, k.version
  FROM business.colour k
  WHERE p_colour IS NULL OR k.id = p_colour
  ORDER BY k.position, k.label, k.code;
END;
$$;

REVOKE ALL ON TABLE business.colour FROM PUBLIC;

REVOKE ALL ON FUNCTION business.catalog_colour_frozen() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_colour_usable(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_colour_check(text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_colour_create(uuid, text, text, text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_colour_update(uuid, text, text, uuid, integer, text, integer, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_cake_create(uuid, text, text, text, text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_cake_update(uuid, text, text, uuid, integer, text, text, boolean, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_public_cake_colours(uuid[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_cake_colours(uuid, uuid[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.catalog_staff_colours(uuid, uuid) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.catalog_colour_create(uuid, text, text, text, text, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_colour_update(uuid, text, text, uuid, integer, text, integer, boolean) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_cake_create(uuid, text, text, text, text, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_cake_update(uuid, text, text, uuid, integer, text, text, boolean, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_public_cake_colours(uuid[]) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_staff_cake_colours(uuid, uuid[]) TO gk_api;
GRANT EXECUTE ON FUNCTION business.catalog_staff_colours(uuid, uuid) TO gk_api;
