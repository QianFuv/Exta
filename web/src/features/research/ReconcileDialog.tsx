/**
 * One-action reconciliation of uncertain or held research jobs. Each action states its
 * separate effect on the active slot and the budget hold; risky actions require their
 * literal acknowledgement before the request can be sent.
 */
import { useId, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { ApiFailure, call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import type { MessageKey } from "../../lib/messages";
import { useSession } from "../../lib/session";
import { Dialog } from "../../components/Dialog";
import { ConflictRecovery, isConflict, InlineError, Notice } from "../../components/Feedback";
import { useNotify } from "../../components/Notifications";
import { useUnitFormatter } from "../../components/Values";
import { useJobUpdate } from "./useJobUpdate";
import type { Job } from "./jobStatus";

type ReconcileAction = Schemas["ReconcileRequest"]["action"];

const ACTIONS: ReadonlyArray<{
  action: ReconcileAction;
  label: MessageKey;
  effect: MessageKey;
  isRisky: boolean;
}> = [
  {
    action: "attach_upstream_id",
    label: "reconcile.attach",
    effect: "reconcile.attachEffect",
    isRisky: false,
  },
  {
    action: "settle_terminal",
    label: "reconcile.settle",
    effect: "reconcile.settleEffect",
    isRisky: false,
  },
  {
    action: "budget_write_off",
    label: "reconcile.writeOff",
    effect: "reconcile.writeOffEffect",
    isRisky: true,
  },
  {
    action: "capacity_release_override",
    label: "reconcile.override",
    effect: "reconcile.overrideEffect",
    isRisky: true,
  },
];

const DEFAULT_ACTION = {
  action: "attach_upstream_id",
  label: "reconcile.attach",
  effect: "reconcile.attachEffect",
  isRisky: false,
} as const;

const ID_PATTERN = /^[A-Za-z0-9_.:-]{1,200}$/;

/**
 * Reconciliation dialog.
 *
 * @param props - Job and close callback.
 * @returns Dialog element.
 */
export function ReconcileDialog({ job, onClose }: { job: Job; onClose: () => void }) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const onJob = useJobUpdate();
  const format = useUnitFormatter();
  const formId = useId();
  const actions = ACTIONS.filter((entry) => {
    if (entry.action === "attach_upstream_id")
      return (
        job.source_provider === "exa" &&
        ["submission_unknown", "observation_expired"].includes(job.state) &&
        job.upstream_job_id === null
      );
    if (entry.action === "settle_terminal") return job.upstream_job_id !== null;
    if (entry.action === "budget_write_off") return job.budget_hold_state === "held";
    return job.active_slot_held;
  });
  const [action, setAction] = useState<ReconcileAction>(actions[0]?.action ?? "settle_terminal");
  const [reason, setReason] = useState("");
  const [evidenceRef, setEvidenceRef] = useState("");
  const [upstreamJobId, setUpstreamJobId] = useState(job.upstream_job_id ?? "");
  const [upstreamState, setUpstreamState] = useState<"completed" | "failed" | "cancelled">(
    "completed",
  );
  const [hasReportedCost, setHasReportedCost] = useState(false);
  const [costAmount, setCostAmount] = useState("");
  const [isAcknowledged, setAcknowledged] = useState(false);

  const unit = job.reservation.unit;
  const costValue = /^\d+$/.test(costAmount) ? Number(costAmount) : Number.NaN;
  const isCostValid = !hasReportedCost || (Number.isSafeInteger(costValue) && costValue >= 0);
  const isReasonValid = reason.trim().length >= 8 && reason.length <= 1024;
  const isEvidenceValid = evidenceRef.trim().length >= 1 && evidenceRef.length <= 1024;
  const selected = ACTIONS.find((entry) => entry.action === action) ?? DEFAULT_ACTION;
  const isActionValid =
    action === "attach_upstream_id"
      ? upstreamJobId.length >= 1 && upstreamJobId.length <= 200 && ID_PATTERN.test(upstreamJobId)
      : action === "settle_terminal"
        ? isCostValid
        : isAcknowledged;
  const isValid =
    actions.some((entry) => entry.action === action) &&
    isReasonValid &&
    isEvidenceValid &&
    isActionValid;

  const buildRequest = (): Schemas["ReconcileRequest"] => {
    const common = {
      expected_version: job.version,
      reason: reason.trim(),
      evidence_ref: evidenceRef.trim(),
    };
    switch (action) {
      case "attach_upstream_id":
        return { ...common, action, upstream_job_id: upstreamJobId };
      case "settle_terminal":
        return {
          ...common,
          action,
          upstream_state: upstreamState,
          cost: hasReportedCost
            ? {
                unit,
                amount: costValue,
                certainty: "reported",
                source: "operator_evidence",
                observed_at: new Date().toISOString(),
                pricing_revision: null,
              }
            : null,
        };
      case "budget_write_off":
        return { ...common, action, acknowledge_upstream_may_bill: true };
      case "capacity_release_override":
        return { ...common, action, acknowledge_upstream_may_continue: true };
    }
  };

  const reconcile = useMutation({
    mutationFn: (body: Schemas["ReconcileRequest"]) =>
      call(
        api.POST("/v1/admin/research/{id}/reconcile", {
          params: { path: { id: job.id }, header: { "X-Exta-CSRF": csrfToken } },
          body,
        }),
      ),
    onSuccess: (updated) => {
      onJob(updated);
      notify("info", t("reconcile.recorded"));
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.status === 409) {
        onJob(job);
      }
    },
  });

  return (
    <Dialog
      title={t("reconcile.title")}
      description={t("reconcile.description")}
      onClose={onClose}
      isBusy={reconcile.isPending}
      size="large"
    >
      {job.source_provider === "tavily" && job.upstream_job_id === null ? (
        <Notice tone="info">{t("reconcile.tavilyAttachment")}</Notice>
      ) : null}
      <form
        className="form"
        onSubmit={(event) => {
          event.preventDefault();
          if (isValid && !reconcile.isPending && !isConflict(reconcile.error)) {
            reconcile.mutate(buildRequest());
          }
        }}
      >
        <fieldset className="radio-group">
          <legend>{t("reconcile.action")}</legend>
          {actions.map((entry) => (
            <label key={entry.action} className="radio">
              <input
                type="radio"
                name={`${formId}-action`}
                value={entry.action}
                checked={action === entry.action}
                onChange={() => {
                  setAction(entry.action);
                  setAcknowledged(false);
                }}
              />
              <span>{t(entry.label)}</span>
            </label>
          ))}
        </fieldset>
        <Notice
          tone={selected.isRisky ? "warning" : "info"}
          title={t("reconcile.effect")}
          role="status"
        >
          {t(selected.effect)}
        </Notice>
        {action === "attach_upstream_id" ? (
          <div className="field">
            <label htmlFor={`${formId}-upstream`}>{t("reconcile.upstreamJobId")}</label>
            <input
              id={`${formId}-upstream`}
              value={upstreamJobId}
              onChange={(event) => setUpstreamJobId(event.target.value)}
              aria-invalid={!isActionValid}
            />
          </div>
        ) : null}
        {action === "settle_terminal" ? (
          <>
            <div className="field">
              <label htmlFor={`${formId}-state`}>{t("reconcile.upstreamState")}</label>
              <select
                id={`${formId}-state`}
                value={upstreamState}
                onChange={(event) => setUpstreamState(event.target.value as typeof upstreamState)}
              >
                <option value="completed">completed</option>
                <option value="failed">failed</option>
                <option value="cancelled">cancelled</option>
              </select>
            </div>
            <div className="field field-checkbox">
              <input
                id={`${formId}-has-cost`}
                type="checkbox"
                checked={hasReportedCost}
                onChange={(event) => setHasReportedCost(event.target.checked)}
              />
              <label htmlFor={`${formId}-has-cost`}>{t("reconcile.hasCost")}</label>
            </div>
            {hasReportedCost ? (
              <div className="field">
                <label htmlFor={`${formId}-cost`}>
                  {t("reconcile.costAmount", {
                    unit: unit === "micro_usd" ? "micro-USD" : "credits",
                  })}
                </label>
                <input
                  id={`${formId}-cost`}
                  inputMode="numeric"
                  value={costAmount}
                  onChange={(event) => setCostAmount(event.target.value)}
                  aria-invalid={!isCostValid}
                  aria-describedby={`${formId}-cost-preview`}
                />
                <p id={`${formId}-cost-preview`} className="field-hint">
                  {Number.isSafeInteger(costValue)
                    ? format(costValue, unit)
                    : t("reconcile.costHint")}
                </p>
              </div>
            ) : (
              <p className="field-hint">{t("reconcile.noCostEffect")}</p>
            )}
          </>
        ) : null}
        <div className="field">
          <label htmlFor={`${formId}-reason`}>{t("reconcile.reason")}</label>
          <textarea
            id={`${formId}-reason`}
            rows={3}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            aria-invalid={reason.length > 0 && !isReasonValid}
            aria-describedby={`${formId}-reason-hint`}
            maxLength={1024}
          />
          <p id={`${formId}-reason-hint`} className="field-hint">
            {t("reconcile.reasonHint")}
          </p>
        </div>
        <div className="field">
          <label htmlFor={`${formId}-evidence`}>{t("reconcile.evidence")}</label>
          <input
            id={`${formId}-evidence`}
            value={evidenceRef}
            onChange={(event) => setEvidenceRef(event.target.value)}
            maxLength={1024}
          />
        </div>
        {selected.isRisky ? (
          <div className="field field-checkbox">
            <input
              id={`${formId}-ack`}
              type="checkbox"
              checked={isAcknowledged}
              onChange={(event) => setAcknowledged(event.target.checked)}
            />
            <label htmlFor={`${formId}-ack`}>
              {action === "budget_write_off"
                ? t("reconcile.writeOffAck")
                : t("reconcile.overrideAck")}
            </label>
          </div>
        ) : null}
        <InlineError error={reconcile.error} />
        <ConflictRecovery error={reconcile.error} onClose={onClose} />
        <div className="button-row">
          <button type="button" className="button" onClick={onClose} disabled={reconcile.isPending}>
            {t("common.cancel")}
          </button>
          <button
            type="submit"
            className={selected.isRisky ? "button button-danger" : "button button-primary"}
            disabled={!isValid || reconcile.isPending || isConflict(reconcile.error)}
          >
            {reconcile.isPending ? t("common.working") : t("reconcile.submit")}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
