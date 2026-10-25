/** Shell: Exta branding, every management page reachable, Chinese-only text and focus. */
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import { renderApp } from "../test/renderApp";

beforeAll(async () => {
  await import("../features/overview/OverviewPage");
});

const PAGES = ["统计", "上游提供商密钥", "服务访问密钥", "调用日志", "设置"];

describe("application shell", () => {
  it("reaches every management page from the primary navigation", async () => {
    renderApp("/overview");
    expect(await screen.findByRole("heading", { name: "统计", level: 1 })).toBeInTheDocument();
    expect(screen.getByText("Exta", { selector: ".brand-name" })).toBeInTheDocument();
    const navigation = screen.getByRole("navigation", { name: "主导航" });
    expect(within(navigation).getAllByRole("link")).toHaveLength(5);
    for (const page of PAGES) {
      await userEvent.click(within(navigation).getByRole("link", { name: page }));
      expect(await screen.findByRole("heading", { name: page, level: 1 })).toBeInTheDocument();
      expect(within(navigation).getByRole("link", { name: page })).toHaveAttribute(
        "aria-current",
        "page",
      );
      await waitFor(() => expect(document.title).toBe(`${page} · Exta`));
    }
    expect(document.activeElement).toBe(screen.getByRole("main"));
  });

  it("presents only Simplified Chinese without a language control", async () => {
    window.localStorage.setItem("exta.locale", "en");
    renderApp("/overview");
    expect(await screen.findByRole("heading", { name: "统计", level: 1 })).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(screen.queryByRole("button", { name: /语言|Language/ })).toBeNull();
    expect(screen.getByRole("button", { name: "外观" })).toBeInTheDocument();
  });

  it("closes dialogs with Escape and returns focus to the opener", async () => {
    renderApp("/access-keys");
    const opener = await screen.findByRole("button", { name: "创建访问密钥" });
    await userEvent.click(opener);
    const dialog = screen.getByRole("dialog", { name: "创建访问密钥" });
    expect(within(dialog).getByLabelText("名称")).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(opener).toHaveFocus();
  });

  it("shows a not-found page for unknown routes inside the shell", async () => {
    renderApp("/nowhere");
    expect(await screen.findByRole("heading", { name: "页面不存在" })).toBeInTheDocument();
  });
  it.each(["/research", "/research/job_unknown", "/routing", "/usage", "/connections", "/audit"])(
    "does not expose a removed entry at %s",
    async (path) => {
      renderApp(path);
      expect(await screen.findByRole("heading", { name: "页面不存在" })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "创建研究" })).toBeNull();
    },
  );
});
