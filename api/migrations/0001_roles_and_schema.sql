-- GK-020: private business schema and least-privilege role groups (K00, K09).
--
-- Group roles never log in. Per-environment login users are provisioned outside
-- migrations (no passwords in Git) and granted exactly one runtime group:
--   gk_api    API runtime          gk_worker  maintenance/job runtime
--   gk_backup read-only backup     gk_owner   owns business objects; never granted to runtime
-- The migration credential may SET ROLE gk_owner; runtime credentials may not.

DO $$
DECLARE
  group_role text;
BEGIN
  FOREACH group_role IN ARRAY ARRAY['gk_owner', 'gk_api', 'gk_worker', 'gk_backup'] LOOP
    BEGIN
      EXECUTE pg_catalog.format('CREATE ROLE %I NOLOGIN', group_role);
    EXCEPTION WHEN duplicate_object OR unique_violation THEN
      NULL; -- roles are cluster-wide and may already exist
    END;
  END LOOP;

  IF EXISTS (
    SELECT 1 FROM pg_catalog.pg_roles
    WHERE rolname IN ('gk_owner', 'gk_api', 'gk_worker', 'gk_backup')
      AND (rolcanlogin OR rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls)
  ) THEN
    RAISE EXCEPTION 'gk group roles must be NOLOGIN roles without elevated attributes';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM pg_catalog.pg_auth_members m
    JOIN pg_catalog.pg_roles granted ON granted.oid = m.roleid
    JOIN pg_catalog.pg_roles member ON member.oid = m.member
    WHERE granted.rolname = 'gk_owner' AND member.rolname IN ('gk_api', 'gk_worker', 'gk_backup')
  ) THEN
    RAISE EXCEPTION 'runtime and backup groups must not be members of gk_owner';
  END IF;
END
$$;

GRANT gk_owner TO CURRENT_USER WITH INHERIT FALSE, SET TRUE;

CREATE SCHEMA IF NOT EXISTS business AUTHORIZATION gk_owner;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

SET LOCAL ROLE gk_owner;

REVOKE ALL ON SCHEMA business FROM PUBLIC;
GRANT USAGE ON SCHEMA business TO gk_api, gk_worker, gk_backup;

ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA business GRANT SELECT ON TABLES TO gk_backup;

-- Supabase Data API roles get nothing in the private schema.
DO $$
DECLARE
  api_role text;
BEGIN
  FOREACH api_role IN ARRAY ARRAY['anon', 'authenticated'] LOOP
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = api_role) THEN
      EXECUTE pg_catalog.format('REVOKE ALL ON SCHEMA business FROM %I', api_role);
    END IF;
  END LOOP;
END
$$;
