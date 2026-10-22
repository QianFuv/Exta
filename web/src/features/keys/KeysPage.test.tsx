/** Key pools: grouping, masked keys, write-only secrets and version conflicts. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { server } from "../../test/server";
import { describe, expect, it } from "vitest";
import { ALL_PERMISSIONS, mockSession } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { allBrowserStorage, callsTo, renderApp } from "../../test/renderApp";

describe("key pools", () => {
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
    const card = (await screen.findByRole("heading", { name: "Exa main (fixture)" })).closest(
      "article",
    );
    if (!card) throw new Error("Missing account");
    await userEvent.click(
      within(card).getAllByRole("button", { name: action === "create" ? "Add key" : "Rotate" })[0]!,
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
  it("separates providers, flags account exhaustion, invalid keys and unknown grouping", async () => {
    renderApp("/keys");
    expect(await screen.findByRole("heading", { name: "Exa main (fixture)" })).toBeInTheDocument();
    expect(await screen.findByText("Account blocked")).toBeInTheDocument();
    expect(screen.getByText("Invalid key")).toHaveClass("badge-danger");
    expect(screen.getByText("Breaker open")).toBeInTheDocument();
    expect(screen.getByText("Unknown account grouping")).toBeInTheDocument();
    expect(screen.getByText(/Credential retained for 1 unresolved job/)).toBeInTheDocument();
    expect(screen.getByLabelText("Key ending in a1b2")).toHaveTextContent("••••a1b2");
  });

  it("sends a new secret once and keeps it out of storage", async () => {
    renderApp("/keys");
    const card = (await screen.findByRole("heading", { name: "Exa main (fixture)" })).closest(
      "article",
    );
    if (card === null) {
      throw new Error("Account card missing");
    }
    await userEvent.click(within(card).getByRole("button", { name: "Add key" }));
    const dialog = screen.getByRole("dialog", { name: "Add key" });
    await userEvent.type(within(dialog).getByLabelText("Label"), "Exa third");
    await userEvent.type(within(dialog).getByLabelText("Secret"), "synthetic-secret-9999");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add key" }));
    expect(await screen.findByText("Key added.")).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/keys")[0]?.body).toEqual({
      account_id: "acc_exa_main",
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
    await userEvent.click(within(row).getByRole("button", { name: "Disable" }));
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
    for (const button of screen.getAllByRole("button", { name: "Rotate" })) {
      expect(button).toBeDisabled();
    }
    expect(mockDb.state.calls.filter((call) => call.method !== "GET")).toHaveLength(0);
  });
});
