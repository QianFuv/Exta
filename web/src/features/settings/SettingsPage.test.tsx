/** Settings and audit: contract mismatch, versioned edits and permission gating. */
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ALL_PERMISSIONS, mockSession } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { callsTo, renderApp } from "../../test/renderApp";

describe("settings and audit", () => {
  it("warns about an incompatible server contract", async () => {
    mockDb.state.settings = {
      ...mockDb.state.settings,
      build: { ...mockDb.state.settings.build, contract_hash: "0".repeat(64) },
    };
    renderApp("/settings");
    expect(await screen.findByText("Incompatible contract")).toBeInTheDocument();
    expect(screen.getByText("Mismatch")).toHaveClass("badge-danger");
  });

  it("saves limits with the settings version and constant fields", async () => {
    renderApp("/settings");
    const field = await screen.findByLabelText("Max polls");
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
    const field = await screen.findByLabelText("Research content days");
    await userEvent.clear(field);
    await userEvent.type(field, "31");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("shows audit as locked without admin:audit:read and makes no request", async () => {
    mockDb.state.session = mockSession(
      ALL_PERMISSIONS.filter((permission) => permission !== "admin:audit:read"),
    );
    renderApp("/audit");
    expect(await screen.findByText("Not available for this principal")).toBeInTheDocument();
    expect(callsTo("GET", "/v1/admin/audit")).toHaveLength(0);
  });

  it("lists redacted audit events", async () => {
    renderApp("/audit");
    expect(await screen.findByText("route.replace")).toBeInTheDocument();
    expect(screen.getByText("key.create")).toBeInTheDocument();
  });
});
