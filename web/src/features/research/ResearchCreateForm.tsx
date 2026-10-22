/**
 * Research creation through `POST /v1/admin/research` with the in-memory CSRF token and a
 * persistent `Idempotency-Key`. When the outcome is unknown the exact request body and
 * key are kept in sessionStorage and the form is locked so only an identical retry, or
 * an explicit discard, is possible.
 */
import { useEffect, useId, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiFailure, call, callWithStatus, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import type { MessageKey } from "../../lib/messages";
import { useSession } from "../../lib/session";
import {
  clearPendingSubmission,
  loadPendingSubmission,
  prepareSubmission,
  savePendingSubmission,
} from "../../lib/researchDraft";
import type { PendingSubmission } from "../../lib/researchDraft";
import { InlineError, Notice } from "../../components/Feedback";
import { Dialog } from "../../components/Dialog";
import { useNotify } from "../../components/Notifications";
import { PermissionHint } from "../../components/Page";

/** Research profiles selectable at creation. */
export type ResearchProfile = NonNullable<Schemas["ResearchCreate"]["profile"]>;

interface FormValues {
  input: string;
  profile: ResearchProfile;
  usdCap: string;
  credits: string;
  acknowledgeEstimate: boolean;
  observationSeconds: string;
  outputSchema: string;
}

type FieldName = "input" | "budget" | "acknowledgeEstimate" | "observationSeconds" | "outputSchema";

type FieldErrors = Partial<Record<FieldName, MessageKey>>;

const MINIMUM_CREDITS: Record<Exclude<ResearchProfile, "exa_auto">, number> = {
  tavily_mini: 110,
  tavily_pro: 250,
};

const MAXIMUM_CREDITS = 250;

const DEFAULT_VALUES: FormValues = {
  input: "",
  profile: "exa_auto",
  usdCap: "1",
  credits: "110",
  acknowledgeEstimate: false,
  observationSeconds: "",
  outputSchema: "",
};

/**
 * Measure the nesting depth and widest object of a JSON value.
 *
 * @param value - Parsed JSON.
 * @param depth - Current depth.
 * @returns True when the schema respects depth 8 and 128 properties per object.
 */
function isWithinSchemaShape(value: unknown, depth = 0): boolean {
  if (depth > 8) {
    return false;
  }
  if (Array.isArray(value)) {
    return value.every((entry) => isWithinSchemaShape(entry, depth + 1));
  }
  if (typeof value === "object" && value !== null) {
    const entries = Object.values(value);
    return entries.length <= 128 && entries.every((entry) => isWithinSchemaShape(entry, depth + 1));
  }
  return true;
}

/**
 * Restore form values from a stored request body.
 *
 * @param body - Stored body.
 * @returns Form values representing the body.
 */
function valuesFromBody(body: Schemas["ResearchCreate"]): FormValues {
  const profile = body.profile ?? (body.budget.mode === "hard" ? "exa_auto" : "tavily_mini");
  return {
    input: body.input,
    profile,
    usdCap:
      body.budget.mode === "hard" ? String(body.budget.amount / 1_000_000) : DEFAULT_VALUES.usdCap,
    credits: body.budget.mode === "estimated" ? String(body.budget.amount) : DEFAULT_VALUES.credits,
    acknowledgeEstimate: body.budget.mode === "estimated",
    observationSeconds:
      body.observation_seconds === undefined ? "" : String(body.observation_seconds),
    outputSchema:
      body.output_schema === undefined ? "" : JSON.stringify(body.output_schema, null, 2),
  };
}

/**
 * Validate the form and build the contract request body.
 *
 * @param values - Form values.
 * @returns The body or field errors.
 */
export function buildResearchBody(
  values: FormValues,
): { body: Schemas["ResearchCreate"]; errors: null } | { body: null; errors: FieldErrors } {
  const errors: FieldErrors = {};
  const input = values.input;
  if (input.trim().length === 0 || input.length > 32000) {
    errors.input = "research.form.inputInvalid";
  }

  let budget: Schemas["ResearchBudget"] | null = null;
  if (values.profile === "exa_auto") {
    const text = values.usdCap.trim();
    const amount = /^\d{1,3}(\.\d{1,6})?$/.test(text)
      ? Math.round(Number(text) * 1_000_000)
      : Number.NaN;
    if (Number.isInteger(amount) && amount >= 1_000_000 && amount <= 100_000_000) {
      budget = { mode: "hard", unit: "micro_usd", amount };
    } else {
      errors.budget = "research.form.usdInvalid";
    }
  } else {
    const minimum = MINIMUM_CREDITS[values.profile];
    const amount = /^\d+$/.test(values.credits.trim()) ? Number(values.credits.trim()) : Number.NaN;
    if (!Number.isInteger(amount) || amount < minimum || amount > MAXIMUM_CREDITS) {
      errors.budget =
        values.profile === "tavily_pro"
          ? "research.form.creditsInvalidPro"
          : "research.form.creditsInvalidMini";
    } else if (!values.acknowledgeEstimate) {
      errors.acknowledgeEstimate = "research.form.acknowledgeRequired";
    } else {
      budget = {
        mode: "estimated",
        unit: "tavily_credit",
        amount,
        acknowledge_estimated_budget: true,
      };
    }
  }

  let observationSeconds: number | undefined;
  if (values.observationSeconds.trim() !== "") {
    const parsed = Number(values.observationSeconds.trim());
    if (Number.isInteger(parsed) && parsed >= 60 && parsed <= 3600) {
      observationSeconds = parsed;
    } else {
      errors.observationSeconds = "research.form.observationInvalid";
    }
  }

  let outputSchema: Schemas["OutputSchema"] | undefined;
  if (values.outputSchema.trim() !== "") {
    try {
      const parsed: unknown = JSON.parse(values.outputSchema);
      const size = new TextEncoder().encode(values.outputSchema).length;
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
        errors.outputSchema = "research.form.schemaNotObject";
      } else if (size > 16 * 1024 || !isWithinSchemaShape(parsed)) {
        errors.outputSchema = "research.form.schemaTooLarge";
      } else {
        outputSchema = parsed as Schemas["OutputSchema"];
      }
    } catch {
      errors.outputSchema = "research.form.schemaInvalidJson";
    }
  }

  if (Object.keys(errors).length > 0 || budget === null) {
    return { body: null, errors };
  }
  const body: Schemas["ResearchCreate"] = { input, profile: values.profile, budget };
  if (observationSeconds !== undefined) {
    body.observation_seconds = observationSeconds;
  }
  if (outputSchema !== undefined) {
    body.output_schema = outputSchema;
  }
  return { body, errors: null };
}

