import { Button } from "./Controls";
/** Small value renderers shared across pages: money/credits, revisions, copy and dates. */
import { useState } from "react";
import type { Schemas } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { Badge } from "./Feedback";
import type { Tone } from "./Feedback";

/**
 * Format an integer amount in its own unit. Units are never converted or combined.
 *
 * @param amount - Integer micro-USD or Tavily credits.
 * @param unit - Contract unit.
 * @param locale - Intl locale tag.
 * @param creditTemplate - Credit label template containing `{amount}`.
 * @returns Formatted text.
 */
export function formatUnitAmount(
  amount: number,
  unit: Schemas["UsageUnit"],
  locale: string,
  creditTemplate = "{amount} credits",
): string {
  if (unit === "micro_usd") {
    return new Intl.NumberFormat(locale, {
      style: "currency",
      currency: "USD",
      minimumFractionDigits: 2,
      maximumFractionDigits: 6,
    }).format(amount / 1_000_000);
  }
  return creditTemplate.replace("{amount}", new Intl.NumberFormat(locale).format(amount));
}

/**
 * Hook returning a unit formatter bound to the current locale.
 *
 * @returns Formatter for amount and unit.
 */
export function useUnitFormatter(): (amount: number, unit: Schemas["UsageUnit"]) => string {
  const { locale, t } = useI18n();
  const tag = locale === "zh" ? "zh-CN" : "en-US";
  const creditTemplate = t("unit.credits");
  return (amount, unit) => formatUnitAmount(amount, unit, tag, creditTemplate);
}

const CERTAINTY_TONE: Record<Schemas["UsageCertainty"], Tone> = {
  reported: "info",
  estimated: "warning",
  unknown: "warning",
};

/**
 * Render a usage amount with explicit certainty; a null amount is shown as unknown.
 *
 * @param props - Usage amount.
 * @returns Amount text with certainty badge.
 */
export function UsageAmountView({ usage }: { usage: Schemas["UsageAmount"] }) {
  const { t } = useI18n();
  const format = useUnitFormatter();
  return (
    <span className="usage-amount">
      <span>
        {usage.amount === null ? t("usage.amountUnknown") : format(usage.amount, usage.unit)}
      </span>{" "}
      <Badge tone={CERTAINTY_TONE[usage.certainty]}>{t(`certainty.${usage.certainty}`)}</Badge>
    </span>
  );
}

/**
 * Render a route revision; 0 means no route was applied.
 *
 * @param props - Revision number.
 * @returns Revision text.
 */
export function RouteRevision({ revision }: { revision: number }) {
  const { t } = useI18n();
  if (revision === 0) {
    return <span className="muted">{t("routing.revisionUnset")}</span>;
  }
  return <span>r{revision}</span>;
}

/**
 * Locale-formatted timestamp with a machine-readable `<time>` element.
 *
 * @param props - RFC3339 value or null.
 * @returns Time element or dash.
 */
export function DateTime({ value }: { value: string | null | undefined }) {
  const { formatDateTime } = useI18n();
  if (value === null || value === undefined) {
    return <span className="muted">—</span>;
  }
  return <time dateTime={value}>{formatDateTime(value)}</time>;
}

/**
 * Copy text to the clipboard and announce the result.
 *
 * @param props - Text and accessible label.
 * @returns Copy button.
 */
export function CopyButton({
  text,
  label,
  primary = false,
}: {
  text: string;
  label: string;
  primary?: boolean;
}) {
  const { t } = useI18n();
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setState("copied");
    } catch {
      setState("failed");
    }
  };
  return (
    <span className="copy">
      <Button
        type="button"
        className={primary ? "button button-primary" : "button button-small"}
        onClick={() => void copy()}
        aria-label={label}
      >
        {t("common.copy")}
      </Button>
      <span className="muted" aria-live="polite">
        {state === "copied" ? t("common.copied") : state === "failed" ? t("common.copyFailed") : ""}
      </span>
    </span>
  );
}

/**
 * Render an unknown JSON object as escaped, pretty-printed text.
 *
 * @param props - Value to display.
 * @returns Preformatted block.
 */
export function JsonBlock({ value }: { value: unknown }) {
  return <pre className="json-block">{JSON.stringify(value, null, 2)}</pre>;
}

/**
 * Render a nullable count; null means not yet known and is never shown as zero.
 *
 * @param props - Count or null.
 * @returns Count text.
 */
export function NullableCount({ value }: { value: number | null }) {
  const { t, formatNumber } = useI18n();
  return value === null ? (
    <span className="muted">{t("common.unknown")}</span>
  ) : (
    <span>{formatNumber(value)}</span>
  );
}
