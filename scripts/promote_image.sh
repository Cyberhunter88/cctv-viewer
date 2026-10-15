#!/usr/bin/env bash
# Copy the exact tested commit image to release tags without rebuilding.
set -euo pipefail
: "${IMAGE:?}" "${VERSION:?}" "${COMMIT:?}" "${RELEASES_FILE:?}" "${RUNNER_TEMP:?}"
tags=$(python3 - <<'PYTAGS'
import json
import os
import sys
sys.path.insert(0, "scripts")
from release import tags_for

with open(os.environ["RELEASES_FILE"]) as source:
    releases = [release for page in json.load(source) for release in page]
version = os.environ["VERSION"]
print(",".join(tags_for("v" + version, "-" in version, releases)))
PYTAGS
)
digest=$(docker buildx imagetools inspect "$IMAGE:sha-$COMMIT" --format '{{json .Manifest}}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["digest"])')
if docker buildx imagetools inspect "$IMAGE:$VERSION" --format '{{json .Manifest}}' > "$RUNNER_TEMP/existing" 2> "$RUNNER_TEMP/inspect-error"; then
  existing=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["digest"])' "$RUNNER_TEMP/existing")
  test "$existing" = "$digest" || { echo 'Versions-Tag gehört bereits zu einem anderen Image.'; exit 1; }
else
  grep -Eiq 'not found|manifest unknown|name unknown' "$RUNNER_TEMP/inspect-error" || { cat "$RUNNER_TEMP/inspect-error"; exit 1; }
  docker buildx imagetools create -t "$IMAGE:$VERSION" "$IMAGE@$digest"
fi
IFS=',' read -ra release_tags <<< "$tags"
for tag in "${release_tags[@]}"; do
  if [ "$tag" != "$VERSION" ]; then docker buildx imagetools create -t "$IMAGE:$tag" "$IMAGE@$digest"; fi
  actual=$(docker buildx imagetools inspect "$IMAGE:$tag" --format '{{json .Manifest}}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["digest"])')
  test "$actual" = "$digest" || { echo "Digest von $tag stimmt nicht mit dem Commit-Image überein."; exit 1; }
done
echo "## Docker-Image" >> "$GITHUB_STEP_SUMMARY"
echo "\`$IMAGE:$VERSION\`" >> "$GITHUB_STEP_SUMMARY"
echo "Digest: \`$digest\`" >> "$GITHUB_STEP_SUMMARY"
docker buildx imagetools inspect "$IMAGE:$VERSION"
