/** Versioned cancel, stop-observation and resume-observation actions for one job. */
import { useId, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { ApiFailure, callWithStatus, useApi } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { Dialog } from "../../components/Dialog";
import { ConflictRecovery, isConflict, InlineError, Notice } from "../../components/Feedback";
import { useNotify } from "../../components/Notifications";
import { ReconcileDialog } from "./ReconcileDialog";
import { useJobUpdate } from "./useJobUpdate";
import type { Job } from "./jobStatus";

const TERMINAL_STATES = new Set<Job["state"]>(["succeeded", "failed", "cancelled"]);

type OpenDialog = "upstream" | "observation" | "resume" | "reconcile" | null;

/**
 * Whether a job still has unresolved holds or uncertainty that reconciliation may address.
 *
 * @param job - Job.
 * @returns True when reconciliation is relevant.
 */
export function needsReconciliation(job: Job): boolean {
  return (
    job.state === "submission_unknown" ||
    job.state === "observation_expired" ||
    job.uncertainty ||
    (TERMINAL_STATES.has(job.state) && (job.active_slot_held || job.budget_hold_state === "held"))
  );
}

/**
 * Action bar for a research job. Every request carries the job's current version and
 * CSRF token; stale versions surface the server's conflict instead of retrying.
 *
 * @param props - Current job.
 * @returns Action controls and dialogs.
 */
export function JobActions({ job }: { job: Job }) {
  const { t } = useI18n();
  const { can } = useSession();
  const [open, setOpen] = useState<OpenDialog>(null);
  const isTerminal = TERMINAL_STATES.has(job.state);
  const capability = job.cancel_capability;
  const canCancel = can("admin:research:cancel");
  const canObserve = can("admin:research:observe");
  const canReconcile = can("admin:research:reconcile");
  const showUpstreamCancel = !isTerminal && capability.upstream;
  const showObservationStop =
    !isTerminal && capability.observation && job.state !== "observation_expired";
  const showResume =
    (job.upstream_job_id !== null || job.source_provider === "exa") &&
    (job.state === "observation_expired" || job.state === "submission_unknown");
  const showReconcile = needsReconciliation(job);

  return (
    <div className="job-actions">
      {!isTerminal && !capability.upstream ? (
        <Notice tone="info" title={t("research.actions.remoteCancelUnsupported")}>
          {capability.unavailable_reason ?? t("common.unknown")}
        </Notice>
      ) : null}
      <div className="button-row">
        {showUpstreamCancel ? (
          <button
            type="button"
            className="button button-danger"
            disabled={!canCancel}
            onClick={() => setOpen("upstream")}
          >
            {t("research.actions.cancelUpstream")}
          </button>
        ) : null}
        {showObservationStop ? (
          <button
            type="button"
            className="button"
            disabled={!canCancel}
            onClick={() => setOpen("observation")}
          >
            {t("research.actions.stopObservation")}
          </button>
        ) : null}
        {showResume ? (
          <button
            type="button"
            className="button"
            disabled={!canObserve}
            onClick={() => setOpen("resume")}
          >
            {t("research.actions.resume")}
          </button>
        ) : null}
        {showReconcile ? (
          <button
            type="button"
            className="button"
            disabled={!canReconcile}
            onClick={() => setOpen("reconcile")}
          >
            {t("research.actions.reconcile")}
          </button>
        ) : null}
      </div>
      {(showUpstreamCancel || showObservationStop) && !canCancel ? (
        <p className="permission-hint">
          {t("permission.actionDisabled")} <code>admin:research:cancel</code>
        </p>
      ) : null}
      {showResume && !canObserve ? (
        <p className="permission-hint">
          {t("permission.actionDisabled")} <code>admin:research:observe</code>
        </p>
      ) : null}
      {showReconcile && !canReconcile ? (
        <p className="permission-hint">
          {t("permission.actionDisabled")} <code>admin:research:reconcile</code>
        </p>
      ) : null}
      {open === "upstream" || open === "observation" ? (
        <CancelDialog job={job} mode={open} onClose={() => setOpen(null)} />
      ) : null}
      {open === "resume" ? <ResumeDialog job={job} onClose={() => setOpen(null)} /> : null}
      {open === "reconcile" ? <ReconcileDialog job={job} onClose={() => setOpen(null)} /> : null}
    </div>
  );
}

/**
 * Confirm upstream cancellation or local observation stop.
 *
 * @param props - Job, cancellation mode and close callback.
 * @returns Dialog element.
 */
function CancelDialog({
  job,
  mode,
  onClose,
}: {
  job: Job;
  mode: "upstream" | "observation";
  onClose: () => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const onJob = useJobUpdate();
  const cancel = useMutation({
    mutationFn: () =>
      callWithStatus(
        api.POST("/v1/admin/research/{id}/cancel", {
          params: { path: { id: job.id }, header: { "X-Exta-CSRF": csrfToken } },
          body: { mode, expected_version: job.version },
        }),
      ),
    onSuccess: ({ data, status }) => {
      onJob(data);
      if (mode === "observation" && data.budget_hold_state === "held") {
        notify("warning", t("research.actions.observationStopped"));
      } else {
        notify(
          status === 202 ? "info" : "neutral",
          status === 202
            ? t("research.actions.cancelPending")
            : t("research.actions.cancelRecorded"),
        );
      }
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.status === 409) {
        onJob(job);
      }
    },
  });
  const isObservation = mode === "observation";
  return (
    <Dialog
      title={
        isObservation
          ? t("research.actions.stopObservationTitle")
          : t("research.actions.cancelUpstreamTitle")
      }
      description={
        isObservation
          ? t("research.actions.stopObservationDetail")
          : t("research.actions.cancelUpstreamDetail")
      }
      onClose={onClose}
      isBusy={cancel.isPending}
    >
      {isObservation && job.state !== "queued" ? (
        <Notice tone="warning" title={t("research.actions.holdsRetained")}>
          {t("research.upstreamMayContinueDetail")}
        </Notice>
      ) : null}
      <InlineError error={cancel.error} />
      <ConflictRecovery error={cancel.error} onClose={onClose} />
      <div className="button-row">
        <button
          type="button"
          className="button"
          onClick={onClose}
          disabled={cancel.isPending}
          data-autofocus
        >
          {t("common.cancel")}
        </button>
        <button
          type="button"
          className={isObservation ? "button button-primary" : "button button-danger"}
          onClick={() => {
            if (!isConflict(cancel.error)) cancel.mutate();
          }}
          disabled={cancel.isPending || isConflict(cancel.error)}
        >
          {cancel.isPending
            ? t("common.working")
            : isObservation
              ? t("research.actions.stopObservation")
              : t("research.actions.cancelUpstream")}
        </button>
      </div>
    </Dialog>
  );
}

/**
 * Resume local observation for a job with a known upstream ID.
 *
 * @param props - Job and close callback.
 * @returns Dialog element.
 */
function ResumeDialog({ job, onClose }: { job: Job; onClose: () => void }) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const notify = useNotify();
  const onJob = useJobUpdate();
  const fieldId = useId();
  const [seconds, setSeconds] = useState("3600");
  const parsed = Number(seconds);
  const isValid = /^\d+$/.test(seconds) && parsed >= 60 && parsed <= 3600;
  const resume = useMutation({
    mutationFn: () =>
      callWithStatus(
        api.POST("/v1/admin/research/{id}/resume-observation", {
          params: { path: { id: job.id }, header: { "X-Exta-CSRF": csrfToken } },
          body: { expected_version: job.version, observation_seconds: parsed },
        }),
      ),
    onSuccess: ({ data }) => {
      onJob(data);
      notify("info", t("research.actions.resumed"));
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
      title={t("research.actions.resumeTitle")}
      description={t("research.actions.resumeDetail")}
      onClose={onClose}
      isBusy={resume.isPending}
    >
      <form
        className="form"
        onSubmit={(event) => {
          event.preventDefault();
          if (isValid && !resume.isPending && !isConflict(resume.error)) {
            resume.mutate();
          }
        }}
      >
        <div className="field">
          <label htmlFor={fieldId}>{t("research.form.observation")}</label>
          <input
            id={fieldId}
            inputMode="numeric"
            value={seconds}
            onChange={(event) => setSeconds(event.target.value)}
            aria-invalid={!isValid}
            aria-describedby={`${fieldId}-hint`}
            data-autofocus
          />
          <p id={`${fieldId}-hint`} className="field-hint">
            {t("research.form.observationHint")}
          </p>
        </div>
        <InlineError error={resume.error} />
        <ConflictRecovery error={resume.error} onClose={onClose} />
        <div className="button-row">
          <button type="button" className="button" onClick={onClose} disabled={resume.isPending}>
            {t("common.cancel")}
          </button>
          <button
            type="submit"
            className="button button-primary"
            disabled={!isValid || resume.isPending || isConflict(resume.error)}
          >
            {resume.isPending ? t("common.working") : t("research.actions.resume")}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
