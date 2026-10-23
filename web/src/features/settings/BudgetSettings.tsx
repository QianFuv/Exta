import { DataTable } from "../../components/DataTable";
import { Button } from "../../components/Controls";
/** Key budget policy editing belongs to configuration, without touching the ledger. */
import { useState } from "react";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { useCursorList } from "../../lib/useCursorList";
import { QueryView, EmptyState } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { LoadMore } from "../../components/LoadMore";
import { useUnitFormatter } from "../../components/Values";
import { KeySettingsDialog } from "../keys/KeyDialogs";

/** Display versioned providerKey policies with their own units and existing write permissions. */
export function BudgetSettings() {
  const { t } = useI18n();
  const api = useApi();
  const { can } = useSession();
  const format = useUnitFormatter();
  const [editing, setEditing] = useState<Schemas["UpstreamKey"] | null>(null);
  const { query, items } = useCursorList(["keys", "budget-settings"], (cursor) =>
    call(
      api.GET("/v1/admin/keys", {
        params: { query: { limit: 100, ...(cursor ? { cursor } : {}) } },
      }),
    ),
  );
  return (
    <Section title={t("keys.budgetSettings")} description={t("usage.unitsNeverCombined")}>
      <QueryView query={query}>
        {() =>
          items.length === 0 ? (
            <EmptyState title={t("keys.emptyDescription")} />
          ) : (
            <div
              className="table-wrap"
              role="region"
              aria-label={t("keys.budgetSettings")}
              tabIndex={0}
            >
              <DataTable
                dataSource={items}
                rowKey={(providerKey) => providerKey.id}
                columns={[
                  {
                    key: "column-0",
                    title: <>{t("keys.key")}</>,
                    render: (_value, providerKey) => {
                      return (
                        <>
                          <strong>{providerKey.label}</strong>
                          <small className="cell-secondary">
                            <code>{providerKey.id}</code>
                          </small>
                        </>
                      );
                    },
                  },
                  {
                    key: "column-1",
                    title: <>{t("common.provider")}</>,
                    render: (_value, providerKey) => {
                      return <>{t(`provider.${providerKey.provider}`)}</>;
                    },
                  },
                  {
                    key: "column-2",
                    title: <>{t("keys.allocation")}</>,
                    render: (_value, providerKey) => {
                      return (
                        <>
                          {format(
                            providerKey.quota_policy.allocation,
                            providerKey.quota_policy.unit,
                          )}
                        </>
                      );
                    },
                  },
                  {
                    key: "column-3",
                    title: <>{t("keys.perOperation")}</>,
                    render: (_value, providerKey) => {
                      return (
                        <>
                          {format(
                            providerKey.quota_policy.per_operation_reservation,
                            providerKey.quota_policy.unit,
                          )}
                        </>
                      );
                    },
                  },
                  {
                    key: "column-4",
                    title: <>{t("keys.activeJobs")}</>,
                    render: (_value, providerKey) => {
                      return <>{providerKey.quota_policy.active_jobs}</>;
                    },
                  },
                  {
                    key: "column-5",
                    title: <>{t("common.actions")}</>,
                    render: (_value, providerKey) => {
                      return (
                        <>
                          <Button
                            className="button button-small"
                            disabled={!can("admin:keys:write")}
                            onClick={() => setEditing(providerKey)}
                          >
                            {t("common.edit")}
                          </Button>
                        </>
                      );
                    },
                  },
                ]}
              />
            </div>
          )
        }
      </QueryView>
      <LoadMore query={query} />
      {editing ? (
        <KeySettingsDialog providerKey={editing} onClose={() => setEditing(null)} />
      ) : null}
    </Section>
  );
}
