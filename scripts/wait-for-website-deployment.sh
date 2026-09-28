#!/usr/bin/env bash
set -euo pipefail

expected=${1:?Usage: wait-for-website-deployment.sh EXPECTED_JSON DEPLOYMENT_URL}
url=${2:?Usage: wait-for-website-deployment.sh EXPECTED_JSON DEPLOYMENT_URL}
[[ -s "$expected" ]] || { echo 'Expected deployment metadata is missing or empty.' >&2; exit 1; }

actual=$(mktemp)
trap 'rm -f "$actual"' EXIT

# A successful HTTP response can still come from the preceding deployment.
for attempt in {1..12}; do
  if curl --fail --silent --show-error --proto '=https' \
    --connect-timeout 5 --max-time 15 --header 'Cache-Control: no-cache' \
    "$url" -o "$actual" && cmp -s "$expected" "$actual"; then
    printf 'The expected website revision is public (attempt %s/12).\n' "$attempt"
    exit 0
  fi
  if (( attempt < 12 )); then
    printf 'Waiting for the expected website revision (attempt %s/12).\n' "$attempt"
    sleep 5
  fi
done

echo 'The website did not serve the expected deployment metadata after 12 attempts.' >&2
exit 1
