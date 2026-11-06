#!/usr/bin/env bash
# Runs a command against a local production UI build wired to the staging API
# through the read-only QA proxy, then stops both (they never outlive it):
#
#   scripts/dev/qa/with-ui.sh [--no-build] [--auth staging|none] -- <command...>
#
# The command gets GK_QA_BASE (the UI, http://127.0.0.1:4192), GK_QA_PROXY
# (http://127.0.0.1:4191, with /__ctl test controls) and GK_PUPPETEER.
# --auth staging (default) uses the staging sign-in project (public URL and
# publishable key, as in SETUP.md), so a test user can sign in; staff
# passwords stay in the Keychain, e.g.
#   STAFF_PW="$(security find-generic-password -a gk-test-staff@gkcake.com -s gk-supabase-test-staff -w)" \
#     scripts/dev/qa/with-ui.sh -- node my-check.cjs
# Ports: GK_QA_UI_PORT (4192), GK_QA_PROXY_PORT (4191).
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=scripts/dev/lib.sh
. "$here/../lib.sh"
use_node

BUILD=1; AUTH=staging
while [ $# -gt 0 ]; do
	case "$1" in
		--no-build) BUILD=0; shift ;;
		--auth) AUTH="${2:-}"; shift 2 ;;
		--) shift; break ;;
		*) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
	esac
done
[ $# -gt 0 ] || die "give a command after --"
UI_PORT="${GK_QA_UI_PORT:-4192}"; PROXY_PORT="${GK_QA_PROXY_PORT:-4191}"
for p in "$UI_PORT" "$PROXY_PORT"; do
	! lsof -nP -iTCP:"$p" -sTCP:LISTEN >/dev/null 2>&1 || die "port $p is in use"
done
case "$AUTH" in
	staging) AUTH_URL=https://flbgymxjelzexgoxrcss.supabase.co; AUTH_KEY=sb_publishable_2Ykzv4WThHGbG1LJlAksWg_5sbxfkfc ;;
	none) AUTH_URL=https://auth.example.com; AUTH_KEY=sb_publishable_0123456789abcdefXYZ ;;
	*) die "--auth staging|none" ;;
esac

ui="$(git rev-parse --show-toplevel)/ui"
[ -d "$ui/node_modules" ] || (cd "$ui" && npm ci --no-audit --no-fund >/dev/null)
if [ "$BUILD" = 1 ] || [ ! -d "$ui/build" ]; then (cd "$ui" && npm run --silent build >/dev/null); fi

pids=""
trap 'kill $pids 2>/dev/null || true' EXIT
PORT="$PROXY_PORT" node "$here/proxy.mjs" & pids="$pids $!"
(cd "$ui" && PORT="$UI_PORT" ORIGIN="http://127.0.0.1:$UI_PORT" PUBLIC_API_URL="http://127.0.0.1:$PROXY_PORT" \
	PUBLIC_AUTH_URL="$AUTH_URL" PUBLIC_AUTH_PUBLISHABLE_KEY="$AUTH_KEY" exec node build >/dev/null 2>&1) & pids="$pids $!"
for _ in $(seq 1 75); do curl -fs -o /dev/null "http://127.0.0.1:$UI_PORT/" && break; sleep 0.2; done
curl -fs -o /dev/null "http://127.0.0.1:$UI_PORT/" || die "the UI did not start"

export GK_QA_BASE="http://127.0.0.1:$UI_PORT" GK_QA_PROXY="http://127.0.0.1:$PROXY_PORT"
export GK_PUPPETEER="${GK_PUPPETEER:-${GK_QA_DIR:-$HOME/.cache/gk-qa}/node_modules/puppeteer-core}"
"$@"
