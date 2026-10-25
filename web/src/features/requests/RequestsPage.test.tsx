import { selectOption } from "../../test/selectOption";
/** Requests: empty success vs unknown counts, partial failures and pre-routing rejection. */
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import { mockDb } from "../../mocks/handlers";
import { callsTo, renderApp } from "../../test/renderApp";

beforeAll(async () => {
  await import("./RequestsPage");
});

/**
 * Find the table row containing a request ID.
 *
 * @param id - Request ID.
 * @returns Scoped queries for the row.
 */
async function rowFor(id: string) {
  const cell = await screen.findByText(id);
  const row = cell.closest("tr");
  if (row === null) {
    throw new Error(`Row ${id} missing`);
  }
  return within(row);
}

describe("requests", () => {
  it("finds a logical call by request ID without showing unrelated calls", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    await userEvent.type(screen.getByRole("textbox", { name: "请求 ID" }), "req_unknown");
    await userEvent.click(screen.getByRole("button", { name: "应用" }));
    await waitFor(() => expect(screen.queryByText("req_ok")).toBeNull());
    expect(screen.getByText("req_unknown")).toBeInTheDocument();
  });
  it("refuses malformed typed dates without issuing a filtered request", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    const count = callsTo("GET", "/v1/admin/requests").length;
    await userEvent.type(screen.getByLabelText("创建时间晚于"), "not-a-date");
    expect(screen.getByRole("button", { name: "应用" })).toBeDisabled();
    expect(callsTo("GET", "/v1/admin/requests")).toHaveLength(count);
  });

  it("explains reversed date filters without fetching another list", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    const before = callsTo("GET", "/v1/admin/requests").length;
    fireEvent.change(screen.getByLabelText("创建时间晚于"), {
      target: { value: "2026-10-23T12:00" },
    });
    fireEvent.change(screen.getByLabelText("创建时间早于"), {
      target: { value: "2026-10-22T12:00" },
    });
    expect(screen.getByRole("alert")).toHaveTextContent("开始时间必须早于结束时间。");
    expect(screen.getByRole("button", { name: "应用" })).toBeDisabled();
    expect(callsTo("GET", "/v1/admin/requests")).toHaveLength(before);
  });
  it("refreshes a progressing request using only local reads", async () => {
    const detail = mockDb.state.requests.find((entry) => entry.request.id === "req_ok");
    if (!detail) throw new Error("Missing request fixture");
    detail.request.status = "running";
    renderApp("/requests/req_ok");
    await screen.findByText("运行中", { selector: ".semi-tag-content" });
    detail.request.status = "succeeded";
    await userEvent.click(screen.getByRole("button", { name: "刷新" }));
    await waitFor(() =>
      expect(screen.getByText("已成功", { selector: ".semi-tag-content" })).toBeInTheDocument(),
    );
    expect(callsTo("GET", "/v1/admin/requests/req_ok")).toHaveLength(2);
    expect(mockDb.state.calls.filter((entry) => entry.method !== "GET")).toHaveLength(0);
  });
  it("distinguishes known empty success, unknown summaries and unset route revisions", async () => {
    renderApp("/requests");
    expect((await rowFor("req_empty")).getByText("成功，结果为零条")).toBeInTheDocument();
    const unknown = await rowFor("req_unknown");
    expect(unknown.getByText("尚未知晓")).toBeInTheDocument();
    expect(unknown.getByText("不确定").closest(".semi-tag")).toHaveClass("status-warning");
    expect(unknown.getByText("计费不确定")).toBeInTheDocument();
    const rejected = await rowFor("req_rejected");
    expect(rejected.getByText("无（分发前）")).toBeInTheDocument();
    const partial = await rowFor("req_partial");
    expect(partial.getByText("部分结果")).toBeInTheDocument();
    expect(partial.getByText("已成功").closest(".semi-tag")).toHaveClass("status-warning");
    expect((await rowFor("req_too_large")).getByText("response_too_large")).toBeInTheDocument();
    await userEvent.click(rejected.getByRole("link"));
    expect(await screen.findByText("未应用路由")).toBeInTheDocument();
  });

  it("applies filters as contract query parameters", async () => {
    renderApp("/requests");
    await screen.findByText("req_ok");
    await selectOption(screen.getByLabelText("状态"), "已失败");
    await userEvent.click(screen.getByRole("button", { name: "应用" }));
    expect(await screen.findByText("req_rejected")).toBeInTheDocument();
    expect(screen.queryByText("req_ok")).toBeNull();
  });

  it("shows an empty state when there are no records", async () => {
    mockDb.state.requests = [];
    renderApp("/requests");
    expect(await screen.findByText("暂无请求")).toBeInTheDocument();
  });

  it("shows attempts, partial and billing uncertainty on detail without raw content", async () => {
    const first = renderApp("/requests/req_partial");
    expect(await screen.findByText("仅返回了部分结果")).toBeInTheDocument();
    expect(screen.getByText("第 1 次尝试")).toBeInTheDocument();
    expect(screen.getByText(/不保留原始查询、输入或结果内容/)).toBeInTheDocument();
    first.unmount();

    renderApp("/requests/req_unknown");
    expect(
      await screen.findByText("计费不确定", { selector: ".semi-banner-title" }),
    ).toBeInTheDocument();
    expect(screen.getByText("可能已发送")).toBeInTheDocument();
  });

  it("reports an unknown or unauthorized detail as not found", async () => {
    renderApp("/requests/req_missing");
    expect(await screen.findByText("未找到。")).toBeInTheDocument();
    expect(callsTo("GET", "/v1/admin/requests/req_missing")).toHaveLength(1);
  });
  it("opens details without losing list filters and restores keyboard focus on repeated close", async () => {
    renderApp("/requests");
    await screen.findByText("req_partial");
    await selectOption(screen.getByLabelText("状态"), "已成功");
    await userEvent.click(screen.getByRole("button", { name: "应用" }));
    for (let index = 0; index < 2; index++) {
      const row = await rowFor("req_partial");
      const opener = row.getByRole("link");
      await userEvent.click(opener);
      const dialog = await screen.findByRole("dialog", { name: "请求详情" });
      expect(await within(dialog).findByText("第 1 次尝试")).toBeInTheDocument();
      await userEvent.keyboard("{Escape}");
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(opener).toHaveFocus();
      expect(screen.getByLabelText("状态")).toHaveTextContent("已成功");
    }
    expect(mockDb.state.calls.filter((call) => call.method !== "GET")).toHaveLength(0);
  });
});
