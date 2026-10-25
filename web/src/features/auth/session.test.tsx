/** Session lifecycle: memory-only credentials, expiry, logout and lookup failures. */
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { mockSession } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { allBrowserStorage, callsTo, renderApp } from "../../test/renderApp";
import { server } from "../../test/server";

describe("admin session", () => {
  it("counts administrator password Unicode characters without truncating exact input", async () => {
    mockDb.state.session = null;
    renderApp("/login");
    const field = await screen.findByLabelText("管理员密码");
    const submit = screen.getByRole("button", { name: "登录" });
    for (const character of ["a", "界", "🔐"]) {
      for (const length of [7, 8, 10, 512, 513]) {
        const password = character.repeat(length);
        fireEvent.change(field, { target: { value: password } });
        expect(field).toHaveValue(password);
        if (length >= 8 && length <= 512) expect(submit).toBeEnabled();
        else expect(submit).toBeDisabled();
      }
    }
    expect(callsTo("POST", "/v1/admin/session")).toHaveLength(0);
    mockDb.state.validCredential = "🔐".repeat(8);
    fireEvent.change(field, { target: { value: mockDb.state.validCredential } });
    await userEvent.click(submit);
    expect(await screen.findByRole("heading", { name: "统计", level: 1 })).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/session")).toHaveLength(1);
  });

  it("signs in without persisting the credential or CSRF token", async () => {
    mockDb.state.session = null;
    const { router, queryClient } = renderApp("/requests");
    const field = await screen.findByLabelText("管理员密码");
    expect(router.state.location.pathname).toBe("/login");

    await userEvent.type(field, "wrong-credential-value");
    await userEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(await screen.findByText("凭据未被接受。")).toBeInTheDocument();
    expect(field).toHaveValue("");
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((entry) => entry.state),
      ),
    ).not.toContain("wrong-credential-value");

    await userEvent.type(field, mockDb.state.validCredential);
    await userEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(await screen.findByRole("heading", { name: "调用日志", level: 1 })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/requests");

    const stored = allBrowserStorage();
    expect(stored).not.toContain(mockDb.state.validCredential);
    expect(stored).not.toContain(mockSession().csrf_token);
  });

  it("returns to login with an explanation when the session expires mid-use", async () => {
    const { router } = renderApp("/overview");
    expect(await screen.findByRole("heading", { name: "统计", level: 1 })).toBeInTheDocument();
    mockDb.state.session = null;
    await userEvent.click(
      within(screen.getByRole("navigation", { name: "主导航" })).getByRole("link", {
        name: "上游提供商密钥",
      }),
    );
    expect(await screen.findByText(/会话已结束/)).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("signs out with the CSRF header", async () => {
    renderApp("/overview");
    await userEvent.click(await screen.findByRole("button", { name: "退出登录" }));
    expect(await screen.findByText("已退出登录。")).toBeInTheDocument();
    const calls = callsTo("DELETE", "/v1/admin/session");
    expect(calls).toHaveLength(1);
    expect(calls[0]?.headers["x-exta-csrf"]).toBe(mockSession().csrf_token);
  });

  it("shows an error, not the login page, when the session lookup fails for another reason", async () => {
    server.use(
      http.get("*/v1/admin/session", () =>
        HttpResponse.json(
          {
            error: {
              code: "storage_unavailable",
              message: "Database unavailable.",
              request_id: "req_s",
              retryable: true,
              retry_after_ms: null,
              details: {},
            },
          },
          { status: 503 },
        ),
      ),
    );
    renderApp("/overview");
    expect(await screen.findByText("无法获取会话状态")).toBeInTheDocument();
    expect(screen.getByText("存储不可用。")).toBeInTheDocument();
    expect(screen.queryByLabelText("管理员密码")).toBeNull();
  });
});
