#!/usr/bin/env bash
# GK-034: public smoke check after a staging deploy, through the real public
# hostnames (Cloudflare -> Railway). Read-only requests; fails the deploy job
# (and so its deployment record) when the public URL does not serve a healthy
# component within the wait budget.
#   smoke.sh api <api-url> <ui-url>   /readyz ready, /healthz ok, CORS for the UI origin
#   smoke.sh ui  <ui-url>             /healthz ok, home page serves HTML
set -euo pipefail

component=${1:-}
url=${2:-}
ui_origin=${3:-}
wait_seconds=${SMOKE_WAIT_SECONDS:-180}

fail() {
  echo "smoke: $*" >&2
  exit 1
}

check_url() {
  [[ "$1" =~ ^https://[a-z0-9.-]+$ ]] || fail "$2 must be https://<host> with no path or trailing slash (got '$1')"
}

summary() {
  echo "- smoke $component: $*" | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
}

# wait_for <path> <body substring>: poll until 200 with the expected body.
wait_for() {
  local deadline=$((SECONDS + wait_seconds)) code body
  while :; do
    body=$(curl -sS --max-time 10 -o - -w '\n%{http_code}' "$url$1" 2>/dev/null || true)
    code=${body##*$'\n'}
    body=${body%$'\n'*}
    if [[ "$code" == 200 && "$body" == *"$2"* ]]; then
      body=${body//$'\n'/ }
      summary "GET $url$1 -> 200 ${body:0:80}"
      return 0
    fi
    ((SECONDS < deadline)) || fail "GET $url$1 did not return 200 with '$2' within ${wait_seconds}s (last: HTTP $code)"
    sleep 5
  done
}

header() { # header <name> <curl args...>: value of a response header
  local name=$1
  shift
  curl -sS --max-time 10 -o /dev/null -D - "$@" | tr -d '\r' |
    awk -v n="$(tr '[:upper:]' '[:lower:]' <<<"$name")" -F': ' 'tolower($1) == n { print $2 }'
}

case "$component" in
  api)
    check_url "$url" "API URL"
    check_url "$ui_origin" "UI origin"
    wait_for /readyz '"status":"ready"'
    wait_for /healthz '"status":"ok"'
    allowed=$(header access-control-allow-origin -H "Origin: $ui_origin" "$url/healthz")
    [[ "$allowed" == "$ui_origin" ]] || fail "CORS: Origin $ui_origin got access-control-allow-origin '$allowed'"
    other=$(header access-control-allow-origin -H "Origin: https://not-gk.invalid" "$url/healthz")
    [[ -z "$other" ]] || fail "CORS: a foreign origin was allowed ('$other')"
    methods=$(header access-control-allow-methods -X OPTIONS -H "Origin: $ui_origin" \
      -H "Access-Control-Request-Method: POST" "$url/healthz")
    [[ "$methods" == *POST* ]] || fail "CORS preflight from $ui_origin did not allow POST ('$methods')"
    summary "CORS allows $ui_origin only (preflight methods: $methods)"
    ;;
  ui)
    check_url "$url" "UI URL"
    wait_for /healthz '"status":"ok"'
    wait_for / '<html'
    ;;
  *)
    fail "usage: smoke.sh api <api-url> <ui-url> | smoke.sh ui <ui-url>"
    ;;
esac
