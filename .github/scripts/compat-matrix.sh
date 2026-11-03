#!/usr/bin/env bash
# GK-034 AC2: previous/current compatibility matrix for one staging deploy.
# "deployed" is what staging runs now, "candidate" what is about to deploy; in a
# rollback the candidate is the older build. Both builds run against one
# disposable PostgreSQL, migrated by a non-superuser database owner (as on
# Supabase), in deploy order:
#   1. deployed gk-migrate on an empty database; runtime logins; a seeded job;
#      deployed API ready.
#   2. candidate gk-migrate. Business tables keep their rows, and the business
#      schema keeps every column (type, NOT NULL), column default, function
#      (signature, SECURITY DEFINER) and runtime-group privilege it had:
#      migrations are expand-only (additions are fine, changes are not), so
#      the deployed build keeps working (rolling deploy, rollback target). A run
#      that applies nothing must change nothing. The deployed API stays ready.
#   3. candidate API ready on the result.
#   4. deployed gk-migrate again (the other direction): succeeds and changes
#      nothing.
#   5. both API builds allow the UI origin and refuse a foreign one (CORS).
#   6. both gk-worker builds claim and release a job, freshly enqueued through
#      business.job_enqueue on the result, via job_claim / job_release without
#      errors, leaving it in retry without a lease.
#   7. both UI builds serve /healthz and the home page.
# API readiness is a database ping, and the UI calls no API endpoint yet: features
# that add UI->API calls or new runtime database use extend these checks.
#   compat-matrix.sh   (env: DEPLOYED_API CANDIDATE_API DEPLOYED_UI CANDIDATE_UI, image@sha256 refs)
set -euo pipefail

for v in DEPLOYED_API CANDIDATE_API DEPLOYED_UI CANDIDATE_UI; do
  if [[ ! "${!v:-}" =~ ^[a-z0-9.:/_-]+@sha256:[0-9a-f]{64}$ ]]; then
    echo "compat: $v must be an image@sha256 reference (got '${!v:-}')" >&2
    exit 1
  fi
done

pg_image=postgres:17.11-alpine@sha256:266d3c257594e195b309ae9d1805e54c788f1d7b375f56bf6c367aac6c917e7b
prefix=gk-compat-$$
pg=$prefix-pg
ui_origin=https://ui.compat.invalid
api_port_deployed=18081
api_port_candidate=18082
ui_port_deployed=13001
ui_port_candidate=13002
summary_file=${GITHUB_STEP_SUMMARY:-/dev/null}
work=$(mktemp -d)

