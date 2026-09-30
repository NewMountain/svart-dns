import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { Page } from "playwright-core";
import * as schema from "../../frontend/src/api/generated";
import { capture, clickMutation, request } from "./browser";
import { base, dns, output, until } from "./world";

async function configRefreshRace(page: Page, suffix: string): Promise<void> {
  await page.goto(base + "/config");
  await page.waitForLoadState("networkidle");
  const strategy = page
    .locator(".form-group")
    .filter({ has: page.getByText("Upstream Strategy", { exact: true }) })
    .locator("select");
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let fetched = false;
  let held = false;
  await page.route("**/api/settings", async (route) => {
    if (route.request().method() !== "GET" || held) {
      await route.continue();
      return;
    }
    held = true;
    const response = await route.fetch();
    fetched = true;
    await gate;
    await route.fulfill({ response });
  });
  try {
    await strategy.selectOption("blended");
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    await until(() => fetched, "held real settings response");
    await strategy.selectOption("random");
    const before = await strategy.inputValue();
    const enabled = await page
      .getByRole("button", { name: "Saving...", exact: true })
      .isEnabled();
    await page.screenshot({
      path: join(output, suffix + "-draft-before-refresh.png"),
      fullPage: true,
    });
    const completedRead = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/settings",
    );
    release();
    await (await completedRead).finished();
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .waitFor();
    await page.waitForLoadState("networkidle");
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() =>
            requestAnimationFrame(() => {
              resolve();
            }),
          ),
        ),
    );
    const after = await strategy.inputValue();
    const savedRead = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/settings",
    );
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    await (await savedRead).finished();
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .waitFor();
    await page.waitForLoadState("networkidle");
    const saved = schema.parseGetApiSettingsData(
      await request(page, "GET", "/api/settings"),
    );
    await strategy.scrollIntoViewIfNeeded();
    await capture(page, suffix + "-draft-after-save");
    await writeFile(
      join(output, suffix + "-config-refresh-race.json"),
      JSON.stringify(
        { before, enabled, after, persisted: saved?.["strategy"] },
        null,
        2,
      ),
    );
    assert.equal(
      enabled,
      false,
      "save remains busy until its readback completes",
    );
    assert.equal(before, "random");
    assert.equal(after, "random");
    assert.equal(saved?.["strategy"], "random");
  } finally {
    release();
    await page.unrouteAll({ behavior: "wait" });
  }
}

async function configPendingSave(page: Page, suffix: string): Promise<void> {
  await page.goto(base + "/config");
  await page.waitForLoadState("networkidle");
  const ttl = page
    .locator(".form-group")
    .filter({ has: page.getByText("Cache TTL (Seconds)", { exact: true }) })
    .locator("input");
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let fetched = false;
  let held = false;
  await page.route("**/api/settings/cache_ttl", async (route) => {
    if (route.request().method() !== "PUT" || held) {
      await route.continue();
      return;
    }
    held = true;
    const response = await route.fetch();
    fetched = true;
    await gate;
    await route.fulfill({ response });
  });
  try {
    await ttl.fill("600");
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    await until(() => fetched, "held real settings response");
    await ttl.fill("900");
    await page.screenshot({
      path: join(output, suffix + "-draft-during-put.png"),
      fullPage: true,
    });
    release();
    await page.getByText("Submitted changes saved", { exact: true }).waitFor();
    await page.waitForLoadState("networkidle");
    assert.equal(await ttl.inputValue(), "900");
    assert.equal(
      schema.parseGetApiSettingsData(
        await request(page, "GET", "/api/settings"),
      )?.["cache_ttl"],
      "600",
    );
    await capture(page, suffix + "-newer-draft-unsaved");
    const refreshed = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/settings",
    );
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    await (await refreshed).finished();
    assert.equal(
      schema.parseGetApiSettingsData(
        await request(page, "GET", "/api/settings"),
      )?.["cache_ttl"],
      "900",
    );
  } finally {
    release();
    await page.unrouteAll({ behavior: "wait" });
  }
}

async function configConfirmedRead(page: Page, suffix: string): Promise<void> {
  await page.goto(base + "/config");
  await page.waitForLoadState("networkidle");
  const ttl = page
    .locator(".form-group")
    .filter({ has: page.getByText("Cache TTL (Seconds)", { exact: true }) })
    .locator("input");
  let reads = 0;
  await page.route("**/api/settings", async (route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }
    reads += 1;
    if (reads === 1) {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({
        data: null,
        error: "Follow-up read unavailable",
        error_code: "unavailable",
      }),
    });
  });
  try {
    await ttl.fill("777");
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    await page.getByText("Submitted changes saved", { exact: true }).waitFor();
    await page.waitForLoadState("networkidle");
    const displayed = await ttl.inputValue();
    await ttl.scrollIntoViewIfNeeded();
    await capture(page, suffix + "-confirmed-read-published");
    await writeFile(
      join(output, suffix + "-confirmed-read.json"),
      JSON.stringify({ displayed, reads }, null, 2),
    );
    assert.equal(displayed, "777");
    assert.equal(
      reads,
      1,
      "save publishes its confirmed read without a redundant refresh",
    );
  } finally {
    await page.unrouteAll({ behavior: "wait" });
  }
  assert.equal(
    schema.parseGetApiSettingsData(
      await request(page, "GET", "/api/settings"),
    )?.["cache_ttl"],
    "777",
  );
}

