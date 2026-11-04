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
| `PAYMENT_WEBHOOK_SECRET`, `STORAGE_WRITE_KEY` | api | optional until their adapters exist; redacted |
| `EMAIL_WEBHOOK_SECRET`   | api        | Resend webhook signing secret (`whsec_` + base64), redacted; when set, the API serves `POST /webhooks/email`, otherwise that path is 404 |
| `AUTH_ISSUER`, `AUTH_PUBLISHABLE_KEY` | api | public identity-provider settings: the exact token issuer `https://<project>.supabase.co/auth/v1` and the publishable key `sb_publishable_...` (a secret key is refused); required in staging/production, otherwise both or neither; without them the authenticated `/api/v1` routes are not served, see [Identity](#identity-k00-gk-021) |
| `EMAIL_API_KEY`          | worker     | Resend sending key (`re_...`), redacted; see email dispatch below                          |
| `RECOVERY_B2_KEY_ID`, `RECOVERY_B2_KEY` | worker | B2 application key for the recovery journal (key secret redacted); scope enforced at start, see [Recovery journal](#recovery-journal-k11-gk-024) |
| `RECOVERY_ENCRYPTION_KEYS` | worker   | journal encryption keys `kid:base64(32 bytes)[,...]`, redacted                              |
| `BACKUP_DATABASE_URL`    | gk-backup dump | the `gk_backup` login (read only), same rules as `DATABASE_URL` |
| `RESTORE_DATABASE_URL`   | gk-backup restore-check | a disposable database on a loopback address only |
| `BACKUP_B2_KEY_ID`, `BACKUP_B2_KEY`, `BACKUP_ENCRYPTION_KEYS` | gk-backup | B2 key restricted to `database/` (list/read/write, no delete) and the backup encryption keys `kid:base64(32 bytes)[,...]`; see [Database backups](#database-backups-k11-gk-034) |
| `PG_DUMP`, `PG_RESTORE`, `BACKUP_MAX_AGE` | gk-backup | optional tool paths (default from `PATH`; version 17 or newer) and the restore check's maximum backup age (default `26h`) |

Email dispatch: the four worker variables above are required in staging and
production; in local and test they are set together or not at all, and a worker
without them sends no email (notification jobs are retried and then reach
attention). At start the worker checks the B2 key's scope and reachability and
exits with status 1 if the journal is not usable: no email is sent without it.
The API still accepts an `EMAIL_API_KEY` but does not use it.

API endpoints: `GET /healthz` (liveness), `GET /readyz` (database reachable and,
outside local, the login check), `POST /webhooks/email` (Resend events,
Svix-signed; only with `EMAIL_WEBHOOK_SECRET`), and with `AUTH_ISSUER` the
authenticated `GET /api/v1/me`, `GET /api/v1/admin/me` and
`GET /api/v1/admin/staff` (see [Identity](#identity-k00-gk-021)).

### Secret inventory

Values never enter Git, Trello, logs or the UI. Owner for every entry: Creator
(Sasha), who holds the provider accounts. Staging is configured (GK-034);
production stores wait for the first authorized production release (GK-036).

| Secret                   | Scope                               | Storage location                                  | Rotation                                                        | Revocation                                   |
| ------------------------ | ----------------------------------- | ------------------------------------------------- | --------------------------------------------------------------- | -------------------------------------------- |
| `DATABASE_URL` (api)     | `gk_api` login, one per environment | API service variables (Railway, per environment; staging login `gk_api_staging`; seal the variable) | `ALTER ROLE <login> PASSWORD`, update the variable, deploy the staged change | `ALTER ROLE <login> NOLOGIN` or drop it      |
| `DATABASE_URL` (worker)  | `gk_worker` login, one per environment | worker/cron service variables                  | as above                                                        | as above                                     |
| `EMAIL_API_KEY`          | Resend sending-only key restricted to `gkcake.com`, one per environment | worker service variables (sealed); Bitwarden | create a new key in Resend, update the variable, redeploy, delete the old key | delete the key in Resend |
| `RECOVERY_B2_KEY_ID`, `RECOVERY_B2_KEY` | B2 key: one bucket, prefix `recovery/`, list/read/write only (no delete) | worker service variables (sealed); Bitwarden | create a new key with the B2 CLI, update the variables, redeploy, delete the old key | delete the key in B2 |
| `EMAIL_WEBHOOK_SECRET`   | signing secret of the Resend webhook endpoint for this environment's API | API service variables (sealed) | replace the endpoint's secret in Resend, update the variable and redeploy promptly (deliveries signed with a secret the API does not have are refused and retried by Resend) | delete the webhook endpoint in Resend |
| `RECOVERY_ENCRYPTION_KEYS` | seals journal envelopes; one list per environment | worker service variables (sealed) **and** Bitwarden (envelopes are unreadable without it) | prepend a new `kid:key`, keep the old ones listed (they still open older envelopes) | never drop a key while envelopes sealed with it are retained |
| `BACKUP_DATABASE_URL`    | `gk_backup` login, one per environment (staging `gk_backup_staging`) | GitHub environment secret, staging: `STAGING_BACKUP_DATABASE_URL` | `ALTER ROLE <login> PASSWORD`, update the secret | `ALTER ROLE <login> NOLOGIN` or drop it |
| `BACKUP_B2_KEY_ID`, `BACKUP_B2_KEY` | B2 key: backup bucket, prefix `database/`, list/read/write only (no delete) | GitHub environment secrets `STAGING_BACKUP_B2_KEY_ID`, `STAGING_BACKUP_B2_KEY`; Bitwarden | create a new key with the B2 CLI, update the secrets, delete the old key | delete the key in B2 |
| `BACKUP_ENCRYPTION_KEYS` | seals database backups; one list per environment | GitHub environment secret `STAGING_BACKUP_ENCRYPTION_KEYS` **and** Bitwarden (backups are unreadable without it) | prepend a new `kid:key`, keep the old ones for at least 31 days (30 visible + 1 hidden) | never drop a key while backups sealed with it are retained |
| `MIGRATION_DATABASE_URL` | migration credential (used by `gk-migrate` in deploys and by the manual Email proof and Identity admin workflows) | GitHub environment secret, staging: `STAGING_MIGRATION_DATABASE_URL` | rotate the password of the same role (see below)                | rotate; runtime services never hold it       |
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
| `gk_api`    | API runtime                   | schema `USAGE`; `EXECUTE` on `email_event_record` (webhook evidence), `identity_resolve` and `identity_staff_list` (identity); more gateway functions as features add them |
| `gk_worker` | worker runtime                | `SELECT` on `business.job`; `EXECUTE` on job lease functions (`job_claim`, `job_heartbeat`, `job_complete`, `job_release`, `job_fail`, `job_attention`) and the lease-fenced notification gateway (`notification_dispatch_begin`, `notification_attempt_record`, `notification_recovery_*`) |
| `gk_backup` | backup reader                 | `SELECT` on business tables and future sequences, and on `gk_meta.schema_migration` (migration 0008) |

The migration credential needs `CREATEROLE` and `CREATE` on the database.
Runtime roles cannot write protected tables, run DDL, `TRUNCATE` or `SET ROLE gk_owner`;
the integration suite proves this with real logins. Migration 0001 refuses to run
if a group already inherits `gk_owner` or a predefined data role.

After provisioning a login, check that it holds exactly its one group, inherits
its privileges, holds no other privileged role (expect a single row, e.g.
`gk_api | true`), and owns nothing and holds no direct grant:

```sql
SELECT r.rolname, pg_has_role('<login>', r.oid, 'USAGE') AS inherits FROM pg_roles r
WHERE pg_has_role('<login>', r.oid, 'MEMBER') AND r.rolname <> '<login>';

-- expect every column false
SELECT rolsuper, rolcreaterole, rolcreatedb, rolreplication, rolbypassrls
FROM pg_roles WHERE rolname = '<login>';

-- expect false (run in the service's database): the login owns nothing and
-- holds no privilege granted to it directly
SELECT EXISTS (SELECT 1 FROM pg_class WHERE relowner = r.oid)
    OR EXISTS (SELECT 1 FROM pg_namespace WHERE nspowner = r.oid)
    OR EXISTS (SELECT 1 FROM pg_proc WHERE proowner = r.oid)
    OR EXISTS (SELECT 1 FROM pg_class c, aclexplode(c.relacl) a WHERE a.grantee = r.oid)
    OR EXISTS (SELECT 1 FROM pg_attribute t, aclexplode(t.attacl) a WHERE a.grantee = r.oid)
    OR EXISTS (SELECT 1 FROM pg_namespace n, aclexplode(n.nspacl) a WHERE a.grantee = r.oid)
    OR EXISTS (SELECT 1 FROM pg_proc p, aclexplode(p.proacl) a WHERE a.grantee = r.oid)
    OR EXISTS (SELECT 1 FROM pg_database d, aclexplode(d.datacl) a
               WHERE d.datname = current_database() AND a.grantee = r.oid)
FROM pg_roles r WHERE r.rolname = '<login>';
```

The services repeat these checks themselves in every environment except
`local` (`database.CheckLogin`, on the session's login, `session_user`): the
login must hold and inherit exactly its group, have no elevated attribute and
not have switched role. It must also, in the service's database, own no
table, schema or function and hold no privilege granted to it directly (table,
column, schema, function or database): an owner bypasses grants and a direct
grant bypasses the group. The API's `/readyz`, which is its Railway health
check, stays 503 and logs `database login refused` until its login passes for
`gk_api`, so a deploy with an administrator, migration or other-service login
fails and the previous one keeps serving. The worker exits at start (code 2)
unless its login passes for `gk_worker`, and waits up to 30 seconds for an
unreachable database (code 1; a shutdown signal while waiting is a normal
stop). The worker has no fallback: once its new deployment reaches SUCCESS,
Railway has replaced the old one, so a wrong worker login stops email
dispatch until a fixed login is deployed (the deploy fails, which is the
signal). On staging (2026-11-03) the API had been running with a login other
than `gk_api_staging` and only failed once the webhook needed a granted
function. Both staging logins passed these checks on 2026-11-04.

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
- **Worker.** After a successful API deploy (main or release), `deploy-worker`
  deploys the same API image to the worker's own Railway service with the start
  command `/usr/local/bin/gk-worker` and no health check or public domain. It
  refuses a `RAILWAY_WORKER_SERVICE_ID` equal to the API or UI service (the
  start command persists on a service). It then watches for 90 seconds: the
  deployment must stay SUCCESS and keep running its first container. Railway
  restarts a crashed process under its restart policy without leaving SUCCESS,
  so the check counts starts in the newest lines of the deployment's own log:
  Railway's `Starting Container` lines and the worker's own first log line
  (`worker starting`, passed as `-start-line`). A second start by either count
  fails the deploy (the service runs one replica).
  Railway's `deploymentInstanceExecutions` listed nothing for the running
  staging worker, so it is not used. The worker exits at start when its email
  or recovery-journal settings or its database login are wrong. If the token
  cannot read the deployment log (after five consecutive errors, or on every
  read of the period), or the latest read failed or is inconclusive (no start
  line, the named `-start-line` missing, or a full page that may hide an
  earlier start), the check falls back to the status and prints a GitHub
  warning. On GitHub Actions every output line with provider text is escaped:
  line breaks and `%` are encoded, so it cannot end an annotation or start a
  `::command::` line, and the legacy `##[` form is broken up; step outputs
  accept only `[A-Za-z0-9_-]` values. It is off until
  `STAGING_WORKER_ENABLED` is `true` and is recorded as `staging-worker` (not
  read by the release gate).
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
     schema keeps every column (type, NOT NULL), column default, function
     (signature, SECURITY DEFINER) and effective runtime-group privilege it had
     (additions are fine); a run that applies nothing must change nothing; the
     deployed API stays ready;
  3. candidate API ready on the result;
  4. deployed `gk-migrate` again: succeeds and changes nothing (rollback);
  5. both API builds allow only the UI origin (CORS);
  6. both `gk-worker` builds claim and release a job freshly enqueued through
     `job_enqueue` on the result, via `job_claim` / `job_release`, without errors;
  7. both UI builds serve `/healthz` and the home page.

  In a rollback the candidate is the older build, so the same steps check the
  other direction. It runs in staging (`compatibility-matrix`, before any
  deploy; both deploy jobs require it) and on pull requests (`compat` in
  `components.yml`, the PR's images against what staging runs; not a required
  check yet). Images are resolved by `.github/scripts/resolve-images.sh`. Limits:
  - "Deployed" is the newest staging record with a successful status; a deploy
    marked failed while live is not the baseline. The lookup fails closed when
    records exist but none of the newest 100 succeeded; recover by deleting the
    stale failed records (mark them inactive, then delete them).
  - A release of one component needs a successful record of the other.
  - API readiness is a database ping, and the UI calls no API endpoint yet:
    features that add UI→API calls or new runtime database use must extend the
    script's checks.
  - Removing or changing any existing line of that schema contract fails the
    matrix (additions pass), so the first contract migration must add a
    reviewed exception mechanism.

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
| `STAGING_WORKER_ENABLED` | repository variable | `true` enables worker deploys and the email proof workflow |
| `RAILWAY_WORKER_SERVICE_ID` | `staging` environment variable | worker deploy target |
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

## Recovery journal (K11, GK-024)

`internal/recovery` implements the minimal confirmed-order recovery protocol
from [K11](../docs/design/contracts.md#k11--confirmed-order-recovery-contract)
for outgoing dispatches (email now; payments and refunds later). Accepted
order commands (confirmation, changes) are designed with GK-025 and are not
supported yet. Every dispatch runs through `Coordinator.Dispatch`:

1. reserve it in `business.recovery_operation`: one unresolved operation per
   aggregate (`ErrBusy`), prior versions only increase (`ErrStale`);
2. write the encrypted **prepared** envelope to B2 (reusing a copy already
   there) and wait for B2's checksum acknowledgment, then mark it `prepared`;
3. run the effect, only while outgoing effects are not frozen (checked again
   right before it); an error means the outcome is unknown and the operation
   stays `prepared`;
4. record the outcome (`committed`), write the encrypted **outcome**
   envelope, then mark it `secured`.

Rules for callers:

- The payload is the exact provider request (account, environment,
  idempotency key, from, reply-to, recipient, subject, rendered body). After a
  primary database loss it is the only copy, and the effect must be rebuilt
  from it alone: Resend rejects a repeated key with a different body.
- `ReplayUntil` is fixed at the first reservation, below the provider window
  (for Resend's 24 hours: first dispatch + at most 23 hours), and can never be
  extended. Until then a resumed dispatch repeats the identical request; from
  then on an unknown outcome goes to `reconcile`, never to a new send. An
  effect outcome that cannot be recorded also goes to `reconcile`.
- Use the dispatch's own intent (for example the notification intent) as the
  aggregate, not the order, so an unresolved email never blocks order work.
  A successor resend uses the next prior version after `Coordinator.Resolve`
  records the manager's outcome for the reconciling one.
- Store phases on an autocommit connection or pool (`NewSQLStore` refuses a
  transaction): no lock or transaction is held across B2 or provider calls.
  Serialize work on one operation with the job lease.

`Coordinator.Pending` lists prepared operations without an outcome from B2
alone, so after a primary database loss they can be resumed against a
restored database. A `reconcile` decision is not journalled: after a loss the
deadline check reproduces an expired one, and an operation sent to `reconcile`
because its outcome could not be recorded is attempted again with the same
identity (provider-idempotent within the window) and goes to `reconcile` again
if its outcome still cannot be recorded. `CopyBlob` stores an essential
attachment's bytes before its reference is committed.

Envelopes (format version 2) carry the payload as opaque bytes, so a replay
gets exactly the stored request back; reusing an existing prepared envelope
requires the same payload and replay deadline, so a resume can never extend the
provider window.

Objects live under `recovery/<APP_ENV>/` in the sandbox bucket
`gk-cakes-sandbox-backups-flbgymxjelzexgoxrcss`:
`op/<aggregate>/<prior version>-<operation>.{prepared,outcome}` and
`blob/<kid>-<keyed hash>`. Names contain only IDs, versions and keyed hashes.
Contents are AES-256-GCM sealed in the application and bound to their object
name, on top of B2's SSE-B2; the bucket keeps all versions, and the journal
never deletes.

Setup requirements (the code refuses any other key):

- **B2 application key:** restricted to that one bucket and the name prefix
  `recovery/` (or exactly `recovery/<APP_ENV>/`), with `listFiles`, `readFiles`
  and `writeFiles` plus at most read-only capabilities: no `deleteFiles`, bucket
  settings, retention or sharing. The B2 web console's access presets include
  delete, so create it with the B2 CLI, for example
  `b2 key create --bucket gk-cakes-sandbox-backups-flbgymxjelzexgoxrcss
  --name-prefix recovery/ gk-recovery-staging listFiles,readFiles,writeFiles`.
- **Encryption key:** `kid:base64(32 random bytes)`, for example `k1:` followed
  by `openssl rand -base64 32`. Keep it in Bitwarden as well as the service
  variables: without it the envelopes cannot be read. A comma-separated list
  rotates keys (the first seals, all open).

The tables and functions are owner-internal: domain gateway functions call
them in the same transaction as their own state change, and no runtime role
can call them directly (`gk_backup` may read the tables, as every business
table). Restore tooling (as `gk_owner`) freezes all outgoing effects with
`business.recovery_set_freeze(true, reason)`.

Not yet:

- B2 Object Lock retention, restore tooling and the restore drill (GK-034
  Milestone B, GK-035). The restore tooling must replay the journal before
  workers resume: a restored database can re-anchor a notice's window (the
  existing prepared envelope then refuses the resume), and an existing outcome
  envelope should be adopted rather than replayed;
- K09 retention for the journal: it keeps recipients and message bodies (and
  later essential photos) until deleted, and this key cannot delete. Keeping
  only current copies of still-retained records, 30-day restore points and
  deletion tombstones carried into replay belong to GK-034 Milestone B with
  the retention work.

## Transactional email (K08, GK-024)

`internal/notifications` sends transactional email through Resend from the
worker, protected by the recovery journal above. No marketing or bulk sending.

**Enqueue.** A domain gateway function calls the owner-internal
`business.notification_enqueue` inside the transaction that commits its business
transition, so the notification intent, its job and a `notification.queued`
audit event exist exactly when the transition does: a rollback leaves none of
them, and no enqueue happens after commit. The application builds the notice
with `notifications.NewNotice`: sender `GK Cakes <orders@gkcake.com>`, Reply-To
`gk.cake.london@gmail.com`, one bare recipient address, subject at most 200
characters without CR/LF, text body, optional HTML, and a Resend tag with the
intent ID. The exact payload (environment, idempotency key and request) is
stored once with its SHA-256; recipient and content are frozen, so later profile
changes never retarget it. One intent per transition, recipient and template
revision (a second is refused).

**Dispatch.** The worker claims the job and calls only the gateway functions.
Each one that changes state or admits a call checks the live lease (owner,
generation, unexpired; `GK001` otherwise) and that the recovery operation is
exactly the intent's (operation = idempotency key, aggregate = intent, payload
hash = stored payload). The first dispatch fixes `first_dispatch_at` and
`replay_until` (+23 hours, below Resend's 24-hour idempotency retention). The
prepared envelope is in B2 before the provider call. Right before each call,
`notification_dispatch_admit` re-checks the restore freeze, a revocation and the
window on the database clock; only then are the stored request bytes sent, in
canonical form, with the `Idempotency-Key` header (5 s timeout). Every call is
appended to `business.notification_attempt`, even when the worker's lease was
lost meanwhile (the call happened). A row is one call made by the worker: Go's
HTTP client may transparently resend a POST that carries an `Idempotency-Key`
when a reused keep-alive connection drops before any response, which Resend
deduplicates by key.

| Provider response                                     | Attempt     | Intent                     | Job                          |
| ----------------------------------------------------- | ----------- | -------------------------- | ---------------------------- |
| 2xx with message ID                                   | `accepted`  | `accepted` (message ID)    | completed                    |
| 400/422 with a content error name (`validation_error`, `missing_required_field`, ...) | `rejected` | `failed` (`unknown` after an unknown attempt) | attention (recipient did not get it; not retried) |
| 429, 409 concurrent request                           | `deferred`  | unchanged                  | retry                        |
| timeout, connection loss, 5xx, other                  | `unknown`   | `unknown`                  | retry (same key and body)    |
| 401/403/404/405; 400/422 invalid API key, invalid idempotency key or unrecognised | `refused` | unchanged | attention (fix, then requeue)|
| 409 key used with a different body                    | `conflict`  | `unknown`                  | attention                    |

Retries come after 1m, 5m, 15m and 1h (`business.job_fail`); the fifth
failure moves the job to attention for a manager, who can re-admit it with
`business.job_requeue` (owner tooling). An unknown outcome is replayed with the
original key until `replay_until`; after that it goes to `reconcile` and
attention and is never sent again blindly, requeued or not. A restore freeze
defers dispatch (5 minutes, not a failure) without starting the window. A
revoked recipient (`business.notification_suspend`, audited with the intent's
state at that moment) is never sent to, including replays of an unknown
dispatch and a revocation that lands while the dispatch is being prepared. A
payload of another environment or sender, or not in canonical form, is refused
before any call.

**Provider evidence (webhook).** Resend posts email events to
`POST /webhooks/email` (configure the endpoint `https://<api host>/webhooks/email`
in Resend with the events `email.sent`, `email.delivered`,
`email.delivery_delayed`, `email.complained`, `email.bounced`, `email.failed`,
`email.suppressed`). The API checks the Svix signature over the raw body
(`svix-id`, `svix-timestamp` within 5 minutes, `v1` HMAC-SHA256 with the
signing secret; 401 otherwise; bodies over 256 KiB are 413), then
`business.email_event_record` stores the event and applies it in one
transaction before the API answers 200; a storage failure is 503, so Resend
retries with the same `svix-id`, which deduplicates.

- Webhooks are account-wide: every request carries the tags `intent` and `env`,
  and the API stores only events tagged with its own `APP_ENV`. Untagged events
  (for example auth emails), opens, clicks and non-email events are acknowledged
  and ignored.
- Correlation is by provider message ID, or by the intent tag when the
  acceptance response was lost. A tag and message ID pointing at different
  intents or messages is a `conflict`; an unknown intent is `unmatched`
  (pending correlation). Neither changes an intent.
- Evidence only moves an intent forward: `sending`/`unknown` < `accepted`
  (`email.sent`) < `delivered_to_server` (`email.delivered`,
  `email.complained`) < `failed` (`email.failed`, `email.suppressed`) =
  `bounced` (`email.bounced`). A later, milder event never overwrites a
  bounce or failure, the worker's own acceptance never overwrites delivery
  evidence, and a queued intent is never changed. Every event is kept.
- The intent row is locked while an event is applied and the update repeats
  the rank check, so two different events for one email arriving together
  still apply in turn. A bounce or failure found this way is audited
  (`notification.adverse_evidence`, no personal data).
- Parsing is lenient beyond the event type: an unexpected field shape is
  dropped, not refused, so a signed delivery never fails forever (repeated
  failures would make Resend disable the endpoint). Only a body that is not a
  JSON object with a plausible `type` is 400.
- `business.email_event` keeps the event type, times, IDs, correlation, the
  applied state, bounce/failure/suppression categories and the payload hash;
  never recipients, subjects, diagnostic codes or provider messages.

The engine never claims inbox delivery or reading. Logs and stored evidence
carry IDs, HTTP status and the provider's error name only, never recipients,
content or provider error messages.

**Sandbox proof (AC3).** The manual workflow `.github/workflows/email-proof.yml`
(`staging` environment, `main` only, needs `STAGING_WORKER_ENABLED`) runs
`cmd/gk-email-proof`:
- with the migration credential, it enqueues controlled notices as a domain
  transaction would: to `delivered+<run>@resend.dev` and `bounced+<run>@resend.dev`,
  and optionally to the business mailbox for the display-name check;
- it waits for `delivered_to_server` and `bounced` evidence from the real
  worker and webhook;
- it checks that `/webhooks/email` refuses unsigned, forged and stale (401) and
  oversized (413) deliveries, none of them stored, and matches each refusal to
  the reason the API logged (`missing or malformed signature headers`,
  `no matching signature`, `timestamp outside the tolerance`, and the access
  log's 413), so the stale probe proves the timestamp check, not just a bad
  signature;
- it scans the staging environment's logs (Railway, the window since the run
  started) for the recipients, their local parts, the subject and the run
  label (which is also in the body). This run's dispatch and recorded webhook
  lines, carrying its intent IDs, must be present, so an empty, unrelated or
  partial log cannot pass. The scan waits for log indexing and retries.

Result **pass** means everything above was shown. **Fail** means some evidence
contradicts AC3. **Incomplete** (also a failed run) means the logs could not be
read or the window was truncated; the workflow's `skip_logs` input explicitly
accepts a run without the log checks. The report in the job summary carries
IDs, states and counts only. Each run sends two or three emails against the
Resend Free quota.

Not yet: manager reconcile/resend and custom-message endpoints, and showing or alerting
managers about adverse evidence beyond the audit trail (GK-024 Milestone B,
after GK-021 and GK-023/025); after a restore, re-correlating `unmatched`
events and applying `matched` events that arrived while their intent was
still `queued` (GK-034 Milestone B).

## Database backups (K11, GK-034)

Independent daily restore points, kept 30 days, separate from current order
records (K11). `gk-backup dump` (workflow **Database backup**, daily 02:30 UTC
and on demand) uses the read-only backup login to open one database snapshot,
read the migration history and the row count of every business table, and
`pg_dump` the `business` schema and `gk_meta` in that same snapshot. The
archive and its manifest (environment, time, schema version, migrations, row
counts, SHA-256 and size, `pg_dump` and server versions; no business data) are
sealed with AES-256-GCM, bound to their object names, and stored in the B2
backup bucket as `database/<environment>/<yyyymmdd-hhmmss>z/dump.sealed` and
`manifest.sealed`. The manifest is written last: a backup without one is
incomplete and ignored. The process never deletes; a B2 lifecycle rule on
`database/` hides files after 30 days and deletes them a day later (set
2026-11-04; the `recovery/` journal has no lifecycle rule). Dumps are held in
memory for sealing, up to 44 MiB, so the sealed object stays within the 64 MiB
the B2 client reads back (a larger response is an error, never truncated).
The client tools get the password in `PGPASSWORD` and no other `PG*` or
backup variables; their errors are reduced to one line (PostgreSQL's
DETAIL/CONTEXT lines can quote row values, and the workflow logs are public).
Staging decision for Creator review: one B2 key (list/read/write) serves the
backup and the restore check, and both decrypt on GitHub-hosted runners.
Before production: separate write-only and read-only keys, and a decision on
where personal data is decrypted (GK-014 exception 1).

`gk-backup restore-check` (workflow **Backup restore check**, after every
successful backup, daily at 06:00 UTC, and on demand) opens the newest
complete backup, refuses one older than 26 hours (backups have stopped) or
dated in the future, checks the dump against
its manifest, restores it with `pg_restore --no-owner --no-privileges
--single-transaction` into a disposable PostgreSQL (loopback only; in CI a
service container of the pinned image), and compares the migrations and every
business table's row count. A failure fails the workflow, which GitHub
reports by email.

Restoring to the isolated restore project (`gk-cakes-restore-test`, Frankfurt,
synthetic data only) uses credentials the Creator issues at restore time
(GK-014) and is **not yet built or verified** (Milestone B part 2). The CI
check proves a different path: a full restore into an empty database without
owners or privileges. A real restore must at least: create roles and schema
with `gk-migrate` up to the backup's version; restore business data only
(not `gk_meta`), replacing rows the migrations inserted (for example
`business.recovery_freeze`) and without firing triggers; set the outgoing
freeze in the same transaction, before any runtime connects; replay the
recovery journal, deletions and tombstones, and reconcile providers, as K09
and K11 describe. Object backups (Supabase Storage) start when the first
essential files exist (catalog images, GK-022).

## Identity (K00, GK-021)

Supabase authenticates; the API decides who a signed-in user is here and what
they may do. The browser sends the provider's access token as
`Authorization: Bearer <token>`; the API never sees passwords.

**Token checks** (`internal/identity`, `Verifier`): ES256 only, signed by a key
from the fixed configured issuer's JWKS (`AUTH_ISSUER` + `/.well-known/jwks.json`);
`iss` exactly `AUTH_ISSUER`; `aud` `authenticated`; `exp` (and `nbf`/`iat`
when present) within 30 seconds of clock skew; `sub` and `session_id` UUIDs;
`role` `authenticated`; not an anonymous session. Other algorithms, a missing
key ID, critical header extensions, other key types and malformed or oversized
tokens are refused. Claims are read by exact name (duplicates, quoted numbers
and non-canonical base64 are refused). Keys are cached. A token whose key is
unknown (a rotation) or whose cached set is older than 10 minutes waits for a
refresh, so a withdrawn key stops working. Refreshes start at most every 30
seconds (5 while no set was ever fetched), run one at a time in the background
with their own 5-second timeout, follow no redirects, and never delay a token
whose key is cached and fresh; a cancelled request cannot abort one. If a
refresh fails, the cached set stays usable for up to an hour without waiting
for the retries (a withdrawn key cannot be detected while the provider is
unreachable anyway), then requests fail closed (503), as they do when no set
was ever fetched. Provider metadata
(`app_metadata`, `user_metadata`, any role claim) is never read for
authorization.

**Accounts and capabilities** (migration `0007_identity.sql`): the first
authenticated request of a subject creates its local account
(`identity_resolve`, audited `account.created`). Staff capabilities are granted
locally: `chef` (manages the business, lists staff), `fulfillment` (prepare,
ready, dispatch, handover) and `technical_support` (bounded audited access);
future roles grant nothing. A disabled account can do nothing (403
`ACCOUNT_DISABLED`).

**Sessions.** Signing out ends the provider session but cannot revoke an
issued token (1 hour on the sandbox). Staff routes, and later every sensitive
mutation, ask the provider whether the session is current (`GET
AUTH_ISSUER/user`): a refusal naming an ended session or user
(`session_not_found`, `user_not_found`, `user_banned`, ...) is 401
`SESSION_ENDED` at once; any other answer, including a wrong publishable key
(401 without a code), fails closed (503) and is logged with its status and
code. Customer reads of their own account rely on the verified token until it
expires (a GK-021 decision, to revisit before order history is served).
Observed on the sandbox (2026-11-04): after sign-out, `/user` answers 403
`session_not_found` for the old token and the refresh token is refused; a
wrong publishable key gets 401 "Invalid API key".

| Route | Who | Responses |
| --- | --- | --- |
| `GET /api/v1/me` | any signed-in, enabled account | `{account_id, capabilities, session_expires_at}` |
| `GET /api/v1/admin/me` | any staff capability, current session | same |
| `GET /api/v1/admin/staff` | `chef`, current session (checked again by the database, which trusts the subject the API verified) | `{staff: [{account_id, capability, granted_at}]}` (enabled accounts only) |

Errors: 401 `UNAUTHENTICATED` (no or refused token, with
`WWW-Authenticate: Bearer`), 401 `SESSION_ENDED`, 403 `FORBIDDEN` (missing
capability), 403 `ACCOUNT_DISABLED`, 503 `UNAVAILABLE` (provider or database).
Responses never say which token check failed; the reason is logged without
the token.

**Operator commands.** Until chefs manage staff in the application, grants,
revocations and disabling are operator commands, audited by the database:
`go run ./cmd/gk-identity-admin -action grant|revoke|disable|enable|show
-subject <uuid> [-capability ...] -note "..."` with `MIGRATION_DATABASE_URL`, or
on staging the **Identity admin** workflow (write access, from `main` only,
where the staging environment holds the migration credential; the run ID is
appended to the note). A subject can be disabled before its first sign-in. The
API login can only resolve accounts and list staff for a chef.

## Catalog (K01, K07, GK-022)

Migration `0009_catalog.sql`, package `internal/catalog`. A **cake** is a
listing (draft, active or archived); its **variants** are one flavour in one
GK-002 size (`mini`, `s`, `m`, `l`, `xl`, `xxl` and the tiered sizes),
made to order or from ready stock. A variant's price (GBP pence), servings,
coating, composition, allergen declaration and planned recipe lines live in an
immutable **recipe version**; a new version becomes current, earlier ones stay
exactly as they were (what an order pins). A version copies the flavour name,
size and stock mode it was created with, so it never depends on live rows.
Triggers stop even the owner role from updating, deleting or truncating
versions, their allergens and lines, and the notice; cakes, variants and
flavours cannot be deleted or truncated, a variant's cake, flavour, size and
stock mode and a flavour's name cannot change (archive and create a new one).
Each size has its own recipe; nothing scales between sizes.

Allergens are the 14 UK FIC Annex II codes, each with the exact label the chef
approved (for example `CEREALS CONTAINING GLUTEN (WHEAT)`) and a `prominent`
flag (Snickers: `PEANUTS`). The shared-kitchen notice is versioned separately
and pinned by each recipe version. A variant without a current recipe version,
which always declares at least one allergen and the notice, is not purchasable
(missing disclosure disables buying, K01). Recipe quantities are exact
decimals with at most two places; counts are whole. Inventory links and batch
use come with GK-032; real recipe data waits for GK-038's measurements.

**Commands** need the chef capability and a current provider session, an
`Idempotency-Key` header (1 to 128 visible ASCII characters) and, for an
existing object, `expected_version`. Bodies are one UTF-8 JSON object with
lower_snake_case keys, no unknown or repeated fields and no `null` (no catalog
field is nullable; null is 422). A key is scoped to (account, command, path
resource); the same key and body return the original object, in its current
state, with `Idempotent-Replayed: true`; another body is 409 `KEY_REUSED`. A
refused command stores no key. The database checks the capability again under
a share lock on the account and capability rows (a revocation waits for a
command in progress, and a revoked chef cannot replay) and audits every
change. Technical support's catalog reads are not audited: the catalog is
public business content, not personal data.

| Route | Who | Notes |
| --- | --- | --- |
| `GET /api/v1/cakes?cursor&limit` | anyone | active cakes, newest first; limit 1-50 (default 20); `next_cursor` |
| `GET /api/v1/cakes/{id}` | anyone | 404 unless active; only active variants |
| `GET /api/v1/admin/cakes`, `/admin/cakes/{id}` | chef, technical support | every state, with versions and archived variants |
| `GET /api/v1/admin/recipes/versions/{id}` | chef, technical support | one immutable version with its lines |
| `GET /api/v1/admin/flavours`, `/admin/allergen-notices/current`, `/admin/sizes` | chef, technical support | |
| `POST /api/v1/admin/flavours` | chef | `{name}` |
| `POST /api/v1/admin/allergen-notices` | chef | `{body}`: a new notice version |
| `POST /api/v1/admin/cakes` | chef | `{name, description}`: a draft |
| `PATCH /api/v1/admin/cakes/{id}` | chef | `{expected_version, name?, description?}` (not nullable) |
| `POST /api/v1/admin/cakes/{id}/state` | chef | `{expected_version, state: active\|archived}` |
| `POST /api/v1/admin/cakes/{id}/variants` | chef | `{flavour_id, size_code, stock_mode}` |
| `POST /api/v1/admin/variants/{id}/archive` | chef | `{expected_version}` |
| `POST /api/v1/admin/recipes/versions` | chef | `{variant_id, expected_version, price_minor, servings_min, servings_max, coating, composition, allergen_notice_id, allergens, lines}` |

Errors: 400 `BAD_REQUEST` / `IDEMPOTENCY_KEY_REQUIRED`, 403 `FORBIDDEN`, 404,
409 `STALE_VERSION` / `KEY_REUSED` / `DUPLICATE` / `CONFLICT`, 413, 422
`INVALID_INPUT` (names the field; also an out-of-range `limit` or
`expected_version`), 503. The page query seeks on its index for every page
(no `OR` in the keyset predicate). Command receipts are kept until a
later maintenance job applies the 30-day retention (K00). Images (upload,
pending to ready) are GK-022 Milestone A2; availability and ready-stock lots
are GK-023.

## Schedule, capacity and finished cakes (K01, K04, K06, GK-023)

Migration `0010_schedule_capacity.sql`, package `internal/schedule`. Nothing
is seeded: until the chef publishes a schedule version, every date is
`outside_horizon`.

- **Schedule versions** (immutable, new bookings only) hold the default
  standard places per day, the last bookable date (the horizon: a concrete
  chef-approved date, never a month conversion), opening hours per weekday,
  the exact 60-minute appointment starts (each must start and end within the
  hours; no inferred grid) and closed dates. Local times are Europe/London;
  slot instants are computed per date. An appointment may not touch local
  01:00-02:00, where UK clocks change, so every start maps to one instant and
  each appointment lasts exactly 60 minutes.
- **Day capacity** is counted in cakes per handover date and is soft (K04).
  Checkout (GK-025) will call the owner-internal engine: `capacity_admit`
  refuses (`day_full`) when committed standard cakes plus the quantity exceed
  the limit, while other orders' tentative holds do not count against
  admission, so two holds for the last place can both be admitted and both
  commit; `capacity_commit` never refuses; `capacity_release` frees. The chef
  may set a day's limit below what is committed; accepted orders stay. Chef
  extras are outside the standard count but in the staff workload. The public
  sees only `remaining = max(0, limit - committed - tentative)`; every
  tentative hold counts until reconciliation releases it (the 15 minutes is a
  reconcile trigger, K04). A day override can be cleared
  (`use_schedule_default`) so later schedule defaults apply again.
- **Lead time** for a new purchase is 192 elapsed hours from now to the
  appointment start, equality allowed (BO-GK-001.lead_time). Chef-approved
  shorter lead for a specific finished cake (`lead_time_exception`) comes
  with checkout.
- **Finished cake lots** (ready stock or resale) are physical cakes of a
  variant's current recipe version: created unreleased, released by the
  chef, withdrawn; `lot_reserve` is exclusive (no oversell; a waiting second
  reservation gets `stock_unavailable`) and `lot_settle` returns or sells
  reserved cakes. Ready lots need a ready-stock variant and carry the chef's
  per-cake price (GK-002 `ready_cake_price_editing`), independent of the
  recipe version's price. A resale lot keeps its original handover time,
  which must be in the future and no later than its expiry, and costs the
  chef-entered base x 50%, half up. Lots are offered and reservable only while
  released, not withdrawn, unexpired, before a resale handover, and of an
  active cake and variant.

| Route | Who | Notes |
| --- | --- | --- |
| `GET /api/v1/availability?from&to&quantity` | anyone | at most 31 dates; `{timezone, as_of, schedule_version, days[{date, status, remaining_places}]}`; status `open`, `full`, `lead_time`, `closed`, `outside_horizon` or `no_times` |
| `GET /api/v1/schedule?date&fulfillment_mode&purpose=new_purchase&quantity` | anyone | `slots[{id, starts_at, ends_at, availability, reason_code}]`: `available`, `requestable` (GK delivery: subject to chef confirmation) or `unavailable` (`lead_time`, `full`, `closed`, `outside_horizon`); stable slot IDs per version; no per-slot cap; no hold |
| `GET /api/v1/ready-cakes` | anyone | `{cakes: [...]}`: offered lots with stock left |
| `GET /api/v1/admin/schedule` | chef, fulfilment, technical support | the current version; 404 before the first one |
| `GET /api/v1/admin/days?from&to` | chef, fulfilment, technical support | `{timezone, days[...]}`: limit, override, version, committed, tentative, chef extras, workload, remaining, `online_closed` |
| `GET /api/v1/admin/lots?include_inactive=true\|false` | chef, fulfilment, technical support | `{lots: [...]}` with stock counts |
| `POST /api/v1/admin/schedule/versions` | chef | `{expected_revision, standard_limit, bookable_until, weekdays[{weekday, opens, closes, starts}], closed_dates}`; answers with the version it published, also on a replay |
| `POST /api/v1/admin/days/{date}/limit` | chef | `{expected_version (0 for a day without a row), standard_limit}` or `{expected_version, use_schedule_default: true}` |
| `POST /api/v1/admin/lots` | chef | `{variant_id, kind: ready\|resale, quantity, price_minor \| resale_base_minor + original_handover_at, expires_at}` |
| `POST /api/v1/admin/lots/{id}/state` | chef | `{expected_version, action: release\|withdraw}` |

Commands follow the catalog's rules (Idempotency-Key, expected version,
strict JSON, audit). Schema r1.5 deviations in this milestone: slots are
derived from the published starts (stable IDs) instead of stored
`pickup_slot` rows; `capacity_allocation.order_ref` gets its foreign key to
`orders` with GK-025; `pickup_change`, `delivery_agreement` and
`lead_time_exception` come with orders.

## Discount codes and quotes (K02, K03, K04, GK-025 A1)

Migration `0011_checkout_quotes.sql`, package `internal/checkout`.

- **Discount codes** (chef): a code is stored upper case and matched without
  regard to case or surrounding spaces (an agent decision; no owner rule
  exists). Terms are immutable versions: `percentage` (basis points 1-10000
  of the cake price, never delivery) or `free_delivery` (no cake amount; it
  waives the chef-quoted GK delivery charge later, after the chef's distance
  review), an optional expiry, total limit and per-customer limit (which
  needs a signed-in customer, GK-007). One code per order. Uses are reserved
  and consumed by checkout (next milestone); quotes count none.
- **Quotes** price a basket for one appointment on the server: made-to-order
  variants (the current recipe version's price) or ready-made cake lots (the
  lot's price), 1-20 lines and at most 20 cakes. The slot must be offered now
  by the public schedule for the made-to-order quantity (ready cakes need no
  day places). A percentage code gives `D = round_half_up(subtotal x bps /
  10000)`, allocated to lines by floor share then largest remainder, ties by
  line number (K03): 1001p + 1000p at 10% is 200p as 100p + 100p. No VAT is
  added (GK-005: not registered). GK delivery is quoted later by the chef and
  paid separately, so a quote covers cakes only. A quote is immutable, valid
  24 elapsed hours from when it is first returned, reserves nothing and
  belongs to the signed-in customer or to the holder of a guest proof (32
  random bytes, unpadded base64url; only the SHA-256 digest is stored). A
  guessed quote ID gives no access (404). Resale lots and chef-approved
  shorter lead times are not quotable yet.

| Route | Who | Notes |
| --- | --- | --- |
| `POST /api/v1/checkout/quotes` | a guest (`guest_proof` in the body) or a signed-in customer (bearer; a refused token is 401, never a guest) | `{date, slot_id, fulfillment_mode, lines[{variant_id \| lot_id, quantity}], discount_code?}` with Idempotency-Key (scoped to the customer or guest proof); 409 `SLOT_UNAVAILABLE` / `ITEM_UNAVAILABLE` when the choice changed |
| `GET /api/v1/checkout/quotes/{id}` | the owner (bearer) or the proof holder (`X-Quote-Proof` header) | 404 otherwise |
| `GET /api/v1/admin/discount-codes` | chef, technical support | current terms of each code |
| `POST /api/v1/admin/discount-codes` | chef | `{code, kind, basis_points?, expires_at?, total_limit?, per_customer_limit?}` |
| `POST /api/v1/admin/discount-codes/{id}/versions`, `/archive` | chef | new terms `{expected_version, ...}`; archive `{expected_version}` |

## Not yet verified

- Runtime-role denial and the gateway on the sandbox. They are proven on
  PostgreSQL 17.11 only.
- Whether Supabase's pooler forwards the `statement_timeout`/`lock_timeout`
  startup parameters (direct connections do).
