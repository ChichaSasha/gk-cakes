#!/usr/bin/env bash
# Posts a Markdown file as a comment on a Trello card with the installed
# Scale-Flow trello CLI, then checks the newest comment is it.
#
#   scripts/dev/trello-comment.sh <card-id> <file>
#
# Comments only: never moves, edits or archives cards. Error output is
# redacted (CLI network errors can carry key/token query values). The CLI's
# credentials stay in the macOS Keychain; outside a sandbox that blocks the
# Keychain this needs no login.
set -euo pipefail
# shellcheck source=scripts/dev/lib.sh
. "$(dirname "$0")/lib.sh"

[ $# -eq 2 ] || { sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
CARD="$1"; FILE="$2"
printf '%s' "$CARD" | grep -Eq '^[0-9a-f]{24}$' || die "card ID must be 24 hex characters"
[ -s "$FILE" ] || die "$FILE is missing or empty"
TEXT="$(cat "$FILE")"
[ "${#TEXT}" -le 16384 ] || die "Trello comments are at most 16384 characters (this one has ${#TEXT})"
if grep -Eiq '(key|token|password|secret)=[A-Za-z0-9]|sk_(live|test)_|sb_secret_|eyJ[A-Za-z0-9_-]{20,}\.' "$FILE"; then
	die "$FILE looks like it contains a credential; remove it first"
fi

TRELLO="$(command -v trello 2>/dev/null || true)"
if [ -z "$TRELLO" ] && command -v go >/dev/null 2>&1; then TRELLO="$(go env GOPATH)/bin/trello"; fi
[ -x "${TRELLO:-}" ] || die "the trello CLI is not installed"

out="$("$TRELLO" comments add --card "$CARD" --text "$TEXT" 2>&1)" || { echo "$out" | redact >&2; die "posting failed"; }
first="$(head -n 1 "$FILE")"
latest="$("$TRELLO" comments list --card "$CARD" 2>&1 | python3 -c '
import json, sys
d = json.load(sys.stdin)
d = d if isinstance(d, list) else d.get("data", d)
c = d[0] if d else {}
print(c.get("id", ""), (c.get("data") or {}).get("text", "").splitlines()[0] if c else "")
')" || die "could not read the card's comments back"
case "$latest" in
	*" $first") echo "posted comment ${latest%% *} on card $CARD" ;;
	*) die "the newest comment on $CARD is not the one just posted" ;;
esac
