#!/bin/sh
# Exercise actual distro tools in disposable containers, never on the caller's host.
set -eu
image=${1:?Usage: run.sh DISTRIBUTION_IMAGE}
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
version=v0.0.0-rc.0
printf '[CONTEXT] Distribution compatibility test: %s.\n' "$image"
printf '[CONTEXT] %s is a fixture version, not a published release. Fixture executables only implement --version.\n' "$version"
printf '%s\n' '[CONTEXT] This test uses real distro tools and a simulated systemctl command; native package jobs test real service startup.'
printf '%s\n' '[SETUP] Generate fixture archives, standalone installers and matching checksums.'
mkdir "$work/assets"
for component in arveld arveld-agent; do
  mkdir "$work/$component"
  printf '#!/bin/sh\n[ "$1" = --version ] || exit 1\necho "%s %s"\n' "$component" "$version" > "$work/$component/$component"
  chmod 0755 "$work/$component/$component"
  for arch in amd64 arm64; do
    tar -czf "$work/assets/$component-${version}_linux_$arch.tar.gz" -C "$work/$component" "./$component"
  done
done
sh "$root/scripts/package-installers.sh" "$version" "$work/assets"
(cd "$work/assets" && sha256sum ./* > "$work/SHA256SUMS")
mv "$work/SHA256SUMS" "$work/assets/"
docker run --rm --env ARVELD_INSTALLER_TEST=1 \
  --volume "$root/tests/installers:/tests:ro" --volume "$work/assets:/release:ro" \
  "$image" sh /tests/container.sh
