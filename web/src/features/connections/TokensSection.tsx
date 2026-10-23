import { DataTable } from "../../components/DataTable";
import { CheckboxField, Button, Input } from "../../components/Controls";
/**
 * Scoped client tokens. A newly minted value is shown exactly once from component state;
 * it is removed when dismissed and never reaches the query cache or browser storage.
 */
import { useEffect, useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ApiFailure, call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { useCursorList } from "../../lib/useCursorList";
import { Dialog } from "../../components/Dialog";
import {
  ConflictRecovery,
  isConflict,
  Badge,
  EmptyState,
  ErrorState,
  InlineError,
  LoadingState,
  Notice,
} from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { useNotify } from "../../components/Notifications";
import { PermissionHint, Section } from "../../components/Page";
import { CopyButton, DateTime } from "../../components/Values";

type ClientScope = Schemas["ClientScope"];
type ClientToken = Schemas["ClientToken"];

const SCOPES: readonly ClientScope[] = [
  "read",
  "execute",
  "research:create",
  "research:read",
  "research:cancel",
  "research:observe",
];

/** Show readable scope names while retaining exact protocol codes as secondary text. */
function ScopeBadges({ scopes }: { scopes: readonly ClientScope[] }) {
  const { t } = useI18n();
  return (
    <span className="badge-group">
      {scopes.map((scope) => (
        <span key={scope} title={scope} aria-label={`${t(`scope.${scope}`)} (${scope})`}>
          <Badge tone="neutral">{t(`scope.${scope}`)}</Badge>
        </span>
      ))}
    </span>
  );
}

/**
 * Format a Date as a `datetime-local` value in local time.
 *
 * @param date - Date.
 * @returns Input value.
 */
function toLocalInput(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/**
 * Token list, mint dialog and one-time reveal.
 *
 * @returns Section element.
 */
export function TokensSection() {
  const { t } = useI18n();
  const api = useApi();
  const { can } = useSession();
  const [isMintOpen, setMintOpen] = useState(false);
  const [revealed, setRevealed] = useState<{ label: string; value: string } | null>(null);
  const [revoking, setRevoking] = useState<ClientToken | null>(null);
  const canWrite = can("admin:tokens:write");
  const { query, items } = useCursorList(["tokens"], (cursor) =>
    call(
      api.GET("/v1/admin/tokens", { params: { query: cursor === undefined ? {} : { cursor } } }),
    ),
  );
  const [now, setNow] = useState(() => Date.now());
  const nextExpiry = Math.min(
    ...items
      .filter((token) => token.revoked_at === null && Date.parse(token.expires_at) > now)
      .map((token) => Date.parse(token.expires_at)),
  );
  useEffect(() => {
    const refresh = () => setNow(Date.now());
    const timer = Number.isFinite(nextExpiry)
      ? window.setTimeout(refresh, Math.min(2147483647, Math.max(1, nextExpiry - Date.now())))
      : undefined;
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [nextExpiry, now]);
  const activeCount = items.filter(
    (token) => token.revoked_at === null && Date.parse(token.expires_at) > now,
  ).length;

  return (
    <Section
      title={t("tokens.title")}
      description={t("tokens.description")}
      actions={
        <Button
          type="button"
          className="button button-primary"
          disabled={!canWrite}
          onClick={() => setMintOpen(true)}
        >
          {t("tokens.mint")}
        </Button>
      }
    >
      <PermissionHint permission="admin:tokens:write" />
      {revealed === null ? null : (
        <div className="token-reveal" role="region" aria-label={t("tokens.revealRegion")}>
          <Notice tone="warning" title={t("tokens.revealTitle", { label: revealed.label })}>
            <p>{t("tokens.revealDetail")}</p>
            <pre className="code-block secret-value">{revealed.value}</pre>
            <div className="button-row">
              <CopyButton text={revealed.value} label={t("tokens.copyToken")} primary />
              <Button type="button" className="button" onClick={() => setRevealed(null)}>
                {t("tokens.dismiss")}
              </Button>
            </div>
          </Notice>
        </div>
      )}
      {query.isPending ? (
        <LoadingState />
      ) : query.isError && items.length === 0 ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState title={t("tokens.emptyTitle")} description={t("tokens.emptyDescription")} />
      ) : (
        <>
          {activeCount === 0 ? <Notice tone="info">{t("tokens.noneActive")}</Notice> : null}
          <div className="table-wrap" role="region" aria-label={t("common.table")} tabIndex={0}>
            <DataTable
              mobileRow={(token) => (
                <>
                  <div className="mobile-record-heading">
                    <strong>{token.label}</strong>
                    <Badge
                      tone={
                        token.revoked_at
                          ? "neutral"
                          : Date.parse(token.expires_at) <= now
                            ? "warning"
                            : "success"
                      }
                    >
                      {token.revoked_at
                        ? t("tokens.revoked")
                        : Date.parse(token.expires_at) <= now
                          ? t("tokens.expired")
                          : t("tokens.active")}
                    </Badge>
                  </div>
                  <p className="cell-secondary">
                    {t("tokens.expires")}: <DateTime value={token.expires_at} />
                  </p>
                  <p className="scope-summary">
                    <ScopeBadges scopes={token.scopes} />
                  </p>
                  {token.revoked_at ? null : (
                    <Button
                      className="button button-danger button-ghost"
                      disabled={!canWrite}
                      onClick={() => setRevoking(token)}
                    >
                      {t("tokens.revoke")}
                    </Button>
                  )}
                </>
              )}
              dataSource={items}
              rowKey={(token) => token.id}
              columns={[
                {
                  key: "column-0",
                  title: <>{t("common.label")}</>,
                  render: (_value, token) => {
                    return (
                      <>
                        <span title={`${token.id} · ${token.principal_id}`}>{token.label}</span>
                      </>
                    );
                  },
                },
                {
                  key: "column-1",
                  title: <>{t("tokens.scopes")}</>,
                  render: (_value, token) => {
                    return (
                      <>
                        <ScopeBadges scopes={token.scopes} />
                      </>
                    );
                  },
                },
                {
                  key: "column-3",
                  title: <>{t("tokens.expires")}</>,
                  render: (_value, token) => {
                    return (
                      <>
                        <DateTime value={token.expires_at} />
                      </>
                    );
                  },
                },
                {
                  key: "column-4",
                  title: <>{t("common.state")}</>,
                  render: (_value, token) => {
                    const isRevoked = token.revoked_at !== null;
                    const isExpired = Date.parse(token.expires_at) <= now;
                    return (
                      <>
                        <Badge tone={isRevoked ? "neutral" : isExpired ? "warning" : "info"}>
                          {isRevoked
                            ? t("tokens.revoked")
                            : isExpired
                              ? t("tokens.expired")
                              : t("tokens.active")}
                        </Badge>
                      </>
                    );
                  },
                },
                {
                  key: "column-5",
                  title: <>{t("common.actions")}</>,
                  render: (_value, token) => {
                    const isRevoked = token.revoked_at !== null;
                    return (
                      <>
                        {isRevoked ? (
                          "—"
                        ) : (
                          <Button
                            type="button"
                            className="button button-small button-danger button-ghost"
                            disabled={!canWrite || isRevoked}
                            onClick={() => setRevoking(token)}
                          >
                            {t("tokens.revoke")}
                          </Button>
                        )}
                      </>
                    );
                  },
                },
              ]}
            />
          </div>
        </>
      )}
      <LoadMore query={query} />
      {isMintOpen ? (
        <MintDialog
          onClose={() => setMintOpen(false)}
          onMinted={(label, value) => {
            setMintOpen(false);
            setRevealed({ label, value });
          }}
        />
      ) : null}
      {revoking === null ? null : (
        <RevokeDialog token={revoking} onClose={() => setRevoking(null)} />
      )}
    </Section>
  );
}

/**
 * Mint a scoped token for the current stable principal.
 *
 * @param props - Close and minted callbacks.
 * @returns Dialog element.
 */
function MintDialog({
  onClose,
  onMinted,
}: {
  onClose: () => void;
  onMinted: (label: string, value: string) => void;
}) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const queryClient = useQueryClient();
  const formId = useId();
  const [label, setLabel] = useState("");
  const [hasSubmitted, setSubmitted] = useState(false);
  const [scopes, setScopes] = useState<ClientScope[]>(["read"]);
  const [openedAt] = useState(() => Date.now());
  const [expiresAt, setExpiresAt] = useState(() =>
    toLocalInput(new Date(openedAt + 30 * 24 * 3600 * 1000)),
  );
  const expiresDate = new Date(expiresAt);
  const isExpiryValid = !Number.isNaN(expiresDate.getTime()) && expiresDate.getTime() > openedAt;
  const isValid =
    label.trim().length >= 1 && label.length <= 128 && scopes.length >= 1 && isExpiryValid;
  const mint = useMutation({
    gcTime: 0,
    mutationFn: () =>
      call(
        api.POST("/v1/admin/tokens", {
          params: { header: { "X-Exta-CSRF": csrfToken } },
          body: { label: label.trim(), scopes, expires_at: expiresDate.toISOString() },
        }),
      ),
    onSuccess: (created) => {
      void queryClient.invalidateQueries({ queryKey: ["tokens"] });
      onMinted(created.token.label, created.token_value);
    },
  });
  return (
    <Dialog
      title={t("tokens.mint")}
      description={t("tokens.mintDetail")}
      onClose={onClose}
      isBusy={mint.isPending}
    >
      <form
        className="form"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          setSubmitted(true);
          if (isValid && !mint.isPending) {
            mint.mutate();
          } else {
            const field =
              label.trim() === "" ? "label" : scopes.length === 0 ? "scope-0" : "expires";
            const target = document.getElementById(`${formId}-${field}`);
            (target?.querySelector("input") ?? target)?.focus();
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
            required
            aria-invalid={hasSubmitted && label.trim() === ""}
            aria-describedby={
              hasSubmitted && label.trim() === "" ? `${formId}-label-error` : undefined
            }
            data-autofocus
          />
          {hasSubmitted && label.trim() === "" ? (
            <p className="field-error" id={`${formId}-label-error`}>
              {t("tokens.labelRequired")}
            </p>
          ) : null}
        </div>
        <fieldset className="checkbox-group fieldset scope-fields">
          <legend>{t("tokens.scopes")}</legend>
          {SCOPES.map((scope, index) => (
            <CheckboxField
              key={scope}
              label={
                <span>
                  {t(`scope.${scope}`)} <code className="cell-secondary">{scope}</code>
                </span>
              }
              id={`${formId}-scope-${index}`}
              aria-describedby={
                hasSubmitted && scopes.length === 0 ? `${formId}-scope-error` : undefined
              }
              checked={scopes.includes(scope)}
              onChange={(event) =>
                setScopes((current) =>
                  event.target.checked
                    ? [...current, scope]
                    : current.filter((entry) => entry !== scope),
                )
              }
            />
          ))}
          {hasSubmitted && scopes.length === 0 ? (
            <p className="field-error" id={`${formId}-scope-error`}>
              {t("tokens.scopeRequired")}
            </p>
          ) : null}
        </fieldset>
        <div className="field">
          <label id={`${formId}-expires` + "-label"} htmlFor={`${formId}-expires`}>
            {t("tokens.expires")}
          </label>
          <Input
            id={`${formId}-expires`}
            type="datetime-local"
            value={expiresAt}
            onChange={(event) => setExpiresAt(event.target.value)}
            aria-invalid={!isExpiryValid}
            aria-describedby={!isExpiryValid ? `${formId}-expiry-error` : undefined}
          />
          {!isExpiryValid ? (
            <p className="field-error" id={`${formId}-expiry-error`}>
              {t("tokens.expiryInvalid")}
            </p>
          ) : null}
        </div>
        <p className="field-hint">{t("tokens.noAdminScopes")}</p>
        <InlineError error={mint.error} />
        <div className="button-row">
          <Button type="button" className="button" onClick={onClose} disabled={mint.isPending}>
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            className="button button-primary"
            disabled={mint.isPending}
            loading={mint.isPending}
          >
            {mint.isPending ? t("common.working") : t("tokens.mint")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/**
 * Confirm revocation of a token at its current version.
 *
 * @param props - Token and close callback.
 * @returns Dialog element.
 */
function RevokeDialog({ token, onClose }: { token: ClientToken; onClose: () => void }) {
  const { t } = useI18n();
  const api = useApi();
  const { csrfToken } = useSession();
  const queryClient = useQueryClient();
  const notify = useNotify();
  const revoke = useMutation({
    mutationFn: () =>
      call(
        api.POST("/v1/admin/tokens/{id}/revoke", {
          params: { path: { id: token.id }, header: { "X-Exta-CSRF": csrfToken } },
          body: { expected_version: token.version },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["tokens"] });
      notify("info", t("tokens.revokedNotice", { label: token.label }));
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.status === 409) {
        void queryClient.invalidateQueries({ queryKey: ["tokens"] });
      }
    },
  });
  return (
    <Dialog
      title={t("tokens.revokeTitle", { label: token.label })}
      description={t("tokens.revokeDetail")}
      onClose={onClose}
      isBusy={revoke.isPending}
    >
      <InlineError error={revoke.error} />
      <ConflictRecovery error={revoke.error} onClose={onClose} />
      <div className="button-row">
        <Button
          type="button"
          className="button"
          onClick={onClose}
          disabled={revoke.isPending}
          data-autofocus
        >
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          className="button button-danger"
          onClick={() => {
            if (!isConflict(revoke.error)) revoke.mutate();
          }}
          disabled={revoke.isPending || isConflict(revoke.error)}
        >
          {revoke.isPending ? t("common.working") : t("tokens.revoke")}
        </Button>
      </div>
    </Dialog>
  );
}
