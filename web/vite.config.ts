/// <reference types="vitest/config" />
/** Build Exta's static application for embedding in the Go executable. */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import type { Plugin } from "vite";
import react from "@vitejs/plugin-react";

/**
 * Serve MSW's worker script only from the development server in `--mode mock`, so it is
 * never copied into production output.
 *
 * @returns Development-only Vite plugin.
 */
function mockWorkerPlugin(): Plugin {
  return {
    name: "exta-mock-worker",
    apply: (config, environment) => environment.command === "serve" && environment.mode === "mock",
    configureServer(server) {
      const workerPath = fileURLToPath(import.meta.resolve("msw/mockServiceWorker.js"));
      server.middlewares.use("/app/mockServiceWorker.js", (request, response) => {
        response.setHeader("Content-Type", "text/javascript");
        response.end(readFileSync(workerPath));
      });
    },
  };
}

/** Emit a parser-blocking theme asset without weakening the production CSP. */
function themeBootstrapPlugin(): Plugin {
  let isBuild = false;
  const source = readFileSync(new URL("./src/theme-init.js", import.meta.url), "utf8").replace(
    /\r\n?/g,
    "\n",
  );
  return {
    name: "exta-theme-bootstrap",
    configResolved(config) {
      isBuild = config.command === "build";
    },
    buildStart() {
      if (isBuild) this.emitFile({ type: "asset", fileName: "assets/theme-init.js", source });
    },
    transformIndexHtml: {
      order: "post",
      handler() {
        return [
          { tag: "script", attrs: { src: "./assets/theme-init.js" }, injectTo: "head-prepend" },
        ];
      },
    },
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        if (request.url?.split("?")[0]?.endsWith("/assets/theme-init.js")) {
          response.setHeader("Content-Type", "text/javascript");
          response.end(source);
        } else next();
      });
    },
  };
}

export default defineConfig({
  base: "./",
  publicDir: false,
  plugins: [themeBootstrapPlugin(), react(), mockWorkerPlugin()],
  build: {
    manifest: true,
    assetsInlineLimit: 0,
    sourcemap: false,
    modulePreload: { polyfill: false },
  },
  server: {
    host: "127.0.0.1",
    strictPort: true,
    proxy: {
      "/v1": "http://127.0.0.1:8789",
      "/mcp": "http://127.0.0.1:8789",
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.tsx"],
    include: ["src/**/*.test.tsx"],
    restoreMocks: true,
    testTimeout: 30_000,
  },
});
