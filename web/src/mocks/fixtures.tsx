/**
 * Development/test-only fixtures typed by the frozen contract. Never imported by
 * production code paths; all identifiers and values are synthetic.
 */
import type { Schemas } from "../lib/api";
import { CONTRACT_SHA256 } from "../generated/client.gen";

type Job = Schemas["Job"];

/** Deterministic counts are synthetic development evidence, never production traffic. */
export function mockCallTrends(hours = 24): Schemas["CallTrends"] {
  const width = hours === 720 ? 86400 : 3600;
  const end = Date.parse("2026-10-24T12:00:00Z");
  const count = (hours * 3600) / width;
  const start = end - count * width * 1000;
  return {
    observed_at: new Date(end).toISOString(),
    from: new Date(start).toISOString(),
    to: new Date(end).toISOString(),
    bucket_seconds: width,
    coverage: "retained_accepted_calls",
    buckets: Array.from({ length: count }, (_, index) => ({
      start: new Date(start + index * width * 1000).toISOString(),
      total: index === count - 2 ? 3 : 0,
      succeeded: index === count - 2 ? 2 : 0,
      failed: index === count - 2 ? 1 : 0,
      pending: 0,
      uncertain: 0,
      cancelled: 0,
    })),
  };
}

/** All admin permissions, for a fully privileged mock principal. */
export const ALL_PERMISSIONS: Schemas["AdminPermission"][] = [
  "admin:read",
  "admin:keys:write",
  "admin:routes:write",
  "admin:usage:refresh",
  "admin:research:create",
  "admin:research:cancel",
  "admin:research:observe",
  "admin:research:reconcile",
  "admin:tokens:write",
  "admin:settings:write",
  "admin:audit:read",
];

const T0 = "2026-10-22T08:00:00Z";
const T1 = "2026-10-22T08:05:00Z";
const T2 = "2026-10-22T09:05:00Z";

/** Synthetic session for the mock principal. */
export function mockSession(
  permissions: Schemas["AdminPermission"][] = ALL_PERMISSIONS,
): Schemas["Session"] {
  return {
    principal_id: "principal_operator",
    permissions,
    expires_at: "2026-10-22T20:00:00Z",
    csrf_token: "mock-csrf-token-000000000000",
  };
}

/** Synthetic readiness; healthy only because it is a fixture. */
export function mockReadiness(overrides: Partial<Schemas["Readiness"]> = {}): Schemas["Readiness"] {
  return {
    ready: true,
    database: "ready",
    secret_store: "unlocked",
    assets: "ready",
    bind_scope: "loopback",
    nonloopback_safety_configured: false,
    network_safety_reason: null,
    providers: [
      { provider: "exa", enabled: true, reason: null },
      { provider: "tavily", enabled: true, reason: null },
    ],
    ...overrides,
  };
}

/** Synthetic independent keys; no provider identity is inferred from their balances. */
export function mockKeys(): Schemas["UpstreamKey"][] {
  const makeKey = (
    id: string,
    provider: Schemas["Provider"],
    label: string,
  ): Schemas["UpstreamKey"] => ({
    id,
    provider,
    label,
    association_ref: id,
    association_evidence: "unknown",
    quota_policy: {
      unit: provider === "exa" ? "micro_usd" : "tavily_credit",
      allocation: provider === "exa" ? 50_000_000 : 1000,
      per_operation_reservation: provider === "exa" ? 1_000_000 : 250,
      active_jobs: 1,
    },
    billing_state: "eligible",
    breaker: "closed",
    cooldown_until: null,
    created_at: T0,
    enabled: true,
    retained_credential_count: 0,
    retirement_state: "active",
    validity: "valid",
    version: 1,
    last_four: "test",
    fingerprint: `fixture-${id}`,
  });
  return [
    makeKey("key_exa_1", "exa", "Exa primary"),
    {
      ...makeKey("key_exa_2", "exa", "Exa standby"),
      validity: "invalid",
      breaker: "open",
      cooldown_until: T2,
    },
    {
      ...makeKey("key_tavily_1", "tavily", "Tavily shared"),
      billing_state: "blocked",
      retained_credential_count: 1,
    },
    { ...makeKey("key_tavily_unverified", "tavily", "Tavily new"), validity: "unknown" },
  ];
}

