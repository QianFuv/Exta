/** Route policies per capability, versioned replacement and local no-charge preview. */
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

const OPERATIONS: readonly Operation[] = [
  "search",
  "advanced_search",
  "fetch",
  "map",
  "crawl",
  "research_create",
];

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
export function RoutingPage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("routing.title")} description={t("routing.description")} />
      <RequirePermission permission="admin:read">
        <RoutePolicies />
        <RoutePreviewPanel />
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
      <QueryView query={policies}>
        {(data) =>
          data.items.length === 0 ? (
            <EmptyState
              title={t("routing.emptyTitle")}
              description={t("routing.emptyDescription")}
            />
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th scope="col">{t("routing.capability")}</th>
                    <th scope="col">{t("common.state")}</th>
                    <th scope="col">{t("routing.targets")}</th>
                    <th scope="col">{t("routing.limits")}</th>
                    <th scope="col">{t("routing.duplicateCost")}</th>
                    <th scope="col">{t("routing.revision")}</th>
                    <th scope="col">{t("common.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((policy) => (
                    <tr key={policy.capability}>
                      <th scope="row">
                        <code>{policy.capability}</code>
                      </th>
                      <td>
                        <Badge tone={policy.enabled ? "neutral" : "warning"}>
                          {policy.enabled ? t("common.enabled") : t("common.disabled")}
                        </Badge>
                        {policy.enabled && policy.targets.length === 0 ? (
                          <Badge tone="danger">{t("routing.noCompatibleRoute")}</Badge>
                        ) : null}
                      </td>
                      <td>
                        {policy.targets.length === 0 ? (
                          <span className="muted">—</span>
                        ) : (
                          <ol className="target-list">
                            {policy.targets.map((target) => {
                              const info = profileInfo(policy.capability, target);
                              return (
                                <li key={`${target.provider}-${target.profile}`}>
                                  {t(`provider.${target.provider}`)} · <code>{target.profile}</code>
                                  {info !== undefined && !info.enabled ? (
                                    <>
                                      {" "}
                                      <Badge tone="warning">{t("routing.providerDisabled")}</Badge>
                                    </>
                                  ) : null}
                                </li>
                              );
                            })}
                          </ol>
                        )}
                      </td>
                      <td>
                        {t("routing.attemptsDeadline", {
                          attempts: policy.max_attempts,
                          seconds: policy.deadline_seconds,
                        })}
                      </td>
                      <td>{policy.allow_duplicate_cost ? t("common.yes") : t("common.no")}</td>
                      <td>
                        <RouteRevision revision={policy.revision} />
                        <br />
                        <span className="muted">
                          <DateTime value={policy.created_at} />
                        </span>
                      </td>
                      <td>
                        <button
                          type="button"
                          className="button button-small"
                          disabled={!can("admin:routes:write")}
                          onClick={() => setEditing(policy)}
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
          <input
            id={`${formId}-enabled`}
            type="checkbox"
            checked={isEnabled}
            onChange={(event) => setEnabled(event.target.checked)}
            data-autofocus
          />
          <label htmlFor={`${formId}-enabled`}>{t("common.enabled")}</label>
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
                    {t(`provider.${target.provider}`)} · <code>{target.profile}</code>
                  </span>
                  <span className="button-row tight">
                    <button
                      type="button"
                      className="button button-small"
                      onClick={() => move(index, -1)}
                      disabled={index === 0}
                      aria-label={t("routing.moveUp", {
                        target: `${target.provider}/${target.profile}`,
                      })}
                    >
                      ↑
                    </button>
                    <button
                      type="button"
                      className="button button-small"
                      onClick={() => move(index, 1)}
                      disabled={index === targets.length - 1}
                      aria-label={t("routing.moveDown", {
                        target: `${target.provider}/${target.profile}`,
                      })}
                    >
                      ↓
                    </button>
                    <button
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
                    </button>
                  </span>
                </li>
              ))}
            </ol>
          )}
          {targets.length < 2 && remaining.length > 0 ? (
            <div className="field-row">
              <div className="field">
                <label htmlFor={`${formId}-candidate`}>{t("routing.addTarget")}</label>
                <select
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
                </select>
              </div>
              <button
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
              </button>
            </div>
          ) : null}
          <p className="field-hint">{t("routing.noDowngrade")}</p>
        </fieldset>
        <div className="field-row">
          <div className="field">
            <label htmlFor={`${formId}-attempts`}>{t("routing.maxAttempts")}</label>
            <input
              id={`${formId}-attempts`}
              inputMode="numeric"
              value={maxAttempts}
              onChange={(event) => setMaxAttempts(event.target.value)}
              aria-describedby={`${formId}-attempts-hint`}
            />
            <p id={`${formId}-attempts-hint`} className="field-hint">
              1–3
            </p>
          </div>
          <div className="field">
            <label htmlFor={`${formId}-deadline`}>{t("routing.deadline")}</label>
            <input
              id={`${formId}-deadline`}
              inputMode="numeric"
              value={deadline}
              onChange={(event) => setDeadline(event.target.value)}
              aria-describedby={`${formId}-deadline-hint`}
            />
            <p id={`${formId}-deadline-hint`} className="field-hint">
              1–{maxDeadline} s
            </p>
          </div>
        </div>
        <div className="field field-checkbox">
          <input
            id={`${formId}-duplicate`}
            type="checkbox"
            checked={allowDuplicateCost}
            onChange={(event) => setAllowDuplicateCost(event.target.checked)}
            disabled={isResearch}
            aria-describedby={`${formId}-duplicate-hint`}
          />
          <label htmlFor={`${formId}-duplicate`}>{t("routing.duplicateCost")}</label>
          <p id={`${formId}-duplicate-hint`} className="field-hint">
            {isResearch ? t("routing.duplicateCostResearch") : t("routing.duplicateCostHint")}
          </p>
        </div>
        <InlineError error={save.error} />
        <ConflictRecovery error={save.error} onClose={onClose} />
        <div className="button-row">
          <button type="button" className="button" onClick={onClose} disabled={save.isPending}>
            {t("common.cancel")}
          </button>
          <button
            type="submit"
            className="button button-primary"
            disabled={!isValid || save.isPending || isConflict(save.error)}
          >
            {save.isPending ? t("common.working") : t("common.save")}
          </button>
        </div>
      </form>
    </Dialog>
  );
}

