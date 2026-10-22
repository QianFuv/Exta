/**
 * Persistence of an in-flight research submission so an uncertain outcome can be retried
 * with the same `Idempotency-Key` and identical intent, even after a reload. Only the
 * research request body and retry key are stored; no session or credential material.
 */
import type { Schemas } from "./api";

/** Lifecycle of the stored submission. */
export type PendingState = "sending" | "uncertain" | "rejected";

/** A research creation attempt awaiting a definitive server outcome. */
export interface PendingSubmission {
  version: 2;
  principalId: string;
  attemptId: string;
  hasUnresolvedAttempt: boolean;
  idempotencyKey: string;
  body: Schemas["ResearchCreate"];
  fingerprint: string;
  state: PendingState;
  updatedAt: string;
}

const STORAGE_KEY = "exta.research.pendingSubmission";

/** Contract pattern for retry keys: 8–128 ASCII letters/digits/`._:-`, alphanumeric first. */
export const IDEMPOTENCY_KEY_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$/;

/**
 * Serialize a JSON value with object keys sorted so equal intent has equal text.
 *
 * @param value - JSON-compatible value.
 * @returns Canonical JSON text.
 */
export function canonicalJson(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map((entry) => canonicalJson(entry)).join(",")}]`;
  }
  if (typeof value === "object" && value !== null) {
    const entries = Object.entries(value as Record<string, unknown>)
      .filter(([, entry]) => entry !== undefined)
      .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0));
    return `{${entries.map(([key, entry]) => `${JSON.stringify(key)}:${canonicalJson(entry)}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

/**
 * Create a new random retry key.
 *
 * @returns A UUID, which satisfies the contract key pattern.
 */
export function newIdempotencyKey(): string {
  return crypto.randomUUID();
}

/**
 * Read the stored submission, discarding anything malformed.
 *
 * @param principalId - Stable authenticated owner of the retry binding.
 * @returns The pending submission or null.
 */
export function loadPendingSubmission(principalId: string): PendingSubmission | null {
  const raw = window.sessionStorage.getItem(`${STORAGE_KEY}.${principalId}`);
  if (raw === null) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as Partial<PendingSubmission>;
    if (
      parsed.version === 2 &&
      parsed.principalId === principalId &&
      typeof parsed.attemptId === "string" &&
      IDEMPOTENCY_KEY_PATTERN.test(parsed.attemptId) &&
      typeof parsed.hasUnresolvedAttempt === "boolean" &&
      typeof parsed.idempotencyKey === "string" &&
      IDEMPOTENCY_KEY_PATTERN.test(parsed.idempotencyKey) &&
      isResearchBody(parsed.body) &&
      parsed.fingerprint === canonicalJson(parsed.body) &&
      (parsed.state === "sending" || parsed.state === "uncertain" || parsed.state === "rejected")
    ) {
      return parsed as PendingSubmission;
    }
  } catch {
    return null;
  }
  return null;
}

/**
 * Store the submission. A reload while `sending` is treated as uncertain.
 *
 * @param submission - Submission to persist.
 */
export function savePendingSubmission(submission: PendingSubmission): void {
  window.sessionStorage.setItem(
    `${STORAGE_KEY}.${submission.principalId}`,
    JSON.stringify(submission),
  );
}

/** Remove this principal's draft after success or explicit discard. */
export function clearPendingSubmission(principalId: string): void {
  window.sessionStorage.removeItem(`${STORAGE_KEY}.${principalId}`);
}

/**
 * Prepare the submission for a request body, reusing the stored key when the intent is
 * unchanged.
 *
 * @param body - Research creation body.
 * @param existing - Previously stored submission.
 * @param principalId - Stable authenticated owner.
 * @returns Submission to send.
 */
export function prepareSubmission(
  body: Schemas["ResearchCreate"],
  existing: PendingSubmission | null,
  principalId: string,
): PendingSubmission {
  const fingerprint = canonicalJson(body);
  const reuse =
    existing !== null &&
    existing.principalId === principalId &&
    existing.fingerprint === fingerprint;
  return {
    version: 2,
    principalId,
    attemptId: crypto.randomUUID(),
    hasUnresolvedAttempt: reuse && existing.hasUnresolvedAttempt,
    idempotencyKey: reuse ? existing.idempotencyKey : newIdempotencyKey(),
    body,
    fingerprint,
    state: "sending",
    updatedAt: new Date().toISOString(),
  };
}

/** Validate persisted intent before accessing it or replaying it to the server. */
function isResearchBody(value: unknown): value is Schemas["ResearchCreate"] {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const body = value as Record<string, unknown>;
  if (
    Object.keys(body).some(
      (key) =>
        !["input", "profile", "budget", "observation_seconds", "output_schema"].includes(key),
    )
  )
    return false;
  if (typeof body.input !== "string" || !body.input.trim() || body.input.length > 32000)
    return false;
  if (
    body.profile !== undefined &&
    !["exa_auto", "tavily_mini", "tavily_pro"].includes(String(body.profile))
  )
    return false;
  if (typeof body.budget !== "object" || body.budget === null || Array.isArray(body.budget))
    return false;
  const budget = body.budget as Record<string, unknown>;
  if (!Number.isSafeInteger(budget.amount)) return false;
  if (budget.mode === "hard") {
    if (
      budget.unit !== "micro_usd" ||
      Number(budget.amount) < 1000000 ||
      Number(budget.amount) > 100000000 ||
      (body.profile !== undefined && body.profile !== "exa_auto")
    )
      return false;
  } else if (budget.mode === "estimated") {
    if (
      budget.unit !== "tavily_credit" ||
      budget.acknowledge_estimated_budget !== true ||
      Number(budget.amount) < 110 ||
      Number(budget.amount) > 250 ||
      body.profile === "exa_auto" ||
      (body.profile === "tavily_pro" && Number(budget.amount) < 250)
    )
      return false;
  } else return false;
  if (
    body.observation_seconds !== undefined &&
    (!Number.isInteger(body.observation_seconds) ||
      Number(body.observation_seconds) < 60 ||
      Number(body.observation_seconds) > 3600)
  )
    return false;
  return (
    body.output_schema === undefined ||
    (typeof body.output_schema === "object" &&
      body.output_schema !== null &&
      !Array.isArray(body.output_schema) &&
      new TextEncoder().encode(JSON.stringify(body.output_schema)).length <= 16384)
  );
}
