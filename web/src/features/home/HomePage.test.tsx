/** Product home: public entry, truthful static content and a session-aware console link. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { createMockState, mockDb } from "../../mocks/handlers";
import { renderApp } from "../../test/renderApp";

describe("product home", () => {
  it("opens the console directly for a signed-in administrator", async () => {
    renderApp("/");
    expect(
      await screen.findByRole("heading", {
        name: "One endpoint for Exa and Tavily search.",
        level: 1,
      }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Primary" })).toBeNull();
    const entry = (await screen.findAllByRole("link", { name: /Open console/ }))[0]!;
    await userEvent.click(entry);
    expect(
      await screen.findByRole("heading", { name: "Statistics", level: 1 }),
    ).toBeInTheDocument();
  });

  it("sends an anonymous visitor to sign in", async () => {
    mockDb.state = createMockState({ signedIn: false });
    renderApp("/");
    const entry = (await screen.findAllByRole("link", { name: /Sign in to console/ }))[0]!;
    await userEvent.click(entry);
    expect(await screen.findByLabelText("Administrator password")).toBeInTheDocument();
  });

  it("describes the implemented operations without loading console data", async () => {
    renderApp("/");
    const operations = await screen.findByRole("list", { name: "Six operations, two providers" });
    for (const operation of [
      "search",
      "advanced_search",
      "fetch",
      "map",
      "crawl",
      "research_create",
    ]) {
      expect(within(operations).getByText(operation)).toBeInTheDocument();
    }
    expect(screen.getByText(/not a third-party security certification/)).toBeInTheDocument();
    const requested = mockDb.state.calls.map((call) => call.path);
    expect(requested.every((path) => path === "/v1/admin/session")).toBe(true);
  });
});
