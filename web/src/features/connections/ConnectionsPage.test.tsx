/** Connections: placeholder examples, OAuth disclosure and one-time token reveal. */
import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { mockDb } from "../../mocks/handlers";
import { allBrowserStorage, callsTo, renderApp } from "../../test/renderApp";

const ONE_TIME_VALUE = "exta_fixture_one_time_value_0001";

describe("connections and tokens", () => {
  it("updates token expiry while the page remains mounted", async () => {
    const token = mockDb.state.tokens[0];
    if (!token) throw new Error("Missing token fixture");
    token.expires_at = new Date(Date.now() + 60000).toISOString();
    token.revoked_at = null;
    mockDb.state.tokens = [token];
    renderApp("/connections");
    await screen.findByText("Active");
    vi.useFakeTimers();
    try {
      vi.setSystemTime(Date.now() + 61000);
      act(() => document.dispatchEvent(new Event("visibilitychange")));
      expect(screen.getByText("Expired")).toBeInTheDocument();
      expect(screen.queryByText("Active")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
  it("shows endpoints, protocol versions and the OAuth-only limitation", async () => {
    renderApp("/connections");
    expect(await screen.findByText("http://127.0.0.1:8789/mcp")).toBeInTheDocument();
    expect(screen.getByText("2026-07-28")).toBeInTheDocument();
    expect(screen.getByText("OAuth-only clients are not supported")).toBeInTheDocument();
    expect(screen.getByText(/<EXTA_TOKEN>/)).toBeInTheDocument();
    expect(screen.getByText("Loopback only")).toBeInTheDocument();
  });

  it("reveals a minted token once and clears it on dismissal and reload", async () => {
    const first = renderApp("/connections");
    await userEvent.click(await screen.findByRole("button", { name: "Create token" }));
    const dialog = screen.getByRole("dialog", { name: "Create token" });
    await userEvent.type(within(dialog).getByLabelText("Label"), "CI runner");
    await userEvent.click(within(dialog).getByRole("checkbox", { name: "research:read" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Create token" }));

    expect(await screen.findByText(ONE_TIME_VALUE)).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/tokens")[0]?.body).toMatchObject({
      label: "CI runner",
      scopes: ["read", "research:read"],
    });
    expect(allBrowserStorage()).not.toContain(ONE_TIME_VALUE);

    await userEvent.click(screen.getByRole("button", { name: "I have copied it" }));
    expect(screen.queryByText(ONE_TIME_VALUE)).toBeNull();

    first.unmount();
    renderApp("/connections");
    expect(await screen.findByText("CI runner")).toBeInTheDocument();
    expect(screen.queryByText(ONE_TIME_VALUE)).toBeNull();
  });

  it("explains that clients cannot connect when no token is active", async () => {
    mockDb.state.tokens = [];
    renderApp("/connections");
    expect(await screen.findByText("No client tokens")).toBeInTheDocument();
  });
});
