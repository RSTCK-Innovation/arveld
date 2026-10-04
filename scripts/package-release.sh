#!/usr/bin/env bash
set -euo pipefail

# Build and test locally, without registry credentials. Publication is a separate job.
version=${1:?Usage: package-release.sh VERSION ARCH OUTPUT_DIRECTORY}
arch=${2:?Missing architecture}
output=${3:?Missing output directory}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$ ]] || { echo 'Expected vX.Y.Z-rc.N' >&2; exit 1; }
[[ "$arch" == amd64 || "$arch" == arm64 ]] || { echo 'Expected amd64 or arm64' >&2; exit 1; }
# The Agent is built from its own Docker context and carries identical legal files.
cmp LICENSE agent/LICENSE
cmp NOTICE agent/NOTICE
mkdir -p "$output/assets" "$output/images"
output=$(cd "$output" && pwd)
revision=$(git rev-parse HEAD)
export DOCKER_DEFAULT_PLATFORM="linux/$arch"
export ARVELD_CONTROLLER_IMAGE="arveld:$version-$arch"
export ARVELD_AGENT_IMAGE="arveld-agent:$version-$arch"
export ARVELD_TEST_COMPOSE_FILE="$PWD/docker/compose.release.yaml"
printf '[CONTEXT] Build real release executables for %s on native Linux %s. No registry publication occurs here.\n' "$version" "$arch"

for component in arveld arveld-agent; do
  context=.
  [[ "$component" != arveld-agent ]] || context=agent
  image="$component:$version-$arch"
  printf '[BUILD] Build the %s runtime image from this commit.\n' "$component"
  docker build --build-arg "VERSION=$version" --build-arg "REVISION=$revision" --tag "$image" "$context"
  printf '[CHECK] The %s image must report version %s.\n' "$component" "$version"
  [[ "$(docker run --rm "$image" --version)" == "$component $version" ]]
  package="$output/$component-${version}_linux_$arch"
  mkdir -p "$package"
  container=$(docker create "$image")
  trap 'docker rm "$container" >/dev/null' EXIT
  docker cp "$container:/$component" "$package/$component"
  docker cp "$container:/usr/share/licenses/$component/." "$package/"
  docker rm "$container" >/dev/null
  trap - EXIT
  chmod +x "$package/$component"
  printf '[CHECK] The exact executable extracted from the image must report %s %s.\n' "$component" "$version"
  [[ "$("$package/$component" --version)" == "$component $version" ]]
  cmp LICENSE "$package/LICENSE"
  cmp NOTICE "$package/NOTICE"
  [[ -s "$package/THIRD_PARTY_NOTICES.txt" ]]
  grep -Fxq 'THIRD-PARTY NOTICES' "$package/THIRD_PARTY_NOTICES.txt"
  cp README.md SECURITY.md "$package/"
  tar -czf "$output/assets/$component-${version}_linux_$arch.tar.gz" -C "$package" .
  docker save "$image" | gzip > "$output/images/$component-$arch.tar.gz"
  printf '[PASS] %s version checks passed; archive and image saved for downstream jobs.\n' "$component"
done

printf '%s\n' '[BUILD] Generate standalone installers with the exact package version embedded.'
sh scripts/package-installers.sh "$version" "$output/assets"

# The generated release recipe uses these same services, volumes and settings.
printf '%s\n' '[TEST] Start the real controller and Agent with Compose; verify measurements and persistence after recreation.'
task test:controller:docker
printf '%s\n' '[PASS] Native packaging and Compose persistence checks passed.'
