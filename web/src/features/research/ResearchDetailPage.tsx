/** Research job detail: state, separate holds, actions, report, citations and events. */
import { Suspense, lazy } from "react";
import { Link, useParams } from "react-router";
import { useInfiniteQuery } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { EmptyState, ErrorState, LoadingState } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { LockedState } from "../../components/Feedback";
import { PageHeader, Section } from "../../components/Page";
import { toSafeHref } from "../../lib/safeUrl";
import { DateTime, JsonBlock, RouteRevision } from "../../components/Values";
import { HoldsPanel, JobStateBadge, JobStateNotices, isPollingState } from "./jobStatus";
import type { Job } from "./jobStatus";
import { JobActions } from "./JobActions";

const SafeMarkdown = lazy(() =>
  import("../../components/SafeMarkdown").then((module) => ({ default: module.SafeMarkdown })),
);

const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/;

/**
 * Polling delay with backoff for a progressing job: 5 s doubling every three updates up
 * to 60 s. Polling stops for expired and terminal states; TanStack Query also
 * pauses it while the tab is hidden.
 *
 * @param job - Latest job, if loaded.
 * @param updateCount - Successful fetch count.
 * @returns Delay in milliseconds or false.
 */
export function pollingDelay(job: Job | undefined, updateCount: number): number | false {
  if (job === undefined || !isPollingState(job.state)) {
    return false;
  }
  return Math.min(5000 * 2 ** Math.floor(updateCount / 3), 60_000);
}

/**
 * Detail page for one job.
 *
 * @returns Page element.
 */
export function ResearchDetailPage() {
  const { t } = useI18n();
  const { id = "" } = useParams();
  const { can } = useSession();
  const isValidId = ID_PATTERN.test(id);
  if (!can("admin:read")) {
    return (
      <>
        <PageHeader title={t("research.detailTitle")} />
        <LockedState permission="admin:read" />
      </>
    );
  }
  if (!isValidId) {
    return (
      <>
        <PageHeader title={t("research.detailTitle")} />
        <EmptyState title={t("error.not_found")} />
      </>
    );
  }
  return <ResearchDetail id={id} />;
}

/**
 * Loaded detail view.
 *
 * @param props - Job ID.
 * @returns Detail content.
 */
