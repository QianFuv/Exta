import { NumericControl } from "../../components/NumericControl";
import { Button } from "../../components/Controls";
/** Non-secret limits/retention editing plus build, schema and readiness diagnostics. */
import { useId, useState } from "react";
import { useSearchParams } from "react-router";
import { SectionNav } from "../../components/SectionNav";
import { RoutingPage } from "../routing/RoutingPage";
import { ConnectionDetails } from "../connections/ConnectionsPage";
import { BudgetSettings } from "./BudgetSettings";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiFailure, call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { CONTRACT_SHA256 } from "../../generated/client.gen";
import { useI18n } from "../../lib/i18n";
import type { MessageKey } from "../../lib/messages";
import { useSession } from "../../lib/session";
import { Badge, InlineError, Notice, QueryView } from "../../components/Feedback";
import { useNotify } from "../../components/Notifications";
import { PageHeader, PermissionHint, RequirePermission, Section } from "../../components/Page";
import { ReadinessList } from "../overview/ReadinessList";

type Settings = Schemas["Settings"];
type EditableLimit = Exclude<keyof Schemas["Limits"], "principal_research_active">;
type EditableRetention = Exclude<keyof Schemas["Retention"], "idempotency_days" | "audit_days">;

const LIMIT_FIELDS: ReadonlyArray<{ name: EditableLimit; minimum: number; maximum: number }> = [
  { name: "synchronous_concurrency", minimum: 1, maximum: 16 },
  { name: "synchronous_waiting", minimum: 0, maximum: 64 },
  { name: "principal_requests_per_minute", minimum: 1, maximum: 60 },
  { name: "research_queued", minimum: 1, maximum: 100 },
  { name: "research_active", minimum: 1, maximum: 2 },
  { name: "request_bytes", minimum: 1024, maximum: 1048576 },
  { name: "response_bytes", minimum: 1024, maximum: 8388608 },
  { name: "observation_seconds", minimum: 60, maximum: 3600 },
  { name: "max_polls", minimum: 1, maximum: 120 },
];

const LIMIT_GROUPS: ReadonlyArray<{ title: MessageKey; fields: EditableLimit[] }> = [
  {
    title: "settings.traffic",
    fields: ["synchronous_concurrency", "synchronous_waiting", "principal_requests_per_minute"],
  },
  {
    title: "settings.research",
    fields: ["research_queued", "research_active", "observation_seconds", "max_polls"],
  },
  { title: "settings.payload", fields: ["request_bytes", "response_bytes"] },
];

/** Keep displayed units explicit without converting the API's integer values. */
function fieldUnit(name: string): MessageKey {
  if (name.endsWith("_bytes")) return "settings.unit.bytes";
  if (name.endsWith("_days")) return "settings.unit.days";
  if (name.endsWith("_seconds")) return "settings.unit.seconds";
  if (name === "principal_requests_per_minute") return "settings.unit.perMinute";
  if (name === "max_polls") return "settings.unit.polls";
  return name.includes("research") ? "settings.unit.jobs" : "settings.unit.requests";
}

const RETENTION_FIELDS: ReadonlyArray<{
  name: EditableRetention;
  minimum: number;
  maximum: number;
}> = [
  { name: "request_days", minimum: 1, maximum: 30 },
  { name: "research_content_days", minimum: 1, maximum: 30 },
];

/**
 * Settings page.
 *
 * @returns Page element.
 */
export function SettingsPage() {
  const { t } = useI18n();
  const [params] = useSearchParams();
  const view = params.get("view") ?? "general";
  return (
    <>
      <PageHeader title={t("settings.title")} />
      <SectionNav
        label={t("settings.title")}
        items={[
          { value: "general", label: t("settings.general") },
          { value: "routing", label: t("nav.routing") },
          { value: "budgets", label: t("keys.budgetSettings") },
          { value: "health", label: t("settings.health") },
          { value: "connection", label: t("settings.access") },
        ]}
      />
      <RequirePermission permission="admin:read">
        {view === "budgets" ? (
          <BudgetSettings />
        ) : view === "routing" ? (
          <RoutingPage embedded />
        ) : view === "connection" ? (
          <ConnectionDetails />
        ) : (
          <SettingsContent health={view === "health"} />
        )}
      </RequirePermission>
    </>
  );
}

/**
 * Loaded settings.
 *
 * @returns Sections.
 */
