#!/usr/bin/env bash
set -euo pipefail

candidate="$(realpath "${1:?verified candidate directory required}")"
image="${2:?local image tag required}"
evidence="${3:?evidence directory required}"
mkdir -p "$evidence"
temporary="$(mktemp -d)"
container="exta-acceptance-${GITHUB_RUN_ID:-local}-${RANDOM}"
cleanup() {
  docker rm -f "$container" "$container-second" >/dev/null 2>&1 || true
  if test -d "$temporary/notices"; then chmod -R u+rwX "$temporary/notices"; fi
  rm -rf "$temporary"
}
trap cleanup EXIT

docker image inspect "$image" > "$evidence/image.json"
test "$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")" = linux/amd64
test "$(docker image inspect --format '{{.Config.User}}' "$image")" = 65532:65532
docker create --name "$container" "$image" version >/dev/null
docker cp "$container:/exta" "$temporary/exta"
cmp "$candidate/exta-linux-amd64" "$temporary/exta"
mkdir "$temporary/notices"
docker cp "$container:/usr/share/exta" - | tar -x -C "$temporary/notices" --strip-components=1 --no-same-owner --delay-directory-restore
cmp "$candidate/dependency-inventory.json" "$temporary/notices/dependency-inventory.json"
cmp "$candidate/frontend-provenance.json" "$temporary/notices/frontend-provenance.json"
diff -r "$candidate/licenses" "$temporary/notices/licenses"
diff -r "$candidate/frontend-licenses" "$temporary/notices/frontend-licenses"
docker rm "$container" >/dev/null
docker run --rm --network none "$image" version > "$evidence/version.json"

mkdir -m 700 "$temporary/data"
identity="$(id -u):$(id -g)"
docker run --rm --network none --user "$identity" \
  --mount "type=bind,src=$temporary/data,dst=/var/lib/exta" \
  "$image" init --data-dir /var/lib/exta/state > "$temporary/bootstrap"
test "$(stat -c '%a' "$temporary/data/state/master.key")" = 600
mkdir -m 700 "$temporary/data/tls"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost \
  -addext subjectAltName=DNS:localhost \
  -keyout "$temporary/data/tls/key.pem" -out "$temporary/data/tls/cert.pem" >/dev/null 2>&1
chmod 600 "$temporary/data/tls/"*.pem
cat > "$temporary/data/state/config.json" <<'JSON'
{"listen":"0.0.0.0:8789","origin":"https://localhost:18789","tls_cert_file":"/var/lib/exta/tls/cert.pem","tls_key_file":"/var/lib/exta/tls/key.pem"}
JSON
key_before="$(sha256sum "$temporary/data/state/master.key" | cut -d ' ' -f 1)"
docker run -d --name "$container" --user "$identity" --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true -p 127.0.0.1:18789:8789 \
  --mount "type=bind,src=$temporary/data,dst=/var/lib/exta" \
  "$image" serve --data-dir /var/lib/exta/state --maintenance >/dev/null
for attempt in {1..50}; do
  if curl --silent --fail --cacert "$temporary/data/tls/cert.pem" https://localhost:18789/healthz > "$temporary/health"; then break; fi
  sleep 0.2
done
curl --silent --show-error --fail --cacert "$temporary/data/tls/cert.pem" \
  -H 'Accept: text/html' https://localhost:18789/app/login > "$temporary/index.html"
grep -q '<div id="root">' "$temporary/index.html"
set +e
timeout 20s docker run --rm --name "$container-second" --network none --user "$identity" \
  --mount "type=bind,src=$temporary/data,dst=/var/lib/exta" \
  "$image" serve --data-dir /var/lib/exta/state --maintenance > "$temporary/second" 2>&1
status=$?
set -e
if test "$status" -ne 1; then
  echo "Expected lock refusal, received exit $status" >&2
  exit 1
fi
grep -q 'exclusive data-directory lock' "$temporary/second"
docker stop --time 45 "$container" >/dev/null
test "$(docker inspect --format '{{.State.ExitCode}}' "$container")" = 0
docker start "$container" >/dev/null
for attempt in {1..50}; do
  if curl --silent --fail --cacert "$temporary/data/tls/cert.pem" https://localhost:18789/healthz > "$temporary/health"; then break; fi
  sleep 0.2
done
curl --silent --show-error --fail --cacert "$temporary/data/tls/cert.pem" https://localhost:18789/healthz >/dev/null
docker stop --time 45 "$container" >/dev/null
test "$(docker inspect --format '{{.State.ExitCode}}' "$container")" = 0
test "$key_before" = "$(sha256sum "$temporary/data/state/master.key" | cut -d ' ' -f 1)"
printf 'PASS: exact Linux binary, nonroot runtime, TLS UI, exclusive lock, graceful stop and persistent restart; no provider configured.\n' > "$evidence/result.txt"
sha256sum "$candidate/exta-linux-amd64" > "$evidence/binary.sha256"
