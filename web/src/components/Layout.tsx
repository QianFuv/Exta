/** Authenticated application shell: brand, navigation, language and sign-out. */
import { Suspense, useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { useMutation } from "@tanstack/react-query";
import { call, useApi } from "../lib/api";
import { useI18n } from "../lib/i18n";
import type { MessageKey } from "../lib/messages";
import { useSession } from "../lib/session";
import { InlineError, LoadingState } from "./Feedback";
import { LanguageSwitch } from "./LanguageSwitch";
import { BrandMark } from "./BrandMark";

const NAVIGATION: ReadonlyArray<{ to: string; label: MessageKey }> = [
  { to: "/overview", label: "nav.overview" },
  { to: "/keys", label: "nav.keys" },
  { to: "/routing", label: "nav.routing" },
  { to: "/usage", label: "nav.usage" },
  { to: "/requests", label: "nav.requests" },
  { to: "/research", label: "nav.research" },
  { to: "/connections", label: "nav.connections" },
  { to: "/settings", label: "nav.settings" },
  { to: "/audit", label: "nav.audit" },
];

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
  const [isNavOpen, setNavOpen] = useState(false);
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

  return (
    <div className="shell">
      <a className="skip-link" href="#main">
        {t("common.skipToContent")}
      </a>
      <MockModeBanner />
      <header className="topbar">
        <button
          type="button"
          className="button button-ghost nav-toggle"
          aria-expanded={isNavOpen}
          aria-controls="primary-navigation"
          onClick={() => setNavOpen((open) => !open)}
        >
          {t("nav.menu")}
        </button>
        <div className="brand">
          <BrandMark />
          <span className="brand-name">Exta</span>
          <span className="brand-tagline">{t("brand.tagline")}</span>
        </div>
        <div className="topbar-actions">
          <LanguageSwitch />
          {session === null ? null : (
            <span className="principal" title={t("session.principal")}>
              <span className="visually-hidden">{t("session.principal")}: </span>
              <code>{session.principal_id}</code>
            </span>
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
      </header>
      {logout.isError ? (
        <div className="topbar-error">
          <InlineError error={logout.error} />
        </div>
      ) : null}
      <div className="body">
        <nav
          id="primary-navigation"
          className={isNavOpen ? "sidebar sidebar-open" : "sidebar"}
          aria-label={t("nav.primary")}
          onKeyDown={(event) => {
            if (event.key === "Escape" && isNavOpen) {
              setNavOpen(false);
            }
          }}
        >
          <ul>
            {NAVIGATION.map((item) => (
              <li key={item.to}>
                <NavLink
                  to={item.to}
                  className={({ isActive }) => (isActive ? "nav-link active" : "nav-link")}
                  onClick={() => setNavOpen(false)}
                >
                  {t(item.label)}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>
        <main id="main" ref={mainRef} tabIndex={-1} className="content">
          <Suspense fallback={<LoadingState />}>
            <Outlet />
          </Suspense>
        </main>
      </div>
    </div>
  );
}
