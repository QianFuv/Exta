/** Provider account and key pools, separated by provider, with versioned management actions. */
import { useState } from "react";
import { useSearchParams } from "react-router";
import { SectionNav } from "../../components/SectionNav";
import { TokensSection } from "../connections/TokensSection";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { useCursorList } from "../../lib/useCursorList";
import {
  Badge,
  EmptyState,
  ErrorState,
  InlineError,
  LoadingState,
  Notice,
} from "../../components/Feedback";
import type { Tone } from "../../components/Feedback";
import { LoadMore } from "../../components/LoadMore";
import { PageHeader, PermissionHint, RequirePermission, Section } from "../../components/Page";
import { DateTime, useUnitFormatter } from "../../components/Values";
import {
  AccountDialog,
  KeyCreateDialog,
  KeyRetireDialog,
  KeyRotateDialog,
  useKeyPatch,
} from "./KeyDialogs";

type Provider = Schemas["Provider"];
type Account = Schemas["Account"];
type ProviderKey = Schemas["ProviderKey"];

type OpenDialog =
  | { kind: "account-create" }
  | { kind: "account-edit"; account: Account }
  | { kind: "key-create"; accountId: string }
  | { kind: "key-rotate"; key: ProviderKey }
  | { kind: "key-retire"; key: ProviderKey }
  | null;

const VALIDITY_TONE: Record<ProviderKey["validity"], Tone> = {
  valid: "success",
  unknown: "warning",
  invalid: "danger",
};
const BREAKER_TONE: Record<ProviderKey["breaker"], Tone> = {
  closed: "neutral",
  half_open: "warning",
  open: "danger",
};
const BILLING_TONE: Record<Account["billing_state"], Tone> = {
  eligible: "neutral",
  unknown: "warning",
  blocked: "danger",
};

/**
 * Key pools page.
 *
 * @returns Page element.
 */
export function KeysPage() {
  const { t } = useI18n();
  const [params] = useSearchParams();
  return (
    <>
      <PageHeader title={t("keys.title")} description={t("keys.description")} />
      <SectionNav
        label={t("keys.title")}
        items={[
          { value: "upstream", label: t("keys.upstream") },
          { value: "access", label: t("keys.access") },
        ]}
      />
      <RequirePermission permission="admin:read">
        {params.get("view") === "access" ? (
          <TokensSection />
        ) : (
          <>
            <ProviderPool provider="exa" />
            <ProviderPool provider="tavily" />
          </>
        )}
      </RequirePermission>
    </>
  );
}

/**
 * One provider's accounts and keys, grouped by account.
 *
 * @param props - Provider.
 * @returns Section element.
 */
