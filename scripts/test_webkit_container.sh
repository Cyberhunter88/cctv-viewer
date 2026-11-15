#!/usr/bin/env bash
# Runs the Playwright tests with WebKit in the official Playwright container, for hosts
# where WebKit does not start natively (for example CachyOS/Arch). Needs Docker or Podman.
# Usage: scripts/test_webkit_container.sh [Playwright arguments, e.g. tests/live.spec.ts]
set -euo pipefail
cd "$(dirname "$0")/.."
engine=$(command -v podman || command -v docker || true)
if [ -z "$engine" ]; then
  echo 'Docker oder Podman wird benötigt.' >&2
  exit 1
fi
# The image must match the locked Playwright version, or WebKit and the library disagree.
version=$(node -p "require('./node_modules/@playwright/test/package.json').version")
image="mcr.microsoft.com/playwright:v${version}-noble"
npm run build
# A static server binary runs unchanged inside the Ubuntu container.
CGO_ENABLED=0 go build -o bin/cctv ./cmd/cctv
"$engine" run --rm --ipc=host \
  --volume "$PWD:/work" --workdir /work \
  --env CTV_TEST_ENGINE=webkit \
  "$image" \
  bash -c 'apt-get update -qq && apt-get install -y -qq --no-install-recommends ffmpeg python3 >/dev/null && npx playwright test "$@"' \
  playwright "$@"