/** Six seeded route policies at revision 1. */
export function mockRoutes(): Schemas["RoutePolicy"][] {
  const policy = (
    capability: Schemas["Operation"],
    targets: Schemas["RouteTarget"][],
    overrides: Partial<Schemas["RoutePolicy"]> = {},
  ): Schemas["RoutePolicy"] => ({
    capability,
    targets,
    enabled: true,
    allow_duplicate_cost: false,
    max_attempts: 2,
    deadline_seconds: capability === "search" || capability === "research_create" ? 30 : 60,
    revision: 1,
    created_at: T0,
    ...overrides,
  });
  return [
    policy("search", [
      { provider: "exa", profile: "common" },
      { provider: "tavily", profile: "common" },
    ]),
    policy("advanced_search", [{ provider: "exa", profile: "common" }], { revision: 2 }),
    policy("fetch", [{ provider: "tavily", profile: "common" }]),
    policy("map", [{ provider: "tavily", profile: "common" }]),
    policy("crawl", [], { enabled: true }),
    policy("research_create", [{ provider: "exa", profile: "exa_auto" }], {
      max_attempts: 1,
      deadline_seconds: 30,
    }),
  ];
}

/**
 * Build a job with safe defaults.
 *
 * @param overrides - Fields to change.
 * @returns Job.
 */
export function mockJob(overrides: Partial<Job> = {}): Job {
  return {
    id: "job_running",
    profile: "exa_auto",
    source_provider: "exa",
    state: "running",
    upstream_state: "running",
    upstream_job_id: "up_exa_001",
    upstream_may_continue: true,
    active_slot_held: true,
    budget: { mode: "hard", unit: "micro_usd", amount: 1_000_000 },
    budget_hold_state: "held",
    capacity_release_basis: "none",
    cancel_capability: { upstream: true, observation: true, unavailable_reason: null },
    completion_reason: null,
    error: null,
    idempotency_expires_at: "2027-01-20T08:00:00Z",
    is_partial: false,
    observation_deadline: T2,
    owner_principal_id: "principal_operator",
    polling_count: 4,
    reservation: {
      unit: "micro_usd",
      amount: 1_000_000,
      certainty: "estimated",
      source: "local_reservation",
      observed_at: T0,
      pricing_revision: "exa-2026-09",
    },
    result: null,
    route_revision: 1,
    uncertainty: false,
    created_at: T0,
    updated_at: T1,
    version: 2,
    ...overrides,
  };
}

/** Markdown with hostile HTML, an image and a script link, for safe rendering checks. */
export const HOSTILE_MARKDOWN = [
  "# Findings",
  "",
  "Safe paragraph with a [source](https://example.com/report).",
  "",
  '<script>window.__extaPwned = true</script><img src="https://tracker.example/pixel.png" onerror="alert(1)">',
  "",
  "![remote chart](https://images.example/chart.png)",
  "",
  "[click me](javascript:alert(1))",
].join("\n");

