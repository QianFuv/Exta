/** Usage by provider/account with separated units, sources, freshness and limit kinds. */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { useCursorList } from "../../lib/useCursorList";
import {
  Badge,
  EmptyState,
  ErrorState,
  InlineError,
  LoadingState,
  Notice,
} from "../../components/Feedback";
import type { Tone } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { useNotify } from "../../components/Notifications";
import { PageHeader, PermissionHint, RequirePermission, Section } from "../../components/Page";
import { DateTime, useUnitFormatter } from "../../components/Values";

type AccountUsage = Schemas["AccountUsage"];
type Provider = Schemas["Provider"];

const FRESHNESS_TONE: Record<AccountUsage["freshness"], Tone> = {
  fresh: "info",
  stale: "warning",
  unknown: "warning",
};
const SOURCE_TONE: Record<AccountUsage["source"], Tone> = {
  upstream: "info",
  local: "neutral",
  estimate: "warning",
};

/**
 * Find accounts whose local ledger and upstream snapshot disagree in the same unit.
 *
 * @param items - Usage rows.
 * @returns Keys `account|key|unit` that diverge.
 */
export function findDivergence(items: AccountUsage[]): Set<string> {
  const groups = new Map<
    string,
    { identity: string; local?: number | null; upstream?: number | null }
  >();
  for (const item of items) {
    if (item.source === "estimate" || item.period_start === null || item.period_end === null) {
      continue;
    }
    const identity = `${item.account_id}|${item.key_id ?? ""}|${item.unit}`;
    const key = `${identity}|${item.period_start}|${item.period_end}`;
    const group = groups.get(key) ?? { identity };
    group[item.source] = item.used;
    groups.set(key, group);
  }
  const diverging = new Set<string>();
  for (const group of groups.values()) {
    if (
      group.local !== undefined &&
      group.upstream !== undefined &&
      group.local !== null &&
      group.upstream !== null &&
      group.local !== group.upstream
    ) {
      diverging.add(group.identity);
    }
  }
  return diverging;
}

/**
 * Usage page.
 *
 * @returns Page element.
 */
export function UsagePage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("usage.title")} description={t("usage.description")} />
      <RequirePermission permission="admin:read">
        <UsageContent />
      </RequirePermission>
    </>
  );
}

/**
 * Loaded usage content.
 *
 * @returns Sections.
 */
