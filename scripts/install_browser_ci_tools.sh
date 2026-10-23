#!/usr/bin/env bash
# Install Playwright system dependencies without the runner's slow Azure mirror.
set -euo pipefail

browser=${1:?Browser fehlt}
case "$browser" in
  chrome|webkit) ;;
  *) echo "Unbekannter Browser: $browser" >&2; exit 2 ;;
esac

source_file=${2:-/etc/apt/sources.list.d/ubuntu.sources}
if [ ! -r "$source_file" ]; then
  echo "Ubuntu-Paketquellen nicht lesbar: $source_file" >&2
  exit 1
fi

directory=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/cctv-browser-apt.XXXXXX")
trap 'rm -rf "$directory"' EXIT
sources="$directory/ubuntu.sources"
config="$directory/apt.conf"
sed -E \
  -e 's@mirror\+file:/etc/apt/apt-mirrors-security.txt@https://security.ubuntu.com/ubuntu/@g' \
  -e 's@mirror\+file:/etc/apt/apt-mirrors.txt@https://archive.ubuntu.com/ubuntu/@g' \
  -e 's@https?://azure.archive.ubuntu.com/ubuntu/?@https://archive.ubuntu.com/ubuntu/@g' \
  -e 's@http://archive.ubuntu.com/ubuntu/?@https://archive.ubuntu.com/ubuntu/@g' \
  -e 's@http://security.ubuntu.com/ubuntu/?@https://security.ubuntu.com/ubuntu/@g' \
  "$source_file" > "$sources"
cat > "$config" <<EOF
Dir::Etc::sourcelist "$sources";
Dir::Etc::sourceparts "-";
Acquire::Retries "2";
Acquire::http::Timeout "20";
Acquire::https::Timeout "20";
APT::Update::Error-Mode "any";
EOF

# Run Playwright as root so its apt-get subprocesses inherit APT_CONFIG.
sudo env APT_CONFIG="$config" timeout 240s "$(command -v npx)" playwright install-deps "$browser"
if ! command -v ffmpeg >/dev/null 2>&1 || ! command -v ffprobe >/dev/null 2>&1; then
  sudo env APT_CONFIG="$config" timeout 180s apt-get install -y --no-install-recommends ffmpeg
fi
command -v ffmpeg >/dev/null
command -v ffprobe >/dev/null
