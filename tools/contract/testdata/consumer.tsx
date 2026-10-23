/** Compile-time consumer cases protect optional intent and strict generated request fields. */
import type { components, operations } from "../../../web/src/generated/types.gen";

export const MINIMAL_SEARCH: components["schemas"]["SearchRequest"] = {
  query: "Synthetic search",
};

export const UNRESOLVED_RESEARCH: components["schemas"]["ResearchCreate"] = {
  input: "Synthetic report",
  budget: { mode: "hard", unit: "micro_usd", amount: 1000000 },
};

export const UPDATE_KEY_LIMITS: components["schemas"]["UpstreamKeyPatch"] = {
  expected_version: 1,
  quota_policy: { unit: "micro_usd", allocation: 100, per_operation_reservation: 10, active_jobs: 1 },
};

export const ADMIN_RESEARCH_HEADERS: operations["adminResearchCreate"]["parameters"]["header"] = {
  "Idempotency-Key": "synthetic-retry-key-0001",
  "X-Exta-CSRF": "synthetic-csrf-for-typecheck-only-0001",
};

export const INVALID_OWNER: components["schemas"]["ResearchCreate"] = {
  ...UNRESOLVED_RESEARCH,
  // @ts-expect-error Caller ownership is not part of the creation contract.
  owner_principal_id: "forged",
};

export const INVALID_NULL_PROFILE: components["schemas"]["ResearchCreate"] = {
  ...UNRESOLVED_RESEARCH,
  // @ts-expect-error Missing profile preserves intent; null is invalid.
  profile: null,
};
