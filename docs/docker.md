# Linux amd64 container

The image packages the exact accepted Linux executable; it does not rebuild application code. Its digest-pinned [Distroless static Debian 13 nonroot base](https://github.com/GoogleContainerTools/distroless) supplies trusted CA certificates without a shell or package manager. The image runs as UID/GID 65532 by default. No container or host deployment is performed by the release workflow.

The authorized public registry destination is `ghcr.io/qianfuv/exta`. Tags `0.1.0` and `sha-<source-sha>` and the registry digest are recorded only after native Linux/race, Windows, browser and container gates pass. Confirm the published package is publicly readable before distributing its pull command. Dependency inventories and Go/frontend license notices are readable under `/usr/share/exta/` in the image.

Build locally on an isolated Linux amd64 Docker host after release verification:

```sh
docker build --platform linux/amd64 -f Dockerfile \
  --build-arg SOURCE_SHA="$(git rev-parse HEAD)" --build-arg VERSION=0.1.0 \
  -t exta:local artifacts/candidate
```

The build context is the verified candidate directory, which contains `exta-linux-amd64`; it must not contain runtime credentials. Compare its binary SHA-256 with `release-manifest.json` before building.

## Persistent state and Compose

Use a new private host directory owned by the UID that will run Exta. Never point these initialization commands at existing user data. Set `EXTA_IMAGE` to the verified image digest when one is published, or `exta:local` for local acceptance:

```sh
export EXTA_IMAGE=exta:local
export EXTA_UID="$(id -u)"
export EXTA_GID="$(id -g)"
mkdir -m 700 exta-data
docker compose run --rm --no-deps exta init --data-dir /var/lib/exta/state
```

Save the one-time operator credential privately. Initialization does not contact providers. The bind mount retains the SQLite database/WAL, master key, configuration and idempotency/recovery evidence across container replacement. Back up the database and separately protect its master-key recovery output using the [operational commands](operations.md); copying a live SQLite main file is not a backup.

The default configuration binds container loopback only. For the supplied bridge-network Compose file, explicitly install your TLS certificate and private key under `exta-data/tls`, owned by the same UID, with private directories and files. Set `exta-data/state/config.json` to:

```json
{
  "listen": "0.0.0.0:8789",
  "origin": "https://localhost:8789",
  "tls_cert_file": "/var/lib/exta/tls/cert.pem",
  "tls_key_file": "/var/lib/exta/tls/key.pem"
}
```

Use a certificate trusted by your browser and matching the configured origin. Keep the configuration mode 0600. The sample publishes only host loopback; adapting it for remote access requires an explicit trusted HTTPS origin and appropriate networking. Exta does not infer trust from proxy headers or disable its nonloopback TLS/Origin checks.

```sh
docker compose run --rm --no-deps exta doctor --data-dir /var/lib/exta/state
docker compose up -d
docker compose stop
```

Stopping gives finalizers time to join. Never run two containers against the same state; the directory lock rejects the second instance. For offline operations, stop the service first, then use `docker compose run --rm --no-deps exta backup ...` with explicitly mounted private output destinations. Do not remove volumes or the bind directory during upgrades. Restore retains its activity/lineage safety checks and has no force rollback bypass.
