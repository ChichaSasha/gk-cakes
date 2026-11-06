#!/usr/bin/env bash
# Creates (or refreshes) a detached review worktree of a PR head or commit at an
# absolute scratch path outside every clone, for a fresh independent reviewer.
#
#   scripts/dev/review-worktree.sh <name> <pr-number|ref|sha> [--node-modules-from <worktree>] [--take-over]
#   scripts/dev/review-worktree.sh --remove <name> [--take-over]
#
# Prints the worktree path on stdout. <name> is a plain label: use one per
# reviewer (e.g. review-pr-52, review-pr-52-api). Only a review worktree this
# checkout created (an owner mark in its Git directory), detached and without
# changes or untracked files, is ever refreshed or removed; --take-over
# accepts one another checkout created.
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

# The checkout running this script owns the review worktrees it creates.
OWNER="$(git rev-parse --show-toplevel)"
TAKE_OVER=0

# Only our own detached, unchanged review worktree of this clone is ever
# moved or removed, so another reviewer's or author's work is never touched.
own_review_worktree() {
	local common mark
	common="$(git -C "$1" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" || die "$1 is not a Git worktree: leaving it alone"
	[ "$(cd "$common" && pwd -P)" = "$(cd "$MAIN/.git" && pwd -P)" ] || die "$1 belongs to another clone: leaving it alone"
	git -C "$1" symbolic-ref -q HEAD >/dev/null && die "$1 is on a branch, not a review worktree: leaving it alone"
	[ -z "$(git -C "$1" status --porcelain)" ] || die "$1 has changes or untracked files: leaving it alone"
	mark="$(cat "$(git -C "$1" rev-parse --absolute-git-dir)/gk-review-owner" 2>/dev/null || true)"
	if [ "$mark" != "$OWNER" ] && [ "$TAKE_OVER" = 0 ]; then
		die "$1 was created by ${mark:-another checkout}; someone may be reviewing in it (use another name, or --take-over)"
	fi
}

mark_owner() { echo "$OWNER" > "$(git -C "$1" rev-parse --absolute-git-dir)/gk-review-owner"; }

if [ "$1" = "--remove" ]; then
	check_label "$2"
	[ "${3:-}" = "--take-over" ] && TAKE_OVER=1
	path="$ROOT/$2"
	[ -e "$path/.git" ] || die "no worktree at $path"
	own_review_worktree "$path"
	git -C "$MAIN" worktree remove "$path" || die "git refused to remove $path"
	note "removed $path"
	exit 0
fi

name="$1"; target="$2"; shift 2
check_label "$name"
from=""
while [ $# -gt 0 ]; do
	case "$1" in
		--node-modules-from) need_value "$1" $#; from="$2"; shift 2 ;;
		--take-over) TAKE_OVER=1; shift ;;
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
	case "$sha" in "$target"*) ;; *) die "'$target' resolves through a ref name; give the commit ID" ;; esac
else
	branch="${target#origin/}"
	git -C "$MAIN" fetch -q origin "+refs/heads/$branch:refs/remotes/origin/$branch" || die "origin has no branch '$branch'"
	sha="$(git -C "$MAIN" rev-parse "refs/remotes/origin/$branch")"
fi

if [ -e "$path/.git" ]; then
	own_review_worktree "$path"
	old="$(git -C "$path" rev-parse HEAD)"
	git -C "$path" checkout -q --detach "$sha"
	mark_owner "$path"
	note "refreshed $path at ${sha:0:7}"
	if [ -d "$path/ui/node_modules" ] && [ -z "$from" ] && ! git -C "$path" diff --quiet "$old" "$sha" -- ui/package-lock.json; then
		note "ui/package-lock.json changed: run npm ci in $path/ui"
	fi
else
	[ -e "$path" ] && die "$path exists and is not a worktree"
	git -C "$MAIN" worktree add -q --detach "$path" "$sha"
	mark_owner "$path"
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
