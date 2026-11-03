# GK Cakes API

Go modular backend: HTTP API (`gk-api`), maintenance/job worker (`gk-worker`) and
migration command (`gk-migrate`), released together as one artifact and deployed
separately from the UI.

## Toolchain

- Go `1.27.3` (`go.mod`: `go 1.27.0`, `toolchain go1.27.3`).
- PostgreSQL 17 for integration tests: `postgres:17.11-alpine@sha256:266d3c257594e195b309ae9d1805e54c788f1d7b375f56bf6c367aac6c917e7b`.
- `govulncheck` `v1.9.0`.

## Commands

Run from `api/`:

| Purpose                     | Command                                                                     |
| --------------------------- | --------------------------------------------------------------------------- |
| Formatting                  | `test -z "$(gofmt -l .)"`                                                   |
| Vet (unit and integration)  | `go vet ./... && go vet -tags=integration ./...`                            |
| Unit tests                  | `go test ./...`                                                             |
| Race tests                  | `go test -race ./...`                                                       |
| Real PostgreSQL tests       | `go test -race -tags=integration -p 1 -count=1 ./...` with `GK_TEST_DATABASE_URL` |
| Vulnerability check         | `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`                     |
| Build                       | `CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/...`                        |

Integration tests need a disposable local cluster; they refuse non-loopback hosts
and create and drop their own databases and roles:

```sh
docker run -d --name gk-pg -e POSTGRES_PASSWORD=gk_disposable -p 127.0.0.1:55432:5432 \
  postgres:17.11-alpine@sha256:266d3c257594e195b309ae9d1805e54c788f1d7b375f56bf6c367aac6c917e7b
export GK_TEST_DATABASE_URL='postgres://postgres:gk_disposable@127.0.0.1:55432/postgres?sslmode=disable'
```

`-p 1` keeps packages from creating cluster-wide roles concurrently.

## Configuration

Every process validates its environment at start and exits with status 2 on any
problem; messages name variables, never values. Database URLs are checked as the
pgx client will use them: any non-local host needs `sslmode=verify-full` (no
plaintext or unverified fallback, including multi-host and `host=` overrides),
and staging/production reject local hosts and Unix sockets.

| Variable                 | Process    | Rule                                                                                        |
| ------------------------ | ---------- | ------------------------------------------------------------------------------------------- |
| `APP_ENV`                | all        | `local`, `test`, `staging` or `production`                                                  |
| `DATABASE_URL`           | api, worker | required secret; each process has its own login; add `sslrootcert=<path>` when the provider's CA is not in the system trust store |
| `MIGRATION_DATABASE_URL` | gk-migrate | required secret, migration credential; never falls back to `DATABASE_URL`                   |
| `UI_ORIGIN`              | api        | required exact origin for CORS; staging/production need public `https`                     |
| `PORT`                   | api        | default `8080`                                                                              |
| `SHUTDOWN_TIMEOUT`       | api, worker | drain budget, default `20s`                                                                |
| `PAYMENT_SECRET`         | api        | optional until its adapter exists; live keys only in production, test keys only elsewhere   |
| `PAYMENT_WEBHOOK_SECRET`, `EMAIL_API_KEY`, `EMAIL_WEBHOOK_SECRET`, `STORAGE_WRITE_KEY` | api | optional until their adapters exist; redacted |

Not consumed yet: `AUTH_*` issuer/audience/JWKS settings (GK-021) and B2 recovery
settings (GK-024). They are added, with validation, by the tickets that use them.

API endpoints: `GET /healthz` (liveness), `GET /readyz` (database reachable).

### Secret inventory

Values never enter Git, Trello, logs or the UI. Owner for every entry: Creator
(Sasha), who holds the provider accounts. Staging is configured (GK-034);
production stores wait for the first authorized production release (GK-036).

| Secret                   | Scope                               | Storage location                                  | Rotation                                                        | Revocation                                   |
| ------------------------ | ----------------------------------- | ------------------------------------------------- | --------------------------------------------------------------- | -------------------------------------------- |
| `DATABASE_URL` (api)     | `gk_api` login, one per environment | API service variables (Railway, per environment; staging login `gk_api_staging`; seal the variable) | `ALTER ROLE <login> PASSWORD`, update the variable, deploy the staged change | `ALTER ROLE <login> NOLOGIN` or drop it      |
| `DATABASE_URL` (worker)  | `gk_worker` login, one per environment | worker/cron service variables                  | as above                                                        | as above                                     |
| `MIGRATION_DATABASE_URL` | migration credential                | GitHub environment secret, staging: `STAGING_MIGRATION_DATABASE_URL` | rotate the password of the same role (see below)                | rotate; runtime services never hold it       |
| `RAILWAY_STAGING_TOKEN`  | Railway project token, staging environment only | GitHub environment `staging` secret (deployable from `main` only) | regenerate in Railway, update the secret                  | delete the token in Railway                  |
| Provider secrets         | per provider account and environment | API service variables                            | provider dashboard, update the variable, redeploy               | revoke the key in the provider dashboard     |

