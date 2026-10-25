import { DiagnosticReason } from "../../components/DiagnosticReason";
import { DataTable } from "../../components/DataTable";
import type { ColumnProps } from "@douyinfe/semi-ui/lib/es/table";
import { Button, Input, Select } from "../../components/Controls";
/** Sanitized request history with time/operation/provider/status filters. */
import { useId, useState } from "react";
import type { FormEvent } from "react";
import { Link, useSearchParams } from "react-router";
import { Dialog } from "../../components/Dialog";
import { RequestDetail } from "./RequestDetailPage";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useCursorList } from "../../lib/useCursorList";
import { EmptyState, ErrorState, LoadingState } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { PageHeader, RequirePermission, Section } from "../../components/Page";
import { DateTime } from "../../components/Values";
import { RequestStatusBadges, ResultSummary } from "./requestStatus";
import { useMediaQuery } from "../../lib/useMediaQuery";
import IconFilter from "@douyinfe/semi-icons/lib/es/icons/IconFilter";

type Filters = {
  requestId: string;
  operation: Schemas["Operation"] | "";
  provider: Schemas["Provider"] | "";
  status: Schemas["RequestStatus"] | "";
  createdAfter: string;
  createdBefore: string;
};

const EMPTY_FILTERS: Filters = {
  requestId: "",
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
      <PageHeader title={t("requests.title")} help={t("requests.noRawContent")} />
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
  const columns = useRequestColumns();
  const { t } = useI18n();
  const api = useApi();
  const formId = useId();
  const [params, setParams] = useSearchParams();
  const selectedId = params.get("request");
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
            ...(filters.requestId === "" ? {} : { request_id: filters.requestId }),
            ...(filters.provider === "" ? {} : { provider: filters.provider }),
            ...(filters.status === "" ? {} : { status: filters.status }),
            ...(createdAfter === undefined ? {} : { created_after: createdAfter }),
            ...(createdBefore === undefined ? {} : { created_before: createdBefore }),
          },
        },
      }),
    ),
  );

  const isInvalidRange =
    (draft.createdAfter !== "" && toInstant(draft.createdAfter) === undefined) ||
    (draft.createdBefore !== "" && toInstant(draft.createdBefore) === undefined) ||
    (draft.createdAfter !== "" &&
      draft.createdBefore !== "" &&
      draft.createdAfter >= draft.createdBefore);

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!isInvalidRange) setFilters(draft);
  };

  const isFiltered = Object.values(filters).some((value) => value !== "");
  const activeFilterCount = Object.values(filters).filter((value) => value !== "").length;
  const isPhone = useMediaQuery("(max-width: 760px)");
  const [isFiltersOpen, setFiltersOpen] = useState(false);
  const isCollapsed = isPhone && !isFiltersOpen;

  return (
    <>
      <Section
        title={t("requests.listTitle")}
        actions={
          <Button
            type="button"
            className="button button-small"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            {t("common.refresh")}
          </Button>
        }
      >
        <form
          className="form filters log-filters"
          onSubmit={onSubmit}
          data-collapsed={isCollapsed}
          aria-label={t("requests.filters")}
        >
          <Button
            type="button"
            className="button filters-toggle"
            aria-expanded={!isCollapsed}
            aria-controls={`${formId}-filter-fields`}
            onClick={() => setFiltersOpen(!isFiltersOpen)}
          >
            <IconFilter aria-hidden />
            {isCollapsed ? t("requests.showFilters") : t("requests.hideFilters")}
            {activeFilterCount > 0 ? (
              <span className="muted">
                {" · "}
                {t("requests.activeFilters", { count: activeFilterCount })}
              </span>
            ) : null}
          </Button>
          <div className="filter-fields" id={`${formId}-filter-fields`}>
            <div className="field">
              <label id={`${formId}-request-id-label`} htmlFor={`${formId}-request-id`}>
                {t("error.requestId")}
              </label>
              <Input
                id={`${formId}-request-id`}
                maxLength={128}
                value={draft.requestId}
                onChange={(event) => setDraft({ ...draft, requestId: event.target.value })}
              />
            </div>
            <div className="field">
              <label id={`${formId}-operation` + "-label"} htmlFor={`${formId}-operation`}>
                {t("requests.operation")}
              </label>
              <Select
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
              </Select>
            </div>
            <div className="field">
              <label id={`${formId}-provider` + "-label"} htmlFor={`${formId}-provider`}>
                {t("common.provider")}
              </label>
              <Select
                id={`${formId}-provider`}
                value={draft.provider}
                onChange={(event) =>
                  setDraft({ ...draft, provider: event.target.value as Filters["provider"] })
                }
              >
                <option value="">{t("common.all")}</option>
                <option value="exa">{t("provider.exa")}</option>
                <option value="tavily">{t("provider.tavily")}</option>
              </Select>
            </div>
            <div className="field">
              <label id={`${formId}-status` + "-label"} htmlFor={`${formId}-status`}>
                {t("common.state")}
              </label>
              <Select
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
              </Select>
            </div>
            <div className="field">
              <label id={`${formId}-after` + "-label"} htmlFor={`${formId}-after`}>
                {t("requests.createdAfter")}
              </label>
              <Input
                id={`${formId}-after`}
                aria-invalid={isInvalidRange}
                aria-describedby={isInvalidRange ? `${formId}-range-error` : undefined}
                type="datetime-local"
                value={draft.createdAfter}
                onChange={(event) => setDraft({ ...draft, createdAfter: event.target.value })}
              />
            </div>
            <div className="field">
              <label id={`${formId}-before` + "-label"} htmlFor={`${formId}-before`}>
                {t("requests.createdBefore")}
              </label>
              <Input
                id={`${formId}-before`}
                aria-invalid={isInvalidRange}
                aria-describedby={isInvalidRange ? `${formId}-range-error` : undefined}
                type="datetime-local"
                value={draft.createdBefore}
                onChange={(event) => setDraft({ ...draft, createdBefore: event.target.value })}
              />
            </div>
          </div>
          <div className="button-row">
            <Button type="submit" className="button button-primary" disabled={isInvalidRange}>
              {t("common.apply")}
            </Button>
            <Button
              type="button"
              className="button"
              onClick={() => {
                setDraft(EMPTY_FILTERS);
                setFilters(EMPTY_FILTERS);
              }}
            >
              {t("common.reset")}
            </Button>
          </div>
          {isInvalidRange ? (
            <p id={`${formId}-range-error`} className="field-error" role="alert">
              {t("requests.dateRangeError")}
            </p>
          ) : null}
        </form>
        <div className="table-meta">
          <span>{t("requests.count", { count: items.length })}</span>
          <span>{t("requests.detailHint")}</span>
        </div>
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
          <div
            className="table-wrap"
            role="region"
            aria-label={t("requests.listTitle")}
            tabIndex={0}
          >
            <DataTable
              dataSource={items}
              rowKey={(record) => record.id}
              columns={columns}
              mobileRow={(record) => (
                <>
                  <div className="mobile-record-heading">
                    <Link to={`?request=${encodeURIComponent(record.id)}`}>{record.operation}</Link>
                    <RequestStatusBadges record={record} />
                  </div>
                  <p className="cell-secondary">
                    <DateTime value={record.created_at} /> ·{" "}
                    {record.source_provider ? t(`provider.${record.source_provider}`) : "—"}
                  </p>
                  {record.error ? (
                    <DiagnosticReason reason={record.error.code} />
                  ) : (
                    <ResultSummary record={record} />
                  )}
                </>
              )}
            />
          </div>
        )}
        <LoadMore query={query} />
      </Section>
      {selectedId !== null && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(selectedId) ? (
        <Dialog
          title={t("requests.detailTitle")}
          description={selectedId}
          variant="drawer"
          size="large"
          onClose={() => {
            const next = new URLSearchParams(params);
            next.delete("request");
            setParams(next);
          }}
        >
          <RequestDetail id={selectedId} />
        </Dialog>
      ) : null}
    </>
  );
}

