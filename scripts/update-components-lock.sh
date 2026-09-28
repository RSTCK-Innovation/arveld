#!/usr/bin/env bash

set -euo pipefail

readonly official_releases_url="https://prometheus.io/download.json"

if [[ "$#" -ne 2 ]]; then
  echo "usage: $0 <prometheus-version> <alertmanager-version>" >&2
  exit 2
fi

prometheus_version="${1#v}"
alertmanager_version="${2#v}"
script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_directory="$(cd "${script_directory}/.." && pwd)"
releases_url="${PROMETHEUS_RELEASES_URL:-${official_releases_url}}"
lock_file="${COMPONENTS_LOCK_FILE:-${repository_directory}/internal/components/components.lock.json}"
lock_directory="$(dirname "${lock_file}")"

mkdir -p "${lock_directory}"
releases_file="$(mktemp "${TMPDIR:-/tmp}/arveld-component-releases.XXXXXX")"
partial_lock_file="$(mktemp "${lock_file}.partial.XXXXXX")"

cleanup() {
  rm -f "${releases_file}" "${partial_lock_file}"
}
trap cleanup EXIT

curl \
  --fail \
  --location \
  --silent \
  --show-error \
  --output "${releases_file}" \
  "${releases_url}"

jq \
  --exit-status \
  --arg prometheus_version "${prometheus_version}" \
  --arg alertmanager_version "${alertmanager_version}" \
  '
    ["darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"] as $expected_platforms
    | . as $release_index
    | def component_lock($name; $version):
        ($release_index[$name] // error("download index has no " + $name + " releases"))
        | map(select(.version == ("v" + $version)))
        | if length == 1 then
            .[0]
          else
            error("expected one " + $name + " release for v" + $version)
          end
        | [
            .files[]
            | select(.kind == "archive")
            | select(.os == "darwin" or .os == "linux" or .os == "windows")
            | select(.arch == "amd64" or .arch == "arm64")
            | {
                key: (.os + "/" + .arch),
                value: {
                  url: .url,
                  sha256: .sha256,
                  size: .size
                }
              }
          ]
        | sort_by(.key)
        | if map(.key) != $expected_platforms then
            error($name + " release does not contain every supported platform")
          elif any(
            .[];
            (.value.url | type) != "string"
              or (.value.url | startswith("https://") | not)
              or (.value.sha256 | type) != "string"
              or (.value.sha256 | test("^[0-9a-f]{64}$") | not)
              or (.value.size | type) != "number"
              or .value.size <= 0
          ) then
            error($name + " release contains invalid artifact metadata")
          else
            {
              version: $version,
              artifacts: from_entries
            }
          end;
      {
        schema_version: 1,
        components: {
          prometheus: component_lock("prometheus"; $prometheus_version),
          alertmanager: component_lock("alertmanager"; $alertmanager_version)
        }
      }
  ' \
  "${releases_file}" >"${partial_lock_file}"

mv "${partial_lock_file}" "${lock_file}"
echo "updated ${lock_file} for prometheus v${prometheus_version} and alertmanager v${alertmanager_version}"
