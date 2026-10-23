import { DiagnosticReason } from "../../components/DiagnosticReason";
import { DataTable } from "../../components/DataTable";
import type { ColumnProps } from "@douyinfe/semi-ui/lib/es/table";
import { Button } from "../../components/Controls";
import { useState } from "react";
import { Dialog } from "../../components/Dialog";
import { useKeyNames } from "../../lib/useKeyNames";
/** Usage by provider/key with separated units, sources, freshness and limit kinds. */
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
} from "../../components/Feedback";
import type { Tone } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { useNotify } from "../../components/Notifications";
import { PageHeader, PermissionHint, RequirePermission, Section } from "../../components/Page";
import { DateTime, useUnitFormatter } from "../../components/Values";

type KeyUsage = Schemas["KeyUsage"];
type Provider = Schemas["Provider"];

const FRESHNESS_TONE: Record<KeyUsage["freshness"], Tone> = {
  fresh: "info",
  stale: "warning",
  unknown: "warning",
};
const SOURCE_TONE: Record<KeyUsage["source"], Tone> = {
  upstream: "info",
  local: "neutral",
  estimate: "warning",
};

/**
 * Find key observations whose local ledger and upstream snapshot disagree in the same unit.
 *
 * @param items - Usage rows.
 * @returns Keys `association|key|unit` that diverge.
 */
