#!/usr/bin/env bash
set -euo pipefail

image="${1:?local image tag required}"
evidence="${2:?evidence directory required}"
temporary="$(mktemp -d)"
container="exta-proxy-acceptance-${GITHUB_RUN_ID:-local}-${RANDOM}"
network="$container-network"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf "$temporary"
}
trap cleanup EXIT

mkdir -p "$evidence"
docker network create "$network" >/dev/null
gateway="$(docker network inspect --format '{{(index .IPAM.Config 0).Gateway}}' "$network")"
identity="$(id -u):$(id -g)"
mkdir -m 700 "$temporary/data"
docker run --rm --network none --user "$identity" \
  --mount "type=bind,src=$temporary/data,dst=/var/lib/exta" \
  "$image" init --data-dir /var/lib/exta/state > "$temporary/bootstrap"
printf '{"listen":"0.0.0.0:8789","origin":"https://exta.example.test","trusted_proxy":{"ips":["%s"]}}\n' \
  "$gateway" > "$temporary/data/state/config.json"
docker run -d --name "$container" --network "$network" --user "$identity" \
  --read-only --cap-drop ALL --security-opt no-new-privileges:true \
  --mount "type=bind,src=$temporary/data,dst=/var/lib/exta" \
  "$image" serve --data-dir /var/lib/exta/state --maintenance >/dev/null
test -z "$(docker port "$container")"
address="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container")"
endpoint="http://$address:8789"
request() {
  curl --noproxy '*' --interface "$gateway" --connect-timeout 2 --max-time 5 \
    --silent --show-error -H 'Host: exta.example.test' "$@"
}
trusted() {
  request -H 'X-Forwarded-Proto: https' -H 'X-Real-IP: 198.51.100.9' "$@"
}
for attempt in {1..50}; do
  if trusted --fail "$endpoint/healthz" > "$temporary/health" 2>/dev/null; then break; fi
  sleep 0.2
done
trusted --fail "$endpoint/healthz" >/dev/null
trusted --fail -H 'Accept: text/html' "$endpoint/app/login" > "$temporary/index.html"
grep -q '<div id="root">' "$temporary/index.html"
test "$(request -o /dev/null -w '%{http_code}' "$endpoint/healthz")" = 403
test "$(request -H 'X-Forwarded-Proto: http' -H 'X-Real-IP: 198.51.100.9' -o /dev/null -w '%{http_code}' "$endpoint/healthz")" = 403
test "$(curl --noproxy '*' --interface "$gateway" --connect-timeout 2 --max-time 5 \
  --silent --show-error -H 'Host: attacker.example.test' -H 'X-Forwarded-Proto: https' \
  -H 'X-Real-IP: 198.51.100.9' -o /dev/null -w '%{http_code}' "$endpoint/healthz")" = 403
test "$(trusted -o /dev/null -w '%{http_code}' "$endpoint/v1/capabilities")" = 401
docker stop --time 45 "$container" >/dev/null
test "$(docker inspect --format '{{.State.ExitCode}}' "$container")" = 0
printf 'PASS: HTTP container without published ports, explicit trusted peer, HTTPS/header/Host checks, embedded UI, unauthorized API refusal and graceful stop. Synthetic host-gateway proxy; no NPM deployment or provider configured.\n' > "$evidence/proxy-result.txt"
