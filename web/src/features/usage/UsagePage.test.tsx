/** Usage: separated units, unlimited vs unknown, permission gaps, divergence and cooldown. */
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { renderApp } from "../../test/renderApp";
import { lastSuccessfulObservation } from "./UsagePage";
import { mockCallTrends } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { server } from "../../test/server";

describe("usage", () => {
  it("never presents a failed attempt or local ledger timestamp as a successful refresh", () => {
    const item = mockDb.state.usage.items.find(
      (entry) => entry.source === "upstream" && entry.unavailable_reason === null,
    )!;
    const later = "2030-01-01T00:00:00Z";
    expect(
      lastSuccessfulObservation([
        item,
        { ...item, source: "local", observed_at: later },
        { ...item, unavailable_reason: "usage_unavailable", observed_at: later },
      ]),
    ).toBe(item.observed_at);
    expect(lastSuccessfulObservation([{ ...item, freshness: "unknown" }])).toBeNull();
  });
  it("renders call counts and an accessible table without money or balance figures", async () => {
    renderApp("/overview?view=usage");
    expect(await screen.findByRole("img", { name: /Call trends.*Total: 3/ })).toBeInTheDocument();
    expect(screen.queryByText(/640 credits|\$12.50|Limit unknown/)).toBeNull();
    await userEvent.click(screen.getByText("View count data"));
    expect(await screen.findByRole("region", { name: "View count data" })).toBeInTheDocument();
  });

  it("keeps zero axes with an explicit empty state", async () => {
    const data = mockCallTrends();
    server.use(
      http.get("*/v1/admin/usage/trends", () =>
        HttpResponse.json({
          ...data,
          buckets: data.buckets.map((bucket) => ({ ...bucket, total: 0, succeeded: 0, failed: 0 })),
        }),
      ),
    );
    renderApp("/overview?view=usage");
    expect(await screen.findByText("No calls in this period")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /Call trends.*Total: 0/ })).toBeInTheDocument();
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
