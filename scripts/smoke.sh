#!/usr/bin/env bash
set -euo pipefail
image=${1:?Docker image required}
# In CI also verify the native architecture, OCI labels and the running binary version.
if [ "$#" -ge 4 ]; then
  expected_version=$2 expected_commit=$3 expected_arch=$4
  test "$(docker image inspect --format '{{.Architecture}}' "$image")" = "$expected_arch" || { echo "Image-Architektur stimmt nicht mit $expected_arch überein."; exit 1; }
  test "$(docker image inspect --format '{{.Os}}' "$image")" = linux || { echo 'Image ist kein Linux-Image.'; exit 1; }
  test "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$image")" = "$expected_version" || { echo 'OCI-Versionslabel stimmt nicht mit der getesteten Projektversion überein.'; exit 1; }
  test "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")" = "$expected_commit" || { echo 'OCI-Commitlabel stimmt nicht mit dem getesteten Commit überein.'; exit 1; }
  test "$(docker run --rm "$image" version)" = "$expected_version" || { echo 'Anwendungsversion und Release-Version stimmen nicht überein.'; exit 1; }
fi
name="cctv-smoke-${RANDOM}"
volume="${name}-data"
tmp=$(mktemp -d)
cleanup() { result=$?; if [ "$result" != 0 ]; then docker logs --tail 50 "$name" 2>&1 || true; docker inspect --format '{{json .State}}' "$name" || true; fi; docker rm -f "$name" >/dev/null 2>&1 || true; docker volume rm "$volume" >/dev/null 2>&1 || true; rm -rf "$tmp"; }
trap cleanup EXIT
mkdir "$tmp/recordings"
chmod 755 "$tmp" "$tmp/recordings"
docker volume create "$volume" >/dev/null
# The named volume inherits the image's /data ownership for UID 10001.
docker run --rm --mount "source=$volume,target=/data" -e CTV_ADMIN_PASSWORD='synthetic smoke test password' "$image" admin --username smoke
docker run -d --name "$name" --read-only --cap-drop ALL --security-opt no-new-privileges --tmpfs /tmp:rw,size=32m -p 127.0.0.1::8080 -e CTV_DEV_HTTP=true -e CTV_BASE_URL=http://localhost:8080 --mount "source=$volume,target=/data" --mount "type=bind,source=$tmp/recordings,target=/recordings,readonly" "$image"
port=$(docker port "$name" 8080/tcp | head -1 | cut -d: -f2)
for _ in $(seq 1 40); do if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null; then break; fi; sleep 1; done
curl -fsS "http://127.0.0.1:$port/" | grep -q 'lang="de"'
code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/api/cameras")
test "$code" = 401
curl -fsS -c "$tmp/cookies" -H 'Origin: http://localhost:8080' -H 'Content-Type: application/json' --data '{"username":"smoke","password":"synthetic smoke test password"}' "http://127.0.0.1:$port/auth/local"
curl -fsS -b "$tmp/cookies" "http://127.0.0.1:$port/api/session" | grep -q '"role":"admin"'
docker restart "$name" >/dev/null
port=$(docker port "$name" 8080/tcp | head -1 | cut -d: -f2)
for _ in $(seq 1 40); do if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null; then break; fi; sleep 1; done
curl -fsS -b "$tmp/cookies" "http://127.0.0.1:$port/api/session" | grep -q '"role":"admin"'
docker exec "$name" /usr/local/bin/cctv healthcheck
docker exec "$name" /usr/local/bin/go2rtc -version | grep -q "1.9.14"
for _ in $(seq 1 20); do if curl -fsS -b "$tmp/cookies" "http://127.0.0.1:$port/api/live" | grep -q '"ready":true'; then break; fi; sleep 1; done
curl -fsS -b "$tmp/cookies" "http://127.0.0.1:$port/api/live" | grep -q '"ready":true'
# Read-only originals and non-root runtime are mandatory.
test "$(docker inspect --format '{{.Config.User}}' "$name")" = '10001:10001'
test "$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/recordings"}}{{.RW}}{{end}}{{end}}' "$name")" = false
docker image inspect --format 'Image-Größe (unkomprimiert): {{.Size}} Bytes' "$image"
docker stats --no-stream --format 'Laufzeit: {{.MemUsage}} RAM, {{.CPUPerc}} CPU' "$name"
echo 'Docker-Smoke-Test erfolgreich.'
