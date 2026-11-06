#!/usr/bin/env bash
# The delivery contract's merge steps for a reviewed PR, in one command
# (docs/planning/DEV-LOOP.md "Merge"):
#
#   scripts/dev/merge-flow.sh <pr> <reviewed-sha> [--wait] [--dry-run] [--repost] [-m "<ai-review description>"]
#
# <reviewed-sha> must be the commit ID the reviewer saw (7-40 hex
# characters), never a branch or other ref name.
# 1. Checks the PR is open against main and its head is the reviewed commit,
#    or reaches it through clean merges of main commits whose trees are
#    exactly what Git's own merges produce (as `gh pr update-branch` makes).
#    Anything else stops: the new code needs review. Prefer the full SHA.
# 2. If the branch is BEHIND main, updates it and checks that again (also
#    while waiting, up to 3 times, re-binding auto-merge and the status to the
#    new head).
# 3. Marks the PR ready, enables native auto-merge (merge commit) bound to that
#    exact head (expectedHeadOid), then posts the commit status
#    "ai-review" = success on that head. Auto-merge first, status second.
# 4. --wait: waits for the merge (stops early on a failing check), then for
#    the Staging workflow run of the merge commit, and prints its jobs.
#
# --repost       re-posts ai-review as pending then success (an auto-merge that
#                stalls on a clean PR).
# --dry-run      prints what would happen.
# --verify-only  only checks that the PR's current head is still the reviewed
#                code (any PR state), then stops.
# Never merges by hand, never force-pushes, never reruns workflows.
set -euo pipefail
# shellcheck source=scripts/dev/lib.sh
. "$(dirname "$0")/lib.sh"
need git; need gh

usage() { usage_of "$0"; }
[ $# -ge 2 ] || usage
PR="$1"; REVIEWED_IN="$2"; shift 2
WAIT=0; DRY=0; REPOST=0; VERIFY_ONLY=0; DESC=""
while [ $# -gt 0 ]; do
	case "$1" in
		--wait) WAIT=1; shift ;;
		--dry-run) DRY=1; shift ;;
		--repost) REPOST=1; shift ;;
		--verify-only) VERIFY_ONLY=1; shift ;;
		-m) need_value -m $#; DESC="$2"; shift 2 ;;
		*) usage ;;
	esac
done
printf '%s' "$PR" | grep -Eq '^[0-9]+$' || die "PR must be a number"
WAIT_MINUTES="${GK_WAIT_MINUTES:-45}"

MAIN="$(main_clone)"
ORIGIN_MAIN=refs/remotes/origin/main
git -C "$MAIN" fetch -q origin "+refs/heads/main:$ORIGIN_MAIN" "pull/$PR/head"
printf '%s' "$REVIEWED_IN" | grep -Eq '^[0-9a-f]{7,40}$' || die "<reviewed-sha> must be a commit ID (7-40 hex characters), not '$REVIEWED_IN'"
REVIEWED="$(git -C "$MAIN" rev-parse --verify --quiet "$REVIEWED_IN^{commit}")" || die "unknown commit $REVIEWED_IN"
case "$REVIEWED" in "$REVIEWED_IN"*) ;; *) die "'$REVIEWED_IN' resolves through a ref name; give the commit ID" ;; esac
[ -n "$DESC" ] || DESC="Independent review PASS on ${REVIEWED:0:7}"
[ "${#DESC}" -le 140 ] || die "the status description must be at most 140 characters"

pr_field() { gh pr view "$PR" -R "$GK_REPO" --json "$1" -q ".$1"; }

