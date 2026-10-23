/** Routing: no-route states, versioned replacement and local preview. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { mockDb } from "../../mocks/handlers";
import { callsTo, renderApp } from "../../test/renderApp";

/**
 * Find a policy row by capability.
 *
 * @param capability - Operation name.
 * @returns Row element.
 */
async function policyRow(capability: string): Promise<HTMLElement> {
  const row = (await screen.findByText(capability, { selector: "th code" })).closest("tr");
  if (row === null) {
    throw new Error(`${capability} row missing`);
  }
  return row;
}

describe("routing", () => {
  it.each([
    ["search", 30],
    ["research_create", 30],
    ["advanced_search", 60],
    ["fetch", 60],
    ["map", 165],
    ["crawl", 165],
  ])("bounds %s deadlines before dispatch", async (operation, limit) => {
    renderApp("/settings?view=routing");
    await userEvent.click(
      within(await policyRow(String(operation))).getByRole("button", { name: "Edit" }),
    );
    const dialog = screen.getByRole("dialog");
    const field = within(dialog).getByLabelText("Deadline (seconds)");
    await userEvent.clear(field);
    await userEvent.type(field, String(Number(limit) + 1));
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    expect(callsTo("PUT", `/v1/admin/routes/${operation}`)).toHaveLength(0);
    await userEvent.clear(field);
    await userEvent.type(field, String(limit));
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeEnabled();
  });
  it("flags an enabled policy without targets", async () => {
    renderApp("/settings?view=routing");
    expect(within(await policyRow("crawl")).getByText("No compatible route")).toHaveClass(
      "badge-danger",
    );
  });

  it("saves with expected_version equal to the current revision", async () => {
    renderApp("/settings?view=routing");
    await userEvent.click(
      within(await policyRow("advanced_search")).getByRole("button", { name: "Edit" }),
    );
    const dialog = screen.getByRole("dialog", { name: "Edit route: advanced_search" });
    await userEvent.selectOptions(within(dialog).getByLabelText("Add target"), "tavily/common");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Move tavily/common up" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Route saved as revision 3.")).toBeInTheDocument();
    expect(callsTo("PUT", "/v1/admin/routes/advanced_search")[0]?.body).toEqual({
      expected_version: 2,
      enabled: true,
      targets: [
        { provider: "tavily", profile: "common" },
        { provider: "exa", profile: "common" },
      ],
      max_attempts: 2,
      deadline_seconds: 60,
      allow_duplicate_cost: false,
    });
  });

  it("shows a stale revision conflict", async () => {
    renderApp("/settings?view=routing");
    await userEvent.click(within(await policyRow("search")).getByRole("button", { name: "Edit" }));
    const route = mockDb.state.routes.find((entry) => entry.capability === "search");
    if (route !== undefined) {
      route.revision = 5;
    }
    const dialog = screen.getByRole("dialog", { name: "Edit route: search" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText(/changed elsewhere/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    await userEvent.click(within(dialog).getByRole("button", { name: "Reload and review latest" }));
    await userEvent.click(within(await policyRow("search")).getByRole("button", { name: "Edit" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Route saved as revision 6.")).toBeInTheDocument();
    expect(callsTo("PUT", "/v1/admin/routes/search").at(-1)?.body).toMatchObject({
      expected_version: 5,
    });
  });
});
