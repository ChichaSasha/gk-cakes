#!/usr/bin/env bash
# The delivery contract's merge steps for a reviewed PR, in one command
# (docs/planning/DEV-LOOP.md "Merge"):
#
#   scripts/dev/merge-flow.sh <pr> <reviewed-sha> [--wait] [--dry-run] [--repost] [-m "<ai-review description>"]
#
# <reviewed-sha> must be the commit ID the reviewer saw (7-40 hex
# characters), never a branch or other ref name.
# 1. Checks the PR is open against main and its head is the reviewed commit,
#    or a clean merge of a main commit onto it whose tree is exactly what Git's
#    own merge of the two produces (as `gh pr update-branch` makes). Anything
#    else stops: the new code needs review.
# 2. If the branch is BEHIND main, updates it and checks that again.
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

# Prints why HEAD is acceptable for the reviewed commit, or fails.
verify_head() {
	local head="$1" parents second expected
	if [ "$head" = "$REVIEWED" ]; then
		note "head ${head:0:7} is the reviewed commit"
		return 0
	fi
	git -C "$MAIN" cat-file -e "$head^{commit}" 2>/dev/null || git -C "$MAIN" fetch -q origin "pull/$PR/head"
	parents="$(git -C "$MAIN" rev-list --parents -n 1 "$head")"
	# shellcheck disable=SC2086 # split the commit and its parents into $1..$n
	set -- $parents
	[ $# -eq 3 ] && [ "$2" = "$REVIEWED" ] || die "head ${head:0:7} is not the reviewed commit ${REVIEWED:0:7} or a merge onto it: review the new code first"
	second="$3"
	git -C "$MAIN" merge-base --is-ancestor "$second" "$ORIGIN_MAIN" || die "head ${head:0:7} merges ${second:0:7}, which is not on main"
	# Git's own merge of the reviewed commit and that main commit must be clean
	# and give exactly the head's tree: no conflict resolution, no extra edit.
	expected="$(git -C "$MAIN" merge-tree --write-tree "$REVIEWED" "$second" 2>/dev/null | head -n 1)" ||
		die "merging ${second:0:7} into the reviewed commit conflicts: review the resolution first"
	[ "$expected" = "$(git -C "$MAIN" rev-parse "$head^{tree}")" ] ||
		die "the merge in ${head:0:7} differs from a clean merge of main ${second:0:7}: review it first"
	note "head ${head:0:7} = reviewed ${REVIEWED:0:7} + clean merge of main ${second:0:7}"
}

if [ "$VERIFY_ONLY" = 1 ]; then
	verify_head "$(pr_field headRefOid)"
	exit 0
fi

STATE="$(pr_field state)"; BASE="$(pr_field baseRefName)"
[ "$STATE" = "OPEN" ] || die "PR #$PR is $STATE"
[ "$BASE" = "main" ] || die "PR #$PR targets $BASE, not main"
HEAD="$(pr_field headRefOid)"
verify_head "$HEAD"

MERGE_STATE="$(pr_field mergeStateStatus)"
case "$MERGE_STATE" in
	DIRTY) die "PR #$PR has conflicts with main: resolve them on the branch, then review that change" ;;
	BEHIND)
		if [ "$DRY" = 1 ]; then
			note "would run: gh pr update-branch $PR, check the new head, then continue with that head"
			exit 0
		else
			note "branch is behind main: updating"
			gh pr update-branch "$PR" -R "$GK_REPO" >/dev/null
			for _ in $(seq 1 30); do
				NEW="$(pr_field headRefOid)"
				[ "$NEW" != "$HEAD" ] && break
				sleep 2
			done
			[ "$NEW" != "$HEAD" ] || die "the branch update did not produce a new head"
			HEAD="$NEW"
			git -C "$MAIN" fetch -q origin "+refs/heads/main:$ORIGIN_MAIN" "pull/$PR/head"
			verify_head "$HEAD"
		fi
		;;
esac

run() {
	if [ "$DRY" = 1 ]; then note "would run: $*"; else "$@"; fi
}

if [ "$(pr_field isDraft)" = "true" ]; then
	run gh pr ready "$PR" -R "$GK_REPO" >/dev/null
	[ "$DRY" = 1 ] || note "marked ready for review"
fi

if [ -z "$(gh pr view "$PR" -R "$GK_REPO" --json autoMergeRequest -q '.autoMergeRequest.mergeMethod // empty')" ]; then
	ID="$(pr_field id)"
	# shellcheck disable=SC2016 # GraphQL variables, not shell
	run gh api graphql \
		-f query='mutation($id:ID!,$oid:GitObjectID!){enablePullRequestAutoMerge(input:{pullRequestId:$id,mergeMethod:MERGE,expectedHeadOid:$oid}){pullRequest{number}}}' \
		-f id="$ID" -f oid="$HEAD" >/dev/null
	[ "$DRY" = 1 ] || note "auto-merge enabled (merge commit) for head ${HEAD:0:7}"
else
	note "auto-merge already enabled"
fi

status() {
	run gh api -X POST "repos/$GK_REPO/statuses/$HEAD" -f state="$1" -f context=ai-review -f description="$2" >/dev/null
}
if [ "$REPOST" = 1 ]; then
	status pending "Re-posting the review status"
	sleep 5
fi
status success "$DESC"
[ "$DRY" = 1 ] || note "ai-review = success on ${HEAD:0:7}"

[ "$WAIT" = 1 ] && [ "$DRY" = 0 ] || exit 0

note "waiting up to $WAIT_MINUTES minutes for the merge"
deadline=$(( $(date +%s) + WAIT_MINUTES * 60 ))
last_report=0
while :; do
	STATE="$(pr_field state)"
	[ "$STATE" = "MERGED" ] && break
	[ "$STATE" = "OPEN" ] || die "PR #$PR is $STATE"
	failing="$(gh pr checks "$PR" -R "$GK_REPO" --json name,bucket -q '.[] | select(.bucket=="fail" or .bucket=="cancel") | .name' 2>/dev/null || true)"
	[ -z "$failing" ] || die "failing checks on PR #$PR: $(echo "$failing" | tr '\n' ' ')"
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
