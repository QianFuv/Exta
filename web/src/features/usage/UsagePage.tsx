/** Local logical-call trends with an independently timestamped upstream refresh action. */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { useCursorList } from "../../lib/useCursorList";
import { Button } from "../../components/Controls";
import { InlineError, Notice } from "../../components/Feedback";
import { useNotify } from "../../components/Notifications";
import { PermissionHint } from "../../components/Page";
import { DateTime } from "../../components/Values";
import { CallTrend } from "./CallTrend";

type KeyUsage = Schemas["KeyUsage"];

/** Only successful upstream observations can establish the displayed refresh time. */
export function lastSuccessfulObservation(items: KeyUsage[]): string | null {
  return (
    items
      .filter(
        (item) =>
          item.source === "upstream" &&
          item.freshness !== "unknown" &&
          item.unavailable_reason === null &&
          item.observed_at !== null,
      )
      .map((item) => item.observed_at!)
      .sort((first, second) => Date.parse(second) - Date.parse(first))[0] ?? null
  );
}

/** Keep upstream refresh failures separate from the local calls chart. */
export function UsageContent() {
  const { t } = useI18n();
  const api = useApi();
  const usage = useCursorList(["usage"], (cursor) =>
    call(
      api.GET("/v1/admin/usage", {
        params: { query: { limit: 100, ...(cursor ? { cursor } : {}) } },
      }),
    ),
  );
  const refresh = useRefresh();
  const refreshedAt = lastSuccessfulObservation(usage.items);
  return (
    <>
      <div className="usage-refresh-toolbar">
        <Button
          onClick={() => refresh.mutation.mutate({})}
          disabled={!refresh.canRefresh || refresh.mutation.isPending}
          loading={refresh.mutation.isPending}
        >
          {t("usage.refreshAll")}
        </Button>
        <span className="muted">
          {t("usage.lastRefresh")}：
          {refreshedAt ? <DateTime value={refreshedAt} /> : t("usage.notRefreshed")}
        </span>
        <PermissionHint permission="admin:usage:refresh" />
        <RefreshStatus mutation={refresh.mutation} />
        {usage.query.isError ? <InlineError error={usage.query.error} /> : null}
      </div>
      {refresh.mutation.isSuccess &&
      usage.items.some((item) => item.source === "upstream" && item.unavailable_reason !== null) ? (
        <Notice tone="warning" title={t("usage.refreshIncomplete")} />
      ) : null}
      <CallTrend />
    </>
  );
}

/**
 * Refresh mutation with cooldown awareness.
 *
 * @returns Mutation and permission flag.
 */
function useRefresh() {
  const api = useApi();
  const { csrfToken, can } = useSession();
  const queryClient = useQueryClient();
  const notify = useNotify();
  const { t } = useI18n();
  const mutation = useMutation({
    mutationFn: (body: Schemas["KeyUsageRefresh"]) =>
      call(
        api.POST("/v1/admin/usage/refresh", {
          params: { header: { "X-Exta-CSRF": csrfToken } },
          body,
        }),
      ),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ["usage"] });
      notify("info", result.status === "coalesced" ? t("usage.coalesced") : t("usage.refreshed"));
    },
  });
  return { mutation, canRefresh: can("admin:usage:refresh") };
}

/**
 * Show the last refresh outcome, including the cooldown hint on 429.
 *
 * @param props - Refresh mutation.
 * @returns Status content.
 */
function RefreshStatus({
  mutation,
}: {
  mutation: { data: Schemas["UsageRefreshResult"] | undefined; error: unknown; isError: boolean };
}) {
  return mutation.isError ? <InlineError error={mutation.error} /> : null;
}
