/** Connections: placeholder examples, OAuth disclosure and one-time token reveal. */
import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { mockDb } from "../../mocks/handlers";
import { allBrowserStorage, callsTo, renderApp } from "../../test/renderApp";

const ONE_TIME_VALUE = "exta_fixture_one_time_value_0001";

describe("connections and tokens", () => {
  it("rechecks expiry at submission after a dialog has remained open", async () => {
    renderApp("/access-keys");
    await userEvent.click(await screen.findByRole("button", { name: "创建访问密钥" }));
    const dialog = within(screen.getByRole("dialog"));
    await userEvent.type(dialog.getByLabelText("名称"), "Expiry boundary");
    const future = Date.now() + 60_000;
    const local = new Date(future - new Date(future).getTimezoneOffset() * 60_000)
      .toISOString()
      .slice(0, 16)
      .replace("T", " ");
    await userEvent.clear(dialog.getByLabelText("到期时间"));
    await userEvent.type(dialog.getByLabelText("到期时间"), local);
    const clock = vi.spyOn(Date, "now").mockReturnValue(future + 60_000);
    try {
      await userEvent.click(dialog.getByRole("button", { name: "创建访问密钥" }));
      expect(dialog.getByText("请选择未来的到期时间。")).toBeInTheDocument();
      expect(callsTo("POST", "/v1/admin/tokens")).toHaveLength(0);
    } finally {
      clock.mockRestore();
    }
  });
  it("explains blocked capabilities and labels access scopes without hiding protocol codes", async () => {
    const capability = mockDb.state.connectionInfo.capabilities.capabilities[0];
    if (!capability) throw new Error("Missing capability fixture");
    capability.enabled = false;
    capability.unavailable_reason = "No eligible configured route";
    const connection = renderApp("/settings?view=connection");
    expect(await screen.findByText("尚无符合条件的路由。")).toBeInTheDocument();
    connection.unmount();
    renderApp("/access-keys");
    const scopes = await screen.findAllByTitle("read");
    expect(scopes[0]).toHaveTextContent("读取元信息");
    expect(scopes[0]).toHaveAttribute("aria-label", "读取元信息（read）");
  });
  it("explains invalid token fields without dispatch and focuses the field", async () => {
    renderApp("/access-keys");
    await userEvent.click(await screen.findByRole("button", { name: "创建访问密钥" }));
    const dialog = within(screen.getByRole("dialog"));
    await userEvent.click(dialog.getByRole("button", { name: "创建访问密钥" }));
    expect(dialog.getByLabelText("名称")).toHaveFocus();
    expect(dialog.getByText("请填写访问密钥名称。")).toBeInTheDocument();
    await userEvent.type(dialog.getByLabelText("名称"), "Offline client");
    await userEvent.click(dialog.getByRole("checkbox", { name: "读取元信息 read" }));
    await userEvent.click(dialog.getByRole("button", { name: "创建访问密钥" }));
    expect(dialog.getByText("请至少选择一个作用域。")).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/tokens")).toHaveLength(0);
  });
  it("updates token expiry while the page remains mounted", async () => {
    const token = mockDb.state.tokens[0];
    if (!token) throw new Error("Missing token fixture");
    token.expires_at = new Date(Date.now() + 60000).toISOString();
    token.revoked_at = null;
    mockDb.state.tokens = [token];
    renderApp("/access-keys");
    await screen.findByText("有效");
    vi.useFakeTimers();
    try {
      vi.setSystemTime(Date.now() + 61000);
      act(() => document.dispatchEvent(new Event("visibilitychange")));
      expect(screen.getByText("已过期")).toBeInTheDocument();
      expect(screen.queryByText("有效")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
  it("shows endpoints, protocol versions and the OAuth-only limitation", async () => {
    renderApp("/settings?view=connection");
    expect(await screen.findByText("http://127.0.0.1:8789/mcp")).toBeInTheDocument();
    expect(screen.getByText("2026-07-28")).toBeInTheDocument();
    expect(screen.getByText("不支持仅 OAuth 的客户端")).toBeInTheDocument();
    expect(screen.getByText(/<EXTA_TOKEN>/)).toBeInTheDocument();
    expect(screen.getByText("仅回环地址")).toBeInTheDocument();
  });

  it("reveals a minted token once and clears it on dismissal and reload", async () => {
    const first = renderApp("/access-keys");
    await userEvent.click(await screen.findByRole("button", { name: "创建访问密钥" }));
    const dialog = screen.getByRole("dialog", { name: "创建访问密钥" });
    await userEvent.type(within(dialog).getByLabelText("名称"), "CI runner");
    await userEvent.click(within(dialog).getByRole("checkbox", { name: "读取研究 research:read" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "创建访问密钥" }));

    expect(await screen.findByText(ONE_TIME_VALUE)).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/tokens")[0]?.body).toMatchObject({
      label: "CI runner",
      scopes: ["read", "research:read"],
    });
    expect(allBrowserStorage()).not.toContain(ONE_TIME_VALUE);

    await userEvent.click(screen.getByRole("button", { name: "我已复制" }));
    expect(screen.queryByText(ONE_TIME_VALUE)).toBeNull();

    first.unmount();
    renderApp("/access-keys");
    expect(await screen.findByText("CI runner")).toBeInTheDocument();
    expect(screen.queryByText(ONE_TIME_VALUE)).toBeNull();
  });

  it("explains that clients cannot connect when no token is active", async () => {
    mockDb.state.tokens = [];
    renderApp("/access-keys");
    expect(await screen.findByText("没有服务访问密钥")).toBeInTheDocument();
  });
});
