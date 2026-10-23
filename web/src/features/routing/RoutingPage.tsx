import { NumericControl } from "../../components/NumericControl";
import { DataTable } from "../../components/DataTable";
import { Link } from "react-router";
import { CheckboxField, Button, Select } from "../../components/Controls";
/** Route configuration with versioned replacement and capability constraints. */
import { useId, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiFailure, call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { Dialog } from "../../components/Dialog";
import {
  ConflictRecovery,
  isConflict,
  Badge,
  EmptyState,
  InlineError,
  Notice,
  QueryView,
} from "../../components/Feedback";
import { useNotify } from "../../components/Notifications";
import { PageHeader, PermissionHint, RequirePermission, Section } from "../../components/Page";
import { DateTime, RouteRevision } from "../../components/Values";

type Operation = Schemas["Operation"];
type RoutePolicy = Schemas["RoutePolicy"];
type RouteTarget = Schemas["RouteTarget"];

/** Static profile/provider pairs per operation from the contract's mapping table. */
const STATIC_TARGETS: Record<Operation, RouteTarget[]> = {
  search: [
    { provider: "exa", profile: "common" },
    { provider: "tavily", profile: "common" },
  ],
  advanced_search: [
    { provider: "exa", profile: "common" },
    { provider: "tavily", profile: "common" },
    { provider: "exa", profile: "exa_deep" },
    { provider: "tavily", profile: "tavily_advanced" },
  ],
  fetch: [
    { provider: "exa", profile: "common" },
    { provider: "tavily", profile: "common" },
  ],
  map: [{ provider: "tavily", profile: "common" }],
  crawl: [{ provider: "tavily", profile: "common" }],
  research_create: [
    { provider: "exa", profile: "exa_auto" },
    { provider: "tavily", profile: "tavily_mini" },
    { provider: "tavily", profile: "tavily_pro" },
  ],
};

/**
 * Compare two route targets.
 *
 * @param left - First target.
 * @param right - Second target.
 * @returns True when provider and profile match.
 */
function sameTarget(left: RouteTarget, right: RouteTarget): boolean {
  return left.provider === right.provider && left.profile === right.profile;
}

/**
 * Load capability profile availability from connection info.
 *
 * @returns Query result.
 */
function useCapabilities() {
  const api = useApi();
  return useQuery({
    queryKey: ["connection-info"],
    queryFn: () => call(api.GET("/v1/admin/connection-info")),
  });
}

/**
 * Routing page.
 *
 * @returns Page element.
 */
export function RoutingPage({ embedded = false }: { embedded?: boolean }) {
  const { t } = useI18n();
  return (
    <>
      {embedded ? null : (
        <PageHeader title={t("routing.title")} description={t("routing.description")} />
      )}
      <RequirePermission permission="admin:read">
        <RoutePolicies />
      </RequirePermission>
    </>
  );
}

/**
 * Policy table and edit dialog.
 *
 * @returns Section element.
 */
function RoutePolicies() {
  const { t } = useI18n();
  const api = useApi();
  const { can } = useSession();
  const capabilities = useCapabilities();
  const [editing, setEditing] = useState<RoutePolicy | null>(null);
  const policies = useQuery({
    queryKey: ["routes"],
    queryFn: () => call(api.GET("/v1/admin/routes", { params: { query: { limit: 100 } } })),
  });
  const profileInfo = (operation: Operation, target: RouteTarget) =>
    capabilities.data?.capabilities.capabilities
      .find((capability) => capability.operation === operation)
      ?.profiles.find(
        (profile) => profile.provider === target.provider && profile.profile === target.profile,
      );

  return (
    <Section title={t("routing.policies")}>
      <PermissionHint permission="admin:routes:write" />
      {capabilities.data?.capabilities.capabilities.some((entry) => !entry.enabled) ? (
        <Notice tone="warning">
          {t(
            capabilities.data.capabilities.capabilities.every((entry) => !entry.enabled)
              ? "dispatch.allRoutesUnavailable"
              : "dispatch.routeNotice",
          )}{" "}
          <Link to="/keys">{t("keys.upstream")}</Link>
        </Notice>
      ) : null}
      <QueryView query={policies}>
        {(data) =>
          data.items.length === 0 ? (
            <EmptyState
              title={t("routing.emptyTitle")}
              description={t("routing.emptyDescription")}
            />
          ) : (
            <div className="table-wrap" role="region" aria-label={t("common.table")} tabIndex={0}>
              <DataTable
                dataSource={data.items}
                rowKey={(policy) => policy.capability}
                columns={[
                  {
                    key: "column-0",
                    title: <>{t("routing.capability")}</>,
                    render: (_value, policy) => {
                      return (
                        <>
                          <span className="operation-name">{policy.capability}</span>
                        </>
                      );
                    },
                  },
                  {
                    key: "column-1",
                    title: <>{t("dispatch.configuration")}</>,
                    render: (_value, policy) => {
                      return (
                        <>
                          <Badge tone={policy.enabled ? "neutral" : "warning"}>
                            {policy.enabled ? t("common.enabled") : t("common.disabled")}
                          </Badge>
                          {policy.enabled && policy.targets.length === 0 ? (
                            <Badge tone="danger">{t("routing.noCompatibleRoute")}</Badge>
                          ) : null}
                        </>
                      );
                    },
                  },
                  {
                    key: "column-2",
                    title: <>{t("routing.targets")}</>,
                    render: (_value, policy) => {
                      return (
                        <>
                          {policy.targets.length === 0 ? (
                            <span className="muted">—</span>
                          ) : (
                            <ol className="target-list">
                              {policy.targets.map((target) => {
                                const info = profileInfo(policy.capability, target);
                                return (
                                  <li key={`${target.provider}-${target.profile}`}>
                                    {t(`provider.${target.provider}`)} ·{" "}
                                    <span>{target.profile}</span>
                                    {info !== undefined && !info.enabled ? (
                                      <>
                                        {" "}
                                        <span
                                          className="availability-dot"
                                          role="img"
                                          title={t("dispatch.blocked")}
                                          aria-label={t("dispatch.blocked")}
                                        />
                                      </>
                                    ) : null}
                                  </li>
                                );
                              })}
                            </ol>
                          )}
                        </>
                      );
                    },
                  },
                  {
                    key: "column-3",
                    title: <>{t("routing.limits")}</>,
                    render: (_value, policy) => {
                      return (
                        <>
                          {t("routing.attemptsDeadline", {
                            attempts: policy.max_attempts,
                            seconds: policy.deadline_seconds,
                          })}
                        </>
                      );
                    },
                  },
                  {
                    key: "column-4",
                    title: <>{t("routing.duplicateCost")}</>,
                    render: (_value, policy) => {
                      return <>{policy.allow_duplicate_cost ? t("common.yes") : t("common.no")}</>;
                    },
                  },
                  {
                    key: "column-5",
                    title: <>{t("routing.revision")}</>,
                    render: (_value, policy) => {
                      return (
                        <>
                          <RouteRevision revision={policy.revision} />
                          <br />
                          <span className="muted">
                            <DateTime value={policy.created_at} />
                          </span>
                        </>
                      );
                    },
                  },
                  {
                    key: "column-6",
                    title: <>{t("common.actions")}</>,
                    render: (_value, policy) => {
                      return (
                        <>
                          <Button
                            type="button"
                            className="button button-small"
                            disabled={!can("admin:routes:write")}
                            onClick={() => setEditing(policy)}
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
      {editing === null ? null : (
        <RouteEditDialog
          policy={editing}
          availableTargets={
            capabilities.data?.capabilities.capabilities
              .find((capability) => capability.operation === editing.capability)
              ?.profiles.map((profile) => ({
                provider: profile.provider,
                profile: profile.profile,
              })) ?? STATIC_TARGETS[editing.capability]
          }
          onClose={() => setEditing(null)}
        />
      )}
    </Section>
  );
}

/**
 * Replace one capability's policy using its current revision.
 *
 * @param props - Policy, selectable targets and close callback.
 * @returns Dialog element.
 */
function RouteEditDialog({
  policy,
  availableTargets,
  onClose,
}: {
  policy: RoutePolicy;
  availableTargets: RouteTarget[];
  onClose: () => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const queryClient = useQueryClient();
  const notify = useNotify();
  const formId = useId();
  const isResearch = policy.capability === "research_create";
  const [isEnabled, setEnabled] = useState(policy.enabled);
  const [targets, setTargets] = useState<RouteTarget[]>(policy.targets);
  const [maxAttempts, setMaxAttempts] = useState(String(policy.max_attempts));
  const [deadline, setDeadline] = useState(String(policy.deadline_seconds));
  const [allowDuplicateCost, setAllowDuplicateCost] = useState(
    policy.allow_duplicate_cost && !isResearch,
  );
  const [candidate, setCandidate] = useState("");

  const maxDeadline =
    policy.capability === "search" || isResearch
      ? 30
      : policy.capability === "fetch" || policy.capability === "advanced_search"
        ? 60
        : 165;
  const attemptsValue = Number(maxAttempts);
  const deadlineValue = Number(deadline);
  const isValid =
    Number.isInteger(attemptsValue) &&
    attemptsValue >= 1 &&
    attemptsValue <= 3 &&
    Number.isInteger(deadlineValue) &&
    deadlineValue >= 1 &&
    deadlineValue <= maxDeadline &&
    targets.length <= 2;
  const remaining = availableTargets.filter(
    (target) => !targets.some((entry) => sameTarget(entry, target)),
  );

  const save = useMutation({
    mutationFn: () =>
      call(
        api.PUT("/v1/admin/routes/{capability}", {
          params: { path: { capability: policy.capability }, header: { "X-Exta-CSRF": csrfToken } },
          body: {
            expected_version: policy.revision,
            enabled: isEnabled,
            targets,
            max_attempts: attemptsValue,
            deadline_seconds: deadlineValue,
            allow_duplicate_cost: allowDuplicateCost,
          },
        }),
      ),
    onSuccess: (updated) => {
      void queryClient.invalidateQueries({ queryKey: ["routes"] });
      notify("info", t("routing.saved", { revision: updated.revision }));
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.status === 409) {
        void queryClient.invalidateQueries({ queryKey: ["routes"] });
      }
    },
  });

  const move = (index: number, offset: -1 | 1) => {
    setTargets((current) => {
      const next = [...current];
      const other = index + offset;
      const moving = next[index];
      const swapped = next[other];
      if (moving === undefined || swapped === undefined) {
        return current;
      }
      next[index] = swapped;
      next[other] = moving;
      return next;
    });
  };

  return (
    <Dialog
      title={t("routing.editTitle", { capability: policy.capability })}
      description={t("routing.editDescription", { revision: policy.revision })}
      onClose={onClose}
      isBusy={save.isPending}
      size="large"
    >
      <form
        className="form"
        onSubmit={(event) => {
          event.preventDefault();
          if (isValid && !save.isPending && !isConflict(save.error)) {
            save.mutate();
          }
        }}
      >
        <div className="field field-checkbox">
          <CheckboxField
            label={<>{t("common.enabled")}</>}
            id={`${formId}-enabled`}
            checked={isEnabled}
            onChange={(event) => setEnabled(event.target.checked)}
            data-autofocus
          />
        </div>
        <fieldset className="fieldset">
          <legend>{t("routing.targetsOrdered")}</legend>
          {targets.length === 0 ? (
            <Notice tone="warning">{t("routing.noTargetsWarning")}</Notice>
          ) : (
            <ol className="target-editor">
              {targets.map((target, index) => (
                <li key={`${target.provider}-${target.profile}`}>
                  <span>
                    {t(`provider.${target.provider}`)} · <span>{target.profile}</span>
                  </span>
                  <span className="button-row tight">
                    <Button
                      type="button"
                      className="button button-small"
                      onClick={() => move(index, -1)}
                      disabled={index === 0}
                      aria-label={t("routing.moveUp", {
                        target: `${target.provider}/${target.profile}`,
                      })}
                    >
                      ↑
                    </Button>
                    <Button
                      type="button"
                      className="button button-small"
                      onClick={() => move(index, 1)}
                      disabled={index === targets.length - 1}
                      aria-label={t("routing.moveDown", {
                        target: `${target.provider}/${target.profile}`,
                      })}
                    >
                      ↓
                    </Button>
                    <Button
                      type="button"
                      className="button button-small"
                      onClick={() =>
                        setTargets((current) => current.filter((_, position) => position !== index))
                      }
                      aria-label={t("routing.removeTarget", {
                        target: `${target.provider}/${target.profile}`,
                      })}
                    >
                      {t("common.remove")}
                    </Button>
                  </span>
                </li>
              ))}
            </ol>
          )}
          {targets.length < 2 && remaining.length > 0 ? (
            <div className="field-row">
              <div className="field">
                <label id={`${formId}-candidate` + "-label"} htmlFor={`${formId}-candidate`}>
                  {t("routing.addTarget")}
                </label>
                <Select
                  id={`${formId}-candidate`}
                  value={candidate}
                  onChange={(event) => setCandidate(event.target.value)}
                >
                  <option value="">{t("common.choose")}</option>
                  {remaining.map((target) => (
                    <option
                      key={`${target.provider}/${target.profile}`}
                      value={`${target.provider}/${target.profile}`}
                    >
                      {t(`provider.${target.provider}`)} · {target.profile}
                    </option>
                  ))}
                </Select>
              </div>
              <Button
                type="button"
                className="button align-end"
                disabled={candidate === ""}
                onClick={() => {
                  const chosen = remaining.find(
                    (target) => `${target.provider}/${target.profile}` === candidate,
                  );
                  if (chosen !== undefined) {
                    setTargets((current) => [...current, chosen]);
                    setCandidate("");
                  }
                }}
              >
                {t("common.add")}
              </Button>
            </div>
          ) : null}
          <p className="field-hint">{t("routing.noDowngrade")}</p>
        </fieldset>
        <div className="field-row">
          <div className="field">
            <label id={`${formId}-attempts` + "-label"} htmlFor={`${formId}-attempts`}>
              {t("routing.maxAttempts")}
            </label>
            <NumericControl
              id={`${formId}-attempts`}
              minimum={1}
              maximum={3}
              presets={[1, 2, 3]}
              value={maxAttempts}
              onChange={setMaxAttempts}
              describedBy={`${formId}-attempts-hint`}
            />
            <p id={`${formId}-attempts-hint`} className="field-hint">
              1–3
            </p>
          </div>
          <div className="field">
            <label id={`${formId}-deadline` + "-label"} htmlFor={`${formId}-deadline`}>
              {t("routing.deadline")}
            </label>
            <NumericControl
              id={`${formId}-deadline`}
              minimum={1}
              maximum={maxDeadline}
              presets={
                maxDeadline <= 30
                  ? [1, 10, 20, maxDeadline]
                  : maxDeadline <= 60
                    ? [1, 15, 30, maxDeadline]
                    : [1, 60, 120, maxDeadline]
              }
              value={deadline}
              onChange={setDeadline}
              describedBy={`${formId}-deadline-hint`}
            />
            <p id={`${formId}-deadline-hint`} className="field-hint">
              1–{maxDeadline} s
            </p>
          </div>
        </div>
        <div className="field field-checkbox">
          <CheckboxField
            label={<>{t("routing.duplicateCost")}</>}
            id={`${formId}-duplicate`}
            checked={allowDuplicateCost}
            onChange={(event) => setAllowDuplicateCost(event.target.checked)}
            disabled={isResearch}
            aria-describedby={`${formId}-duplicate-hint`}
          />
          <p id={`${formId}-duplicate-hint`} className="field-hint">
            {isResearch ? t("routing.duplicateCostResearch") : t("routing.duplicateCostHint")}
          </p>
        </div>
        <InlineError error={save.error} />
        <ConflictRecovery error={save.error} onClose={onClose} />
        <div className="button-row">
          <Button type="button" className="button" onClick={onClose} disabled={save.isPending}>
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            className="button button-primary"
            disabled={!isValid || save.isPending || isConflict(save.error)}
            loading={save.isPending}
          >
            {save.isPending ? t("common.working") : t("common.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
