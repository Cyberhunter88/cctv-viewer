#!/usr/bin/env bash
# Copy the exact tested commit image to release tags without rebuilding.
set -euo pipefail
: "${IMAGE:?}" "${VERSION:?}" "${COMMIT:?}" "${RELEASES_FILE:?}" "${PROOF_FILE:?}"
operation=promote
if [ "${CHECK_ONLY:-false}" = true ]; then operation=preflight; fi
python3 scripts/image_registry.py "$operation" "$IMAGE" "$VERSION" "$COMMIT" "$PROOF_FILE" "$RELEASES_FILE"
