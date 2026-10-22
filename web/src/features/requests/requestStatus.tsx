/** Request status presentation shared by list and detail. */
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { Badge } from "../../components/Feedback";
import type { Tone } from "../../components/Feedback";

/** A sanitized request record. */
export type RequestRecord = Schemas["RequestRecord"];

/**
 * Choose a tone; partial or billing-uncertain success is never shown as clean success.
 *
 * @param record - Request record.
 * @returns Visual tone.
 */
export function requestTone(
  record: Pick<RequestRecord, "status" | "partial" | "billing_uncertain">,
): Tone {
  switch (record.status) {
    case "succeeded":
      return record.partial === true || record.billing_uncertain ? "warning" : "success";
    case "failed":
      return "danger";
    case "uncertain":
      return "warning";
    default:
      return "info";
  }
}

/**
 * Status badges for a request record.
 *
 * @param props - Request record.
 * @returns Badge group.
 */
export function RequestStatusBadges({ record }: { record: RequestRecord }) {
  const { t } = useI18n();
  return (
    <span className="badge-group">
      <Badge tone={requestTone(record)}>{t(`requestStatus.${record.status}`)}</Badge>
      {record.partial === true ? <Badge tone="warning">{t("requests.partial")}</Badge> : null}
      {record.billing_uncertain ? (
        <Badge tone="warning">{t("requests.billingUncertain")}</Badge>
      ) : null}
    </span>
  );
}

/**
 * Explain the result summary: unknown, known empty success, or counts.
 *
 * @param props - Request record.
 * @returns Summary text.
 */
export function ResultSummary({ record }: { record: RequestRecord }) {
  const { t, formatNumber } = useI18n();
  if (record.result_count === null) {
    return <span className="muted">{t("requests.summaryUnknown")}</span>;
  }
  if (record.result_count === 0 && record.partial === false && record.status === "succeeded") {
    return <span>{t("requests.emptySuccess")}</span>;
  }
  return (
    <span>
      {t("requests.resultCount", { count: formatNumber(record.result_count) })}
      {record.failure_count === null ? null : (
        <>
          {" · "}
          {t("requests.failureCount", { count: formatNumber(record.failure_count) })}
        </>
      )}
    </span>
  );
}
