#!/bin/sh
# Build verified, pinned upstream sources; omit standalone C ioctl generators.
# The generated Go definitions are already included by upstream.
set -eu
output=${1:?Output path required}
mkdir -p "$(dirname "$output")"
output=$(cd "$(dirname "$output")" && pwd)/$(basename "$output")
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
cd "$stage"
go mod download github.com/AlexxIT/go2rtc@v1.9.14
source_dir=$(go list -m -f '{{.Dir}}' github.com/AlexxIT/go2rtc@v1.9.14)
cp -R "$source_dir/." "$stage/"
chmod -R u+w "$stage"
rm "$stage/pkg/alsa/device/asound_arch.c" "$stage/pkg/v4l2/device/videodev2_arch.c"
CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o "$output" .
cp LICENSE "$(dirname "$output")/go2rtc-LICENSE"