export function UsageContent() {
  const { t } = useI18n();
  const api = useApi();
  const usage = useCursorList(["usage"], (cursor) =>
    call(
      api.GET("/v1/admin/usage", {
        params: { query: { limit: 100, ...(cursor === undefined ? {} : { cursor }) } },
      }),
    ),
  );
  const refresh = useRefresh();

  if (usage.query.isPending) {
    return <LoadingState />;
  }
  if (usage.query.isError && usage.items.length === 0) {
    return <ErrorState error={usage.query.error} onRetry={() => void usage.query.refetch()} />;
  }
  const overview = usage.query.data?.pages[0]?.overview;
  const diverging = findDivergence(usage.items);

  return (
    <>
      <Section
        title={t("usage.refreshTitle")}
        description={t("usage.refreshDescription")}
        actions={
          <button
            type="button"
            className="button"
            onClick={() => refresh.mutation.mutate({})}
            disabled={!refresh.canRefresh || refresh.mutation.isPending}
          >
            {refresh.mutation.isPending ? t("common.working") : t("usage.refreshAll")}
          </button>
        }
      >
        <PermissionHint permission="admin:usage:refresh" />
        <RefreshStatus mutation={refresh.mutation} />
        <Notice tone="info">{t("usage.unitsNeverCombined")}</Notice>
        {overview === undefined ? null : (
          <p className="muted">
            {t("usage.observedAt")}: <DateTime value={overview.observed_at} />
          </p>
        )}
      </Section>
      {(["exa", "tavily"] as const).map((provider) => (
        <ProviderUsage
          key={provider}
          provider={provider}
          items={usage.items.filter((item) => item.provider === provider)}
          diverging={diverging}
          onRefresh={(accountId) => refresh.mutation.mutate({ account_id: accountId })}
          canRefresh={refresh.canRefresh && !refresh.mutation.isPending}
        />
      ))}
      <LoadMore query={usage.query} />
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
    mutationFn: (body: Schemas["UsageRefresh"]) =>
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
  const { t } = useI18n();
  if (mutation.isError) {
    return <InlineError error={mutation.error} />;
  }
  if (mutation.data === undefined) {
    return null;
  }
  return (
    <p role="status">
      {mutation.data.status === "coalesced" ? t("usage.coalesced") : t("usage.refreshed")}{" "}
      <span className="muted">
        {t("usage.nextRefresh")}: <DateTime value={mutation.data.next_refresh_at} />
      </span>
    </p>
  );
}

/**
 * One provider's usage rows.
 *
 * @param props - Provider, rows, divergence keys and refresh callback.
 * @returns Section element.
 */
function ProviderUsage({
  provider,
  items,
  diverging,
  onRefresh,
  canRefresh,
}: {
  provider: Provider;
  items: AccountUsage[];
  diverging: Set<string>;
  onRefresh: (accountId: string) => void;
  canRefresh: boolean;
}) {
  const { t } = useI18n();
  return (
    <Section title={t(`provider.${provider}`)}>
      {items.length === 0 ? (
        <EmptyState title={t("usage.emptyTitle")} description={t("usage.emptyDescription")} />
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th scope="col">{t("usage.account")}</th>
                <th scope="col">{t("usage.source")}</th>
                <th scope="col">{t("usage.used")}</th>
                <th scope="col">{t("usage.limit")}</th>
                <th scope="col">{t("usage.remaining")}</th>
                <th scope="col">{t("usage.reserved")}</th>
                <th scope="col">{t("usage.period")}</th>
                <th scope="col">{t("usage.freshness")}</th>
                <th scope="col">{t("common.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <UsageRow
                  key={`${item.account_id}-${item.key_id ?? "account"}-${item.source}-${item.unit}`}
                  item={item}
                  isDiverging={diverging.has(
                    `${item.account_id}|${item.key_id ?? ""}|${item.unit}`,
                  )}
                  onRefresh={onRefresh}
                  canRefresh={canRefresh}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  );
}

/**
 * One usage row. Null amounts are unknown; unlimited is explicit and distinct.
 *
 * @param props - Usage row, divergence flag and refresh callback.
 * @returns Table row.
 */
function UsageRow({
  item,
  isDiverging,
  onRefresh,
  canRefresh,
}: {
  item: AccountUsage;
  isDiverging: boolean;
  onRefresh: (accountId: string) => void;
  canRefresh: boolean;
}) {
  const { t } = useI18n();
  const format = useUnitFormatter();
  const amount = (value: number | null) =>
    value === null ? (
      <span className="muted">{t("common.unknown")}</span>
    ) : (
      format(value, item.unit)
    );
  return (
    <tr>
      <td>
        <code>{item.account_id}</code>
        <br />
        <span className="muted">
          {item.key_id === null ? t("usage.accountLevel") : <code>{item.key_id}</code>}
        </span>
        {isDiverging ? (
          <p>
            <Badge tone="warning">{t("usage.divergence")}</Badge>
          </p>
        ) : null}
        {item.unavailable_reason === null ? null : (
          <p className="inline-warning">
            {item.unavailable_reason === "service_permission_missing"
              ? t("usage.servicePermissionMissing")
              : item.unavailable_reason}
          </p>
        )}
      </td>
      <td>
        <Badge tone={SOURCE_TONE[item.source]}>{t(`usageSource.${item.source}`)}</Badge>
      </td>
      <td>{amount(item.used)}</td>
      <td>
        {item.limit_kind === "finite" ? (
          amount(item.limit)
        ) : (
          <Badge tone={item.limit_kind === "unlimited" ? "neutral" : "warning"}>
            {t(`limitKind.${item.limit_kind}`)}
          </Badge>
        )}
      </td>
      <td>
        {item.limit_kind === "finite" ? amount(item.remaining) : <span className="muted">—</span>}
      </td>
      <td>{format(item.reserved, item.unit)}</td>
      <td>
        {item.period_start === null && item.period_end === null ? (
          <span className="muted">{t("usage.notComparable")}</span>
        ) : (
          <>
            <DateTime value={item.period_start} /> – <DateTime value={item.period_end} />
          </>
        )}
      </td>
      <td>
        <Badge tone={FRESHNESS_TONE[item.freshness]}>{t(`freshness.${item.freshness}`)}</Badge>
        <br />
        <span className="muted">
          <DateTime value={item.observed_at} />
        </span>
      </td>
      <td>
        <button
          type="button"
          className="button button-small"
          disabled={!canRefresh}
          onClick={() => onRefresh(item.account_id)}
          aria-label={t("usage.refreshAccount", { account: item.account_id })}
        >
          {t("usage.refresh")}
        </button>
      </td>
    </tr>
  );
}
