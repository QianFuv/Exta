import { NumericControl } from "../../components/NumericControl";
import { SwitchField, Button, Input } from "../../components/Controls";
/**
 * Key mutation dialogs. Secrets are write-only: they live in a password field
 * of a dialog that unmounts on close, are cleared after every attempt and are never
 * cached, logged or stored.
 */
import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ApiFailure, call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { Dialog } from "../../components/Dialog";
import { ConflictRecovery, isConflict, InlineError, Notice } from "../../components/Feedback";
import { useNotify } from "../../components/Notifications";
import { useUnitFormatter } from "../../components/Values";

type Provider = Schemas["Provider"];
type ProviderKey = Schemas["UpstreamKey"];

const PROVIDER_UNIT: Record<Provider, Schemas["UsageUnit"]> = {
  exa: "micro_usd",
  tavily: "tavily_credit",
};

/**
 * Invalidate key and usage caches after a change or conflict.
 *
 * @returns Refresh callback.
 */
function useRefreshPools(): () => void {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: ["keys"] });
    void queryClient.invalidateQueries({ queryKey: ["usage"] });
  };
}

/**
 * Parse a non-negative safe integer from text.
 *
 * @param text - Input text.
 * @returns Integer or NaN.
 */
function parseInteger(text: string): number {
  const trimmed = text.trim();
  if (trimmed === "") return 0;
  if (!/^\d+$/.test(trimmed)) {
    return Number.NaN;
  }
  const value = Number(trimmed);
  return Number.isSafeInteger(value) ? value : Number.NaN;
}

/**
 * Toggle a key's enabled flag with its current version.
 *
 * @param providerKey - Key to patch.
 * @returns Mutation.
 */
export function useKeyPatch(providerKey: ProviderKey) {
  const api = useApi();
  const { csrfToken } = useSession();
  const refresh = useRefreshPools();
  return useMutation({
    mutationFn: (changes: { enabled: boolean }) =>
      call(
        api.PATCH("/v1/admin/keys/{id}", {
          params: { path: { id: providerKey.id }, header: { "X-Exta-CSRF": csrfToken } },
          body: { expected_version: providerKey.version, ...changes },
        }),
      ),
    onSettled: refresh,
  });
}

/**
 * Edit a key and its local safety limits using the current revision.
 *
 * @param props - Existing key and close callback.
 * @returns Dialog element.
 */
