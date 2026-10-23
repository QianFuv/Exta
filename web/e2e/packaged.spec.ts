/** Exercise real cookies, embedded chunks and durable recovery without provider traffic. */
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { spawn, execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import net from "node:net";

/** Bridge the locked axe peer's newer Page type; axe uses APIs present in Playwright 1.63. */
function audit(page: Page) {
  return new AxeBuilder({
    page: page as unknown as ConstructorParameters<typeof AxeBuilder>[0]["page"],
  }).analyze();
}

/** Choose an official Semi option by its visible accessible label. */
async function chooseTheme(page: Page, theme: "light" | "dark" | "system") {
  const label = theme === "light" ? "Light" : theme === "dark" ? "Dark" : "System";
  const preferences = page.getByRole("button", { name: "Preferences", exact: true });
  if (await preferences.isVisible()) {
    await preferences.click();
    await page.getByRole("menuitem", { name: label, exact: true }).click();
  } else {
    await page.getByRole("combobox", { name: "Appearance", exact: true }).click();
    await page.getByRole("option", { name: label, exact: true }).click();
  }
}

/** Await layout and integer scroll rounding while rejecting clipped navigation labels. */
async function expectActiveNavigationVisible(page: Page, description: string) {
  await expect
    .poll(
      () =>
        page.locator('#primary-navigation [aria-current="page"]').evaluate((element) => {
          const item = element.getBoundingClientRect();
          const viewport = document.getElementById("primary-navigation")!.getBoundingClientRect();
          return {
            leftOverflow: Math.max(0, Math.ceil(viewport.left - item.left - 1)),
            rightOverflow: Math.max(0, Math.ceil(item.right - viewport.right - 1)),
          };
        }),
      { message: description },
    )
    .toEqual({ leftOverflow: 0, rightOverflow: 0 });
}

test("standalone key-only administration and persistent appearance", async ({
  page,
  context,
}, info) => {
  test.setTimeout(240_000);
  page.setDefaultTimeout(15_000);
  const binary = process.env.EXTA_E2E_BINARY;
  if (!binary || !path.isAbsolute(binary))
    throw new Error("EXTA_E2E_BINARY must identify the native production executable");
  const root = path.resolve(import.meta.dirname, "../..");
  const directory = mkdtempSync(path.join(tmpdir(), "exta-browser-"));
  const state = path.join(directory, "state");
  const initialized = execFileSync(binary, ["init", "--data-dir", state], {
    encoding: "utf8",
    windowsHide: true,
    timeout: 20_000,
  });
  const credential = initialized.trim().split(/\r?\n/).at(-1)!;
  const environment = { ...process.env, EXTA_BROWSER_STATE: state };
  const go = process.env.EXTA_GO_BINARY ?? "go";
  execFileSync(go, ["test", "./tools/release", "-run", "^TestBrowserFixture$", "-count=1"], {
    cwd: root,
    env: environment,
    windowsHide: true,
    timeout: 60_000,
  });
  const fixture = JSON.parse(readFileSync(path.join(directory, "fixture.json"), "utf8")) as {
    owner: string;
    jobs: Record<string, string>;
    body: object;
    key: string;
    upstreamKey: string;
    request: string;
  };
  const socket = net.createServer();
  await new Promise<void>((resolve) => socket.listen(0, "127.0.0.1", resolve));
  const port = (socket.address() as net.AddressInfo).port;
  await new Promise<void>((resolve, reject) =>
    socket.close((error) => (error ? reject(error) : resolve())),
  );
  const origin = `http://127.0.0.1:${port}`;
  writeFileSync(
    path.join(state, "config.json"),
    JSON.stringify({ listen: `127.0.0.1:${port}`, origin }),
  );
  const server = spawn(binary, ["serve", "--data-dir", state], {
    windowsHide: true,
    stdio: "ignore",
  });
  const joined = new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.once("exit", () => resolve());
  });
  const errors: string[] = [];
  const external: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (/violates.*content security|refused to.*content security/i.test(message.text()))
      errors.push(message.text());
  });
  await context.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) {
      external.push(url.origin);
      await route.abort();
    } else await route.continue();
  });
  try {
    await expect
      .poll(
        async () => {
          try {
            return (await fetch(`${origin}/healthz`)).status;
          } catch {
            return 0;
          }
        },
        { timeout: 10_000 },
      )
      .toBe(200);
    await page.goto(`${origin}/app/`);
    await page.getByRole("button", { name: /English/ }).click();
    await expect(audit(page).then((result) => result.violations)).resolves.toEqual([]);
    await page.getByLabel("Operator credential").fill(credential);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Statistics", exact: true })).toBeVisible();
    const cookies = await context.cookies();
    expect(cookies.some((cookie) => cookie.httpOnly)).toBe(true);
    expect(await page.evaluate(() => document.cookie)).not.toContain("exta_session");
    for (const route of [
      "overview",
      "keys",
      "access-keys",
      "requests",
      "settings",
      "settings?view=routing",
      "settings?view=health",
      "settings?view=budgets",
      "settings?view=connection",
      "settings?view=audit",
      "overview?view=usage",
    ]) {
      const response = await page.goto(`${origin}/app/${route}`);
      expect(response?.status(), route).toBe(200);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await expect(page.locator(".state-loading")).toHaveCount(0);
    }
    const session = await (await page.request.get(`${origin}/v1/admin/session`)).json();
    expect(session.principal_id).toBe(fixture.owner);
    const denied = await page.request.post(`${origin}/v1/admin/research`, {
      data: fixture.body,
      headers: { Origin: origin, "Idempotency-Key": "browser-csrf-rejection" },
    });
    expect(denied.status()).toBe(403);
    await page.goto(`${origin}/app/keys`);
    let moreActions = page.getByRole("button", {
      name: "More actions for Invalid offline fixture key",
      exact: true,
    });
    await expect(page.getByRole("menuitem", { name: "Retire", exact: true })).not.toBeVisible();
    for (let attempt = 0; attempt < 2; attempt += 1) {
      await moreActions.focus();
      await page.keyboard.press("Enter");
      await page.getByRole("menuitem", { name: "Retire", exact: true }).click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await page.getByRole("dialog").getByRole("button", { name: "Cancel", exact: true }).click();
      await expect(moreActions).toBeFocused();
      await expect(page.getByRole("menuitem", { name: "Retire", exact: true })).not.toBeVisible();
    }
    await moreActions.click();
    await page.getByRole("menuitem", { name: "Edit key & limits", exact: true }).click();
    const keyDialog = page.getByRole("dialog", { name: "Edit key & limits" });
    const grouping = keyDialog.getByRole("combobox");
    await grouping.click();
    await expect(page.getByRole("option", { name: "1", exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(grouping).toHaveAttribute("aria-expanded", "false");
    await expect(keyDialog).toBeVisible();
    expect((await audit(page)).violations).toEqual([]);
    const keys = await (await page.request.get(`${origin}/v1/admin/keys`)).json();
    const key = keys.items.find((entry: { id: string }) => entry.id === fixture.upstreamKey);
    const concurrent = await page.request.patch(`${origin}/v1/admin/keys/${fixture.upstreamKey}`, {
      data: { expected_version: key.version, label: "Concurrently updated fixture" },
      headers: { Origin: origin, "X-Exta-CSRF": session.csrf_token },
    });
    expect(concurrent.status()).toBe(200);
    await page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }),
    ).toBeDisabled();
    await page.getByRole("button", { name: "Reload and review latest", exact: true }).click();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(page.getByText("Concurrently updated fixture", { exact: true })).toBeVisible();
    moreActions = page.getByRole("button", {
      name: "More actions for Concurrently updated fixture",
      exact: true,
    });
    await moreActions.click();
    await page.getByRole("menuitem", { name: "Edit key & limits", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByLabel("Label", { exact: true })
      .fill("Reviewed fixture key");
    await page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(page.getByText("Reviewed fixture key", { exact: true })).toBeVisible();
    await page.goto(`${origin}/app/access-keys`);
    await page.getByRole("button", { name: "Create access key", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByLabel("Label", { exact: true })
      .fill("Browser acceptance token");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Create access key", exact: true })
      .click();
    await page.getByRole("button", { name: "I have copied it", exact: true }).click();
    await page
      .getByRole("row")
      .filter({ hasText: "Browser acceptance token" })
      .getByRole("button", { name: "Revoke", exact: true })
      .click();
    await page.getByRole("dialog").getByRole("button", { name: "Revoke", exact: true }).click();
    await expect(
      page.getByRole("row").filter({ hasText: "Browser acceptance token" }),
    ).toContainText("Revoked");
    for (const route of ["/assets/missing.js", "/v1/missing"]) {
      const response = await page.request.get(origin + route);
      expect(response.status()).toBe(404);
      expect(response.headers()["content-type"]).toContain("application/json");
    }
    for (const theme of ["light", "dark"] as const) {
      await chooseTheme(page, theme);
      await page.emulateMedia({ colorScheme: theme === "light" ? "dark" : "light" });
      for (const route of [
        "overview",
        "keys",
        "access-keys",
        "requests",
        "settings",
        "settings?view=routing",
        "settings?view=budgets",
        "settings?view=health",
        "settings?view=connection",
        "settings?view=audit",
        "overview?view=usage",
      ]) {
        await page.goto(`${origin}/app/${route}`);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await expect(page.locator(".state-loading")).toHaveCount(0);
        await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
        expect(
          await page.locator("html").evaluate((element) => getComputedStyle(element).colorScheme),
        ).toBe(theme);
        const fontFamilies = await page
          .locator("h1, .page-description, .nav-link, .semi-tag-content, .semi-button-content")
          .evaluateAll((elements) => [
            ...new Set(elements.map((element) => getComputedStyle(element).fontFamily)),
          ]);
        expect(fontFamilies, `${theme}: coherent typography on ${route}`).toHaveLength(1);
        expect(fontFamilies[0]).toContain("Segoe UI Variable Text");
        expect.soft((await audit(page)).violations, `${theme}: ${route}`).toEqual([]);
      }
      await page.goto(`${origin}/app/requests`);
      const dateField = page.getByLabel("Created after", { exact: true });
      await dateField.focus();
      await page.keyboard.press("ArrowDown");
      await expect(page.locator(".semi-datepicker-month-grid")).toBeVisible();
      await page.keyboard.press("Escape");
      await dateField.fill("not-a-date");
      await expect(page.getByRole("button", { name: "Apply", exact: true })).toBeDisabled();
      await dateField.fill("");
      await page.keyboard.press("Escape");
      const opener = page.locator(`a[href$="?request=${fixture.request}"]`);
      for (let attempt = 0; attempt < 2; attempt++) {
        await opener.click();
        const dialog = page.getByRole("dialog", { name: "Request detail" });
        await expect(dialog.getByText("Attempt 1", { exact: true })).toBeVisible();
        expect((await audit(page)).violations).toEqual([]);
        await page.keyboard.press("Shift+Tab");
        expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(
          true,
        );
        await page.keyboard.press("Tab");
        await page.keyboard.press("Escape");
        await expect(dialog).not.toBeVisible();
        await expect(opener).toBeFocused();
      }
      await page.screenshot({ path: info.outputPath(`logs-${theme}.png`), fullPage: true });
      await page.setViewportSize({ width: 390, height: 844 });
      await page.getByRole("button", { name: "Preferences", exact: true }).click();
      await expect(page.getByRole("menuitem", { name: "System", exact: true })).toBeVisible();
      await page.keyboard.press("Escape");
      await expectActiveNavigationVisible(page, `${theme}: navigation after mobile resize`);
      for (const route of ["overview", "keys", "access-keys", "requests", "settings"]) {
        await page.goto(`${origin}/app/${route}`);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await expect(page.locator(".state-loading")).toHaveCount(0);
        expect(
          await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
          `${theme}: mobile ${route}`,
        ).toBe(true);
        if (route === "keys" || route === "access-keys") {
          const records = page.locator(".mobile-records").first();
          await expect(records).toBeVisible();
          expect(
            await records.evaluate((element) => element.scrollWidth <= element.clientWidth),
          ).toBe(true);
          await expect(records.locator(".semi-tag").first()).toBeVisible();
          if (route === "keys") {
            const action = records.getByRole("button", { name: /More actions/ }).first();
            await action.focus();
            await page.keyboard.press("Enter");
            await expect(page.getByRole("menuitem", { name: "Edit key" })).toBeVisible();
            await page.keyboard.press("Escape");
            await expect(action).toBeFocused();
          }
        }
        await expectActiveNavigationVisible(page, `${theme}: mobile navigation on ${route}`);
      }
      await page.screenshot({
        path: info.outputPath(`settings-mobile-${theme}.png`),
        fullPage: true,
      });
      await page.setViewportSize({ width: 1440, height: 1000 });
    }
    await chooseTheme(page, "system");
    for (const scheme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      expect(
        await page.locator("html").evaluate((element) => getComputedStyle(element).colorScheme),
      ).toBe(scheme);
    }
    await chooseTheme(page, "dark");
    await page.route("**/*.js", async (route) => {
      if (route.request().url().endsWith("/assets/theme-init.js")) return route.continue();
      await route.abort();
    });
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect(page.locator("body")).toHaveAttribute("theme-mode", "dark");
    expect(await page.locator("#root").textContent()).toBe("");
    await page.unroute("**/*.js");
    await page.reload();
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await expect(page.locator(".state-loading")).toHaveCount(0);
    for (const route of [
      "research",
      "research/job_unknown",
      "routing",
      "usage",
      "connections",
      "audit",
    ]) {
      const response = await page.request.get(`${origin}/app/${route}`, {
        headers: { Accept: "text/html" },
      });
      expect(response.status()).toBe(404);
      expect(response.headers()["content-type"]).toContain("application/json");
    }
    expect(
      await page.evaluate(() =>
        navigator.serviceWorker.getRegistrations().then((entries) => entries.length),
      ),
    ).toBe(0);
    expect(errors).toEqual([]);
    expect(external).toEqual([]);
    await page.getByRole("button", { name: "Preferences", exact: true }).click();
    await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
    await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeVisible();
    expect((await page.request.get(`${origin}/v1/admin/session`)).status()).toBe(401);
    await info.attach("binary-identity", {
      body: JSON.stringify({
        sha256: createHash("sha256").update(readFileSync(binary)).digest("hex"),
        platform: process.platform,
        arch: process.arch,
      }),
      contentType: "application/json",
    });
  } finally {
    server.kill();
    await joined;
    execFileSync(go, ["test", "./tools/release", "-run", "^TestBrowserFixture$", "-count=1"], {
      cwd: root,
      env: { ...environment, EXTA_BROWSER_PHASE: "verify" },
      windowsHide: true,
      timeout: 60_000,
    });
  }
});