The migration credential should stay one stable role (on Supabase, `postgres`)
and be rotated by password. A different migration role would also need
`GRANT gk_owner TO <role> WITH INHERIT FALSE, SET TRUE` and ownership of the
`gk_meta` schema before it could migrate.

## Database roles

Migrations create the private `business` schema and NOLOGIN groups. Login users
are provisioned per environment outside Git and granted exactly one group:

| Group       | Purpose                       | Privileges                                                  |
| ----------- | ----------------------------- | ----------------------------------------------------------- |
| `gk_owner`  | owns business objects         | never granted to runtime; migration credential may `SET ROLE` |
| `gk_api`    | API runtime                   | schema `USAGE`; gateway functions as features add them      |
| `gk_worker` | worker runtime                | `SELECT` on `business.job`; `EXECUTE` on job lease functions |
| `gk_backup` | backup reader                 | `SELECT` on business tables                                 |

The migration credential needs `CREATEROLE` and `CREATE` on the database.
Runtime roles cannot write protected tables, run DDL, `TRUNCATE` or `SET ROLE gk_owner`;
the integration suite proves this with real logins. Migration 0001 refuses to run
if a group already inherits `gk_owner` or a predefined data role.

After provisioning a login, check that it holds exactly its one group and no
other privileged role (expect a single row, e.g. `gk_api`):

```sql
SELECT r.rolname FROM pg_roles r
WHERE pg_has_role('<login>', r.oid, 'MEMBER') AND r.rolname <> '<login>';

-- expect every column false
SELECT rolsuper, rolcreaterole, rolcreatedb, rolreplication, rolbypassrls
FROM pg_roles WHERE rolname = '<login>';
```