/** Synthetic job detail records keyed by job ID. */
export function mockJobDetails(): Map<string, Schemas["JobDetail"]> {
  const tavilyReservation = {
    unit: "tavily_credit" as const,
    amount: 110,
    certainty: "estimated" as const,
    source: "local_reservation",
    observed_at: T0,
    pricing_revision: null,
  };
  const jobs: Job[] = [
    mockJob(),
    mockJob({
      id: "job_unknown",
      state: "submission_unknown",
      upstream_state: null,
      upstream_job_id: null,
      uncertainty: true,
      cancel_capability: {
        upstream: false,
        observation: false,
        unavailable_reason: "upstream ID unknown",
      },
      version: 5,
    }),
    mockJob({
      id: "job_expired",
      state: "observation_expired",
      upstream_state: "running",
      upstream_may_continue: true,
      cancel_capability: { upstream: true, observation: false, unavailable_reason: null },
      version: 7,
    }),
    mockJob({
      id: "job_done",
      state: "succeeded",
      upstream_state: "completed",
      upstream_may_continue: false,
      active_slot_held: false,
      budget_hold_state: "settled",
      capacity_release_basis: "verified_terminal",
      completion_reason: "completed",
      result: {
        markdown: HOSTILE_MARKDOWN,
        structured_output: { summary: "fixture" },
        citations: [
          {
            id: "c1",
            title: "Example source",
            url: "https://example.com/report",
            source_provider: "exa",
          },
          { id: "c2", title: "Bad scheme", url: "javascript:alert(1)", source_provider: "exa" },
        ],
      },
      cancel_capability: {
        upstream: false,
        observation: false,
        unavailable_reason: "job is terminal",
      },
      version: 9,
    }),
    mockJob({
      id: "job_partial",
      profile: "tavily_mini",
      source_provider: "tavily",
      state: "succeeded",
      is_partial: true,
      completion_reason: "budget_exhausted",
      upstream_state: "completed",
      upstream_may_continue: false,
      active_slot_held: false,
      budget: {
        mode: "estimated",
        unit: "tavily_credit",
        amount: 110,
        acknowledge_estimated_budget: true,
      },
      reservation: tavilyReservation,
      budget_hold_state: "settled",
      capacity_release_basis: "verified_terminal",
      result: { markdown: "Partial **report**.", structured_output: null, citations: [] },
      cancel_capability: {
        upstream: false,
        observation: false,
        unavailable_reason: "job is terminal",
      },
      version: 4,
    }),
    mockJob({
      id: "job_failed",
      profile: "tavily_pro",
      source_provider: "tavily",
      state: "failed",
      upstream_state: "failed",
      upstream_may_continue: false,
      active_slot_held: false,
      budget: {
        mode: "estimated",
        unit: "tavily_credit",
        amount: 250,
        acknowledge_estimated_budget: true,
      },
      reservation: { ...tavilyReservation, amount: 250 },
      budget_hold_state: "held",
      capacity_release_basis: "verified_terminal",
      error: {
        code: "upstream_failure",
        message: "Provider reported failure in a 200 response.",
        request_id: "req_fixture_failed",
        retryable: false,
        retry_after_ms: null,
        details: {},
      },
      cancel_capability: {
        upstream: false,
        observation: false,
        unavailable_reason: "job is terminal",
      },
      version: 6,
    }),
  ];
  return new Map(
    jobs.map((job) => [
      job.id,
      {
        job,
        events: [
          {
            id: `${job.id}_e2`,
            created_at: T1,
            from_state: "accepted",
            to_state: job.state,
            reason: "fixture transition",
            actor_principal_id: null,
            action_ref: null,
          },
          {
            id: `${job.id}_e1`,
            created_at: T0,
            from_state: null,
            to_state: "accepted",
            reason: "accepted by fixture",
            actor_principal_id: "principal_operator",
            action_ref: null,
          },
        ],
        events_next_cursor: null,
      },
    ]),
  );
}