# Prints why a head is acceptable for the reviewed commit, or fails. The head
# may be the reviewed commit or reach it through first parents that are each a
# clean merge of a main commit (one per `gh pr update-branch`): each merge's
# tree must equal Git's own merge of its two parents, so no conflict
# resolution or extra edit can hide in it.
verify_head() {
	local head="$1" cur parents second expected steps=0 merged=""
	git -C "$MAIN" cat-file -e "$head^{commit}" 2>/dev/null || git -C "$MAIN" fetch -q origin "pull/$PR/head"
	cur="$head"
	while [ "$cur" != "$REVIEWED" ]; do
		steps=$((steps + 1))
		[ "$steps" -le 10 ] || die "head ${head:0:7}: more than 10 merges since the reviewed commit"
		parents="$(git -C "$MAIN" rev-list --parents -n 1 "$cur")"
		# shellcheck disable=SC2086 # split the commit and its parents into $1..$n
		set -- $parents
		[ $# -eq 3 ] || die "head ${head:0:7} is not the reviewed commit ${REVIEWED:0:7} or merges of main onto it: review the new code first"
		second="$3"
		git -C "$MAIN" merge-base --is-ancestor "$second" "$ORIGIN_MAIN" ||
			die "${cur:0:7} merges ${second:0:7}, which is not on main: review the new code first"
		expected="$(git -C "$MAIN" merge-tree --write-tree "$2" "$second" 2>/dev/null | head -n 1)" ||
			die "${cur:0:7}: merging main ${second:0:7} conflicts: review the resolution first"
		[ "$expected" = "$(git -C "$MAIN" rev-parse "$cur^{tree}")" ] ||
			die "${cur:0:7} differs from a clean merge of main ${second:0:7}: review it first"
		merged="${second:0:7} $merged"
		cur="$2"
	done
	if [ -z "$merged" ]; then
		note "head ${head:0:7} is the reviewed commit"
	else
		note "head ${head:0:7} = reviewed ${REVIEWED:0:7} + clean merges of main: ${merged% }"
	fi
}

if [ "$VERIFY_ONLY" = 1 ]; then
	verify_head "$(pr_field headRefOid)"
	exit 0
fi

run() {
	if [ "$DRY" = 1 ]; then note "would run: $*"; else "$@"; fi
}

# Updates a branch that is behind main and checks the new head.
update_branch() {
	local new=""
	note "branch is behind main: updating"
	gh pr update-branch "$PR" -R "$GK_REPO" >/dev/null
	for _ in $(seq 1 30); do
		new="$(pr_field headRefOid)"
		[ "$new" != "$HEAD" ] && break
		sleep 2
	done
	[ "$new" != "$HEAD" ] || die "the branch update did not produce a new head"
	HEAD="$new"
	git -C "$MAIN" fetch -q origin "+refs/heads/main:$ORIGIN_MAIN" "pull/$PR/head"
	verify_head "$HEAD"
}

# Auto-merge bound to $HEAD (merge commit), then ai-review = success on $HEAD.
land() {
	local method
	method="$(gh pr view "$PR" -R "$GK_REPO" --json autoMergeRequest -q '.autoMergeRequest.mergeMethod // empty')"
	if [ -z "$method" ]; then
		# shellcheck disable=SC2016 # GraphQL variables, not shell
		run gh api graphql \
			-f query='mutation($id:ID!,$oid:GitObjectID!){enablePullRequestAutoMerge(input:{pullRequestId:$id,mergeMethod:MERGE,expectedHeadOid:$oid}){pullRequest{number}}}' \
			-f id="$(pr_field id)" -f oid="$HEAD" >/dev/null
		[ "$DRY" = 1 ] || note "auto-merge enabled (merge commit) for head ${HEAD:0:7}"
	elif [ "$method" != "MERGE" ]; then
		die "auto-merge is enabled with $method; the delivery contract uses a merge commit: disable it in GitHub, then run this again"
	else
		note "auto-merge already enabled (merge commit)"
	fi
	if [ "$REPOST" = 1 ]; then
		run gh api -X POST "repos/$GK_REPO/statuses/$HEAD" -f state=pending -f context=ai-review -f description="Re-posting the review status" >/dev/null
		sleep 5
	fi
	run gh api -X POST "repos/$GK_REPO/statuses/$HEAD" -f state=success -f context=ai-review -f description="$DESC" >/dev/null
	[ "$DRY" = 1 ] || note "ai-review = success on ${HEAD:0:7}"
}

STATE="$(pr_field state)"; BASE="$(pr_field baseRefName)"
[ "$STATE" = "OPEN" ] || die "PR #$PR is $STATE"
[ "$BASE" = "main" ] || die "PR #$PR targets $BASE, not main"
HEAD="$(pr_field headRefOid)"
verify_head "$HEAD"

case "$(pr_field mergeStateStatus)" in
	DIRTY) die "PR #$PR has conflicts with main: resolve them on the branch, then review that change" ;;
	BEHIND)
		if [ "$DRY" = 1 ]; then
			note "would run: gh pr update-branch $PR, check the new head, then continue with that head"
			exit 0
		fi
		update_branch
		;;
