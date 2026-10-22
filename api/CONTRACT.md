# Exta contract candidate

This is the T1 implementation of approved planning revision 2. `openapi.yaml` is
the canonical OpenAPI 3.1 document, written in the JSON subset of YAML. Its byte
SHA256 is recorded in `contract-manifest.json`. The controller's S4 acceptance is
still required before T2 backend/frontend writing starts. This file records
resolved details, not a new approval or a claim that the service exists.

## Generation and verification

Run from the project root with the installed Go compiler and Node:

```powershell
$env:GOTOOLCHAIN = 'local'
go run ./tools/contract generate
go run ./tools/contract check
npm --prefix web run typecheck
go test ./internal/domain ./tools/contract
```

`check` validates OpenAPI against the locally retained official 3.1 meta-schema,
compiles every JSON schema and MCP input/output schema without network loading,
checks positive/negative examples, and regenerates artifacts into a temporary
directory for byte comparison. It invokes the pinned local openapi-typescript
package through Node; it does not install dependencies or change generated files.
The ordinary build setup is `npm --prefix web ci --ignore-scripts` and Go module
download with `GOTOOLCHAIN=local`. Tests never require provider credentials.

Generated outputs are `schemas/exta.json`, `schemas/mcp-tools.json`,
`internal/domain/{types.gen.go,schemas.gen.json}` and `web/src/generated/`.
Go tagged unions and arbitrary JSON values use `json.RawMessage` aliases to keep
their wire representation intact; the named variant types are generated from
the same schemas. Validate before decoding. Optional nullable patch fields use
raw JSON to distinguish absent/no change, null/clear and a replacement value.
Generated integer fields use the exact `domain.Integer` decoder, so schema-valid
forms such as `60.0` and `6e1` decode to the same integer without float64 rounding.
Fractional and overflowing values are rejected; large exponents cannot trigger
value-sized allocations. Field-specific schema bounds still apply before decoding.
TypeScript uses openapi-typescript's discriminated types with
`exactOptionalPropertyTypes` and `defaultNonNullable=false`: a default annotation
does not make an optional request field required. The generated client uses
same-origin credentials and accepts a test adapter's `baseUrl` or `fetch`.

The official meta-schema source is
<https://spec.openapis.org/oas/3.1/schema/2025-09-15>, retained at
`tools/contract/testdata/oas31.json`. It is validation data, not executable code.
The generator's source policy and artifact hashes are part of the T1 review.

## Frontend review resolutions

The Opus T1 review identified A1–A22. The following decisions stay within the
approved endpoints and management features:

- **A1–A3, data sources:** `GET /v1/admin/settings` includes local readiness and
  build/schema information; `GET /v1/admin/connection-info` includes capabilities
  and contract version/hash. `GET /v1/admin/usage` includes timestamped queue,
  active-slot and rate/error counters. Overview combines those with bounded
  requests/research lists. No browser Bearer token is required. Research detail
  returns `JobDetail { job, events, events_next_cursor }`; `events_cursor` and
  `events_limit` paginate its event timeline without a new endpoint.
- **A4–A5, session:** login body is `{ credential }`. The session response contains
  stable `principal_id`, closed `permissions`, `expires_at` and a memory-only
  `csrf_token`. Admin mutations require `X-Exta-CSRF` and exact allowed Origin.
  Login instead requires exact Origin plus the local credential, because there
  is no pre-session CSRF token. Cookie is HttpOnly, SameSite=Strict,
  Path=/v1/admin; Secure except explicit loopback HTTP. Missing/expired session
  is `401 authentication_required`; wrong login credentials return `401
  invalid_token`. Bad CSRF is `403 csrf_invalid`, distinct
  from `origin_rejected`. Session rotation never changes principal identity.
