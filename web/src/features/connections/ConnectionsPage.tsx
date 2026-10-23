/** REST/MCP connection details, readiness diagnostics and scoped client tokens. */
import { useQuery } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { Badge, Notice, QueryView } from "../../components/Feedback";
import { PageHeader, RequirePermission, Section } from "../../components/Page";
import { CopyButton } from "../../components/Values";

/**
 * Connections page.
 *
 * @returns Page element.
 */
export function ConnectionsPage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("connections.title")} description={t("connections.description")} />
      <RequirePermission permission="admin:read">
        <ConnectionDetails />
      </RequirePermission>
    </>
  );
}

/**
 * Endpoint, protocol, example and capability details.
 *
 * @returns Sections.
 */
export function ConnectionDetails() {
  const { t } = useI18n();
  const api = useApi();
  const info = useQuery({
    queryKey: ["connection-info"],
    queryFn: () => call(api.GET("/v1/admin/connection-info")),
  });
  return (
    <QueryView query={info}>
      {(data) => (
        <>
          <Section title={t("connections.endpoints")}>
            <dl className="details-grid">
              <dt>{t("connections.restBase")}</dt>
              <dd>
                <code>{data.rest_base_url}</code>{" "}
                <CopyButton text={data.rest_base_url} label={t("connections.copyRest")} />
              </dd>
              <dt>{t("connections.mcpUrl")}</dt>
              <dd>
                <code>{data.mcp_url}</code>{" "}
                <CopyButton text={data.mcp_url} label={t("connections.copyMcp")} />
              </dd>
              <dt>{t("connections.authentication")}</dt>
              <dd>{t("connections.bearer")}</dd>
              <dt>{t("connections.protocolVersions")}</dt>
              <dd>
                <span className="badge-group">
                  {data.protocol_versions.map((version) => (
                    <Badge key={version} tone="neutral">
                      {version}
                    </Badge>
                  ))}
                </span>
              </dd>
              <dt>{t("connections.contract")}</dt>
              <dd>
                <code>{data.capabilities.contract_version}</code>{" "}
                <code className="muted hash">{data.capabilities.contract_hash}</code>
              </dd>
            </dl>
            <Notice tone="info" title={t("connections.oauthUnsupported")}>
              {t("connections.oauthUnsupportedDetail")}
            </Notice>
          </Section>
          <NetworkDiagnostics readiness={data.readiness} />
          <Section
            title={t("connections.examples")}
            description={t("connections.examplesDescription")}
          >
            {data.examples.length === 0 ? (
              <p className="muted">{t("connections.noExamples")}</p>
            ) : (
              data.examples.map((example) => (
                <figure key={example.title} className="example">
                  <figcaption>
                    {example.title}{" "}
                    <CopyButton
                      text={example.command}
                      label={t("connections.copyExample", { title: example.title })}
                    />
                  </figcaption>
                  <pre className="code-block">{example.command}</pre>
                </figure>
              ))
            )}
          </Section>
          <CapabilityTable capabilities={data.capabilities.capabilities} />
        </>
      )}
    </QueryView>
  );
}

/**
 * Bind scope and nonloopback safety diagnostics.
 *
 * @param props - Readiness.
 * @returns Section element.
 */
function NetworkDiagnostics({ readiness }: { readiness: Schemas["Readiness"] }) {
  const { t } = useI18n();
  const isUnsafe =
    readiness.bind_scope === "nonloopback" && !readiness.nonloopback_safety_configured;
  return (
    <Section title={t("connections.network")}>
      <dl className="details-grid">
        <dt>{t("connections.bindScope")}</dt>
        <dd>
          <Badge tone={readiness.bind_scope === "loopback" ? "neutral" : "warning"}>
            {t(`bindScope.${readiness.bind_scope}`)}
          </Badge>
        </dd>
        <dt>{t("connections.nonloopbackSafety")}</dt>
        <dd>
          <Badge
            tone={
              readiness.nonloopback_safety_configured
                ? "info"
                : readiness.bind_scope === "loopback"
                  ? "neutral"
                  : "danger"
            }
          >
            {readiness.nonloopback_safety_configured
              ? t("connections.configured")
              : t("connections.notConfigured")}
          </Badge>
        </dd>
      </dl>
      {readiness.network_safety_reason === null ? null : (
        <Notice tone={isUnsafe ? "danger" : "info"}>{readiness.network_safety_reason}</Notice>
      )}
      {readiness.bind_scope === "loopback" && !readiness.nonloopback_safety_configured ? (
        <p className="muted">{t("connections.loopbackOnly")}</p>
      ) : null}
    </Section>
  );
}

/**
 * Capability and profile matrix.
 *
 * @param props - Capabilities.
 * @returns Section element.
 */
function CapabilityTable({ capabilities }: { capabilities: Schemas["Capability"][] }) {
  const { t } = useI18n();
  return (
    <Section title={t("connections.capabilities")}>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th scope="col">{t("routing.capability")}</th>
              <th scope="col">{t("common.state")}</th>
              <th scope="col">{t("connections.profiles")}</th>
            </tr>
          </thead>
          <tbody>
            {capabilities.map((capability) => (
              <tr key={capability.operation}>
                <th scope="row">
                  <code>{capability.operation}</code>
                </th>
                <td>
                  <Badge tone={capability.enabled ? "neutral" : "warning"}>
                    {capability.enabled ? t("common.enabled") : t("common.disabled")}
                  </Badge>
                  {capability.unavailable_reason === null ? null : (
                    <p className="muted">{capability.unavailable_reason}</p>
                  )}
                </td>
                <td>
                  <ul className="plain-list">
                    {capability.profiles.map((profile) => (
                      <li key={`${profile.provider}-${profile.profile}`}>
                        {t(`provider.${profile.provider}`)} · <code>{profile.profile}</code>{" "}
                        {profile.enabled ? null : (
                          <Badge tone="warning">{t("common.disabled")}</Badge>
                        )}{" "}
                        <span className="muted">
                          {t("connections.cancellation")}:{" "}
                          {profile.cancellation.upstream
                            ? t("connections.upstreamCancel")
                            : t("connections.noUpstreamCancel")}
                        </span>
                        {profile.unavailable_reason === null ? null : (
                          <span className="muted"> · {profile.unavailable_reason}</span>
                        )}
                      </li>
                    ))}
                  </ul>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  );
}
