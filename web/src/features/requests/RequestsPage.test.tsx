import { selectOption } from "../../test/selectOption";
/** Requests: empty success vs unknown counts, partial failures and pre-routing rejection. */
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import { mockDb } from "../../mocks/handlers";
import { callsTo, renderApp } from "../../test/renderApp";

beforeAll(async () => {
  await import("./RequestsPage");
});

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
  it("finds a logical call by request ID without showing unrelated calls", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    await userEvent.type(screen.getByRole("textbox", { name: "Request ID" }), "req_unknown");
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(screen.queryByText("req_ok")).toBeNull());
    expect(screen.getByText("req_unknown")).toBeInTheDocument();
  });
  it("refuses malformed typed dates without issuing a filtered request", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    const count = callsTo("GET", "/v1/admin/requests").length;
    await userEvent.type(screen.getByLabelText("Created after"), "not-a-date");
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    expect(callsTo("GET", "/v1/admin/requests")).toHaveLength(count);
  });

  it("explains reversed date filters without fetching another list", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    const before = callsTo("GET", "/v1/admin/requests").length;
    fireEvent.change(screen.getByLabelText("Created after"), {
      target: { value: "2026-10-23T12:00" },
    });
    fireEvent.change(screen.getByLabelText("Created before"), {
      target: { value: "2026-10-22T12:00" },
    });
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Start time must be earlier than end time.",
    );
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    expect(callsTo("GET", "/v1/admin/requests")).toHaveLength(before);
  });
  it("refreshes a progressing request using only local reads", async () => {
    const detail = mockDb.state.requests.find((entry) => entry.request.id === "req_ok");
    if (!detail) throw new Error("Missing request fixture");
    detail.request.status = "running";
    renderApp("/requests/req_ok");
    await screen.findByText("Running", { selector: ".semi-tag-content" });
    detail.request.status = "succeeded";
    await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() =>
      expect(screen.getByText("Succeeded", { selector: ".semi-tag-content" })).toBeInTheDocument(),
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
    expect(unknown.getByText("Uncertain").closest(".semi-tag")).toHaveClass("status-warning");
    expect(unknown.getByText("Billing uncertain")).toBeInTheDocument();
    const rejected = await rowFor("req_rejected");
    expect(rejected.getByText("None (pre-dispatch)")).toBeInTheDocument();
    const partial = await rowFor("req_partial");
    expect(partial.getByText("Partial")).toBeInTheDocument();
    expect(partial.getByText("Succeeded").closest(".semi-tag")).toHaveClass("status-warning");
    expect((await rowFor("req_too_large")).getByText("response_too_large")).toBeInTheDocument();
    await userEvent.click(rejected.getByRole("link"));
    expect(await screen.findByText("No route applied")).toBeInTheDocument();
  });

  it("applies filters as contract query parameters", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    await selectOption(screen.getByLabelText("Status"), "Failed");
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
      await screen.findByText("Billing uncertain", { selector: ".semi-banner-title" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Possibly sent")).toBeInTheDocument();
  });

  it("reports an unknown or unauthorized detail as not found", async () => {
    renderApp("/requests/req_missing");
    expect(await screen.findByText("Not found.")).toBeInTheDocument();
    expect(callsTo("GET", "/v1/admin/requests/req_missing")).toHaveLength(1);
  });
  it("opens details without losing list filters and restores keyboard focus on repeated close", async () => {
    renderApp("/requests");
    await screen.findByText("req_partial");
    await selectOption(screen.getByLabelText("Status"), "Succeeded");
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    for (let index = 0; index < 2; index++) {
      const row = await rowFor("req_partial");
      const opener = row.getByRole("link");
      await userEvent.click(opener);
      const dialog = await screen.findByRole("dialog", { name: "Request detail" });
      expect(await within(dialog).findByText("Attempt 1")).toBeInTheDocument();
      await userEvent.keyboard("{Escape}");
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(opener).toHaveFocus();
      expect(screen.getByLabelText("Status")).toHaveTextContent("Succeeded");
    }
    expect(mockDb.state.calls.filter((call) => call.method !== "GET")).toHaveLength(0);
  });
});
