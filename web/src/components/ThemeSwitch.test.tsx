/** Appearance preferences must never depend on API calls or available browser storage. */
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderApp } from "../test/renderApp";
import { mockDb } from "../mocks/handlers";

describe("appearance", () => {
  it("offers one appearance menu and no language choice before login", async () => {
    mockDb.state.session = null;
    renderApp("/login");
    const theme = await screen.findByRole("button", { name: "外观" });
    expect(screen.queryByRole("button", { name: "语言" })).toBeNull();
    expect(screen.queryByRole("button", { name: "偏好设置" })).toBeNull();
    await userEvent.click(theme);
    expect(screen.queryByRole("menuitem", { name: "English" })).toBeNull();
    await userEvent.click(screen.getByRole("menuitem", { name: "深色" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(theme).toHaveFocus();
  });
  it("changes the document and persists repeated explicit and system choices", async () => {
    renderApp("/overview");
    const control = await screen.findByRole("button", { name: "外观" });
    for (const theme of ["dark", "light", "system", "dark"]) {
      await userEvent.click(control);
      await userEvent.click(
        screen.getByRole("menuitem", {
          name: theme === "system" ? "跟随系统" : theme === "dark" ? "深色" : "浅色",
        }),
      );
      expect(document.documentElement.dataset.theme).toBe(theme);
      expect(control).toHaveAttribute("aria-expanded", "false");
      expect(control).toHaveFocus();
      expect(localStorage.getItem("exta.theme")).toBe(theme);
      await userEvent.click(control);
      const selected = screen.getByRole("menuitem", {
        name: theme === "system" ? "跟随系统" : theme === "dark" ? "深色" : "浅色",
      });
      expect(selected.querySelector(".semi-icon-tick")).not.toBeNull();
      expect(selected.querySelector(".semi-icon-tick")).not.toHaveStyle({ color: "transparent" });
      expect(selected).toHaveAttribute("aria-current", "true");
      expect(screen.queryByRole("tooltip", { name: "外观" })).not.toBeInTheDocument();
      await userEvent.keyboard("{Escape}");
    }
  });

  it("applies a session preference and explains failed persistence", async () => {
    renderApp("/overview");
    const control = await screen.findByRole("button", { name: "外观" });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("Storage unavailable");
    });
    await userEvent.click(control);
    await userEvent.click(screen.getByRole("menuitem", { name: "浅色" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(screen.getByText("外观仅在本次会话生效。")).toHaveAttribute("role", "status");
  });
});
