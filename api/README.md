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

### Secret inventory (proposed until GK-013/GK-034 configure the stores)

Values never enter Git, Trello, logs or the UI. Owner for every entry: Creator
(Sasha), who holds the provider accounts.

| Secret                   | Scope                               | Storage location                                  | Rotation                                                        | Revocation                                   |
| ------------------------ | ----------------------------------- | ------------------------------------------------- | --------------------------------------------------------------- | -------------------------------------------- |
| `DATABASE_URL` (api)     | `gk_api` login, one per environment | API service variables (Railway, per environment)  | `ALTER ROLE <login> PASSWORD`, update the variable, redeploy    | `ALTER ROLE <login> NOLOGIN` or drop it      |
| `DATABASE_URL` (worker)  | `gk_worker` login, one per environment | worker/cron service variables                  | as above                                                        | as above                                     |
| `MIGRATION_DATABASE_URL` | migration credential                | protected CI migration environment only           | rotate the password of the same role (see below)                | rotate; runtime services never hold it       |
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
```

`gk-migrate` applies files once, in order, under an advisory lock. Applied files
are immutable (checksummed), and an older build refuses to run against a database
with newer migrations. Never add destructive down migrations.
