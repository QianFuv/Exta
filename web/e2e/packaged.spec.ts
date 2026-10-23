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

test("standalone four-area administration and persistent appearance", async ({
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
    account: string;
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
    await page.getByRole("button").filter({ hasText: "EN" }).click();
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
      "keys?view=access",
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
      expect(response?.status()).toBe(200);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    }
    const session = await (await page.request.get(`${origin}/v1/admin/session`)).json();
    expect(session.principal_id).toBe(fixture.owner);
    const denied = await page.request.post(`${origin}/v1/admin/research`, {
      data: fixture.body,
      headers: { Origin: origin, "Idempotency-Key": "browser-csrf-rejection" },
    });
    expect(denied.status()).toBe(403);
    await page.goto(`${origin}/app/keys`);
    const moreActions = page.locator(".key-secondary-actions summary").first();
    await expect(page.getByRole("button", { name: "Retire", exact: true })).not.toBeVisible();
    for (let attempt = 0; attempt < 2; attempt += 1) {
      await moreActions.focus();
      await page.keyboard.press("Enter");
      await page.getByRole("button", { name: "Retire", exact: true }).click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await page.getByRole("dialog").getByRole("button", { name: "Cancel", exact: true }).click();
      await expect(moreActions).toBeFocused();
      await expect(page.getByRole("button", { name: "Retire", exact: true })).not.toBeVisible();
    }
    await page.getByRole("button", { name: "Edit account", exact: true }).click();
    const accounts = await (await page.request.get(`${origin}/v1/admin/accounts`)).json();
    const account = accounts.items.find((entry: { id: string }) => entry.id === fixture.account);
    const concurrent = await page.request.patch(`${origin}/v1/admin/accounts/${fixture.account}`, {
      data: { expected_version: account.version, label: "Concurrently updated fixture" },
      headers: { Origin: origin, "X-Exta-CSRF": session.csrf_token },
    });
    expect(concurrent.status()).toBe(200);
    await page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }),
    ).toBeDisabled();
    await page.getByRole("button", { name: "Reload and review latest", exact: true }).click();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Concurrently updated fixture", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Edit account", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByLabel("Label", { exact: true })
      .fill("Reviewed fixture account");
    await page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Reviewed fixture account", exact: true }),
    ).toBeVisible();
    await page.goto(`${origin}/app/keys?view=access`);
    await page.getByRole("button", { name: "Create token", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByLabel("Label", { exact: true })
      .fill("Browser acceptance token");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Create token", exact: true })
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
      await page.getByRole("combobox", { name: "Appearance", exact: true }).selectOption(theme);
      await page.emulateMedia({ colorScheme: theme === "light" ? "dark" : "light" });
      for (const route of [
        "overview",
        "keys",
        "keys?view=access",
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
        await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
        expect(
          await page.locator("html").evaluate((element) => getComputedStyle(element).colorScheme),
        ).toBe(theme);
        expect.soft((await audit(page)).violations, `${theme}: ${route}`).toEqual([]);
      }
      await page.goto(`${origin}/app/requests`);
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
      for (const route of ["overview", "keys", "requests", "settings"]) {
        await page.goto(`${origin}/app/${route}`);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
          true,
        );
      }
      await page.screenshot({
        path: info.outputPath(`settings-mobile-${theme}.png`),
        fullPage: true,
      });
      await page.setViewportSize({ width: 1440, height: 1000 });
    }
    await page.getByRole("combobox", { name: "Appearance", exact: true }).selectOption("system");
    for (const scheme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      expect(
        await page.locator("html").evaluate((element) => getComputedStyle(element).colorScheme),
      ).toBe(scheme);
    }
    await page.getByRole("combobox", { name: "Appearance", exact: true }).selectOption("dark");
    await page.route("**/*.js", async (route) => {
      if (route.request().url().endsWith("/assets/theme-init.js")) return route.continue();
      await route.abort();
    });
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await page.locator("#root").textContent()).toBe("");
    await page.unroute("**/*.js");
    await page.reload();
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
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
    await page.locator(".session-menu summary").click();
    await page.getByRole("button", { name: "Sign out", exact: true }).click();
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