type PreviewProfile = Schemas["Profile"];

/**
 * Build a preview request for a sample input.
 *
 * @param operation - Operation.
 * @param sample - Query, URL or research input.
 * @param profile - Requested profile.
 * @returns Preview request body.
 */
export function buildPreviewRequest(
  operation: Operation,
  sample: string,
  profile: PreviewProfile,
): Schemas["RoutePreviewRequest"] {
  switch (operation) {
    case "search":
      return { operation, request: { query: sample } };
    case "advanced_search":
      if (profile === "exa_deep" || profile === "tavily_advanced") {
        return {
          operation,
          request: { query: sample, profile, provider_options: { kind: profile } },
        };
      }
      return { operation, request: { query: sample, profile: "common" } };
    case "fetch":
      return { operation, request: { urls: [sample] } };
    case "map":
      return { operation, request: { url: sample } };
    case "crawl":
      return { operation, request: { url: sample } };
    case "research_create":
      if (profile === "tavily_mini" || profile === "tavily_pro") {
        return {
          operation,
          request: {
            input: sample,
            profile,
            budget: {
              mode: "estimated",
              unit: "tavily_credit",
              amount: profile === "tavily_pro" ? 250 : 110,
              acknowledge_estimated_budget: true,
            },
          },
        };
      }
      return {
        operation,
        request: {
          input: sample,
          profile: "exa_auto",
          budget: { mode: "hard", unit: "micro_usd", amount: 1_000_000 },
        },
      };
  }
}

const PREVIEW_PROFILES: Record<Operation, PreviewProfile[]> = {
  search: ["common"],
  advanced_search: ["common", "exa_deep", "tavily_advanced"],
  fetch: ["common"],
  map: ["common"],
  crawl: ["common"],
  research_create: ["exa_auto", "tavily_mini", "tavily_pro"],
};

/**
 * Local, no-charge semantic routing preview.
 *
 * @returns Section element.
 */
