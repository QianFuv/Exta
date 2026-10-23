/** Usage: separated units, unlimited vs unknown, permission gaps, divergence and cooldown. */
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { renderApp } from "../../test/renderApp";
import { findDivergence } from "./UsagePage";
import { mockDb } from "../../mocks/handlers";
import { server } from "../../test/server";

describe("usage", () => {
  it("does not compare unknown or different accounting periods", () => {
    const upstream = mockDb.state.usage.items.find((entry) => entry.source === "upstream");
    if (!upstream) throw new Error("Missing usage fixture");
    const local = { ...upstream, source: "local" as const, used: (upstream.used ?? 0) + 10 };
    expect(findDivergence([upstream, local]).size).toBe(1);
    expect(
      findDivergence([upstream, { ...local, period_start: null, period_end: null }]).size,
    ).toBe(0);
    expect(
      findDivergence([upstream, { ...local, period_start: "2026-01-01T00:00:00Z" }]).size,
    ).toBe(0);
  });
  it("labels unlimited, unknown, missing permission and ledger divergence distinctly", async () => {
    renderApp("/overview?view=usage");
    expect(await screen.findByText(/Unlimited/)).toBeInTheDocument();
    expect(screen.getAllByText(/Limit unknown/).length).toBeGreaterThan(0);
    expect(screen.getByText(/lacks the service permission/)).toBeInTheDocument();
    expect(
      screen.getAllByText("Local ledger differs from upstream snapshot").length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("Stale")).toBeInTheDocument();
    expect(screen.getByText(/640 credits/)).toBeInTheDocument();
    expect(screen.getAllByText(/\$12.50/).length).toBeGreaterThan(0);
  });

  it("shows the refresh cooldown from a 429 response", async () => {
    server.use(
      http.post("*/v1/admin/usage/refresh", () =>
        HttpResponse.json(
          {
            error: {
              code: "local_rate_limited",
              message: "Cooldown.",
              request_id: "req_cool",
              retryable: true,
              retry_after_ms: 45000,
              details: {},
            },
          },
          { status: 429 },
        ),
      ),
    );
    renderApp("/overview?view=usage");
    await userEvent.click(await screen.findByRole("button", { name: "Refresh all" }));
    expect(await screen.findByText(/Rate limited by Exta/)).toBeInTheDocument();
    expect(screen.getByText(/local_rate_limited, 45 s/)).toBeInTheDocument();
  });
});