/** Synthetic request records with attempts. */
export function mockRequests(): Schemas["RequestDetail"][] {
  const record = (overrides: Partial<Schemas["RequestRecord"]>): Schemas["RequestRecord"] => ({
    id: "req_ok",
    operation: "search",
    status: "succeeded",
    source_provider: "exa",
    route_revision: 1,
    billing_uncertain: false,
    created_at: T1,
    completed_at: T1,
    duration_ms: 840,
    error: null,
    result_count: 5,
    failure_count: 0,
    partial: false,
    usage: [
      {
        unit: "micro_usd",
        amount: 5000,
        certainty: "reported",
        source: "exa_cost_dollars",
        observed_at: T1,
        pricing_revision: null,
      },
    ],
    ...overrides,
  });
  const attempt = (overrides: Partial<Schemas["Attempt"]>): Schemas["Attempt"] => ({
    id: "att_1",
    attempt_number: 1,
    provider: "exa",
    key_id: "key_exa_1",
    credential_version_id: "cred_v1",
    transmission_state: "response_received",
    http_status: 200,
    retry_reason: null,
    duration_ms: 800,
    billing_uncertain: false,
    upstream_request_id: "up_req_1",
    created_at: T1,
    ...overrides,
  });
  return [
    { request: record({}), attempts: [attempt({})] },
    {
      request: record({ id: "req_empty", result_count: 0, failure_count: 0 }),
      attempts: [attempt({ id: "att_e" })],
    },
    {
      request: record({
        id: "req_partial",
        operation: "fetch",
        source_provider: "tavily",
        partial: true,
        result_count: 3,
        failure_count: 2,
        usage: [],
      }),
      attempts: [attempt({ id: "att_p", provider: "tavily", key_id: "key_tavily_1" })],
    },
    {
      request: record({
        id: "req_unknown",
        status: "uncertain",
        billing_uncertain: true,
        result_count: null,
        failure_count: null,
        partial: null,
        completed_at: null,
        usage: [],
      }),
      attempts: [
        attempt({
          id: "att_u1",
          transmission_state: "possibly_sent",
          http_status: null,
          billing_uncertain: true,
          retry_reason: "timeout",
          upstream_request_id: null,
        }),
      ],
    },
    {
      request: record({
        id: "req_too_large",
        status: "failed",
        billing_uncertain: true,
        result_count: null,
        failure_count: null,
        partial: null,
        error: {
          code: "response_too_large",
          message: "Upstream response exceeded the configured limit.",
          request_id: "req_too_large",
          retryable: false,
          retry_after_ms: null,
          details: {},
        },
      }),
      attempts: [attempt({ id: "att_l", billing_uncertain: true })],
    },
    {
      request: record({
        id: "req_rejected",
        status: "failed",
        source_provider: null,
        route_revision: 0,
        result_count: null,
        failure_count: null,
        partial: null,
        duration_ms: 2,
        usage: [],
        error: {
          code: "unsupported_capability",
          message: "No compatible route.",
          request_id: "req_rejected",
          retryable: false,
          retry_after_ms: null,
          details: {},
        },
      }),
      attempts: [],
    },
  ];
}

/** Synthetic usage view with separated units and unknown/unlimited limits. */
export function mockUsage(): Schemas["KeyUsageView"] {
  const row = (overrides: Partial<Schemas["KeyUsage"]>): Schemas["KeyUsage"] => ({
    association_ref: "key_exa_1",
    association_evidence: "unknown",
    scope: "key",
    key_id: "key_exa_1",
    provider: "exa",
    unit: "micro_usd",
    source: "upstream",
    freshness: "fresh",
    observed_at: T1,
    period_start: "2026-10-01T00:00:00Z",
    period_end: "2026-11-01T00:00:00Z",
    limit_kind: "finite",
    limit: 50_000_000,
    used: 12_500_000,
    remaining: 37_500_000,
    reserved: 1_000_000,
    unavailable_reason: null,
    ...overrides,
  });
  return {
    overview: {
      active_slots_held: 2,
      errors_last_minute: 1,
      observed_at: T1,
      queued_jobs: 0,
      requests_last_minute: 7,
      uncertain_jobs: 1,
      waiting_requests: 0,
    },
    items: [
      row({}),
      row({
        source: "local",
        used: 12_900_000,
        remaining: null,
        limit_kind: "unknown",
        limit: null,
      }),
      row({
        key_id: "key_exa_2",
        source: "upstream",
        freshness: "unknown",
        observed_at: null,
        used: null,
        limit: null,
        remaining: null,
        limit_kind: "unknown",
        reserved: 0,
        unavailable_reason: "service_permission_missing",
      }),
      row({
        association_ref: "key_tavily_1",
        key_id: "key_tavily_1",
        provider: "tavily",
        unit: "tavily_credit",
        source: "upstream",
        freshness: "stale",
        limit_kind: "unlimited",
        limit: null,
        remaining: null,
        used: 640,
        reserved: 250,
      }),
    ],
    next_cursor: null,
  };
}

