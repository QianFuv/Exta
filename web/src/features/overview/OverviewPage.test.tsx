/** Overview: no fabricated health; locked/no-key/stale and failure states are explicit. */
import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { mockReadiness } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { renderApp } from "../../test/renderApp";
import { server } from "../../test/server";

describe("overview", () => {
  it("shows locked secret store, missing keys and stale usage", async () => {
    mockDb.state.settings = {
      ...mockDb.state.settings,
      readiness: mockReadiness({ ready: false, secret_store: "locked" }),
    };
    mockDb.state.keys = [];
    renderApp("/overview");
    expect(await screen.findByText("Secret store locked")).toBeInTheDocument();
    expect(screen.getByText("Not ready")).toHaveClass("badge-danger");
    expect(await screen.findByText("No provider keys configured")).toBeInTheDocument();
    expect(await screen.findByText(/usage rows are stale/)).toBeInTheDocument();
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

  it("lists research that needs attention", async () => {
    renderApp("/overview");
    expect(await screen.findByText("job_unknown")).toBeInTheDocument();
    expect(screen.getByText("job_expired")).toBeInTheDocument();
    expect(screen.queryByText("job_done")).toBeNull();
  });
});
