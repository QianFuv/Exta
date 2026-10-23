/**
 * Development/test-only contract mocks. The handlers model session, CSRF, permission,
 * version and idempotency checks so UI tests can assert side effects. They are not
 * security evidence: browser Origin enforcement belongs to backend/browser tests.
 */
import { HttpResponse, http } from "msw";
import type { HttpResponseResolver } from "msw";
import type { Schemas } from "../lib/api";
import { canonicalJson } from "../lib/researchDraft";
import {
  mockConnectionInfo,
  mockJob,
  mockJobDetails,
  mockKeys,
  mockRequests,
  mockRoutes,
  mockSession,
  mockSettings,
  mockTokens,
  mockUsage,
  mockCallTrends,
} from "./fixtures";

/** A recorded request for test assertions. */
export interface RecordedCall {
  method: string;
  path: string;
  headers: Record<string, string>;
  body: unknown;
}

/** Mutable mock backend state. */
export interface MockState {
  session: Schemas["Session"] | null;
  validCredential: string;
  keys: Schemas["UpstreamKey"][];
  routes: Schemas["RoutePolicy"][];
  jobs: Map<string, Schemas["JobDetail"]>;
  requests: Schemas["RequestDetail"][];
  usage: Schemas["KeyUsageView"];
  connectionInfo: Schemas["ConnectionInfo"];
  settings: Schemas["Settings"];
  tokens: Schemas["ClientToken"][];
  idempotency: Map<string, { fingerprint: string; jobId: string }>;
  calls: RecordedCall[];
  nextId: number;
}

/**
 * Create a fresh mock state.
 *
 * @param options - Whether a session already exists.
 * @returns State object.
 */
export function createMockState(options: { signedIn?: boolean } = {}): MockState {
  return {
    session: options.signedIn === false ? null : mockSession(),
    validCredential: "fixture-operator-credential",
    keys: mockKeys(),
    routes: mockRoutes(),
    jobs: mockJobDetails(),
    requests: mockRequests(),
    usage: mockUsage(),
    connectionInfo: mockConnectionInfo(),
    settings: mockSettings(),
    tokens: mockTokens(),
    idempotency: new Map(),
    calls: [],
    nextId: 1,
  };
}

/** Shared state mutated by the handlers; tests replace it per case. */
export const mockDb: { state: MockState } = { state: createMockState() };

/**
 * Build an Exta error response.
 *
 * @param status - HTTP status.
 * @param code - Error code.
 * @param message - Safe message.
 * @param retryAfterMs - Optional retry hint.
 * @returns JSON response.
 */
export function apiError(
  status: number,
  code: Schemas["ErrorCode"],
  message: string,
  retryAfterMs: number | null = null,
) {
  return HttpResponse.json(
    {
      error: {
        code,
        message,
        request_id: `req_mock_${code}`,
        retryable: retryAfterMs !== null,
        retry_after_ms: retryAfterMs,
        details: {},
      },
    },
    { status },
  );
}

/**
 * Parse a recorded request body.
 *
 * @param text - Raw body text.
 * @returns Parsed JSON, raw text when not JSON, or null when empty.
 */
function parseBody(text: string): unknown {
  if (text === "") {
    return null;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return text;
  }
}

/**
 * Wrap a resolver with call recording and session/CSRF/permission enforcement.
 *
 * @param permission - Required permission, or null for any authenticated session.
 * @param isMutation - Whether the CSRF header is required.
 * @param resolver - Inner resolver.
 * @returns Guarded resolver.
 */
function guarded(
  permission: Schemas["AdminPermission"] | null,
  isMutation: boolean,
  resolver: (info: {
    request: Request;
    params: Record<string, string | readonly string[] | undefined>;
    body: unknown;
  }) => Response | Promise<Response>,
): HttpResponseResolver {
  return async ({ request, params }) => {
    const state = mockDb.state;
    const text =
      request.method === "GET" || request.method === "HEAD" ? "" : await request.clone().text();
    const body = parseBody(text);
    state.calls.push({
      method: request.method,
      path: new URL(request.url).pathname,
      headers: Object.fromEntries(request.headers.entries()),
      body,
    });
    if (state.session === null) {
      return apiError(401, "authentication_required", "Sign in required.");
    }
    if (isMutation && request.headers.get("X-Exta-CSRF") !== state.session.csrf_token) {
      return apiError(403, "csrf_invalid", "CSRF token rejected.");
    }
    if (permission !== null && !state.session.permissions.includes(permission)) {
      return apiError(403, "forbidden", "Permission denied.");
    }
    return resolver({ request, params, body });
  };
}