/** Synthetic connection info with placeholder examples only. */
export function mockConnectionInfo(): Schemas["ConnectionInfo"] {
  const cancellation = { upstream: true, observation: true, unavailable_reason: null };
  const noCancel = { upstream: false, observation: false, unavailable_reason: "synchronous" };
  return {
    authentication: "preconfigured_bearer",
    oauth_supported: false,
    rest_base_url: "http://127.0.0.1:8789/v1",
    mcp_url: "http://127.0.0.1:8789/mcp",
    protocol_versions: ["2026-07-28", "2025-11-25"],
    readiness: mockReadiness(),
    examples: [
      {
        title: "REST search",
        command:
          'curl -H "Authorization: Bearer <EXTA_TOKEN>" -H "Content-Type: application/json" -d \'{"query":"example"}\' http://127.0.0.1:8789/v1/search',
      },
    ],
    capabilities: {
      contract_hash: CONTRACT_SHA256,
      contract_version: "1",
      capabilities: (["search", "advanced_search", "fetch", "map", "crawl"] as const)
        .map((operation): Schemas["Capability"] => ({
          operation,
          enabled: true,
          unavailable_reason: null,
          constraints: {},
          default_limits: {},
          hard_limits: {},
          profiles: [
            ...(operation === "map" || operation === "crawl"
              ? []
              : [
                  {
                    provider: "exa" as const,
                    profile: "common" as const,
                    enabled: true,
                    unavailable_reason: null,
                    budget_modes: [],
                    cancellation: noCancel,
                  },
                ]),
            {
              provider: "tavily" as const,
              profile: "common" as const,
              enabled: true,
              unavailable_reason: null,
              budget_modes: [],
              cancellation: noCancel,
            },
            ...(operation === "advanced_search"
              ? [
                  {
                    provider: "exa" as const,
                    profile: "exa_deep" as const,
                    enabled: true,
                    unavailable_reason: null,
                    budget_modes: [],
                    cancellation: noCancel,
                  },
                  {
                    provider: "tavily" as const,
                    profile: "tavily_advanced" as const,
                    enabled: false,
                    unavailable_reason: "disabled by operator",
                    budget_modes: [],
                    cancellation: noCancel,
                  },
                ]
              : []),
          ],
        }))
        .concat([
          {
            operation: "research_create",
            enabled: true,
            unavailable_reason: null,
            constraints: {},
            default_limits: {},
            hard_limits: {},
            profiles: [
              {
                provider: "exa",
                profile: "exa_auto",
                enabled: true,
                unavailable_reason: null,
                budget_modes: ["hard"],
                cancellation,
              },
              {
                provider: "tavily",
                profile: "tavily_mini",
                enabled: true,
                unavailable_reason: null,
                budget_modes: ["estimated"],
                cancellation: {
                  ...cancellation,
                  upstream: false,
                  unavailable_reason: "provider has no cancel API",
                },
              },
              {
                provider: "tavily",
                profile: "tavily_pro",
                enabled: true,
                unavailable_reason: null,
                budget_modes: ["estimated"],
                cancellation: {
                  ...cancellation,
                  upstream: false,
                  unavailable_reason: "provider has no cancel API",
                },
              },
            ],
          },
        ]),
    },
  };
}

/** Synthetic settings. */
export function mockSettings(): Schemas["Settings"] {
  return {
    version: 4,
    build: {
      version: "0.0.0-fixture",
      target: "fixture",
      schema_version: 1,
      contract_hash: CONTRACT_SHA256,
    },
    readiness: mockReadiness(),
    limits: {
      synchronous_concurrency: 4,
      synchronous_waiting: 16,
      principal_requests_per_minute: 30,
      research_queued: 10,
      research_active: 1,
      principal_research_active: 1,
      request_bytes: 65536,
      response_bytes: 4194304,
      observation_seconds: 3600,
      max_polls: 60,
    },
    retention: {
      request_days: 14,
      research_content_days: 14,
      audit_days: 90,
      idempotency_days: 90,
    },
    protected_fields: ["bind_address", "egress_policy", "data_dir", "master_key_source"],
  };
}

/** Synthetic client tokens. */
export function mockTokens(): Schemas["ClientToken"][] {
  return [
    {
      id: "tok_cli",
      label: "Local CLI",
      principal_id: "principal_operator",
      scopes: ["read", "execute"],
      created_at: T0,
      expires_at: "2027-10-22T00:00:00Z",
      revoked_at: null,
      version: 1,
    },
  ];
}