export function findDivergence(items: KeyUsage[]): Set<string> {
  const groups = new Map<
    string,
    { identity: string; local?: number | null; upstream?: number | null }
  >();
  for (const item of items) {
    if (item.source === "estimate" || item.period_start === null || item.period_end === null) {
      continue;
    }
    const identity = `${item.association_ref}|${item.key_id ?? ""}|${item.unit}`;
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
  const refreshedAt = lastSuccessfulObservation(usage.items);
  const diverging = findDivergence(usage.items);

  return (
    <>
      <div className="usage-refresh-toolbar">
        <Button
          type="button"
          onClick={() => refresh.mutation.mutate({})}
          disabled={!refresh.canRefresh || refresh.mutation.isPending}
          loading={refresh.mutation.isPending}
        >
          {t("usage.refreshAll")}
        </Button>
        <span className="muted">
          {t("usage.lastRefresh")}:{" "}
          {refreshedAt ? <DateTime value={refreshedAt} /> : t("usage.notRefreshed")}
        </span>
        <PermissionHint permission="admin:usage:refresh" />
        <RefreshStatus mutation={refresh.mutation} />
      </div>
      {(["exa", "tavily"] as const).map((provider) => (
        <ProviderUsage
          key={provider}
          provider={provider}
          items={usage.items.filter((item) => item.provider === provider)}
          diverging={diverging}
          onRefresh={(keyId) => refresh.mutation.mutate({ key_id: keyId })}
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
  items: KeyUsage[];
  diverging: Set<string>;
  onRefresh: (keyId: string) => void;
  canRefresh: boolean;
}) {
  const columns = useUsageColumns(diverging, onRefresh, canRefresh);
  const { t } = useI18n();
  const names = useKeyNames();
  const format = useUnitFormatter();
  const [selected, setSelected] = useState<string | null>(null);
  const groups = new Map<string, KeyUsage[]>();
  for (const item of items) {
    const identity = `${item.key_id ?? item.association_ref}|${item.unit}`;
    groups.set(identity, [...(groups.get(identity) ?? []), item]);
  }
  const rows = [...groups].map(([identity, observations]) => ({ identity, observations }));
  const evidence = selected ? groups.get(selected) : undefined;
  return (
    <Section title={t(`provider.${provider}`)}>
      <p className="field-hint usage-period">{t("usage.notComparable")}</p>
      {items.length === 0 ? (
        <EmptyState title={t("usage.emptyTitle")} description={t("usage.emptyDescription")} />
      ) : (
        <div
          className="table-wrap"
          role="region"
          aria-label={`${t(`provider.${provider}`)} · ${t("usage.title")}`}
          tabIndex={0}
        >
          <DataTable
            dataSource={rows}
            rowKey={(row) => row.identity}
            columns={[
              {
                key: "key",
                width: 240,
                title: t("keys.key"),
                render: (_value, row) => {
                  const item = row.observations[0]!;
                  return (
                    <>
                      <span title={item.key_id ?? item.association_ref}>
                        {item.key_id
                          ? (names.get(item.key_id) ?? item.key_id)
                          : t("keys.sharedAllowance")}
                      </span>
                      <small className="cell-secondary">
                        {item.scope === "shared" ? t("keys.sharedAllowance") : t("keys.localLimit")}
                      </small>
                      {diverging.has(
                        `${item.association_ref}|${item.key_id ?? ""}|${item.unit}`,
                      ) ? (
                        <Badge tone="warning">{t("usage.divergence")}</Badge>
                      ) : null}
                    </>
                  );
                },
              },
              {
                key: "local",
                width: 180,
                title: t("usageSource.local"),
                render: (_value, row) => {
                  const local = row.observations.find((item) => item.source === "local");
                  return local ? (
                    <>
                      <span className="usage-amount">
                        {t("usage.used")}:{" "}
                        {local.used === null ? t("common.unknown") : format(local.used, local.unit)}
                      </span>
                      <small className="cell-secondary usage-amount">
                        {t("usage.reserved")}: {format(local.reserved, local.unit)}
                      </small>
                    </>
                  ) : (
                    t("common.unknown")
                  );
                },
              },
              {
                key: "upstream",
                width: 220,
                title: t("usageSource.upstream"),
                render: (_value, row) => {
                  const item = row.observations.find((entry) => entry.source === "upstream");
                  if (!item) return t("common.unknown");
                  return (
                    <>
                      <span className="usage-amount">
                        {t("usage.used")}:{" "}
                        {item.used === null ? t("common.unknown") : format(item.used, item.unit)}
                      </span>
                      <small className="cell-secondary usage-amount">
                        {item.limit_kind === "finite" && item.limit !== null
                          ? `${t("usage.limit")}: ${format(item.limit, item.unit)}`
                          : t(`limitKind.${item.limit_kind}`)}
                      </small>
                      {item.unavailable_reason ? (
                        <DiagnosticReason reason={item.unavailable_reason} />
                      ) : null}
                    </>
                  );
                },
              },
              {
                key: "freshness",
                width: 180,
                title: t("usage.freshness"),
                render: (_value, row) => {
                  const item =
                    row.observations.find((entry) => entry.source === "upstream") ??
                    row.observations[0]!;
                  return (
                    <>
                      <Badge tone={FRESHNESS_TONE[item.freshness]}>
                        {t(`freshness.${item.freshness}`)}
                      </Badge>
                      {item.observed_at ? (
                        <small className="cell-secondary">
                          <DateTime value={item.observed_at} />
                        </small>
                      ) : null}
                    </>
                  );
                },
              },
              {
                key: "actions",
                width: 120,
                title: t("common.actions"),
                render: (_value, row) => (
                  <Button onClick={() => setSelected(row.identity)}>{t("usage.evidence")}</Button>
                ),
              },
            ]}
          />
        </div>
      )}
      {evidence ? (
        <Dialog title={t("usage.evidence")} size="large" onClose={() => setSelected(null)}>
          <p className="field-hint">{t("usage.notComparable")}</p>
          <div className="table-wrap" role="region" aria-label={t("usage.evidence")} tabIndex={0}>
            <DataTable
              dataSource={evidence}
              rowKey={(item) =>
                `${item.source}-${item.scope}-${item.period_start}-${item.period_end}`
              }
              columns={columns}
            />
          </div>
        </Dialog>
      ) : null}
    </Section>
  );
}

/**
 * One usage row. Null amounts are unknown; unlimited is explicit and distinct.
 *
 * @param props - Usage row, divergence flag and refresh callback.
 * @returns Table row.
 */
/** Build typed Semi columns with the existing permissions and evidence semantics. */
function useUsageColumns(
  diverging: Set<string>,
  onRefresh: (keyId: string) => void,
  canRefresh: boolean,
): ColumnProps<KeyUsage>[] {
  const { t } = useI18n();
  const format = useUnitFormatter();
  const names = useKeyNames();
  return [
    {
      key: "column-0",
      title: <>{t("keys.key")}</>,
      render: (_value, item) => {
        const isDiverging = diverging.has(
          `${item.association_ref}|${item.key_id ?? ""}|${item.unit}`,
        );
        return (
          <>
            <span title={item.key_id ?? item.association_ref}>
              {item.key_id ? (names.get(item.key_id) ?? item.key_id) : t("keys.sharedAllowance")}
            </span>
            <br />
            <span className="muted">
              {item.scope === "shared" ? t("keys.sharedAllowance") : t("keys.localLimit")}
            </span>
            {isDiverging ? (
              <p>
                <Badge tone="warning">{t("usage.divergence")}</Badge>
              </p>
            ) : null}
            {item.unavailable_reason === null ? null : (
              <p className="inline-warning">
                {item.unavailable_reason === "service_permission_missing" ? (
                  t("usage.servicePermissionMissing")
                ) : (
                  <DiagnosticReason reason={item.unavailable_reason} />
                )}
              </p>
            )}
          </>
        );
      },
    },
    {
      key: "column-1",
      title: <>{t("usage.source")}</>,
      render: (_value, item) => {
        return (
          <>
            <Badge tone={SOURCE_TONE[item.source]}>{t(`usageSource.${item.source}`)}</Badge>
          </>
        );
      },
    },
    {
      key: "column-2",
      title: <>{t("usage.used")}</>,
      render: (_value, item) => {
        const amount = (value: number | null) =>
          value === null ? (
            <span className="muted">{t("common.unknown")}</span>
          ) : (
            format(value, item.unit)
          );
        return <span className="usage-amount">{amount(item.used)}</span>;
      },
    },
    {
      key: "column-3",
      title: <>{t("usage.limit")}</>,
      render: (_value, item) => {
        const amount = (value: number | null) =>
          value === null ? (
            <span className="muted">{t("common.unknown")}</span>
          ) : (
            format(value, item.unit)
          );
        return (
          <>
            {item.limit_kind === "finite" ? (
              amount(item.limit)
            ) : (
              <Badge tone={item.limit_kind === "unlimited" ? "neutral" : "warning"}>
                {t(`limitKind.${item.limit_kind}`)}
              </Badge>
            )}
          </>
        );
      },
    },
    {
      key: "column-4",
      title: <>{t("usage.remaining")}</>,
      render: (_value, item) => {
        const amount = (value: number | null) =>
          value === null ? (
            <span className="muted">{t("common.unknown")}</span>
          ) : (
            format(value, item.unit)
          );
        return (
          <>
            {item.limit_kind === "finite" ? (
              amount(item.remaining)
            ) : (
              <span className="muted">—</span>
            )}
          </>
        );
      },
    },
    {
      key: "column-5",
      title: <>{t("usage.reserved")}</>,
      render: (_value, item) => {
        return <>{format(item.reserved, item.unit)}</>;
      },
    },
    {
      key: "column-6",
      title: <>{t("usage.period")}</>,
      render: (_value, item) => {
        return (
          <>
            {item.period_start === null && item.period_end === null ? (
              <span className="muted">{t("common.unknown")}</span>
            ) : (
              <>
                <DateTime value={item.period_start} /> – <DateTime value={item.period_end} />
              </>
            )}
          </>
        );
      },
    },
    {
      key: "column-7",
      title: <>{t("usage.freshness")}</>,
      render: (_value, item) => {
        return (
          <>
            <Badge tone={FRESHNESS_TONE[item.freshness]}>{t(`freshness.${item.freshness}`)}</Badge>
            <br />
            <span className="muted">
              <DateTime value={item.observed_at} />
            </span>
          </>
        );
      },
    },
    {
      key: "column-8",
      title: <>{t("common.actions")}</>,
      render: (_value, item) => {
        return (
          <>
            <Button
              type="button"
              className="button button-small"
              disabled={!canRefresh || item.key_id === null}
              onClick={() => {
                if (item.key_id) onRefresh(item.key_id);
              }}
              aria-label={t("usage.refreshKey", { key: item.key_id ?? item.association_ref })}
            >
              {t("usage.refresh")}
            </Button>
          </>
        );
      },
    },
  ];
}
