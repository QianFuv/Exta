/** Key pools: grouping, masked keys, write-only secrets and version conflicts. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { server } from "../../test/server";
import { beforeAll, describe, expect, it } from "vitest";
import { ALL_PERMISSIONS, mockSession } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { allBrowserStorage, callsTo, renderApp } from "../../test/renderApp";

beforeAll(async () => {
  await import("./KeysPage");
});

describe("key pools", () => {
  it("shows disabled state once and keeps the edit switch label independent of its value", async () => {
    mockDb.state.keys.find((entry) => entry.id === "key_exa_1")!.enabled = false;
    renderApp("/keys");
    const row = within((await screen.findByText("Exa primary")).closest("tr")!);
    expect(row.getAllByText("Disabled")).toHaveLength(1);
    await userEvent.click(row.getByRole("button", { name: "More actions for Exa primary" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Edit key & limits" }));
    const dialog = within(screen.getByRole("dialog"));
    const toggle = dialog.getByRole("switch", { name: "Enable this key" });
    expect(toggle).not.toBeChecked();
    expect(dialog.queryByText("Enabled")).toBeNull();
    await userEvent.click(toggle);
    expect(toggle).toBeChecked();
    expect(dialog.getByRole("switch", { name: "Enable this key" })).toBe(toggle);
  });
  it("does not label an enabled but blocked key as usable", async () => {
    const key = mockDb.state.keys.find((entry) => entry.id === "key_exa_1")!;
    key.enabled = true;
    key.billing_state = "blocked";
    renderApp("/keys");
    const row = within((await screen.findByText("Exa primary")).closest("tr")!);
    expect(row.getByText("Unavailable")).toBeInTheDocument();
    expect(row.getByText("Billing blocked")).toBeInTheDocument();
    expect(row.queryByText("Enabled")).toBeNull();
    expect(row.getByRole("button", { name: "Disable Exa primary" })).toBeEnabled();
  });
  it.each(["create", "rotate"])("clears failed %s secrets from mutation state", async (action) => {
    server.use(
      http.post(action === "create" ? "*/v1/admin/keys" : "*/v1/admin/keys/:id/rotate", () =>
        HttpResponse.json(
          {
            error: {
              code: "invalid_request",
              message: "Rejected",
              request_id: "req_secret",
              retryable: false,
              retry_after_ms: null,
              details: {},
            },
          },
          { status: 400 },
        ),
      ),
    );
    const { queryClient } = renderApp("/keys");
    await screen.findByText("Exa primary");
    await userEvent.click(
      action === "create"
        ? screen.getByRole("button", { name: "Add key" })
        : screen.getByRole("button", { name: "More actions for Exa primary" }),
    );
    await userEvent.click(
      screen.getByRole("menuitem", { name: action === "create" ? "Add key · Exa" : "Rotate" }),
    );
    const dialog = screen.getByRole("dialog");
    if (action === "create")
      await userEvent.type(within(dialog).getByLabelText("Label"), "Rejected key");
    await userEvent.type(within(dialog).getByLabelText("Secret"), "synthetic-rejected-secret");
    await userEvent.click(
      within(dialog).getByRole("button", { name: action === "create" ? "Add key" : "Rotate" }),
    );
    await within(dialog).findByText(/invalid_request/);
    expect(within(dialog).getByLabelText("Secret")).toHaveValue("");
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((entry) => entry.state),
      ),
    ).not.toContain("synthetic-rejected-secret");
  });
  it("keeps secondary actions behind a disclosure and requires retirement confirmation", async () => {
    renderApp("/keys");
    const row = (await screen.findByText("Exa primary")).closest("tr")!;
    const summary = within(row).getByRole("button", { name: /More actions/ });
    expect(screen.queryByRole("menuitem", { name: "Retire" })).toBeNull();
    for (let attempt = 0; attempt < 2; attempt += 1) {
      summary.focus();
      await userEvent.keyboard(attempt === 0 ? "{Enter}" : " ");
      await userEvent.click(screen.getByRole("menuitem", { name: "Retire" }));
      const dialog = screen.getByRole("dialog", { name: "Retire Exa primary" });
      expect(callsTo("POST", "/v1/admin/keys/key_exa_1/retire")).toHaveLength(0);
      await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
      expect(summary).toHaveFocus();
      expect(summary).toHaveAttribute("aria-expanded", "false");
    }
    await userEvent.click(summary);
    await userEvent.keyboard("{Escape}");
    expect(summary).toHaveAttribute("aria-expanded", "false");
    expect(summary).toHaveFocus();
  });

  it("shows key-owned limits and unknown identity without an account workflow", async () => {
    renderApp("/keys");
    expect(await screen.findByText("Exa primary")).toBeInTheDocument();
    expect(
      screen.getByText(/Manage Exa and Tavily keys, enablement and safety limits directly/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/billing account/i)).toBeNull();
    expect(screen.getAllByText("Not independently verified")).toHaveLength(4);
    expect(screen.queryByRole("button", { name: "Add account" })).toBeNull();
    expect(callsTo("GET", "/v1/admin/accounts")).toHaveLength(0);
    expect(screen.getByText(/Credential retained for 1 unresolved job/)).toBeInTheDocument();
  });

  it("sends a new secret once and keeps it out of storage", async () => {
    renderApp("/keys");
    await userEvent.click(await screen.findByRole("button", { name: "Add key" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Add key · Exa" }));
    const dialog = screen.getByRole("dialog", { name: "Add key · Exa" });
    await userEvent.type(within(dialog).getByLabelText("Label"), "Exa third");
    await userEvent.type(within(dialog).getByLabelText("Secret"), "synthetic-secret-9999");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add key" }));
    expect(await screen.findByText("Key added.")).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/keys")[0]?.body).toEqual({
      provider: "exa",
      quota_policy: {
        unit: "micro_usd",
        allocation: 0,
        per_operation_reservation: 0,
        active_jobs: 0,
      },
      label: "Exa third",
      secret: "synthetic-secret-9999",
      enabled: true,
    });
    expect(allBrowserStorage()).not.toContain("synthetic-secret-9999");
    expect(screen.queryByDisplayValue("synthetic-secret-9999")).toBeNull();
  });

  it("reports a concurrent edit as a revision conflict", async () => {
    renderApp("/keys");
    const row = (await screen.findByText("Exa primary")).closest("tr");
    if (row === null) {
      throw new Error("Key row missing");
    }
    const key = mockDb.state.keys.find((entry) => entry.id === "key_exa_1");
    if (key !== undefined) {
      key.version += 1;
    }
    await userEvent.click(within(row).getByRole("button", { name: "Disable Exa primary" }));
    expect(await screen.findByText(/changed elsewhere/)).toBeInTheDocument();
    expect(callsTo("PATCH", "/v1/admin/keys/key_exa_1")[0]?.body).toEqual({
      expected_version: 1,
      enabled: false,
    });
  });

  it("disables write actions without admin:keys:write", async () => {
    mockDb.state.session = mockSession(
      ALL_PERMISSIONS.filter((permission) => permission !== "admin:keys:write"),
    );
    renderApp("/keys");
    await screen.findByText("Exa primary");
    expect(screen.getByRole("button", { name: "Add key" })).toBeDisabled();
    expect(screen.getByText("admin:keys:write")).toBeInTheDocument();
    for (const trigger of screen.getAllByRole("button", { name: /More actions/ })) {
      expect(trigger).toBeDisabled();
    }
    expect(mockDb.state.calls.filter((call) => call.method !== "GET")).toHaveLength(0);
  });
});
