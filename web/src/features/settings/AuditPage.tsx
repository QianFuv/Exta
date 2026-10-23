import { Button, Disclosure, Input } from "../../components/Controls";
/** Redacted audit history with action/entity filters. */
import { useId, useState } from "react";
import { call, useApi } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useCursorList } from "../../lib/useCursorList";
import { EmptyState, ErrorState, LoadingState } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { PageHeader, RequirePermission, Section } from "../../components/Page";
import { DateTime, JsonBlock } from "../../components/Values";
import type { MessageKey } from "../../lib/messages";

const ACTION_LABELS: Record<string, MessageKey> = {
  "token.create": "audit.tokenCreate",
  "token.revoke": "audit.tokenRevoke",
  "key.create": "audit.keyCreate",
  "key.update": "audit.keyUpdate",
  "key.rotate": "audit.keyRotate",
  "key.retire": "audit.keyRetire",
  "settings.update": "audit.settingsUpdate",
  "route.replace": "audit.routeReplace",
};

/**
 * Audit page; requires `admin:audit:read` before any request is made.
 *
 * @returns Page element.
 */
export function AuditPage({ embedded = false }: { embedded?: boolean }) {
  const { t } = useI18n();
  return (
    <>
      {embedded ? null : (
        <PageHeader title={t("audit.title")} description={t("audit.description")} />
      )}
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
              <label id={`${formId}-action` + "-label"} htmlFor={`${formId}-action`}>
                {t("audit.action")}
              </label>
              <Input
                id={`${formId}-action`}
                value={draft.action}
                placeholder="key.rotate"
                onChange={(event) => setDraft({ ...draft, action: event.target.value })}
              />
            </div>
            <div className="field">
              <label id={`${formId}-entity` + "-label"} htmlFor={`${formId}-entity`}>
                {t("audit.entity")}
              </label>
              <Input
                id={`${formId}-entity`}
                value={draft.entityId}
                placeholder="key_…"
                onChange={(event) => setDraft({ ...draft, entityId: event.target.value })}
              />
            </div>
          </div>
          <div className="button-row">
            <Button type="submit" className="button button-primary">
              {t("common.apply")}
            </Button>
            <Button
              type="button"
              onClick={() => {
                setDraft({ action: "", entityId: "" });
                setFilters({ action: "", entityId: "" });
              }}
            >
              {t("common.reset")}
            </Button>
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
                  <strong>
                    {ACTION_LABELS[event.action] ? t(ACTION_LABELS[event.action]!) : event.action}
                  </strong>
                </div>
                <Disclosure label={t("audit.changes")}>
                  <p className="muted">
                    <code>{event.action}</code>
                    {event.entity_id ? (
                      <>
                        {" "}
                        · <code>{event.entity_id}</code>
                      </>
                    ) : null}
                  </p>
                  <p className="muted">
                    {t("audit.actor")}: <code>{event.actor_principal_id}</code>
                  </p>
                  <JsonBlock value={event.changes} />
                </Disclosure>
              </li>
            ))}
          </ol>
        )}
        <LoadMore query={query} />
      </Section>
    </>
  );
}
