/** Usage progress never mixes periods, scopes, units or unknown totals. */
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Schemas } from "../../lib/api";
import { I18nProvider } from "../../lib/i18n";
import { mockKeys } from "../../mocks/fixtures";
import { KeyUsage, selectKeyUsage } from "./KeyUsage";

/** Build one local lifetime observation, matching the configured key's unit. */
function fixture(provider: "exa" | "tavily", allocation = 100) {
  const key = mockKeys().find((item) => item.provider === provider)!;
  key.quota_policy.allocation = allocation;
  const usage: Schemas["KeyUsage"] = {
    key_id: key.id,
    provider,
    association_ref: key.association_ref,
    association_evidence: "unknown",
    scope: "key",
    source: "local",
    unit: key.quota_policy.unit,
    used: 25,
    limit: allocation,
    limit_kind: allocation > 0 ? "finite" : "unlimited",
    remaining: allocation - 25,
    reserved: 0,
    period_start: null,
    period_end: null,
    freshness: "fresh",
    unavailable_reason: null,
    observed_at: "2026-10-24T00:00:00Z",
  };
  return { key, usage };
}

/** Render actual Semi progress and localized numbers without network requests. */
function show(key: Schemas["UpstreamKey"], items: Schemas["KeyUsage"][]) {
  return render(
    <I18nProvider>
      <KeyUsage providerKey={key} items={items} />
    </I18nProvider>,
  );
}

describe("key usage", () => {
  it.each(["exa", "tavily"] as const)(
    "uses matching %s cumulative amounts and keeps reservations separate",
    (provider) => {
      const { key, usage } = fixture(provider);
      usage.reserved = 10;
      show(key, [usage]);
      expect(screen.getByText("25%")).toBeVisible();
      expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "25");
      expect(
        screen.getByText(provider === "exa" ? "US$0.000025 / US$0.0001" : "25 积分 / 100 积分"),
      ).toBeVisible();
      expect(screen.getByText(/预留/)).toBeVisible();
      expect(screen.getByText(/剩余/)).toHaveTextContent(
        provider === "exa" ? "US$0.000065" : "65 积分",
      );
    },
  );
  it.each([
    [100, "额度耗尽"],
    [125, "已超额度"],
  ])("shows %s used without hiding exhaustion or overspend", (used, label) => {
    const { key, usage } = fixture("tavily");
    usage.used = Number(used);
    show(key, [usage]);
    expect(screen.getByText(`${used}%`)).toBeVisible();
    expect(screen.getByText(String(label))).toBeVisible();
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
  });
  it("does not pair a monthly upstream numerator with a configured lifetime allowance", () => {
    const { key, usage } = fixture("exa");
    const monthly = {
      ...usage,
      source: "upstream" as const,
      used: 90,
      period_start: "2026-10-01T00:00:00Z",
      period_end: "2026-11-01T00:00:00Z",
    };
    expect(selectKeyUsage(key, [monthly, usage])).toBe(usage);
    expect(selectKeyUsage(key, [monthly])).toBeUndefined();
  });
  it("uses a verified upstream total only when local allowance is unlimited", () => {
    const { key, usage } = fixture("tavily", 0);
    const upstream = {
      ...usage,
      source: "upstream" as const,
      limit_kind: "finite" as const,
      limit: 200,
      remaining: 175,
    };
    show(key, [usage, upstream]);
    expect(screen.getByText("12.5%")).toBeVisible();
    expect(screen.getByText("25 积分 / 200 积分")).toBeVisible();
    expect(screen.getByText(/本地不限/)).toBeVisible();
  });
  it("does not manufacture an upstream total from a balance, a shared scope or another unit", () => {
    const { key, usage } = fixture("exa", 0);
    const shared = {
      ...usage,
      key_id: null,
      scope: "shared" as const,
      source: "upstream" as const,
      limit: 1000,
    };
    const wrongUnit = {
      ...usage,
      source: "upstream" as const,
      unit: "tavily_credit" as const,
      limit: 1000,
    };
    const upstream = {
      ...usage,
      source: "upstream" as const,
      limit_kind: "unknown" as const,
      limit: null,
      remaining: 900,
    };
    show(key, [shared, wrongUnit, upstream]);
    expect(screen.getByText("US$0.000025 / 总量未知")).toBeVisible();
    expect(screen.queryByRole("progressbar")).toBeNull();
  });
  it("keeps missing use unknown instead of showing zero percent", () => {
    const { key } = fixture("tavily", 0);
    show(key, []);
    expect(screen.getByText(/总量未知/)).toBeVisible();
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(screen.queryByText("0%")).toBeNull();
  });
});
