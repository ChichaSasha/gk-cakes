#!/usr/bin/env bash
# Checks the security headers of a running UI server (GK-021): the Content
# Security Policy (nonce'd scripts, nothing unsafe in script-src or
# style-src, no plugins or framing, runtime API and identity origins in
# connect-src) and the private-page headers, including on a
# percent-encoded path.
# Usage: check-headers.sh <base-url> <api-origin> <auth-origin>
set -euo pipefail

base=$1
api=$2
auth=$3
failures=0

fail() {
  echo "header check: $1" >&2
  failures=$((failures + 1))
}

headers() {
  curl -fsS -D - -o /dev/null "$base$1" | tr -d '\r'
}

value() { # value <headers> <name>; empty when absent
  { grep -i "^$2:" <<<"$1" || true; } | head -n 1 | cut -d: -f2- | sed 's/^ *//'
}

directive() { # directive <policy> <name>
  tr ';' '\n' <<<"$1" | sed 's/^ *//' | grep -E "^$2( |$)" || true
}

# Public pages (cacheable, indexable): the home and discovery pages (GK-028).
public_pages=" / /shop /sizes /bag "

for page in / /sign-in /account /admin /auth/callback /%61ccount /checkout /orders/00000000-0000-4000-8000-000000000000 /shop /sizes /bag; do
  h=$(headers "$page")
  csp=$(value "$h" content-security-policy)
  [[ -n "$csp" ]] || { fail "$page: no Content-Security-Policy"; continue; }
  script=$(directive "$csp" script-src)
  [[ "$script" == *"'nonce-"* ]] || fail "$page: script-src has no nonce ($script)"
  [[ "$script" != *unsafe* ]] || fail "$page: script-src allows unsafe code ($script)"
  style=$(directive "$csp" style-src)
  [[ "$style" != *unsafe* ]] || fail "$page: style-src allows unsafe code ($style)"
  [[ "$(directive "$csp" object-src)" == "object-src 'none'" ]] || fail "$page: object-src is not 'none'"
  [[ "$(directive "$csp" frame-ancestors)" == "frame-ancestors 'none'" ]] || fail "$page: frame-ancestors is not 'none'"
  connect=$(directive "$csp" connect-src)
  for origin in "$api" "$auth"; do
    [[ " $connect " == *" $origin "* ]] || fail "$page: connect-src lacks $origin ($connect)"
  done
  [[ "$(value "$h" x-content-type-options)" == nosniff ]] || fail "$page: no nosniff"
  [[ "$(value "$h" cross-origin-opener-policy)" == same-origin ]] || fail "$page: no Cross-Origin-Opener-Policy"
  if [[ "$public_pages" != *" $page "* ]]; then
    [[ "$(value "$h" cache-control)" == no-store ]] || fail "$page: private page is cacheable"
    [[ "$(value "$h" referrer-policy)" == no-referrer ]] || fail "$page: private page sends a referrer"
    [[ "$(value "$h" x-robots-tag)" == *noindex* ]] || fail "$page: private page is indexable"
  fi
done

if ((failures > 0)); then
  echo "header check: $failures problem(s)" >&2
  exit 1
fi
echo "header check: Content-Security-Policy and private-page headers as required"
