/**
 * Account and key mutation dialogs. Secrets are write-only: they live in a password field
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
type Account = Schemas["Account"];
type ProviderKey = Schemas["ProviderKey"];

const PROVIDER_UNIT: Record<Provider, Schemas["UsageUnit"]> = {
  exa: "micro_usd",
  tavily: "tavily_credit",
};

/**
 * Invalidate account and key caches after a change or conflict.
 *
 * @returns Refresh callback.
 */
function useRefreshPools(): () => void {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: ["accounts"] });
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
 * Create or edit an account. Edits send the account's current version.
 *
 * @param props - Provider, optional existing account and close callback.
 * @returns Dialog element.
 */
export function AccountDialog({
  provider,
  account,
  onClose,
}: {
  provider: Provider;
  account?: Account;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const refresh = useRefreshPools();
  const format = useUnitFormatter();
  const formId = useId();
  const unit = account?.quota_policy.unit ?? PROVIDER_UNIT[provider];
  const [label, setLabel] = useState(account?.label ?? "");
  const [externalRef, setExternalRef] = useState(account?.external_ref ?? "");
  const [isEnabled, setEnabled] = useState(account?.enabled ?? true);
  const [verification, setVerification] = useState<Account["verification_state"]>(
    account?.verification_state ?? "unverified",
  );
  const [allocation, setAllocation] = useState(String(account?.quota_policy.allocation ?? 0));
  const [perOperation, setPerOperation] = useState(
    String(account?.quota_policy.per_operation_reservation ?? 0),
  );
  const [activeJobs, setActiveJobs] = useState(String(account?.quota_policy.active_jobs ?? 1));

  const allocationValue = parseInteger(allocation);
  const perOperationValue = parseInteger(perOperation);
  const activeJobsValue = parseInteger(activeJobs);
  const isValid =
    label.trim().length >= 1 &&
    label.length <= 128 &&
    externalRef.length <= 256 &&
    !Number.isNaN(allocationValue) &&
    !Number.isNaN(perOperationValue) &&
    (activeJobsValue === 1 || activeJobsValue === 2);

  const save = useMutation({
    mutationFn: () => {
      const quotaPolicy: Schemas["QuotaPolicy"] = {
        unit,
        allocation: allocationValue,
        per_operation_reservation: perOperationValue,
        active_jobs: activeJobsValue,
      };
      const reference = externalRef.trim() === "" ? null : externalRef.trim();
      if (account === undefined) {
        return call(
          api.POST("/v1/admin/accounts", {
            params: { header: { "X-Exta-CSRF": csrfToken } },
            body: {
              provider,
              label: label.trim(),
              external_ref: reference,
              enabled: isEnabled,
              verification_state: verification,
              quota_policy: quotaPolicy,
            },
          }),
        );
      }
      return call(
        api.PATCH("/v1/admin/accounts/{id}", {
          params: { path: { id: account.id }, header: { "X-Exta-CSRF": csrfToken } },
          body: {
            expected_version: account.version,
            label: label.trim(),
            external_ref: reference,
            enabled: isEnabled,
            verification_state: verification,
            quota_policy: quotaPolicy,
          },
        }),
      );
    },
    onSuccess: () => {
      refresh();
      notify("info", t("keys.accountSaved"));
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
      title={account === undefined ? t("keys.addAccount") : t("keys.editAccount")}
      description={t(`provider.${provider}`)}
      onClose={onClose}
      isBusy={save.isPending}
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
        <div className="field">
          <label htmlFor={`${formId}-label`}>{t("common.label")}</label>
          <input
            id={`${formId}-label`}
            value={label}
            onChange={(event) => setLabel(event.target.value)}
            maxLength={128}
            data-autofocus
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-ref`}>{t("keys.externalRef")}</label>
          <input
            id={`${formId}-ref`}
            value={externalRef}
            onChange={(event) => setExternalRef(event.target.value)}
            maxLength={256}
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-verification`}>{t("keys.verification")}</label>
          <select
            id={`${formId}-verification`}
            value={verification}
            onChange={(event) =>
              setVerification(event.target.value as Account["verification_state"])
            }
          >
            <option value="unverified">{t("verificationState.unverified")}</option>
            <option value="verified">{t("verificationState.verified")}</option>
          </select>
        </div>
        <fieldset className="fieldset">
          <legend>{t("keys.quotaPolicy")}</legend>
          <p className="field-hint">
            {t("keys.quotaUnit", { unit: unit === "micro_usd" ? "micro-USD" : "Tavily credits" })}
          </p>
          <div className="field-row">
            <div className="field">
              <label htmlFor={`${formId}-allocation`}>{t("keys.allocation")}</label>
              <input
                id={`${formId}-allocation`}
                inputMode="numeric"
                value={allocation}
                onChange={(event) => setAllocation(event.target.value)}
                aria-invalid={Number.isNaN(allocationValue)}
                aria-describedby={`${formId}-allocation-preview`}
              />
              <p id={`${formId}-allocation-preview`} className="field-hint">
                {Number.isNaN(allocationValue)
                  ? t("common.invalidNumber")
                  : format(allocationValue, unit)}
              </p>
            </div>
            <div className="field">
              <label htmlFor={`${formId}-per-op`}>{t("keys.perOperation")}</label>
              <input
                id={`${formId}-per-op`}
                inputMode="numeric"
                value={perOperation}
                onChange={(event) => setPerOperation(event.target.value)}
                aria-invalid={Number.isNaN(perOperationValue)}
                aria-describedby={`${formId}-per-op-preview`}
              />
              <p id={`${formId}-per-op-preview`} className="field-hint">
                {Number.isNaN(perOperationValue)
                  ? t("common.invalidNumber")
                  : format(perOperationValue, unit)}
              </p>
            </div>
            <div className="field">
              <label htmlFor={`${formId}-active`}>{t("keys.activeJobs")}</label>
              <select
                id={`${formId}-active`}
                value={activeJobs}
                onChange={(event) => setActiveJobs(event.target.value)}
              >
                <option value="1">1</option>
                <option value="2">2</option>
              </select>
            </div>
          </div>
          <p className="field-hint">{t("keys.noBudgetNoDispatch")}</p>
        </fieldset>
        <div className="field field-checkbox">
          <input
            id={`${formId}-enabled`}
            type="checkbox"
            checked={isEnabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          <label htmlFor={`${formId}-enabled`}>{t("common.enabled")}</label>
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
      <label htmlFor={id}>{t("keys.secret")}</label>
      <input
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
 * Add a key to an account.
 *
 * @param props - Account ID and close callback.
 * @returns Dialog element.
 */
export function KeyCreateDialog({
  accountId,
  onClose,
}: {
  accountId: string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const refresh = useRefreshPools();
  const formId = useId();
  const [label, setLabel] = useState("");
  const [secret, setSecret] = useState("");
  const [isEnabled, setEnabled] = useState(true);
  const isValid =
    label.trim().length >= 1 && label.length <= 128 && secret.length >= 8 && secret.length <= 4096;
  const create = useMutation({
    gcTime: 0,
    mutationFn: (value: { secret: string }) =>
      call(
        api.POST("/v1/admin/keys", {
          params: { header: { "X-Exta-CSRF": csrfToken } },
          body: {
            account_id: accountId,
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
      title={t("keys.addKey")}
      description={accountId}
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
          <label htmlFor={`${formId}-label`}>{t("common.label")}</label>
          <input
            id={`${formId}-label`}
            value={label}
            onChange={(event) => setLabel(event.target.value)}
            maxLength={128}
            data-autofocus
          />
        </div>
        <SecretField id={`${formId}-secret`} value={secret} onChange={setSecret} />
        <div className="field field-checkbox">
          <input
            id={`${formId}-enabled`}
            type="checkbox"
            checked={isEnabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          <label htmlFor={`${formId}-enabled`}>{t("common.enabled")}</label>
        </div>
        <InlineError error={create.error} />
        <div className="button-row">
          <button type="button" className="button" onClick={onClose} disabled={create.isPending}>
            {t("common.cancel")}
          </button>
          <button
            type="submit"
            className="button button-primary"
            disabled={!isValid || create.isPending}
          >
            {create.isPending ? t("common.working") : t("keys.addKey")}
          </button>
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
          <button type="button" className="button" onClick={onClose} disabled={rotate.isPending}>
            {t("common.cancel")}
          </button>
          <button
            type="submit"
            className="button button-primary"
            disabled={!isValid || rotate.isPending || isConflict(rotate.error)}
          >
            {rotate.isPending ? t("common.working") : t("keys.rotate")}
          </button>
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
        <button
          type="button"
          className="button"
          onClick={onClose}
          disabled={retire.isPending}
          data-autofocus
        >
          {t("common.cancel")}
        </button>
        <button
          type="button"
          className="button button-danger"
          onClick={() => {
            if (!isConflict(retire.error)) retire.mutate();
          }}
          disabled={retire.isPending || isConflict(retire.error)}
        >
          {retire.isPending ? t("common.working") : t("keys.retire")}
        </button>
      </div>
    </Dialog>
  );
}
