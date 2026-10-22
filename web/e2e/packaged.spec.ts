/** Exercise real cookies, embedded chunks and durable recovery without provider traffic. */
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { spawn, execFileSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import net from "node:net";
import { canonicalJson } from "../src/lib/researchDraft";

/** Bridge the locked axe peer's newer Page type; axe uses APIs present in Playwright 1.63. */
function audit(page: Page) {
  return new AxeBuilder({
    page: page as unknown as ConstructorParameters<typeof AxeBuilder>[0]["page"],
  }).analyze();
}

test("standalone management and durable Research recovery", async ({ page, context }, info) => {
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
    await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
    const cookies = await context.cookies();
    expect(cookies.some((cookie) => cookie.httpOnly)).toBe(true);
    expect(await page.evaluate(() => document.cookie)).not.toContain("exta_session");
    for (const route of [
      "keys",
      "routing",
      "usage",
      "requests",
      "research",
      "connections",
      "settings",
      "audit",
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
    await page.goto(`${origin}/app/connections`);
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
    await page.evaluate(
      (pending) =>
        sessionStorage.setItem(
          `exta.research.pendingSubmission.${pending.principalId}`,
          JSON.stringify(pending),
        ),
      {
        version: 2,
        principalId: fixture.owner,
        attemptId: randomUUID(),
        hasUnresolvedAttempt: true,
        idempotencyKey: fixture.key,
        body: fixture.body,
        fingerprint: canonicalJson(fixture.body),
        state: "uncertain",
        updatedAt: new Date().toISOString(),
      },
    );
    await page.goto(`${origin}/app/research`);
    let forwarded = false;
    await page.route(`${origin}/v1/admin/research`, async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      expect(route.request().headers()["idempotency-key"]).toBe(fixture.key);
      const response = await route.fetch();
      expect(response.status()).toBe(202);
      expect((await response.json()).id).toBe(fixture.jobs.queued);
      forwarded = true;
      await route.abort();
    });
    await page.getByRole("button", { name: "Retry with same key", exact: true }).click();
    await expect.poll(() => forwarded).toBe(true);
    await expect
      .poll(
        async () =>
          JSON.parse(
            (await page.evaluate(
              (owner) => sessionStorage.getItem(`exta.research.pendingSubmission.${owner}`),
              fixture.owner,
            ))!,
          ).state,
      )
      .toBe("uncertain");
    await page.unroute(`${origin}/v1/admin/research`);
    await page.reload();
    await page.getByRole("button", { name: "Retry with same key", exact: true }).click();
    await expect(page).toHaveURL(`${origin}/app/research/${fixture.jobs.queued}`);
    await page.getByRole("button", { name: "Stop observing", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Stop observing", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (
            await (
              await page.request.get(`${origin}/v1/admin/research/${fixture.jobs.queued}`)
            ).json()
          ).job.state,
      )
      .toBe("cancelled");
    const cancelled = await (
      await page.request.get(`${origin}/v1/admin/research/${fixture.jobs.queued}`)
    ).json();
    expect(cancelled.job.active_slot_held).toBe(false);
    expect(cancelled.job.budget_hold_state).toBe("released_unspent");
    await page.goto(`${origin}/app/research/${fixture.jobs.unknown}`);
    for (const action of ["Write off budget (risk)", "Override capacity release (risk)"]) {
      await page.getByRole("button", { name: "Reconcile…", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await dialog.getByRole("radio", { name: action, exact: true }).check();
      await dialog
        .getByLabel("Reason", { exact: true })
        .fill("Synthetic offline acceptance evidence");
      await dialog.getByLabel("Evidence reference", { exact: true }).fill("browser-fixture");
      await dialog.getByRole("checkbox").check();
      await dialog.getByRole("button", { name: "Record reconciliation", exact: true }).click();
      await expect(dialog).not.toBeVisible();
      const job = await (
        await page.request.get(`${origin}/v1/admin/research/${fixture.jobs.unknown}`)
      ).json();
      expect(job.job.budget_hold_state).toBe("written_off");
      expect(job.job.active_slot_held).toBe(action === "Write off budget (risk)");
    }
    await page.goto(`${origin}/app/research/${fixture.jobs.report}`);
    await expect(page.getByRole("heading", { name: "Synthetic report" })).toBeVisible();
    await expect(page.getByRole("cell", { name: "Rendered", exact: true })).toBeVisible();
    expect(await page.evaluate(() => "extaUnsafe" in window)).toBe(false);
    await expect(page.locator('img[src*="invalid.example"], a[href^="javascript:"]')).toHaveCount(
      0,
    );
    expect((await audit(page)).violations).toEqual([]);
    await page.screenshot({ path: info.outputPath("report-desktop.png"), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: info.outputPath("report-mobile.png"), fullPage: true });
    expect(
      await page.evaluate(() =>
        navigator.serviceWorker.getRegistrations().then((entries) => entries.length),
      ),
    ).toBe(0);
    expect(errors).toEqual([]);
    expect(external).toEqual([]);
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
