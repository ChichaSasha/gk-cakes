-- GK-034 Milestone B: each database backup records the schema version it
-- belongs to, so a restore can bring a fresh database to exactly that
-- version before the data goes in. The backup login (gk_backup) may read the
-- migration history; it still cannot change anything. gk_meta belongs to the
-- migration credential, which runs this file before any role change.

GRANT USAGE ON SCHEMA gk_meta TO gk_backup;
GRANT SELECT ON TABLE gk_meta.schema_migration TO gk_backup;

-- Future sequences (an identity or serial column) stay readable by the
-- backup login, as tables already are (0001), so pg_dump never fails on one.
SET LOCAL ROLE gk_owner;
ALTER DEFAULT PRIVILEGES IN SCHEMA business GRANT SELECT ON SEQUENCES TO gk_backup;
