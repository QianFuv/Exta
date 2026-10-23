/** Sanitized request detail with per-attempt timeline. */
import { Link, useParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import {
  Badge,
  EmptyState,
  ErrorState,
  LoadingState,
  LockedState,
  Notice,
} from "../../components/Feedback";
import type { Tone } from "../../components/Feedback";
import { PageHeader, Section } from "../../components/Page";
import { DateTime, RouteRevision, UsageAmountView } from "../../components/Values";
import { RequestStatusBadges, ResultSummary } from "./requestStatus";

const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/;

const TRANSMISSION_TONE: Record<Schemas["Attempt"]["transmission_state"], Tone> = {
  not_sent: "neutral",
  possibly_sent: "warning",
  response_received: "info",
};

/**
 * Request detail page.
 *
 * @returns Page element.
 */
export function RequestDetailPage() {
  const { t } = useI18n();
  const { id = "" } = useParams();
  const { can } = useSession();
  const header = (
    <PageHeader
      title={t("requests.detailTitle")}
      description={id}
      actions={
        <Link className="button" to="/requests">
          {t("requests.backToList")}
        </Link>
      }
    />
  );
  if (!can("admin:read")) {
    return (
      <>
        {header}
        <LockedState permission="admin:read" />
      </>
    );
  }
  if (!ID_PATTERN.test(id)) {
    return (
      <>
        {header}
        <EmptyState title={t("error.not_found")} />
      </>
    );
  }
  return (
    <>
      {header}
      <RequestDetail id={id} />
    </>
  );
}

/**
 * Loaded request detail.
 *
 * @param props - Request ID.
 * @returns Detail sections.
 */
export function RequestDetail({ id }: { id: string }) {
  const { t, formatDuration } = useI18n();
  const api = useApi();
  const query = useQuery({
    queryKey: ["request-detail", id],
    queryFn: () => call(api.GET("/v1/admin/requests/{id}", { params: { path: { id } } })),
  });
  if (query.isPending) {
    return <LoadingState />;
  }
  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  }
  const { request, attempts } = query.data;
  return (
    <>
      <Section
        title={t("requests.summary")}
        actions={
          <>
            <RequestStatusBadges record={request} />
            <button
              type="button"
              className="button"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t("common.refresh")}
            </button>
          </>
        }
      >
        {request.partial === true ? (
          <Notice tone="warning" title={t("requests.partialTitle")}>
            {t("requests.partialDetail")}
          </Notice>
        ) : null}
        {request.billing_uncertain ? (
          <Notice tone="warning" title={t("requests.billingUncertainTitle")}>
            {t("requests.billingUncertainDetail")}
          </Notice>
        ) : null}
        {request.error === null ? null : (
          <Notice tone="danger" title={t(`error.${request.error.code}`)}>
            <code>{request.error.code}</code>: {request.error.message}
          </Notice>
        )}
        <dl className="details-grid request-summary">
          <dt>{t("requests.operation")}</dt>
          <dd>
            <code>{request.operation}</code>
          </dd>
          <dt>{t("common.provider")}</dt>
          <dd>
            {request.source_provider === null
              ? t("requests.noProvider")
              : t(`provider.${request.source_provider}`)}
          </dd>
          <dt>{t("requests.results")}</dt>
          <dd>
            <ResultSummary record={request} />
          </dd>
          <dt>{t("requests.duration")}</dt>
          <dd>{formatDuration(request.duration_ms)}</dd>
          <dt>{t("routing.revision")}</dt>
          <dd>
            <RouteRevision revision={request.route_revision} />
          </dd>
          <dt>{t("common.created")}</dt>
          <dd>
            <DateTime value={request.created_at} />
          </dd>
          <dt>{t("requests.completedAt")}</dt>
          <dd>
            <DateTime value={request.completed_at} />
          </dd>
          <dt>{t("requests.usage")}</dt>
          <dd>
            {request.usage.length === 0 ? (
              <span className="muted">{t("requests.noUsage")}</span>
            ) : (
              <ul className="plain-list">
                {request.usage.map((usage, index) => (
                  <li key={`${usage.unit}-${usage.source}-${index}`}>
                    <UsageAmountView usage={usage} />{" "}
                    <span className="muted">({usage.source})</span>
                  </li>
                ))}
              </ul>
            )}
          </dd>
        </dl>
        <p className="muted">{t("requests.noRawContent")}</p>
      </Section>
      <Section title={t("requests.attempts")}>
        {attempts.length === 0 ? (
          <EmptyState
            title={t("requests.noAttempts")}
            description={t("requests.noAttemptsDetail")}
          />
        ) : (
          <ol className="timeline">
            {attempts.map((attempt) => (
              <li key={attempt.id}>
                <div className="timeline-head">
                  <strong>{t("requests.attemptNumber", { number: attempt.attempt_number })}</strong>
                  <span>{t(`provider.${attempt.provider}`)}</span>
                  <Badge tone={TRANSMISSION_TONE[attempt.transmission_state]}>
                    {t(`transmission.${attempt.transmission_state}`)}
                  </Badge>
                  {attempt.billing_uncertain ? (
                    <Badge tone="warning">{t("requests.billingUncertain")}</Badge>
                  ) : null}
                </div>
                <dl className="details-grid compact">
                  <dt>{t("common.created")}</dt>
                  <dd>
                    <DateTime value={attempt.created_at} />
                  </dd>
                  <dt>{t("requests.duration")}</dt>
                  <dd>{formatDuration(attempt.duration_ms)}</dd>
                  <dt>{t("requests.httpStatus")}</dt>
                  <dd>{attempt.http_status ?? "—"}</dd>
                  <dt>{t("requests.retryReason")}</dt>
                  <dd>{attempt.retry_reason ?? "—"}</dd>
                  <dt>{t("usage.account")}</dt>
                  <dd>
                    <code>{attempt.account_id}</code>
                  </dd>
                  <dt>{t("requests.credentialVersion")}</dt>
                  <dd>
                    <code>{attempt.credential_version_id}</code>
                  </dd>
                  <dt>{t("requests.upstreamRequestId")}</dt>
                  <dd>
                    {attempt.upstream_request_id === null ? (
                      "—"
                    ) : (
                      <code>{attempt.upstream_request_id}</code>
                    )}
                  </dd>
                </dl>
              </li>
            ))}
          </ol>
        )}
      </Section>
    </>
  );
}