/**
 * Paginate a list with numeric cursors.
 *
 * @param items - Full list.
 * @param request - Request carrying cursor/limit.
 * @returns Page.
 */
function page<T>(items: T[], request: Request): { items: T[]; next_cursor: string | null } {
  const url = new URL(request.url);
  const start = Number(url.searchParams.get("cursor") ?? "0");
  const limit = Number(url.searchParams.get("limit") ?? "25");
  const slice = items.slice(start, start + limit);
  return { items: slice, next_cursor: start + limit < items.length ? String(start + limit) : null };
}

/**
 * Read a path parameter as a string.
 *
 * @param params - Route params.
 * @param name - Parameter name.
 * @returns Value.
 */
function param(
  params: Record<string, string | readonly string[] | undefined>,
  name: string,
): string {
  const value = params[name];
  return typeof value === "string" ? value : "";
}

const NOW = "2026-10-22T10:00:00Z";

/**
 * Admit a new research job and bind it to a retry key, as the backend's shared create
 * core would. Exposed so tests can model "accepted, then the response was lost".
 *
 * @param key - Idempotency key.
 * @param input - Research request body.
 * @returns The new job.
 */
export function admitResearch(key: string, input: Schemas["ResearchCreate"]): Schemas["Job"] {
  const id = `job_new_${mockDb.state.nextId++}`;
  const profile = input.profile ?? "exa_auto";
  const job = mockJob({
    id,
    profile,
    source_provider: profile === "exa_auto" ? "exa" : "tavily",
    state: "queued",
    upstream_state: null,
    upstream_job_id: null,
    upstream_may_continue: false,
    budget: input.budget,
    reservation: {
      unit: input.budget.unit,
      amount: input.budget.amount,
      certainty: "estimated",
      source: "local_reservation",
      observed_at: NOW,
      pricing_revision: null,
    },
    created_at: NOW,
    updated_at: NOW,
    version: 1,
  });
  mockDb.state.jobs.set(id, { job, events: [], events_next_cursor: null });
  mockDb.state.idempotency.set(key, { fingerprint: canonicalJson(input), jobId: id });
  return job;
}

