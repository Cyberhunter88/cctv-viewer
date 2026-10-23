#!/usr/bin/env bash
# Ubuntu CI only. Keep package signatures and use direct HTTPS mirrors instead
# of the runner's potentially slow Azure mirror list. Never edit system sources.
set -euo pipefail
missing=()
if ! command -v ffmpeg >/dev/null 2>&1 || ! command -v ffprobe >/dev/null 2>&1; then
  missing+=(ffmpeg)
fi
if ! command -v shellcheck >/dev/null 2>&1; then
  missing+=(shellcheck)
fi
if [ "${#missing[@]}" -eq 0 ]; then
  echo 'FFmpeg, FFprobe und ShellCheck bereits vorhanden.'
  exit 0
fi
source_file=${1:-/etc/apt/sources.list.d/ubuntu.sources}
if [ ! -r "$source_file" ]; then
  echo "Ubuntu-Paketquellen nicht lesbar: $source_file" >&2
  exit 1
fi
directory=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/cctv-apt.XXXXXX")
trap 'rm -rf "$directory"' EXIT
sources="$directory/ubuntu.sources"
sed -E \
  -e 's@mirror\+file:/etc/apt/apt-mirrors-security.txt@https://security.ubuntu.com/ubuntu/@g' \
  -e 's@mirror\+file:/etc/apt/apt-mirrors.txt@https://archive.ubuntu.com/ubuntu/@g' \
  -e 's@https?://azure.archive.ubuntu.com/ubuntu/?@https://archive.ubuntu.com/ubuntu/@g' \
  -e 's@http://archive.ubuntu.com/ubuntu/?@https://archive.ubuntu.com/ubuntu/@g' \
  -e 's@http://security.ubuntu.com/ubuntu/?@https://security.ubuntu.com/ubuntu/@g' \
  "$source_file" > "$sources"
options=(
  -o "Dir::Etc::sourcelist=$sources"
  -o 'Dir::Etc::sourceparts=-'
  -o Acquire::Retries=2
  -o Acquire::http::Timeout=20
  -o Acquire::https::Timeout=20
  -o APT::Update::Error-Mode=any
)
printf 'Installiere fehlende Prüfwerkzeuge über Ubuntu HTTPS: %s\n' "${missing[*]}"
sudo timeout 120s apt-get "${options[@]}" update
sudo timeout 180s apt-get "${options[@]}" install -y --no-install-recommends "${missing[@]}"
for tool in ffmpeg ffprobe shellcheck; do
  command -v "$tool" >/dev/null
done