/**
 * Read the stored submission; a reload during sending is an unknown outcome.
 *
 * @returns Stored submission or null.
 */
function readStoredSubmission(principalId: string): PendingSubmission | null {
  const stored = loadPendingSubmission(principalId);
  return stored?.state === "sending"
    ? { ...stored, state: "uncertain", hasUnresolvedAttempt: true }
    : stored;
}

/**
 * Research creation form.
 *
 * @returns Form element.
 */
export function ResearchCreateForm() {
  const { t } = useI18n();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const notify = useNotify();
  const { can, csrfToken, session } = useSession();
  const principalId = session?.principal_id ?? "";
  const formId = useId();
  const canCreate = can("admin:research:create");
  const isMounted = useRef(true);
  useEffect(() => {
    isMounted.current = true;
    return () => {
      isMounted.current = false;
    };
  }, []);

  const [initial] = useState(() => {
    try {
      return { pending: readStoredSubmission(principalId), failed: false };
    } catch {
      return { pending: null, failed: true };
    }
  });
  const [pending, setPending] = useState<PendingSubmission | null>(initial.pending);
  const [values, setValues] = useState<FormValues>(() =>
    pending === null ? DEFAULT_VALUES : valuesFromBody(pending.body),
  );
  const [storageFailed, setStorageFailed] = useState(initial.failed);
  const [errors, setErrors] = useState<FieldErrors>({});
  const [isDiscardOpen, setDiscardOpen] = useState(false);

  const capabilities = useQuery({
    queryKey: ["connection-info"],
    queryFn: () => call(api.GET("/v1/admin/connection-info")),
    enabled: can("admin:read"),
  });
  const researchCapability = capabilities.data?.capabilities.capabilities.find(
    (capability) => capability.operation === "research_create",
  );
  const selectedProfile = researchCapability?.profiles.find(
    (entry) => entry.profile === values.profile,
  );

  const persist = (next: PendingSubmission | null) => {
    if (next === null) {
      clearPendingSubmission(principalId);
    } else {
      savePendingSubmission(next);
    }
    setPending(next);
    setStorageFailed(false);
  };

  const submit = useMutation({
    mutationFn: async (submission: PendingSubmission) => {
      const result = await callWithStatus(
        api.POST("/v1/admin/research", {
          params: {
            header: { "Idempotency-Key": submission.idempotencyKey, "X-Exta-CSRF": csrfToken },
          },
          body: submission.body,
        }),
      );
      const job = result.data;
      if (
        typeof job !== "object" ||
        job === null ||
        typeof job.id !== "string" ||
        !/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(job.id) ||
        job.owner_principal_id !== submission.principalId ||
        !Number.isSafeInteger(job.version) ||
        job.version < 1
      ) {
        throw new ApiFailure("malformed", result.status, null);
      }
      return result;
    },
    onSuccess: ({ data: job }, submission) => {
      try {
        if (loadPendingSubmission(principalId)?.attemptId !== submission.attemptId) return;
        persist(null);
      } catch {
        setStorageFailed(true);
      }
      if (!isMounted.current) return;
      setValues(DEFAULT_VALUES);
      void queryClient.invalidateQueries({ queryKey: ["research"] });
      notify("info", t("research.form.accepted", { id: job.id }));
      void navigate(`/research/${encodeURIComponent(job.id)}`);
    },
    onError: (error, submission) => {
      const isUnknown =
        submission.hasUnresolvedAttempt ||
        !(error instanceof ApiFailure) ||
        error.isOutcomeUnknown ||
        error.code === "idempotency_conflict";
      const next: PendingSubmission = {
        ...submission,
        hasUnresolvedAttempt: isUnknown,
        state: isUnknown ? "uncertain" : "rejected",
        updatedAt: new Date().toISOString(),
      };
      try {
        if (loadPendingSubmission(principalId)?.attemptId !== submission.attemptId) return;
        setPending(next);
        persist(next);
      } catch {
        setPending({ ...next, state: "uncertain", hasUnresolvedAttempt: true });
        setStorageFailed(true);
      }
    },
  });

  const isLocked = pending?.state === "uncertain";
  const isBusy = submit.isPending;
  const isDisabled = !canCreate || isLocked || isBusy || initial.failed;

  const send = (submission: PendingSubmission) => {
    if (!canCreate || submit.isPending || initial.failed) {
      return;
    }
    const sending: PendingSubmission = {
      ...submission,
      attemptId: crypto.randomUUID(),
      state: "sending",
      updatedAt: new Date().toISOString(),
    };
    try {
      persist(sending);
    } catch {
      setStorageFailed(true);
      return;
    }
    submit.mutate(sending);
  };

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (isDisabled) {
      return;
    }
    const result = buildResearchBody(values);
    setErrors(result.errors ?? {});
    if (result.body === null) {
      return;
    }
    send(prepareSubmission(result.body, pending, principalId));
  };

  const update = <K extends keyof FormValues>(name: K, value: FormValues[K]) => {
    setValues((current) => ({ ...current, [name]: value }));
  };

  const errorId = (name: FieldName) => `${formId}-${name}-error`;
  const fieldError = (name: FieldName) =>
    errors[name] === undefined ? null : (
      <p id={errorId(name)} className="field-error">
        {t(errors[name])}
      </p>
    );
  const describedBy = (name: FieldName, hintId?: string) =>
    [hintId, errors[name] === undefined ? undefined : errorId(name)].filter(Boolean).join(" ") ||
    undefined;

  const isEstimated = values.profile !== "exa_auto";

  return (
    <div className="research-create">
      <PermissionHint permission="admin:research:create" />
      {storageFailed ? (
        <Notice tone="danger" role="alert">
          {t("research.form.storageFailed")}
          {initial.failed ? (
            <button type="button" className="button" onClick={() => window.location.reload()}>
              {t("common.refresh")}
            </button>
          ) : null}
        </Notice>
      ) : null}
      {isLocked && pending !== null ? (
        <Notice tone="warning" role="alert" title={t("research.form.unknownTitle")}>
          <p>{t("research.form.unknownDetail")}</p>
          <p className="muted">
            {t("research.form.retryKey")}: <code>{pending.idempotencyKey}</code>
          </p>
          <div className="button-row">
            <button
              type="button"
              className="button button-primary"
              onClick={() => send(pending)}
              disabled={!canCreate || isBusy}
            >
              {isBusy ? t("research.form.submitting") : t("research.form.retrySame")}
            </button>
            <button
              type="button"
              className="button"
              onClick={() => setDiscardOpen(true)}
              disabled={isBusy}
            >
              {t("research.form.discard")}
            </button>
          </div>
        </Notice>
      ) : null}
      {pending?.state === "rejected" && submit.isIdle ? (
        <Notice tone="info" role="status" title={t("research.form.restoredTitle")}>
          {t("research.form.restoredDetail")}
        </Notice>
      ) : null}
      {submit.isError && !isLocked ? <InlineError error={submit.error} /> : null}
      {submit.isError &&
      submit.error instanceof ApiFailure &&
      submit.error.code === "idempotency_conflict" ? (
        <Notice tone="danger" role="alert" title={t("research.form.conflictTitle")}>
          {t("research.form.conflictDetail")}
        </Notice>
      ) : null}
      <form
        className="form"
        onSubmit={onSubmit}
        aria-describedby={`${formId}-disclosure`}
        noValidate
      >
        <div className="field">
          <label htmlFor={`${formId}-input`}>{t("research.form.input")}</label>
          <textarea
            id={`${formId}-input`}
            rows={5}
            value={values.input}
            onChange={(event) => update("input", event.target.value)}
            readOnly={isLocked}
            disabled={!canCreate}
            aria-invalid={errors.input !== undefined}
            aria-describedby={describedBy("input")}
            maxLength={32000}
          />
          {fieldError("input")}
        </div>
        <div className="field-row">
          <div className="field">
            <label htmlFor={`${formId}-profile`}>{t("research.form.profile")}</label>
            <select
              id={`${formId}-profile`}
              value={values.profile}
              onChange={(event) => {
                const profile = event.target.value as ResearchProfile;
                setValues((current) => ({
                  ...current,
                  profile,
                  credits:
                    profile === "exa_auto" ? current.credits : String(MINIMUM_CREDITS[profile]),
                  acknowledgeEstimate: false,
                }));
              }}
              disabled={isDisabled}
            >
              <option value="exa_auto">{t("profile.exa_auto")}</option>
              <option value="tavily_mini">{t("profile.tavily_mini")}</option>
              <option value="tavily_pro">{t("profile.tavily_pro")}</option>
            </select>
          </div>
          {isEstimated ? (
            <div className="field">
              <label htmlFor={`${formId}-credits`}>{t("research.form.credits")}</label>
              <input
                id={`${formId}-credits`}
                inputMode="numeric"
                value={values.credits}
                onChange={(event) => update("credits", event.target.value)}
                disabled={isDisabled}
                aria-invalid={errors.budget !== undefined}
                aria-describedby={describedBy("budget", `${formId}-credits-hint`)}
              />
              <p id={`${formId}-credits-hint`} className="field-hint">
                {values.profile === "tavily_pro"
                  ? t("research.form.creditsHintPro")
                  : t("research.form.creditsHintMini")}
              </p>
              {fieldError("budget")}
            </div>
          ) : (
            <div className="field">
              <label htmlFor={`${formId}-usd`}>{t("research.form.usdCap")}</label>
              <input
                id={`${formId}-usd`}
                inputMode="decimal"
                value={values.usdCap}
                onChange={(event) => update("usdCap", event.target.value)}
                disabled={isDisabled}
                aria-invalid={errors.budget !== undefined}
                aria-describedby={describedBy("budget", `${formId}-usd-hint`)}
              />
              <p id={`${formId}-usd-hint`} className="field-hint">
                {t("research.form.usdHint")}
              </p>
              {fieldError("budget")}
            </div>
          )}
          <div className="field">
            <label htmlFor={`${formId}-observation`}>{t("research.form.observation")}</label>
            <input
              id={`${formId}-observation`}
              inputMode="numeric"
              placeholder="3600"
              value={values.observationSeconds}
              onChange={(event) => update("observationSeconds", event.target.value)}
              disabled={isDisabled}
              aria-invalid={errors.observationSeconds !== undefined}
              aria-describedby={describedBy("observationSeconds", `${formId}-observation-hint`)}
            />
            <p id={`${formId}-observation-hint`} className="field-hint">
              {t("research.form.observationHint")}
            </p>
            {fieldError("observationSeconds")}
          </div>
        </div>
        <div id={`${formId}-disclosure`}>
          {isEstimated ? (
            <Notice tone="warning" title={t("research.form.estimatedTitle")}>
              {t("research.form.estimatedDetail")}
            </Notice>
          ) : (
            <Notice tone="info" title={t("research.form.hardTitle")}>
              {t("research.form.hardDetail")}
            </Notice>
          )}
          {selectedProfile !== undefined &&
          (!selectedProfile.enabled || selectedProfile.unavailable_reason !== null) ? (
            <Notice tone="warning" title={t("research.form.profileUnavailable")}>
              {selectedProfile.unavailable_reason ?? t("common.unknown")}
            </Notice>
          ) : null}
        </div>
        {isEstimated ? (
          <div className="field field-checkbox">
            <input
              id={`${formId}-ack`}
              type="checkbox"
              checked={values.acknowledgeEstimate}
              onChange={(event) => update("acknowledgeEstimate", event.target.checked)}
              disabled={isDisabled}
              aria-invalid={errors.acknowledgeEstimate !== undefined}
              aria-describedby={describedBy("acknowledgeEstimate")}
            />
            <label htmlFor={`${formId}-ack`}>{t("research.form.acknowledge")}</label>
            {fieldError("acknowledgeEstimate")}
          </div>
        ) : null}
        <details className="advanced">
          <summary>{t("research.form.advanced")}</summary>
          <div className="field">
            <label htmlFor={`${formId}-schema`}>{t("research.form.outputSchema")}</label>
            <textarea
              id={`${formId}-schema`}
              rows={6}
              className="mono"
              value={values.outputSchema}
              onChange={(event) => update("outputSchema", event.target.value)}
              readOnly={isLocked}
              disabled={!canCreate}
              spellCheck={false}
              aria-invalid={errors.outputSchema !== undefined}
              aria-describedby={describedBy("outputSchema", `${formId}-schema-hint`)}
            />
            <p id={`${formId}-schema-hint`} className="field-hint">
              {t("research.form.outputSchemaHint")}
            </p>
            {fieldError("outputSchema")}
          </div>
        </details>
        <div className="button-row">
          <button type="submit" className="button button-primary" disabled={isDisabled}>
            {isBusy ? t("research.form.submitting") : t("research.form.submit")}
          </button>
        </div>
      </form>
      {isDiscardOpen && pending !== null ? (
        <Dialog
          title={t("research.form.discardTitle")}
          description={t("research.form.discardDetail")}
          onClose={() => setDiscardOpen(false)}
        >
          <div className="button-row">
            <button
              type="button"
              className="button"
              onClick={() => setDiscardOpen(false)}
              data-autofocus
            >
              {t("common.cancel")}
            </button>
            <button
              type="button"
              className="button button-danger"
              onClick={() => {
                try {
                  persist(null);
                } catch {
                  setStorageFailed(true);
                  return;
                }
                submit.reset();
                setDiscardOpen(false);
              }}
            >
              {t("research.form.discardConfirm")}
            </button>
          </div>
        </Dialog>
      ) : null}
    </div>
  );
}
