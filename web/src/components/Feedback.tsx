import Tag from "@douyinfe/semi-ui/lib/es/tag";
import Banner from "@douyinfe/semi-ui/lib/es/banner";
import Spin from "@douyinfe/semi-ui/lib/es/spin";
import { Button } from "./Controls";
/** Shared loading, error, empty, locked and status presentation. */
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ApiFailure } from "../lib/api";
import { useI18n } from "../lib/i18n";
import type { MessageKey } from "../lib/messages";

/** Visual tone of a badge or notice. Success is reserved for verified, certain outcomes. */
export type Tone = "success" | "warning" | "danger" | "info" | "neutral";

/**
 * Compact status label. The text always carries the meaning; color is supplementary.
 *
 * @param props - Tone and label.
 * @returns Badge element.
 */
export function Badge({ tone, children }: { tone: Tone; children: ReactNode }) {
  return (
    <Tag
      color={
        tone === "danger"
          ? "red"
          : tone === "warning"
            ? "orange"
            : tone === "success"
              ? "green"
              : tone === "info"
                ? "blue"
                : "grey"
      }
      className={`status-${tone}`}
      shape="square"
    >
      {children}
    </Tag>
  );
}

/**
 * Inline notice block for warnings and explanations.
 *
 * @param props - Tone, optional title and content.
 * @returns Notice element.
 */
export function Notice({
  tone,
  title,
  children,
  role,
}: {
  tone: Tone;
  title?: string;
  children?: ReactNode;
  role?: "alert" | "status";
}) {
  return (
    <div role={role} className="exta-notice">
      <Banner
        fullMode={false}
        type={tone === "neutral" ? "info" : tone}
        title={title}
        description={children}
        closeIcon={null}
      />
    </div>
  );
}

/**
 * Busy indicator announced to assistive technology.
 *
 * @param props - Optional label override.
 * @returns Loading element.
 */
export function LoadingState({ label }: { label?: string }) {
  const { t } = useI18n();
  return (
    <div className="state state-loading" role="status" aria-live="polite">
      <Spin size="small" />
      <span>{label ?? t("common.loading")}</span>
    </div>
  );
}

/**
 * Empty-collection explanation with optional action.
 *
 * @param props - Title, description and optional action.
 * @returns Empty-state element.
 */
export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="state state-empty">
      <p className="state-title">{title}</p>
      {description === undefined ? null : <p className="state-description">{description}</p>}
      {action ?? null}
    </div>
  );
}

/**
 * Explain that the stable principal lacks a permission. No request is made.
 *
 * @param props - The missing permission name.
 * @returns Locked-state element.
 */
export function LockedState({ permission }: { permission: string }) {
  const { t } = useI18n();
  return (
    <div className="state state-locked" role="status">
      <p className="state-title">{t("permission.lockedTitle")}</p>
      <p className="state-description">
        {t("permission.lockedDescription")} <code>{permission}</code>
      </p>
    </div>
  );
}

/**
 * Translate an error into an operator-facing headline.
 *
 * @param error - Any thrown value.
 * @param t - Translation function.
 * @returns Localized headline.
 */
export function describeError(
  error: unknown,
  t: (key: MessageKey, values?: Record<string, string | number>) => string,
): string {
  if (error instanceof ApiFailure) {
    if (error.kind === "network") {
      return t("error.network");
    }
    if (error.kind === "malformed" || error.code === null) {
      return t("error.malformed", { status: error.status });
    }
    return t(`error.${error.code}`);
  }
  return t("error.unexpected");
}

/**
 * Full error panel with code, request ID, retry hint and optional retry action.
 *
 * @param props - Error and optional retry callback.
 * @returns Error element.
 */
export function ErrorState({
  error,
  onRetry,
  title,
}: {
  error: unknown;
  onRetry?: () => void;
  title?: string;
}) {
  const { t } = useI18n();
  const failure = error instanceof ApiFailure ? error : null;
  const detail = failure?.detail ?? null;
  return (
    <div className="state state-error" role="alert">
      <p className="state-title">{title ?? describeError(error, t)}</p>
      {title === undefined ? null : <p className="state-description">{describeError(error, t)}</p>}
      {detail === null || detail.code === "invalid_token" ? null : (
        <dl className="error-meta">
          <dt>{t("error.code")}</dt>
          <dd>
            <code>{detail.code}</code>
          </dd>
          <dt>{t("error.message")}</dt>
          <dd>{detail.message}</dd>
          <dt>{t("error.requestId")}</dt>
          <dd>
            <code>{detail.request_id}</code>
          </dd>
          {detail.retry_after_ms === null ? null : (
            <>
              <dt>{t("error.retryAfter")}</dt>
              <dd>{Math.ceil(detail.retry_after_ms / 1000)} s</dd>
            </>
          )}
        </dl>
      )}
      {onRetry === undefined ? null : (
        <Button type="button" className="button" onClick={onRetry}>
          {t("common.retry")}
        </Button>
      )}
    </div>
  );
}

/**
 * Compact error line for forms and dialogs.
 *
 * @param props - Error value.
 * @returns Alert paragraph or nothing.
 */
export function InlineError({ error }: { error: unknown }) {
  const { t } = useI18n();
  if (error === null || error === undefined) {
    return null;
  }
  const detail = error instanceof ApiFailure ? error.detail : null;
  return (
    <div className="exta-notice">
      <Banner
        fullMode={false}
        type="danger"
        closeIcon={null}
        description={
          <>
            {describeError(error, t)}
            {detail === null ||
            detail.code === "invalid_token" ||
            detail.code === "revision_conflict" ? null : (
              <>
                {" "}
                <span className="muted">
                  ({detail.code}
                  {detail.retry_after_ms === null
                    ? ""
                    : `, ${Math.ceil(detail.retry_after_ms / 1000)} s`}
                  )
                </span>
              </>
            )}
          </>
        }
      />
    </div>
  );
}

/**
 * Show a page section's content according to query state.
 *
 * @param props - Query state and children renderer.
 * @returns The appropriate state view.
 */
export function QueryView<T>({
  query,
  children,
  loadingLabel,
}: {
  query: {
    isPending: boolean;
    isError: boolean;
    error: unknown;
    data: T | undefined;
    refetch: () => unknown;
  };
  children: (data: T) => ReactNode;
  loadingLabel?: string;
}) {
  if (query.isPending) {
    return loadingLabel === undefined ? <LoadingState /> : <LoadingState label={loadingLabel} />;
  }
  if (query.isError || query.data === undefined) {
    return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  }
  return <>{children(query.data)}</>;
}

/** Identify a stale version without retrying the mutation automatically. */
export function isConflict(error: unknown): boolean {
  return error instanceof ApiFailure && error.status === 409;
}

/** Refresh current read models, then close the stale editor for deliberate review. */
export function ConflictRecovery({ error, onClose }: { error: unknown; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [isLoading, setLoading] = useState(false);
  const [reloadError, setReloadError] = useState<unknown>(null);
  if (!isConflict(error)) return null;
  return (
    <>
      <InlineError error={reloadError} />
      <Button
        type="button"
        className="button"
        disabled={isLoading}
        onClick={async () => {
          setLoading(true);
          try {
            await queryClient.invalidateQueries({}, { throwOnError: true });
            onClose();
          } catch (failure) {
            setReloadError(failure);
          } finally {
            setLoading(false);
          }
        }}
      >
        {t("common.reviewLatest")}
      </Button>
    </>
  );
}
