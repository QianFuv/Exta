/** Account budget policy editing belongs to configuration, without touching the ledger. */
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
import { AccountDialog } from "../keys/KeyDialogs";

/** Display versioned account policies with their own units and existing write permissions. */
export function BudgetSettings() {
  const { t } = useI18n();
  const api = useApi();
  const { can } = useSession();
  const format = useUnitFormatter();
  const [editing, setEditing] = useState<Schemas["Account"] | null>(null);
  const { query, items } = useCursorList(["accounts", "budget-settings"], (cursor) =>
    call(
      api.GET("/v1/admin/accounts", {
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
              <table className="table">
                <thead>
                  <tr>
                    <th>{t("usage.account")}</th>
                    <th>{t("common.provider")}</th>
                    <th>{t("keys.allocation")}</th>
                    <th>{t("keys.perOperation")}</th>
                    <th>{t("keys.activeJobs")}</th>
                    <th>{t("common.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((account) => (
                    <tr key={account.id}>
                      <td>
                        <strong>{account.label}</strong>
                        <small className="cell-secondary">{account.id}</small>
                      </td>
                      <td>{t(`provider.${account.provider}`)}</td>
                      <td>{format(account.quota_policy.allocation, account.quota_policy.unit)}</td>
                      <td>
                        {format(
                          account.quota_policy.per_operation_reservation,
                          account.quota_policy.unit,
                        )}
                      </td>
                      <td>{account.quota_policy.active_jobs}</td>
                      <td>
                        <button
                          className="button button-small"
                          disabled={!can("admin:accounts:write")}
                          onClick={() => setEditing(account)}
                        >
                          {t("common.edit")}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </QueryView>
      <LoadMore query={query} />
      {editing ? (
        <AccountDialog
          provider={editing.provider}
          account={editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </Section>
  );
}