# Names of this run's containers (the name filter matches substrings).
containers() { docker ps -a --filter "name=$prefix-" --format '{{.Names}}'; }
cleanup() {
  local names=() name
  while read -r name; do
    [[ -z "$name" ]] || names+=("$name")
  done < <(containers)
  ((${#names[@]} == 0)) || docker rm -f "${names[@]}" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

fail() {
  echo "compat: FAIL: $*" >&2
  echo "- **FAIL**: $*" >>"$summary_file"
  local name
  while read -r name; do
    [[ -n "$name" && "$name" != "$pg" ]] || continue
    echo "--- last log lines of $name" >&2
    docker logs --tail 30 "$name" >&2 2>&1 || true
  done < <(containers)
  exit 1
}
pass() {
  echo "compat: ok: $*"
  echo "- ok: $*" >>"$summary_file"
}

url_for() { echo "postgres://$1:compat@127.0.0.1:5432/compat?sslmode=disable"; }
psql_q() { docker exec -i "$pg" psql -v ON_ERROR_STOP=1 -qAt -U postgres -d "${1:-compat}"; } # SQL on stdin

# Business table rows (count and content fingerprint) and migration history.
data_snapshot() {
  psql_q <<'SQL'
SELECT coalesce(string_agg(format('%s=%s/%s', c.relname,
         (xpath('/row/n/text()', query_to_xml(format('SELECT count(*) AS n FROM business.%I', c.relname), false, true, '')))[1]::text,
         md5(query_to_xml(format('SELECT * FROM business.%I ORDER BY 1', c.relname), false, true, '')::text)),
       ',' ORDER BY c.relname), '')
       || '|' || (SELECT coalesce(string_agg(version, ',' ORDER BY version), '') FROM gk_meta.schema_migration)
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'business' AND c.relkind = 'r';
SQL
}

# The business schema contract older code relies on, one line per object:
# columns (type, NOT NULL), column defaults (separate lines, so adding one is
# compatible), functions (signature, result, SECURITY DEFINER) and the runtime
# groups' effective privileges (explicit, via PUBLIC or membership).
schema_snapshot() {
  psql_q <<'SQL' | LC_ALL=C sort
WITH rel AS (
  SELECT c.oid, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'business' AND c.relkind IN ('r', 'p', 'v', 'm')
), fn AS (
  SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'business'
), grp(g) AS (VALUES ('gk_api'), ('gk_worker'), ('gk_backup'))
SELECT format('column %s.%s %s%s', r.relname, a.attname, format_type(a.atttypid, a.atttypmod),
              CASE WHEN a.attnotnull THEN ' not null' ELSE '' END)
FROM pg_attribute a JOIN rel r ON r.oid = a.attrelid
WHERE a.attnum > 0 AND NOT a.attisdropped
UNION ALL
SELECT format('default %s.%s %s', r.relname, a.attname, pg_get_expr(d.adbin, d.adrelid))
FROM pg_attrdef d JOIN rel r ON r.oid = d.adrelid
JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum AND NOT a.attisdropped
UNION ALL
SELECT format('function %s returns %s%s', fn.oid::regprocedure, pg_get_function_result(fn.oid),
              CASE WHEN p.prosecdef THEN ' security definer' ELSE '' END)
FROM fn JOIN pg_proc p ON p.oid = fn.oid
UNION ALL
SELECT format('grant %s %s on business.%s', grp.g, pr.p, r.relname)
FROM rel r CROSS JOIN grp
CROSS JOIN (VALUES ('SELECT'), ('INSERT'), ('UPDATE'), ('DELETE'), ('TRUNCATE'), ('REFERENCES'), ('TRIGGER')) AS pr(p)
WHERE has_table_privilege(grp.g, r.oid, pr.p)
UNION ALL
SELECT format('grant %s EXECUTE on %s', grp.g, fn.oid::regprocedure)
FROM fn CROSS JOIN grp
WHERE has_function_privilege(grp.g, fn.oid, 'EXECUTE')
UNION ALL
SELECT format('grant %s USAGE on schema business', grp.g)
FROM grp
WHERE has_schema_privilege(grp.g, 'business', 'USAGE');
SQL
}

# rows_kept <before> <after>: every business table still exists with at least
# as many rows. Prints the first problem.
rows_kept() {
  local entry table count now
  IFS=',' read -ra entries <<<"${1%%|*}"
  for entry in "${entries[@]}"; do
    [[ -n "$entry" ]] || continue
    table=${entry%%=*}
    count=${entry#*=}
    count=${count%%/*}
    now=$(tr ',' '\n' <<<"${2%%|*}" | awk -F'[=/]' -v t="$table" '$1 == t { print $2 }')
    if [[ -z "$now" ]]; then
      echo "table business.$table was removed"
      return 1
    elif ((now < count)); then
      echo "business.$table lost rows ($count -> $now)"
      return 1
    fi
  done
}

migrate_with() { # migrate_with <image>: prints gk-migrate's output
  docker run --rm --platform linux/amd64 --network "container:$pg" -e APP_ENV=test \
    -e MIGRATION_DATABASE_URL="$(url_for gk_migrator)" --entrypoint /usr/local/bin/gk-migrate "$1" 2>&1
}

start_api() { # start_api <name> <image> <port>
  docker run -d --platform linux/amd64 --name "$prefix-api-$1" --network "container:$pg" -e APP_ENV=test -e PORT="$3" \
    -e DATABASE_URL="$(url_for gk_api_compat)" -e UI_ORIGIN="$ui_origin" "$2" >/dev/null
}

start_ui() { # start_ui <name> <image> <port>
  docker run -d --platform linux/amd64 --name "$prefix-ui-$1" -p "127.0.0.1:$3:3000" -e PORT=3000 \
    -e ORIGIN="http://127.0.0.1:$3" "$2" >/dev/null
}

wait_200() { # wait_200 <url> <body substring> <what>
  local body
  for _ in $(seq 1 60); do
    if body=$(curl -fsS --max-time 5 "$1" 2>/dev/null) && [[ "$body" == *"$2"* ]]; then
      return 0
    fi
    sleep 1
  done
  fail "$3: $1 did not answer 200 with '$2'"
}

allow_origin() { # allow_origin <port> <origin>: the response's Access-Control-Allow-Origin
  local headers
  headers=$(curl -sS --max-time 5 -o /dev/null -D - -H "Origin: $2" "http://127.0.0.1:$1/healthz") ||
    fail "CORS request to port $1 failed"
  tr -d '\r' <<<"$headers" | awk -F': ' 'tolower($1) == "access-control-allow-origin" { print $2 }'
}

cors_ok() { # cors_ok <port> <what>
  local allowed foreign
  allowed=$(allow_origin "$1" "$ui_origin")
  foreign=$(allow_origin "$1" "https://not-gk.invalid")
  [[ "$allowed" == "$ui_origin" && -z "$foreign" ]] ||
    fail "$2: CORS allowed '$allowed' for the UI origin and '$foreign' for a foreign one"
}

# enqueue <when>: enqueue a job through business.job_enqueue on the current
# schema (as a domain command would) and print its id.
enqueue() {
  psql_q <<SQL
BEGIN;
SET LOCAL ROLE gk_owner;
SELECT business.job_enqueue(gen_random_uuid(), $1);
COMMIT;
SQL
}

worker_ok() { # worker_ok <name> <image>: the worker claims and releases a fresh job, error-free
  local job logs="" code state
  job=$(enqueue "NULL") || fail "enqueue through business.job_enqueue on the resulting schema failed"
  docker run -d --platform linux/amd64 --name "$prefix-worker-$1" --network "container:$pg" -e APP_ENV=test \
    -e DATABASE_URL="$(url_for gk_worker_compat)" --entrypoint /usr/local/bin/gk-worker "$2" >/dev/null
  for _ in $(seq 1 30); do
    logs=$(docker logs "$prefix-worker-$1" 2>&1)
    [[ "$logs" == *'"msg":"job failed; scheduling retry"'* || "$logs" == *'"level":"ERROR"'* ]] && break
    sleep 1
  done
  docker stop -t 20 "$prefix-worker-$1" >/dev/null
  code=$(docker inspect -f '{{.State.ExitCode}}' "$prefix-worker-$1")
  logs=$(docker logs "$prefix-worker-$1" 2>&1)
  [[ "$logs" != *'"level":"ERROR"'* ]] || fail "$1 gk-worker logged an error: $(grep '"level":"ERROR"' <<<"$logs" | head -1)"
  [[ "$logs" == *'"msg":"job failed; scheduling retry"'* ]] || fail "$1 gk-worker did not claim and release the job"
  [[ "$code" == 0 ]] || fail "$1 gk-worker exited $code on SIGTERM"
  state=$(psql_q <<<"SELECT state || '/' || (lease_owner IS NULL)::text FROM business.job WHERE id = '$job';")
  [[ "$state" == retry/true ]] || fail "$1 gk-worker left job $job as '$state', want retry/true (released, no lease)"
}

{
  echo "### Compatibility matrix"
  echo "- deployed API \`$DEPLOYED_API\`, candidate API \`$CANDIDATE_API\`"
  echo "- deployed UI \`$DEPLOYED_UI\`, candidate UI \`$CANDIDATE_UI\`"
} >>"$summary_file"

docker run -d --name "$pg" -e POSTGRES_PASSWORD=compat \
  -p "127.0.0.1:$api_port_deployed:$api_port_deployed" -p "127.0.0.1:$api_port_candidate:$api_port_candidate" \
  "$pg_image" >/dev/null
# Over TCP: the image's init-time server answers on the socket only.
for _ in $(seq 1 60); do
  docker exec "$pg" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$pg" pg_isready -h 127.0.0.1 -U postgres >/dev/null || fail "PostgreSQL did not start"
psql_q postgres <<'SQL' >/dev/null
CREATE ROLE gk_migrator LOGIN CREATEROLE PASSWORD 'compat';
CREATE DATABASE compat OWNER gk_migrator;
SQL

# 1. Staging as it is.
out=$(migrate_with "$DEPLOYED_API") || fail "deployed gk-migrate on an empty database: $out"
psql_q <<'SQL' >/dev/null || fail "creating the runtime logins failed"
CREATE ROLE gk_api_compat LOGIN PASSWORD 'compat' IN ROLE gk_api;
CREATE ROLE gk_worker_compat LOGIN PASSWORD 'compat' IN ROLE gk_worker;
SQL
# Not claimable before step 6, so the API steps see stable data.
enqueue "now() + interval '1 day'" >/dev/null || fail "enqueue through business.job_enqueue on the deployed schema failed"
start_api deployed "$DEPLOYED_API" "$api_port_deployed"
wait_200 "http://127.0.0.1:$api_port_deployed/readyz" '"status":"ready"' "deployed API on the deployed schema"
data_before=$(data_snapshot)
schema_snapshot >"$work/schema-before"
pass "deployed API ready on its own schema (seeded: ${data_before%%|*})"

# 2. Candidate migrations while the deployed API keeps serving.
out=$(migrate_with "$CANDIDATE_API") || fail "candidate gk-migrate on the deployed schema: $out"
data_after=$(data_snapshot)
schema_snapshot >"$work/schema-after"
problem=$(rows_kept "$data_before" "$data_after") || fail "candidate migrations: $problem"
removed=$(LC_ALL=C comm -23 "$work/schema-before" "$work/schema-after")
[[ -z "$removed" ]] || fail "candidate migrations removed or changed schema the deployed build relies on: $(tr '\n' ';' <<<"$removed")"
if [[ "${data_after#*|}" == "${data_before#*|}" ]]; then
  [[ "$data_after" == "$data_before" ]] || fail "candidate gk-migrate applied nothing but changed data: $data_before -> $data_after"
  cmp -s "$work/schema-before" "$work/schema-after" || fail "candidate gk-migrate applied nothing but changed the schema"
fi
pass "candidate gk-migrate on the deployed schema keeps rows and the deployed schema contract: $(grep -oE '"msg":"[^"]*"(,"(applied|ahead)":[^,}]*)?' <<<"$out" | head -1)"
wait_200 "http://127.0.0.1:$api_port_deployed/readyz" '"status":"ready"' "deployed API after candidate migrations"
pass "deployed API still ready after candidate migrations"

# 3. Candidate API on the result.
start_api candidate "$CANDIDATE_API" "$api_port_candidate"
wait_200 "http://127.0.0.1:$api_port_candidate/readyz" '"status":"ready"' "candidate API on the resulting schema"
pass "candidate API ready on the resulting schema"

# 4. The other direction.
out=$(migrate_with "$DEPLOYED_API") || fail "deployed gk-migrate after candidate migrations: $out"
[[ "$(data_snapshot)" == "$data_after" ]] || fail "deployed gk-migrate after candidate migrations changed data or history"
schema_snapshot >"$work/schema-again"
cmp -s "$work/schema-after" "$work/schema-again" || fail "deployed gk-migrate after candidate migrations changed the schema"
pass "deployed gk-migrate after candidate migrations changed nothing"

# 5. Browser contract of both API builds.
cors_ok "$api_port_deployed" "deployed API"
cors_ok "$api_port_candidate" "candidate API"
pass "both API builds allow the UI origin and refuse a foreign one"

# 6. Both workers use the job gateway on the result.
worker_ok deployed "$DEPLOYED_API"
worker_ok candidate "$CANDIDATE_API"
pass "deployed and candidate gk-worker claim and release a fresh job through the gateway"

# 7. Both UI builds.
start_ui deployed "$DEPLOYED_UI" "$ui_port_deployed"
start_ui candidate "$CANDIDATE_UI" "$ui_port_candidate"
for p in "$ui_port_deployed" "$ui_port_candidate"; do
  wait_200 "http://127.0.0.1:$p/healthz" '"status":"ok"' "UI on port $p"
  wait_200 "http://127.0.0.1:$p/" '<html' "UI home page on port $p"
done
pass "deployed and candidate UI builds serve /healthz and the home page"
echo "compat: matrix passed"
