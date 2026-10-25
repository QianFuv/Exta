import { keyRestriction } from "../keys/KeyStatus";
import { useState } from "react";
import { Button } from "../../components/Controls";
import { DiagnosticReason } from "../../components/DiagnosticReason";
import { DataTable } from "../../components/DataTable";
/**
 * Overview of local readiness, queue/slot/rate counters, active research and recent
 * errors. Each panel loads independently and shows its own failure; nothing is inferred
 * as healthy when data is missing.
 */
import { Link, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import type { MessageKey } from "../../lib/messages";
import { EmptyState, Notice, QueryView } from "../../components/Feedback";
import { PageHeader, RequirePermission, Section } from "../../components/Page";
import { DateTime } from "../../components/Values";
import { UsageContent } from "../usage/UsagePage";
import { SectionNav } from "../../components/SectionNav";
import { RequestStatusBadges } from "../requests/requestStatus";
import { ReadinessList } from "./ReadinessList";

const METRICS: ReadonlyArray<{
  name: keyof Omit<Schemas["OverviewMetrics"], "observed_at">;
  label: MessageKey;
  isRisk: boolean;
}> = [
  { name: "requests_last_minute", label: "overview.requestsPerMinute", isRisk: false },
  { name: "errors_last_minute", label: "overview.errorsPerMinute", isRisk: true },
  { name: "waiting_requests", label: "overview.waitingRequests", isRisk: false },
  { name: "queued_jobs", label: "overview.queuedJobs", isRisk: false },
  { name: "active_slots_held", label: "overview.activeSlots", isRisk: false },
  { name: "uncertain_jobs", label: "overview.uncertainJobs", isRisk: true },
];

/**
 * Overview page.
 *
 * @returns Page element.
 */
export function OverviewPage() {
  const { t } = useI18n();
  const [params] = useSearchParams();
  return (
    <>
      <PageHeader title={t("overview.title")} />
      <SectionNav
        label={t("overview.title")}
        items={[
          { value: "summary", label: t("overview.activity") },
          { value: "usage", label: t("nav.usage") },
        ]}
      />
      <RequirePermission permission="admin:read">
        {params.get("view") === "usage" ? (
          <UsageContent />
        ) : (
          <>
            <MetricsPanel />
            <div className="dashboard-columns">
              <div>
                <RecentCalls />
              </div>
              <div>
                <KeysPanel />
                <ReadinessPanel />
              </div>
            </div>
          </>
        )}
      </RequirePermission>
    </>
  );
}

/**
 * Local readiness from settings.
 *
 * @returns Section.
 */
function ReadinessPanel() {
  const { t } = useI18n();
  const api = useApi();
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => call(api.GET("/v1/admin/settings")),
  });
  return (
    <Section title={t("overview.readiness")} help={t("readiness.localOnly")}>
      <QueryView query={settings}>
        {(data) => <ReadinessList readiness={data.readiness} />}
      </QueryView>
    </Section>
  );
}

/**
 * Queue, slot and rate counters plus usage freshness.
 *
 * @returns Section.
 */
function MetricsPanel() {
  const { t, formatNumber, formatDateTime } = useI18n();
  const api = useApi();
  const usage = useQuery({
    queryKey: ["usage", "overview"],
    queryFn: () => call(api.GET("/v1/admin/usage", { params: { query: { limit: 100 } } })),
    refetchInterval: 30_000,
  });
  return (
    <section className="metrics-section" aria-label={t("overview.activity")}>
      <QueryView query={usage}>
        {(data) => {
          return (
            <>
              <ul className="metrics">
                {METRICS.map((metric) => {
                  const value = data.overview[metric.name];
                  return (
                    <li
                      key={metric.name}
                      className={metric.isRisk && value > 0 ? "metric metric-risk" : "metric"}
                    >
                      <span className="metric-value">{formatNumber(value)}</span>
                      <span className="metric-label">{t(metric.label)}</span>
                    </li>
                  );
                })}
              </ul>
              <p className="metrics-observed">
                {t("overview.observedAt", { time: formatDateTime(data.overview.observed_at) })}
              </p>
            </>
          );
        }}
      </QueryView>
    </section>
  );
}

/**
 * Key-pool presence check.
 *
 * @returns Section.
 */
