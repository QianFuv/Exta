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

export default defineConfig({
  base: "./",
  publicDir: false,
  plugins: [react(), mockWorkerPlugin()],
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
