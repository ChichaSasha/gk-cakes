#!/usr/bin/env bash
# Every UI check the PR's "ui" job needs, locally, in one command, and the
# header check against a local production build:
#
#   scripts/dev/ui-preflight.sh [--fast] [--port 4190]
#
# Runs in ui/: npm run check, lint, test, test:bundle-secrets, build, then
# starts the build with placeholder public settings and runs
# scripts/check-headers.sh against it (the server is always stopped).
# --fast skips the build and header check (for quick loops; CI runs them).
# Installs dependencies with `npm ci` only when ui/node_modules is missing.
set -euo pipefail
# shellcheck source=scripts/dev/lib.sh
. "$(dirname "$0")/lib.sh"
use_node

FAST=0; PORT=4190
while [ $# -gt 0 ]; do
	case "$1" in
		--fast) FAST=1; shift ;;
		--port) PORT="${2:-}"; shift 2 ;;
		*) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
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
PORT="$PORT" ORIGIN="$URL" PUBLIC_API_URL=https://api.example.com \
	PUBLIC_AUTH_URL=https://auth.example.com PUBLIC_AUTH_PUBLISHABLE_KEY=sb_publishable_0123456789abcdefXYZ \
	node build >/dev/null 2>&1 &
SERVER=$!
trap 'kill "$SERVER" 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do curl -fs -o /dev/null "$URL/" && break; sleep 0.2; done
step "header check" bash scripts/check-headers.sh "$URL" https://api.example.com https://auth.example.com
note "UI preflight passed"
