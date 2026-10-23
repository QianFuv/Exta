/** Display usage only against a total from the same accounting scope and period. */
import Progress from "@douyinfe/semi-ui/lib/es/progress";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useUnitFormatter, DateTime } from "../../components/Values";

type Usage = Schemas["KeyUsage"];
type Key = Schemas["UpstreamKey"];

/** Local lifetime limits never borrow a numerator from a provider's monthly observation. */
export function selectKeyUsage(key: Key, items: Usage[]): Usage | undefined {
  const matching = items.filter(
    (item) =>
      item.key_id === key.id &&
      item.scope === "key" &&
      item.provider === key.provider &&
      item.unit === key.quota_policy.unit,
  );
  const local = matching.find(
    (item) => item.source !== "upstream" && item.period_start === null && item.period_end === null,
  );
  if (key.quota_policy.allocation > 0) return local;
  return (
    matching.find(
      (item) =>
        item.source === "upstream" &&
        item.freshness !== "unknown" &&
        item.unavailable_reason === null &&
        item.used !== null,
    ) ?? local
  );
}

/** Unknown totals have no percentage; held charges remain distinct from settled use. */
export function KeyUsage({ providerKey, items }: { providerKey: Key; items: Usage[] }) {
  const { t, formatNumber } = useI18n();
  const format = useUnitFormatter();
  const usage = selectKeyUsage(providerKey, items);
  const used = usage?.used ?? null;
  const total =
    providerKey.quota_policy.allocation > 0
      ? providerKey.quota_policy.allocation
      : usage?.source === "upstream" && usage.limit_kind === "finite"
        ? usage.limit
        : null;
  const percentage = used !== null && total !== null && total > 0 ? (used / total) * 100 : null;
  const unit = providerKey.quota_policy.unit;
  const summary = `${used === null ? t("usage.amountUnknown") : format(used, unit)} / ${total === null ? t("keys.totalUnknown") : format(total, unit)}`;
  return (
    <div className="key-usage">
      <div className="key-usage-numbers">
        <span>{summary}</span>
        {percentage !== null ? <strong>{formatNumber(percentage, 1)}%</strong> : null}
      </div>
      {percentage !== null ? (
        <Progress
          motion={false}
          percent={Math.min(100, percentage)}
          showInfo={false}
          aria-label={t("keys.usageFor", { label: providerKey.label })}
          aria-valuetext={summary}
          stroke={percentage >= 100 ? "var(--semi-color-danger)" : "var(--semi-color-primary)"}
        />
      ) : null}
      {total !== null && used !== null ? (
        <small className="cell-secondary">
          {used > total
            ? t("keys.overLimit")
            : used === total
              ? t("keys.exhausted")
              : t("keys.remaining", {
                  amount: format(Math.max(0, total - used - (usage?.reserved ?? 0)), unit),
                })}
        </small>
      ) : null}
      {usage && usage.reserved > 0 ? (
        <small className="cell-secondary">
          {t("keys.reservedUsage", { amount: format(usage.reserved, unit) })}
        </small>
      ) : null}
      <small className="cell-secondary">
        {usage?.source === "upstream" ? t("keys.upstreamObservation") : t("keys.localCumulative")}
        {usage?.source === "estimate" ? ` · ${t("certainty.estimated")}` : ""}
        {providerKey.quota_policy.allocation === 0 ? ` · ${t("keys.unlimited")}` : ""}
        {usage?.freshness === "stale" ? ` · ${t("keys.staleObservation")}` : ""}
      </small>
      {usage?.source === "upstream" ? (
        <small className="cell-secondary">
          {usage.period_start && usage.period_end ? (
            <>
              <DateTime value={usage.period_start} /> – <DateTime value={usage.period_end} />
            </>
          ) : (
            t("keys.periodUnknown")
          )}
        </small>
      ) : null}
    </div>
  );
}
