#!/usr/bin/env bash
set -euo pipefail

# Apply the versioned baseline without changing repository/package visibility,
# organization settings or paid security subscriptions. Requires repository admin.
repo=RSTCK-Innovation/arveld
for configuration in .github/rulesets/*.json; do
  name=$(jq -r .name "$configuration")
  id=$(gh api "repos/$repo/rulesets" --jq ".[] | select(.name == \"$name\") | .id")
  if [[ -n "$id" ]]; then
    gh api --method PUT "repos/$repo/rulesets/$id" --input "$configuration" --silent
  else
    gh api --method POST "repos/$repo/rulesets" --input "$configuration" --silent
  fi
done

gh api --method PUT "repos/$repo/actions/permissions" --input - <<'JSON'
{"enabled":true,"allowed_actions":"selected","sha_pinning_required":true}
JSON
gh api --method PUT "repos/$repo/actions/permissions/selected-actions" --input - <<'JSON'
{"github_owned_allowed":false,"verified_allowed":false,"patterns_allowed":["actions/checkout@*","actions/setup-go@*","actions/setup-node@*","actions/cache@*","actions/upload-artifact@*","actions/download-artifact@*","actions/attest-build-provenance@*","actions/attest@*","docker/setup-buildx-action@*","docker/login-action@*","oven-sh/setup-bun@*","go-task/setup-task@*","golangci/golangci-lint-action@*","anchore/sbom-action@*","github/codeql-action/*@*"]}
JSON
gh api --method PUT "repos/$repo/actions/permissions/workflow" --input - <<'JSON'
{"default_workflow_permissions":"read","can_approve_pull_request_reviews":false}
JSON
gh api --silent --method PATCH "repos/$repo" --input - <<'JSON'
{"allow_squash_merge":true,"allow_merge_commit":false,"allow_rebase_merge":false,"delete_branch_on_merge":true,"allow_auto_merge":false}
JSON
gh api --method PUT "repos/$repo/immutable-releases"
gh api --method PUT "repos/$repo/vulnerability-alerts"
gh api --method PUT "repos/$repo/automated-security-fixes"
