import { KeyStatus } from "./KeyStatus";
/** Direct upstream-key administration with no parent-account workflow. */
import { useState } from "react";
import { Button, Input, Select, ActionMenu } from "../../components/Controls";
import { DataTable } from "../../components/DataTable";
import { EmptyState, InlineError, QueryView, Notice } from "../../components/Feedback";
import { PageHeader, PermissionHint, RequirePermission, Section } from "../../components/Page";
import { LoadMore } from "../../components/LoadMore";
import { useUnitFormatter } from "../../components/Values";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { useCursorList } from "../../lib/useCursorList";
import {
  KeyCreateDialog,
  KeySettingsDialog,
  KeyRetireDialog,
  KeyRotateDialog,
  useKeyPatch,
} from "./KeyDialogs";

type UpstreamKey = Schemas["UpstreamKey"];
type Provider = Schemas["Provider"];
type OpenDialog =
  | { kind: "create"; provider: Provider }
  | { kind: "edit" | "rotate" | "retire"; key: UpstreamKey }
  | null;

/** Keep read permission ahead of metadata retrieval and all mutation controls. */
export function KeysPage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("keys.upstream")} description={t("keys.description")} />
      <RequirePermission permission="admin:read">
        <KeyInventory />
      </RequirePermission>
    </>
  );
}

