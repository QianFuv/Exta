/** Session lifecycle: memory-only credentials, expiry, logout and lookup failures. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { mockSession } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { allBrowserStorage, callsTo, renderApp } from "../../test/renderApp";
import { server } from "../../test/server";

describe("admin session", () => {
  it("signs in without persisting the credential or CSRF token", async () => {
    mockDb.state.session = null;
    const { router, queryClient } = renderApp("/requests");
    const field = await screen.findByLabelText("Administrator password");
    expect(router.state.location.pathname).toBe("/login");

    await userEvent.type(field, "wrong-credential-value");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("The credential was not accepted.")).toBeInTheDocument();
    expect(field).toHaveValue("");
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((entry) => entry.state),
      ),
    ).not.toContain("wrong-credential-value");

    await userEvent.type(field, mockDb.state.validCredential);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("heading", { name: "Call logs", level: 1 })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/requests");

    const stored = allBrowserStorage();
    expect(stored).not.toContain(mockDb.state.validCredential);
    expect(stored).not.toContain(mockSession().csrf_token);
  });

  it("returns to login with an explanation when the session expires mid-use", async () => {
    const { router } = renderApp("/overview");
    expect(
      await screen.findByRole("heading", { name: "Statistics", level: 1 }),
    ).toBeInTheDocument();
    mockDb.state.session = null;
    await userEvent.click(
      within(screen.getByRole("navigation", { name: "Primary" })).getByRole("link", {
        name: "Upstream provider keys",
      }),
    );
    expect(await screen.findByText(/Your session ended/)).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("signs out with the CSRF header", async () => {
    renderApp("/overview");
    await userEvent.click(await screen.findByRole("button", { name: "Preferences" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Sign out" }));
    expect(await screen.findByText("You signed out.")).toBeInTheDocument();
    const calls = callsTo("DELETE", "/v1/admin/session");
    expect(calls).toHaveLength(1);
    expect(calls[0]?.headers["x-exta-csrf"]).toBe(mockSession().csrf_token);
  });

  it("shows an error, not the login page, when the session lookup fails for another reason", async () => {
    server.use(
      http.get("*/v1/admin/session", () =>
        HttpResponse.json(
          {
            error: {
              code: "storage_unavailable",
              message: "Database unavailable.",
              request_id: "req_s",
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
    expect(await screen.findByText("Session status is unavailable")).toBeInTheDocument();
    expect(screen.getByText("Storage is unavailable.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Administrator password")).toBeNull();
  });
});
