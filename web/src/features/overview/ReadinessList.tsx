import { DiagnosticReason } from "../../components/DiagnosticReason";
/** Local readiness rendering shared by Overview and Settings; no upstream probing. */
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { Badge, Notice } from "../../components/Feedback";

/**
 * Readiness facts with explicit locked/unavailable/incompatible states.
 *
 * @param props - Readiness.
 * @returns Notices and list.
 */
export function ReadinessList({ readiness }: { readiness: Schemas["Readiness"] }) {
  const { t } = useI18n();
  return (
    <div className="readiness">
      {readiness.secret_store === "locked" ? (
        <Notice tone="danger" role="alert" title={t("readiness.lockedTitle")}>
          {t("readiness.lockedDetail")}
        </Notice>
      ) : null}
      {readiness.database === "unavailable" ? (
        <Notice tone="danger" role="alert" title={t("readiness.dbUnavailable")} />
      ) : null}
      {readiness.database === "incompatible" ? (
        <Notice tone="danger" role="alert" title={t("readiness.dbIncompatible")} />
      ) : null}
      {readiness.assets === "missing" ? (
        <Notice tone="warning" title={t("readiness.assetsMissing")} />
      ) : null}
      <dl className="details-grid">
        <dt>{t("readiness.overall")}</dt>
        <dd>
          <Badge
            tone={
              readiness.ready
                ? "success"
                : readiness.database !== "ready" || readiness.secret_store !== "unlocked"
                  ? "danger"
                  : "warning"
            }
          >
            {readiness.ready ? t("readiness.ready") : t("readiness.notReady")}
          </Badge>
        </dd>
        <dt>{t("readiness.database")}</dt>
        <dd>
          <Badge tone={readiness.database === "ready" ? "success" : "danger"}>
            {t(`dbState.${readiness.database}`)}
          </Badge>
        </dd>
        <dt>{t("readiness.secretStore")}</dt>
        <dd>
          <Badge tone={readiness.secret_store === "unlocked" ? "success" : "danger"}>
            {t(`secretStore.${readiness.secret_store}`)}
          </Badge>
        </dd>
        <dt>{t("readiness.assets")}</dt>
        <dd>
          <Badge tone={readiness.assets === "ready" ? "success" : "warning"}>
            {t(`assets.${readiness.assets}`)}
          </Badge>
        </dd>
        {readiness.providers.map((provider) => (
          <ProviderReadiness key={provider.provider} provider={provider} />
        ))}
      </dl>
      <p className="muted">{t("readiness.localOnly")}</p>
    </div>
  );
}

/**
 * One provider readiness pair.
 *
 * @param props - Provider readiness.
 * @returns Term/definition fragment.
 */
function ProviderReadiness({ provider }: { provider: Schemas["Readiness"]["providers"][number] }) {
  const { t } = useI18n();
  return (
    <>
      <dt>{t(`provider.${provider.provider}`)}</dt>
      <dd className="provider-readiness">
        <Badge tone={provider.enabled ? "success" : "warning"}>
          {provider.enabled ? t("dispatch.available") : t("dispatch.blocked")}
        </Badge>
        {provider.reason === null ? null : <DiagnosticReason reason={provider.reason} />}
      </dd>
    </>
  );
}