- **A6–A7, versions/lists:** body `expected_version` applies consistently to
  versioned mutations; stale input returns `409 revision_conflict`. No If-Match
  or 412 profile is implemented. Lists use `{ items, next_cursor }`, null when
  exhausted, default limit 25/max 100 and stable descending `(created_at,id)`.
  All six operation route policies are seeded at revision 1. They can be
  disabled or replaced, never deleted; `RouteReplace.expected_version` equals
  `RoutePolicy.revision`, including the first update.
- **A8–A9, creation:** retry keys are 8–128 ASCII letters/digits/`._:-`, starting
  alphanumeric; UUIDs fit. Preserve the key for uncertain retries. Budget is
  explicit: hard integer micro-USD for Exa auto (1–100 USD), or estimated Tavily
  credits with `acknowledge_estimated_budget=true` (mini at least 110, pro 250).
  A missing profile remains unresolved until binding/default lookup. The UI's
  initial Exa choice has a USD 1 cap. `output_schema` is a JSON Schema object
  limited to 16 KiB, depth 8 and 128 properties per object, without external
  reference loading. Provider compatibility still requires semantic validation.
- **A10–A12, job state:** the ten local states are closed; upstream status and
  completion reason are open provider strings. Job uses `is_partial`; retrieval
  uses `partial`. `upstream_may_continue` is always present on Job. Cancellation
  capability lists `upstream`, `observation` and nullable unavailable reason.
  Budget holds are `held`, `settled`, `released_unspent`, `written_off`;
  capacity basis is `none`, `proven_non_submission`, `verified_terminal` or
  `manual_override`. A local observation stop does not imply terminal upstream.
- **A13–A15, actions/reports:** cancel returns Job with 200 for local/terminal
  results or 202 for pending remote cancellation. Resume returns 202 Job.
  Reconciliation is a one-action tagged union: attach verified ID, settle
  verified terminal evidence, write off budget, or override capacity. Each has
  expected version, reason and evidence reference; risky actions require their
  own literal acknowledgement. Report has nullable markdown, nullable structured
  output and citations with ID/title/URL/provider. Locked report decryption
  returns `503 secret_store_locked`, never a fabricated empty success.
- **A16–A20, management:** canonical schemas define write-only key inputs,
  masked status/retirement/credential retention counts, versioned route policy,
  no-charge previews, sanitized request attempts, one-time token reveal, scoped
  token ownership, settings and audit. `limit_kind` separates finite, unlimited
  and unknown usage. Cooldown refresh returns 429 with `retry_after_ms`;
  unavailable Exa usage has reason `service_permission_missing`. Settings PATCH
  cannot alter bind/egress/data paths or master-key sources.
  Request summaries expose nullable `result_count`, `failure_count` and `partial`;
  null means unknown, while zero results with `partial=false` means known empty
  success. An oversized upstream response is `502 response_too_large` and retains
  billing uncertainty, rather than manufacturing empty success. Readiness exposes
  `bind_scope`, `nonloopback_safety_configured` and `network_safety_reason` for the
  Connections diagnostics; unsafe nonloopback configuration still rejects startup.
- **A21–A22, tooling/routes:** openapi-typescript 7.13.0 produces TypeScript;
  the Go checker invokes that installed generator. The canonical `x-exta`
  extension owns the `/app/` route allowlist. Admin creation's Location is
  `/v1/admin/research/{id}`. UI route fallback is GET/HEAD HTML navigation only;
  reserved backend namespaces and missing assets keep their actual errors.

The second Opus review's three medium findings are resolved by the optional-field
generator setting, explicit request summaries/error, and seeded route revisions.
Origin enforcement is represented by `x-origin-policy` and the security text,
not a caller-settable OpenAPI parameter: browsers supply this forbidden header
themselves. Every admin mutation, including login, retains exact-origin checks.
T2/T4 browser tests must verify the actual browser header and backend checks;
an injected Node/MSW Origin alone is not security evidence. Research lists show
IDs, profile, state and timestamps; no raw input preview is added to metadata.
Request metadata uses `route_revision=0` to mean no route was applied, such as a
pre-routing rejection; the UI displays this as an unset revision. An actual
executed route always records its positive persisted revision. Local Vite
integration will use an explicit loopback development Origin in backend config;
the proxy must not rewrite Origin to manufacture security-test acceptance.