async function configCombinedRace(page: Page, suffix: string): Promise<void> {
  await request(page, "PUT", "/api/settings/strategy", { value: "blended" });
  const strategy = page
    .locator(".form-group")
    .filter({ has: page.getByText("Upstream Strategy", { exact: true }) })
    .locator("select");
  let releaseRead: () => void = () => undefined;
  const readGate = new Promise<void>((resolve) => {
    releaseRead = resolve;
  });
  let readReady = false;
  let readHeld = false;
  let releaseWrite: () => void = () => undefined;
  const writeGate = new Promise<void>((resolve) => {
    releaseWrite = resolve;
  });
  let writeReady = false;
  await page.route("**/api/settings", async (route) => {
    if (route.request().method() !== "GET" || readHeld) {
      await route.continue();
      return;
    }
    readHeld = true;
    const response = await route.fetch();
    readReady = true;
    await readGate;
    await route.fulfill({ response });
  });
  await page.route("**/api/settings/strategy", async (route) => {
    const body: unknown = JSON.parse(route.request().postData() ?? "null");
    if (
      route.request().method() !== "PUT" ||
      !body ||
      typeof body !== "object" ||
      !("value" in body) ||
      body.value !== "random"
    ) {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    writeReady = true;
    await writeGate;
    await route.fulfill({ response });
  });
  try {
    await page.goto(base + "/config");
    await until(() => readReady, "older real settings read held");
    await strategy.selectOption("random");
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    await until(() => writeReady, "later real settings write held");
    await strategy.selectOption("blended");
    const before = await strategy.inputValue();
    await page.screenshot({
      path: join(output, suffix + "-combined-draft-before.png"),
      fullPage: true,
    });
    const delivered = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/settings",
    );
    releaseRead();
    await (await delivered).finished();
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() =>
            requestAnimationFrame(() => {
              resolve();
            }),
          ),
        ),
    );
    releaseWrite();
    await until(
      () =>
        page
          .getByRole("button", { name: "Save Changes", exact: true })
          .isEnabled(),
      "later save completed",
    );
    await page.waitForLoadState("networkidle");
    const after = await strategy.inputValue();
    const finalWrite = page.waitForResponse(
      (response) =>
        response.request().method() === "PUT" &&
        new URL(response.url()).pathname === "/api/settings/strategy",
    );
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    assert.ok((await finalWrite).ok());
    await until(
      () =>
        page
          .getByRole("button", { name: "Save Changes", exact: true })
          .isEnabled(),
      "reverted intention saved",
    );
    const saved = schema.parseGetApiSettingsData(
      await request(page, "GET", "/api/settings"),
    );
    await writeFile(
      join(output, suffix + "-config-combined-race.json"),
      JSON.stringify(
        { before, after, persisted: saved?.["strategy"] },
        null,
        2,
      ),
    );
    await strategy.scrollIntoViewIfNeeded();
    await capture(page, suffix + "-combined-draft-after");
    assert.equal(before, "blended");
    assert.equal(after, "blended");
    assert.equal(saved?.["strategy"], "blended");
  } finally {
    releaseRead();
    releaseWrite();
    await page.unrouteAll({ behavior: "wait" });
  }
}

