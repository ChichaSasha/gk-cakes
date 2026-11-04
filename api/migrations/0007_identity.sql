-- GK-021 A1: identity (contracts r6.5 K00). Supabase authenticates; this
-- database decides who an authenticated subject is here and what they may do.
--
-- An account is the local identity of one verified provider subject (the
-- access token's sub). It is created on first use by identity_resolve, which
-- the API calls only after verifying the token. Staff capabilities are granted
-- locally and never derived from provider metadata: chef manages the business,
-- fulfillment prepares and hands over orders, technical_support gets bounded
-- audited access (K00). A capability is active until revoked; revocation keeps
-- the row as history. A disabled account can do nothing, whatever it holds.
--
-- Grants, revocations and account disabling are operator commands in this
-- milestone (owner-only functions run with the migration credential through
-- gk-identity-admin), used to bootstrap the first manager approved in GK-010
-- and to disable access. Every change is audited.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.account (
  id               uuid PRIMARY KEY,
  provider_subject uuid NOT NULL UNIQUE,
  created_at       timestamptz NOT NULL DEFAULT now(),
  disabled_at      timestamptz NULL,
  version          integer NOT NULL DEFAULT 1 CHECK (version >= 1)
);

CREATE TABLE business.staff_capability (
  id         uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES business.account (id),
  capability text NOT NULL CHECK (capability IN ('chef', 'fulfillment', 'technical_support')),
  granted_at timestamptz NOT NULL DEFAULT now(),
  -- NULL: granted by an operator command (bootstrap), not by a staff member.
  granted_by uuid NULL REFERENCES business.account (id),
  revoked_at timestamptz NULL,
  revoked_by uuid NULL REFERENCES business.account (id),
  CONSTRAINT staff_capability_revoker CHECK (revoked_at IS NOT NULL OR revoked_by IS NULL),
  CONSTRAINT staff_capability_order CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);

-- At most one active grant of a capability per account.
CREATE UNIQUE INDEX staff_capability_active_idx
  ON business.staff_capability (account_id, capability) WHERE revoked_at IS NULL;

-- Owner-internal: the account of a subject, created when missing. The insert
-- tolerates a concurrent first use of the same subject.
CREATE FUNCTION business.account_ensure(p_subject uuid, p_created_by_operator boolean)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid;
BEGIN
  IF p_subject IS NULL THEN
    RAISE EXCEPTION 'a provider subject is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT a.id INTO v_id FROM business.account a WHERE a.provider_subject = p_subject;
  IF FOUND THEN
    RETURN v_id;
  END IF;
  INSERT INTO business.account (id, provider_subject)
  VALUES (pg_catalog.gen_random_uuid(), p_subject)
  ON CONFLICT (provider_subject) DO NOTHING
  RETURNING id INTO v_id;
  IF v_id IS NULL THEN
    SELECT a.id INTO STRICT v_id FROM business.account a WHERE a.provider_subject = p_subject;
    RETURN v_id;
  END IF;
  IF p_created_by_operator THEN
    PERFORM business.audit_record('system', NULL, 'account.created', 'account', v_id, NULL,
                                  pg_catalog.json_build_object('via', 'operator'));
  ELSE
    PERFORM business.audit_record('customer', v_id, 'account.created', 'account', v_id, NULL, NULL);
  END IF;
  RETURN v_id;
END;
$$;

-- The API calls this for every authenticated request, after verifying the
-- access token: the subject's account (created on first use), whether it is
-- disabled, and its active staff capabilities in name order.
CREATE FUNCTION business.identity_resolve(p_subject uuid)
RETURNS TABLE (account_id uuid, account_disabled boolean, account_capabilities text[])
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id uuid := business.account_ensure(p_subject, false);
BEGIN
  RETURN QUERY
  SELECT a.id, a.disabled_at IS NOT NULL,
         coalesce((SELECT pg_catalog.array_agg(c.capability ORDER BY c.capability)
                   FROM business.staff_capability c
                   WHERE c.account_id = a.id AND c.revoked_at IS NULL), '{}'::text[])
  FROM business.account a
  WHERE a.id = v_id;
END;
$$;

-- Active staff and their capabilities, for a chef. The actor's own chef
-- capability is checked here as well as in the API (a revocation between the
-- two is caught). The database trusts the subject the API passes, which the
-- API has verified; this is not independent authentication. A refusal raises
-- GK403, so a missing grant (42501) is never mistaken for one.
CREATE FUNCTION business.identity_staff_list(p_actor_subject uuid)
RETURNS TABLE (staff_account_id uuid, staff_capability text, staff_granted_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM business.account a
    JOIN business.staff_capability c ON c.account_id = a.id
    WHERE a.provider_subject = p_actor_subject AND a.disabled_at IS NULL
      AND c.capability = 'chef' AND c.revoked_at IS NULL) THEN
    RAISE EXCEPTION 'listing staff requires an active chef capability' USING ERRCODE = 'GK403';
  END IF;
  RETURN QUERY
  SELECT c.account_id, c.capability, c.granted_at
  FROM business.staff_capability c
  JOIN business.account a ON a.id = c.account_id
  WHERE c.revoked_at IS NULL AND a.disabled_at IS NULL
  ORDER BY c.granted_at, c.account_id, c.capability;
END;
$$;

-- Operator commands (owner-only; gk-identity-admin). Each returns whether it
-- changed anything, so a repeated command is harmless and visible as such.
CREATE FUNCTION business.staff_grant(p_subject uuid, p_capability text, p_note text)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
  v_id uuid;
BEGIN
  IF p_capability IS NULL OR p_capability NOT IN ('chef', 'fulfillment', 'technical_support') THEN
    RAISE EXCEPTION 'unknown staff capability' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF p_note IS NULL OR length(p_note) NOT BETWEEN 1 AND 200 THEN
    RAISE EXCEPTION 'a note of 1 to 200 characters is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  v_account := business.account_ensure(p_subject, true);
  INSERT INTO business.staff_capability (id, account_id, capability)
  VALUES (pg_catalog.gen_random_uuid(), v_account, p_capability)
  ON CONFLICT (account_id, capability) WHERE revoked_at IS NULL DO NOTHING
  RETURNING id INTO v_id;
  IF v_id IS NULL THEN
    RETURN false;
  END IF;
  PERFORM business.audit_record('system', NULL, 'staff.capability_granted', 'account', v_account, NULL,
                                pg_catalog.json_build_object('capability', p_capability, 'via', 'operator', 'note', p_note));
  RETURN true;
END;
$$;

CREATE FUNCTION business.staff_revoke(p_subject uuid, p_capability text, p_note text)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
BEGIN
  IF p_note IS NULL OR length(p_note) NOT BETWEEN 1 AND 200 THEN
    RAISE EXCEPTION 'a note of 1 to 200 characters is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.staff_capability c
  SET revoked_at = GREATEST(pg_catalog.now(), c.granted_at)
  FROM business.account a
  WHERE a.id = c.account_id AND a.provider_subject = p_subject
    AND c.capability = p_capability AND c.revoked_at IS NULL
  RETURNING c.account_id INTO v_account;
  IF v_account IS NULL THEN
    RETURN false;
  END IF;
  PERFORM business.audit_record('system', NULL, 'staff.capability_revoked', 'account', v_account, NULL,
                                pg_catalog.json_build_object('capability', p_capability, 'via', 'operator', 'note', p_note));
  RETURN true;
END;
$$;

CREATE FUNCTION business.account_set_disabled(p_subject uuid, p_disabled boolean, p_note text)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid;
BEGIN
  IF p_disabled IS NULL THEN
    RAISE EXCEPTION 'disabled must be true or false' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF p_note IS NULL OR length(p_note) NOT BETWEEN 1 AND 200 THEN
    RAISE EXCEPTION 'a note of 1 to 200 characters is required' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- A subject can be disabled before its first sign-in: the account is
  -- created (audited as an operator action) and disabled.
  v_account := business.account_ensure(p_subject, true);
  UPDATE business.account a
  SET disabled_at = CASE WHEN p_disabled THEN pg_catalog.now() END, version = a.version + 1
  WHERE a.id = v_account AND (a.disabled_at IS NOT NULL) <> p_disabled;
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  PERFORM business.audit_record('system', NULL,
                                CASE WHEN p_disabled THEN 'account.disabled' ELSE 'account.enabled' END,
                                'account', v_account, NULL,
                                pg_catalog.json_build_object('via', 'operator', 'note', p_note));
  RETURN true;
END;
$$;

REVOKE ALL ON TABLE business.account FROM PUBLIC;
REVOKE ALL ON TABLE business.staff_capability FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_ensure(uuid, boolean) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.identity_resolve(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.identity_staff_list(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.staff_grant(uuid, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.staff_revoke(uuid, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.account_set_disabled(uuid, boolean, text) FROM PUBLIC;

-- The API resolves verified subjects and lists staff for a chef; it cannot
-- grant, revoke or disable.
GRANT EXECUTE ON FUNCTION business.identity_resolve(uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.identity_staff_list(uuid) TO gk_api;
