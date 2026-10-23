/** Localize known operational reasons without discarding unknown diagnostic evidence. */
import { useI18n } from "../lib/i18n";

/** Render a concise explanation for reasons returned by the local readiness APIs. */
export function DiagnosticReason({ reason }: { reason: string }) {
  const { t } = useI18n();
  const message =
    reason === "No eligible configured upstream key" ||
    reason === "No eligible configured account and key"
      ? t("diagnostic.noKey")
      : reason === "No eligible configured route" ||
          reason.startsWith("Profile is not currently configured")
        ? t("diagnostic.noRoute")
        : reason === "account_total_unavailable"
          ? t("diagnostic.totalUnknown")
          : reason === "not_refreshed"
            ? t("diagnostic.notRefreshed")
            : reason === "service_permission_missing"
              ? t("usage.servicePermissionMissing")
              : reason === "upstream_rate_limited"
                ? t("error.upstream_rate_limited")
                : reason;
  return <span className="muted">{message}</span>;
}
