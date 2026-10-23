# Changelog

## 0.1.3 — 2026-10-24

- Treat missing, null and zero key safety caps as no local cap; positive limits, shared reservations, upstream billing blocks and rate backoff remain enforced. Unknown upstream balance permits otherwise eligible calls and remains unknown. Existing zero caps now have this explicit unrestricted meaning.
- Initialize new installations with an explicit reusable administrator password from standard input, stored using salted PBKDF2-HMAC-SHA256. Previously issued administrator credentials were already reusable and remain valid without reset or automatic rehashing. Keep API/MCP access keys separate. No production credential is changed by this release.
- Correct Semi calendar selection for access-key expiry, preserve controlled clear/reselection, and recheck expiry at submission. Add UTC, Asia/Shanghai and DST-zone browser round trips against isolated backend state.
- Pair settings, route and key numeric controls with common slider marks and exact inputs; preserve legal values beyond displayed slider presets and label zero-limit semantics. Align route targets and their move/remove actions.
- Consolidate recent calls and errors on the left of Statistics with key-pool/readiness panels on the right. Add UTC call-count trends over 24 hours, 7 days or 30 days, counting each retained synchronous request or Research job once, never retries, fallback attempts or polling. Outcomes reflect current state in the original creation bucket; pre-admission rejections and deleted history are outside coverage.
- Remove the independent Audit interface while preserving internal transactional evidence and existing history. Add exact request-ID filtering and provider filtering across failed fallback attempts without duplicate call rows.
- Unify login and application language/appearance icon menus, remove redundant breadcrumbs and subtitles, and move selected explanations into accessible heading help. Apply restrained graphite/indigo light, dark and system themes. Keep units, errors, critical states and destructive confirmations visible.
- Keep the initial database schema unchanged from 0.1.2. New password-verifier state requires this or a newer binary for login; old credentials are not rewritten. No automatic production deployment or password reset is performed.

## 0.1.2 — 2026-10-24

- Add explicit trusted reverse-proxy support for HTTP inside an isolated Docker network, with a fixed external HTTPS origin. Public TLS remains at the existing reverse proxy.
- Require exact trusted socket-peer addresses and unambiguous proxy-supplied HTTPS/client-IP headers. Preserve Secure cookies, Host/Origin/CSRF checks and per-client login rate limits; direct-listener safety defaults remain unchanged.
- Publish no host port in the supplied Compose configuration. Document NPM/Cloudflare termination without backend certificate copies or renewal synchronization.
- Add HTTPS-to-HTTP protocol regressions and isolated HTTP container acceptance alongside the existing Windows/Linux native, browser and Linux race gates. No database schema change is introduced by this patch.

## 0.1.1 — 2026-10-23

- Make upstream keys the direct management unit across management contracts, scheduling, local safety limits, usage and status. Remove the account-management layer. Unknown shared ownership remains explicitly unknown; unverified balances are never summed into independent available capacity.
- Separate service access credentials (clients to Exta) from Exa/Tavily upstream keys. Keep search and Research execution in REST/MCP, outside the management interface.
- Adopt official Semi Design controls, consistent typography, compact preferences, persistent light/dark/system appearance, responsive management tables and accessible dialogs/drawers.
- Complete three independent Opus 5.5 high visual review/correction cycles for the key-only console, followed by Astra fixes and production-browser verification.
- Preserve request idempotency, shared-limit enforcement, unknown-cost reservations, credential secrecy, CSRF and strict CSP protections.

### Development database compatibility

The database design is still evolving. The initial schema/checksum changed from 0.1.0; this release does **not** provide or claim a lossless in-place upgrade of an existing 0.1.0 database. Older state is rejected on schema/checksum mismatch. Keep existing data intact and use a separate new data directory for a fresh 0.1.1 instance. Do not delete or overwrite an old database to make startup succeed. No account-to-key migration or rollback facility is introduced for this redesign.

### Distribution and verification

Only Windows amd64 and Linux amd64 standalone programs and a Linux amd64 container are supported. Release publication is gated by native checks on both platforms, Linux race detection, production-browser checks and container acceptance. These synthetic/offline checks do not establish actual provider balances or live paid API behavior. No application license is assigned; bundled dependency notices remain included.
