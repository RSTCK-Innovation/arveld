#!/bin/sh
# Publish only verified release installers; binaries remain on GitHub Releases.
set -eu
version=${1:?Usage: publish-installers.sh VERSION ASSETS_DIRECTORY}
assets=${2:?Missing assets directory}
: "${ARVELD_INSTALLERS_BUCKET:?Missing ARVELD_INSTALLERS_BUCKET}"
promote=${PROMOTE_INSTALLERS:-false}
case "$promote" in true|false) ;; *) echo 'PROMOTE_INSTALLERS must be true or false.' >&2; exit 1 ;; esac
case "$version" in *[!0-9A-Za-z.-]*) exit 1 ;; esac
printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$' || exit 1
if [ "$promote" = true ] && ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo 'Only a stable release (vX.Y.Z) can update the root installer URLs.' >&2
  exit 1
fi
case "$ARVELD_INSTALLERS_BUCKET" in *[!a-z0-9.-]*) echo 'Invalid R2 bucket name.' >&2; exit 1 ;; esac
assets=$(CDPATH= cd -- "$assets" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 1' HUP INT TERM
printf '[CHECK] Validate installer syntax, embedded component/version and checksums for %s before any R2 upload.\n' "$version"

# Validate the complete allowlist before the first remote write. Never upload
# an assets directory recursively: it also contains private release binaries.
for component in arveld arveld-agent; do
  file=install-$component.sh
  [ -f "$assets/$file" ] && [ ! -L "$assets/$file" ]
  cp "$assets/$file" "$work/$file"
  grep -Fx "export ARVELD_VERSION=$version" "$work/$file" >/dev/null
  grep -Fx "export ARVELD_INSTALL_COMPONENT=$component" "$work/$file" >/dev/null
  sh -n "$work/$file"
  awk -v file="$file" '$2 == file || $2 == "./" file { print $1 "  " file; count++ }
    END { if (count != 1) exit 1 }' "$assets/SHA256SUMS" >> "$work/SHA256SUMS"
done
(cd "$work" && sha256sum --check SHA256SUMS)
printf '%s\n' '[PASS] Both installer assets passed validation; only scripts and their checksums will be uploaded.'

upload() {
  wrangler r2 object put "$ARVELD_INSTALLERS_BUCKET/$1" --remote \
    --file "$work/$2" --content-type 'text/plain; charset=utf-8' --cache-control "$3"
}

for file in install-arveld.sh install-arveld-agent.sh SHA256SUMS; do
  printf '[UPLOAD] Publish versioned object %s/%s.\n' "$version" "$file"
  upload "$version/$file" "$file" 'public, max-age=31536000, immutable'
done
# Update the convenient entry points only after every versioned object exists.
# Recovery runs leave them alone unless promotion was explicitly requested.
if [ "$promote" = true ]; then
  printf '[PROMOTE] Update the root installer URLs to stable release %s.\n' "$version"
  for file in install-arveld.sh install-arveld-agent.sh; do
    upload "$file" "$file" 'no-store'
  done
else
  printf '%s\n' '[POLICY] Promotion is disabled; both root installer URLs remain unchanged.'
fi
printf 'Published installers: https://install.arveld.com/%s/\n' "$version"