export function KeySettingsDialog({
  providerKey,
  onClose,
}: {
  providerKey: ProviderKey;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const refresh = useRefreshPools();
  const format = useUnitFormatter();
  const formId = useId();
  const unit = providerKey.quota_policy.unit;
  const [label, setLabel] = useState(providerKey.label ?? "");
  const [isEnabled, setEnabled] = useState(providerKey.enabled);
  const [allocation, setAllocation] = useState(String(providerKey.quota_policy.allocation ?? 0));
  const [perOperation, setPerOperation] = useState(
    String(providerKey.quota_policy.per_operation_reservation ?? 0),
  );
  const [activeJobs, setActiveJobs] = useState(String(providerKey.quota_policy.active_jobs ?? 0));

  const allocationValue = parseInteger(allocation);
  const perOperationValue = parseInteger(perOperation);
  const activeJobsValue = parseInteger(activeJobs);
  const isValid =
    label.trim().length >= 1 &&
    label.length <= 128 &&
    !Number.isNaN(allocationValue) &&
    !Number.isNaN(perOperationValue) &&
    (activeJobsValue === 0 || activeJobsValue === 1 || activeJobsValue === 2);

  const save = useMutation({
    mutationFn: () => {
      const quotaPolicy: Schemas["QuotaPolicy"] = {
        unit,
        allocation: allocationValue,
        per_operation_reservation: perOperationValue,
        active_jobs: activeJobsValue,
      };
      return call(
        api.PATCH("/v1/admin/keys/{id}", {
          params: { path: { id: providerKey.id }, header: { "X-Exta-CSRF": csrfToken } },
          body: {
            expected_version: providerKey.version,
            label: label.trim(),
            enabled: isEnabled,
            quota_policy: quotaPolicy,
          },
        }),
      );
    },
    onSuccess: () => {
      refresh();
      notify("info", t("keys.keySaved"));
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.status === 409) {
        refresh();
      }
    },
  });

  return (
    <Dialog title={t("keys.editKey")} onClose={onClose} isBusy={save.isPending}>
      <form
        className="form"
        onSubmit={(event) => {
          event.preventDefault();
          if (isValid && !save.isPending && !isConflict(save.error)) {
            save.mutate();
          }
        }}
      >
        <div className="field">
          <label id={`${formId}-label` + "-label"} htmlFor={`${formId}-label`}>
            {t("common.label")}
          </label>
          <Input
            id={`${formId}-label`}
            value={label}
            onChange={(event) => setLabel(event.target.value)}
            maxLength={128}
            data-autofocus
          />
        </div>
        <fieldset className="fieldset">
          <legend>{t("keys.quotaPolicy")}</legend>
          <p className="field-hint">
            {t("keys.quotaUnit", {
              unit: t(unit === "micro_usd" ? "keys.unitMicroUsd" : "keys.unitCredits"),
            })}
          </p>
          <div className="field-row">
            <div className="field">
              <label id={`${formId}-allocation` + "-label"} htmlFor={`${formId}-allocation`}>
                {t("keys.allocation")}
              </label>
              <NumericControl
                emptyIsZero
                id={`${formId}-allocation`}
                minimum={0}
                maximum={Number.MAX_SAFE_INTEGER}
                presets={unit === "micro_usd" ? [0, 250000, 500000, 1000000] : [0, 250, 500, 1000]}
                zeroLabel={t("keys.unlimited")}
                value={allocation}
                onChange={setAllocation}
                invalid={Number.isNaN(allocationValue)}
                describedBy={`${formId}-allocation-preview`}
              />
              <p id={`${formId}-allocation-preview`} className="field-hint">
                {Number.isNaN(allocationValue)
                  ? t("common.invalidNumber")
                  : allocationValue === 0
                    ? t("keys.unlimited")
                    : format(allocationValue, unit)}
              </p>
            </div>
            <div className="field">
              <label id={`${formId}-per-op` + "-label"} htmlFor={`${formId}-per-op`}>
                {t("keys.perOperation")}
              </label>
              <NumericControl
                emptyIsZero
                id={`${formId}-per-op`}
                minimum={0}
                maximum={Number.MAX_SAFE_INTEGER}
                presets={unit === "micro_usd" ? [0, 250000, 500000, 1000000] : [0, 250, 500, 1000]}
                zeroLabel={t("keys.automaticReservation")}
                value={perOperation}
                onChange={setPerOperation}
                invalid={Number.isNaN(perOperationValue)}
                describedBy={`${formId}-per-op-preview`}
              />
              <p id={`${formId}-per-op-preview`} className="field-hint">
                {Number.isNaN(perOperationValue)
                  ? t("common.invalidNumber")
                  : perOperationValue === 0
                    ? t("keys.automaticReservation")
                    : format(perOperationValue, unit)}
              </p>
            </div>
            <div className="field">
              <label id={`${formId}-active` + "-label"} htmlFor={`${formId}-active`}>
                {t("keys.activeJobs")}
              </label>
              <NumericControl
                emptyIsZero
                id={`${formId}-active`}
                minimum={0}
                maximum={2}
                presets={[0, 1, 2]}
                zeroLabel={t("keys.unlimited")}
                value={activeJobs}
                onChange={setActiveJobs}
              />
            </div>
          </div>
          <p className="field-hint">{t("keys.noBudgetNoDispatch")}</p>
        </fieldset>
        <div className="field field-checkbox">
          <SwitchField
            label={t("keys.enableKey")}
            id={`${formId}-enabled`}
            checked={isEnabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
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

/**
 * Write-only secret input shared by create and rotate dialogs.
 *
 * @param props - Field ID, value and change handler.
 * @returns Field element.
 */
function SecretField({
  id,
  value,
  onChange,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="field">
      <label id={id + "-label"} htmlFor={id}>
        {t("keys.secret")}
      </label>
      <Input
        id={id}
        type="password"
        autoComplete="off"
        spellCheck={false}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-describedby={`${id}-hint`}
        minLength={8}
        maxLength={4096}
      />
      <p id={`${id}-hint`} className="field-hint">
        {t("keys.secretHint")}
      </p>
    </div>
  );
}

/**
 * Add an upstream key directly with optional local safety limits.
 *
 * @param props - Provider and close callback.
 * @returns Dialog element.
 */
export function KeyCreateDialog({
  provider,
  onClose,
}: {
  provider: Provider;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const refresh = useRefreshPools();
  const formId = useId();
  const format = useUnitFormatter();
  const [label, setLabel] = useState("");
  const [secret, setSecret] = useState("");
  const [allocation, setAllocation] = useState("0");
  const [perOperation, setPerOperation] = useState("0");
  const [isEnabled, setEnabled] = useState(true);
  const isValid =
    label.trim().length >= 1 &&
    label.length <= 128 &&
    secret.length >= 8 &&
    secret.length <= 4096 &&
    Number.isSafeInteger(parseInteger(allocation)) &&
    Number.isSafeInteger(parseInteger(perOperation));
  const create = useMutation({
    gcTime: 0,
    mutationFn: (value: { secret: string }) =>
      call(
        api.POST("/v1/admin/keys", {
          params: { header: { "X-Exta-CSRF": csrfToken } },
          body: {
            provider,
            quota_policy: {
              unit: PROVIDER_UNIT[provider],
              allocation: parseInteger(allocation),
              per_operation_reservation: parseInteger(perOperation),
              active_jobs: 0,
            },
            label: label.trim(),
            secret: value.secret,
            enabled: isEnabled,
          },
        }),
      ),
    onSuccess: () => {
      refresh();
      notify("info", t("keys.keyAdded"));
      onClose();
    },
    onSettled: (...settled) => {
      settled[2].secret = "";
      setSecret("");
    },
  });
  return (
    <Dialog
      title={`${t("keys.addKey")} · ${t(`provider.${provider}`)}`}
      onClose={onClose}
      isBusy={create.isPending}
    >
      <form
        className="form"
        onSubmit={(event) => {
          event.preventDefault();
          if (isValid && !create.isPending) {
            create.mutate({ secret });
          }
        }}
      >
        <div className="field">
          <label id={`${formId}-label` + "-label"} htmlFor={`${formId}-label`}>
            {t("common.label")}
          </label>
          <Input
            id={`${formId}-label`}
            value={label}
            onChange={(event) => setLabel(event.target.value)}
            maxLength={128}
            data-autofocus
          />
        </div>
        <SecretField id={`${formId}-secret`} value={secret} onChange={setSecret} />
        <div className="field-row">
          <div className="field">
            <label id={`${formId}-allocation-label`} htmlFor={`${formId}-allocation`}>
              {t("keys.allocation")}
            </label>
            <NumericControl
              emptyIsZero
              id={`${formId}-allocation`}
              minimum={0}
              maximum={Number.MAX_SAFE_INTEGER}
              presets={provider === "exa" ? [0, 250000, 500000, 1000000] : [0, 250, 500, 1000]}
              zeroLabel={t("keys.unlimited")}
              value={allocation}
              onChange={setAllocation}
              describedBy={`${formId}-allocation-preview`}
            />
            <p id={`${formId}-allocation-preview`} className="field-hint">
              {Number.isSafeInteger(parseInteger(allocation))
                ? parseInteger(allocation) === 0
                  ? t("keys.unlimited")
                  : format(parseInteger(allocation), PROVIDER_UNIT[provider])
                : t("common.invalidNumber")}
            </p>
          </div>
          <div className="field">
            <label id={`${formId}-reservation-label`} htmlFor={`${formId}-reservation`}>
              {t("keys.perOperation")}
            </label>
            <NumericControl
              emptyIsZero
              id={`${formId}-reservation`}
              minimum={0}
              maximum={Number.MAX_SAFE_INTEGER}
              presets={provider === "exa" ? [0, 250000, 500000, 1000000] : [0, 250, 500, 1000]}
              zeroLabel={t("keys.automaticReservation")}
              value={perOperation}
              onChange={setPerOperation}
              describedBy={`${formId}-reservation-preview`}
            />
            <p id={`${formId}-reservation-preview`} className="field-hint">
              {Number.isSafeInteger(parseInteger(perOperation))
                ? parseInteger(perOperation) === 0
                  ? t("keys.automaticReservation")
                  : format(parseInteger(perOperation), PROVIDER_UNIT[provider])
                : t("common.invalidNumber")}
            </p>
          </div>
        </div>
        <p className="field-hint">
          {t("keys.quotaUnit", {
            unit: t(provider === "exa" ? "keys.unitMicroUsd" : "keys.unitCredits"),
          })}{" "}
          {t("keys.noBudgetNoDispatch")}
        </p>
        <div className="field field-checkbox">
          <SwitchField
            label={t("keys.enableKey")}
            id={`${formId}-enabled`}
            checked={isEnabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
        </div>
        <InlineError error={create.error} />
        <div className="button-row">
          <Button type="button" className="button" onClick={onClose} disabled={create.isPending}>
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            className="button button-primary"
            disabled={!isValid || create.isPending}
            loading={create.isPending}
          >
            {create.isPending ? t("common.working") : t("keys.addKey")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/**
 * Rotate a key's secret atomically under its current version.
 *
 * @param props - Key and close callback.
 * @returns Dialog element.
 */
export function KeyRotateDialog({
  providerKey,
  onClose,
}: {
  providerKey: ProviderKey;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const refresh = useRefreshPools();
  const formId = useId();
  const [secret, setSecret] = useState("");
  const isValid = secret.length >= 8 && secret.length <= 4096;
  const rotate = useMutation({
    gcTime: 0,
    mutationFn: (value: { secret: string }) =>
      call(
        api.POST("/v1/admin/keys/{id}/rotate", {
          params: { path: { id: providerKey.id }, header: { "X-Exta-CSRF": csrfToken } },
          body: { expected_version: providerKey.version, secret: value.secret },
        }),
      ),
    onSuccess: () => {
      refresh();
      notify("info", t("keys.rotated"));
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.status === 409) {
        refresh();
      }
    },
    onSettled: (...settled) => {
      settled[2].secret = "";
      setSecret("");
    },
  });
  return (
    <Dialog
      title={t("keys.rotateTitle", { label: providerKey.label })}
      description={t("keys.rotateDetail")}
      onClose={onClose}
      isBusy={rotate.isPending}
    >
      <form
        className="form"
        onSubmit={(event) => {
          event.preventDefault();
          if (isValid && !rotate.isPending && !isConflict(rotate.error)) {
            rotate.mutate({ secret });
          }
        }}
      >
        <SecretField id={`${formId}-secret`} value={secret} onChange={setSecret} />
        <InlineError error={rotate.error} />
        <ConflictRecovery error={rotate.error} onClose={onClose} />
        <div className="button-row">
          <Button type="button" className="button" onClick={onClose} disabled={rotate.isPending}>
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            className="button button-primary"
            disabled={!isValid || rotate.isPending || isConflict(rotate.error)}
            loading={rotate.isPending}
          >
            {rotate.isPending ? t("common.working") : t("keys.rotate")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/**
 * Retire a key; retained credential versions stay bound to active jobs.
 *
 * @param props - Key and close callback.
 * @returns Dialog element.
 */
export function KeyRetireDialog({
  providerKey,
  onClose,
}: {
  providerKey: ProviderKey;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const refresh = useRefreshPools();
  const retire = useMutation({
    mutationFn: () =>
      call(
        api.POST("/v1/admin/keys/{id}/retire", {
          params: { path: { id: providerKey.id }, header: { "X-Exta-CSRF": csrfToken } },
          body: { expected_version: providerKey.version },
        }),
      ),
    onSuccess: () => {
      refresh();
      notify("info", t("keys.retired"));
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.status === 409) {
        refresh();
      }
    },
  });
  return (
    <Dialog
      title={t("keys.retireTitle", { label: providerKey.label })}
      description={t("keys.retireDetail")}
      onClose={onClose}
      isBusy={retire.isPending}
    >
      {providerKey.retained_credential_count > 0 ? (
        <Notice tone="warning">
          {t("keys.retainedCredentials", { count: providerKey.retained_credential_count })}
        </Notice>
      ) : null}
      <InlineError error={retire.error} />
      <ConflictRecovery error={retire.error} onClose={onClose} />
      <div className="button-row">
        <Button
          type="button"
          className="button"
          onClick={onClose}
          disabled={retire.isPending}
          data-autofocus
        >
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          className="button button-danger"
          onClick={() => {
            if (!isConflict(retire.error)) retire.mutate();
          }}
          disabled={retire.isPending || isConflict(retire.error)}
        >
          {retire.isPending ? t("common.working") : t("keys.retire")}
        </Button>
      </div>
    </Dialog>
  );
}