esac

if [ "$(pr_field isDraft)" = "true" ]; then
	run gh pr ready "$PR" -R "$GK_REPO" >/dev/null
	[ "$DRY" = 1 ] || note "marked ready for review"
fi
land

[ "$WAIT" = 1 ] && [ "$DRY" = 0 ] || exit 0

note "waiting up to $WAIT_MINUTES minutes for the merge"
deadline=$(( $(date +%s) + WAIT_MINUTES * 60 ))
last_report=0
updates=0
while :; do
	STATE="$(pr_field state)"
	[ "$STATE" = "MERGED" ] && break
	[ "$STATE" = "OPEN" ] || die "PR #$PR is $STATE"
	failing="$(gh pr checks "$PR" -R "$GK_REPO" --json name,bucket -q '.[] | select(.bucket=="fail" or .bucket=="cancel") | .name' 2>/dev/null || true)"
	[ -z "$failing" ] || die "failing checks on PR #$PR: $(echo "$failing" | tr '\n' ' ')"
	if [ "$(pr_field mergeStateStatus)" = "BEHIND" ]; then
		# Main moved again before the merge (branches must be up to date).
		updates=$((updates + 1))
		[ "$updates" -le 3 ] || die "main keeps moving: the branch was updated 3 times; run this again later"
		update_branch
		land
	fi
	now=$(date +%s)
	[ "$now" -lt "$deadline" ] || die "not merged after $WAIT_MINUTES minutes (state $(pr_field mergeStateStatus)); try --repost if every check is green"
	if [ $(( now - last_report )) -ge 300 ]; then
		pending="$(gh pr checks "$PR" -R "$GK_REPO" --json name,bucket -q '.[] | select(.bucket=="pending") | .name' 2>/dev/null || true)"
		note "still open; pending: $(echo "${pending:-none}" | tr '\n' ' ')"
		last_report=$now
	fi
	sleep 30
done
MERGE_SHA="$(gh pr view "$PR" -R "$GK_REPO" --json mergeCommit -q .mergeCommit.oid)"
note "merged as ${MERGE_SHA:0:7}"

note "waiting for the Staging run of ${MERGE_SHA:0:7}"
deadline=$(( $(date +%s) + WAIT_MINUTES * 60 ))
while :; do
	line="$(gh run list -R "$GK_REPO" --workflow staging.yml --limit 20 --json headSha,status,conclusion,databaseId \
		-q "[.[] | select(.headSha==\"$MERGE_SHA\")] | first | select(. != null) | [.status, .conclusion, (.databaseId|tostring)] | join(\" \")")"
	case "$line" in completed*) break ;; esac
	[ "$(date +%s)" -lt "$deadline" ] || die "no completed Staging run for ${MERGE_SHA:0:7} after $WAIT_MINUTES minutes (a newer main commit may deploy it instead)"
	sleep 30
done
# shellcheck disable=SC2086 # status, conclusion and run ID
set -- $line
note "Staging run $3: $2"
gh run view "$3" -R "$GK_REPO" --json jobs -q '.jobs[] | "  \(.name): \(.conclusion)"'
if [ "$2" != "success" ]; then
	note "inspect: gh run view $3 -R $GK_REPO --log-failed (redact key/token values before sharing); a transient error (e.g. registry 'unknown blob') may be re-run with: gh run rerun $3 -R $GK_REPO --failed"
	exit 1
fi
echo "$MERGE_SHA"
