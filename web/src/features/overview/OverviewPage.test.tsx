/** Overview: no fabricated health; locked/no-key/stale and failure states are explicit. */
import userEvent from "@testing-library/user-event";
import { screen, within, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { mockReadiness } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { callsTo, renderApp } from "../../test/renderApp";
import { server } from "../../test/server";

describe("overview", () => {
  it("combines recent calls and errors without duplicating records", async () => {
    const record = mockDb.state.requests[0]!.request;
    let selectedStatus: string | null = null;
    server.use(
      http.get("*/v1/admin/requests", ({ request }) => {
        selectedStatus = new URL(request.url).searchParams.get("status");
        return HttpResponse.json({
          items: Array.from({ length: 8 }, (_, index) => ({ ...record, id: `request_${index}` })),
          next_cursor: null,
        });
      }),
    );
    renderApp("/overview");
    const recent = await screen.findByRole("region", { name: "Recent calls" }, { timeout: 15_000 });
    expect(within(recent).getAllByRole("row")).toHaveLength(6);
    expect(screen.getAllByRole("link", { name: /View all calls/ })).toHaveLength(1);
    expect(screen.queryByRole("heading", { name: "Recent errors" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Recent errors" }));
    await waitFor(() => expect(selectedStatus).toBe("failed"));
    expect(screen.getAllByRole("link", { name: /View all calls/ })).toHaveLength(1);
  });

  it("shows locked secret store and missing keys without unrelated usage banners", async () => {
    mockDb.state.settings = {
      ...mockDb.state.settings,
      readiness: mockReadiness({ ready: false, secret_store: "locked" }),
    };
    mockDb.state.keys = [];
    renderApp("/overview");
    expect(await screen.findByText("Secret store locked")).toBeInTheDocument();
    expect(screen.getByText("Not ready").closest(".semi-tag")).toHaveClass("status-danger");
    expect(await screen.findByText("No provider keys configured")).toBeInTheDocument();
    expect(screen.queryByText(/usage rows are stale/)).toBeNull();
    expect(screen.getByText("Uncertain research")).toBeInTheDocument();
  });

  it("never reports readiness when settings cannot be loaded", async () => {
    server.use(
      http.get("*/v1/admin/settings", () =>
        HttpResponse.json(
          {
            error: {
              code: "storage_unavailable",
              message: "Database unavailable.",
              request_id: "req_db",
              retryable: true,
              retry_after_ms: null,
              details: {},
            },
          },
          { status: 503 },
        ),
      ),
    );
    renderApp("/overview");
    expect(await screen.findByText("Storage is unavailable.")).toBeInTheDocument();
    expect(screen.queryByText("Ready")).toBeNull();
  });

  it("shows stored calls without mounting research execution or polling", async () => {
    renderApp("/overview");
    expect(await screen.findByText("Recent calls")).toBeInTheDocument();
    expect(callsTo("GET", "/v1/admin/research")).toHaveLength(0);
    expect(screen.queryByRole("link", { name: "Research" })).toBeNull();
  });
});
