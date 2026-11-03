#!/usr/bin/env bash
# GK-034: resolve the image pairs for compat-matrix.sh.
#   deployed  = the newest staging-<component> deployment record whose latest
#               status is success (GitHub deployments, payload.artifact_digest);
#   candidate = NEW_API / NEW_UI when set, otherwise the deployed image (a
#               release changes one component; the other stays as deployed).
# With no records at all (first deploy) the deployed image is the candidate.
# Fails closed when records exist but none of the newest 100 succeeded, when a
# successful record has a malformed digest, or on any GitHub API error.
# Prints "resolve: NAME=value" for DEPLOYED_API, CANDIDATE_API, DEPLOYED_UI and
# CANDIDATE_UI, and appends NAME=value to $GITHUB_ENV when it is set.
# Requires GH_TOKEN (deployments: read), GITHUB_REPOSITORY, REGISTRY_OWNER.
set -euo pipefail

registry="ghcr.io/$(tr '[:upper:]' '[:lower:]' <<<"${REGISTRY_OWNER:?REGISTRY_OWNER is required}")"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

# deployed <component>: sets DEPLOYED to the image of the newest successful
# record ("" when there are no records); returns 1 on any doubt.
deployed() {
  local list id digest state any=0
  DEPLOYED=""
  list=$(gh api "repos/$GITHUB_REPOSITORY/deployments?environment=staging-$1&per_page=100" \
    --jq '.[] | [.id, (.payload.artifact_digest // "")] | @tsv') || {
    echo "resolve: cannot list staging-$1 deployments" >&2
    return 1
  }
  while IFS=$'\t' read -r id digest; do
    [[ -n "$id" ]] || continue
    any=1
    state=$(gh api "repos/$GITHUB_REPOSITORY/deployments/$id/statuses?per_page=1" --jq '.[0].state // ""') || {
      echo "resolve: cannot read the status of deployment $id" >&2
      return 1
    }
    if [[ "$state" == success ]]; then
      if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
        echo "resolve: successful deployment $id has a malformed digest '$digest'" >&2
        return 1
      fi
      DEPLOYED="$registry/gk-cakes-$1@$digest"
      return 0
    fi
  done <<<"$list"
  if ((any)); then
    echo "resolve: staging-$1 has deployment records, but none of the newest 100 succeeded" >&2
    return 1
  fi
}

out=()
for c in api ui; do
  upper=$(tr '[:lower:]' '[:upper:]' <<<"$c")
  new_var="NEW_$upper"
  deployed "$c" || exit 1
  candidate=${!new_var:-$DEPLOYED}
  dep=${DEPLOYED:-$candidate}
  if [[ -z "$candidate" ]]; then
    echo "resolve: no $c image to check (nothing deployed and none given)" >&2
    exit 1
  fi
  out+=("DEPLOYED_$upper=$dep" "CANDIDATE_$upper=$candidate")
done
for line in "${out[@]}"; do
  echo "resolve: $line"
  if [[ -n "${GITHUB_ENV:-}" ]]; then
    echo "$line" >>"$GITHUB_ENV"
  fi
done
