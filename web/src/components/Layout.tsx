import { PreferencesMenu } from "./PreferencesMenu";
/** Authenticated application shell: brand, navigation, language and sign-out. */
import { Suspense, useEffect, useRef } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { useMutation } from "@tanstack/react-query";
import { call, useApi } from "../lib/api";
import { useI18n } from "../lib/i18n";
import type { MessageKey } from "../lib/messages";
import { useSession } from "../lib/session";
import { InlineError, LoadingState } from "./Feedback";
import { BrandMark } from "./BrandMark";
import { NavigationIcon } from "./NavigationIcon";

const NAVIGATION = [
  { to: "/overview", label: "nav.overview", icon: "overview" },
  { to: "/keys", label: "keys.upstream", icon: "keys" },
  { to: "/access-keys", label: "keys.access", icon: "keys" },
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
  const { t, locale } = useI18n();
  const api = useApi();
  const { csrfToken, expire } = useSession();
  const location = useLocation();
  const mainRef = useRef<HTMLElement>(null);
  const navigationRef = useRef<HTMLElement>(null);
  useEffect(() => {
    /** Keep the selected destination visible after navigation, translation or viewport changes. */
    const revealActive = () => {
      const nav = navigationRef.current;
      const active = nav?.querySelector<HTMLElement>("[aria-current=page]");
      if (!nav || !active || !window.matchMedia?.("(max-width: 640px)").matches) return;
      const bounds = nav.getBoundingClientRect();
      const item = active.getBoundingClientRect();
      if (item.left < bounds.left) nav.scrollLeft -= bounds.left - item.left;
      else if (item.right > bounds.right) nav.scrollLeft += item.right - bounds.right;
    };
    const frame = requestAnimationFrame(revealActive);
    window.addEventListener("resize", revealActive);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("resize", revealActive);
    };
  }, [location.pathname, locale]);
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
        <nav ref={navigationRef} id="primary-navigation" aria-label={t("nav.primary")} tabIndex={0}>
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
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div className="breadcrumb">
            <span>Exta</span>
            <span aria-hidden="true">/</span>
            <strong>{current ? t(current.label) : t("nav.workspace")}</strong>
          </div>
          <div className="topbar-actions">
            <PreferencesMenu onLogout={() => logout.mutate()} isLoggingOut={logout.isPending} />
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
