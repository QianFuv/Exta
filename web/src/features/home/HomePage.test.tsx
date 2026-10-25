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
        name: "一个入口，统一调度 Exa 与 Tavily 检索",
        level: 1,
      }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "主导航" })).toBeNull();
    const entry = (await screen.findAllByRole("link", { name: /进入控制台/ }))[0]!;
    await userEvent.click(entry);
    expect(await screen.findByRole("heading", { name: "统计", level: 1 })).toBeInTheDocument();
  });

  it("sends an anonymous visitor to sign in", async () => {
    mockDb.state = createMockState({ signedIn: false });
    renderApp("/");
    const entry = (await screen.findAllByRole("link", { name: /登录控制台/ }))[0]!;
    await userEvent.click(entry);
    expect(await screen.findByLabelText("管理员密码")).toBeInTheDocument();
  });

  it("describes the implemented operations without loading console data", async () => {
    renderApp("/");
    const operations = await screen.findByRole("list", { name: "六类操作，两家提供商" });
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
    expect(screen.getByText(/并非第三方安全认证/)).toBeInTheDocument();
    const requested = mockDb.state.calls.map((call) => call.path);
    expect(requested.every((path) => path === "/v1/admin/session")).toBe(true);
  });
});
