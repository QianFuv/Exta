import { Button } from "./Controls";
import { PreferencesMenu } from "./PreferencesMenu";
/**
 * Authenticated application shell built on Semi Layout: a full sidebar on desktop, an
 * icon rail on tablets and a bottom tab bar on phones. One semantic navigation list is
 * recomposed by CSS so its links, names and current-page state stay identical.
 */
import { Suspense, useEffect, useRef } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";
import { useMutation } from "@tanstack/react-query";
import SemiLayout from "@douyinfe/semi-ui/lib/es/layout";
import IconExit from "@douyinfe/semi-icons/lib/es/icons/IconExit";
import IconHomeStroked from "@douyinfe/semi-icons/lib/es/icons/IconHomeStroked";
import { call, useApi } from "../lib/api";
import { useI18n } from "../lib/i18n";
import type { MessageKey } from "../lib/messages";
import { useSession } from "../lib/session";
import { InlineError, LoadingState } from "./Feedback";
import { BrandMark } from "./BrandMark";
import { NavigationIcon } from "./NavigationIcon";
import type { NavigationIconName } from "./NavigationIcon";

const NAVIGATION = [
  { to: "/overview", label: "nav.overview", short: "nav.short.overview", icon: "overview" },
  { to: "/keys", label: "keys.upstream", short: "nav.short.keys", icon: "keys" },
  { to: "/access-keys", label: "keys.access", short: "nav.short.access", icon: "access" },
  { to: "/requests", label: "nav.requests", short: "nav.short.requests", icon: "requests" },
  { to: "/settings", label: "nav.settings", short: "nav.short.settings", icon: "settings" },
] as const satisfies ReadonlyArray<{
  to: string;
  label: MessageKey;
  short: MessageKey;
  icon: NavigationIconName;
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
  const { csrfToken, expire } = useSession();
  const location = useLocation();
  const navigationRef = useRef<HTMLElement>(null);
  const isFirstRender = useRef(true);
  const current = NAVIGATION.find(
    (item) => location.pathname === item.to || location.pathname.startsWith(item.to + "/"),
  );

  useEffect(() => {
    /** Keep the selected destination visible after navigation, translation or resizing. */
    const revealActive = () => {
      const nav = navigationRef.current;
      const active = nav?.querySelector<HTMLElement>("[aria-current=page]");
      if (!nav || !active || nav.scrollWidth <= nav.clientWidth) return;
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
  }, [location.pathname]);

  useEffect(() => {
    if (isFirstRender.current) {
      isFirstRender.current = false;
      return;
    }
    document.getElementById("main")?.focus();
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
      <SemiLayout className="shell-frame" hasSider tagName="div">
        <SemiLayout.Sider className="shell-sider">
          <Link className="brand" to="/" aria-label={`Exta · ${t("nav.home")}`}>
            <BrandMark />
            <span className="brand-name">Exta</span>
          </Link>
          <p className="nav-caption">{t("nav.workspace")}</p>
          <nav
            ref={navigationRef}
            id="primary-navigation"
            aria-label={t("nav.primary")}
            className="primary-navigation"
          >
            <ul>
              {NAVIGATION.map((item) => (
                <li key={item.to}>
                  <NavLink
                    to={item.to}
                    className={({ isActive }) => (isActive ? "nav-link active" : "nav-link")}
                  >
                    <NavigationIcon name={item.icon} />
                    <span className="nav-label">{t(item.label)}</span>
                    <span className="nav-label-short" aria-hidden="true">
                      {t(item.short)}
                    </span>
                  </NavLink>
                </li>
              ))}
            </ul>
          </nav>
          <div className="sider-footer">
            <Link className="sider-home" to="/">
              <IconHomeStroked aria-hidden />
              <span>{t("nav.home")}</span>
            </Link>
          </div>
        </SemiLayout.Sider>
        <SemiLayout className="shell-main" tagName="div">
          <SemiLayout.Header className="topbar">
            <Link className="topbar-brand" to="/" aria-label={`Exta · ${t("nav.home")}`}>
              <BrandMark size={26} />
              <span aria-hidden="true">Exta</span>
            </Link>
            <p className="topbar-context">
              <span>{t("nav.console")}</span>
              {current ? (
                <>
                  <span className="topbar-separator" aria-hidden="true">
                    /
                  </span>
                  <span className="topbar-current">{t(current.label)}</span>
                </>
              ) : null}
            </p>
            <div className="topbar-actions">
              <PreferencesMenu />
              <Button
                className="button-ghost session-sign-out"
                onClick={() => logout.mutate()}
                disabled={logout.isPending}
                aria-label={logout.isPending ? t("session.signingOut") : t("session.signOut")}
              >
                <IconExit aria-hidden className="session-sign-out-icon" />
                <span className="session-sign-out-label">
                  {logout.isPending ? t("session.signingOut") : t("session.signOut")}
                </span>
              </Button>
            </div>
          </SemiLayout.Header>
          {logout.isError ? (
            <div className="topbar-error">
              <InlineError error={logout.error} />
            </div>
          ) : null}
          <main id="main" tabIndex={-1} className="content">
            <div key={location.pathname} className="route-view">
              <Suspense fallback={<LoadingState />}>
                <Outlet />
              </Suspense>
            </div>
          </main>
        </SemiLayout>
      </SemiLayout>
    </div>
  );
}
