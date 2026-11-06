#!/usr/bin/env bash
# The PR "ui" job's checks, locally, in one command:
#
#   scripts/dev/ui-preflight.sh [--fast] [--port 4190]
#
# Runs in ui/: npm run check, lint, test, test:bundle-secrets, build; starts
# the build without sign-in settings and checks /healthz; then starts it with
# placeholder public settings and runs scripts/check-headers.sh (servers are
# always stopped). Not covered: the container image build (needs Docker; CI
# runs it). --fast skips the build and the server checks (quick loops).
# Installs dependencies with `npm ci` only when ui/node_modules is missing.
set -euo pipefail
# shellcheck source=scripts/dev/lib.sh
. "$(dirname "$0")/lib.sh"
use_node

FAST=0; PORT=4190
while [ $# -gt 0 ]; do
	case "$1" in
		--fast) FAST=1; shift ;;
		--port) need_value "$1" $#; PORT="$2"; shift 2 ;;
		*) usage_of "$0" ;;
	esac
done
cd "$(git rev-parse --show-toplevel)/ui"
[ -d node_modules ] || { note "npm ci"; npm ci --no-audit --no-fund >/dev/null; }

step() { note "$1"; shift; "$@" || die "failed: $*"; }
step "svelte-check" npm run --silent check
step "lint" npm run --silent lint
step "unit tests" npm test --silent
step "bundle secret check" npm run --silent test:bundle-secrets
[ "$FAST" = 1 ] && { note "fast: build and header check skipped"; exit 0; }
step "build" npm run --silent build

if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then die "port $PORT is in use (use --port)"; fi
URL="http://127.0.0.1:$PORT"
SERVER=""
trap 'kill "$SERVER" 2>/dev/null || true' EXIT

# As CI: the server starts without sign-in settings and reports healthy.
note "start without sign-in settings, /healthz"
PORT="$PORT" ORIGIN="$URL" node build >/dev/null 2>&1 &
SERVER=$!
for _ in $(seq 1 50); do curl -fs -o /dev/null "$URL/healthz" && break; sleep 0.2; done
curl -fsS "$URL/healthz" 2>/dev/null | grep -q '"status":"ok"' || die "/healthz did not report ok"
kill "$SERVER"; wait "$SERVER" 2>/dev/null || true

PORT="$PORT" ORIGIN="$URL" PUBLIC_API_URL=https://api.example.com \
	PUBLIC_AUTH_URL=https://auth.example.com PUBLIC_AUTH_PUBLISHABLE_KEY=sb_publishable_0123456789abcdefXYZ \
	node build >/dev/null 2>&1 &
SERVER=$!
for _ in $(seq 1 50); do curl -fs -o /dev/null "$URL/" && break; sleep 0.2; done
step "header check" bash scripts/check-headers.sh "$URL" https://api.example.com https://auth.example.com
note "UI preflight passed"
