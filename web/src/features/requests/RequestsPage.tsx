/** Sanitized request history with time/operation/provider/status filters. */
import { useId, useState } from "react";
import type { FormEvent } from "react";
import { Link } from "react-router";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useCursorList } from "../../lib/useCursorList";
import { EmptyState, ErrorState, LoadingState } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { PageHeader, RequirePermission, Section } from "../../components/Page";
import { DateTime, RouteRevision } from "../../components/Values";
import { RequestStatusBadges, ResultSummary } from "./requestStatus";

type Filters = {
  operation: Schemas["Operation"] | "";
  provider: Schemas["Provider"] | "";
  status: Schemas["RequestStatus"] | "";
  createdAfter: string;
  createdBefore: string;
};

const EMPTY_FILTERS: Filters = {
  operation: "",
  provider: "",
  status: "",
  createdAfter: "",
  createdBefore: "",
};
const OPERATIONS: readonly Schemas["Operation"][] = [
  "search",
  "advanced_search",
  "fetch",
  "map",
  "crawl",
  "research_create",
];
const STATUSES: readonly Schemas["RequestStatus"][] = [
  "accepted",
  "running",
  "succeeded",
  "failed",
  "uncertain",
];

/**
 * Convert a local datetime input value to RFC3339 UTC.
 *
 * @param value - `datetime-local` value.
 * @returns ISO string or undefined.
 */
function toInstant(value: string): string | undefined {
  if (value === "") {
    return undefined;
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

/**
 * Requests page.
 *
 * @returns Page element.
 */
export function RequestsPage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("requests.title")} description={t("requests.description")} />
      <RequirePermission permission="admin:read">
        <RequestsContent />
      </RequirePermission>
    </>
  );
}

/**
 * Filter form and list.
 *
 * @returns Sections.
 */
function RequestsContent() {
  const { t } = useI18n();
  const api = useApi();
  const formId = useId();
  const [draft, setDraft] = useState<Filters>(EMPTY_FILTERS);
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS);
  const createdAfter = toInstant(filters.createdAfter);
  const createdBefore = toInstant(filters.createdBefore);
  const { query, items } = useCursorList(["requests", filters], (cursor) =>
    call(
      api.GET("/v1/admin/requests", {
        params: {
          query: {
            ...(cursor === undefined ? {} : { cursor }),
            ...(filters.operation === "" ? {} : { operation: filters.operation }),
            ...(filters.provider === "" ? {} : { provider: filters.provider }),
            ...(filters.status === "" ? {} : { status: filters.status }),
            ...(createdAfter === undefined ? {} : { created_after: createdAfter }),
            ...(createdBefore === undefined ? {} : { created_before: createdBefore }),
          },
        },
      }),
    ),
  );

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFilters(draft);
  };

  const isFiltered = Object.values(filters).some((value) => value !== "");

  return (
    <>
      <Section title={t("requests.filters")}>
        <form className="form filters" onSubmit={onSubmit}>
          <div className="field-row">
            <div className="field">
              <label htmlFor={`${formId}-operation`}>{t("requests.operation")}</label>
              <select
                id={`${formId}-operation`}
                value={draft.operation}
                onChange={(event) =>
                  setDraft({ ...draft, operation: event.target.value as Filters["operation"] })
                }
              >
                <option value="">{t("common.all")}</option>
                {OPERATIONS.map((entry) => (
                  <option key={entry} value={entry}>
                    {entry}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor={`${formId}-provider`}>{t("common.provider")}</label>
              <select
                id={`${formId}-provider`}
                value={draft.provider}
                onChange={(event) =>
                  setDraft({ ...draft, provider: event.target.value as Filters["provider"] })
                }
              >
                <option value="">{t("common.all")}</option>
                <option value="exa">{t("provider.exa")}</option>
                <option value="tavily">{t("provider.tavily")}</option>
              </select>
            </div>
            <div className="field">
              <label htmlFor={`${formId}-status`}>{t("common.state")}</label>
              <select
                id={`${formId}-status`}
                value={draft.status}
                onChange={(event) =>
                  setDraft({ ...draft, status: event.target.value as Filters["status"] })
                }
              >
                <option value="">{t("common.all")}</option>
                {STATUSES.map((entry) => (
                  <option key={entry} value={entry}>
                    {t(`requestStatus.${entry}`)}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor={`${formId}-after`}>{t("requests.createdAfter")}</label>
              <input
                id={`${formId}-after`}
                type="datetime-local"
                value={draft.createdAfter}
                onChange={(event) => setDraft({ ...draft, createdAfter: event.target.value })}
              />
            </div>
            <div className="field">
              <label htmlFor={`${formId}-before`}>{t("requests.createdBefore")}</label>
              <input
                id={`${formId}-before`}
                type="datetime-local"
                value={draft.createdBefore}
                onChange={(event) => setDraft({ ...draft, createdBefore: event.target.value })}
              />
            </div>
          </div>
          <div className="button-row">
            <button type="submit" className="button button-primary">
              {t("common.apply")}
            </button>
            <button
              type="button"
              className="button"
              onClick={() => {
                setDraft(EMPTY_FILTERS);
                setFilters(EMPTY_FILTERS);
              }}
            >
              {t("common.reset")}
            </button>
          </div>
        </form>
      </Section>
      <Section title={t("requests.listTitle")} description={t("requests.noRawContent")}>
        {query.isPending ? (
          <LoadingState />
        ) : query.isError && items.length === 0 ? (
          <ErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState
            title={isFiltered ? t("requests.emptyFiltered") : t("requests.emptyTitle")}
            description={t("requests.emptyDescription")}
          />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">{t("common.created")}</th>
                  <th scope="col">{t("requests.operation")}</th>
                  <th scope="col">{t("common.provider")}</th>
                  <th scope="col">{t("common.state")}</th>
                  <th scope="col">{t("requests.results")}</th>
                  <th scope="col">{t("requests.duration")}</th>
                  <th scope="col">{t("routing.revision")}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((record) => (
                  <RequestRow key={record.id} record={record} />
                ))}
              </tbody>
            </table>
          </div>
        )}
        <LoadMore query={query} />
      </Section>
    </>
  );
}

/**
 * One request row.
 *
 * @param props - Request record.
 * @returns Table row.
 */
function RequestRow({ record }: { record: Schemas["RequestRecord"] }) {
  const { t, formatDuration } = useI18n();
  return (
    <tr>
      <td>
        <Link to={`/requests/${encodeURIComponent(record.id)}`}>
          <DateTime value={record.created_at} />
        </Link>
        <br />
        <code className="muted">{record.id}</code>
      </td>
      <td>
        <code>{record.operation}</code>
      </td>
      <td>
        {record.source_provider === null ? (
          <span className="muted">{t("requests.noProvider")}</span>
        ) : (
          t(`provider.${record.source_provider}`)
        )}
      </td>
      <td>
        <RequestStatusBadges record={record} />
        {record.error === null ? null : (
          <>
            <br />
            <code className="muted">{record.error.code}</code>
          </>
        )}
      </td>
      <td>
        <ResultSummary record={record} />
      </td>
      <td>{formatDuration(record.duration_ms)}</td>
      <td>
        <RouteRevision revision={record.route_revision} />
      </td>
    </tr>
  );
}
