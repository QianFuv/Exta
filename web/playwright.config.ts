/** Run acceptance only against an explicitly supplied standalone executable. */
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  retries: 0,
  timeout: 90_000,
  globalTimeout: 300_000,
  reporter: [["list"], ["json", { outputFile: "test-results/browser.json" }]],
  use: { browserName: "chromium", headless: true, screenshot: "only-on-failure" },
});
