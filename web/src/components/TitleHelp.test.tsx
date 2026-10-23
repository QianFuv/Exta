/** Optional explanations remain available to pointer, keyboard and touch users. */
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import { renderApp } from "../test/renderApp";

beforeAll(async () => {
  await import("../features/connections/AccessKeysPage");
});

describe("heading help", () => {
  it("keeps route explanations inside dialog help and dismisses help before the dialog", async () => {
    renderApp("/settings?view=routing");
    const operation = await screen.findByText("advanced_search", {
      selector: "td .operation-name",
    });
    await userEvent.click(within(operation.closest("tr")!).getByRole("button", { name: "Edit" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog.querySelector(".dialog-description")).toBeNull();
    expect(screen.queryByRole("tooltip")).toBeNull();
    const help = within(dialog).getByRole("button", { name: /About Edit route/ });
    help.focus();
    const tooltip = await screen.findByRole("tooltip");
    expect(dialog).toContainElement(tooltip);
    expect(tooltip).toHaveTextContent(/revision/i);
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    expect(dialog).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

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
