/** Exercise the installed calendar rather than substituting a callback mock. */
import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { Input } from "./Controls";

/** Keep the same string-valued draft used by access-key and log forms. */
function DateFixture() {
  const [value, setValue] = useState("2028-06-15T10:30");
  return (
    <Input
      aria-label="Expiry"
      type="datetime-local"
      value={value}
      onChange={(event) => setValue(event.target.value)}
    />
  );
}

it("retains a calendar-selected 2028 date and supports clear then reselect", async () => {
  render(<DateFixture />);
  const input = screen.getByRole("textbox", { name: "Expiry" });
  await userEvent.click(input);
  const day = await screen.findByRole("gridcell", { name: "2028-06-20" });
  await userEvent.click(day);
  expect(input).toHaveValue("2028-06-20 10:30");
  await userEvent.clear(input);
  expect(input).toHaveValue("");
  await userEvent.type(input, "2028-06-15 10:30");
  await userEvent.click(await screen.findByRole("gridcell", { name: "2028-06-20" }));
  expect(input).toHaveValue("2028-06-20 10:30");
});
