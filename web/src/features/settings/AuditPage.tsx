/** Redacted audit history with action/entity filters. */
import { useId, useState } from "react";
import { call, useApi } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useCursorList } from "../../lib/useCursorList";
import { EmptyState, ErrorState, LoadingState } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { PageHeader, RequirePermission, Section } from "../../components/Page";
import { DateTime, JsonBlock } from "../../components/Values";

/**
 * Audit page; requires `admin:audit:read` before any request is made.
 *
 * @returns Page element.
 */
export function AuditPage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("audit.title")} description={t("audit.description")} />
      <RequirePermission permission="admin:audit:read">
        <AuditContent />
      </RequirePermission>
    </>
  );
}

/**
 * Filters and event list.
 *
 * @returns Sections.
 */
function AuditContent() {
  const { t } = useI18n();
  const api = useApi();
  const formId = useId();
  const [draft, setDraft] = useState({ action: "", entityId: "" });
  const [filters, setFilters] = useState({ action: "", entityId: "" });
  const { query, items } = useCursorList(["audit", filters], (cursor) =>
    call(
      api.GET("/v1/admin/audit", {
        params: {
          query: {
            ...(cursor === undefined ? {} : { cursor }),
            ...(filters.action === "" ? {} : { action: filters.action }),
            ...(filters.entityId === "" ? {} : { entity_id: filters.entityId }),
          },
        },
      }),
    ),
  );
  return (
    <>
      <Section title={t("requests.filters")}>
        <form
          className="form"
          onSubmit={(event) => {
            event.preventDefault();
            setFilters({ action: draft.action.trim(), entityId: draft.entityId.trim() });
          }}
        >
          <div className="field-row">
            <div className="field">
              <label htmlFor={`${formId}-action`}>{t("audit.action")}</label>
              <input
                id={`${formId}-action`}
                value={draft.action}
                onChange={(event) => setDraft({ ...draft, action: event.target.value })}
              />
            </div>
            <div className="field">
              <label htmlFor={`${formId}-entity`}>{t("audit.entity")}</label>
              <input
                id={`${formId}-entity`}
                value={draft.entityId}
                onChange={(event) => setDraft({ ...draft, entityId: event.target.value })}
              />
            </div>
          </div>
          <div className="button-row">
            <button type="submit" className="button button-primary">
              {t("common.apply")}
            </button>
          </div>
        </form>
      </Section>
      <Section title={t("audit.events")}>
        {query.isPending ? (
          <LoadingState />
        ) : query.isError && items.length === 0 ? (
          <ErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState title={t("audit.emptyTitle")} />
        ) : (
          <ol className="timeline">
            {items.map((event) => (
              <li key={event.id}>
                <div className="timeline-head">
                  <DateTime value={event.created_at} />
                  <code>{event.action}</code>
                  {event.entity_id === null ? null : (
                    <code className="muted">{event.entity_id}</code>
                  )}
                </div>
                <p className="muted">
                  {t("audit.actor")}: <code>{event.actor_principal_id}</code>
                </p>
                {Object.keys(event.changes).length === 0 ? null : (
                  <details>
                    <summary>{t("audit.changes")}</summary>
                    <JsonBlock value={event.changes} />
                  </details>
                )}
              </li>
            ))}
          </ol>
        )}
        <LoadMore query={query} />
      </Section>
    </>
  );
}
