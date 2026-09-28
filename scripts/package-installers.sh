#!/bin/sh
set -eu
version=${1:?Usage: package-installers.sh VERSION_OR_--unversioned OUTPUT_DIRECTORY}
output=${2:?Missing output directory}
if [ "$version" = --unversioned ]; then
  version=
else
  case "$version" in *[!0-9A-Za-z.-]*) exit 1 ;; esac
  printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$' || exit 1
fi
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
mkdir -p "$output"
for component in arveld arveld-agent; do
  {
    printf '#!/bin/sh\nexport ARVELD_INSTALL_COMPONENT=%s\n' "$component"
    if [ -n "$version" ]; then printf 'export ARVELD_VERSION=%s\n' "$version"; fi
    tail -n +2 "$source_dir/install-linux.sh"
  } > "$output/install-$component.sh"
  chmod 0755 "$output/install-$component.sh"
done
