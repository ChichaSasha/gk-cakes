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
| `PAYMENT_SECRET`         | api        | legacy placeholder, still validated (live keys only in production, test keys only elsewhere) but unused: the worker's `STRIPE_SECRET_KEY` is the payment key |
| `PAYMENT_WEBHOOK_SECRET`, `STORAGE_WRITE_KEY` | api | optional until their adapters exist; redacted (`PAYMENT_WEBHOOK_SECRET` is unused: see `STRIPE_WEBHOOK_SECRET`) |
| `STRIPE_ACCOUNT_ID`      | api, worker | the Stripe account collections are made in (`acct_...`, public); on the API it enables `POST /api/v1/orders/{id}/payment` (503 without it) and names the account of events that carry none; on the worker it must be set with `STRIPE_SECRET_KEY` and match the key's account (checked at start) |
| `STRIPE_WEBHOOK_SECRET`  | api        | signing secret of this environment's Stripe webhook endpoint (`whsec_...`), redacted; needs `STRIPE_ACCOUNT_ID`; when set the API serves `POST /webhooks/stripe`, otherwise that path is 404 |
| `STRIPE_SECRET_KEY`      | worker     | Stripe secret or restricted key, redacted: test mode (`sk_test_`, `rk_test_`) outside production, live mode only in production (nothing is configured for production now); without it payment jobs wait, see [Payments](#payments-k04-gk-025-a3b) |
| `EMAIL_WEBHOOK_SECRET`   | api        | Resend webhook signing secret (`whsec_` + base64), redacted; when set, the API serves `POST /webhooks/email`, otherwise that path is 404 |
| `AUTH_ISSUER`, `AUTH_PUBLISHABLE_KEY` | api | public identity-provider settings: the exact token issuer `https://<project>.supabase.co/auth/v1` and the publishable key `sb_publishable_...` (a secret key is refused); required in staging/production, otherwise both or neither; without them the authenticated `/api/v1` routes are not served, see [Identity](#identity-k00-gk-021) |
| `CHECKOUT_ENABLED`       | api        | `true` serves `POST /api/v1/checkouts`, the free confirmation and the payment request; default off (`false`); turn it on only once a worker of the same release (with the hold review, GK-025 A3c1) runs |
| `EMAIL_API_KEY`          | worker     | Resend sending key (`re_...`), redacted; see email dispatch below                          |
| `RECOVERY_B2_KEY_ID`, `RECOVERY_B2_KEY` | worker | B2 application key for the recovery journal (key secret redacted); scope enforced at start, see [Recovery journal](#recovery-journal-k11-gk-024) |
| `RECOVERY_ENCRYPTION_KEYS` | worker   | journal encryption keys `kid:base64(32 bytes)[,...]`, redacted                              |
| `BACKUP_DATABASE_URL`    | gk-backup dump | the `gk_backup` login (read only), same rules as `DATABASE_URL` |
| `RESTORE_DATABASE_URL`   | gk-backup restore-check | a disposable database on a loopback address only |
| `RESTORE_TARGET_DATABASE_URL` | gk-backup restore | the migration credential of the isolated restore database, same rules as `MIGRATION_DATABASE_URL`; the user must be in the URL's userinfo (`postgres://user:password@host/db`; no `user=` or `password=` query parameter: the synthetic check derives its temporary login's URL from it); `APP_ENV` names the environment whose backups are read (production is refused) |
| `RESTORE_BACKUP`         | gk-backup restore | optional backup time such as `20261104-052703z`; default the newest complete backup |
| `BACKUP_B2_KEY_ID`, `BACKUP_B2_KEY`, `BACKUP_ENCRYPTION_KEYS` | gk-backup | B2 key restricted to `database/` (list/read/write, no delete) and the backup encryption keys `kid:base64(32 bytes)[,...]`; see [Database backups](#database-backups-k11-gk-034) |
| `PG_DUMP`, `PG_RESTORE`, `BACKUP_MAX_AGE` | gk-backup | optional tool paths (default from `PATH`; version 17 or newer; `restore` runs `pg_restore` offline only, archive in and script out) and the restore check's maximum backup age (default `26h`) |

Email dispatch: the four worker variables above are required in staging and
production; in local and test they are set together or not at all, and a worker
without them sends no email (notification jobs are retried and then reach
attention). At start the worker checks the B2 key's scope and reachability and
exits with status 1 if the journal is not usable: no email is sent without it.
The API still accepts an `EMAIL_API_KEY` but does not use it.

API endpoints: `GET /healthz` (liveness), `GET /readyz` (database reachable and,
outside local, the login check), `POST /webhooks/email` (Resend events,
Svix-signed; only with `EMAIL_WEBHOOK_SECRET`), `POST /webhooks/stripe` (Stripe
events, signed; only with `STRIPE_WEBHOOK_SECRET`), and with `AUTH_ISSUER` the
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
| `RESTORE_TARGET_DATABASE_URL` | `postgres` role of the isolated restore project `gk-cakes-restore-test` (synthetic/staging test data only) | Creator's Keychain `gk-restore-test-DATABASE_PASSWORD` (Bitwarden); not in GitHub or Railway | issued at restore time: reset through the Supabase Management API (Creator decision E1, 2026-11-04) | reset the password again |
| `MIGRATION_DATABASE_URL` | migration credential (used by `gk-migrate` in deploys and by the manual Email proof and Identity admin workflows) | GitHub environment secret, staging: `STAGING_MIGRATION_DATABASE_URL` | rotate the password of the same role (see below)                | rotate; runtime services never hold it       |
| `RAILWAY_STAGING_TOKEN`  | Railway project token, staging environment only | GitHub environment `staging` secret (deployable from `main` only) | regenerate in Railway, update the secret                  | delete the token in Railway                  |
| `STRIPE_SECRET_KEY`      | Stripe sandbox (test-mode) key of account `acct_1UNHEgPqGToF2ulH`, one per environment; a restricted key limited to Checkout Sessions (write) and PaymentIntents, Charges and the account (read) is preferred | worker service variables (sealed); Creator's Keychain/Bitwarden | roll the key in the Stripe dashboard, update the variable, redeploy, expire the old key | roll or delete the key in the Stripe dashboard |
| `STRIPE_WEBHOOK_SECRET`  | signing secret of the Stripe webhook endpoint for this environment's API (sandbox) | API service variables (sealed) | roll the endpoint's secret in Stripe (it can keep the old one valid for a while), update the variable and redeploy promptly | delete the webhook endpoint in Stripe |
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
| `gk_api`    | API runtime                   | schema `USAGE`; `EXECUTE` on `email_event_record` (webhook evidence), `identity_resolve` and `identity_staff_list` (identity), the catalog, schedule, checkout and order gateway functions, and `payment_initiate` and `payment_event_record` (payments); the exact list is tested |
| `gk_worker` | worker runtime                | `SELECT` on `business.job`; `EXECUTE` on job lease functions (`job_claim`, `job_heartbeat`, `job_complete`, `job_release`, `job_fail`, `job_attention`), the lease-fenced notification gateway (`notification_dispatch_begin`, `notification_attempt_record`, `notification_recovery_*`) and the lease-fenced order-command gateway (`order_command_begin`, `order_command_reserve`, `order_command_prepared`, `order_command_apply`, `order_command_secured`, `order_command_frozen`) and the lease-fenced payment gateway (`payment_job_begin`, `payment_dispatch_admit`, `payment_attempt_record`, `payment_reconcile_record`, `payment_recovery_*`, `payment_expire_admit`, `payment_expire_record`, `payment_under_reconciliation`) and the order hold review (`order_hold_review_run`); the exact list is tested |
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
for outgoing dispatches (email, and Stripe Checkout Sessions since GK-025 A3b;
refunds later) and accepted order commands (GK-025: free and paid
confirmation now; changes later; see "Accepted order commands" below). Every dispatch runs through
`Coordinator.Dispatch`:

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
table). Restore tooling (`gk-backup restore`, as `gk_owner`) freezes all
outgoing effects (`business.recovery_freeze`) in the same transaction that
loads the backup.

Not yet:

- B2 Object Lock retention and the domain restore drill (GK-035). The
  database restore (`gk-backup restore`, see
  [Database backups](#database-backups-k11-gk-034)) restores frozen; replaying
  the journal before workers resume is GK-035's: a restored database can re-anchor a notice's window (the
  existing prepared envelope then refuses the resume), and an existing outcome
  envelope should be adopted rather than replayed;
- K09 retention for order commands: `business.order_command.payload` (and its
  prepared envelope) is an immutable copy of the order snapshot, including
  the customer's name and email. Redacting it once the operation is secured,
  with order anonymisation, belongs to the retention work (GK-034 Milestone B,
  GK-037 deletion inventory);
- K09 retention for the journal: it keeps recipients and message bodies (and
  later essential photos) until deleted, and this key cannot delete. Keeping
  only current copies of still-retained records, 30-day restore points and
  deletion tombstones carried into replay belong to GK-034 Milestone B with
  the retention work.

### Accepted order commands (K11, GK-025 A3a)

An accepted order command is a recovery operation of kind `command` on its
order (aggregate = order, prior version = the order's version), run by
`Coordinator.Command`:

1. the API records the command, its exact accepted payload (the full order
   snapshot: items, holds, code use, guest capability digests, contact) and
   its receipt instant, reserves the operation and enqueues a worker job in
   one transaction, and answers 202;
2. the worker writes the encrypted **prepared** envelope and marks it
   `prepared`;
3. the domain commit re-validates the command and changes the order (or
   records a refusal) in one transaction with the `committed` phase; it is
   idempotent for the operation;
4. the worker writes the **outcome** envelope and marks it `secured`; only
   then is the result visible. Between the commit and the outcome
   acknowledgment the order shows `confirmation_pending`, never `confirmed`.

A command has no replay deadline (its commit is local and idempotent for the
operation forever; the database records `infinity`, keeping the column
`NOT NULL` for the running build)
and never goes to `reconcile`. A restore freeze and a missing journal make
the job wait (`Defer`), never commit without protection: the commit itself
re-checks the freeze under a share lock on the freeze row (SQLSTATE `GK002`),
so a freeze that lands after the worker's last check still stops it. After a primary
database loss `Pending` lists commands as well; restore tooling resumes each
with `Command` after checking the restored order.

Since GK-025 A3b the same path carries the system's paid confirmation
(`paid_confirm`, recorded by the payment reconcile; see
[Payments](#payments-k04-gk-025-a3b)); `order_command_apply` dispatches on the
command, and a secured `paid_awaiting_agreement` outcome is exposed like a
confirmation.

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

### Restoring into the isolated restore project (GK-034 Milestone B)

`gk-backup restore` restores a backup (the newest complete one, or
`RESTORE_BACKUP`) into the isolated restore database
(`gk-cakes-restore-test`, Frankfurt, synthetic/staging data only), frozen.
It is a different path from the CI check above, which restores everything
into an empty database without owners or privileges:

1. **Select and validate** (no target access yet): the dump must match its
   manifest (SHA-256, size); `pg_restore --data-only` turns the archive into
   a text script offline (no database connection), and the script is
   accepted only as data: session settings, comments, psql's `\restrict`
   guard and COPY blocks into `business` and `gk_meta` (any function call,
   DDL or unknown statement is refused, naming a line number only). Every
   business table's row count must match the manifest, and the backup's
   migration history must be this build's, file for file (same SHA-256);
   a backup with a migration this build does not know is refused (restore
   with the release that took it).
2. **Target**: a session advisory lock allows one restore per target at a
   time (held to the end). The target is refused unless it is empty (no
   `business` or `gk_meta` schema) or exactly a fresh migrated schema: a
   history that is a prefix of the backup's and no rows except those
   migrations insert (`business.recovery_freeze`). A restored target is
   therefore refused too.
3. **Migrate** with `gk-migrate`'s code up to the backup's schema version
   (roles, schema, gateway functions, privileges), then freeze the fresh
   schema at once.
4. **Load, in one transaction**, as `gk_owner` with
   `session_replication_role = replica` (no trigger fires; the migration
   credential needs superuser or `GRANT SET ON PARAMETER
   session_replication_role`, which Supabase's `postgres` role has): lock
   every business table against writers and check again that none holds
   more than its fresh rows (anything written since step 2 is refused,
   never deleted), delete every business table's rows (the migration seed
   rows included), COPY the backup's business data (not `gk_meta`: the
   history comes from this build's migrations), revoke every job lease
   (every generation moves up by 1,000,000, so a lease held at backup time
   or issued after it, in the lost window, is fenced out even if its worker
   ID came back; jobs leased at backup time go to `retry`), set `recovery_freeze` to frozen with the reason `gk-backup
   restore of database/<env>/<time>/: outgoing effects frozen until
   reconciled (K11)`, check every foreign key (the load skipped their
   triggers) and every row count, and commit. No state ever exists in which
   restored jobs are visible and not frozen.
5. **Verify** after the commit: history with checksums, row counts, the
   freeze, no live lease, and that the freeze row and every restored
   business row carry the load transaction's ID (`xmin`).
6. **Synthetic dispatch check**: a temporary `gk_worker` login (created with
   a SCRAM verifier, never the password, valid for an hour, checked with
   `database.CheckLogin`, dropped at the end) presents every lease from the
   backup to `job_heartbeat`, `job_complete`, `job_release`, `job_attention`
   and `job_fail` (all must refuse), then runs the worker's own handler
   chain (`internal/handlers`, the constructor `gk-worker` uses: order
   commands, payments, refunds, hold reviews, email) over every claimable
   job with recording providers and a journal that refuses writes. Every
   job must answer frozen (a suspended notice may answer attention: its
   manager hold is checked before the freeze); a job no handler owns, a
   provider call, an envelope written, or a change to any business table
   but `job` (content digests before and after) fails the check. Each job
   is checked once and released for an hour, never completed, failed or
   escalated; the check fails as well if the temporary login cannot be
   dropped (`synthetic_login_dropped`).

   **A failed check has already modified the target**: the claimed jobs
   moved (new generations, released for an hour) and a handler that went
   past the freeze may have written. Empty the target (runbook step 2)
   before the next restore; it would be refused as non-empty anyway when
   any business row remains.

The result is one JSON object: backup name and time, schema version,
migrations applied, tables and rows, `frozen`, `freeze_same_transaction`,
leases in the backup and revoked, the generation epoch, the synthetic
dispatch counts, the
data-loss window (restore time minus backup time) and per-step timings.
Errors keep PostgreSQL's data messages out (SQLSTATE classes 22 and 23 are
reduced to code, table and constraint).

Runbook (local, by the agent or the Creator; no GitHub secret or workflow:
the restore credential stays in the Keychain, Creator decision E1):

```sh
# 1. Credential: reset the restore project's postgres password through the
#    Management API (PAT in Keychain gk-supabase-pat, project gjrhgikkvxrvvogvxnrs)
#    and keep it in Keychain gk-restore-test-DATABASE_PASSWORD (account $USER).
# 2. Empty the target if it holds an earlier restore (synthetic data only):
#    as postgres, SET ROLE gk_owner; DROP SCHEMA business CASCADE;
#    RESET ROLE; DROP SCHEMA gk_meta CASCADE;  (the NOLOGIN groups stay)
# 3. Restore (from api/; pg_restore 17+, e.g. a wrapper running
#    `docker exec -i <postgres:17 container> pg_restore "$@"`):
CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/gk-backup
PW=$(security find-generic-password -a "$USER" -s gk-restore-test-DATABASE_PASSWORD -w)
APP_ENV=staging \
BACKUP_B2_KEY_ID=$(security find-generic-password -s gk-staging-backup-B2_KEY_ID -w) \
BACKUP_B2_KEY=$(security find-generic-password -s gk-staging-backup-B2_KEY -w) \
BACKUP_ENCRYPTION_KEYS=$(security find-generic-password -s gk-staging-backup-ENCRYPTION_KEYS -w) \
RESTORE_TARGET_DATABASE_URL="postgres://postgres.gjrhgikkvxrvvogvxnrs:$PW@aws-1-eu-central-1.pooler.supabase.com:5432/postgres?sslmode=verify-full&sslrootcert=$PWD/certs/supabase-root-2021.crt" \
PG_RESTORE=/path/to/pg_restore17 bin/gk-backup restore
```

The restore project's direct host (`db.<ref>.supabase.co`) is IPv6 only;
from an IPv4 network use the session pooler (port 5432, user
`postgres.<ref>`), which supports the advisory lock, the transaction and
COPY. The synthetic worker login keeps the pooler's `.<ref>` suffix.

**Exercise, 2026-11-05** (build of commit `5848a4e`, into
`gk-cakes-restore-test` through the session pooler, `verify-full`; that
build revoked leases with a generation step of 1 and did not yet re-check
the target under table locks, fail on unowned jobs or report the login
drop; those review fixes are covered by the integration tests, not by a
new exercise):

| Run | Backup | Schema | Tables / rows | Leases revoked | Synthetic dispatch | Total (select, fetch, validate, target, migrate, load, verify, check) |
| --- | --- | --- | --- | --- | --- | --- |
| staging | `database/staging/20261104-052703z/` (newest complete) | `0008` (8 migrations) | 9 / 90, all matching | 0 of 0 | 0 claimable (all 10 restored jobs completed, all 10 recovery operations secured) | 14.5 s (1.8, 0.2, 0.1, 0.4, 2.9, 3.2, 1.8, 4.1) |
| synthetic | a seeded local backup with one due job of every kind, a leased job and a suspended notice | `0017` (17) | 46 / 25, all matching | 1 of 1; 5 of 5 old-lease calls refused | 7 claimed: 6 deferred frozen (order command, payment, refund, hold review, 2 notifications), 1 suspended notice held; 0 provider calls, 0 journal writes, no other table changed | 51.9 s (load 18.3, check 18.0: one pooler round trip per table and foreign key) |

Both runs set the freeze in the load transaction (`freeze_same_transaction`)
and left no synthetic login. A second restore over the restored target was
refused (`business.account has 3 rows`). Data-loss window of the staging
backup: 23 h 30 min (taken 2026-11-04 05:27 UTC by hand; the 02:30 UTC
schedule had not run on 2026-11-05 by 04:57). The staging recovery journal
(`recovery/staging/`) holds 15 operations, all with an outcome envelope (none
pending), against 10 in that backup: the 5 newer ones are the lost window
GK-035 must replay. Object restoration had nothing to restore: no business
record references a stored file yet (catalog images are not built), and no
journal blob exists.

Remaining for GK-035 (not done here): provider reconciliation of the lost
window (Resend, Stripe sessions and refunds), journal replay (adopting
outcome envelopes, resuming prepared operations with `Coordinator.Pending`;
opening envelopes needs the environment's `RECOVERY_ENCRYPTION_KEYS`),
deletions and tombstones carried into the restore (K09), object
restoration once essential files exist, and a controlled, audited unfreeze
(`business.recovery_set_freeze(false, …)`) after reconciliation. Before production: a read-only restore key and a
decision on where personal data is decrypted (GK-014 exception 1).

Hold-review note (found in this exercise, closed by
`0019_hold_review_freeze.sql`): `order_hold_review_run` answered `done` for
an already decided review, and closed an undecided one (`closed` or `paid`)
whose order no longer awaited payment or had a succeeded collection, before
it read the freeze, so on a restored database the worker completed those
jobs while frozen (the synthetic check counted `hold_review:completed` and
failed; the staging backup had no such job). The review now reads the
freeze first (a share-locked read of `business.recovery_freeze`, before any
lock, outcome or audit) and defers every hold review job while frozen; the
synthetic restore test seeds an undecided, a decided and a closing review
and expects all three deferred frozen. The other worker begin functions
(order command, payment, refund, notification) already answered `frozen`
right after identifying their job.

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
decimals with at most two places; counts are whole. Catalog text refuses
control characters (C0 and C1) and U+2028/U+2029, in the API and in the
database whatever its locale (`0018_small_fixes.sql`); the notice and a
description may contain line feeds. The copied snapshot columns keep their
source rules there too (trimmed flavour name of 1 to 80 characters, a GK-002
size code, a stock mode, a one-dimensional composition without nulls), and a
quote's and an order's copied slot window must end after it starts. (Those
constraints are added `NOT VALID` and then validated in the same migration;
inside one migration transaction that gives no locking benefit over a plain
`ADD CONSTRAINT`, the table locks are held until it commits.) Inventory links and batch
use come with GK-032; real recipe data waits for GK-038's measurements.

**Commands** need the chef capability and a current provider session, an
`Idempotency-Key` header (1 to 128 visible ASCII characters) and, for an
existing object, `expected_version`. Bodies are one UTF-8 JSON object with
lower_snake_case keys, no unknown or repeated fields and no `null` (null is
422) except on a field documented as nullable (only a cake's `colour_id`). The structural checks win: a `null` on an
unknown field is 400 (unknown field), not 422; this holds for every command
that reads its body this way (catalog, schedule and checkout). A key is scoped to (account, command, path
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
| `GET /api/v1/admin/colours` | chef, technical support | every colour, archived included: `{id, code, label, position, archived_at, version}` by position, then label |
| `POST /api/v1/admin/colours` | chef | `{code, label, position}` → 201; a used code (archived included) is 409 `DUPLICATE` |
| `PATCH /api/v1/admin/colours/{id}` | chef | `{expected_version, label?, position?, archived?}` (at least one; none nullable; `code` cannot change); `archived: true` archives, `false` restores |
| `POST /api/v1/admin/flavours` | chef | `{name}` |
| `POST /api/v1/admin/allergen-notices` | chef | `{body}`: a new notice version |
| `POST /api/v1/admin/cakes` | chef | `{name, description, colour_id?}`: a draft; `colour_id` a colour ID or `null` (none) |
| `PATCH /api/v1/admin/cakes/{id}` | chef | `{expected_version, name?, description?, colour_id?}`; name and description not nullable; `colour_id: null` clears the colour, absent keeps it |
| `POST /api/v1/admin/cakes/{id}/state` | chef | `{expected_version, state: active\|archived}` |
| `POST /api/v1/admin/cakes/{id}/variants` | chef | `{flavour_id, size_code, stock_mode}` |
| `POST /api/v1/admin/variants/{id}/archive` | chef | `{expected_version}` |
| `POST /api/v1/admin/recipes/versions` | chef | `{variant_id, expected_version, price_minor, servings_min, servings_max, coating, composition, allergen_notice_id, allergens, lines}` |

**Colours** (`0028_catalog_colours.sql`; K01 amendment 2026-11-05, from the
Creator-approved GK-016 r01c sketch) feed the shop's Colour filter. The chef
keeps the list: a `code` (`^[a-z0-9][a-z0-9-]{0,39}$`, not `all`, the filter's
"All colours" value; unique among all colours, archived ones included, and
never changed), a `label` (1 to 40 Unicode code points, single line, no
surrounding spaces: refused, not trimmed) and a `position` (0 to 10000, the
filter order). Colours are archived, never deleted, and can be restored. A cake
has at most one colour (`colour_id`); an unknown or archived colour is 422
`INVALID_INPUT` (`colour_id is not an active colour`). Every cake in
`GET /api/v1/cakes` and `/cakes/{id}` carries `"colour": {"code", "label",
"position"}` or `"colour": null` (no colour, or an archived one); staff reads
add the colour's `id` and `archived_at`. Nothing is seeded: the sketch's
starting list (Pink, Silver, Red, Chocolate, White) is for the chef to create
(chef screens are GK-018). The 0009 cake create/update functions stay for the
previous build and leave the colour unchanged; a body without `colour_id`
hashes as before, so a retry across a rolling deploy replays. Colours are
public catalogue content, not personal data.

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
  random bytes, unpadded base64url; only the SHA-256 digest is stored), which
  must be fresh for each new quote (a replay of the same key is fine). A
  guessed quote ID gives no access (404). A ready cake must be within its safe
  expiry at the end of the appointment. Resale lots and chef-approved shorter
  lead times are not quotable yet. A client price, total, currency or unknown
  field is a malformed request (400). The response carries `version: 1`
  (quotes are immutable; a changed price is a new quote). Expired quotes are
  removed only by the owner-only `quote_purge_expired` (at least 30 days after
  expiry). **Before production:** rate-limit quote creation (anonymous
  callers can create rows) and the code lookup it implies.

| Route | Who | Notes |
| --- | --- | --- |
| `POST /api/v1/checkout/quotes` | a guest (`guest_proof` in the body) or a signed-in customer (bearer; a refused token, or any token when sign-in is not configured, is 401, never a guest) | `{date, slot_id, fulfillment_mode, lines[{variant_id \| lot_id, quantity}], discount_code?}` with Idempotency-Key (scoped to the customer or guest proof); 409 `SLOT_UNAVAILABLE` / `ITEM_UNAVAILABLE` when the choice changed |
| `GET /api/v1/checkout/quotes/{id}` | the owner (bearer) or the proof holder (`X-Quote-Proof` header) | 404 otherwise |
| `GET /api/v1/admin/discount-codes` | chef, technical support | current terms of each code |
| `POST /api/v1/admin/discount-codes` | chef | `{code, kind, basis_points?, expires_at?, total_limit?, per_customer_limit?}` |
| `POST /api/v1/admin/discount-codes/{id}/versions`, `/archive` | chef | new terms `{expected_version, ...}`; archive `{expected_version}` |

## Orders and checkout holds (K02, K04, K05, GK-025 A2)

Migration `0012_orders.sql`. The chef records **policy versions** (refund
and cancellation terms citing their owner decision and source hash; final
legal text is reviewed before launch, GK-037); customers consent to the
newest effective one. `POST /api/v1/checkouts` turns one quote into one order
awaiting payment, in one transaction:

- the quote's owner (bearer) or guest proof must match (404 otherwise); the
  quote must not have expired (24 h; equality expires: `QUOTE_EXPIRED`); the
  consented policy must be the current one (`POLICY_CHANGED`); every recipe
  version, price, lot price and code version must still be current
  (`PRICE_CHANGED`: the customer reviews a new quote); the appointment must
  still be offered (`SLOT_UNAVAILABLE`);
- it inserts the order (customer full name, a different payer's name only
  when someone else pays, an email for service notices) and immutable items
  (unit price, discount share, net paid per line), fixes the refund deadline
  as the appointment start minus 120 hours (K05), admits the made-to-order
  cakes to the day (soft capacity: committed cakes plus the quantity within
  the limit; other orders' holds never own the last place, so two customers
  can both take it), reserves ready cakes exclusively (`ITEM_UNAVAILABLE`
  when gone), reserves the code's use within its total and per-customer
  limits (`CODE_USED_UP`), and binds a guest order to the quote's proof as
  an order capability (`read`, `pay`, `cancel`, `move_pickup`);
- a refused checkout leaves nothing behind; a repeated checkout of the same
  quote returns the same order (`Idempotent-Replayed`), another body is
  `KEY_REUSED`. The answer is 202 `{order_id, state: "pending",
  poll_after_seconds: 3}`: never an accepted order.

Quotes now judge day places as admission does (committed cakes only), and
the quote purge keeps quotes that became orders. Locks follow K00 (day, lots
by ID, code; rows referencing them only afterwards), so concurrent checkouts
of the same ready cakes never deadlock; the code is rechecked after its lock.
A guest replays a checkout only while its order capability is active. Code
uses move reserved to consumed or released and never back; revoked
capabilities stay revoked; order snapshots are immutable and terminal orders
are never reopened.

**Free confirmation (K03, GK-025 A3a, migration `0013_order_commands.sql`).**
A zero-amount order is confirmed by the customer's explicit
`POST /api/v1/orders/{id}/free-confirmation` (`{expected_version}` with an
Idempotency-Key; the owner, or a guest capability with `pay`), without any
payment row. The persisted receipt instant is the anchor (K04): the quote's
24 h window must still be open then (equality expires: `QUOTE_EXPIRED`) and
the handover at least 192 elapsed hours later (equality allowed:
`LEAD_TIME`); the order must be awaiting payment with its holds intact and
cost nothing (`PAYMENT_REQUIRED` otherwise). The answer is 202
`{order_id, operation_id, state: "confirmation_pending", poll_after_seconds: 3}`; the
worker commits it under K11 (above): the day allocation becomes committed
(never refused for capacity), the ready cakes sold, the code use consumed,
`free_confirmed_at` = the receipt instant. The order reads
`confirmation_pending` until the outcome is secured, then `confirmed`, and
`free_confirmation` reports `processing`, `confirmed` or `refused` (with a
reason: `order_changed`, `holds_released`, `window_closed`; a refusal changes
nothing but the order's version). At most one free confirmation per order
is pending or confirmed: the same actor (account, or guest capability) and
key replays, another key is a conflict; after a refusal the customer may ask
again at the new version. No confirmation email is sent yet.

**Switched off by default:** `POST /api/v1/checkouts`, the free
confirmation and the payment request are served only with
`CHECKOUT_ENABLED=true`. Payment (Stripe sandbox Checkout behind the K11
recovery gate) is [below](#payments-k04-gk-025-a3b); the hold review that
releases the holds of abandoned and proven-unpaid orders is
there too (step 7). Recorded
deviations: the 202 carries the order ID (no payment operation exists yet);
a 409 `PRICE_CHANGED` asks the client for a new quote (with a fresh guest
proof) instead of returning one; the appointment is the quote's.

| Route | Who | Notes |
| --- | --- | --- |
| `GET /api/v1/policies/current` | anyone | `{id, summary, effective_at, source_decision_id}`; 404 before the first |
| `POST /api/v1/admin/policies` | chef | `{source_decision_id, source_revision_sha256, summary, effective_at, approver_evidence}`; an `effective_at` more than 1 minute before the database's current time is refused (422 `INVALID_INPUT` "effective_at is not valid", `business.policy_record`): a policy cannot be back-dated; a future instant is accepted and becomes current then |
| `POST /api/v1/checkouts` (only with `CHECKOUT_ENABLED=true`) | a guest (`guest_proof` of the quote) or the signed-in owner | `{quote_id, quote_version: 1, consented_policy_version, contact{full_name, email, payer_name?}}` with Idempotency-Key |
| `POST /api/v1/orders/{id}/free-confirmation` (only with `CHECKOUT_ENABLED=true`) | the owner (bearer) or the guest (`X-Order-Capability` with `pay`) | `{expected_version}` with Idempotency-Key; 202 `confirmation_pending` |
| `POST /api/v1/orders/{id}/payment` (only with `CHECKOUT_ENABLED=true`; 503 without `STRIPE_ACCOUNT_ID` or during a restore freeze) | the owner (bearer) or the guest (`X-Order-Capability` with `pay`) | `{expected_version}` with Idempotency-Key; 202 `payment_pending`, see [Payments](#payments-k04-gk-025-a3b) |
| `GET /api/v1/orders/{id}` | the owner (bearer) or the guest (`X-Order-Capability`: the quote's proof) | the order snapshot (`lifecycle` is `confirmation_pending` until K11 secures a commit; `paid_at` only then; `payment`: the latest collection); 404 otherwise |
| `POST /webhooks/stripe` (only with `STRIPE_WEBHOOK_SECRET`) | Stripe (signed) | events; 200 only after the durable insert |

## Payments (K04, GK-025 A3b)

Migration `0014_payments.sql`, `internal/payments`. A full-payment order is
paid through a Stripe **sandbox** Checkout Session; the payment confirms the
order only after the worker has verified Stripe's evidence, through the same
K11 command path as the free confirmation.

**1. Initiation (API).** `POST /api/v1/orders/{id}/payment`
(`{expected_version}`, Idempotency-Key; the owner, or a guest capability with
`pay`) calls `business.payment_initiate` under the order lock. The order must
be awaiting a full payment, not yet committed, with its holds intact, at the
expected version (`STALE_VERSION`); at the request instant the quote's 24 h
window must be open (equality expires: `QUOTE_EXPIRED`) and the handover at
least 192 elapsed hours away (equality allowed: `LEAD_TIME`; the late-success
path is never a way around a lead time already missed). No other collection
of the order may still succeed (`created`, `pending` or `unknown`):
`PAYMENT_IN_PROGRESS` ("do not pay again"), except that the same actor
(account, or guest capability) with the same key replays the same collection
(`Idempotent-Replayed`; another body is `KEY_REUSED`). A failed, expired or
cancelled collection allows a new one; a succeeded one never does. While a
restore freeze is on, a new collection is refused with 503 `UNAVAILABLE`
(retryable; a replay still answers). The collection stores the amount (the
order total, GBP pence), the environment, the Stripe account
(`STRIPE_ACCOUNT_ID`), the mode (live only in production) and the UI origin
of the return URLs, its dispatch job and an audit event, in one transaction,
and the API answers 202
`{order_id, operation_id, state: "payment_pending", poll_after_seconds: 3}`
(`operation_id` is the collection, which is also the Stripe idempotency key).

**2. The Checkout Session request** is built once, at the first dispatch, and
never rebuilt (stored in the payload, form-encoded, keys sorted): `mode=payment`,
one line item (`price_data[currency]=gbp`, `price_data[unit_amount]` = the
order total, `price_data[product_data][name]=GK Cakes order`, `quantity=1`),
`client_reference_id` = the order, `metadata[order_id|collection_id|environment]`
and the same three under `payment_intent_data[metadata]`, `success_url` and
`cancel_url` = `<UI_ORIGIN>/orders/<id>?payment=returned` (the return never
confirms anything) and `expires_at` = the first dispatch second (database
clock) + 2 hours (Stripe allows 30 minutes to 24 hours; two hours leaves room
for retries of a call that may never have reached Stripe, and the
15-minute expire request (step 7) bounds how long a hold really waits). No customer
name, email or cake detail is sent. Every
request pins `Stripe-Version: 2026-08-26.dahlia` (verified on the sandbox).

**3. Dispatch (worker, K11 `Dispatch`).** Only once the worker has its Stripe
key and journal (a waiting job fixes nothing and starts no window),
`payment_dispatch_fix` stores the request built on the database clock (the
function checks it belongs to the collection, order, amount, environment,
account, mode and expiry) and only then fixes `first_dispatch_at` and
`replay_until` = first dispatch + 23 hours (Stripe keeps idempotency keys 24
hours). The dispatch is a recovery operation of kind `dispatch` (aggregate =
collection, prior version 0) reserved with that payload's hash; the worker
validates the stored payload against the collection (exact keys and values,
canonical encoding, environment, account, mode, return origin, expiry),
writes the encrypted prepared envelope (the body is persisted before the
external create, K04), and right before each call `payment_dispatch_admit`
re-checks the restore freeze and the window on the database clock and records
`dispatch_started_at`. Since migration `0020_dispatch_freeze_first.sql`
`payment_dispatch_fix` and `payment_dispatch_admit` read the freeze (share
lock) before any other answer, so a freeze set while a worker is between its
begin and fix/admit defers the job instead of letting a `closed` collection
complete. A first call (none ever admitted) with less than 31
minutes of session lifetime left (for example after the journal was down for
over 1.5 hours) is not made: the dispatch is recorded as `rejected`
(`expired_before_dispatch`), the collection `failed` without any Stripe call
and the job goes to attention. Every call is appended to
`business.payment_attempt`.

| Stripe response | Attempt | Collection | Job |
| --- | --- | --- | --- |
| 200 Checkout Session | `created` | `pending` (session ID, URL, provider `expires_at`, mode); a reconcile 15 minutes later | completed |
| 400/402 `invalid_request_error` or `card_error` (for example a retry so late that the stored `expires_at` is under 30 minutes away) | `rejected` | `failed` (the customer may start a new one) | attention |
| 400 `idempotency_error` | `conflict` | `unknown`; the dispatch goes to `reconcile` | attention (never a new session under another key) |
| 401/403/404 and other 4xx | `refused` | unchanged | attention (fix the key, then requeue) |
| 409, 429 | `deferred` | unchanged | retry with the same key and body |
| timeout, connection loss, 5xx | `unknown` | `unknown` | retry with the same key and body (Stripe returns the same session) |

A rejected dispatch is audited once as `payment.failed` on the order, the
collection ID and reason in its data like every other payment audit (migration
`0017_followups.sql`; earlier events keep subject kind `payment_collection`).
A session created in the wrong mode makes the collection `unknown` (attention).
After the window, an unknown dispatch goes to reconciliation, never to a new
send. Without `STRIPE_SECRET_KEY` (or the journal) payment jobs wait (15
minutes, not a failure); a restore freeze defers them (5 minutes).

**4. Webhook (API).** Configure the endpoint `https://<api host>/webhooks/stripe`
in the Stripe sandbox with at least `checkout.session.completed`,
`checkout.session.async_payment_succeeded`, `checkout.session.async_payment_failed`,
`checkout.session.expired`, `payment_intent.succeeded`,
`payment_intent.payment_failed` and `charge.refunded`, plus (full return,
A3c2) `charge.refund.updated`, `refund.created`, `refund.updated` and
`refund.failed`. The API verifies
`Stripe-Signature` over the raw body (`t=` within 5 minutes either way, one of
the `v1=` signatures = HMAC-SHA256 of `<t>.<body>` with the signing secret,
constant-time; bodies over 256 KiB are 413; otherwise 400), parses the event
ID, type, account (absent: `STRIPE_ACCOUNT_ID`), mode, time, the object and
its metadata, and `business.payment_event_record` stores it once per
(account, mode, event ID) before the API answers 200 (a storage failure is
503, so Stripe retries the same event; a duplicate is acknowledged without new
work). When the metadata names a collection of this environment, account and
mode that is not yet decided, that collection's reconcile is scheduled now (a
waiting one is brought forward under the collection's row lock, so duplicate
or concurrent events never pile up jobs).
Every verified event is stored (IDs, type, mode, times, body hash; never
customer or card data), including events of other collections or
environments, which start nothing. A refund event (`charge.refunded`,
`charge.refund.updated`, `refund.*`) whose object's metadata names a
`succeeded` collection brings that collection's **refund reconcile** forward
(see "Full return" below; at most one waits per collection).

**5. Reconcile (worker).** The worker retrieves
`GET /v1/checkout/sessions/{id}?expand[]=payment_intent.latest_charge` (the
created session, or one a verified event named) and decides:

- **mismatch** (collection `unknown`, attention, never confirmed): the
  session's metadata or `client_reference_id` do not name this collection,
  order and environment, the mode is wrong, the currency is not GBP, the
  amount differs, the payment intent or charge contradicts them (metadata,
  mode, currency, a different received or captured amount), the charge is
  refunded (in any part) or disputed, the session is not found in this
  account, or an expired session has a succeeded payment. On a collection
  that already succeeded the state and paid evidence stay; the contradicting
  observation is kept beside them (`mismatch_reason`, `mismatch_evidence`).
  A stale mismatch on a `cancelled` collection (migration
  `0017_followups.sql`) likewise keeps `cancelled` and its evidence and
  records only the mismatch (audit `payment.mismatch`, job in attention); the
  money it may show is never hidden: a collection under reconciliation
  (`mismatch_reason` set, not succeeded) keeps the order's holds in the hold
  review exactly as `unknown` does, refuses a second collection
  (`payment_in_progress`) and reads `unknown` in the order view. An order
  released before the mismatch stays cancelled; a manager reconciles it. A
  reconcile of a collection under reconciliation that ends without a new
  mismatch (its paid, cancelled or pending verdict ignored, or the collection
  already settled; asked through `payment_under_reconciliation`) goes to
  attention, never completing or waiting quietly while the holds stay;
- **paid** only when all hold: session `payment_status = paid`; payment
  intent `succeeded`, `amount_received` = the amount, GBP; latest charge
  `paid`, `captured`, `succeeded`, `amount_captured` = the amount, not
  refunded (`refunded = false`, `amount_refunded = 0`) and not disputed. The
  authoritative success instant is the charge's `created` (provider time;
  never the webhook arrival), `observed_at` is recorded separately;
- **cancelled** (the hold review then releases the order's holds, step 7): the session
  expired and its payment intent is absent, `canceled` or
  `requires_payment_method` (no successful or unresolved funds); a later
  stale observation never reopens it;
- **pending** otherwise (open, processing, requires_capture, an expired
  session whose intent is still processing or awaiting action, unpaid,
  no_payment_required on a nonzero amount, complete without a succeeded
  payment intent or charge): the observation is recorded and the session is
  retrieved again in 15 minutes (or at the next event).

Evidence kept on the collection is bounded: IDs, statuses, amounts,
currencies and times. Network errors and 5xx retry.

**6. Paid confirmation (A3a command path).** In the transaction that records
`succeeded` (with `succeeded_at`), `payment_reconcile_record` records the
order command `paid_confirm` (actor system; receipt instant = the success
instant; exact payload = the order snapshot and the collection evidence),
reserves its K11 command operation on the order and enqueues its job, at most
once per collection (unique), however many events arrive in whatever order.
Another unresolved protected operation on the order makes the record retry
(counted: 1, 5, 15 and 60 minutes, then attention; nothing is recorded
meanwhile); an operation already at this or a later order version can never
succeed and goes to attention at once; a restore freeze stops it under its
own lock. `order_command_apply` then commits it: an order still awaiting
payment with its holds intact gets its day allocation committed, ready cakes
sold, code consumed and `paid_at` = the success instant, as `confirmed` when
the handover is at least 192 h after the success (equality allowed), or as
`paid_awaiting_agreement` otherwise (K04 Q2: the money is real; no
fulfillment; the job goes to attention so the case is never silent). An order
that changed or lost its holds is `refused` (`order_changed`,
`holds_released`): the collection stays `succeeded`, the order's version
moves, and the job goes to attention (full return is A3c2). From the moment
the payment is recorded (K11 "payment received, confirmation being secured")
until the paid confirmation's outcome envelope is secured the order reads
`confirmation_pending`; only then `confirmed` (or `paid_awaiting_agreement`)
with `paid_at`, or back to `awaiting_payment` after a refusal.

`GET /api/v1/orders/{id}` adds `payment` (the latest collection: `id`,
`state`, `amount_minor`, `currency`, `requested_at`, `checkout_url` only while
the session is open and unexpired, the order still awaits payment and no
expire request was made, `expires_at` from Stripe), and `cancel_reason` and
`cancelled_at` (step 7).

**7. Expiry, never-dispatched cleanup and release (worker, GK-025 A3c1,
migration `0015_payment_expiry.sql`).** K03/K04: the timer is a trigger to
ask and verify, never proof that the money failed; only a verified
terminal-unpaid situation releases holds and code uses; anything unknown keeps
them.

- **Expire request at 15 minutes.** The collection's reconcile (the one the
  dispatch schedules 15 minutes after the session is created, or any later
  one) first calls `payment_expire_admit`: when the collection is `pending`,
  its session was last seen `open`, Stripe's own `expires_at`
  (`provider_expires_at`) has not passed (after it Stripe has expired the
  session itself; migration `0017_followups.sql`), the first admitted call
  (`dispatch_started_at`) is at least 15 minutes old and no expire request was
  answered yet, it records `expire_requested_at` (a concurrent reconcile
  within 30 seconds skips) and the worker posts
  `POST /v1/checkout/sessions/{id}/expire` with `Idempotency-Key:
  <collection>-expire`. `payment_expire_record` appends the answer to
  `business.payment_attempt` (kind `expire`): `expired` (200), `not_open`
  (400 `invalid_request_error`: the session is already complete or expired),
  `rejected` (404, other 4xx) end the expire requests; `refused` (401/403)
  goes to attention; after `unknown` (no answer, 5xx) or `deferred` (409,
  429) the reconcile still retrieves and records its verdict, and a session
  still pending is asked again with the same key a minute later (Stripe
  replays its first answer; a deferral, not a counted failure). Once an
  expire request was made the order view no longer offers the checkout link.
  Whatever Stripe answers, the reconcile retrieves and verifies the session
  as in step 5 and only that verdict counts: a payment still processing stays `pending` (no second
  expire request; re-checked every 15 minutes or at the next event), a paid
  one is confirmed, an expired one with no or a failed payment intent is
  `cancelled`. Verified on the sandbox on 2026-11-04 with
  `gk-stripe-proof`: an open session answers 200 `expired`, the same key
  again replays that 200, and a new request for the expired session answers
  400 `invalid_request_error`.
- **Never-dispatched cleanup.** A collection still `created` whose Stripe
  call was never admitted (`dispatch_started_at` null: no key, journal down, a
  backlog) is closed by the hold review once 15 minutes old (the later of
  `initiated_at` and its insertion): `cancelled` with
  `cancel_reason = never_dispatched`, in one transaction under the order and
  collection locks. `payment_dispatch_admit` records `dispatch_started_at`
  under the same collection lock and now answers `closed` for such a
  collection, so exactly one side wins: the cleanup first means zero Stripe
  calls (the dispatch records a `rejected` outcome with reason
  `never_dispatched` without calling, or simply completes when its request
  was never fixed); admission first means the cleanup leaves it alone and the
  normal reconcile decides. A collection whose call was admitted is never
  closed this way.
- **Hold review and release.** Every order gets a hold review (AFTER INSERT
  trigger on `business.orders`; table `business.order_hold_review` and its
  jobs in `business.order_hold_review_job`, at most one waiting per order),
  due when its hold ends: `hold_until` = the later of `placed_at` and the
  insertion instant + 15 minutes (at or after it the hold has ended). A
  collection becoming `cancelled` or `failed` brings it forward (never before
  `hold_until`). `order_hold_review_run` (lease-fenced; it needs neither Stripe
  nor the journal) re-checks everything under the order lock (lock order: the
  order, its collections by ID, the review row, then the day, lots by ID and
  the code, as checkout and the order commands). A restore freeze defers it
  first (5 minutes), before any lock or outcome, an already decided review
  included: nothing is recorded or audited while frozen (migration
  `0019_hold_review_freeze.sql`). Then an already decided review ends;
  an order no longer awaiting payment ends the review; a `succeeded`
  collection ends it too (the money is real: confirmation or a full return
  decide, never a release); before `hold_until` it waits; it closes never-dispatched collections; while a
  collection is `created` (admitted), `pending` or `unknown` or under
  reconciliation (`mismatch_reason` set), or a protected
  operation on the order (a free or paid confirmation) is not yet secured, it
  keeps the holds and looks again in 15 minutes (or when a young
  never-admitted collection reaches 15 minutes). Otherwise every collection
  is `failed` or `cancelled` (or there is none: nobody started paying or asked
  for the free confirmation) and the order is abandoned: its day allocation
  is released, reserved ready cakes return to their lot (reservation
  `returned`), the code use is `released`, and the order becomes `cancelled`
  with `cancel_reason` `payment_not_completed` (full payment) or
  `free_not_confirmed` (free order), `cancelled_at` and version + 1, audited as
  `order.released_unpaid`. A free order whose free confirmation is pending is
  kept; a refused one is released at the next review.
- **After a release** a new payment is refused (`STATE_CONFLICT`: the order no
  longer awaits payment). A success observed later for one of its collections
  (real funds) is still recorded as `succeeded` with its evidence; its paid
  confirmation is refused (`order_changed`), the order stays `cancelled` and
  the job goes to attention; because the order is cancelled, the full return
  is recorded in the same transaction (see "Full return" below). The order view
  shows `lifecycle: cancelled` with `cancel_reason` and `cancelled_at`, and
  nothing implies a refund. While a collection is still `pending` past its 15
  minutes the view keeps `payment.state: pending` (the UI says "Still checking
  payment; do not pay again").

Recorded decisions: the storefront sends the customer to pay (or to confirm a
free order) right after checkout, so an order with no payment started 15
minutes after placement is abandoned; coming back later means a new quote and
checkout. The 15-minute clocks of the hold and of the cleanup use the later of
the API's request instant and the database insertion instant, so a skewed (or
test) request clock never shortens them. The expire request is timed from the
first admitted call (K04 "local attempt admitted + 15m"), at most one
answered request per collection.

**Sandbox proof.** `STRIPE_SECRET_KEY=sk_test_... STRIPE_ACCOUNT_ID=acct_... go run ./cmd/gk-stripe-proof`
creates one session twice under one idempotency key through the worker's
client (one session), retrieves and verifies it (pending), expires it under
the worker's expire key (200 `expired`; the same key again replays it; a new
request for the expired session is 400 `invalid_request_error`: not open),
verifies it again (cancelled) and shows that a changed request under the used
key is an idempotency conflict; it refuses live keys and prints IDs only.
Passed against account `acct_1UNHEgPqGToF2ulH` on 2026-11-04, the expire
steps the same day with the A3c1 build (the same day the sandbox also
showed that `expires_at` = creation + 30 minutes is accepted with about a
minute of tolerance and refused beyond it, which is why the request is fixed
at the first dispatch with two hours of lifetime, not at the initiation).

Recorded decisions: the request and its window are fixed at the first
dispatch, and the dispatch's recovery operation is reserved then (as for
email), because its replay deadline is fixed at reservation; the collection,
its key, account, environment and return origin exist from the initiation.
The customer's email is not sent to Stripe in this milestone.
`STRIPE_SECRET_KEY` lives on the worker only; the API never calls Stripe.

**Release order.** Deploy the worker of this release before (or with) the
API, and keep `CHECKOUT_ENABLED` off until the new worker runs with
`STRIPE_SECRET_KEY`/`STRIPE_ACCOUNT_ID` and the journal: an older (A3a)
worker does not know payment jobs and backs them off as unhandled (retries
1, 5, 15, 60 minutes, then attention), so a payment requested before the new
worker is deployed waits and may reach attention (requeue it with
`business.job_requeue` once the new worker runs). A new worker without a key
makes payment jobs wait without starting their windows.
With A3c1 (migration 0015) every new order also gets a hold review job, and
the migration enqueues one for each order already awaiting payment. An older
worker does not know review jobs, the `closed` dispatch status or the expire
step: it backs review jobs off as unhandled (then attention) and retries a
closed dispatch until attention, never calling Stripe for it. Deploy the
A3c1 worker before (or with) the API, and requeue any review job that
reached attention meanwhile.

### Full return (K05, GK-025 A3c2)

Migration `0016_full_return.sql`: the minimum full-return core, which
GK-026 extends (customer/chef cancellation, partial goals, the Q5 manual
alternative) without a second subsystem.

- **Goal** (`business.refund_goal`): one versioned goal per order;
  `target_minor` is the cumulative whole-order obligation (an obligation, not
  reserved money), `reason` `late_capture_terminal` (the CHECK leaves room
  for GK-026 reasons), `source` `system`/`chef`.
- **Operation** (`business.refund_operation`): one attempt to return money
  from one collection; its ID is the Stripe `Idempotency-Key` and the K11
  recovery operation (kind `dispatch`, aggregate = the operation, prior
  version 0). States `reserved` (nothing sent), `submitting` (admitted: a
  request may be at Stripe), `pending`, `requires_action`, `unknown` (the
  answer was lost or contradicts the operation), `succeeded`, `failed`
  (definitive; the remainder reopens), `cancelled` (withdrawn before any
  request). Channel `provider` (`manual_alternative` is reserved for Q5,
  unused). **At most one unresolved operation per collection** (partial
  unique index). Every create call is appended to `business.refund_attempt`.
- **Observation** (`business.refund_observation`): every refund Stripe
  reports for a collection, once per (account, mode, refund ID). One whose
  `metadata[refund_operation_id]` names a local operation of the collection
  with the same amount, currency and payment intent is matched (counted
  through the operation); any other (a dashboard refund, another amount) is
  **quarantined**: counted as exposure, and while unresolved it blocks every
  new refund dispatch for the collection (resolution columns exist; manager
  tooling: Not yet). One that names an operation but contradicts its
  amount, currency or payment intent is linked to it (`named_operation_id`,
  for reporting and manager tooling only): it cannot be that operation's
  result, so it adds to the exposure beside the operation's own allocation
  (never clipped). A matched refund
  whose status later contradicts its decided operation (Stripe correcting a
  `succeeded` refund to `failed`/`canceled`, a `failed` one turning out
  otherwise, or any refund for a withdrawn operation) records the
  contradiction on the operation: nothing becomes available, no automatic
  payout or reopening, the order view says `needs_attention` and the worker
  asks a manager (every reconcile also re-checks succeeded and failed
  operations against Stripe's list). A refund matched to a failed or
  withdrawn operation still counts by its own status (succeeded in S,
  pending in R).
- **Accounting** (`business.refund_balance`, `refund_available`), per
  succeeded collection C: S = succeeded local operations + succeeded
  observations not counted through an operation (unmatched, or matched to a
  failed/withdrawn one); R = unresolved local operations + pending such
  observations; each provider refund counted once. A new
  operation may take min(max(0, C − S − R), goal remaining) and only with no
  unresolved operation, no unresolved quarantined observation and no
  contradicting evidence on the collection. The K05 fixture (10000p capture,
  a local `unknown` 10000p, an external succeeded 5000p) gives allocation
  10000p, observation 5000p, available 0, possible exposure 15000p and no
  new dispatch (`TestRefundAccountingPreservesPossibleExposure`).
- **Trigger.** A refused paid confirmation on a **cancelled** order (released
  before the success was recorded) records, in the same transaction, the
  goal (target = every succeeded collection of the order; raised if more is
  captured later) and reserves one operation per succeeded collection for
  what is available, with its dispatch job. Idempotent. The order is never
  reopened or fulfilled; the paid confirmation job still goes to attention.
  A refusal on an order that is not cancelled (confirmed by another
  collection, awaiting payment after its holds were released) records no
  goal: attention only, since refunding automatically could return the
  confirmed collection too; `refund_late_capture` itself refuses a
  non-cancelled order. `paid_awaiting_agreement` creates no goal (honour or
  full return is a chef/customer decision: GK-026).
- **Dispatch (worker, `payments.Refunds`, chained in `gk-worker`).** At the
  first dispatch (after the key and journal checks) the worker builds the
  exact `POST /v1/refunds` form (`payment_intent`, `amount` in pence,
  `reason=requested_by_customer`,
  `metadata[order_id|collection_id|refund_operation_id|environment]`) and
  `refund_dispatch_fix` stores it with `replay_until` = first dispatch + 23
  hours, never rebuilt. The K11 recovery operation is reserved with that
  payload, the prepared envelope is written, and right before every call
  `refund_dispatch_admit` re-checks, under the collection and operation
  locks, the restore freeze, that no quarantined observation appeared, that
  the operation is not decided and that the window is open (then
  `dispatch_started_at`, `reserved` → `submitting`). Both
  `refund_dispatch_fix` and `refund_dispatch_admit` check the freeze first
  (share lock, migration `0020_dispatch_freeze_first.sql`): while frozen
  they answer `frozen` (deferred) even for a `closed` or `settled`
  operation. Stripe's answer:
  `succeeded` → succeeded; `pending` → pending (reconcile in 15 minutes);
  `requires_action` → attention; `failed`/`canceled` → failed + attention
  (the remainder reopens, never re-sent automatically); 400
  `invalid_request_error` → failed + attention and the collection's refunds
  are reconciled now; 400 `idempotency_error` → reconcile + attention;
  401/403 → attention; 409/429/5xx/network → `unknown`, retried with the same
  key and body (an unanswered call also schedules the collection's
  reconcile); past the window an unknown dispatch goes to reconciliation,
  never a blind second POST; a dispatch never admitted before its window
  ended is withdrawn (`cancelled`: provably nothing sent). A returned refund
  object is verified (amount, `gbp`, payment intent, mode) before it is
  trusted.
- **Reconcile** (one job per collection, at most one waiting): lists the
  payment intent's refunds completely (`GET /v1/refunds?payment_intent=…&limit=100`,
  following `has_more`; an incomplete listing concludes nothing), records
  each as an observation, matches operations by recorded refund ID or
  `metadata[refund_operation_id]` (reading a recorded refund directly with
  `GET /v1/refunds/{id}` when the list lags) and decides them. A lost-response
  operation absent from the complete list stays `unknown` (never failed on a
  timer); past its window it goes to a manager.
- **Order view.** `order_get` adds `refund` (only with a goal):
  `target_minor`, `succeeded_minor`, `in_progress_minor`, `currency`,
  `requested_at`, `goal_version` and `state` `requested` (nothing sent yet),
  `pending`, `partly_returned`, `returned` (all succeeded) or
  `needs_attention`; never `returned` while anything is unresolved, so the UI
  can tell "refund requested" from "refund succeeded". No emails yet.

Tests: `internal/checkout/refunds_integration_test.go` (accounting, trigger),
`refund_gateways_integration_test.go` (the gateway call by call, lease
fencing, verification) and `refund_worker_integration_test.go` (the worker
end to end against a fake Stripe refund API: late capture returned while the
order stays cancelled; a lost response retried with the same key and body,
one refund; unknown past the window with no match stays unknown + attention,
no second POST; pending → succeeded on its event; failed → the remainder
reopens once under a new key; a dashboard refund quarantined, blocking
dispatch; duplicate refund events counted once; a freeze defers; a
succeeded refund Stripe later reports failed needs a manager; a failed
refund Stripe later reports succeeded is counted as returned; a refund naming
an operation with another amount adds to the exposure; a refusal on a confirmed
order records no full return).

Recorded decisions: a failed refund is never re-sent automatically (a later
decision reserves the remainder: GK-026/manager); a reconcile never fails an
operation from absence; any contradiction or quarantine stops automatic
payouts for that collection until a manager resolves it. Matching a
dashboard `charge.refunded` to its collection relies on the payment intent's
metadata being copied to its charge; refund events of refunds made outside
GK Cakes without that metadata start nothing, and are found by the next
reconcile of the collection.

**Release order.** Deploy the A3c2 worker before (or with) the API; an older
worker backs refund jobs off as unhandled (then attention) and never calls
Stripe for them: requeue them with `business.job_requeue` once the new
worker runs. The staging webhook subscription needs the refund events
listed in step 4.

Not yet:

- a failed delayed payment on a completed session (its payment intent back
  to `requires_payment_method`, for example a bank debit that fails after
  the customer finished Checkout): Stripe never expires a complete session,
  so Evaluate keeps the collection `pending` and the order keeps its holds
  (the review looks again every 15 minutes) until a reviewed Evaluate rule
  or manager tooling exists;
- an unmatched refund Stripe reports `failed` or `canceled` (for example a
  failed dashboard refund) is quarantined like any other unmatched refund
  and blocks the collection's dispatch until a manager resolves it, although
  it returned no money (deliberately conservative);
- a contradiction recorded on an operation stays until manager tooling
  clears it: even a stale sighting (for example a `pending` status seen for
  a refund already recorded `succeeded`) flags the operation and blocks new
  refunds for its collection permanently until then;
- the money of a refused paid confirmation on an order that is not
  cancelled (confirmed by another collection, or awaiting payment after its
  holds were released): attention only, no goal, until manager/chef tooling
  (GK-026) decides it;
- (done in GK-026 B2, see "Chef refunds and recovery") manager tooling for the full return: resolving quarantined observations,
  clearing contradicting evidence, deciding `unknown` refunds past their
  window and re-reserving a failed refund's remainder; the Q5 manual
  alternative (`manual_alternative` channel); partial refund goals
  (GK-026); refund emails; a sandbox proof of a refund;
- visible chef cases beyond job attention (the late-payment decision itself
  is GK-026 B1 part 2); confirmation emails;
- payment-method configuration snapshot and drift detection, Q4 method
  qualification, Adaptive Pricing and presentment-currency evidence (any
  non-GBP session, intent or charge is a mismatch for now), delivery
  collections from checkout (the kind exists since 0022);
- manager reconciliation tooling for `unknown` collections and dispatches in
  `reconcile`;
- journal readiness reaching the API: initiation is refused during a restore
  freeze, but an unreachable journal only makes the worker wait.

## Cancellation (K05, GK-026 B1 part 1)

Migration `0021_cancellation.sql`. Customer and chef cancellation of an
accepted order, as a new K11 order command `cancel` through the A3a
machinery (prepared envelope before the commit, outcome envelope before it
is visible) and the A3c2 refund core (no second refund ledger). Owner rule
BO-GK-001.ready_cancellation (`docs/creator/GK-001/decisions.json`): strictly
before `full_refund_deadline_at` (original handover − 120 elapsed hours)
returns 100% of the actual paid aggregate (after discounts); at equality or
later exactly 50%; the chef's cancellation 100%; production never reduces it.

| Route (only with `CHECKOUT_ENABLED=true`) | Who | Input / answer |
| --- | --- | --- |
| `POST /api/v1/orders/{id}/cancellation-preview` | the owner (bearer) or the guest (`X-Order-Capability` with `cancel`) | no body (or `{}`); 200 `{preview_id, order_id, order_version, rate, paid_minor, amount_minor, currency, allocations[{collection_id, amount_minor}], full_refund_deadline_at, computed_at, expires_at, preview_hash}` |
| `POST /api/v1/orders/{id}/cancel` | as above | `{expected_version, preview_id, preview_hash}` with Idempotency-Key; 202 `cancellation_pending` |
| `POST /api/v1/admin/orders/{id}/cancel` | chef | `{expected_version, reason}` (1–500 characters, one line) with Idempotency-Key; 202 `cancellation_pending` |

- **Preview.** Computed by the database at the request instant: the rate,
  the paid aggregate (succeeded collections), the amount = nearest-penny
  half-up of rate × paid (a half penny goes to the customer: 9501p at 50% is
  4751p), the allocation as `refund_reserve` would reserve it, and a
  10-minute expiry. Stored immutably with the order version, the actor and a
  SHA-256 of its body.
- **Cancel.** Under the order lock, at the persisted receipt instant: the
  same actor's unexpired preview of the current version must still give the
  same rate and amount. A preview that said 100% when the receipt is at or
  after the deadline (the boundary was crossed since) is 409
  `PREVIEW_STALE` (refresh and reconfirm); so is an expired one. An unknown
  preview or wrong hash is 422 (`preview_id`). Eligible lifecycles:
  `confirmed`, `in_production`, `ready`; otherwise 409
  `CANCELLATION_NOT_ELIGIBLE` (awaiting payment keeps its release paths;
  `paid_awaiting_agreement` is the chef's late-payment decision, part 2).
  One cancellation at a time (`CONFLICT` while one is pending); the same
  actor and key replays, the same key with another body is `KEY_REUSED`.
- **Commit** (worker, `order_command_apply`): re-checks version, lifecycle
  and that the acceptance is secured (otherwise refused `order_changed`,
  version + 1 only); sets `cancelled`, `cancel_reason`
  `customer_cancelled` | `chef_cancelled`, `cancelled_at` = the receipt
  instant, releases the day place (`capacity_release`), keeps the code use
  consumed (K03), records the order's refund goal (target, reason,
  `policy_rate`, `paid_basis_minor`) and reserves the refund operations with
  `refund_reserve` (C − S − R, aggregate remainder, one unresolved operation
  per collection). The refund dispatch then runs as in the full return.
- **Read model.** Until the outcome envelope is acknowledged the order reads
  `cancellation_pending` without `cancel_reason`, `cancelled_at` or
  `refund`; `cancellation` reports the latest cancel command (`state`
  processing | cancelled | refused, `reason`, `rate`, `amount_minor`,
  `requested_by` customer | chef, `requested_at`). The refund reads
  `requested` until Stripe has it and `returned` only when every penny
  succeeded.
- A restore freeze defers the command (nothing committed); the request
  itself is still recorded.

### Production start, late payment and allocation (GK-026 B1 part 2)

Migration `0022_production_late_payment.sql`.

| Route (only with `CHECKOUT_ENABLED=true`) | Who | Input / answer |
| --- | --- | --- |
| `POST /api/v1/admin/orders/{id}/start-production` | chef or fulfillment (K00) | `{expected_version}` with Idempotency-Key; 200 `{order_id, lifecycle}` (the order's current lifecycle; `Idempotent-Replayed: true` for the same key again, also during a restore freeze, or when production had already started); a new start is 503 during a restore freeze |
| `POST /api/v1/admin/orders/{id}/late-payment` | chef | `{expected_version, decision: "honour" \| "full_return", customer_agreement: {channel, agreed_at, evidence_ref}}` (agreement required for honour, refused for full_return) with Idempotency-Key; 202 `decision_pending` |

- **Production start (K05).** One transition per order
  (`business.production_transition`, key `production_start:<order>`),
  shared by the manual start (effective at the receipt instant) and the
  timer job (worker kind `production`: due at `paid_at` or
  `free_confirmed_at` + 48 h, effective at that scheduled instant, observed
  when it ran). Whichever comes second changes nothing (the manual one
  answers as a replay). Only `confirmed` → `in_production`; no ingredient is
  deducted. The timer is scheduled when an order becomes visibly confirmed
  (secured), reads the restore freeze first, is lease-fenced, and waits
  while a cancellation or late-payment decision on the order is unresolved
  (cancel-first); a manual start then conflicts. Production never changes
  the cancellation entitlement: the rate depends only on the cancel receipt
  instant (start-first at the boundary is 50%, before it 100%). A start
  bumps the order version, so a cancellation preview held from before it
  must be refreshed (the cancel answers `STALE_VERSION`). Not a K11 command:
  it changes no customer commitment or money. Accepted risk for review: on
  a primary database loss, a manual start's effective time (and the start
  itself, until the timer runs) can be lost; the timer re-derives only the
  scheduled start (payment or free confirmation + 48 h).
- **Late payment (K04 Q2).** A K11 order command `late_payment` on a
  `paid_awaiting_agreement` order. `honour` needs the customer's agreement
  (`channel` email | phone | instagram | whatsapp | in_person | other,
  `agreed_at` not after the receipt, `evidence_ref` 1–200 characters;
  silence never counts; `agreed_at` not before the payment, else 422) and
  is refused 409 `HONOUR_TOO_LATE` once the original handover is not after
  the receipt instant. It commits `confirmed` when the order still has its
  original slot and committed holds (otherwise refused `holds_changed`); the
  production timer then follows from `paid_at` + 48 h. `full_return` cancels
  with `cancel_reason` `late_payment_returned` and a goal of 100% of the
  actual paid aggregate, never 50%, and reads like a cancellation (the
  `cancellation` block with reason `late_payment_returned`). The decision and
  agreement are stored immutably (`business.late_payment_decision`), with the
  chef as the feasibility recorder at the receipt instant.
- **Allocation (K05).** `business.refund_plan` allocates a goal over the
  order's succeeded collections in stable order (cake, delivery, ID): each
  recorded floor first, then floor + largest remainder over the rest (9501p
  cake + 2000p delivery at 50% is 5751p: 4751p + 1000p). A new reservation
  for a collection is min(share − its S + R, its room, target − all S + R),
  never while it is blocked; skewed prior actuals while money is still owed
  pause automatic allocation. Delivery refunds are chef-only (K05): a
  delivery collection is reserved automatically only under a chef-recorded
  floor; otherwise its share is a visible obligation, `delivery_pending_minor`
  in the preview and the order's `refund` block (capped by what the goal
  still needs), and the refund reads `partly_returned` until it is paid.
  `refund_reserve` and the cancellation preview use it. An unexpired
  identical preview of the same actor and order version is returned again
  instead of a new row. Payment collections may be of kind `delivery` (data only: no
  delivery checkout creates them yet).
- **Raised goal after a definitive outcome.** When an unresolved refund
  operation becomes succeeded or failed under a goal raised since it was
  reserved, the whole remaining allocation is reserved once (10000p
  captured, 2000p succeeded, 3000p unknown, goal raised to 10000p: nothing
  sent; then success → 5000p once, failure → 8000p once). A failure under an
  unchanged goal still waits for a person.
- **Delivery failure floor (interim).** `business.refund_delivery_failure_record`
  (chef, `checkout.Store.RecordDeliveryFailure`; no HTTP route yet) records
  a 100% floor on a succeeded delivery collection. The goal keeps its largest
  policy target (`policy_target_minor`) apart from floors; its target is the
  larger of the policy target and the floor plus each other collection's
  larger of its policy share and S + R, capped by paid, never lowered. So
  the order does not matter: cake 10000p + delivery 2000p at 50% with a
  delivery floor is 7000p either way; with a prior cake return of 6000p it
  is 8000p (delivery 2000p sent once).
- **Tests.** Scheduled production jobs are due by the fixed test clock, so
  the integration harness claims them only when a test runs them
  (`holdProduction`).

Not yet (the delivery share action and the agreed exception goal: GK-026 B2
below): chef-confirmed revised allocations after skewed prior actuals; the
completed/no-show chef request that creates a financial case while
preserving fulfillment history (K05); gating refund dispatch on the
cancellation being secured (refunds are reserved and dispatched from the
commit, as for the 0016 late-capture full return, each under its own K11
dispatch protection); the Q5
manual alternative payout; manager recovery tools (quarantine, contradicting
evidence, `unknown` past its window, re-reserving a failed remainder under an
unchanged goal); an HTTP route for the delivery failure and delivery
collections from checkout; returning a sold ready cake to stock on
cancellation; staff order reads; cancellation, production and refund emails;
the UI (B2/B3).

## Chef refunds and recovery (K05, GK-026 B2)

Part 1: migrations `0023_b1_followups.sql` and `0024_chef_refunds.sql`.
Both routes reuse the A3c2 ledger (`refund_goal`, `refund_operation`, the
dispatch worker and its K11 protection per operation); there is no second
refund ledger.

| Route (only with `CHECKOUT_ENABLED=true`) | Who | Input / answer |
| --- | --- | --- |
| `POST /api/v1/admin/orders/{id}/refunds` | chef | `{expected_version, amount_minor, currency: "GBP", allocations: [{collection_id, amount_minor}], reason}` with Idempotency-Key; 202 `{order_id, operation_id (the exception), state: "refund_pending", poll_after_seconds}` |
| `POST /api/v1/admin/orders/{id}/delivery-refund` | chef | `{expected_version, collection_id}` with Idempotency-Key; 202 `{order_id, operation_id (the refund operation), state: "refund_pending", poll_after_seconds}` |

- **Agreed exception (partial refund goal).** `amount_minor` 1–100000000
  pence; 1–10 allocations, each a distinct succeeded collection of the order
  (lower-case UUID), at least 1p, adding up to the amount (otherwise 422
  `allocations`); reason 1–500 characters on one line (kept in
  `business.refund_exception`, never in the audit detail or logs). Under the
  order lock: 409 `RECONCILIATION_REQUIRED` while the order has an
  unresolved quarantined refund observation, contradicting evidence on a
  refund operation or payment collection, a refund in `requires_action`, an
  unresolved refund whose dispatch is in K11 `reconcile`, or a payment
  collection in `unknown`; 409 `REFUND_IN_PROGRESS` while any refund of the
  order is unresolved (the explicit allocation is reserved against settled
  balances, so no automatic reservation splits it); 409 `REFUND_LIMIT` when
  the amount exceeds paid − S − R over the order or an allocation exceeds
  its collection's room (C − S − R). Then the goal target becomes
  min(paid, max(target, S + R) + amount), version + 1 (a new goal has reason
  `chef_exception`), exactly the allocations are reserved as refund
  operations under that version, the order version is bumped and the
  exception is stored immutably. It is an obligation on top of what is owed:
  a pending ordinary share stays pending. A later cancellation nets it (K05:
  the policy target is cumulative): exceptions of 9500p on a 12000p order
  cover a 50% cancellation (6000p), which then raises only the policy target
  (audited `refund.policy_target_raised`) and sends nothing.
- **Pending delivery share.** Reserves, for one unfloored succeeded delivery
  collection, what `delivery_pending_minor` reports for it: min(its share −
  its S + R, its room, what the goal still needs after everything returned,
  reserved or automatically reservable). 404 for a collection of another
  order, 409 `CONFLICT` for a cake or unpaid collection, 409
  `NO_REFUND_PENDING` when nothing is pending, `REFUND_IN_PROGRESS` while
  that collection has an unresolved refund, `RECONCILIATION_REQUIRED` as
  above. Cake 9501p + delivery 2000p at 50%: the cancellation sends 4751p,
  the chef's delivery refund 1000p once, and the refund reads `returned`.
- **Both:** chef only (403 otherwise); 409 `STALE_VERSION`; the same key and
  body replays (`Idempotent-Replayed: true`, also during a restore freeze),
  another body is `KEY_REUSED`; a new request during a restore freeze is 503
  and records nothing; 409 `CONFLICT` while a K11 command of the order is
  not secured or a cancellation/late-payment decision is unresolved. Not
  K11 order commands themselves (no customer commitment changes); each
  reserved operation is protected under K11 before its first Stripe call.
  Audited: `refund.exception_recorded` / `refund.delivery_paid`,
  `refund.goal_recorded` / `refund.goal_raised`, `refund.reserved`.
- **B1 follow-ups (0023).** After a failed refund the order's refund reads
  `needs_attention` only when the goal lacks money beyond what is returned,
  reserved and `delivery_pending_minor` (a pending chef delivery action is
  not a failure); a policy target raised without raising the goal is audited
  `refund.policy_target_raised`; the commit-time `honour_too_late` refusal
  is audited `order.late_payment_refused`.
- **Tests:** `internal/checkout/recovery_integration_test.go` (real
  PostgreSQL, fake Stripe), `internal/httpapi` `TestChefRefundRoutes`.

### Manager recovery tools (GK-026 B2 part 2, migration 0025)

| Route (only with `CHECKOUT_ENABLED=true`) | Who | Input / answer |
| --- | --- | --- |
| `POST /api/v1/admin/orders/{id}/refund-observations/{observation_id}/resolve` | chef | `{expected_version, resolution: "match" \| "accept_external", refund_operation_id (match only), note}`; 200 `{order_id, decision_id}` |
| `POST /api/v1/admin/orders/{id}/refund-operations/{operation_id}/decide` | chef | `{expected_version, decision: "failed" \| "succeeded", provider_refund_id (succeeded only), note}`; 200 |
| `POST /api/v1/admin/orders/{id}/refund-operations/{operation_id}/clear-mismatch` | chef | `{expected_version, decision: "keep" \| "failed" \| "succeeded", note}`; 200 |
| `POST /api/v1/admin/orders/{id}/payment-collections/{collection_id}/clear-mismatch` | chef | `{expected_version, note}`; 200 |
| `POST /api/v1/admin/orders/{id}/refund-operations/{operation_id}/re-reserve` | chef | `{expected_version, note?}`; 202 `refund_pending` |
| `GET /api/v1/admin/attention` | chef; technical support (no amounts, audited `attention.viewed`) | 200 `{items: [{kind, order_id, collection_id, refund_operation_id, observation_id, job_id, reason, amount_minor, currency, window_ended, since}]}` |

Every decision: Idempotency-Key (replays also during a restore freeze;
another body `KEY_REUSED`), `expected_version` (the order version is
bumped), 503 for a new decision during a restore freeze, `CONFLICT` while a
K11 command of the order is unresolved, the note (1–500 characters, one
line) stored immutably in `business.refund_recovery_decision` and kept out
of the audit (`refund.recovery_decided`). No decision calls Stripe. A
decision completes the order's refund jobs in attention that it settled (a
dispatch now decided without contradiction, a reconcile whose collection
has nothing unresolved, quarantined or contradicted; audited
`refund.attention_settled`), and the attention list never shows such jobs.
When a decision settles an operation whose dispatch K11 record is still
prepared or in reconcile, a `refund_resolve` job (worker kind
`refund_resolve`, after `refund`) records the outcome `manager_failed` |
`manager_succeeded` (evidence: the decision ID and state) in the
independent journal with `recovery.Coordinator.Resolve`, outside any
transaction, and secures the record; a record already committed with
another outcome is left alone. Deploy the worker with (or before) the API
for this: an older worker backs these jobs off as unhandled.

- **Resolve a quarantined observation.** `match` only when the identities
  prove it (the refund's metadata names the operation or the operation
  records that refund ID; same amount, `gbp`, the collection's payment
  intent and Stripe account; no other refund matched to it; the operation
  unresolved): the operation is settled by the refund's own status;
  otherwise 409 `IDENTITY_UNPROVEN`. `accept_external` keeps the observation
  (never deleted), lifts its block and counts its money as returned by its
  own status; nothing new is reserved.
- **Decide an `unknown` refund** only past its replay window (409
  `WINDOW_OPEN`). `failed` needs complete-provider-list evidence of
  non-execution (`business.refund_not_executed_proven`, migration 0027): a
  complete reconcile listing after the window found nothing
  (`refund_operation.not_found_at` ≥ `replay_until`; only the reconcile's
  not_found verdict, which follows a complete list, sets it), no
  observation names, matches or links the operation, it records no refund
  ID, carries no contradiction and no refund object in its evidence (409
  `EVIDENCE_REQUIRED`). `succeeded` needs the Stripe refund ID: an observed
  refund of that ID is matched at once only when Stripe's record proves the
  identity (its metadata names the operation, or the operation already
  records that ID); otherwise the ID is recorded and the collection's
  reconcile runs now, which verifies it at Stripe (complete list, then
  `GET /v1/refunds/{id}`: amount, currency, payment intent) and matches an
  unmatched observation of that ID to the operation (counted once) before
  the operation is decided. An ID held by another operation, or observed on
  another collection, is 409 `DUPLICATE`.
- **Clear a contradiction.** On a refund operation: `keep` (the recorded
  state stands), `failed` (its remainder reopens, nothing is re-sent): on an
  unknown refund exactly the proof of a `failed` decision above (409
  `WINDOW_OPEN`, then `EVIDENCE_REQUIRED`), on a succeeded refund only when
  Stripe's record of that same refund (matched, same ID) is failed or
  canceled; `succeeded` (only with a succeeded refund of the same ID and
  amount matched to it, else `EVIDENCE_REQUIRED`). On a payment collection: only when it is succeeded
  or failed (an unknown payment outcome is payment reconciliation, not a
  note).
- **Re-reserve a failed remainder.** The latest operation of its collection,
  a failed provider refund; min(its amount, the collection's room, what the
  goal still needs) on the same collection (so a failed agreed-exception
  refund returns to the collection the chef chose); `REFUND_IN_PROGRESS`
  while anything is unresolved on the collection, `RECONCILIATION_REQUIRED`
  while anything on the order needs reconciliation, `NO_REFUND_PENDING` at
  0, `CONFLICT` once superseded.
- **Attention list.** Jobs in attention (their reason; the order when the
  job belongs to one), unresolved quarantined observations, contradictions
  on refunds and payment collections, unknown or requires_action refunds
  (`window_ended`), failed refunds whose remainder is still owed
  (`refund_failed_owed`, with the re-reservable amount), pending delivery
  shares and unresolved manual payouts. Fulfillment has no access.
- `refund_chef_admit` (B2 part 1) no longer requires a `secured` recovery
  state (a never-confirmed order is `prepared`): it refuses only pending
  confirmation/cancellation and unsecured K11 commands.
- K05 card fixture (`TestCardFixtureResolvedByEvidence`): 10000p collection,
  local unknown 10000p, external 5000p: match refused, the external 5000p
  accepted (S 5000p, R 10000p, nothing sent), the unknown failed with the
  complete-list evidence, the remainder re-reserved: exactly 5000p sent
  once, the order reads `returned`.

### Q5 manual alternative payout (GK-026 B2 part 2, migration 0026)

| Route (only with `CHECKOUT_ENABLED=true`) | Who | Input / answer |
| --- | --- | --- |
| `POST /api/v1/admin/orders/{id}/manual-payouts/preview` | chef | `{collection_id, unavailable_evidence_ref, recipient_evidence_ref}` (no key); 200 `{preview_id, order_id, order_version, collection_id, failed_refund_operation_id, amount_minor, currency, unavailable_evidence_ref, recipient_evidence_ref, computed_at, expires_at, preview_hash}` |
| `POST /api/v1/admin/orders/{id}/manual-payouts` | chef | `{expected_version, preview_id, preview_hash}` with Idempotency-Key; 202 `payout_pending` (operation_id = the K11 command) |
| `POST /api/v1/admin/orders/{id}/manual-payouts/{refund_operation_id}/outcome` | chef | `{expected_version, outcome, transfer_identity?, evidence_ref?, amount_minor?, currency?}` with Idempotency-Key; 202 `outcome_pending` |
| `GET /api/v1/admin/orders/{id}/manual-payouts` | chef | 200 `{items: [{refund_operation_id, collection_id, state, amount_minor, currency, unavailable_evidence_ref, instructions, submitted_at, transfer_identity, decided_at, outcome_pending}]}` |

- **Eligible only after definitive provider unavailability:** the
  collection's latest decided refund is a definitive failure (and a provider
  refund failed definitively on it), nothing unresolved
  (`REFUND_IN_PROGRESS`), no reconciliation outstanding; otherwise 409
  `MANUAL_PAYOUT_NOT_ELIGIBLE`.
- **References only.** Evidence, recipient and transfer references: 1–200
  visible characters; once spaces, hyphens and dots are removed, no run of
  six or more digits and nothing IBAN-like (two letters, two digits, 11–30
  letters or digits) (422); the evidence itself lives outside this system.
  Never logged.
- **Preview:** immutable for 10 minutes and the order version; the whole
  remaining allocation (min(the failed refund's amount, the room, what the
  goal still needs)); a changed recipient or amount is a new preview.
- **Reservation:** a K11 order command `manual_payout` (prepared envelope
  before the commit, outcome envelope before it is visible). The commit
  re-checks everything under the order and collection locks and reserves
  one refund operation with channel `manual_alternative` for the whole
  amount (no dispatch job; the one-unresolved-per-collection rule blocks
  provider refunds and other manual payouts), or refuses (`order_changed`,
  `not_eligible`, `amount_changed`). The list shows `instructions` (amount,
  GBP, recipient evidence reference, payment reference = the operation ID)
  only once the reservation's outcome is secured (`protecting` before).
- **Outcomes** (K11 order command `manual_payout_outcome`): `submitted`
  (reserved → pending), `unknown` (pending → unknown: blocks every retry and
  channel), `succeeded` (pending/unknown → succeeded; a transfer identity
  unique across all manual payouts (409 `DUPLICATE`), an evidence reference,
  exactly the reserved amount in GBP), `failed` (pending/unknown → failed
  with evidence: the remainder is owed again, a new manual payout may
  follow), `withdrawn` (reserved → cancelled: nothing was paid; needs an
  evidence reference). Each is in
  `business.manual_payout_event`. A success counts once (the operation's
  S, no Stripe refund ID invented); until its outcome is secured the order
  view reports it in progress, never `returned`.
- **Restore freeze:** like the B1 order commands, the preview and the
  request do not check the freeze (the request is recorded); the commit of
  each K11 command checks it first and waits while frozen, so nothing is
  reserved or recorded during a restore.
- **Tests:** `internal/checkout/recovery_tools_integration_test.go`,
  `manual_payout_integration_test.go`; `internal/httpapi`
  `TestRecoveryToolRoutes`, `TestManualPayoutRoutes`.

Not yet (after B2): the UI for all of this; refund and payout emails or
notifications; protected storage of the evidence the references point to;
journal resolution of a dispatch record that a reconcile (not a manager)
decided after the window (it stays `reconcile`, as before B2);
foreign-currency manual payouts (liability stays open);
re-reserving a remainder by policy shares across collections (the
remainder returns to the failed operation's collection); payment
reconciliation of an `unknown` payment collection; chef-confirmed revised
allocations after skewed prior actuals; staff order reads (the chef needs
the order's `expected_version`); a raised goal settling a manual payout
reserves the remainder through the provider (it fails again if the
provider is still unavailable, then needs another decision).

## Not yet verified

- Runtime-role denial and the gateway on the sandbox. They are proven on
  PostgreSQL 17.11 only.
- Whether Supabase's pooler forwards the `statement_timeout`/`lock_timeout`
  startup parameters (direct connections do).
