/** Key pools: grouping, masked keys, write-only secrets and version conflicts. */
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { server } from "../../test/server";
import { beforeAll, describe, expect, it } from "vitest";
import { ALL_PERMISSIONS, mockSession } from "../../mocks/fixtures";
import { mockDb } from "../../mocks/handlers";
import { allBrowserStorage, callsTo, renderApp } from "../../test/renderApp";

beforeAll(async () => {
  await import("./KeysPage");
});

describe("key pools", () => {
  it("explains a backend duplicate rejection without exposing the submitted secret", async () => {
    server.use(
      http.post("*/v1/admin/keys", () =>
        HttpResponse.json(
          {
            error: {
              code: "invalid_request",
              message: "Credential already exists",
              request_id: "req_duplicate",
              retryable: false,
              retry_after_ms: null,
              details: { reason: "duplicate_credential" },
            },
          },
          { status: 400 },
        ),
      ),
    );
    renderApp("/keys");
    await userEvent.click(await screen.findByRole("button", { name: "添加密钥" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "添加密钥 · Exa" }));
    const dialog = screen.getByRole("dialog");
    await userEvent.type(within(dialog).getByLabelText("名称"), "Duplicate fixture");
    await userEvent.type(within(dialog).getByLabelText("密钥内容"), "synthetic-duplicate-secret");
    await userEvent.click(within(dialog).getByRole("button", { name: "添加密钥" }));
    expect(await screen.findByText("此密钥已存在，无法重复添加。")).toBeVisible();
    expect(within(dialog).getByLabelText("密钥内容")).toHaveValue("");
    expect(allBrowserStorage()).not.toContain("synthetic-duplicate-secret");
  });

  it("shows disabled state once and keeps the edit switch label independent of its value", async () => {
    mockDb.state.keys.find((entry) => entry.id === "key_exa_1")!.enabled = false;
    renderApp("/keys");
    const row = within((await screen.findByText("Exa primary")).closest("tr")!);
    expect(row.getAllByText("已停用")).toHaveLength(1);
    await userEvent.click(row.getByRole("button", { name: "Exa primary 的更多操作" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "编辑密钥与限额" }));
    const dialog = within(screen.getByRole("dialog"));
    const toggle = dialog.getByRole("switch", { name: "启用此密钥" });
    expect(toggle).not.toBeChecked();
    expect(dialog.queryByText("已启用")).toBeNull();
    await userEvent.click(toggle);
    expect(toggle).toBeChecked();
    expect(dialog.getByRole("switch", { name: "启用此密钥" })).toBe(toggle);
  });
  it("does not label an enabled but blocked key as usable", async () => {
    const key = mockDb.state.keys.find((entry) => entry.id === "key_exa_1")!;
    key.enabled = true;
    key.billing_state = "blocked";
    renderApp("/keys");
    const row = within((await screen.findByText("Exa primary")).closest("tr")!);
    expect(row.getByText("受阻")).toBeInTheDocument();
    expect(row.getByText("计费受阻")).toBeInTheDocument();
    expect(row.queryByText("已启用")).toBeNull();
    expect(row.getByRole("button", { name: "停用 Exa primary" })).toBeEnabled();
  });
  it.each(["create", "rotate"])("clears failed %s secrets from mutation state", async (action) => {
    server.use(
      http.post(action === "create" ? "*/v1/admin/keys" : "*/v1/admin/keys/:id/rotate", () =>
        HttpResponse.json(
          {
            error: {
              code: "invalid_request",
              message: "Rejected",
              request_id: "req_secret",
              retryable: false,
              retry_after_ms: null,
              details: {},
            },
          },
          { status: 400 },
        ),
      ),
    );
    const { queryClient } = renderApp("/keys");
    await screen.findByText("Exa primary");
    await userEvent.click(
      action === "create"
        ? screen.getByRole("button", { name: "添加密钥" })
        : screen.getByRole("button", { name: "Exa primary 的更多操作" }),
    );
    await userEvent.click(
      screen.getByRole("menuitem", { name: action === "create" ? "添加密钥 · Exa" : "轮换" }),
    );
    const dialog = screen.getByRole("dialog");
    if (action === "create")
      await userEvent.type(within(dialog).getByLabelText("名称"), "Rejected key");
    await userEvent.type(within(dialog).getByLabelText("密钥内容"), "synthetic-rejected-secret");
    await userEvent.click(
      within(dialog).getByRole("button", { name: action === "create" ? "添加密钥" : "轮换" }),
    );
    await within(dialog).findByText(/invalid_request/);
    expect(within(dialog).getByLabelText("密钥内容")).toHaveValue("");
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((entry) => entry.state),
      ),
    ).not.toContain("synthetic-rejected-secret");
  });
  it("keeps secondary actions behind a disclosure and requires retirement confirmation", async () => {
    renderApp("/keys");
    const row = (await screen.findByText("Exa primary")).closest("tr")!;
    const summary = within(row).getByRole("button", { name: /的更多操作/ });
    expect(screen.queryByRole("menuitem", { name: "退役" })).toBeNull();
    for (let attempt = 0; attempt < 2; attempt += 1) {
      summary.focus();
      await userEvent.keyboard(attempt === 0 ? "{Enter}" : " ");
      await userEvent.click(screen.getByRole("menuitem", { name: "退役" }));
      const dialog = screen.getByRole("dialog", { name: "退役 Exa primary" });
      expect(callsTo("POST", "/v1/admin/keys/key_exa_1/retire")).toHaveLength(0);
      await userEvent.click(within(dialog).getByRole("button", { name: "取消" }));
      expect(summary).toHaveFocus();
      expect(summary).toHaveAttribute("aria-expanded", "false");
    }
    await userEvent.click(summary);
    await userEvent.keyboard("{Escape}");
    expect(summary).toHaveAttribute("aria-expanded", "false");
    expect(summary).toHaveFocus();
  });

  it("shows per-key usage without ownership identification or an account workflow", async () => {
    renderApp("/keys");
    expect(await screen.findByText("Exa primary")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "关于上游提供商密钥" })).toBeNull();
    expect(screen.queryByText(/not independently verified|shared allowance/i)).toBeNull();
    expect(screen.queryByText(/billing account/i)).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "Shared allowance" })).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "本地安全限额" })).toBeNull();
    expect(screen.getByRole("columnheader", { name: "用量" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add account" })).toBeNull();
    expect(callsTo("GET", "/v1/admin/accounts")).toHaveLength(0);
    expect(screen.getByText(/凭据仍为 1 个未结任务保留/)).toBeInTheDocument();
  });

  it("sends a new secret once and keeps it out of storage", async () => {
    renderApp("/keys");
    await userEvent.click(await screen.findByRole("button", { name: "添加密钥" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "添加密钥 · Exa" }));
    const dialog = screen.getByRole("dialog", { name: "添加密钥 · Exa" });
    await userEvent.type(within(dialog).getByLabelText("名称"), "Exa third");
    await userEvent.type(within(dialog).getByLabelText("密钥内容"), "synthetic-secret-9999");
    await userEvent.click(within(dialog).getByRole("button", { name: "添加密钥" }));
    expect(await screen.findByText("密钥已添加。")).toBeInTheDocument();
    expect(callsTo("POST", "/v1/admin/keys")[0]?.body).toEqual({
      provider: "exa",
      quota_policy: {
        unit: "micro_usd",
        allocation: 0,
        per_operation_reservation: 0,
        active_jobs: 0,
      },
      label: "Exa third",
      secret: "synthetic-secret-9999",
      enabled: true,
    });
    expect(allBrowserStorage()).not.toContain("synthetic-secret-9999");
    expect(screen.queryByDisplayValue("synthetic-secret-9999")).toBeNull();
  });

  it("reports a concurrent edit as a revision conflict", async () => {
    renderApp("/keys");
    const row = (await screen.findByText("Exa primary")).closest("tr");
    if (row === null) {
      throw new Error("Key row missing");
    }
    const key = mockDb.state.keys.find((entry) => entry.id === "key_exa_1");
    if (key !== undefined) {
      key.version += 1;
    }
    await userEvent.click(within(row).getByRole("button", { name: "停用 Exa primary" }));
    expect(await screen.findByText(/已在别处被修改/)).toBeInTheDocument();
    expect(callsTo("PATCH", "/v1/admin/keys/key_exa_1")[0]?.body).toEqual({
      expected_version: 1,
      enabled: false,
    });
  });

  it("disables write actions without admin:keys:write", async () => {
    mockDb.state.session = mockSession(
      ALL_PERMISSIONS.filter((permission) => permission !== "admin:keys:write"),
    );
    renderApp("/keys");
    await screen.findByText("Exa primary");
    expect(screen.getByRole("button", { name: "添加密钥" })).toBeDisabled();
    expect(screen.getByText("admin:keys:write")).toBeInTheDocument();
    for (const trigger of screen.getAllByRole("button", { name: /的更多操作/ })) {
      expect(trigger).toBeDisabled();
    }
    expect(mockDb.state.calls.filter((call) => call.method !== "GET")).toHaveLength(0);
  });
});