/** Present a single flat inventory with nearby filtering and key-owned safety limits. */
function KeyInventory() {
  const { t } = useI18n();
  const api = useApi();
  const { can } = useSession();
  const format = useUnitFormatter();
  const [provider, setProvider] = useState<Provider | "all">("all");
  const [filter, setFilter] = useState("");
  const [dialog, setDialog] = useState<OpenDialog>(null);
  const { query, items } = useCursorList(["keys", provider], (cursor) =>
    call(
      api.GET("/v1/admin/keys", {
        params: {
          query: {
            limit: 100,
            ...(provider === "all" ? {} : { provider }),
            ...(cursor ? { cursor } : {}),
          },
        },
      }),
    ),
  );
  const visible = items.filter((key) =>
    `${key.label} ${key.id} ${key.last_four}`.toLowerCase().includes(filter.trim().toLowerCase()),
  );
  const canWrite = can("admin:keys:write");
  return (
    <Section
      title={t("keys.inventory")}
      actions={
        <ActionMenu
          label={t("keys.addKey")}
          primary
          disabled={!canWrite}
          items={(["exa", "tavily"] as const).map((value) => ({
            label: `${t("keys.addKey")} · ${t(`provider.${value}`)}`,
            disabled: !canWrite,
            onSelect: () => setDialog({ kind: "create", provider: value }),
          }))}
        />
      }
    >
      <PermissionHint permission="admin:keys:write" />
      <div className="inventory-toolbar">
        <Input
          aria-label={t("keys.find")}
          placeholder={t("keys.findHint")}
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
        />
        <Select
          aria-label={t("common.provider")}
          value={provider}
          onChange={(event) => setProvider(event.target.value as Provider | "all")}
        >
          <option value="all">{t("common.all")}</option>
          <option value="exa">Exa</option>
          <option value="tavily">Tavily</option>
        </Select>
        <Button onClick={() => void query.refetch()} disabled={query.isFetching}>
          {t("common.refresh")}
        </Button>
      </div>
      <Notice tone="info">{t("keys.identityNotice")}</Notice>
      <QueryView query={query}>
        {() =>
          visible.length === 0 ? (
            <EmptyState title={t("keys.noMatches")} description={t("keys.emptyDescription")} />
          ) : (
            <>
              <p className="table-caption">{t("keys.keyCount", { count: visible.length })}</p>
              <div
                className="table-wrap"
                role="region"
                aria-label={t("keys.upstream")}
                tabIndex={0}
              >
                <DataTable
                  mobileRow={(key) => (
                    <>
                      <div className="mobile-record-heading">
                        <strong>{key.label}</strong>
                        <KeyStatus providerKey={key} />
                      </div>
                      <p className="cell-secondary">
                        {t(`provider.${key.provider}`)} · ••••{key.last_four}
                      </p>
                      <p className="numeric">
                        {t("keys.allocation")}:{" "}
                        {format(key.quota_policy.allocation, key.quota_policy.unit)}
                      </p>
                      <KeyActions providerKey={key} canWrite={canWrite} onDialog={setDialog} />
                    </>
                  )}
                  dataSource={visible}
                  rowKey={(key) => key.id}
                  columns={[
                    {
                      key: "key",
                      title: t("keys.key"),
                      width: 240,
                      render: (_value, key) => (
                        <div className="key-label">
                          <strong>{key.label}</strong>
                          <span className="cell-secondary">
                            {key.provider === "exa" ? "Exa" : "Tavily"} · ••••{key.last_four}
                          </span>
                          {key.retained_credential_count > 0 ? (
                            <small>
                              {t("keys.retainedCredentials", {
                                count: key.retained_credential_count,
                              })}
                            </small>
                          ) : null}
                        </div>
                      ),
                    },
                    {
                      key: "status",
                      title: t("common.status"),
                      width: 190,
                      render: (_value, key) => <KeyStatus providerKey={key} />,
                    },
                    {
                      key: "limit",
                      title: t("keys.allocation"),
                      width: 180,
                      render: (_value, key) => (
                        <>
                          <span className="numeric">
                            {format(key.quota_policy.allocation, key.quota_policy.unit)}
                          </span>
                          <small className="cell-secondary">
                            {key.quota_policy.allocation > 0 &&
                            key.quota_policy.per_operation_reservation > 0
                              ? t("keys.localLimit")
                              : t("keys.needsLimits")}
                          </small>
                        </>
                      ),
                    },
                    {
                      key: "association",
                      title: t("keys.sharedAllowance"),
                      width: 180,
                      render: (_value, key) => (
                        <span className="muted">
                          {key.association_evidence === "unknown"
                            ? t("keys.identityUnknown")
                            : t("keys.sharedKnown")}
                        </span>
                      ),
                    },
                    {
                      key: "actions",
                      title: t("common.actions"),
                      width: 190,
                      render: (_value, key) => (
                        <KeyActions providerKey={key} canWrite={canWrite} onDialog={setDialog} />
                      ),
                    },
                  ]}
                />
              </div>
            </>
          )
        }
      </QueryView>
      <LoadMore query={query} />
      {dialog?.kind === "create" ? (
        <KeyCreateDialog provider={dialog.provider} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.kind === "edit" ? (
        <KeySettingsDialog providerKey={dialog.key} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.kind === "rotate" ? (
        <KeyRotateDialog providerKey={dialog.key} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.kind === "retire" ? (
        <KeyRetireDialog providerKey={dialog.key} onClose={() => setDialog(null)} />
      ) : null}
    </Section>
  );
}

/** Keep destructive actions behind their existing confirmation and version fences. */
function KeyActions({
  providerKey,
  canWrite,
  onDialog,
}: {
  providerKey: UpstreamKey;
  canWrite: boolean;
  onDialog: (dialog: OpenDialog) => void;
}) {
  const { t } = useI18n();
  const patch = useKeyPatch(providerKey);
  const disabled = !canWrite || providerKey.retirement_state !== "active" || patch.isPending;
  return (
    <>
      <div className="key-actions">
        <Button
          className="button button-small"
          disabled={disabled}
          loading={patch.isPending}
          onClick={() => patch.mutate({ enabled: !providerKey.enabled })}
          aria-label={`${providerKey.enabled ? t("keys.disable") : t("keys.enable")} ${providerKey.label}`}
        >
          {providerKey.enabled ? t("keys.disable") : t("keys.enable")}
        </Button>
        <ActionMenu
          label={t("keys.more")}
          accessibleLabel={t("keys.moreFor", { label: providerKey.label })}
          disabled={disabled}
          items={[
            {
              label: t("keys.editKey"),
              disabled,
              onSelect: () => onDialog({ kind: "edit", key: providerKey }),
            },
            {
              label: t("keys.rotate"),
              disabled,
              onSelect: () => onDialog({ kind: "rotate", key: providerKey }),
            },
            {
              label: t("keys.retire"),
              disabled,
              danger: true,
              onSelect: () => onDialog({ kind: "retire", key: providerKey }),
            },
          ]}
        />
      </div>
      <InlineError error={patch.error} />
    </>
  );
}