function KeysPanel() {
  const { t } = useI18n();
  const api = useApi();
  const keys = useQuery({
    queryKey: ["keys", "overview"],
    queryFn: () => call(api.GET("/v1/admin/keys", { params: { query: { limit: 100 } } })),
  });
  return (
    <Section
      title={t("overview.keys")}
      actions={
        <Link className="text-action" to="/keys">
          {t("nav.keys")}
        </Link>
      }
    >
      <QueryView query={keys}>
        {(data) => {
          if (data.items.length === 0) {
            return (
              <Notice tone="warning" title={t("overview.noKeys")}>
                {t("overview.noKeysDetail")}
              </Notice>
            );
          }
          const counts = (["exa", "tavily"] as const).map((provider) => {
            const providerKeys = data.items.filter((key) => key.provider === provider);
            return {
              provider,
              total: providerKeys.length,
              usable: providerKeys.filter((key) => keyRestriction(key) === null).length,
            };
          });
          return (
            <>
              <ul className="key-pool">
                {counts.map((entry) => (
                  <li key={entry.provider} className="key-pool-row">
                    <div className="key-pool-head">
                      <strong>{t(`provider.${entry.provider}`)}</strong>
                      <span className="key-pool-count">
                        {t("overview.keyCounts", { usable: entry.usable, total: entry.total })}
                      </span>
                    </div>
                    <span className="key-pool-bar" aria-hidden="true">
                      <span
                        style={{
                          width: `${entry.total === 0 ? 0 : (entry.usable / entry.total) * 100}%`,
                        }}
                      />
                    </span>
                    {entry.total > 0 && entry.usable === 0 ? (
                      <span className="inline-warning">{t("overview.noUsableKeys")}</span>
                    ) : null}
                  </li>
                ))}
              </ul>
              <p className="key-pool-note">
                {data.next_cursor === null ? t("overview.keyPoolHint") : t("overview.moreKeys")}
              </p>
            </>
          );
        }}
      </QueryView>
    </Section>
  );
}

/** Recent calls use stored metadata only; opening this view never dispatches a provider. */
function RecentCalls() {
  const { t, formatDuration } = useI18n();
  const [failedOnly, setFailedOnly] = useState(false);
  const api = useApi();
  const query = useQuery({
    queryKey: ["requests", "statistics", failedOnly],
    queryFn: () =>
      call(
        api.GET("/v1/admin/requests", {
          params: { query: { limit: 5, ...(failedOnly ? { status: "failed" as const } : {}) } },
        }),
      ),
    refetchInterval: 30_000,
  });
  return (
    <Section
      title={t("overview.recentRequests")}
      actions={<Link to="/requests">{t("overview.allCalls")} →</Link>}
    >
      <div className="activity-filters" role="group" aria-label={t("overview.recentRequests")}>
        <Button
          aria-pressed={!failedOnly}
          className={!failedOnly ? "button-primary" : ""}
          onClick={() => setFailedOnly(false)}
        >
          {t("common.all")}
        </Button>
        <Button
          aria-pressed={failedOnly}
          className={failedOnly ? "button-primary" : ""}
          onClick={() => setFailedOnly(true)}
        >
          {t("overview.recentErrors")}
        </Button>
      </div>
      <QueryView query={query}>
        {(data) =>
          data.items.length === 0 ? (
            <EmptyState title={t(failedOnly ? "overview.noRecentErrors" : "requests.emptyTitle")} />
          ) : (
            <div
              className="table-wrap"
              role="region"
              aria-label={t("overview.recentRequests")}
              tabIndex={0}
            >
              <DataTable
                mobileRow={(record) => (
                  <>
                    <div className="mobile-record-heading">
                      <Link to={`/requests/${encodeURIComponent(record.id)}`}>
                        {record.operation}
                      </Link>
                      <RequestStatusBadges record={record} />
                    </div>
                    <p className="cell-secondary">
                      <DateTime value={record.created_at} /> · {formatDuration(record.duration_ms)}
                    </p>
                  </>
                )}
                dataSource={data.items.slice(0, 5)}
                rowKey={(record) => record.id}
                columns={[
                  {
                    key: "column-0",
                    title: <>{t("common.created")}</>,
                    render: (_value, record) => {
                      return (
                        <>
                          <Link to={`/requests/${encodeURIComponent(record.id)}`}>
                            <DateTime value={record.created_at} />
                          </Link>
                        </>
                      );
                    },
                  },
                  {
                    key: "column-1",
                    title: <>{t("requests.operation")}</>,
                    render: (_value, record) => {
                      return (
                        <>
                          <span className="operation-name">{record.operation}</span>
                          <small className="cell-secondary">
                            {record.source_provider === null
                              ? "—"
                              : t(`provider.${record.source_provider}`)}
                          </small>
                        </>
                      );
                    },
                  },
                  {
                    key: "column-2",
                    title: <>{t("common.state")}</>,
                    render: (_value, record) => {
                      return (
                        <>
                          <RequestStatusBadges record={record} />
                          {record.error ? (
                            <small className="cell-secondary">
                              <DiagnosticReason reason={record.error.code} />
                            </small>
                          ) : null}
                        </>
                      );
                    },
                  },
                  {
                    key: "column-3",
                    title: <>{t("requests.duration")}</>,
                    render: (_value, record) => {
                      return <>{formatDuration(record.duration_ms)}</>;
                    },
                  },
                ]}
              />
            </div>
          )
        }
      </QueryView>
    </Section>
  );
}
