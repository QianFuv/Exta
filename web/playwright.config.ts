/** Run acceptance only against an explicitly supplied standalone executable. */
import { defineConfig } from "@playwright/test";

process.env.PLAYWRIGHT_NO_COPY_PROMPT = "1";

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  retries: 0,
  timeout: 90_000,
  globalTimeout: 480_000,
  reporter: [["list"], ["json", { outputFile: "test-results/browser.json" }]],
  use: { browserName: "chromium", headless: true, screenshot: "off", trace: "off", video: "off" },
});
