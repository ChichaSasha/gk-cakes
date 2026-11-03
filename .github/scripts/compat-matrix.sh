#!/usr/bin/env bash
# GK-034 AC2: previous/current compatibility matrix for one staging deploy.
# "deployed" is what staging runs now, "candidate" what is about to deploy;
# in a rollback the candidate is the older one. Runs both API images against
# one disposable PostgreSQL in deploy order and checks, in both directions:
#   - each build's gk-migrate succeeds on the other's schema (forward-only:
#     an older build on a newer database applies nothing);
#   - neither migration run deletes rows, and the rollback-direction run
#     changes nothing at all;
#   - both API builds are ready on the final schema, the deployed one also
#     while the candidate's migrations are applied (rolling deploy);
#   - both API builds allow the UI origin (CORS contract) and only it;
#   - both UI builds serve /healthz and the home page.
# The UI calls no API endpoint yet; features that add UI->API calls extend
# the contract checks here.
#   compat-matrix.sh   (env: DEPLOYED_API CANDIDATE_API DEPLOYED_UI CANDIDATE_UI, image@sha256 refs)
set -euo pipefail

for v in DEPLOYED_API CANDIDATE_API DEPLOYED_UI CANDIDATE_UI; do
  [[ "${!v:-}" =~ ^[a-z0-9.:/_-]+@sha256:[0-9a-f]{64}$ ]] || { echo "compat: $v must be an image@sha256 reference (got '${!v:-}')" >&2; exit 1; }
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

cleanup() { docker rm -f "$pg" "$prefix-api-deployed" "$prefix-api-candidate" "$prefix-ui-deployed" "$prefix-ui-candidate" >/dev/null 2>&1 || true; }
trap cleanup EXIT

fail() {
  echo "compat: FAIL: $*" >&2
  echo "- **FAIL**: $*" >>"$summary_file"
  exit 1
}
pass() {
  echo "compat: ok: $*"
  echo "- ok: $*" >>"$summary_file"
}

psql_q() { docker exec -i "$pg" psql -v ON_ERROR_STOP=1 -qAt -U postgres -d postgres; } # SQL on stdin

