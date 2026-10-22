/** Retry-key persistence rules for research submissions. */
import { describe, expect, it } from "vitest";
import type { Schemas } from "./api";
import {
  IDEMPOTENCY_KEY_PATTERN,
  canonicalJson,
  loadPendingSubmission,
  prepareSubmission,
  savePendingSubmission,
} from "./researchDraft";

const BODY: Schemas["ResearchCreate"] = {
  input: "Question",
  profile: "exa_auto",
  budget: { mode: "hard", unit: "micro_usd", amount: 1_000_000 },
};

describe("research draft persistence", () => {
  it("treats objects with different key order as the same intent", () => {
    expect(canonicalJson({ b: 1, a: [2, { d: 3, c: 4 }] })).toBe(
      canonicalJson({ a: [2, { c: 4, d: 3 }], b: 1 }),
    );
    expect(canonicalJson(["x", "y"])).not.toBe(canonicalJson(["y", "x"]));
  });

  it("reuses the key only for unchanged intent", () => {
    const first = prepareSubmission(BODY, null, "principal_operator");
    expect(first.idempotencyKey).toMatch(IDEMPOTENCY_KEY_PATTERN);
    const reordered: Schemas["ResearchCreate"] = {
      budget: BODY.budget,
      profile: "exa_auto",
      input: BODY.input,
    };
    expect(prepareSubmission(reordered, first, "principal_operator").idempotencyKey).toBe(
      first.idempotencyKey,
    );
    expect(
      prepareSubmission({ ...BODY, input: "Changed" }, first, "principal_operator").idempotencyKey,
    ).not.toBe(first.idempotencyKey);
  });

  it("stores only the request body and key in session storage and rejects malformed data", () => {
    const submission = prepareSubmission(BODY, null, "principal_operator");
    savePendingSubmission(submission);
    expect(loadPendingSubmission("principal_operator")).toEqual(submission);
    expect(window.localStorage.length).toBe(0);
    window.sessionStorage.setItem(
      "exta.research.pendingSubmission.principal_operator",
      JSON.stringify({ ...submission, idempotencyKey: "bad key" }),
    );
    expect(loadPendingSubmission("principal_operator")).toBeNull();
  });
});

/** Corrupt or foreign principal state must never be replayed. */
describe("research principal isolation", () => {
  it("restores only the same principal and a matching valid body", () => {
    const submission = prepareSubmission(BODY, null, "principal_operator");
    savePendingSubmission(submission);
    expect(loadPendingSubmission("different_operator")).toBeNull();
    expect(loadPendingSubmission("principal_operator")).toEqual(submission);
    for (const body of [{}, { ...BODY, budget: {} }, { ...BODY, input: "changed" }]) {
      sessionStorage.setItem(
        "exta.research.pendingSubmission.principal_operator",
        JSON.stringify({ ...submission, body }),
      );
      expect(loadPendingSubmission("principal_operator")).toBeNull();
    }
  });
});
