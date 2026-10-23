/** Appearance preferences must never depend on API calls or available browser storage. */
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderApp } from "../test/renderApp";

describe("appearance", () => {
  it("changes the document and persists repeated explicit and system choices", async () => {
    renderApp("/overview");
    const control = await screen.findByRole("combobox", { name: "Appearance" });
    for (const theme of ["dark", "light", "system", "dark"]) {
      await userEvent.selectOptions(control, theme);
      expect(document.documentElement.dataset.theme).toBe(theme);
      expect(localStorage.getItem("exta.theme")).toBe(theme);
    }
  });

  it("applies a session preference and explains failed persistence", async () => {
    renderApp("/overview");
    const control = await screen.findByRole("combobox", { name: "Appearance" });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("Storage unavailable");
    });
    await userEvent.selectOptions(control, "light");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(screen.getByText("Appearance applies to this session only.")).toHaveAttribute(
      "role",
      "status",
    );
  });
});