## Required runtime invariants

Contract validation does not prove the later SQLite/HTTP/worker implementation.
T2–T4 tests must demonstrate these behaviors using local simulated providers:

1. Authenticate/authorize and parse syntax; look up the stable
   `(principal, research_create, idempotency_key)` binding **before** current
   defaults, routing or new admission. Compare replay under its persisted original
   canonicalization/schema/default context. A concurrent insert loser rereads
   the winner's context. Equal original intent returns the original job/current
   state without any new hold or upstream create; changed intent is 409.
2. Resolve missing defaults only after that lookup. The raw ResearchCreate schema
   deliberately permits an omitted profile with either budget shape, so it cannot
   reject an originally Tavily-bound replay by prematurely applying Exa defaults.
   A new request's resolved effective profile/budget must then pass the same
   canonical schema and semantic routing checks. Missing required fields are
   never repaired from the original request.
3. Observation expiry/stop, unknown submission and missing polling evidence keep
   active-slot and budget holds through restart. Verified termination releases
   capacity; only settled cost releases unused budget. Manual write-off spends
   the reservation conservatively. Capacity override is separate, audited and
   retains unknown upstream state; neither action authorizes another create.
4. Admin Research uses `admin:research:create`, cookie auth, Origin and CSRF before
   the shared use case. Tokens owned by the same stable principal share dedupe;
   session/token IDs and transport paths never change ownership scope. Denials
   create zero jobs, holds and upstream calls. No owner override field exists.

`ResearchReplayContext` and `ResearchExecutionSnapshot` are internal persistence
schemas, with no public endpoint exposure. The former freezes versioned schema,
canonicalization and semantic configuration plus the two optional defaults;
the latter records the original route, provider, account, credential reference
and reservation provenance. The replay digest includes every submitted intent
field after filling only the two declared defaults. It never includes current
route or session/token provenance. Preserve text and array order; compare JSON
objects independently of key order. Missing or unreadable original context is
`storage_unavailable`, not permission to recreate a job. T4 owns implementation
and persisted restart/concurrent-loser regressions for these requirements.

## Ownership and installed tooling

Astra owns Go, schemas/generation, integration tests and task/Git records. Opus
owns frontend implementation under `web/**` excluding `src/generated/**`, and
`docs/ui.md`, only after S4. Package/lock/config ownership transfers together;
later changes coordinate with the integrator. No concurrent shared-file writes.

The resumed compiler is existing Go 1.26.10 (`D:\Go\bin\go.exe`), with
`GOTOOLCHAIN=local`. Node 24.22.1/npm 12.2.0 remain installed. Dependencies are
exactly locked in go.mod/go.sum/package-lock.json. Go MCP SDK v1.8.0 supports the
approved 2026-07-28 and 2025-11-25 profiles; modern transport will explicitly use
Stateless and PropagateRequestCancellation, restricted protocol versions and
separate modern/legacy negotiation tests. SQLite v1.61.0 is the CGO-free choice;
its libc dependency is not independently replaced.

Playwright 1.63.0 matches already installed Chromium/headless-shell 1243 and
FFmpeg 1011. Browser installation/downloads are disabled. ESLint 10 replaces the
unsupported 9.x suggestion; the incompatible JSX-a11y plugin is omitted, while
axe/keyboard/mobile accessibility acceptance remains required. Docker, real
credentials, provider charges, deployments and remote Git operations are unused.

T1 verification covers the contract and boundary behavior only. Stateful replay,
retained-resource accounting, admin authentication flows, all eight real UI pages,
five-target packaging and native-platform release evidence remain later tasks.