/** All admin API handlers. */
export const handlers = [
  http.get("*/v1/admin/session", ({ request }) => {
    mockDb.state.calls.push({
      method: "GET",
      path: new URL(request.url).pathname,
      headers: {},
      body: null,
    });
    return mockDb.state.session === null
      ? apiError(401, "authentication_required", "Sign in required.")
      : HttpResponse.json(mockDb.state.session);
  }),
  http.post("*/v1/admin/session", async ({ request }) => {
    const body = (await request.json()) as Schemas["SessionCreate"];
    mockDb.state.calls.push({
      method: "POST",
      path: "/v1/admin/session",
      headers: {},
      body: { credential: "[redacted]" },
    });
    if (body.credential !== mockDb.state.validCredential) {
      return apiError(401, "invalid_token", "Credential rejected.");
    }
    mockDb.state.session = mockSession();
    return HttpResponse.json(mockDb.state.session);
  }),
  http.delete(
    "*/v1/admin/session",
    guarded(null, true, () => {
      mockDb.state.session = null;
      return HttpResponse.json({ ok: true });
    }),
  ),
  http.get(
    "*/v1/admin/keys",
    guarded("admin:read", false, ({ request }) => {
      const provider = new URL(request.url).searchParams.get("provider");
      return HttpResponse.json(
        page(
          mockDb.state.keys.filter((key) => !provider || key.provider === provider),
          request,
        ),
      );
    }),
  ),
  http.post(
    "*/v1/admin/keys",
    guarded("admin:keys:write", true, ({ body }) => {
      const input = body as Schemas["UpstreamKeyCreate"];
      const id = `key_new_${mockDb.state.nextId++}`;
      const key: Schemas["UpstreamKey"] = {
        id,
        provider: input.provider,
        association_ref: id,
        association_evidence: "unknown",
        quota_policy: normalizePolicy(input.provider, input.quota_policy),
        label: input.label,
        last_four: input.secret.slice(-4),
        fingerprint: "fp:mock",
        enabled: input.enabled,
        validity: "unknown",
        billing_state: "unknown",
        breaker: "closed",
        cooldown_until: null,
        retirement_state: "active",
        retained_credential_count: 0,
        created_at: NOW,
        version: 1,
      };
      mockDb.state.keys.push(key);
      return HttpResponse.json(key, { status: 201 });
    }),
  ),
  ...(["patch", "rotate", "retire"] as const).map((action) =>
    (action === "patch" ? http.patch : http.post)(
      action === "patch" ? "*/v1/admin/keys/:id" : `*/v1/admin/keys/:id/${action}`,
      guarded("admin:keys:write", true, ({ params, body }) => {
        const input = body as Schemas["UpstreamKeyPatch"];
        const key = mockDb.state.keys.find((entry) => entry.id === param(params, "id"));
        if (key === undefined) {
          return apiError(404, "not_found", "Key not found.");
        }
        if (key.version !== input.expected_version) {
          return apiError(409, "revision_conflict", "Stale version.");
        }
        key.version += 1;
        key.label = input.label ?? key.label;
        if (input.quota_policy !== undefined)
          key.quota_policy = normalizePolicy(key.provider, input.quota_policy);
        if (action === "patch" && input.enabled !== undefined) {
          key.enabled = input.enabled;
        }
        if (action === "retire") {
          key.retirement_state = key.retained_credential_count > 0 ? "retiring" : "retired";
        }
        return HttpResponse.json(key);
      }),
    ),
  ),
  http.get(
    "*/v1/admin/routes",
    guarded("admin:read", false, ({ request }) =>
      HttpResponse.json(page(mockDb.state.routes, request)),
    ),
  ),
  http.post(
    "*/v1/admin/routes/preview",
    guarded("admin:read", true, ({ body }) => {
      const input = body as Schemas["RoutePreviewRequest"];
      const route = mockDb.state.routes.find((entry) => entry.capability === input.operation);
      const requested = "profile" in input.request ? input.request.profile : undefined;
      const eligible = (route?.enabled ? route.targets : []).filter(
        (target) => requested === undefined || target.profile === requested,
      );
      const preview: Schemas["RoutePreview"] = {
        route_revision: route?.revision ?? 0,
        eligible,
        exclusions:
          eligible.length === 0 && requested !== undefined
            ? [{ provider: "tavily", field: "profile", reason: "semantic downgrade refused" }]
            : [],
        warnings: [],
      };
      return HttpResponse.json(preview);
    }),
  ),
  http.put(
    "*/v1/admin/routes/:capability",
    guarded("admin:routes:write", true, ({ params, body }) => {
      const input = body as Schemas["RouteReplace"];
      const route = mockDb.state.routes.find(
        (entry) => entry.capability === param(params, "capability"),
      );
      if (route === undefined) {
        return apiError(404, "not_found", "Route not found.");
      }
      if (route.revision !== input.expected_version) {
        return apiError(409, "revision_conflict", "Stale revision.");
      }
      Object.assign(route, {
        enabled: input.enabled,
        targets: input.targets,
        max_attempts: input.max_attempts,
        deadline_seconds: input.deadline_seconds,
        allow_duplicate_cost: input.allow_duplicate_cost,
        revision: route.revision + 1,
        created_at: NOW,
      });
      return HttpResponse.json(route);
    }),
  ),
  http.get(
    "*/v1/admin/usage/trends",
    guarded("admin:read", false, ({ request }) =>
      HttpResponse.json(
        mockCallTrends(Number(new URL(request.url).searchParams.get("hours") ?? 24)),
      ),
    ),
  ),
  http.get(
    "*/v1/admin/usage",
    guarded("admin:read", false, () => HttpResponse.json(mockDb.state.usage)),
  ),
  http.post(
    "*/v1/admin/usage/refresh",
    guarded("admin:usage:refresh", true, () =>
      HttpResponse.json({
        status: "refreshed",
        observed_at: NOW,
        next_refresh_at: "2026-10-22T10:05:00Z",
      }),
    ),
  ),
  http.get(
    "*/v1/admin/requests",
    guarded("admin:read", false, ({ request }) => {
      const url = new URL(request.url);
      const filtered = mockDb.state.requests
        .map((entry) => entry.request)
        .filter(
          (record) =>
            (!url.searchParams.get("status") || record.status === url.searchParams.get("status")) &&
            (!url.searchParams.get("request_id") ||
              record.id === url.searchParams.get("request_id")) &&
            (!url.searchParams.get("operation") ||
              record.operation === url.searchParams.get("operation")) &&
            (!url.searchParams.get("provider") ||
              record.source_provider === url.searchParams.get("provider")),
        );
      return HttpResponse.json(page(filtered, request));
    }),
  ),
  http.get(
    "*/v1/admin/requests/:id",
    guarded("admin:read", false, ({ params }) => {
      const detail = mockDb.state.requests.find(
        (entry) => entry.request.id === param(params, "id"),
      );
      return detail === undefined
        ? apiError(404, "not_found", "Request not found.")
        : HttpResponse.json(detail);
    }),
  ),
  http.get(
    "*/v1/admin/research",
    guarded("admin:read", false, ({ request }) => {
      const state = new URL(request.url).searchParams.get("state");
      const jobs = [...mockDb.state.jobs.values()]
        .map((detail) => detail.job)
        .filter((job) => !state || job.state === state);
      return HttpResponse.json(page(jobs, request));
    }),
  ),
  http.post(
    "*/v1/admin/research",
    guarded("admin:research:create", true, ({ request, body }) => {
      const key = request.headers.get("Idempotency-Key") ?? "";
      if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$/.test(key)) {
        return apiError(400, "invalid_request", "Idempotency-Key is required.");
      }
      const input = body as Schemas["ResearchCreate"];
      const fingerprint = canonicalJson(input);
      const existing = mockDb.state.idempotency.get(key);
      if (existing !== undefined) {
        if (existing.fingerprint !== fingerprint) {
          return apiError(409, "idempotency_conflict", "Key bound to different intent.");
        }
        const replay = mockDb.state.jobs.get(existing.jobId);
        return HttpResponse.json(replay?.job, {
          status: 202,
          headers: { Location: `/v1/admin/research/${existing.jobId}` },
        });
      }
      const job = admitResearch(key, input);
      return HttpResponse.json(job, {
        status: 202,
        headers: { Location: `/v1/admin/research/${job.id}` },
      });
    }),
  ),
  http.get(
    "*/v1/admin/research/:id",
    guarded("admin:read", false, ({ params }) => {
      const detail = mockDb.state.jobs.get(param(params, "id"));
      return detail === undefined
        ? apiError(404, "not_found", "Job not found.")
        : HttpResponse.json(detail);
    }),
  ),
  http.post(
    "*/v1/admin/research/:id/cancel",
    guarded("admin:research:cancel", true, ({ params, body }) => {
      const input = body as Schemas["CancelRequest"];
      const detail = mockDb.state.jobs.get(param(params, "id"));
      if (detail === undefined) {
        return apiError(404, "not_found", "Job not found.");
      }
      if (detail.job.version !== input.expected_version) {
        return apiError(409, "revision_conflict", "Stale version.");
      }
      if (["succeeded", "failed", "cancelled"].includes(detail.job.state))
        return HttpResponse.json(detail.job);
      if (detail.job.state === "queued") {
        detail.job = {
          ...detail.job,
          state: "cancelled",
          active_slot_held: false,
          budget_hold_state: "released_unspent",
          upstream_may_continue: false,
          capacity_release_basis: "proven_non_submission",
          completion_reason: "cancelled_before_submission",
          version: detail.job.version + 1,
        };
        return HttpResponse.json(detail.job);
      }
      if (input.mode === "upstream" && detail.job.source_provider !== "exa")
        return apiError(409, "cancel_not_supported", "Provider cannot cancel.");
      if (input.mode === "observation") {
        detail.job = {
          ...detail.job,
          state: "observation_expired",
          upstream_may_continue: true,
          version: detail.job.version + 1,
        };
        return HttpResponse.json(detail.job, { status: 200 });
      }
      detail.job = { ...detail.job, state: "cancel_requested", version: detail.job.version + 1 };
      return HttpResponse.json(detail.job, { status: 202 });
    }),
  ),
  http.post(
    "*/v1/admin/research/:id/resume-observation",
    guarded("admin:research:observe", true, ({ params, body }) => {
      const input = body as Schemas["ResumeRequest"];
      const detail = mockDb.state.jobs.get(param(params, "id"));
      if (detail === undefined) {
        return apiError(404, "not_found", "Job not found.");
      }
      if (detail.job.version !== input.expected_version) {
        return apiError(409, "revision_conflict", "Stale version.");
      }
      if (
        !["submission_unknown", "observation_expired"].includes(detail.job.state) ||
        (detail.job.upstream_job_id === null && detail.job.source_provider !== "exa")
      )
        return apiError(409, "job_state_conflict", "Cannot resume this job.");
      detail.job = {
        ...detail.job,
        state: detail.job.upstream_job_id === null ? "submission_unknown" : "accepted",
        version: detail.job.version + 1,
      };
      return HttpResponse.json(detail.job, { status: 202 });
    }),
  ),
  http.post(
    "*/v1/admin/research/:id/reconcile",
    guarded("admin:research:reconcile", true, ({ params, body }) => {
      const input = body as Schemas["ReconcileRequest"];
      const detail = mockDb.state.jobs.get(param(params, "id"));
      if (detail === undefined) {
        return apiError(404, "not_found", "Job not found.");
      }
      if (detail.job.version !== input.expected_version) {
        return apiError(409, "revision_conflict", "Stale version.");
      }
      if (
        ["queued", "submitting"].includes(detail.job.state) ||
        (input.action === "attach_upstream_id" &&
          (detail.job.source_provider !== "exa" ||
            ["succeeded", "failed", "cancelled"].includes(detail.job.state))) ||
        (input.action === "settle_terminal" && detail.job.upstream_job_id === null)
      )
        return apiError(409, "job_state_conflict", "Reconciliation is unavailable.");
      const job = { ...detail.job, version: detail.job.version + 1 };
      if (input.action === "budget_write_off") {
        job.budget_hold_state = "written_off";
      } else if (input.action === "capacity_release_override") {
        job.active_slot_held = false;
        job.capacity_release_basis = "manual_override";
      } else if (input.action === "attach_upstream_id") {
        job.upstream_job_id = input.upstream_job_id;
      } else {
        job.active_slot_held = false;
        job.capacity_release_basis = "verified_terminal";
        job.budget_hold_state = input.cost === null ? "held" : "settled";
      }
      detail.job = job;
      return HttpResponse.json(job);
    }),
  ),
  http.get(
    "*/v1/admin/tokens",
    guarded("admin:read", false, ({ request }) =>
      HttpResponse.json(page(mockDb.state.tokens, request)),
    ),
  ),
  http.post(
    "*/v1/admin/tokens",
    guarded("admin:tokens:write", true, ({ body }) => {
      const input = body as Schemas["TokenCreate"];
      const token: Schemas["ClientToken"] = {
        id: `tok_new_${mockDb.state.nextId++}`,
        label: input.label,
        scopes: input.scopes,
        expires_at: input.expires_at,
        principal_id: mockDb.state.session?.principal_id ?? "",
        created_at: NOW,
        revoked_at: null,
        version: 1,
      };
      mockDb.state.tokens.unshift(token);
      return HttpResponse.json(
        { token, token_value: "exta_fixture_one_time_value_0001" },
        { status: 201 },
      );
    }),
  ),
  http.post(
    "*/v1/admin/tokens/:id/revoke",
    guarded("admin:tokens:write", true, ({ params, body }) => {
      const input = body as Schemas["VersionedAction"];
      const token = mockDb.state.tokens.find((entry) => entry.id === param(params, "id"));
      if (token === undefined) {
        return apiError(404, "not_found", "Token not found.");
      }
      if (token.version !== input.expected_version) {
        return apiError(409, "revision_conflict", "Stale version.");
      }
      token.revoked_at = NOW;
      token.version += 1;
      return HttpResponse.json(token);
    }),
  ),
  http.get(
    "*/v1/admin/settings",
    guarded("admin:read", false, () => HttpResponse.json(mockDb.state.settings)),
  ),
  http.patch(
    "*/v1/admin/settings",
    guarded("admin:settings:write", true, ({ body }) => {
      const input = body as Schemas["SettingsPatch"];
      const settings = mockDb.state.settings;
      if (settings.version !== input.expected_version) {
        return apiError(409, "revision_conflict", "Stale version.");
      }
      mockDb.state.settings = {
        ...settings,
        limits: input.limits ?? settings.limits,
        retention: input.retention ?? settings.retention,
        version: settings.version + 1,
      };
      return HttpResponse.json(mockDb.state.settings);
    }),
  ),
  http.get(
    "*/v1/admin/connection-info",
    guarded("admin:read", false, () => HttpResponse.json(mockDb.state.connectionInfo)),
  ),
];

/** Normalize fixture policy inputs using the management contract's zero semantics. */
function normalizePolicy(
  provider: Schemas["Provider"],
  input: Schemas["QuotaPolicyInput"] | undefined,
): Schemas["QuotaPolicy"] {
  return {
    unit: input?.unit ?? (provider === "exa" ? "micro_usd" : "tavily_credit"),
    allocation: input?.allocation ?? 0,
    per_operation_reservation: input?.per_operation_reservation ?? 0,
    active_jobs: input?.active_jobs ?? 0,
  };
}
