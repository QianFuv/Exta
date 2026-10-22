/** Shell: Exta branding, every management page reachable, language switch and focus. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderApp } from "../test/renderApp";

const PAGES: readonly string[] = [
  "Overview",
  "Key pools",
  "Routing",
  "Usage",
  "Requests",
  "Research",
  "Connections",
  "Settings",
  "Audit",
];

describe("application shell", () => {
  it("reaches every management page from the primary navigation", async () => {
    renderApp("/");
    expect(await screen.findByRole("heading", { name: "Overview", level: 1 })).toBeInTheDocument();
    expect(screen.getByText("Exta")).toBeInTheDocument();
    const navigation = screen.getByRole("navigation", { name: "Primary" });
    for (const page of PAGES) {
      await userEvent.click(within(navigation).getByRole("link", { name: page }));
      expect(await screen.findByRole("heading", { name: page, level: 1 })).toBeInTheDocument();
      expect(within(navigation).getByRole("link", { name: page })).toHaveAttribute(
        "aria-current",
        "page",
      );
      expect(document.title).toBe(`${page} · Exta`);
    }
    expect(document.activeElement).toBe(screen.getByRole("main"));
  });

  it("defaults to Chinese and switches to English", async () => {
    renderApp("/overview", { locale: "zh" });
    expect(await screen.findByRole("heading", { name: "概览", level: 1 })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "切换语言" }));
    expect(await screen.findByRole("heading", { name: "Overview", level: 1 })).toBeInTheDocument();
    expect(window.localStorage.getItem("exta.locale")).toBe("en");
    expect(document.documentElement.lang).toBe("en-US");
  });

  it("closes dialogs with Escape and returns focus to the opener", async () => {
    renderApp("/connections");
    const opener = await screen.findByRole("button", { name: "Create token" });
    await userEvent.click(opener);
    const dialog = screen.getByRole("dialog", { name: "Create token" });
    expect(within(dialog).getByLabelText("Label")).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(opener).toHaveFocus();
  });

  it("shows a not-found page for unknown routes inside the shell", async () => {
    renderApp("/nowhere");
    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument();
  });
});
