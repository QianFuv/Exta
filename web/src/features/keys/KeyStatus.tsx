/** Present configured enablement separately from known dispatch restrictions. */
import type { Schemas } from "../../lib/api";
import type { MessageKey } from "../../lib/messages";
import { useI18n } from "../../lib/i18n";
import { Badge } from "../../components/Feedback";

/** Derive only restrictions evidenced by the public key metadata, never a live balance. */
export function keyRestriction(key: Schemas["UpstreamKey"]): MessageKey | null {
  if (key.retirement_state !== "active") return "keys.retired";
  if (!key.enabled) return "common.disabled";
  if (key.validity === "invalid") return "validity.invalid";
  if (key.billing_state === "blocked") return "billingState.blocked";
  if (key.breaker !== "closed") return "dispatch.breaker";
  if (key.cooldown_until && Date.parse(key.cooldown_until) > Date.now()) return "dispatch.cooldown";
  if (key.quota_policy.allocation <= 0 || key.quota_policy.per_operation_reservation <= 0)
    return "keys.needsLimits";
  return null;
}

/** Show a clear restriction; an otherwise configured key remains subject to admission checks. */
export function KeyStatus({ providerKey }: { providerKey: Schemas["UpstreamKey"] }) {
  const { t } = useI18n();
  const reason = keyRestriction(providerKey);
  const isDisabled = !providerKey.enabled || providerKey.retirement_state !== "active";
  return (
    <div className="status-stack">
      <Badge tone={isDisabled ? "neutral" : reason ? "warning" : "success"}>
        {providerKey.retirement_state !== "active"
          ? t("keys.retiredState")
          : isDisabled
            ? t("common.disabled")
            : reason
              ? t("dispatch.blocked")
              : t("dispatch.configured")}
      </Badge>
      {reason && !isDisabled ? (
        <small className="muted">{t(reason)}</small>
      ) : !reason ? (
        <small className="muted">{t("dispatch.admissionCheck")}</small>
      ) : null}
    </div>
  );
}
