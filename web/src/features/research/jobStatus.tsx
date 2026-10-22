/** Research job state semantics: tones, polling eligibility and retained-hold display. */
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { Badge, Notice } from "../../components/Feedback";
import type { Tone } from "../../components/Feedback";
import { UsageAmountView, useUnitFormatter } from "../../components/Values";

/** A research job. */
export type Job = Schemas["Job"];

/** Closed local job states. */
export type JobState = Schemas["JobState"];

/** All local states in contract order, for filters. */
export const JOB_STATES: readonly JobState[] = [
  "queued",
  "submitting",
  "submission_unknown",
  "accepted",
  "running",
  "cancel_requested",
  "observation_expired",
  "succeeded",
  "failed",
  "cancelled",
];

const ACTIVE_STATES = new Set<JobState>([
  "submission_unknown",
  "queued",
  "submitting",
  "accepted",
  "running",
  "cancel_requested",
]);

/**
 * Whether the browser should keep polling the local API for this job. Unknown submissions may still be discovered;
 * expired observation waits for an operator action.
 *
 * @param state - Local job state.
 * @returns True while the local worker is still progressing the job.
 */
export function isPollingState(state: JobState): boolean {
  return ACTIVE_STATES.has(state);
}

/**
 * Choose a tone. Success is used only for a complete, certain, successful job; uncertain,
 * partial or expired observation states are never green.
 *
 * @param job - Job to classify.
 * @returns Visual tone.
 */
export function jobTone(job: Pick<Job, "state" | "uncertainty" | "is_partial">): Tone {
  switch (job.state) {
    case "succeeded":
      return job.uncertainty || job.is_partial ? "warning" : "success";
    case "failed":
      return "danger";
    case "cancelled":
      return "neutral";
    case "submission_unknown":
    case "observation_expired":
      return "warning";
    default:
      return job.uncertainty ? "warning" : "info";
  }
}

/**
 * Badge describing the job state, with uncertainty and partial qualifiers in text.
 *
 * @param props - Job.
 * @returns Badge group.
 */
export function JobStateBadge({ job }: { job: Pick<Job, "state" | "uncertainty" | "is_partial"> }) {
  const { t } = useI18n();
  return (
    <span className="badge-group">
      <Badge tone={jobTone(job)}>{t(`jobState.${job.state}`)}</Badge>
      {job.is_partial ? <Badge tone="warning">{t("research.partial")}</Badge> : null}
      {job.uncertainty && job.state !== "submission_unknown" ? (
        <Badge tone="warning">{t("research.uncertain")}</Badge>
      ) : null}
    </span>
  );
}

const BUDGET_HOLD_TONE: Record<Schemas["BudgetHoldState"], Tone> = {
  held: "warning",
  settled: "info",
  released_unspent: "neutral",
  written_off: "warning",
};

/**
 * Present the job's budget and its separately tracked slot and budget holds.
 *
 * @param props - Job.
 * @returns Definition list and warnings.
 */
export function HoldsPanel({ job }: { job: Job }) {
  const { t } = useI18n();
  const format = useUnitFormatter();
  return (
    <div className="holds">
      {job.upstream_may_continue ? (
        <Notice tone="warning" role="status" title={t("research.upstreamMayContinue")}>
          {t("research.upstreamMayContinueDetail")}
        </Notice>
      ) : null}
      <dl className="details-grid">
        <dt>{t("research.budget")}</dt>
        <dd>
          {format(job.budget.amount, job.budget.unit)}{" "}
          <Badge tone={job.budget.mode === "hard" ? "info" : "warning"}>
            {job.budget.mode === "hard" ? t("research.budgetHard") : t("research.budgetEstimated")}
          </Badge>
        </dd>
        <dt>{t("research.reservation")}</dt>
        <dd>
          <UsageAmountView usage={job.reservation} />
        </dd>
        <dt>{t("research.activeSlot")}</dt>
        <dd>
          <Badge tone={job.active_slot_held ? "warning" : "neutral"}>
            {job.active_slot_held ? t("research.slotHeld") : t("research.slotReleased")}
          </Badge>
        </dd>
        <dt>{t("research.capacityBasis")}</dt>
        <dd>
          <Badge tone={job.capacity_release_basis === "manual_override" ? "warning" : "neutral"}>
            {t(`capacityBasis.${job.capacity_release_basis}`)}
          </Badge>
        </dd>
        <dt>{t("research.budgetHold")}</dt>
        <dd>
          <Badge tone={BUDGET_HOLD_TONE[job.budget_hold_state]}>
            {t(`budgetHold.${job.budget_hold_state}`)}
          </Badge>
        </dd>
        <dt>{t("research.upstreamContinues")}</dt>
        <dd>
          <Badge tone={job.upstream_may_continue ? "warning" : "neutral"}>
            {job.upstream_may_continue ? t("common.yes") : t("common.no")}
          </Badge>
        </dd>
      </dl>
    </div>
  );
}

/**
 * Explain uncertain, expired, partial or failed outcomes in plain words.
 *
 * @param props - Job.
 * @returns Notices for the job's risk states.
 */
export function JobStateNotices({ job }: { job: Job }) {
  const { t } = useI18n();
  return (
    <>
      {job.state === "submission_unknown" ? (
        <Notice tone="warning" role="status" title={t("research.submissionUnknownTitle")}>
          {t("research.submissionUnknownDetail")}
        </Notice>
      ) : null}
      {job.state === "observation_expired" ? (
        <Notice tone="warning" role="status" title={t("research.observationExpiredTitle")}>
          {t("research.observationExpiredDetail")}
        </Notice>
      ) : null}
      {job.state === "succeeded" && job.is_partial ? (
        <Notice tone="warning" title={t("research.partialTitle")}>
          {t("research.partialDetail", { reason: job.completion_reason ?? t("common.unknown") })}
        </Notice>
      ) : null}
      {job.state === "failed" ? (
        <Notice tone="danger" title={t("research.failedTitle")}>
          {job.error === null
            ? t("research.failedNoDetail")
            : `${job.error.code}: ${job.error.message}`}
        </Notice>
      ) : null}
      {job.uncertainty &&
      job.state !== "submission_unknown" &&
      job.state !== "observation_expired" ? (
        <Notice tone="warning" title={t("research.uncertaintyTitle")}>
          {t("research.uncertaintyDetail")}
        </Notice>
      ) : null}
    </>
  );
}
