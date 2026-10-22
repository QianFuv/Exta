/** Research detail: honest uncertain/expired/partial states, holds and versioned actions. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ALL_PERMISSIONS, mockSession } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { callsTo, renderApp } from "../../test/renderApp";
import { pollingDelay } from "./ResearchDetailPage";
import { jobTone } from "./jobStatus";
import { mockJob } from "../../mocks/fixtures";

/**
 * Find the status section's state badge.
 *
 * @param label - Badge text.
 * @returns Badge element.
 */
async function stateBadge(label: string) {
  const heading = await screen.findByRole("heading", { name: "Status" });
  const section = heading.closest("section");
  if (section === null) {
    throw new Error("Status section missing");
  }
  return within(section).getAllByText(label)[0] as HTMLElement;
}

describe("research detail", () => {
  it("offers unknown-ID Exa discovery but refuses unsupported Tavily attachment", async () => {
    const detail = mockDb.state.jobs.get("job_unknown");
    if (!detail) throw new Error("Missing job fixture");
    detail.job.source_provider = "exa";
    const first = renderApp("/research/job_unknown");
    expect(await screen.findByRole("button", { name: "Resume observation" })).toBeEnabled();
    first.unmount();
    detail.job.source_provider = "tavily";
    renderApp("/research/job_unknown");
    await userEvent.click(await screen.findByRole("button", { name: "Reconcile…" }));
    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).queryByRole("radio", { name: /Attach verified upstream ID/ }),
    ).not.toBeInTheDocument();
    expect(within(dialog).queryByRole("radio", { name: /Settle/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Resume observation" })).not.toBeInTheDocument();
  });

  it("releases queued unsent holds when observation is cancelled", async () => {
    const detail = mockDb.state.jobs.get("job_running");
    if (!detail) throw new Error("Missing job fixture");
    detail.job.state = "queued";
    detail.job.upstream_job_id = null;
    renderApp("/research/job_running");
    await userEvent.click(await screen.findByRole("button", { name: "Stop observing" }));
    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).queryByText("Slot and budget holds are retained"),
    ).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Stop observing" }));
    await stateBadge("Cancelled");
    expect(detail.job.active_slot_held).toBe(false);
    expect(detail.job.budget_hold_state).toBe("released_unspent");
  });
  it("never shows uncertain or expired observation states as success", () => {
    expect(jobTone({ state: "submission_unknown", uncertainty: true, is_partial: false })).toBe(
      "warning",
    );
    expect(jobTone({ state: "observation_expired", uncertainty: false, is_partial: false })).toBe(
      "warning",
    );
    expect(jobTone({ state: "succeeded", uncertainty: false, is_partial: true })).toBe("warning");
    expect(jobTone({ state: "succeeded", uncertainty: true, is_partial: false })).toBe("warning");
    expect(jobTone({ state: "succeeded", uncertainty: false, is_partial: false })).toBe("success");
  });

  it("shows retained slot and budget holds and that upstream may continue after observation expiry", async () => {
    renderApp("/research/job_expired");
    const badge = await stateBadge("Observation expired");
    expect(badge).toHaveClass("badge-warning");
    expect(badge).not.toHaveClass("badge-success");
    expect(screen.getByText("The provider may still be running this job")).toBeInTheDocument();
    expect(screen.getAllByText("Slot held").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Budget held").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "Resume observation" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Reconcile…" })).toBeEnabled();
  });

  it("explains submission_unknown without offering cancellation", async () => {
    renderApp("/research/job_unknown");
    const badge = await stateBadge("Submission unknown");
    expect(badge).toHaveClass("badge-warning");
    expect(screen.getByText("Submission outcome unknown")).toBeInTheDocument();
    expect(screen.getByText("Provider cancellation is not available")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel at provider" })).toBeNull();
    expect(screen.getByText("Not known")).toBeInTheDocument();
  });

  it("labels a partial result and a provider failure honestly", async () => {
    const first = renderApp("/research/job_partial");
    expect(await stateBadge("Succeeded")).toHaveClass("badge-warning");
    expect(screen.getByText(/Reason: budget_exhausted/)).toBeInTheDocument();
    first.unmount();

    renderApp("/research/job_failed");
    expect(await stateBadge("Failed")).toHaveClass("badge-danger");
    expect(screen.getByText(/upstream_failure: Provider reported failure/)).toBeInTheDocument();
    expect(screen.getAllByText("Budget held").length).toBeGreaterThan(0);
  });

  it("renders the stored report safely with only safe citation links", async () => {
    const { container } = renderApp("/research/job_done");
    expect(await stateBadge("Succeeded")).toHaveClass("badge-success");
    expect(await screen.findByRole("heading", { name: "Findings", level: 3 })).toBeInTheDocument();
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("script")).toBeNull();
    expect(screen.getByRole("link", { name: "Example source" })).toHaveAttribute(
      "rel",
      "noopener noreferrer nofollow",
    );
    expect(screen.queryByRole("link", { name: "Bad scheme" })).toBeNull();
  });

  it("stops observation with the current version and keeps holds", async () => {
    renderApp("/research/job_running");
    await userEvent.click(await screen.findByRole("button", { name: "Stop observing" }));
    const dialog = screen.getByRole("dialog", { name: "Stop observing this job?" });
    expect(within(dialog).getByText("Slot and budget holds are retained")).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Stop observing" }));

    expect(await screen.findByText(/Observation stopped/)).toBeInTheDocument();
    const calls = callsTo("POST", "/v1/admin/research/job_running/cancel");
    expect(calls).toHaveLength(1);
    expect(calls[0]?.body).toEqual({ mode: "observation", expected_version: 2 });
    expect(calls[0]?.headers["x-exta-csrf"]).toBe(mockSession().csrf_token);
  });

  it("requires the literal acknowledgement before a budget write-off", async () => {
    renderApp("/research/job_unknown");
    await userEvent.click(await screen.findByRole("button", { name: "Reconcile…" }));
    const dialog = screen.getByRole("dialog", { name: "Reconcile job" });
    await userEvent.click(within(dialog).getByLabelText("Write off budget (risk)"));
    expect(
      within(dialog).getByText(/Allowance is not restored and the active slot is not released/),
    ).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("Reason"), "Provider confirmed no record");
    await userEvent.type(within(dialog).getByLabelText("Evidence reference"), "ticket-42");
    const submit = within(dialog).getByRole("button", { name: "Record reconciliation" });
    expect(submit).toBeDisabled();
    await userEvent.click(within(dialog).getByLabelText(/full reservation will count as spent/));
    expect(submit).toBeEnabled();
    await userEvent.click(submit);

    expect(await screen.findByText("Reconciliation recorded.")).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/research/job_unknown/reconcile")[0]?.body).toEqual({
      action: "budget_write_off",
      expected_version: 5,
      reason: "Provider confirmed no record",
      evidence_ref: "ticket-42",
      acknowledge_upstream_may_bill: true,
    });
  });

  it("surfaces a stale-version conflict instead of retrying", async () => {
    renderApp("/research/job_expired");
    await userEvent.click(await screen.findByRole("button", { name: "Resume observation" }));
    const detail = mockDb.state.jobs.get("job_expired");
    if (detail !== undefined) {
      detail.job = { ...detail.job, version: detail.job.version + 1 };
    }
    const dialog = screen.getByRole("dialog", { name: "Resume observation" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Resume observation" }));
    expect(await within(dialog).findByText(/changed elsewhere/)).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/research/job_expired/resume-observation")).toHaveLength(1);
  });

  it("disables actions without the matching permission and sends nothing", async () => {
    mockDb.state.session = mockSession(
      ALL_PERMISSIONS.filter(
        (permission) =>
          permission !== "admin:research:cancel" && permission !== "admin:research:reconcile",
      ),
    );
    renderApp("/research/job_running");
    const stop = await screen.findByRole("button", { name: "Stop observing" });
    expect(stop).toBeDisabled();
    await userEvent.click(stop);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mockDb.state.calls.filter((call) => call.method === "POST")).toHaveLength(0);
  });

  it("polls only progressing jobs, with backoff", () => {
    expect(pollingDelay(mockJob({ state: "running" }), 0)).toBe(5000);
    expect(pollingDelay(mockJob({ state: "running" }), 6)).toBe(20000);
    expect(pollingDelay(mockJob({ state: "running" }), 60)).toBe(60000);
    expect(pollingDelay(mockJob({ state: "observation_expired" }), 0)).toBe(false);
    expect(pollingDelay(mockJob({ state: "submission_unknown" }), 0)).toBe(5000);
    expect(pollingDelay(mockJob({ state: "succeeded" }), 0)).toBe(false);
  });
});
