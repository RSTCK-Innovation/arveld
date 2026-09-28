#!/usr/bin/env bash
set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_directory="$(cd "${script_directory}/.." && pwd)"
lock_file="${COMPONENTS_LOCK_FILE:-${repository_directory}/internal/components/components.lock.json}"
releases_url="${PROMETHEUS_RELEASES_URL:-https://prometheus.io/download.json}"
releases_file="$(mktemp "${TMPDIR:-/tmp}/arveld-component-updates.XXXXXX")"
trap 'rm -f "$releases_file"' EXIT

curl --fail --location --silent --show-error --retry 3 --max-time 60 \
  --output "$releases_file" "$releases_url"

jq --exit-status --raw-output --slurpfile lock "$lock_file" '
  def version:
    if type == "string" and test("^[0-9]+\\.[0-9]+\\.[0-9]+$") then
      split(".") | map(tonumber)
    else error("invalid pinned component version") end;
  . as $index
  | ["prometheus", "alertmanager"]
  | map(
      . as $name
      | $lock[0].components[$name].version as $current
      | ($current | version) as $current_version
      | ($index[$name]
         | map(.version | select(type == "string")
               | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+$")) | ltrimstr("v"))
         | sort_by(version) | last) as $latest
      | if $latest == null then error("no stable release for " + $name) else . end
      | ($latest | version) as $latest_version
      | (if $latest_version > $current_version then "Update available"
         elif $latest_version == $current_version then "Up to date"
         else "Pinned version is newer" end) as $status
      | "| \($name) | \($current) | \($latest) | \($status) |"
    )
  | ["## Managed components", "",
     "Source: [official Prometheus release index](https://prometheus.io/download.json). Stable releases only; no files are changed.", "",
     "| Component | Pinned | Latest stable | Status |",
     "| --- | --- | --- | --- |"] + .
    + ["", "Adopt engine updates together with `task components:manifest:update PROMETHEUS_VERSION=... ALERTMANAGER_VERSION=...`, review the checksums and run the Linux packaging/integration checks before merging."]
  | join("\n")
' "$releases_file"
