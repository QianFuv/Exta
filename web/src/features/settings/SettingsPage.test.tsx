/** Settings: contract mismatch, versioned edits and permission gating. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import { mockDb } from "../../mocks/handlers";
import { callsTo, renderApp } from "../../test/renderApp";

beforeAll(async () => {
  await import("./SettingsPage");
});

describe("settings", () => {
  it("groups settings by purpose with explicit units and one save action", async () => {
    renderApp("/settings");
    const traffic = await screen.findByRole("group", { name: "Traffic and concurrency" });
    expect(within(traffic).getAllByRole("spinbutton")).toHaveLength(3);
    const research = screen.getByRole("group", { name: "Research capacity and observation" });
    expect(
      within(research).getByRole("spinbutton", { name: "Max polls" }),
    ).toHaveAccessibleDescription(/polls/);
    const payload = screen.getByRole("group", { name: "Request and response size" });
    expect(
      within(payload).getByRole("spinbutton", { name: "Max request bytes" }),
    ).toHaveAccessibleDescription(/bytes/);
    expect(screen.getAllByRole("button", { name: "Save" })).toHaveLength(1);
  });

  it("warns about an incompatible server contract", async () => {
    mockDb.state.settings = {
      ...mockDb.state.settings,
      build: { ...mockDb.state.settings.build, contract_hash: "0".repeat(64) },
    };
    renderApp("/settings?view=health");
    expect(await screen.findByText("Incompatible contract")).toBeInTheDocument();
    expect(screen.getByText("Mismatch").closest(".semi-tag")).toHaveClass("status-danger");
  });

  it("saves limits with the settings version and constant fields", async () => {
    renderApp("/settings");
    const field = await screen.findByRole("spinbutton", { name: "Max polls" });
    await userEvent.clear(field);
    await userEvent.type(field, "90");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Settings saved.")).toBeInTheDocument();
    const body = callsTo("PATCH", "/v1/admin/settings")[0]?.body as {
      expected_version: number;
      limits: { max_polls: number; principal_research_active: number };
      retention: { idempotency_days: number };
    };
    expect(body.expected_version).toBe(4);
    expect(body.limits.max_polls).toBe(90);
    expect(body.limits.principal_research_active).toBe(1);
    expect(body.retention.idempotency_days).toBe(90);
  });

  it("blocks out-of-range values before sending", async () => {
    renderApp("/settings");
    const field = await screen.findByRole("spinbutton", { name: "Research content days" });
    await userEvent.clear(field);
    await userEvent.type(field, "31");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("does not expose or fetch a standalone audit view and preserves internal retention", async () => {
    mockDb.state.settings.retention.audit_days = 61;
    renderApp("/settings?view=audit");
    expect(
      await screen.findByRole("group", { name: "Traffic and concurrency" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Audit" })).toBeNull();
    expect(callsTo("GET", "/v1/admin/audit")).toHaveLength(0);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Settings saved.")).toBeInTheDocument();
    expect(callsTo("PATCH", "/v1/admin/settings")[0]?.body).toMatchObject({
      retention: { audit_days: 61 },
    });
  });
});