function RoutePreviewPanel() {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const formId = useId();
  const [operation, setOperation] = useState<Operation>("search");
  const [profile, setProfile] = useState<PreviewProfile>("common");
  const [sample, setSample] = useState("");
  const preview = useMutation({
    mutationFn: (body: Schemas["RoutePreviewRequest"]) =>
      call(
        api.POST("/v1/admin/routes/preview", {
          params: { header: { "X-Exta-CSRF": csrfToken } },
          body,
        }),
      ),
  });
  const isUrl = operation === "fetch" || operation === "map" || operation === "crawl";
  const result = preview.data;

  return (
    <Section title={t("routing.previewTitle")} description={t("routing.previewDescription")}>
      <form
        className="form"
        onSubmit={(event) => {
          event.preventDefault();
          if (sample.trim() !== "" && !preview.isPending) {
            preview.mutate(buildPreviewRequest(operation, sample.trim(), profile));
          }
        }}
      >
        <div className="field-row">
          <div className="field">
            <label htmlFor={`${formId}-operation`}>{t("routing.capability")}</label>
            <select
              id={`${formId}-operation`}
              value={operation}
              onChange={(event) => {
                const next = event.target.value as Operation;
                setOperation(next);
                setProfile(PREVIEW_PROFILES[next][0] ?? "common");
                preview.reset();
              }}
            >
              {OPERATIONS.map((entry) => (
                <option key={entry} value={entry}>
                  {entry}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor={`${formId}-profile`}>{t("routing.profile")}</label>
            <select
              id={`${formId}-profile`}
              value={profile}
              onChange={(event) => setProfile(event.target.value as PreviewProfile)}
            >
              {PREVIEW_PROFILES[operation].map((entry) => (
                <option key={entry} value={entry}>
                  {entry}
                </option>
              ))}
            </select>
          </div>
          <div className="field grow">
            <label htmlFor={`${formId}-sample`}>
              {isUrl ? t("routing.sampleUrl") : t("routing.sampleQuery")}
            </label>
            <input
              id={`${formId}-sample`}
              value={sample}
              onChange={(event) => setSample(event.target.value)}
              placeholder={isUrl ? "https://example.com/" : ""}
            />
          </div>
        </div>
        <div className="button-row">
          <button
            type="submit"
            className="button button-primary"
            disabled={sample.trim() === "" || preview.isPending}
          >
            {preview.isPending ? t("common.working") : t("routing.preview")}
          </button>
        </div>
      </form>
      <InlineError error={preview.error} />
      {result === undefined ? null : (
        <div className="preview-result" role="status" aria-live="polite">
          <p>
            {t("routing.revision")}: <RouteRevision revision={result.route_revision} />
          </p>
          {result.eligible.length === 0 ? (
            <Notice tone="danger" title={t("routing.noCompatibleRoute")}>
              {t("routing.noCompatibleRouteDetail")}
            </Notice>
          ) : (
            <>
              <h3>{t("routing.eligible")}</h3>
              <ol className="target-list">
                {result.eligible.map((target) => (
                  <li key={`${target.provider}-${target.profile}`}>
                    {t(`provider.${target.provider}`)} · <code>{target.profile}</code>
                  </li>
                ))}
              </ol>
            </>
          )}
          {result.exclusions.length === 0 ? null : (
            <>
              <h3>{t("routing.exclusions")}</h3>
              <ul className="plain-list">
                {result.exclusions.map((exclusion, index) => (
                  <li key={`${exclusion.provider}-${exclusion.field ?? ""}-${index}`}>
                    <strong>{t(`provider.${exclusion.provider}`)}</strong>
                    {exclusion.field === null ? null : (
                      <>
                        {" "}
                        <code>{exclusion.field}</code>
                      </>
                    )}
                    : {exclusion.reason}
                  </li>
                ))}
              </ul>
            </>
          )}
          {result.warnings.length === 0 ? null : (
            <Notice tone="warning" title={t("routing.warnings")}>
              <ul className="plain-list">
                {result.warnings.map((warning) => (
                  <li key={warning}>{warning}</li>
                ))}
              </ul>
            </Notice>
          )}
          <p className="muted">{t("routing.previewNoCharge")}</p>
        </div>
      )}
    </Section>
  );
}
