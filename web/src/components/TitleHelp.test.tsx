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
    await userEvent.click(within(operation.closest("tr")!).getByRole("button", { name: "编辑" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog.querySelector(".dialog-description")).toBeNull();
    expect(screen.queryByRole("tooltip")).toBeNull();
    const help = within(dialog).getByRole("button", { name: /关于编辑路由/ });
    help.focus();
    const tooltip = await screen.findByRole("tooltip");
    expect(dialog).toContainElement(tooltip);
    expect(tooltip).toHaveTextContent(/版本/);
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    expect(dialog).toBeInTheDocument();
    const input = within(dialog).getAllByRole("spinbutton")[0]!;
    input.focus();
    await userEvent.hover(help);
    expect(await screen.findByRole("tooltip")).toBeVisible();
    expect(input).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    expect(dialog).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("opens on focus, hover or click and dismisses without moving focus", async () => {
    renderApp("/access-keys");
    const help = await screen.findByRole("button", { name: "关于服务访问密钥" });
    const user = userEvent.setup();
    help.focus();
    expect(await screen.findByRole("tooltip")).toHaveTextContent("MCP 与 API 客户端");
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
