/** Appearance preferences must never depend on API calls or available browser storage. */
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderApp } from "../test/renderApp";

describe("appearance", () => {
  it("changes the document and persists repeated explicit and system choices", async () => {
    renderApp("/overview");
    const control = await screen.findByRole("button", { name: "Preferences" });
    for (const theme of ["dark", "light", "system", "dark"]) {
      await userEvent.click(control);
      await userEvent.click(
        screen.getByRole("menuitem", {
          name: theme === "system" ? "System" : theme === "dark" ? "Dark" : "Light",
        }),
      );
      expect(document.documentElement.dataset.theme).toBe(theme);
      expect(control).toHaveAttribute("aria-expanded", "false");
      expect(control).toHaveFocus();
      expect(localStorage.getItem("exta.theme")).toBe(theme);
      await userEvent.click(control);
      const selected = screen.getByRole("menuitem", {
        name: theme === "system" ? "System" : theme === "dark" ? "Dark" : "Light",
      });
      expect(selected.querySelector(".semi-icon-tick")).not.toBeNull();
      expect(selected.querySelector(".semi-icon-tick")).not.toHaveStyle({ color: "transparent" });
      expect(selected).toHaveAttribute("aria-current", "true");
      expect(screen.queryByRole("tooltip", { name: "Preferences" })).not.toBeInTheDocument();
      await userEvent.keyboard("{Escape}");
    }
  });

  it("applies a session preference and explains failed persistence", async () => {
    renderApp("/overview");
    const control = await screen.findByRole("button", { name: "Preferences" });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("Storage unavailable");
    });
    await userEvent.click(control);
    await userEvent.click(screen.getByRole("menuitem", { name: "Light" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(screen.getByText("Appearance applies to this session only.")).toHaveAttribute(
      "role",
      "status",
    );
  });
});
