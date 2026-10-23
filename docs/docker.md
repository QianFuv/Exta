# Linux amd64 container

The image packages the exact accepted Linux executable; it does not rebuild application code. Its digest-pinned [Distroless static Debian 13 nonroot base](https://github.com/GoogleContainerTools/distroless) supplies trusted CA certificates without a shell or package manager. The image runs as UID/GID 65532 by default. No container or host deployment is performed by the release workflow.

The authorized public registry destination is `ghcr.io/qianfuv/exta`. Tags `0.1.1` and `sha-<source-sha>` and the registry digest are recorded only after native Linux/race, Windows, browser and container gates pass. Confirm the published package is publicly readable before distributing its pull command. Dependency inventories and Go/frontend license notices are readable under `/usr/share/exta/` in the image.

Build locally on an isolated Linux amd64 Docker host after release verification:

```sh
docker build --platform linux/amd64 -f Dockerfile \
  --build-arg SOURCE_SHA="$(git rev-parse HEAD)" --build-arg VERSION=0.1.1 \
  -t exta:local artifacts/candidate
```

The build context is the verified candidate directory, which contains `exta-linux-amd64`; it must not contain runtime credentials. Compare its binary SHA-256 with `release-manifest.json` before building.

## Persistent state and Compose

Use a new private host directory owned by the UID that will run Exta. Never point these initialization commands at existing user data. Before using Compose, provision the dedicated proxy network described below and set `EXTA_PROXY_NETWORK` to its name. Set `EXTA_IMAGE` to the verified image digest when one is published, or `exta:local` for local acceptance:

```sh
export EXTA_IMAGE=exta:local
export EXTA_UID="$(id -u)"
export EXTA_GID="$(id -g)"
mkdir -m 700 exta-data
docker compose run --rm --no-deps exta init --data-dir /var/lib/exta/state
```

Save the one-time operator credential privately. Initialization does not contact providers. The bind mount retains the SQLite database/WAL, master key, configuration and idempotency/recovery evidence across container replacement. Back up the database and separately protect its master-key recovery output using the [operational commands](operations.md); copying a live SQLite main file is not a backup.

## HTTP behind NPM and Cloudflare

The trusted-proxy mode described here is newer than v0.1.1. The published v0.1.1 image does not support it; do not overwrite that image or tag. Use a separately accepted build containing this change.

The supplied Compose file publishes no host port. Set `EXTA_PROXY_NETWORK` to a dedicated ordinary Docker bridge shared only by NPM and Exta. Keep NPM's existing management connection. Do not attach unrelated containers, use host networking, or mount the Docker socket into Exta. The bridge must permit Exta's outbound provider connections; `internal: true` would block that traffic. Host administrators remain trusted.

Pin NPM's address on this dedicated network and persist its membership in its existing deployment definition. Inspect existing subnets before allocating anything. The address below is illustrative, not an instruction to reuse an occupied subnet. Exta accepts only explicitly listed private or loopback IPs, not container names, subnet ranges or a forwarding header's claimed proxy address.

Keep the public route as preferred Cloudflare ingress -> Full (strict) HTTPS -> NPM -> private HTTP -> Exta. Cloudflare's edge certificate and NPM's origin certificate continue using their existing management mechanisms. Exta receives no certificate/private-key copy and needs no certificate synchronization or renewal task.

The default initialization still binds loopback only. Explicitly set `exta-data/state/config.json` for the proxy deployment:

```json
{
  "listen": "0.0.0.0:8789",
  "origin": "https://exta.qianfuv.fun",
  "trusted_proxy": {
    "ips": ["172.30.0.2"]
  }
}
```

Keep configuration mode 0600. Proxy mode requires a fixed HTTPS origin, no local TLS files and no development origins. Every request, including health checks, must come from a listed socket peer with exactly one `X-Forwarded-Proto: https` and one valid `X-Real-IP`. Missing, duplicate or malformed values fail closed. Proxy IP changes require an explicit configuration update; they never broaden trust automatically.

NPM forwards to `http://exta:8789` and preserves the external Host, original Origin, cookies, Authorization and CSRF headers. It must overwrite the protocol and client-address headers, not pass user-provided claims. Keep NPM's `trust_forwarded_proto` disabled: its connection from Cloudflare is already TLS. Its real-IP module must trust only the existing Cloudflare source ranges before deriving `$remote_addr` from `CF-Connecting-IP`, with the existing Cloudflare-only origin firewall retained.

The effective NPM proxy location must contain the equivalent of:

```nginx
proxy_pass http://exta:8789;
proxy_set_header Host $host;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header Forwarded "";
proxy_set_header X-Forwarded-Host "";
proxy_set_header X-Forwarded-For "";
proxy_buffering off;
proxy_cache off;
proxy_read_timeout 180s;
```

These are effective-location requirements, not a block to append blindly to NPM's server-level Advanced field: NPM already generates headers inside its locations. Inspect the generated configuration and avoid duplicate `X-Forwarded-Proto`/`X-Real-IP` fields. Never synthesize Origin when it is absent. Retain NPM's HTTP-to-HTTPS redirect and ACME challenge handling.

Exta uses its configured HTTPS origin for Secure/HttpOnly/SameSite session cookies and public REST/MCP URLs. Host/Origin/CSRF and bearer authorization remain enforced. Other forwarding claims are removed before application dispatch. Only the validated client IP from the trusted peer supplies the login rate-limit address; the original socket peer stays available for the trust check. Health probes must go through NPM rather than bypass the peer boundary.

Without `trusted_proxy`, existing direct-listener behavior is unchanged: clear HTTP is loopback-only and a nonloopback listener requires explicit TLS files plus a trusted HTTPS origin. Do not disable those defaults to deploy behind a proxy.

```sh
docker compose run --rm --no-deps exta doctor --data-dir /var/lib/exta/state
docker compose up -d
docker compose stop
```

Stopping gives finalizers time to join. Never run two containers against the same state; the directory lock rejects the second instance. For offline operations, stop the service first, then use `docker compose run --rm --no-deps exta backup ...` with explicitly mounted private output destinations. Do not remove volumes or the bind directory during upgrades. Restore retains its activity/lineage safety checks and has no force rollback bypass.
