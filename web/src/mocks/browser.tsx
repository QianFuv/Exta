/** Development-only mock service worker; loaded exclusively in `vite --mode mock`. */
import { setupWorker } from "msw/browser";
import { createMockState, handlers, mockDb } from "./handlers";

/** Start intercepting admin API calls with fixtures (signed out, to exercise login). */
export async function startMockWorker(): Promise<void> {
  mockDb.state = createMockState({ signedIn: false });
  await setupWorker(...handlers).start({
    serviceWorker: { url: "/app/mockServiceWorker.js" },
    onUnhandledRequest: "bypass",
  });
}
