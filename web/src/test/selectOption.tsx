/** Exercise the visible Semi selection popup rather than a native select implementation. */
import userEvent from "@testing-library/user-event";
import { screen } from "@testing-library/react";

/** Choose the named accessible option through the same interaction used by operators. */
export async function selectOption(control: HTMLElement, label: string) {
  await userEvent.click(control);
  await userEvent.click(await screen.findByRole("option", { name: label }));
}
