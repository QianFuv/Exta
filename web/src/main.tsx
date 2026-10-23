import "@douyinfe/semi-ui/react19-adapter";
import "@douyinfe/semi-ui/lib/es/_base/base.css";
/** Browser entry: mounts Exta under the `/app/` base path. */
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter } from "react-router";
import { RouterProvider } from "react-router/dom";
import { APP_ROUTES, AppProviders } from "./app/App";
import { createBrowserApiClient } from "./lib/api";
import { createAppQueryClient } from "./lib/queryClient";
import "./styles/app.css";
import "./styles/semi.css";

/**
 * Start the optional development-only mock worker. Production builds statically remove
 * this branch, so no worker or fixture code is emitted.
 */
async function prepareMocks(): Promise<void> {
  if (import.meta.env.DEV && import.meta.env.MODE === "mock") {
    const { startMockWorker } = await import("./mocks/browser");
    await startMockWorker();
  }
}

/** Mount the application. */
async function start(): Promise<void> {
  await prepareMocks();
  const container = document.getElementById("root");
  if (container === null) {
    throw new Error("Missing #root element");
  }
  const router = createBrowserRouter(APP_ROUTES, { basename: "/app" });
  createRoot(container).render(
    <StrictMode>
      <AppProviders apiClient={createBrowserApiClient()} queryClient={createAppQueryClient()}>
        <RouterProvider router={router} />
      </AppProviders>
    </StrictMode>,
  );
}

void start();
