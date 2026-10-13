#!/bin/sh
set -eu

image="${1:?usage: scripts/container-smoke-test.sh IMAGE}"
suffix="$$"
container="savorum-smoke-${suffix}"
volume="savorum-smoke-data-${suffix}"

cleanup() {
  docker rm --force "$container" >/dev/null 2>&1 || true
  docker volume rm --force "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT

user="$(docker image inspect --format '{{.Config.User}}' "$image")"
if [ "$user" != "65532:65532" ]; then
  echo "image user is $user, want 65532:65532" >&2
  exit 1
fi
if [ "$(docker image inspect --format '{{index .Config.ExposedPorts "8080/tcp"}}' "$image")" != "{}" ]; then
  echo "image does not expose 8080/tcp" >&2
  exit 1
fi

docker volume create "$volume" >/dev/null
docker run --detach \
  --name "$container" \
  --read-only \
  --tmpfs /tmp:size=256m,mode=1777 \
  --volume "$volume:/data" \
  "$image" >/dev/null

for _ in $(seq 1 45); do
  status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$container")"
  if [ "$status" = "healthy" ]; then
    break
  fi
  if [ "$status" = "unhealthy" ] || [ "$(docker inspect --format '{{.State.Running}}' "$container")" != "true" ]; then
    docker logs "$container" >&2
    echo "container became $status before it was ready" >&2
    exit 1
  fi
  sleep 1
done
if [ "$(docker inspect --format '{{.State.Health.Status}}' "$container")" != "healthy" ]; then
  docker logs "$container" >&2
  echo "container did not become healthy" >&2
  exit 1
fi

docker exec "$container" /savorum healthcheck
docker exec "$container" /savorum healthcheck --url http://127.0.0.1:8080/openapi.json

docker stop --time 20 "$container" >/dev/null
if [ "$(docker inspect --format '{{.State.ExitCode}}' "$container")" != "0" ]; then
  docker logs "$container" >&2
  echo "container did not exit cleanly after SIGTERM" >&2
  exit 1
fi

docker rm "$container" >/dev/null
docker run --detach \
  --name "$container" \
  --read-only \
  --tmpfs /tmp:size=256m,mode=1777 \
  --volume "$volume:/data" \
  "$image" >/dev/null
for _ in $(seq 1 45); do
  if docker exec "$container" /savorum healthcheck >/dev/null 2>&1; then
    exit 0
  fi
  sleep 1
done
docker logs "$container" >&2
echo "container could not restart from its persistent volume" >&2
exit 1
