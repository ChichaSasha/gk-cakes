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
# Refuse text that looks like a credential (values that are exactly REDACTED,
# and Idempotency-Key values, are fine).
if ! python3 - "$FILE" <<'PY'
import re, sys
text = open(sys.argv[1], encoding="utf-8").read()
patterns = [
    r"\bATTA[0-9a-fA-F]{20,}",                      # Trello token
    r"\bgh[pousr]_[A-Za-z0-9]{20,}|\bgithub_pat_\w{20,}",
    r"\b[sr]k_(live|test)_\w{10,}|\bwhsec_\w{10,}",   # Stripe
    r"\bsb_secret_\w{10,}|\bre_[A-Za-z0-9]{20,}",     # Supabase secret, Resend
    r"\beyJ[\w-]{10,}\.eyJ[\w-]{10,}",                # JWT
    r"\bBearer\s+(?!REDACTED)[\w.~+/-]{16,}",
    r"(?<![Ii]dempotency-)\b(api[_-]?key|key|token|password|passwd|secret)\b[\"']?\s*[=:]\s*[\"']?(?!REDACTED)[\w.~+/-]{16,}",
]
hits = [p for p in patterns if re.search(p, text, re.I if "key|token" in p else 0)]
sys.exit(1 if hits else 0)
PY
then
	die "$FILE looks like it contains a credential; remove it first"
fi

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