export async function configControls(
  page: Page,
  suffix: string,
): Promise<void> {
  await page.goto(base + "/config");
  await page.getByText("Cache TTL (Seconds)", { exact: true }).waitFor();
  const values = [
    ["Cache TTL (Seconds)", "cache_ttl", "2345"],
    ["Denied Response TTL (Seconds)", "denied_ttl", "234"],
    ["Bootstrap TTL", "bootstrap_ttl", "3456"],
    ["Log Retention (Days)", "log_retention_days", "45"],
  ] as const;
  for (const [label, , value] of values)
    await page
      .locator(".form-group")
      .filter({ has: page.getByText(label, { exact: true }) })
      .locator("input")
      .fill(value);
  await page
    .locator(".form-group")
    .filter({ has: page.getByText("Timezone", { exact: true }) })
    .locator("select")
    .selectOption("UTC");
  const strategy = page
    .locator(".form-group")
    .filter({ has: page.getByText("Upstream Strategy", { exact: true }) })
    .locator("select");
  for (const value of ["blended", "random", "weighted"]) {
    await page.waitForLoadState("networkidle");
    await strategy.selectOption(value);
    await capture(page, suffix + "-strategy-selected-" + value);
    assert.equal(await strategy.inputValue(), value);
    const refreshed = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/settings",
    );
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    await (await refreshed).finished();
    await capture(page, suffix + "-strategy-saved-" + value);
    await until(
      async () =>
        schema.parseGetApiSettingsData(
          await request(page, "GET", "/api/settings"),
        )?.["strategy"] === value,
      "saved upstream strategy",
    );
  }
  const settings = schema.parseGetApiSettingsData(
    await request(page, "GET", "/api/settings"),
  );
  assert.ok(settings);
  for (const [, key, value] of values) assert.equal(settings[key], value);
  for (const enabled of [false, true]) {
    const checkbox = page.getByRole("checkbox");
    if ((await checkbox.isChecked()) !== enabled) {
      await page
        .locator(".toggle-row")
        .filter({ hasText: "Enable Query Logging" })
        .locator("label")
        .click();
    }
    assert.equal(await checkbox.isChecked(), enabled);
    const refreshed = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/settings",
    );
    await page
      .getByRole("button", { name: "Save Changes", exact: true })
      .click();
    await (await refreshed).finished();
    await page.waitForLoadState("networkidle");
    assert.equal(
      schema.parseGetApiSettingsData(
        await request(page, "GET", "/api/settings"),
      )?.["logging_enabled"],
      String(enabled),
    );
  }
  const upstream = page
    .locator(".upstream-row")
    .filter({ hasText: "127.0.0.1:50053" });
  await clickMutation(
    page,
    upstream.getByRole("button", { name: "Disable upstream", exact: true }),
    "POST",
    "/api/upstreams/",
  );
  assert.equal(
    schema
      .parseGetApiUpstreamsData(await request(page, "GET", "/api/upstreams"))
      ?.find((item) => item.upstream === "127.0.0.1:50053")?.enabled,
    false,
  );
  await clickMutation(
    page,
    upstream.getByRole("button", { name: "Enable upstream", exact: true }),
    "POST",
    "/api/upstreams/",
  );
  assert.equal(
    schema
      .parseGetApiUpstreamsData(await request(page, "GET", "/api/upstreams"))
      ?.find((item) => item.upstream === "127.0.0.1:50053")?.enabled,
    true,
  );
  await page.getByRole("button", { name: "Clear Cache", exact: true }).click();
  await page.getByText("Cache cleared", { exact: true }).waitFor();
  await capture(page, suffix + "-config-saved-cache-cleared");
  await dns(
    "config-control-" + suffix + ".example",
    "NOERROR",
    suffix + "-config-dns",
  );
}

async function bootstrapControls(page: Page, suffix: string): Promise<void> {
  await page.goto(base + "/config");
  await page.waitForLoadState("networkidle");
  const initial =
    schema
      .parseGetApiBootstrapData(await request(page, "GET", "/api/bootstrap"))
      ?.map((item) => item.server) ?? [];
  const card = page
    .locator(".config-card")
    .filter({ has: page.getByText("Bootstrap DNS", { exact: true }) });
  const input = card.getByPlaceholder("9.9.9.9 or 1.1.1.1:53");
  const added = ["127.0.0.2:53", "127.0.0.1:50053"];
  await input.fill(" 127.0.0.2 ");
  const first = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/bootstrap",
  );
  await input.press("Enter");
  assert.ok((await first).ok());
  await card.getByText(added[0] ?? "", { exact: true }).waitFor();
  await input.fill(added[1] ?? "");
  await clickMutation(
    page,
    card.getByRole("button", { name: "Add", exact: true }),
    "POST",
    "/api/bootstrap",
  );
  await card.getByText(added[1] ?? "", { exact: true }).waitFor();
  assert.deepEqual(
    schema
      .parseGetApiBootstrapData(await request(page, "GET", "/api/bootstrap"))
      ?.map((item) => item.server),
    [...initial, ...added],
  );
  await capture(page, suffix + "-bootstrap-added");
  for (let index = 0; index < added.length; index++) {
    const address = added[index];
    assert.ok(address);
    const row = card.locator(".upstream-row").filter({ hasText: address });
    await clickMutation(
      page,
      row.getByRole("button", { name: "Remove server", exact: true }),
      "PUT",
      "/api/bootstrap",
    );
    await row.waitFor({ state: "detached" });
    assert.deepEqual(
      schema
        .parseGetApiBootstrapData(await request(page, "GET", "/api/bootstrap"))
        ?.map((item) => item.server) ?? [],
      [...initial, ...added.slice(index + 1)],
    );
  }
  await capture(page, suffix + "-bootstrap-restored");
}

export async function configScenarios(
  page: Page,
  suffix: string,
): Promise<void> {
  await configRefreshRace(page, suffix);
  await configPendingSave(page, suffix);
  await configCombinedRace(page, suffix);
  await configConfirmedRead(page, suffix);
  await configControls(page, suffix);
  await bootstrapControls(page, suffix);
}
