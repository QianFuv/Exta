/** Requests: empty success vs unknown counts, partial failures and pre-routing rejection. */
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { mockDb } from "../../mocks/handlers";
import { callsTo, renderApp } from "../../test/renderApp";

/**
 * Find the table row containing a request ID.
 *
 * @param id - Request ID.
 * @returns Scoped queries for the row.
 */
async function rowFor(id: string) {
  const cell = await screen.findByText(id);
  const row = cell.closest("tr");
  if (row === null) {
    throw new Error(`Row ${id} missing`);
  }
  return within(row);
}

describe("requests", () => {
  it("refreshes a progressing request using only local reads", async () => {
    const detail = mockDb.state.requests.find((entry) => entry.request.id === "req_ok");
    if (!detail) throw new Error("Missing request fixture");
    detail.request.status = "running";
    renderApp("/requests/req_ok");
    await screen.findByText("Running", { selector: ".badge" });
    detail.request.status = "succeeded";
    await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() =>
      expect(screen.getByText("Succeeded", { selector: ".badge" })).toBeInTheDocument(),
    );
    expect(callsTo("GET", "/v1/admin/requests/req_ok")).toHaveLength(2);
    expect(mockDb.state.calls.filter((entry) => entry.method !== "GET")).toHaveLength(0);
  });
  it("distinguishes known empty success, unknown summaries and unset route revisions", async () => {
    renderApp("/requests");
    expect(
      (await rowFor("req_empty")).getByText("Succeeded with zero results"),
    ).toBeInTheDocument();
    const unknown = await rowFor("req_unknown");
    expect(unknown.getByText("Not yet known")).toBeInTheDocument();
    expect(unknown.getByText("Uncertain")).toHaveClass("badge-warning");
    expect(unknown.getByText("Billing uncertain")).toBeInTheDocument();
    const rejected = await rowFor("req_rejected");
    expect(rejected.getByText("No route applied")).toBeInTheDocument();
    expect(rejected.getByText("None (pre-dispatch)")).toBeInTheDocument();
    const partial = await rowFor("req_partial");
    expect(partial.getByText("Partial")).toBeInTheDocument();
    expect(partial.getByText("Succeeded")).toHaveClass("badge-warning");
    expect((await rowFor("req_too_large")).getByText("response_too_large")).toBeInTheDocument();
  });

  it("applies filters as contract query parameters", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    await userEvent.selectOptions(screen.getByLabelText("Status"), "failed");
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(await screen.findByText("req_rejected")).toBeInTheDocument();
    expect(screen.queryByText("req_ok")).toBeNull();
  });

  it("shows an empty state when there are no records", async () => {
    mockDb.state.requests = [];
    renderApp("/requests");
    expect(await screen.findByText("No requests yet")).toBeInTheDocument();
  });

  it("shows attempts, partial and billing uncertainty on detail without raw content", async () => {
    const first = renderApp("/requests/req_partial");
    expect(await screen.findByText("Partial result")).toBeInTheDocument();
    expect(screen.getByText("Attempt 1")).toBeInTheDocument();
    expect(
      screen.getByText(/Raw queries, inputs and result content are not retained/),
    ).toBeInTheDocument();
    first.unmount();

    renderApp("/requests/req_unknown");
    expect(
      await screen.findByText("Billing uncertain", { selector: ".notice-title" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Possibly sent")).toBeInTheDocument();
  });

  it("reports an unknown or unauthorized detail as not found", async () => {
    renderApp("/requests/req_missing");
    expect(await screen.findByText("Not found.")).toBeInTheDocument();
    expect(callsTo("GET", "/v1/admin/requests/req_missing")).toHaveLength(1);
  });
});
