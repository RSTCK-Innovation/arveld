#!/usr/bin/env bash
set -euo pipefail
: "${VERSION:?Missing VERSION}" "${GH_REPO:?Missing GH_REPO}" "${GITHUB_OUTPUT:?Missing GITHUB_OUTPUT}"
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$ ]]
printf '[CHECK] Refuse an existing release for %s before changing any registry tag.\n' "$VERSION"
# Fail before changing any registry tag when a draft or published release exists.
# API errors must also stop publication, rather than being mistaken for absence.
existing_tags=$(gh api --paginate "repos/$GH_REPO/releases?per_page=100" --jq '.[].tag_name')
if grep -Fqx "$VERSION" <<< "$existing_tags"; then
  echo "Release $VERSION already exists; publication is refused." >&2
  exit 1
fi

registry="ghcr.io/$(echo "$GH_REPO" | tr '[:upper:]' '[:lower:]')"
: > release/assets/images.txt

for component in arveld arveld-agent; do
  printf '[PUBLISH] Load and push the already-tested %s images for amd64 and arm64; no rebuild.\n' "$component"
  image="$registry"
  output_prefix=controller
  if [[ "$component" == arveld-agent ]]; then
    image="$registry-agent"
    output_prefix=agent
  fi
  sources=()
  for arch in amd64 arm64; do
    docker load --input "release/images/$component-$arch.tar.gz"
    docker tag "$component:$VERSION-$arch" "$image:$VERSION-$arch"
    docker push "$image:$VERSION-$arch"
    digest=$(docker buildx imagetools inspect "$image:$VERSION-$arch" --format '{{json .Manifest}}' | jq -er .digest)
    sources+=("$image@$digest")
  done
  docker buildx imagetools create --tag "$image:$VERSION" "${sources[@]}"
  digest=$(docker buildx imagetools inspect "$image:$VERSION" --format '{{json .Manifest}}' | jq -er .digest)
  echo "${output_prefix}_name=$image" >> "$GITHUB_OUTPUT"
  echo "${output_prefix}_digest=$digest" >> "$GITHUB_OUTPUT"
  echo "$image:$VERSION@$digest" >> release/assets/images.txt
  if [[ "$component" == arveld ]]; then
    export ARVELD_CONTROLLER_IMAGE="$image:$VERSION"
  else
    export ARVELD_AGENT_IMAGE="$image:$VERSION"
  fi
done

printf '%s\n' '[METADATA] Generate versioned Compose image references, image digests, component manifest and release checksums.'
# Resolve only image placeholders; leave deployment settings configurable by operators.
python3 - <<'PY'
import os
import re
from pathlib import Path
compose = Path('docker/compose.release.yaml').read_text()
for name in ['ARVELD_CONTROLLER_IMAGE', 'ARVELD_AGENT_IMAGE']:
    compose = re.sub(r'\$\{' + name + r':\?[^}]+\}', os.environ[name], compose)
Path('release/assets/compose.yaml').write_text(compose)
PY
cp internal/components/components.lock.json release/assets/components.lock.json
(cd release/assets && sha256sum ./* > ../SHA256SUMS && mv ../SHA256SUMS .)
printf '%s\n' '[PASS] Tested images published and release metadata/checksums generated.'
