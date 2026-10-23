/** Shell: Exta branding, every management page reachable, language switch and focus. */
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import { renderApp } from "../test/renderApp";

beforeAll(async () => {
  await import("../features/overview/OverviewPage");
});

const PAGES = [
  "Statistics",
  "Upstream provider keys",
  "Service access keys",
  "Call logs",
  "Settings",
];

describe("application shell", () => {
  it("reaches every management page from the primary navigation", async () => {
    renderApp("/");
    expect(
      await screen.findByRole("heading", { name: "Statistics", level: 1 }),
    ).toBeInTheDocument();
    expect(screen.getByText("Exta", { selector: ".brand-name" })).toBeInTheDocument();
    const navigation = screen.getByRole("navigation", { name: "Primary" });
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

  it("defaults to Chinese and switches to English", async () => {
    renderApp("/overview", { locale: "zh" });
    expect(await screen.findByRole("heading", { name: "统计", level: 1 })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "偏好设置" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "English" }));
    expect(
      await screen.findByRole("heading", { name: "Statistics", level: 1 }),
    ).toBeInTheDocument();
    expect(window.localStorage.getItem("exta.locale")).toBe("en");
    expect(document.documentElement.lang).toBe("en-US");
  });

  it("closes dialogs with Escape and returns focus to the opener", async () => {
    renderApp("/access-keys");
    const opener = await screen.findByRole("button", { name: "Create access key" });
    await userEvent.click(opener);
    const dialog = screen.getByRole("dialog", { name: "Create access key" });
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
