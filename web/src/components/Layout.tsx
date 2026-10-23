/** Authenticated application shell: brand, navigation, language and sign-out. */
import { Suspense, useEffect, useRef } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { useMutation } from "@tanstack/react-query";
import { call, useApi } from "../lib/api";
import { useI18n } from "../lib/i18n";
import type { MessageKey } from "../lib/messages";
import { useSession } from "../lib/session";
import { InlineError, LoadingState } from "./Feedback";
import { LanguageSwitch } from "./LanguageSwitch";
import { BrandMark } from "./BrandMark";
import { ThemeSwitch } from "./ThemeSwitch";
import { NavigationIcon } from "./NavigationIcon";

const NAVIGATION = [
  { to: "/overview", label: "nav.overview", icon: "overview" },
  { to: "/keys", label: "nav.keys", icon: "keys" },
  { to: "/requests", label: "nav.requests", icon: "requests" },
  { to: "/settings", label: "nav.settings", icon: "settings" },
] as const satisfies ReadonlyArray<{
  to: string;
  label: MessageKey;
  icon: "overview" | "keys" | "requests" | "settings";
}>;

/**
 * Banner shown only in the build-time mock mode so fixture data is never mistaken for
 * production state.
 *
 * @returns Banner or nothing.
 */
export function MockModeBanner() {
  const { t } = useI18n();
  if (import.meta.env.MODE !== "mock") {
    return null;
  }
  return (
    <div className="mock-banner" role="note">
      {t("mock.banner")}
    </div>
  );
}

/**
 * Application layout for authenticated routes.
 *
 * @returns Shell with navigation and routed content.
 */
export function Layout() {
  const { t } = useI18n();
  const api = useApi();
  const { session, csrfToken, expire } = useSession();
  const location = useLocation();
  const mainRef = useRef<HTMLElement>(null);
  const isFirstRender = useRef(true);

  useEffect(() => {
    if (isFirstRender.current) {
      isFirstRender.current = false;
      return;
    }
    mainRef.current?.focus();
  }, [location.pathname]);

  const logout = useMutation({
    mutationFn: () =>
      call(api.DELETE("/v1/admin/session", { params: { header: { "X-Exta-CSRF": csrfToken } } })),
    onSuccess: () => expire("logged_out"),
  });

  const current = NAVIGATION.find((item) => location.pathname.startsWith(item.to));
  return (
    <div className="shell">
      <a className="skip-link" href="#main">
        {t("common.skipToContent")}
      </a>
      <MockModeBanner />
      <aside className="sidebar">
        <div className="brand">
          <BrandMark />
          <span className="brand-name">Exta</span>
          <span className="brand-console">Console</span>
        </div>
        <p className="nav-caption">{t("nav.workspace")}</p>
        <nav id="primary-navigation" aria-label={t("nav.primary")}>
          <ul>
            {NAVIGATION.map((item) => (
              <li key={item.to}>
                <NavLink
                  to={item.to}
                  className={({ isActive }) => (isActive ? "nav-link active" : "nav-link")}
                >
                  <NavigationIcon name={item.icon} />
                  {t(item.label)}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>
        <div className="sidebar-note">
          <span className="sidebar-dot" />
          MCP / REST API<p>{t("overview.readOnly")}</p>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div className="breadcrumb">
            <span>Exta</span>
            <span aria-hidden="true">/</span>
            <strong>{current ? t(current.label) : t("nav.workspace")}</strong>
          </div>
          <div className="topbar-actions">
            <ThemeSwitch />
            <LanguageSwitch />
            <details className="session-menu">
              <summary>{t("nav.workspace")}</summary>
              <div>
                {session === null ? null : (
                  <p className="principal">
                    {t("session.principal")}
                    <code>{session.principal_id}</code>
                  </p>
                )}
                <button
                  type="button"
                  className="button button-ghost"
                  onClick={() => logout.mutate()}
                  disabled={logout.isPending}
                >
                  {logout.isPending ? t("session.signingOut") : t("session.signOut")}
                </button>
              </div>
            </details>
          </div>
        </header>
        {logout.isError ? (
          <div className="topbar-error">
            <InlineError error={logout.error} />
          </div>
        ) : null}
        <main id="main" ref={mainRef} tabIndex={-1} className="content">
          <Suspense fallback={<LoadingState />}>
            <Outlet />
          </Suspense>
        </main>
      </div>
    </div>
  );
}
