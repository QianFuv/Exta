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
        <button
          type="button"
          className="button button-primary"
          disabled={!canWrite}
          onClick={() => setMintOpen(true)}
        >
          {t("tokens.mint")}
        </button>
      }
    >
      <PermissionHint permission="admin:tokens:write" />
      {revealed === null ? null : (
        <div className="token-reveal" role="region" aria-label={t("tokens.revealRegion")}>
          <Notice tone="warning" title={t("tokens.revealTitle", { label: revealed.label })}>
            <p>{t("tokens.revealDetail")}</p>
            <pre className="code-block secret-value">{revealed.value}</pre>
            <div className="button-row">
              <CopyButton text={revealed.value} label={t("tokens.copyToken")} />
              <button type="button" className="button" onClick={() => setRevealed(null)}>
                {t("tokens.dismiss")}
              </button>
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
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">{t("common.label")}</th>
                  <th scope="col">{t("tokens.scopes")}</th>
                  <th scope="col">{t("tokens.owner")}</th>
                  <th scope="col">{t("tokens.expires")}</th>
                  <th scope="col">{t("common.state")}</th>
                  <th scope="col">{t("common.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((token) => {
                  const isRevoked = token.revoked_at !== null;
                  const isExpired = Date.parse(token.expires_at) <= now;
                  return (
                    <tr key={token.id}>
                      <td>
                        {token.label}
                        <br />
                        <code className="muted">{token.id}</code>
                      </td>
                      <td>
                        <span className="badge-group">
                          {token.scopes.map((scope) => (
                            <Badge key={scope} tone="neutral">
                              {scope}
                            </Badge>
                          ))}
                        </span>
                      </td>
                      <td>
                        <code>{token.principal_id}</code>
                      </td>
                      <td>
                        <DateTime value={token.expires_at} />
                      </td>
                      <td>
                        <Badge tone={isRevoked ? "neutral" : isExpired ? "warning" : "info"}>
                          {isRevoked
                            ? t("tokens.revoked")
                            : isExpired
                              ? t("tokens.expired")
                              : t("tokens.active")}
                        </Badge>
                      </td>
                      <td>
                        <button
                          type="button"
                          className="button button-small button-danger"
                          disabled={!canWrite || isRevoked}
                          onClick={() => setRevoking(token)}
                        >
                          {t("tokens.revoke")}
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
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
        onSubmit={(event) => {
          event.preventDefault();
          if (isValid && !mint.isPending) {
            mint.mutate();
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
        <fieldset className="checkbox-group">
          <legend>{t("tokens.scopes")}</legend>
          {SCOPES.map((scope) => (
            <label key={scope} className="checkbox">
              <input
                type="checkbox"
                checked={scopes.includes(scope)}
                onChange={(event) =>
                  setScopes((current) =>
                    event.target.checked
                      ? [...current, scope]
                      : current.filter((entry) => entry !== scope),
                  )
                }
              />
              <code>{scope}</code>
            </label>
          ))}
        </fieldset>
        <div className="field">
          <label htmlFor={`${formId}-expires`}>{t("tokens.expires")}</label>
          <input
            id={`${formId}-expires`}
            type="datetime-local"
            value={expiresAt}
            onChange={(event) => setExpiresAt(event.target.value)}
            aria-invalid={!isExpiryValid}
          />
        </div>
        <p className="field-hint">{t("tokens.noAdminScopes")}</p>
        <InlineError error={mint.error} />
        <div className="button-row">
          <button type="button" className="button" onClick={onClose} disabled={mint.isPending}>
            {t("common.cancel")}
          </button>
          <button
            type="submit"
            className="button button-primary"
            disabled={!isValid || mint.isPending}
          >
            {mint.isPending ? t("common.working") : t("tokens.mint")}
          </button>
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
        <button
          type="button"
          className="button"
          onClick={onClose}
          disabled={revoke.isPending}
          data-autofocus
        >
          {t("common.cancel")}
        </button>
        <button
          type="button"
          className="button button-danger"
          onClick={() => {
            if (!isConflict(revoke.error)) revoke.mutate();
          }}
          disabled={revoke.isPending || isConflict(revoke.error)}
        >
          {revoke.isPending ? t("common.working") : t("tokens.revoke")}
        </button>
      </div>
    </Dialog>
  );
}
