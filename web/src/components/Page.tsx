/** Page and section scaffolding plus permission gating. */
import { useEffect } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../lib/i18n";
import { useSession } from "../lib/session";
import type { AdminPermission } from "../lib/session";
import { TitleHelp } from "./TitleHelp";
import { LockedState } from "./Feedback";

/**
 * Page heading with optional description and actions; also sets the document title.
 *
 * @param props - Title, description and actions.
 * @returns Header element.
 */
export function PageHeader({
  title,
  description,
  help,
  actions,
}: {
  title: string;
  description?: string;
  help?: string;
  actions?: ReactNode;
}) {
  useEffect(() => {
    document.title = `${title} · Exta`;
  }, [title]);
  return (
    <header className="page-header">
      <div>
        <div className="heading-with-help">
          <h1>{title}</h1>
          {help ? <TitleHelp title={title}>{help}</TitleHelp> : null}
        </div>
        {description === undefined ? null : <p className="page-description">{description}</p>}
      </div>
      {actions === undefined ? null : <div className="page-actions">{actions}</div>}
    </header>
  );
}

/**
 * Titled content section.
 *
 * @param props - Heading, optional actions and content.
 * @returns Section element.
 */
export function Section({
  title,
  actions,
  children,
  description,
  help,
}: {
  title: string;
  actions?: ReactNode;
  description?: string;
  help?: string;
  children: ReactNode;
}) {
  return (
    <section className="section">
      <div className="section-header">
        <div>
          <div className="heading-with-help">
            <h2>{title}</h2>
            {help ? <TitleHelp title={title}>{help}</TitleHelp> : null}
          </div>
          {description === undefined ? null : <p className="section-description">{description}</p>}
        </div>
        {actions === undefined ? null : <div className="section-actions">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

/**
 * Render children only when the stable principal holds the permission. Otherwise show
 * a locked state and mount nothing that could issue requests.
 *
 * @param props - Required permission and gated content.
 * @returns Content or locked state.
 */
export function RequirePermission({
  permission,
  children,
}: {
  permission: AdminPermission;
  children: ReactNode;
}) {
  const { can } = useSession();
  return can(permission) ? <>{children}</> : <LockedState permission={permission} />;
}

/**
 * Explain why a mutation control is disabled for missing permission.
 *
 * @param props - Permission name.
 * @returns Hint text or nothing.
 */
export function PermissionHint({ permission }: { permission: AdminPermission }) {
  const { can } = useSession();
  const { t } = useI18n();
  if (can(permission)) {
    return null;
  }
  return (
    <p className="permission-hint">
      {t("permission.actionDisabled")} <code>{permission}</code>
    </p>
  );
}
