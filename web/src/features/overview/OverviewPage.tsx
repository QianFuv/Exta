/**
 * Overview of local readiness, queue/slot/rate counters, active research and recent
 * errors. Each panel loads independently and shows its own failure; nothing is inferred
 * as healthy when data is missing.
 */
import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import type { MessageKey } from "../../lib/messages";
import { EmptyState, Notice, QueryView } from "../../components/Feedback";
import { PageHeader, RequirePermission, Section } from "../../components/Page";
import { DateTime } from "../../components/Values";
import { JobStateBadge, isPollingState } from "../research/jobStatus";
import { RequestStatusBadges } from "../requests/requestStatus";
import { ReadinessList } from "./ReadinessList";

const METRICS: ReadonlyArray<{
  name: keyof Omit<Schemas["OverviewMetrics"], "observed_at">;
  label: MessageKey;
  isRisk: boolean;
}> = [
  { name: "queued_jobs", label: "overview.queuedJobs", isRisk: false },
  { name: "active_slots_held", label: "overview.activeSlots", isRisk: false },
  { name: "uncertain_jobs", label: "overview.uncertainJobs", isRisk: true },
  { name: "requests_last_minute", label: "overview.requestsPerMinute", isRisk: false },
  { name: "errors_last_minute", label: "overview.errorsPerMinute", isRisk: true },
  { name: "waiting_requests", label: "overview.waitingRequests", isRisk: false },
];

/**
 * Overview page.
 *
 * @returns Page element.
 */
export function OverviewPage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("overview.title")} description={t("overview.description")} />
      <RequirePermission permission="admin:read">
        <div className="overview-grid">
          <ReadinessPanel />
          <MetricsPanel />
          <KeysPanel />
          <ResearchPanel />
          <ErrorsPanel />
        </div>
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
    <Section title={t("overview.readiness")}>
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
  const { t, formatNumber } = useI18n();
  const api = useApi();
  const usage = useQuery({
    queryKey: ["usage", "overview"],
    queryFn: () => call(api.GET("/v1/admin/usage", { params: { query: { limit: 100 } } })),
    refetchInterval: 30_000,
  });
  return (
    <Section
      title={t("overview.activity")}
      actions={
        <Link className="button button-small" to="/usage">
          {t("nav.usage")}
        </Link>
      }
    >
      <QueryView query={usage}>
        {(data) => {
          const staleCount = data.items.filter((item) => item.freshness !== "fresh").length;
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
              <p className="muted">
                {t("usage.observedAt")}: <DateTime value={data.overview.observed_at} />
              </p>
              {staleCount > 0 ? (
                <Notice tone="warning" title={t("overview.staleUsage", { count: staleCount })}>
                  {t("overview.staleUsageDetail")}
                </Notice>
              ) : null}
              {data.items.length === 0 ? <p className="muted">{t("overview.noUsage")}</p> : null}
            </>
          );
        }}
      </QueryView>
    </Section>
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
        <Link className="button button-small" to="/keys">
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
              usable: providerKeys.filter(
                (key) =>
                  key.enabled &&
                  key.retirement_state === "active" &&
                  key.validity !== "invalid" &&
                  key.breaker !== "open" &&
                  key.billing_state !== "blocked",
              ).length,
            };
          });
          return (
            <ul className="plain-list">
              {counts.map((entry) => (
                <li key={entry.provider}>
                  <strong>{t(`provider.${entry.provider}`)}</strong>:{" "}
                  {t("overview.keyCounts", { usable: entry.usable, total: entry.total })}
                  {entry.total > 0 && entry.usable === 0 ? (
                    <span className="inline-warning"> {t("overview.noUsableKeys")}</span>
                  ) : null}
                </li>
              ))}
              {data.next_cursor === null ? null : (
                <li className="muted">{t("overview.moreKeys")}</li>
              )}
            </ul>
          );
        }}
      </QueryView>
    </Section>
  );
}

/**
 * Recent non-terminal or attention-needing research.
 *
 * @returns Section.
 */
function ResearchPanel() {
  const { t } = useI18n();
  const api = useApi();
  const jobs = useQuery({
    queryKey: ["research", "overview"],
    queryFn: () => call(api.GET("/v1/admin/research", { params: { query: { limit: 25 } } })),
    refetchInterval: 30_000,
  });
  return (
    <Section
      title={t("overview.research")}
      actions={
        <Link className="button button-small" to="/research">
          {t("nav.research")}
        </Link>
      }
    >
      <QueryView query={jobs}>
        {(data) => {
          const attention = data.items.filter(
            (job) =>
              isPollingState(job.state) ||
              job.state === "submission_unknown" ||
              job.state === "observation_expired" ||
              job.uncertainty,
          );
          if (attention.length === 0) {
            return (
              <EmptyState
                title={t("overview.noActiveResearch")}
                description={t("overview.recentWindow")}
              />
            );
          }
          return (
            <ul className="plain-list">
              {attention.map((job) => (
                <li key={job.id}>
                  <Link to={`/research/${encodeURIComponent(job.id)}`}>
                    <code>{job.id}</code>
                  </Link>{" "}
                  <JobStateBadge job={job} />
                </li>
              ))}
            </ul>
          );
        }}
      </QueryView>
    </Section>
  );
}

/**
 * Recent failed requests.
 *
 * @returns Section.
 */
function ErrorsPanel() {
  const { t } = useI18n();
  const api = useApi();
  const failures = useQuery({
    queryKey: ["requests", "overview-failed"],
    queryFn: () =>
      call(api.GET("/v1/admin/requests", { params: { query: { status: "failed", limit: 5 } } })),
    refetchInterval: 30_000,
  });
  return (
    <Section
      title={t("overview.recentErrors")}
      actions={
        <Link className="button button-small" to="/requests">
          {t("nav.requests")}
        </Link>
      }
    >
      <QueryView query={failures}>
        {(data) =>
          data.items.length === 0 ? (
            <EmptyState title={t("overview.noRecentErrors")} />
          ) : (
            <ul className="plain-list">
              {data.items.map((record) => (
                <li key={record.id}>
                  <Link to={`/requests/${encodeURIComponent(record.id)}`}>
                    <DateTime value={record.created_at} />
                  </Link>{" "}
                  <code>{record.operation}</code> <RequestStatusBadges record={record} />{" "}
                  {record.error === null ? null : (
                    <code className="muted">{record.error.code}</code>
                  )}
                </li>
              ))}
            </ul>
          )
        }
      </QueryView>
    </Section>
  );
}