`gk-migrate` applies files once, in order, under an advisory lock. Applied files
are immutable (checksummed). An older build on a newer database (a rollback)
checks that its own migrations are all applied unchanged and that every newer
version sorts after its own, then applies nothing and exits 0 ("database is ahead
of this build"); any other mismatch is refused as diverged histories. Migrations
are forward-only: never add down migrations, and keep each one compatible with the
previous release (expand now, contract only once no supported release needs the
old shape).

## Releases and deployment (GK-034)

UI and API are built once for each `main` commit that is still the head of
`main` when its CI passes, as `ghcr.io/chichasasha/gk-cakes-{api,ui}:sha-<commit>`
(labelled `org.opencontainers.image.revision=<commit>`), and deployed by digest;
nothing is rebuilt at deploy time. Commits superseded before their CI finished
get no image and cannot be released. Workflows: `.github/workflows/staging.yml` and
`release.yml`.

- **Staging, automatic.** A green Components run on a push to `main` publishes
  (or reuses) both images, runs the compatibility matrix, and deploys them while
  that commit is still the head of `main`: API first (migrate once, then Railway
  behind `/readyz`), then UI (behind `/healthz`). Each deploy ends with `.github/scripts/smoke.sh` against the
  public URLs and is recorded as a GitHub deployment
  (`staging-api` / `staging-ui`, payload `version` and `artifact_digest`; task
  `deploy` for main deploys, `deploy:release` for releases).
- **Release.** Publish a GitHub release tagged `api-vX.Y.Z` or `ui-vX.Y.Z` on a
  `main` commit. The gate (`gk-release-gate`) deploys only that component and only
  if the tag points at a commit on `main` with a successful main CI run, the
  registry image for that commit exists and carries that commit in its revision
  label, the same release is not already deployed successfully (replay: skip) and
  no release record of a higher version of that component exists in the target,
  whatever its status, since a failed attempt may still be live (downgrade: deny).
  A **prerelease** goes to staging, where the verify-release job repeats the
  artifact, replay and downgrade checks; it runs in the `deploy-staging`
  concurrency group, so it is ordered with every staging deploy. A **full
  release** targets production, which stays disabled until GK-036; its deploy job
  must repeat these checks inside its own serialized section.
- **Queued runs.** GitHub keeps one pending run per concurrency group: a queued
  staging run can be superseded by a newer one before it starts (never while it
  runs, so a migration is not cancelled). A superseded release run fails and can
  be re-run.
- **Rollback.** Publish a new, higher tag pointing at the older commit (for
  example `ui-v1.4.1` at the commit of `ui-v1.3.0`). Lower tags are refused. An API
  rollback keeps the newer schema and data: the older `gk-migrate` applies nothing.
  Builds from before this behaviour (staging deploys up to `c24c709`, including
  `api-v0.0.1`/`api-v0.0.2`) still refuse a newer schema, so they are not
  rollback targets once a migration has been added; while staging runs one, the
  compatibility matrix blocks deploying a new migration.
- **Compatibility matrix (AC2).** `.github/scripts/compat-matrix.sh` runs the
  deployed and candidate images against a disposable PostgreSQL, migrated by a
  non-superuser database owner as on Supabase, in deploy order:
  1. deployed `gk-migrate`, runtime logins, a seeded job, deployed API ready;
  2. candidate `gk-migrate`: business tables keep their rows, and the business
     schema keeps every column (type, NOT NULL), function (signature, SECURITY
     DEFINER) and runtime-group grant it had; a run that applies nothing must
     change nothing; the deployed API stays ready;
  3. candidate API ready on the result;
  4. deployed `gk-migrate` again: succeeds and changes nothing (rollback);
  5. both API builds allow only the UI origin (CORS);
  6. both `gk-worker` builds claim and release a job through `job_claim` /
     `job_release` without errors;
  7. both UI builds serve `/healthz` and the home page.

  In a rollback the candidate is the older build, so the same steps check the
  other direction. It runs in staging (`compatibility-matrix`, before any
  deploy; both deploy jobs require it) and on pull requests (`compat` in
  `components.yml`, the PR's images against what staging runs). Images are
  resolved by `.github/scripts/resolve-images.sh`. Limits:
  - "Deployed" is the newest staging record with a successful status; a deploy
    marked failed while live is not the baseline. The lookup fails closed when
    records exist but none of the newest 100 succeeded.
  - A release of one component needs a successful record of the other.
  - API readiness is a database ping, and the UI calls no API endpoint yet:
    features that add UI→API calls or new runtime database use must extend the
    script's checks.
  - Removing or narrowing anything in the business schema fails the matrix, so
    the first contract migration must add a reviewed exception mechanism.

`gk-release-gate` exit status: `0` deploy, skip or verified; `3` denied, not
verified or a downgrade; `4` verified but already deployed; `1` error.

Limits of the record-based checks:

- Records created before release records had their own task (the `api-v0.0.1`
  and `ui-v0.0.1` staging test prereleases) are not considered: re-running those
  two releases would deploy them again, and `*-v0.0.0` would pass.
- A mistaken high tag (say `ui-v10.0.0`) that gets recorded forces every later
  release of that component above it. The only other way out is deleting that
  deployment record (mark it inactive, then delete it; needs `deployments:write`).
- Each environment can hold fewer than 5000 release records (50 pages); at
  5000 the checks fail closed until old records are deleted.

Staging configuration (all nonsecret unless noted):

| Name | Where | Purpose |
| --- | --- | --- |
| `STAGING_API_ENABLED` | repository variable | `true` enables API deploys (migrations run first) |
| `RAILWAY_ENVIRONMENT_ID`, `RAILWAY_API_SERVICE_ID`, `RAILWAY_UI_SERVICE_ID` | `staging` environment variables | deploy targets |
| `RAILWAY_PROJECT_ID` | `staging` environment variable | reference only; not read by the workflows |
| `STAGING_API_URL`, `STAGING_UI_URL` | `staging` environment variables | public smoke check, `https://<host>` without a trailing slash |
| `RAILWAY_STAGING_TOKEN`, `STAGING_MIGRATION_DATABASE_URL` | `staging` environment secrets | see the secret inventory |

Railway and Cloudflare notes from the first staging setup:

- Railway variable edits are staged until **Deploy** is pressed on the
  staged-changes banner (Option-click commits without a Railway redeploy). The
  pipeline deploys only committed settings, so an unapplied variable is missing
  at runtime.
- A custom domain added before the service's first deploy may target the wrong
  port; check it under Settings → Networking after the first deploy.
- The staging hostnames are proxied by Cloudflare; Railway needs the zone's
  SSL/TLS mode set to **Full** (not Flexible, not Full (Strict)).

## Verified on the Supabase sandbox (staging, 2026-11-03)

- TLS `verify-full` through the IPv4 session pooler with
  `sslrootcert=/etc/gk/supabase-root-2021.crt` (bundled in the image), for both
  the migration credential and the `gk_api_staging` runtime login.
- Migrations `0001` and `0002` applied, which also confirms PostgreSQL 16 or later
  (`GRANT ... WITH INHERIT FALSE, SET TRUE`).

## Not yet verified

- Runtime-role denial and the gateway on the sandbox. They are proven on
  PostgreSQL 17.11 only.
- Whether Supabase's pooler forwards the `statement_timeout`/`lock_timeout`
  startup parameters (direct connections do).
