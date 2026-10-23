/** Optional explanations remain available to pointer, keyboard and touch users. */
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderApp } from "../test/renderApp";

describe("heading help", () => {
  it("opens on focus, hover or click and dismisses without moving focus", async () => {
    renderApp("/access-keys");
    const help = await screen.findByRole("button", { name: "About Service access keys" });
    const user = userEvent.setup();
    help.focus();
    expect(await screen.findByRole("tooltip")).toHaveTextContent("MCP and API clients");
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    expect(help).toHaveFocus();
    await user.click(help);
    expect(await screen.findByRole("tooltip")).toBeInTheDocument();
    await user.click(screen.getByRole("heading", { level: 1 }));
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    await user.hover(help);
    expect(await screen.findByRole("tooltip")).toBeInTheDocument();
    await user.unhover(help);
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
  });
});
