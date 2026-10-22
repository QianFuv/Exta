# Durable Research behavior

REST and MCP use the same persisted jobs, stable principal ownership, admission,
idempotency and accounting. Client scopes are `research:create`, `research:read`,
`research:cancel` and `research:observe`. Administrator mutations use their
separate cookie permissions, exact Origin and CSRF checks.

Creation returns a locally accepted job before any provider call. The job,
idempotency binding, frozen defaults, credential version and financial/capacity
reservation commit together. Replaying the same intent and key returns the
original job even after configuration changes. A different intent returns 409.
The acceptance transaction has a 30-second deadline.

The worker records submission before dispatch. A crash or possibly transmitted
create becomes `submission_unknown`; it never causes a blind create retry.
Only positively unsubmitted failures can retry within the frozen policy, at most
twice. Known tasks always use their original credential version after rotation.
GET/list handlers read local state and do not poll providers.

Stopping observation, exhausting its window, a lookup 404, invalid credentials,
or an unacknowledged cancellation retains money and active-slot holds. Verified
termination releases capacity; an unknown terminal cost still retains money.
An explicit audited write-off consumes the full reserved amount without claiming
a refund or upstream cancellation. Capacity override is a separate acknowledged
action. Resuming observation reuses the original holds.

Exa completion with `budget_reached` is partial success. A cancellation racing
with completion preserves the actual terminal result and reported cost. A lost
cancel response is followed by observation, not an automatic repeated cancel.

## Upstream identifiers

The approved compatibility correction widens only `Job.upstream_job_id` and
`AttachUpstreamId.upstream_job_id` to 1–200 characters matching
`^[A-Za-z0-9_.:-]+$`, the official Exa `AgentRunId` schema. Local Exta IDs retain
their original restrictions. Provider IDs remain intact and are encoded as one
URL path segment, including dot-only segments. Positive/negative examples and
backend tests cover punctuation, 200/201-character boundaries and path separators.
The contract still contains 98 schemas and 46 operations.

Source: [Exa API schema](https://exa.ai/docs/exa-spec.json),
`components.schemas.AgentRunId`, inspected 2026-10-22. Saved source SHA-256:
`cffbc4a535afecdb014ca3ecc994dd4734acbc0c4ed0e9cdff58516c5d198753`.

## Tavily unknown-ID recovery limitation

Tavily creation documents `request_id`, `created_at`, `input` and `model` in its
response. A trusted capture of that original authenticated attempt could supply
correlation evidence, but the current reconciliation request contains no such
verifiable capture. Subsequent lookup does not document echoed input/model/output
schema, arbitrary metadata or a stable billing-account identity. Logs omit
input/output. A masked key suffix, nearby timestamp, successful lookup or an
administrator's text assertion cannot prove the original intent and account.

Consequently, manual attachment of an unknown Tavily ID is rejected with 409.
`evidence_ref` is recorded as text, never fetched as an arbitrary URL. This is an
accepted design limitation, not a claim of recovered identity. Known Tavily IDs
continue polling with their pinned credential; failed HTTP-200 results are stored
as failures. Tavily does not offer the documented upstream-cancel capability;
stopping local observation leaves upstream work potentially active.

Source: [Tavily API schema](https://docs.tavily.com/documentation/api-reference/openapi.json),
inspected 2026-10-22. Saved source SHA-256:
`4958dc0b567ec857892acd088d9a664bb2b35ea501dccc656401c0049986a03a`.

All implementation verification uses synthetic local providers. No live-provider
or frontend acceptance is implied.
