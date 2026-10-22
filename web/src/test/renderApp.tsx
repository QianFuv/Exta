/** Render the full application at a route with test clients. */
import { render } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router";
import { APP_ROUTES, AppProviders } from "../app/App";
import { createApiClient } from "../generated/client.gen";
import type { Locale } from "../lib/i18n";
import { createAppQueryClient } from "../lib/queryClient";
import { mockDb } from "../mocks/handlers";
import type { RecordedCall } from "../mocks/handlers";

/**
 * Render the app with a memory router, an absolute-URL client and no read retries.
 *
 * @param path - Initial route, relative to `/app`.
 * @param options - Interface locale; English by default for readable assertions.
 * @returns Testing Library result plus the router.
 */
export function renderApp(path: string, options: { locale?: Locale } = {}) {
  const router = createMemoryRouter(APP_ROUTES, { initialEntries: [path] });
  const queryClient = createAppQueryClient({ retryReads: false });
  const result = render(
    <AppProviders
      apiClient={createApiClient({ baseUrl: "http://localhost" })}
      queryClient={queryClient}
      initialLocale={options.locale ?? "en"}
    >
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { ...result, router, queryClient };
}

/**
 * Recorded mock calls matching a method and path.
 *
 * @param method - HTTP method.
 * @param path - Exact path.
 * @returns Matching calls.
 */
export function callsTo(method: string, path: string): RecordedCall[] {
  return mockDb.state.calls.filter((call) => call.method === method && call.path === path);
}

/**
 * All text currently stored in local and session storage.
 *
 * @returns Concatenated keys and values.
 */
export function allBrowserStorage(): string {
  const parts: string[] = [];
  for (const storage of [window.localStorage, window.sessionStorage]) {
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index) ?? "";
      parts.push(key, storage.getItem(key) ?? "");
    }
  }
  return parts.join("\n");
}
