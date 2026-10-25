/** Presets never narrow the exact input's legal range. */
import { useState } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { I18nProvider } from "../lib/i18n";
import { NumericControl } from "./NumericControl";

/** Model a local cap with a much wider legal range than its common presets. */
function NumericFixture() {
  const [value, setValue] = useState("0");
  return (
    <I18nProvider>
      <label id="limit-label" htmlFor="limit">
        Limit
      </label>
      <NumericControl
        id="limit"
        value={value}
        onChange={setValue}
        minimum={0}
        maximum={Number.MAX_SAFE_INTEGER}
        presets={[0, 10, 100]}
        zeroLabel="本地不限"
        emptyIsZero
      />
      <output data-testid="draft">{value}</output>
    </I18nProvider>
  );
}

it("synchronizes marks, keyboard and exact values beyond the visible slider", async () => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 100,
    bottom: 20,
    width: 100,
    height: 20,
    toJSON: () => ({}),
  });
  render(<NumericFixture />);
  expect(screen.getByText("0", { selector: ".semi-slider-mark" })).toBeVisible();
  expect(screen.getByRole("slider", { name: "Limit" })).toHaveAttribute(
    "aria-valuetext",
    "本地不限",
  );
  const number = screen.getByRole("spinbutton", { name: "Limit" });
  fireEvent.click(screen.getByText("10", { selector: ".semi-slider-mark" }), {
    clientX: 10,
    clientY: 0,
  });
  expect(screen.getByTestId("draft")).toHaveTextContent("10");
  const slider = screen.getByRole("slider", { name: "Limit" });
  slider.focus();
  await userEvent.keyboard("{ArrowRight}");
  expect(screen.getByTestId("draft")).toHaveTextContent("11");
  await userEvent.clear(number);
  expect(number).toHaveAttribute("aria-invalid", "false");
  await userEvent.type(number, "2500000");
  await userEvent.tab();
  expect(screen.getByTestId("draft")).toHaveTextContent("2500000");
  expect(slider).toHaveAttribute("aria-valuetext", "2,500,000");
});