/**
 * One request row.
 *
 * @param props - Request record.
 * @returns Table row.
 */
/** Build typed Semi columns with the existing permissions and evidence semantics. */
function useRequestColumns(): ColumnProps<Schemas["RequestRecord"]>[] {
  const { t, formatDuration } = useI18n();
  return [
    {
      key: "column-0",
      title: <>{t("common.created")}</>,
      render: (_value, record) => {
        return (
          <>
            <Link to={`?request=${encodeURIComponent(record.id)}`}>
              <DateTime value={record.created_at} />
            </Link>
            <br />
            <code className="muted table-id" title={record.id}>
              {record.id}
            </code>
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
          </>
        );
      },
    },
    {
      key: "column-2",
      title: <>{t("common.provider")}</>,
      render: (_value, record) => {
        return (
          <>
            {record.source_provider === null ? (
              <span className="muted">{t("requests.noProvider")}</span>
            ) : (
              t(`provider.${record.source_provider}`)
            )}
          </>
        );
      },
    },
    {
      key: "column-3",
      title: <>{t("common.state")}</>,
      render: (_value, record) => {
        return (
          <>
            <RequestStatusBadges record={record} />
            {record.error === null ? null : (
              <>
                <br />
                <DiagnosticReason reason={record.error.code} />
              </>
            )}
          </>
        );
      },
    },
    {
      key: "column-4",
      title: <>{t("requests.results")}</>,
      render: (_value, record) => {
        return (
          <>
            <ResultSummary record={record} />
          </>
        );
      },
    },
    {
      key: "column-5",
      title: <>{t("requests.duration")}</>,
      render: (_value, record) => {
        return <>{formatDuration(record.duration_ms)}</>;
      },
    },
  ];
}
