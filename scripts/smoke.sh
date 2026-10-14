#!/usr/bin/env bash
set -euo pipefail
image=${1:?Docker image required}
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
for i in $(seq 1 40); do if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null; then break; fi; sleep 1; done
curl -fsS "http://127.0.0.1:$port/" | grep -q 'lang="de"'
code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/api/cameras")
test "$code" = 401
curl -fsS -c "$tmp/cookies" -H 'Origin: http://localhost:8080' -H 'Content-Type: application/json' --data '{"username":"smoke","password":"synthetic smoke test password"}' "http://127.0.0.1:$port/auth/local"
curl -fsS -b "$tmp/cookies" "http://127.0.0.1:$port/api/session" | grep -q '"role":"admin"'
docker restart "$name" >/dev/null
port=$(docker port "$name" 8080/tcp | head -1 | cut -d: -f2)
for i in $(seq 1 40); do if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null; then break; fi; sleep 1; done
curl -fsS -b "$tmp/cookies" "http://127.0.0.1:$port/api/session" | grep -q '"role":"admin"'
docker exec "$name" /usr/local/bin/cctv healthcheck
# Read-only originals and non-root runtime are mandatory.
test "$(docker inspect --format '{{.Config.User}}' "$name")" = '10001:10001'
test "$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/recordings"}}{{.RW}}{{end}}{{end}}' "$name")" = false
docker image inspect --format 'Image-Größe (unkomprimiert): {{.Size}} Bytes' "$image"
docker stats --no-stream --format 'Laufzeit: {{.MemUsage}} RAM, {{.CPUPerc}} CPU' "$name"
echo 'Docker-Smoke-Test erfolgreich.'
