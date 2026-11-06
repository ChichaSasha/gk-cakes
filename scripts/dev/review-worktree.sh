#!/usr/bin/env bash
# Creates (or refreshes) a detached review worktree of a PR head or commit at an
# absolute scratch path outside every clone, for a fresh independent reviewer.
#
#   scripts/dev/review-worktree.sh <name> <pr-number|ref|sha> [--node-modules-from <worktree>]
#   scripts/dev/review-worktree.sh --remove <name>
#
# Prints the worktree path on stdout. <name> is a plain label (e.g. review-pr-52).
# --node-modules-from copies ui/node_modules from another worktree when both
# ui/package-lock.json files are identical (an APFS clone on macOS: near
# instant), so the reviewer can skip `npm ci`.
set -euo pipefail
# shellcheck source=scripts/dev/lib.sh
. "$(dirname "$0")/lib.sh"
need git

usage() { sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
[ $# -ge 2 ] || usage

MAIN="$(main_clone)"
if [ "$1" = "--remove" ]; then
	name="$2"
	path="$(abs_path "$GK_SCRATCH/$name")"
	[ -d "$path" ] || die "no worktree at $path"
	git -C "$MAIN" worktree remove --force "$path"
	note "removed $path"
	exit 0
fi

name="$1"; target="$2"; shift 2
from=""
while [ $# -gt 0 ]; do
	case "$1" in
		--node-modules-from) from="${2:-}"; shift 2 ;;
		*) usage ;;
	esac
done
case "$name" in */*|.*|"") die "name must be a plain label, got '$name'" ;; esac

path="$(abs_path "$GK_SCRATCH/$name")"
main_real="$(cd "$MAIN" && pwd -P)"
case "$path/" in "$main_real"/*) die "refusing a worktree inside the main clone ($main_real)" ;; esac

# Resolve the target to a commit: a PR number uses the PR's current head.
if printf '%s' "$target" | grep -Eq '^[0-9]+$'; then
	need gh
	sha="$(gh pr view "$target" -R "$GK_REPO" --json headRefOid -q .headRefOid)"
	git -C "$MAIN" fetch -q origin "pull/$target/head"
else
	git -C "$MAIN" fetch -q origin
	sha="$(git -C "$MAIN" rev-parse --verify "$target^{commit}" 2>/dev/null || git -C "$MAIN" rev-parse --verify "origin/$target^{commit}")"
fi

if [ -e "$path/.git" ]; then
	if [ -n "$(git -C "$path" status --porcelain --untracked-files=no)" ]; then
		die "$path has changes to tracked files; remove it first"
	fi
	git -C "$path" checkout -q --detach "$sha"
	note "refreshed $path at ${sha:0:7}"
else
	[ -e "$path" ] && die "$path exists and is not a worktree"
	git -C "$MAIN" worktree add -q --detach "$path" "$sha"
	note "created $path at ${sha:0:7}"
fi

if [ -n "$from" ]; then
	[ -d "$from/ui/node_modules" ] || die "no ui/node_modules in $from"
	if cmp -s "$from/ui/package-lock.json" "$path/ui/package-lock.json"; then
		rm -rf "$path/ui/node_modules"
		cp -cR "$from/ui/node_modules" "$path/ui/node_modules" 2>/dev/null || cp -R "$from/ui/node_modules" "$path/ui/node_modules"
		note "copied ui/node_modules from $from (same lockfile)"
	else
		note "lockfiles differ: run npm ci in $path/ui"
	fi
fi
echo "$path"
