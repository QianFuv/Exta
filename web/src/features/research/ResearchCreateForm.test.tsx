/** Admin research creation: CSRF, retry key persistence, replay and denial side effects. */
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { Schemas } from "../../lib/api";
import { ALL_PERMISSIONS, mockSession } from "../../mocks/fixtures";
import { admitResearch, mockDb } from "../../mocks/handlers";
import { IDEMPOTENCY_KEY_PATTERN, loadPendingSubmission } from "../../lib/researchDraft";
import { callsTo, renderApp } from "../../test/renderApp";
import { server } from "../../test/server";
import { buildResearchBody } from "./ResearchCreateForm";

const CREATE_PATH = "/v1/admin/research";

/**
 * Fill the question field on the Research page.
 *
 * @param text - Question text.
 */
async function typeQuestion(text: string) {
  const field = await screen.findByLabelText("Research question");
  await userEvent.type(field, text);
}

describe("research creation", () => {
  it.each([null, {}, { id: "../invalid" }])(
    "retains the binding after an unusable success response %j",
    async (response) => {
      let capturedKey = "";
      server.use(
        http.post(
          "*/v1/admin/research",
          async ({ request }) => {
            capturedKey = request.headers.get("Idempotency-Key") ?? "";
            admitResearch(capturedKey, (await request.json()) as Schemas["ResearchCreate"]);
            return new HttpResponse(JSON.stringify(response), {
              status: 202,
              headers: { "Content-Type": "application/json" },
            });
          },
          { once: true },
        ),
      );
      const before = mockDb.state.jobs.size;
      renderApp("/research");
      await typeQuestion("Recover malformed success");
      await userEvent.click(screen.getByRole("button", { name: "Create research" }));
      await screen.findByText("Submission outcome unknown");
      expect(loadPendingSubmission("principal_operator")?.idempotencyKey).toBe(capturedKey);
      expect(screen.getByLabelText("Research question")).toHaveAttribute("readonly");
      await userEvent.click(screen.getByRole("button", { name: "Retry with same key" }));
      await screen.findByRole("heading", { name: "Research job" });
      expect(mockDb.state.jobs.size).toBe(before + 1);
      expect(callsTo("POST", CREATE_PATH).at(-1)?.headers["idempotency-key"]).toBe(capturedKey);
    },
  );
  it("does not let an older request overwrite a newer uncertain retry", async () => {
    let finishFirst: (() => void) | undefined;
    server.use(
      http.post(
        "*/v1/admin/research",
        async () => {
          await new Promise<void>((resolve) => {
            finishFirst = resolve;
          });
          return HttpResponse.json(
            {
              error: {
                code: "csrf_invalid",
                message: "Late rejection",
                request_id: "req_late",
                retryable: false,
                retry_after_ms: null,
                details: {},
              },
            },
            { status: 403 },
          );
        },
        { once: true },
      ),
    );
    const first = renderApp("/research");
    await typeQuestion("Concurrent retry identity");
    await userEvent.click(screen.getByRole("button", { name: "Create research" }));
    await waitFor(() => expect(finishFirst).toBeDefined());
    first.unmount();
    server.use(
      http.post(
        "*/v1/admin/research",
        async ({ request }) => {
          admitResearch(
            request.headers.get("Idempotency-Key") ?? "",
            (await request.json()) as Schemas["ResearchCreate"],
          );
          return HttpResponse.error();
        },
        { once: true },
      ),
    );
    renderApp("/research");
    await userEvent.click(await screen.findByRole("button", { name: "Retry with same key" }));
    await waitFor(() =>
      expect(loadPendingSubmission("principal_operator")?.state).toBe("uncertain"),
    );
    finishFirst?.();
    await waitFor(() =>
      expect(first.queryClient.getMutationCache().getAll()[0]?.state.status).toBe("error"),
    );
    expect(loadPendingSubmission("principal_operator")?.state).toBe("uncertain");
    expect(screen.getByLabelText("Research question")).toHaveAttribute("readonly");
  });

  it("uses the server's zero-based schema depth boundary", () => {
    const schema = {
      type: "object",
      properties: {
        a: {
          type: "object",
          properties: {
            b: { type: "object", properties: { c: { type: "array", items: { type: "string" } } } },
          },
        },
      },
    };
    const values = {
      input: "Depth boundary",
      profile: "exa_auto" as const,
      usdCap: "1",
      credits: "110",
      acknowledgeEstimate: false,
      observationSeconds: "",
      outputSchema: JSON.stringify(schema),
    };
    expect(buildResearchBody(values).errors).toBeNull();
    expect(
      buildResearchBody({ ...values, outputSchema: JSON.stringify({ anyOf: [schema] }) }).errors,
    ).toHaveProperty("outputSchema");
  });
  it("keeps uncertain intent across a denied retry and reload", async () => {
    let capturedKey = "";
    server.use(
      http.post(
        "*/v1/admin/research",
        async ({ request }) => {
          capturedKey = request.headers.get("Idempotency-Key") ?? "";
          admitResearch(capturedKey, (await request.json()) as Schemas["ResearchCreate"]);
          return HttpResponse.error();
        },
        { once: true },
      ),
    );
    const before = mockDb.state.jobs.size;
    const first = renderApp("/research");
    await typeQuestion("Retain uncertain intent");
    await userEvent.click(screen.getByRole("button", { name: "Create research" }));
    await screen.findByText("Submission outcome unknown");
    server.use(
      http.post(
        "*/v1/admin/research",
        () =>
          HttpResponse.json(
            {
              error: {
                code: "csrf_invalid",
                message: "Denied",
                request_id: "req_denied",
                retryable: false,
                retry_after_ms: null,
                details: {},
              },
            },
            { status: 403 },
          ),
        { once: true },
      ),
    );
    await userEvent.click(screen.getByRole("button", { name: "Retry with same key" }));
    await waitFor(() =>
      expect(loadPendingSubmission("principal_operator")?.state).not.toBe("sending"),
    );
    expect(loadPendingSubmission("principal_operator")?.state).toBe("uncertain");
    first.unmount();
    renderApp("/research");
    await screen.findByText("Submission outcome unknown");
    expect(screen.getByLabelText("Research question")).toHaveAttribute("readonly");
    await userEvent.click(screen.getByRole("button", { name: "Retry with same key" }));
    await screen.findByRole("heading", { name: "Research job" });
    expect(mockDb.state.jobs.size).toBe(before + 1);
    expect(callsTo("POST", CREATE_PATH).at(-1)?.headers["idempotency-key"]).toBe(capturedKey);
  });

  it("does not dispatch if retry persistence fails", async () => {
    renderApp("/research");
    await typeQuestion("Must retain retry identity");
    const storage = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("Storage unavailable");
    });
    try {
      await userEvent.click(screen.getByRole("button", { name: "Create research" }));
      expect(await screen.findByText(/Cannot save the retry/)).toBeInTheDocument();
      expect(callsTo("POST", CREATE_PATH)).toHaveLength(0);
    } finally {
      storage.mockRestore();
    }
  });

  it("submits through the admin endpoint with CSRF and a contract-valid retry key", async () => {
    const jobsBefore = mockDb.state.jobs.size;
    renderApp("/research");
    await typeQuestion("Compare vector databases");
    await userEvent.click(screen.getByRole("button", { name: "Create research" }));

    expect(await screen.findByRole("heading", { name: "Research job" })).toBeInTheDocument();
    const calls = callsTo("POST", CREATE_PATH);
    expect(calls).toHaveLength(1);
    const [call] = calls;
    expect(call?.headers["x-exta-csrf"]).toBe(mockSession().csrf_token);
    expect(call?.headers["idempotency-key"]).toMatch(IDEMPOTENCY_KEY_PATTERN);
    expect(call?.body).toEqual({
      input: "Compare vector databases",
      profile: "exa_auto",
      budget: { mode: "hard", unit: "micro_usd", amount: 1_000_000 },
    });
    expect(mockDb.state.jobs.size).toBe(jobsBefore + 1);
    expect(loadPendingSubmission("principal_operator")).toBeNull();
  });

  it("keeps the draft and key through a lost response, then replays to the same job", async () => {
    let firstKey = "";
    let firstBody: unknown = null;
    server.use(
      http.post(
        "*/v1/admin/research",
        async ({ request }) => {
          firstKey = request.headers.get("Idempotency-Key") ?? "";
          firstBody = await request.json();
          admitResearch(firstKey, firstBody as Schemas["ResearchCreate"]);
          return HttpResponse.error();
        },
        { once: true },
      ),
    );
    const jobsBefore = mockDb.state.jobs.size;
    const first = renderApp("/research");
    await typeQuestion("Market sizing for solar inverters");
    await userEvent.click(screen.getByRole("button", { name: "Create research" }));

    expect(await screen.findByText("Submission outcome unknown")).toBeInTheDocument();
    expect(screen.getByLabelText("Research question")).toHaveAttribute("readonly");
    expect(screen.getByRole("button", { name: "Create research" })).toBeDisabled();
    expect(loadPendingSubmission("principal_operator")?.idempotencyKey).toBe(firstKey);
    expect(firstKey).toMatch(IDEMPOTENCY_KEY_PATTERN);

    first.unmount();
    renderApp("/research");
    expect(await screen.findByText("Submission outcome unknown")).toBeInTheDocument();
    expect(screen.getByText(firstKey)).toBeInTheDocument();
    expect(screen.getByLabelText("Research question")).toHaveValue(
      "Market sizing for solar inverters",
    );

    await userEvent.click(screen.getByRole("button", { name: "Retry with same key" }));
    expect(await screen.findByRole("heading", { name: "Research job" })).toBeInTheDocument();

    const retries = callsTo("POST", CREATE_PATH);
    expect(retries).toHaveLength(1);
    expect(retries[0]?.headers["idempotency-key"]).toBe(firstKey);
    expect(retries[0]?.body).toEqual(firstBody);
    expect(mockDb.state.jobs.size).toBe(jobsBefore + 1);
    expect(loadPendingSubmission("principal_operator")).toBeNull();
  });

  it("creates no job and does not resend when the server rejects CSRF", async () => {
    const jobsBefore = mockDb.state.jobs.size;
    renderApp("/research");
    await typeQuestion("Denied research");
    if (mockDb.state.session !== null) {
      mockDb.state.session = { ...mockDb.state.session, csrf_token: "rotated-on-server" };
    }
    await userEvent.click(screen.getByRole("button", { name: "Create research" }));

    expect(await screen.findByText(/CSRF token was rejected/)).toBeInTheDocument();
    expect(callsTo("POST", CREATE_PATH)).toHaveLength(1);
    expect(mockDb.state.jobs.size).toBe(jobsBefore);
    expect(mockDb.state.idempotency.size).toBe(0);
    expect(screen.queryByText("Submission outcome unknown")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Research question")).not.toHaveAttribute("readonly");
    expect(loadPendingSubmission("principal_operator")?.state).toBe("rejected");
  });

  it("sends nothing when the principal lacks admin:research:create", async () => {
    mockDb.state.session = mockSession(
      ALL_PERMISSIONS.filter((permission) => permission !== "admin:research:create"),
    );
    renderApp("/research");
    const field = await screen.findByLabelText("Research question");
    expect(field).toBeDisabled();
    const button = screen.getByRole("button", { name: "Create research" });
    expect(button).toBeDisabled();
    expect(screen.getAllByText("admin:research:create").length).toBeGreaterThan(0);
    await userEvent.click(button);
    expect(callsTo("POST", CREATE_PATH)).toHaveLength(0);
  });

  it("requires acknowledging an estimated Tavily budget before sending", async () => {
    renderApp("/research");
    await typeQuestion("Tavily question");
    await userEvent.selectOptions(screen.getByLabelText("Profile"), "tavily_mini");
    expect(screen.getByText("Estimated reservation, not a cap")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Create research" }));
    expect(
      await screen.findByText("Acknowledge the estimated budget to continue."),
    ).toBeInTheDocument();
    expect(callsTo("POST", CREATE_PATH)).toHaveLength(0);

    await userEvent.click(screen.getByLabelText(/I understand this budget is an estimate/));
    await userEvent.click(screen.getByRole("button", { name: "Create research" }));
    await screen.findByRole("heading", { name: "Research job" });
    expect(callsTo("POST", CREATE_PATH)[0]?.body).toEqual({
      input: "Tavily question",
      profile: "tavily_mini",
      budget: {
        mode: "estimated",
        unit: "tavily_credit",
        amount: 110,
        acknowledge_estimated_budget: true,
      },
    });
  });

  it("reuses the same key when an unchanged draft is resent after a definitive rejection", async () => {
    server.use(
      http.post(
        "*/v1/admin/research",
        () =>
          HttpResponse.json(
            {
              error: {
                code: "local_rate_limited",
                message: "Slow down.",
                request_id: "req_rl",
                retryable: true,
                retry_after_ms: 1000,
                details: {},
              },
            },
            { status: 429 },
          ),
        { once: true },
      ),
    );
    renderApp("/research");
    await typeQuestion("Rate limited question");
    await userEvent.click(screen.getByRole("button", { name: "Create research" }));
    const alert = await screen.findByText(/Rate limited by Exta/);
    expect(within(alert).getByText(/local_rate_limited/)).toBeInTheDocument();
    const firstKey = loadPendingSubmission("principal_operator")?.idempotencyKey;

    await userEvent.click(screen.getByRole("button", { name: "Create research" }));
    await screen.findByRole("heading", { name: "Research job" });
    const calls = callsTo("POST", CREATE_PATH);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.headers["idempotency-key"]).toBe(firstKey);
    await waitFor(() => expect(loadPendingSubmission("principal_operator")).toBeNull());
  });
});
