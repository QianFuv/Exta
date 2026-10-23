/** Shell: Exta branding, every management page reachable, language switch and focus. */
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderApp } from "../test/renderApp";

const PAGES = ["Statistics", "Keys", "Call logs", "Settings"];

describe("application shell", () => {
  it("reaches every management page from the primary navigation", async () => {
    renderApp("/");
    expect(
      await screen.findByRole("heading", { name: "Statistics", level: 1 }),
    ).toBeInTheDocument();
    expect(screen.getByText("Exta", { selector: ".brand-name" })).toBeInTheDocument();
    const navigation = screen.getByRole("navigation", { name: "Primary" });
    expect(within(navigation).getAllByRole("link")).toHaveLength(4);
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

  it("defaults to Chinese and switches to English", async () => {
    renderApp("/overview", { locale: "zh" });
    expect(await screen.findByRole("heading", { name: "统计", level: 1 })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "切换语言: English" }));
    expect(screen.getByRole("button", { name: "Switch language: 中文" })).toHaveTextContent("中文");
    expect(
      await screen.findByRole("heading", { name: "Statistics", level: 1 }),
    ).toBeInTheDocument();
    expect(window.localStorage.getItem("exta.locale")).toBe("en");
    expect(document.documentElement.lang).toBe("en-US");
  });

  it("closes dialogs with Escape and returns focus to the opener", async () => {
    renderApp("/keys?view=access");
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
  it.each(["/research", "/research/job_unknown", "/routing", "/usage", "/connections", "/audit"])(
    "does not expose a removed entry at %s",
    async (path) => {
      renderApp(path);
      expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Create research" })).toBeNull();
    },
  );
});