function ResearchDetail({ id }: { id: string }) {
  const { t } = useI18n();
  const api = useApi();
  const query = useInfiniteQuery({
    queryKey: ["research-detail", id],
    queryFn: ({ pageParam }) =>
      call(
        api.GET("/v1/admin/research/{id}", {
          params: {
            path: { id },
            query: pageParam === undefined ? {} : { events_cursor: pageParam },
          },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.events_next_cursor ?? undefined,
    refetchInterval: (current) =>
      pollingDelay(current.state.data?.pages[0]?.job, current.state.dataUpdateCount),
  });

  const job = query.data?.pages[0]?.job;
  const events = query.data?.pages.flatMap((page) => page.events) ?? [];

  return (
    <>
      <PageHeader
        title={t("research.detailTitle")}
        description={id}
        actions={
          <>
            <button
              type="button"
              className="button"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t("common.refresh")}
            </button>
            <Link className="button" to="/research">
              {t("research.backToList")}
            </Link>
          </>
        }
      />
      {query.isPending ? (
        <LoadingState />
      ) : job === undefined ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <>
          {query.isRefetchError ? (
            <ErrorState error={query.error} title={t("common.staleData")} />
          ) : null}
          <Section title={t("research.status")} actions={<JobStateBadge job={job} />}>
            <JobStateNotices job={job} />
            <JobSummary job={job} />
            <JobActions job={job} />
            {isPollingState(job.state) ? (
              <p className="muted" role="status" aria-live="polite">
                {query.isFetching ? t("research.refreshing") : t("research.autoRefresh")}
              </p>
            ) : null}
          </Section>
          <Section title={t("research.holds")}>
            <HoldsPanel job={job} />
          </Section>
          <Section title={t("research.report")}>
            <ReportView job={job} />
          </Section>
          <Section title={t("research.events")}>
            {events.length === 0 ? (
              <EmptyState title={t("research.noEvents")} />
            ) : (
              <ol className="timeline">
                {events.map((event) => (
                  <li key={event.id}>
                    <div className="timeline-head">
                      <DateTime value={event.created_at} />
                      <span>
                        {event.from_state === null ? "—" : t(`jobState.${event.from_state}`)} →{" "}
                        <strong>{t(`jobState.${event.to_state}`)}</strong>
                      </span>
                    </div>
                    <p>{event.reason}</p>
                    {event.actor_principal_id === null ? null : (
                      <p className="muted">
                        {t("research.actor")}: <code>{event.actor_principal_id}</code>
                        {event.action_ref === null ? null : (
                          <>
                            {" · "}
                            <code>{event.action_ref}</code>
                          </>
                        )}
                      </p>
                    )}
                  </li>
                ))}
              </ol>
            )}
            <LoadMore query={query} />
          </Section>
        </>
      )}
    </>
  );
}

/**
 * Key job metadata. Provider status strings are shown verbatim as open values.
 *
 * @param props - Job.
 * @returns Definition list.
 */
function JobSummary({ job }: { job: Job }) {
  const { t, formatNumber } = useI18n();
  return (
    <dl className="details-grid">
      <dt>{t("research.profile")}</dt>
      <dd>{t(`profile.${job.profile}`)}</dd>
      <dt>{t("common.provider")}</dt>
      <dd>{t(`provider.${job.source_provider}`)}</dd>
      <dt>{t("research.upstreamJobId")}</dt>
      <dd>
        {job.upstream_job_id === null ? (
          <span className="muted">{t("research.upstreamIdUnknown")}</span>
        ) : (
          <code>{job.upstream_job_id}</code>
        )}
      </dd>
      <dt>{t("research.upstreamState")}</dt>
      <dd>
        {job.upstream_state === null ? (
          <span className="muted">—</span>
        ) : (
          <code>{job.upstream_state}</code>
        )}{" "}
        <span className="muted">{t("research.providerReported")}</span>
      </dd>
      <dt>{t("research.completionReason")}</dt>
      <dd>
        {job.completion_reason === null ? (
          <span className="muted">—</span>
        ) : (
          <code>{job.completion_reason}</code>
        )}
      </dd>
      <dt>{t("routing.revision")}</dt>
      <dd>
        <RouteRevision revision={job.route_revision} />
      </dd>
      <dt>{t("research.observationDeadline")}</dt>
      <dd>
        <DateTime value={job.observation_deadline} />
      </dd>
      <dt>{t("research.pollingCount")}</dt>
      <dd>{formatNumber(job.polling_count)}</dd>
      <dt>{t("research.owner")}</dt>
      <dd>
        <code>{job.owner_principal_id}</code>
      </dd>
      <dt>{t("research.idempotencyExpires")}</dt>
      <dd>
        <DateTime value={job.idempotency_expires_at} />
      </dd>
      <dt>{t("common.created")}</dt>
      <dd>
        <DateTime value={job.created_at} />
      </dd>
      <dt>{t("common.updated")}</dt>
      <dd>
        <DateTime value={job.updated_at} />
      </dd>
      <dt>{t("common.version")}</dt>
      <dd>{job.version}</dd>
    </dl>
  );
}

/**
 * Render the stored report with safe Markdown, structured output and citations.
 *
 * @param props - Job.
 * @returns Report content or an honest empty state.
 */
function ReportView({ job }: { job: Job }) {
  const { t } = useI18n();
  const report = job.result;
  if (report === null) {
    const isTerminal =
      job.state === "succeeded" || job.state === "failed" || job.state === "cancelled";
    return (
      <EmptyState
        title={isTerminal ? t("research.reportUnavailable") : t("research.reportPending")}
        description={
          isTerminal ? t("research.reportUnavailableDetail") : t("research.reportPendingDetail")
        }
      />
    );
  }
  return (
    <div className="report">
      {report.markdown === null ? (
        <p className="muted">{t("research.noMarkdown")}</p>
      ) : (
        <Suspense fallback={<LoadingState />}>
          <SafeMarkdown source={report.markdown} />
        </Suspense>
      )}
      {report.structured_output === null ? null : (
        <>
          <h3>{t("research.structuredOutput")}</h3>
          <JsonBlock value={report.structured_output} />
        </>
      )}
      <h3>{t("research.citations")}</h3>
      {report.citations.length === 0 ? (
        <p className="muted">{t("research.noCitations")}</p>
      ) : (
        <ol className="citations">
          {report.citations.map((citation) => (
            <CitationItem key={citation.id} citation={citation} />
          ))}
        </ol>
      )}
    </div>
  );
}

/**
 * One citation; unsafe URLs are shown as text rather than links.
 *
 * @param props - Citation.
 * @returns List item.
 */
function CitationItem({ citation }: { citation: Schemas["Citation"] }) {
  const { t } = useI18n();
  const href = toSafeHref(citation.url);
  const label = citation.title ?? citation.url;
  return (
    <li>
      {href ? (
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer nofollow"
          referrerPolicy="no-referrer"
        >
          {label}
        </a>
      ) : (
        <span>{label}</span>
      )}{" "}
      <span className="muted">
        ({t(`provider.${citation.source_provider}`)} · <code>{citation.url}</code>)
      </span>
    </li>
  );
}
