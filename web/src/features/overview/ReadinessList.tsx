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
          <Badge tone={readiness.ready ? "success" : "danger"}>
            {readiness.ready ? t("readiness.ready") : t("readiness.notReady")}
          </Badge>
        </dd>
        <dt>{t("readiness.database")}</dt>
        <dd>
          <Badge tone={readiness.database === "ready" ? "neutral" : "danger"}>
            {t(`dbState.${readiness.database}`)}
          </Badge>
        </dd>
        <dt>{t("readiness.secretStore")}</dt>
        <dd>
          <Badge tone={readiness.secret_store === "unlocked" ? "neutral" : "danger"}>
            {t(`secretStore.${readiness.secret_store}`)}
          </Badge>
        </dd>
        <dt>{t("readiness.assets")}</dt>
        <dd>
          <Badge tone={readiness.assets === "ready" ? "neutral" : "warning"}>
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
      <dd>
        <Badge tone={provider.enabled ? "neutral" : "warning"}>
          {provider.enabled ? t("common.enabled") : t("common.disabled")}
        </Badge>
        {provider.reason === null ? null : <span className="muted"> {provider.reason}</span>}
      </dd>
    </>
  );
}