function ProviderPool({ provider }: { provider: Provider }) {
  const { t } = useI18n();
  const api = useApi();
  const { can } = useSession();
  const [dialog, setDialog] = useState<OpenDialog>(null);
  const [filter, setFilter] = useState("");
  const accounts = useCursorList(["accounts", provider], (cursor) =>
    call(
      api.GET("/v1/admin/accounts", {
        params: { query: { provider, limit: 100, ...(cursor === undefined ? {} : { cursor }) } },
      }),
    ),
  );
  const keys = useCursorList(["keys", provider], (cursor) =>
    call(
      api.GET("/v1/admin/keys", {
        params: { query: { provider, limit: 100, ...(cursor === undefined ? {} : { cursor }) } },
      }),
    ),
  );

  const canWriteAccounts = can("admin:accounts:write");
  const canWriteKeys = can("admin:keys:write");
  const accountIds = new Set(accounts.items.map((account) => account.id));
  const filteredAccounts = accounts.items.filter((account) =>
    `${account.label} ${account.id}`.toLowerCase().includes(filter.trim().toLowerCase()),
  );
  const orphanKeys = keys.items.filter((key) => !accountIds.has(key.account_id));
  const isLoading = accounts.query.isPending || keys.query.isPending;
  const failed = accounts.query.isError ? accounts.query : keys.query.isError ? keys.query : null;

  return (
    <Section
      title={t(`provider.${provider}`)}
      description={t(`keys.providerDescription.${provider}`)}
      actions={
        <button
          type="button"
          className="button button-primary"
          disabled={!canWriteAccounts}
          onClick={() => setDialog({ kind: "account-create" })}
        >
          {t("keys.addAccount")}
        </button>
      }
    >
      <PermissionHint permission="admin:keys:write" />
      <div className="table-toolbar">
        <label className="search-field">
          <span className="visually-hidden">
            {t("keys.find")} · {t(`provider.${provider}`)}
          </span>
          <input
            type="search"
            value={filter}
            placeholder={t("keys.findHint")}
            onChange={(event) => setFilter(event.target.value)}
          />
        </label>
        <span className="muted">{t("keys.accountCount", { count: accounts.items.length })}</span>
      </div>
      {isLoading ? (
        <LoadingState />
      ) : failed !== null ? (
        <ErrorState error={failed.error} onRetry={() => void failed.refetch()} />
      ) : accounts.items.length === 0 && keys.items.length === 0 ? (
        <EmptyState
          title={t("keys.emptyTitle", { provider: t(`provider.${provider}`) })}
          description={t("keys.emptyDescription")}
        />
      ) : (
        <div className="account-list">
          {filteredAccounts.length === 0 ? <EmptyState title={t("keys.noMatches")} /> : null}
          {filteredAccounts.map((account) => (
            <AccountCard
              key={account.id}
              account={account}
              keys={keys.items.filter((key) => key.account_id === account.id)}
              canWriteAccounts={canWriteAccounts}
              canWriteKeys={canWriteKeys}
              onDialog={setDialog}
            />
          ))}
          {orphanKeys.length > 0 ? (
            <article className="card">
              <header className="card-header">
                <h3>{t("keys.unknownGrouping")}</h3>
              </header>
              <Notice tone="warning">{t("keys.unknownGroupingDetail")}</Notice>
              <KeyTable keys={orphanKeys} canWriteKeys={canWriteKeys} onDialog={setDialog} />
            </article>
          ) : null}
        </div>
      )}
      <LoadMore query={accounts.query} />
      <LoadMore query={keys.query} />
      {dialog?.kind === "account-create" ? (
        <AccountDialog provider={provider} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.kind === "account-edit" ? (
        <AccountDialog
          provider={provider}
          account={dialog.account}
          onClose={() => setDialog(null)}
        />
      ) : null}
      {dialog?.kind === "key-create" ? (
        <KeyCreateDialog accountId={dialog.accountId} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.kind === "key-rotate" ? (
        <KeyRotateDialog providerKey={dialog.key} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.kind === "key-retire" ? (
        <KeyRetireDialog providerKey={dialog.key} onClose={() => setDialog(null)} />
      ) : null}
    </Section>
  );
}

/**
 * Account summary with its keys.
 *
 * @param props - Account, its keys, permissions and dialog opener.
 * @returns Card element.
 */
function AccountCard({
  account,
  keys,
  canWriteAccounts,
  canWriteKeys,
  onDialog,
}: {
  account: Account;
  keys: ProviderKey[];
  canWriteAccounts: boolean;
  canWriteKeys: boolean;
  onDialog: (dialog: OpenDialog) => void;
}) {
  const { t, formatNumber } = useI18n();
  const format = useUnitFormatter();
  return (
    <article className="card account-card">
      <header className="card-header">
        <div>
          <h3>{account.label}</h3>
          <p className="muted">
            <code>{account.id}</code>
            {account.external_ref === null || account.external_ref === "" ? null : (
              <> · {account.external_ref}</>
            )}
          </p>
        </div>
        <div className="badge-group">
          <Badge tone={account.enabled ? "neutral" : "warning"}>
            {account.enabled ? t("common.enabled") : t("common.disabled")}
          </Badge>
          <Badge tone={BILLING_TONE[account.billing_state]}>
            {t(`billingState.${account.billing_state}`)}
          </Badge>
          <Badge tone={account.verification_state === "verified" ? "info" : "warning"}>
            {t(`verificationState.${account.verification_state}`)}
          </Badge>
        </div>
        <div className="button-row account-actions">
          <button
            type="button"
            className="button button-small"
            disabled={!canWriteAccounts}
            onClick={() => onDialog({ kind: "account-edit", account })}
          >
            {t("keys.editAccount")}
          </button>
          <button
            type="button"
            className="button button-small"
            disabled={!canWriteKeys}
            onClick={() => onDialog({ kind: "key-create", accountId: account.id })}
          >
            {t("keys.addKey")}
          </button>
        </div>
      </header>
      {account.billing_state === "blocked" ? (
        <Notice tone="danger" title={t("keys.accountExhausted")}>
          {t("keys.accountExhaustedDetail")}
        </Notice>
      ) : null}
      <dl className="account-metrics">
        <dt>{t("keys.allocation")}</dt>
        <dd>{format(account.quota_policy.allocation, account.quota_policy.unit)}</dd>
        <dt>{t("keys.perOperation")}</dt>
        <dd>{format(account.quota_policy.per_operation_reservation, account.quota_policy.unit)}</dd>
        <dt>{t("keys.activeJobs")}</dt>
        <dd>{formatNumber(account.quota_policy.active_jobs)}</dd>
        <dt>{t("common.version")}</dt>
        <dd>{account.version}</dd>
      </dl>
      {keys.length === 0 ? (
        <p className="muted">{t("keys.noKeysInAccount")}</p>
      ) : (
        <KeyTable keys={keys} canWriteKeys={canWriteKeys} onDialog={onDialog} />
      )}
    </article>
  );
}

/**
 * Masked key rows with status and actions.
 *
 * @param props - Keys, permission and dialog opener.
 * @returns Table element.
 */
function KeyTable({
  keys,
  canWriteKeys,
  onDialog,
}: {
  keys: ProviderKey[];
  canWriteKeys: boolean;
  onDialog: (dialog: OpenDialog) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="table-wrap" role="region" aria-label={t("keys.upstream")} tabIndex={0}>
      <table className="table">
        <thead>
          <tr>
            <th scope="col">{t("keys.key")}</th>
            <th scope="col">{t("keys.validity")}</th>
            <th scope="col">{t("keys.billing")}</th>
            <th scope="col">{t("keys.breaker")}</th>
            <th scope="col">{t("keys.cooldown")}</th>
            <th scope="col">{t("keys.retirement")}</th>
            <th scope="col">{t("common.actions")}</th>
          </tr>
        </thead>
        <tbody>
          {keys.map((key) => (
            <KeyRow
              key={key.id}
              providerKey={key}
              canWriteKeys={canWriteKeys}
              onDialog={onDialog}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * One masked key.
 *
 * @param props - Key, permission and dialog opener.
 * @returns Table row.
 */
function KeyRow({
  providerKey,
  canWriteKeys,
  onDialog,
}: {
  providerKey: ProviderKey;
  canWriteKeys: boolean;
  onDialog: (dialog: OpenDialog) => void;
}) {
  const { t } = useI18n();
  const patch = useKeyPatch(providerKey);
  const isRetired = providerKey.retirement_state !== "active";
  return (
    <tr>
      <td>
        <div className="key-label">
          <strong>{providerKey.label}</strong>
          <span
            className="mono"
            aria-label={t("keys.maskedKey", { lastFour: providerKey.last_four })}
          >
            ••••{providerKey.last_four}
          </span>
          <details className="key-fingerprint">
            <summary>{t("keys.fingerprint")}</summary>
            <code>{providerKey.fingerprint}</code>
          </details>
          {providerKey.enabled ? null : <Badge tone="warning">{t("common.disabled")}</Badge>}
        </div>
        {providerKey.retained_credential_count > 0 ? (
          <p className="muted">
            {t("keys.retainedCredentials", { count: providerKey.retained_credential_count })}
          </p>
        ) : null}
      </td>
      <td>
        <Badge tone={VALIDITY_TONE[providerKey.validity]}>
          {t(`validity.${providerKey.validity}`)}
        </Badge>
      </td>
      <td>
        <Badge tone={BILLING_TONE[providerKey.billing_state]}>
          {t(`billingState.${providerKey.billing_state}`)}
        </Badge>
      </td>
      <td>
        <Badge tone={BREAKER_TONE[providerKey.breaker]}>
          {t(`breaker.${providerKey.breaker}`)}
        </Badge>
      </td>
      <td>
        <DateTime value={providerKey.cooldown_until} />
      </td>
      <td>{t(`retirement.${providerKey.retirement_state}`)}</td>
      <td>
        <div className="button-row tight">
          <button
            type="button"
            className="button button-small"
            disabled={!canWriteKeys || isRetired || patch.isPending}
            onClick={() => patch.mutate({ enabled: !providerKey.enabled })}
          >
            {providerKey.enabled ? t("keys.disable") : t("keys.enable")}
          </button>
          <button
            type="button"
            className="button button-small"
            disabled={!canWriteKeys || isRetired}
            onClick={() => onDialog({ kind: "key-rotate", key: providerKey })}
          >
            {t("keys.rotate")}
          </button>
          <button
            type="button"
            className="button button-small button-danger"
            disabled={!canWriteKeys || isRetired}
            onClick={() => onDialog({ kind: "key-retire", key: providerKey })}
          >
            {t("keys.retire")}
          </button>
        </div>
        {patch.isError ? <InlineError error={patch.error} /> : null}
      </td>
    </tr>
  );
}
