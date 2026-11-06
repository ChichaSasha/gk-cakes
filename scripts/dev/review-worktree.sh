#!/usr/bin/env bash
# Creates (or refreshes) a detached review worktree of a PR head or commit at an
# absolute scratch path outside every clone, for a fresh independent reviewer.
#
#   scripts/dev/review-worktree.sh <name> <pr-number|ref|sha> [--node-modules-from <worktree>]
#   scripts/dev/review-worktree.sh --remove <name>
#
# Prints the worktree path on stdout. <name> is a plain label (e.g. review-pr-52).
# The target is a PR number (its current head), a commit ID, or a branch of
# origin (`main`, `origin/main` and `sasha-chycha/x` all mean the remote
# branch, freshly fetched; local branches are never used). GK_SCRATCH (default
# $TMPDIR) must be outside every Git working tree.
# --node-modules-from copies ui/node_modules from another worktree when both
# ui/package-lock.json files are identical (an APFS clone on macOS: near
# instant), so the reviewer can skip `npm ci`.
set -euo pipefail
# shellcheck source=scripts/dev/lib.sh
. "$(dirname "$0")/lib.sh"
need git

usage() { usage_of "$0"; }
[ $# -ge 2 ] || usage

MAIN="$(main_clone)"
ROOT="$(scratch_root)"

if [ "$1" = "--remove" ]; then
	check_label "$2"
	path="$ROOT/$2"
	[ -e "$path/.git" ] || die "no worktree at $path"
	git -C "$MAIN" worktree remove --force "$path"
	note "removed $path"
	exit 0
fi

name="$1"; target="$2"; shift 2
check_label "$name"
from=""
while [ $# -gt 0 ]; do
	case "$1" in
		--node-modules-from) need_value "$1" $#; from="$2"; shift 2 ;;
		*) usage ;;
	esac
done
path="$ROOT/$name"

# Resolve the target to a commit.
if printf '%s' "$target" | grep -Eq '^[0-9]+$'; then
	need gh
	sha="$(gh pr view "$target" -R "$GK_REPO" --json headRefOid -q .headRefOid)"
	git -C "$MAIN" fetch -q origin "pull/$target/head"
elif printf '%s' "$target" | grep -Eq '^[0-9a-f]{7,40}$' && git -C "$MAIN" cat-file -e "$target^{commit}" 2>/dev/null; then
	sha="$(git -C "$MAIN" rev-parse "$target^{commit}")"
else
	branch="${target#origin/}"
	git -C "$MAIN" fetch -q origin "+refs/heads/$branch:refs/remotes/origin/$branch" || die "origin has no branch '$branch'"
	sha="$(git -C "$MAIN" rev-parse "refs/remotes/origin/$branch")"
fi

if [ -e "$path/.git" ]; then
	if [ -n "$(git -C "$path" status --porcelain --untracked-files=no)" ]; then
		die "$path has changes to tracked files; remove it first"
	fi
	old="$(git -C "$path" rev-parse HEAD)"
	git -C "$path" checkout -q --detach "$sha"
	note "refreshed $path at ${sha:0:7}"
	if [ -d "$path/ui/node_modules" ] && [ -z "$from" ] && ! git -C "$path" diff --quiet "$old" "$sha" -- ui/package-lock.json; then
		note "ui/package-lock.json changed: run npm ci in $path/ui"
	fi
else
	[ -e "$path" ] && die "$path exists and is not a worktree"
	git -C "$MAIN" worktree add -q --detach "$path" "$sha"
	note "created $path at ${sha:0:7}"
fi

if [ -n "$from" ]; then
	[ -d "$from/ui/node_modules" ] || die "no ui/node_modules in $from"
	if cmp -s "$from/ui/package-lock.json" "$path/ui/package-lock.json"; then
		rm -rf "$path/ui/node_modules"
		if ! cp -cR "$from/ui/node_modules" "$path/ui/node_modules" 2>/dev/null; then
			rm -rf "$path/ui/node_modules"
			cp -R "$from/ui/node_modules" "$path/ui/node_modules"
		fi
		note "copied ui/node_modules from $from (same lockfile)"
	else
		note "lockfiles differ: run npm ci in $path/ui"
	fi
fi
echo "$path"
