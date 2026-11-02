#!/usr/bin/env bash
# Records deployments in GitHub with the exact artifact digest, so the release
# gate can recognise a replayed release. Requires GH_TOKEN with
# deployments:write and GITHUB_REPOSITORY.
#   record-deployment.sh create <environment> <commit-sha> <digest> <version>   -> prints deployment id
#   record-deployment.sh status <deployment-id> <success|failure|in_progress> <description>
set -euo pipefail

case "${1:-}" in
create)
  environment=$2 sha=$3 digest=$4 version=$5
  jq -n --arg ref "$sha" --arg env "$environment" --arg digest "$digest" --arg version "$version" '{
      ref: $ref, environment: $env, auto_merge: false, required_contexts: [],
      transient_environment: false, production_environment: ($env | startswith("production-")),
      description: ("gk " + $version),
      payload: {artifact_digest: $digest, version: $version}
    }' |
    gh api -X POST "repos/$GITHUB_REPOSITORY/deployments" --input - --jq .id
  ;;
status)
  id=$2 state=$3 description=$4
  gh api -X POST "repos/$GITHUB_REPOSITORY/deployments/$id/statuses" \
    -f state="$state" -f description="$description" --jq .state >/dev/null
  ;;
*)
  echo "usage: $0 create <environment> <sha> <digest> <version> | status <id> <state> <description>" >&2
  exit 2
  ;;
esac