function SettingsContent({ health }: { health: boolean }) {
  const { t } = useI18n();
  const api = useApi();
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => call(api.GET("/v1/admin/settings")),
  });
  return (
    <QueryView query={settings}>
      {(data) => (
        <>
          {health ? (
            <div className="settings-health">
              <Section title={t("settings.diagnostics")}>
                <BuildDiagnostics settings={data} />
                <ReadinessList readiness={data.readiness} />
              </Section>
              <Section title={t("settings.protected")} help={t("settings.protectedDescription")}>
                {data.protected_fields.length === 0 ? (
                  <p className="muted">—</p>
                ) : (
                  <ul className="plain-list">
                    {data.protected_fields.map((field) => (
                      <li key={field}>
                        <span>
                          {t(
                            (
                              {
                                bind: "protected.bind",
                                data_dir: "protected.data_dir",
                                master_key: "protected.master_key",
                                upstream_origins: "protected.upstream_origins",
                              } as Record<string, MessageKey>
                            )[field] ?? "settings.protected",
                          )}{" "}
                          <code className="cell-secondary">{field}</code>
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </Section>
            </div>
          ) : (
            <LimitsForm key={data.version} settings={data} />
          )}
        </>
      )}
    </QueryView>
  );
}

/**
 * Build/version/contract diagnostics, flagging a UI/server contract mismatch.
 *
 * @param props - Settings.
 * @returns Diagnostics list.
 */
function BuildDiagnostics({ settings }: { settings: Settings }) {
  const { t } = useI18n();
  const isMismatch = settings.build.contract_hash !== CONTRACT_SHA256;
  return (
    <>
      {isMismatch ? (
        <Notice tone="danger" role="alert" title={t("settings.contractMismatch")}>
          {t("settings.contractMismatchDetail")}
        </Notice>
      ) : null}
      <dl className="details-grid">
        <dt>{t("settings.version")}</dt>
        <dd>
          <code>{settings.build.version}</code>
        </dd>
        <dt>{t("settings.target")}</dt>
        <dd>
          <code>{settings.build.target}</code>
        </dd>
        <dt>{t("settings.schemaVersion")}</dt>
        <dd>{settings.build.schema_version}</dd>
        <dt>{t("settings.serverContract")}</dt>
        <dd>
          <code className="hash">{settings.build.contract_hash}</code>{" "}
          <Badge tone={isMismatch ? "danger" : "neutral"}>
            {isMismatch ? t("settings.mismatch") : t("settings.matchesUi")}
          </Badge>
        </dd>
        <dt>{t("settings.uiContract")}</dt>
        <dd>
          <code className="hash">{CONTRACT_SHA256}</code>
        </dd>
      </dl>
    </>
  );
}

/**
 * Versioned limits and retention form.
 *
 * @param props - Current settings.
 * @returns Section element.
 */
function LimitsForm({ settings }: { settings: Settings }) {
  const { t, formatNumber } = useI18n();
  const api = useApi();
  const { can, csrfToken } = useSession();
  const queryClient = useQueryClient();
  const notify = useNotify();
  const formId = useId();
  const canWrite = can("admin:settings:write");
  const [limits, setLimits] = useState<Record<EditableLimit, string>>(
    () =>
      Object.fromEntries(
        LIMIT_FIELDS.map((field) => [field.name, String(settings.limits[field.name])]),
      ) as Record<EditableLimit, string>,
  );
  const [retention, setRetention] = useState<Record<EditableRetention, string>>(
    () =>
      Object.fromEntries(
        RETENTION_FIELDS.map((field) => [field.name, String(settings.retention[field.name])]),
      ) as Record<EditableRetention, string>,
  );

  const isInRange = (text: string, minimum: number, maximum: number) => {
    const value = Number(text);
    return /^\d+$/.test(text) && value >= minimum && value <= maximum;
  };
  const isValid =
    LIMIT_FIELDS.every((field) => isInRange(limits[field.name], field.minimum, field.maximum)) &&
    RETENTION_FIELDS.every((field) =>
      isInRange(retention[field.name], field.minimum, field.maximum),
    );

  const save = useMutation({
    mutationFn: () =>
      call(
        api.PATCH("/v1/admin/settings", {
          params: { header: { "X-Exta-CSRF": csrfToken } },
          body: {
            expected_version: settings.version,
            limits: {
              synchronous_concurrency: Number(limits.synchronous_concurrency),
              synchronous_waiting: Number(limits.synchronous_waiting),
              principal_requests_per_minute: Number(limits.principal_requests_per_minute),
              research_queued: Number(limits.research_queued),
              research_active: Number(limits.research_active),
              principal_research_active: 1,
              request_bytes: Number(limits.request_bytes),
              response_bytes: Number(limits.response_bytes),
              observation_seconds: Number(limits.observation_seconds),
              max_polls: Number(limits.max_polls),
            },
            retention: {
              request_days: Number(retention.request_days),
              research_content_days: Number(retention.research_content_days),
              audit_days: settings.retention.audit_days,
              idempotency_days: 90,
            },
          },
        }),
      ),
    onSuccess: (updated) => {
      queryClient.setQueryData(["settings"], updated);
      notify("info", t("settings.saved"));
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.status === 409) {
        void queryClient.invalidateQueries({ queryKey: ["settings"] });
      }
    },
  });

  const renderField = (
    name: string,
    value: string,
    minimum: number,
    maximum: number,
    onChange: (value: string) => void,
  ) => {
    const isFieldValid = isInRange(value, minimum, maximum);
    return (
      <div className="field" key={name}>
        <label id={`${formId}-${name}` + "-label"} htmlFor={`${formId}-${name}`}>
          {t(`settings.field.${name}` as MessageKey)}
        </label>
        <div className="input-with-unit">
          <NumericControl
            id={`${formId}-${name}`}
            minimum={minimum}
            maximum={maximum}
            presets={numericPresets(name, maximum)}
            value={value}
            onChange={onChange}
            disabled={!canWrite || save.isPending}
            invalid={!isFieldValid}
            describedBy={`${formId}-${name}-range`}
          />
        </div>
        <p id={`${formId}-${name}-range`} className={isFieldValid ? "field-hint" : "field-error"}>
          {isFieldValid
            ? `${formatNumber(minimum)}–${formatNumber(maximum)} ${t(fieldUnit(name))}`
            : t("settings.rangeError", { minimum, maximum })}
        </p>
        {name.endsWith("_bytes") && isFieldValid ? (
          <p className="field-hint">≈ {formatNumber(Number(value) / 1048576)} MiB</p>
        ) : null}
        {name === "research_active" ? (
          <p className="field-hint">
            {t("settings.field.principal_research_active")}: 1 {t("settings.unit.job")}
          </p>
        ) : null}
      </div>
    );
  };

  return (
    <Section title={t("settings.limitsTitle")}>
      <PermissionHint permission="admin:settings:write" />
      <form
        className="form settings-form"
        onSubmit={(event) => {
          event.preventDefault();
          if (canWrite && isValid && !save.isPending) {
            save.mutate();
          }
        }}
      >
        {LIMIT_GROUPS.map((group) => (
          <fieldset className="fieldset settings-group" key={group.title}>
            <legend>{t(group.title)}</legend>
            <div className="field-grid">
              {group.fields.map((name) => {
                const field = LIMIT_FIELDS.find((entry) => entry.name === name)!;
                return renderField(name, limits[name], field.minimum, field.maximum, (value) =>
                  setLimits((current) => ({ ...current, [name]: value })),
                );
              })}
            </div>
          </fieldset>
        ))}
        <fieldset className="fieldset settings-group">
          <legend>{t("settings.retention")}</legend>
          <div className="field-grid">
            {RETENTION_FIELDS.map((field) =>
              renderField(
                field.name,
                retention[field.name],
                field.minimum,
                field.maximum,
                (value) => setRetention((current) => ({ ...current, [field.name]: value })),
              ),
            )}
            <div className="field">
              <span className="field-label">{t("settings.field.idempotency_days")}</span>
              <p className="readonly-value">
                90 {t("settings.unit.days")} · {t("settings.fixed")}
              </p>
            </div>
          </div>
          <p className="field-hint">{t("settings.retentionNote")}</p>
        </fieldset>
        <InlineError error={save.error} />
        <div className="settings-save-bar">
          <span className="muted">{t("settings.saveAll")}</span>
          <Button
            type="submit"
            className="button button-primary"
            disabled={!canWrite || !isValid || save.isPending}
            loading={save.isPending}
          >
            {save.isPending ? t("common.working") : t("common.save")}
          </Button>
        </div>
      </form>
    </Section>
  );
}

/** Common workload presets preserve every integer in the backend's legal range. */
function numericPresets(name: string, maximum: number): number[] {
  if (name.endsWith("_bytes")) return [1024, maximum / 4, maximum / 2, maximum];
  if (name.endsWith("_days")) return [1, 7, 14, maximum];
  if (name.endsWith("_seconds")) return [60, 900, 1800, maximum];
  if (maximum <= 3) return [0, 1, 2, maximum];
  if (maximum <= 16) return [1, 4, 8, maximum];
  return [Math.round(maximum / 4), Math.round(maximum / 2), maximum];
}
