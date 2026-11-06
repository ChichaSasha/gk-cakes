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

[ $# -eq 2 ] || { usage_of "$0"; }
CARD="$1"; FILE="$2"
printf '%s' "$CARD" | grep -Eq '^[0-9a-f]{24}$' || die "card ID must be 24 hex characters"
[ -s "$FILE" ] || die "$FILE is missing or empty"
TEXT="$(cat "$FILE")"
[ "${#TEXT}" -le 16384 ] || die "Trello comments are at most 16384 characters (this one has ${#TEXT})"
# Refuse text that looks like a credential (scripts/dev/credential_check.py).
python3 -B "$(dirname "$0")/credential_check.py" "$FILE" || die "$FILE was not posted"

# The installed Scale-Flow trello CLI (AGENTS.md), not another "trello" on PATH.
TRELLO=""
for candidate in "$HOME/go/bin/trello" "$( (go env GOPATH 2>/dev/null || true) | sed 's|$|/bin/trello|')"; do
	if [ -x "$candidate" ]; then TRELLO="$candidate"; break; fi
done
[ -n "$TRELLO" ] || die "the Scale-Flow trello CLI is not installed (~/go/bin/trello)"

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
