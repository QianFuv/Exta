import "@douyinfe/semi-ui/react19-adapter";
import { SemiProvider } from "../components/SemiProvider";
/** Application providers and the route table shared by the browser entry and tests. */
import { lazy } from "react";
import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { Navigate, Outlet, useLocation } from "react-router";
import type { RouteObject } from "react-router";
import { ApiProvider } from "../lib/api";
import type { ApiClient } from "../lib/api";
import { I18nProvider, useI18n } from "../lib/i18n";
import type { Locale } from "../lib/i18n";
import { SessionProvider, useSession } from "../lib/session";
import { NotificationsProvider } from "../components/Notifications";
import { Layout } from "../components/Layout";
import { ErrorState, LoadingState } from "../components/Feedback";
import { LoginPage } from "../features/auth/LoginPage";
import { HomePage } from "../features/home/HomePage";
import { NotFoundPage } from "./NotFoundPage";

const OverviewPage = lazy(() =>
  import("../features/overview/OverviewPage").then((module) => ({ default: module.OverviewPage })),
);
const KeysPage = lazy(() =>
  import("../features/keys/KeysPage").then((module) => ({ default: module.KeysPage })),
);
const AccessKeysPage = lazy(() =>
  import("../features/connections/AccessKeysPage").then((module) => ({
    default: module.AccessKeysPage,
  })),
);
const RequestsPage = lazy(() =>
  import("../features/requests/RequestsPage").then((module) => ({ default: module.RequestsPage })),
);
const RequestDetailPage = lazy(() =>
  import("../features/requests/RequestDetailPage").then((module) => ({
    default: module.RequestDetailPage,
  })),
);
const SettingsPage = lazy(() =>
  import("../features/settings/SettingsPage").then((module) => ({ default: module.SettingsPage })),
);
/**
 * Wrap the application with API, query, language, session and notification providers.
 *
 * @param props - Client instances, optional fixed locale and children.
 * @returns Provider tree.
 */
export function AppProviders({
  apiClient,
  queryClient,
  initialLocale,
  children,
}: {
  apiClient: ApiClient;
  queryClient: QueryClient;
  initialLocale?: Locale;
  children: ReactNode;
}) {
  return (
    <ApiProvider value={apiClient}>
      <QueryClientProvider client={queryClient}>
        <I18nProvider {...(initialLocale === undefined ? {} : { initialLocale })}>
          <SemiProvider>
            <SessionProvider>
              <NotificationsProvider>{children}</NotificationsProvider>
            </SessionProvider>
          </SemiProvider>
        </I18nProvider>
      </QueryClientProvider>
    </ApiProvider>
  );
}

/**
 * Gate authenticated routes on the in-memory session.
 *
 * @returns Child routes, login redirect, or a loading/error state.
 */
function RequireSession() {
  const { status, loadError, retry } = useSession();
  const { t } = useI18n();
  const location = useLocation();
  if (status === "loading") {
    return (
      <div className="standalone">
        <LoadingState label={t("session.checking")} />
      </div>
    );
  }
  if (status === "error") {
    return (
      <div className="standalone">
        <ErrorState error={loadError} onRetry={retry} title={t("session.unavailable")} />
      </div>
    );
  }
  if (status === "anonymous") {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  }
  return <Outlet />;
}

/** Route table relative to the `/app` basename. */
export const APP_ROUTES: RouteObject[] = [
  { index: true, element: <HomePage /> },
  { path: "/login", element: <LoginPage /> },
  {
    element: <RequireSession />,
    children: [
      {
        element: <Layout />,
        children: [
          { path: "/overview", element: <OverviewPage /> },
          { path: "/keys", element: <KeysPage /> },
          { path: "/access-keys", element: <AccessKeysPage /> },
          { path: "/requests", element: <RequestsPage /> },
          { path: "/requests/:id", element: <RequestDetailPage /> },
          { path: "/settings", element: <SettingsPage /> },
          { path: "*", element: <NotFoundPage /> },
        ],
      },
    ],
  },
];
