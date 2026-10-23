# Changelog

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