# Row counts and a content fingerprint of every business table, plus history.
snapshot() {
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

# counts_not_lower <before> <after>: every table's row count is kept or grown.
counts_not_lower() {
  local entry table count after_count
  IFS=',' read -ra entries <<<"${1%%|*}"
  for entry in "${entries[@]}"; do
    [[ -n "$entry" ]] || continue
    table=${entry%%=*}
    count=${entry#*=}
    count=${count%%/*}
    after_count=$(grep -oE "(^|,)$table=[0-9]+" <<<"${2%%|*}" | sed 's/.*=//' || true)
    [[ -n "$after_count" && "$after_count" -ge "$count" ]] || return 1
  done
}

migrate_with() { # migrate_with <image>: prints gk-migrate's log line
  docker run --rm --network "container:$pg" -e APP_ENV=test \
    -e MIGRATION_DATABASE_URL="postgres://postgres:compat@127.0.0.1:5432/postgres?sslmode=disable" \
    --entrypoint /usr/local/bin/gk-migrate "$1"
}

start_api() { # start_api <name> <image> <port>
  docker run -d --name "$prefix-api-$1" --network "container:$pg" -e APP_ENV=test -e PORT="$3" \
    -e DATABASE_URL="postgres://gk_api_compat:compat@127.0.0.1:5432/postgres?sslmode=disable" \
    -e UI_ORIGIN="$ui_origin" "$2" >/dev/null
}

start_ui() { # start_ui <name> <image> <port>
  docker run -d --name "$prefix-ui-$1" -p "127.0.0.1:$3:3000" -e PORT=3000 \
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

cors_ok() { # cors_ok <port> <what>
  local allowed foreign
  allowed=$(curl -sS --max-time 5 -o /dev/null -D - -H "Origin: $ui_origin" "http://127.0.0.1:$1/healthz" |
    tr -d '\r' | awk -F': ' 'tolower($1) == "access-control-allow-origin" { print $2 }')
  foreign=$(curl -sS --max-time 5 -o /dev/null -D - -H "Origin: https://not-gk.invalid" "http://127.0.0.1:$1/healthz" |
    tr -d '\r' | awk -F': ' 'tolower($1) == "access-control-allow-origin" { print $2 }')
  [[ "$allowed" == "$ui_origin" && -z "$foreign" ]] || fail "$2: CORS allowed '$allowed' for the UI origin and '$foreign' for a foreign one"
}

{
  echo "### Compatibility matrix"
  echo "- deployed API \`$DEPLOYED_API\`, candidate API \`$CANDIDATE_API\`"
  echo "- deployed UI \`$DEPLOYED_UI\`, candidate UI \`$CANDIDATE_UI\`"
} >>"$summary_file"

docker run -d --name "$pg" -e POSTGRES_PASSWORD=compat \
  -p "127.0.0.1:$api_port_deployed:$api_port_deployed" -p "127.0.0.1:$api_port_candidate:$api_port_candidate" \
  "$pg_image" >/dev/null
for _ in $(seq 1 60); do
  docker exec "$pg" pg_isready -U postgres >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$pg" pg_isready -U postgres >/dev/null || fail "PostgreSQL did not start"

# 1. Staging as it is: deployed schema, a runtime login and data.
out=$(migrate_with "$DEPLOYED_API") || fail "deployed gk-migrate on an empty database: $out"
psql_q <<'SQL' >/dev/null
CREATE ROLE gk_api_compat LOGIN PASSWORD 'compat' IN ROLE gk_api;
BEGIN;
SET LOCAL ROLE gk_owner;
SELECT business.job_enqueue(gen_random_uuid(), now() + interval '1 day');
COMMIT;
SQL
start_api deployed "$DEPLOYED_API" "$api_port_deployed"
wait_200 "http://127.0.0.1:$api_port_deployed/readyz" '"status":"ready"' "deployed API on the deployed schema"
before=$(snapshot)
pass "deployed API ready on its own schema (seeded: ${before%%|*})"

# 2. Candidate migrations while the deployed API keeps serving.
out=$(migrate_with "$CANDIDATE_API") || fail "candidate gk-migrate on the deployed schema: $out"
after=$(snapshot)
counts_not_lower "$before" "$after" || fail "candidate migrations deleted rows: $before -> $after"
pass "candidate gk-migrate on the deployed schema: $(grep -oE '"msg":"[^"]*"(,"(applied|ahead)":[^,}]*)?' <<<"$out" | head -1)"
wait_200 "http://127.0.0.1:$api_port_deployed/readyz" '"status":"ready"' "deployed API after candidate migrations"
pass "deployed API still ready after candidate migrations (rolling deploy / rollback target)"

# 3. Candidate API on the resulting schema.
start_api candidate "$CANDIDATE_API" "$api_port_candidate"
wait_200 "http://127.0.0.1:$api_port_candidate/readyz" '"status":"ready"' "candidate API on the resulting schema"
pass "candidate API ready on the resulting schema"

# 4. The other direction: the deployed build's gk-migrate on the resulting
# schema must succeed and change nothing (an older build applies nothing).
out=$(migrate_with "$DEPLOYED_API") || fail "deployed gk-migrate after candidate migrations: $out"
again=$(snapshot)
[[ "$again" == "$after" ]] || fail "deployed gk-migrate changed data or history: $after -> $again"
pass "deployed gk-migrate after candidate migrations changed nothing"

# 5. Browser contract of both API builds.
cors_ok "$api_port_deployed" "deployed API"
cors_ok "$api_port_candidate" "candidate API"
pass "both API builds allow the UI origin and refuse a foreign one"

# 6. Both UI builds.
start_ui deployed "$DEPLOYED_UI" "$ui_port_deployed"
start_ui candidate "$CANDIDATE_UI" "$ui_port_candidate"
for p in "$ui_port_deployed" "$ui_port_candidate"; do
  wait_200 "http://127.0.0.1:$p/healthz" '"status":"ok"' "UI on port $p"
  wait_200 "http://127.0.0.1:$p/" '<html' "UI home page on port $p"
done
pass "deployed and candidate UI builds serve /healthz and the home page"
echo "compat: matrix passed"
