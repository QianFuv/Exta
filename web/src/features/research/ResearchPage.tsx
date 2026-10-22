/** Research list with state filter and the admin creation form. */
import { useState } from "react";
import { Link } from "react-router";
import { call, useApi } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { useCursorList } from "../../lib/useCursorList";
import { EmptyState, ErrorState, LoadingState } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { PageHeader, RequirePermission, Section } from "../../components/Page";
import { DateTime } from "../../components/Values";
import { ResearchCreateForm } from "./ResearchCreateForm";
import { JOB_STATES, JobStateBadge } from "./jobStatus";
import type { JobState } from "./jobStatus";

/**
 * Research page.
 *
 * @returns Page element.
 */
export function ResearchPage() {
  const { session } = useSession();
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("research.title")} description={t("research.description")} />
      <Section title={t("research.createTitle")} description={t("research.createDescription")}>
        <ResearchCreateForm key={session?.principal_id} />
      </Section>
      <RequirePermission permission="admin:read">
        <ResearchList />
      </RequirePermission>
    </>
  );
}

/**
 * Paginated job list.
 *
 * @returns List section.
 */
function ResearchList() {
  const { t } = useI18n();
  const api = useApi();
  const { can } = useSession();
  const [state, setState] = useState<JobState | "">("");
  const { query, items } = useCursorList(
    ["research", "list", state],
    (cursor) =>
      call(
        api.GET("/v1/admin/research", {
          params: {
            query: {
              ...(cursor === undefined ? {} : { cursor }),
              ...(state === "" ? {} : { state }),
            },
          },
        }),
      ),
    { enabled: can("admin:read") },
  );

  return (
    <Section
      title={t("research.listTitle")}
      actions={
        <label className="inline-field">
          <span>{t("research.filterState")}</span>
          <select value={state} onChange={(event) => setState(event.target.value as JobState | "")}>
            <option value="">{t("common.all")}</option>
            {JOB_STATES.map((entry) => (
              <option key={entry} value={entry}>
                {t(`jobState.${entry}`)}
              </option>
            ))}
          </select>
        </label>
      }
    >
      {query.isPending ? (
        <LoadingState />
      ) : query.isError && items.length === 0 ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState title={t("research.emptyTitle")} description={t("research.emptyDescription")} />
      ) : (
        <div className="table-wrap">
          <table className="table">
            <caption className="visually-hidden">{t("research.listTitle")}</caption>
            <thead>
              <tr>
                <th scope="col">{t("common.id")}</th>
                <th scope="col">{t("research.profile")}</th>
                <th scope="col">{t("common.state")}</th>
                <th scope="col">{t("research.holds")}</th>
                <th scope="col">{t("common.created")}</th>
                <th scope="col">{t("common.updated")}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((job) => (
                <tr key={job.id}>
                  <td>
                    <Link to={`/research/${encodeURIComponent(job.id)}`}>
                      <code>{job.id}</code>
                    </Link>
                  </td>
                  <td>{t(`profile.${job.profile}`)}</td>
                  <td>
                    <JobStateBadge job={job} />
                  </td>
                  <td>
                    <span className="hold-summary">
                      {job.active_slot_held ? t("research.slotHeld") : t("research.slotReleased")}
                      {" · "}
                      {t(`budgetHold.${job.budget_hold_state}`)}
                    </span>
                  </td>
                  <td>
                    <DateTime value={job.created_at} />
                  </td>
                  <td>
                    <DateTime value={job.updated_at} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <LoadMore query={query} />
    </Section>
  );
}
