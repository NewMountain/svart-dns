import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { Locator, Page } from "playwright-core";
import { captureName } from "../capture-name.mjs";
import * as schema from "../../frontend/src/api/generated";
import {
  App,
  base,
  dns,
  output,
  password,
  sql,
  until,
  username,
} from "./world";

export async function request(
  page: Page,
  method: string,
  path: string,
  data?: unknown,
): Promise<unknown> {
  const response = await page.request.fetch(base + path, {
    method,
    data,
    headers: { Origin: base },
  });
  const body: unknown = await response.json();
  assert.ok(response.ok(), `${method} ${path}: ${JSON.stringify(body)}`);
  assert.ok(body !== null && typeof body === "object" && "data" in body);
  return body.data;
}
export async function capture(
  page: Page,
  requestedName: string,
): Promise<void> {
  const name = await captureName(output, requestedName);
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
  await page.screenshot({ path: join(output, `${name}.png`), fullPage: true });
  if (new URL(page.url()).pathname === "/config") {
    const bounds = await page.evaluate(() => ({
      viewport: innerWidth,
      elements: Array.from(
        document.querySelectorAll(
          ".config-card, .config-card input, .config-card select",
        ),
      ).flatMap((element) => {
        const rect = element.getBoundingClientRect();
        const style = getComputedStyle(element);
        if (
          rect.width === 0 ||
          rect.height === 0 ||
          style.display === "none" ||
          style.visibility === "hidden" ||
          style.opacity === "0"
        )
          return [];
        return [
          {
            tag: element.tagName,
            type: element.getAttribute("type"),
            left: rect.left,
            right: rect.right,
            width: rect.width,
            clientWidth: element.clientWidth,
            scrollWidth: element.scrollWidth,
            card: element.classList.contains("config-card"),
          },
        ];
      }),
    }));
    await writeFile(
      join(output, name + ".bounds.json"),
      JSON.stringify(bounds, null, 2),
    );
    for (const element of bounds.elements) {
      assert.ok(
        element.left >= 0 && element.right <= bounds.viewport + 1,
        `${name}: Config field/card clipped: ${JSON.stringify(element)}`,
      );
      if (element.card)
        assert.ok(
          element.scrollWidth <= element.clientWidth + 1,
          `${name}: Config card descendants overflow: ${JSON.stringify(element)}`,
        );
    }
  }

  if (new URL(page.url()).pathname === "/assignments") {
    const bounds = await page.evaluate(() => ({
      viewport: innerWidth,
      elements: Array.from(
        document.querySelectorAll(
          ".tiers-page .list-pane, .tiers-page .detail-pane, .tiers-page input, .tiers-page select, .tiers-page button",
        ),
      ).flatMap((element) => {
        // Activity tables have their own intentional horizontal scrolling.
        if (element.closest(".log-table")) return [];
        const rect = element.getBoundingClientRect();
        const style = getComputedStyle(element);
        if (
          !rect.width ||
          !rect.height ||
          style.visibility === "hidden" ||
          style.opacity === "0"
        )
          return [];
        return [
          {
            tag: element.tagName,
            className: element.className,
            left: rect.left,
            right: rect.right,
          },
        ];
      }),
    }));
    await writeFile(
      join(output, name + ".bounds.json"),
      JSON.stringify(bounds, null, 2),
    );
    for (const element of bounds.elements) {
      assert.ok(
        element.left >= 0 && element.right <= bounds.viewport + 1,
        `${name}: Assignment pane/control clipped: ${JSON.stringify(element)}`,
      );
    }
  }

  const inventory = await page.evaluate(() => ({
    url: location.href,
    text: document.body.innerText,
    width: innerWidth,
    bodyWidth: document.body.scrollWidth,
    controls: Array.from(
      document.querySelectorAll(
        'button,input,textarea,select,a,[role="button"]',
      ),
    ).map((element) => ({
      tag: element.tagName,
      text: element.textContent,
      aria: element.getAttribute("aria-label"),
      placeholder: element.getAttribute("placeholder"),
      href: element.getAttribute("href"),
      type: element.getAttribute("type"),
    })),
  }));
  assert.ok(
    inventory.bodyWidth <= inventory.width,
    `${name}: page overflows horizontally`,
  );
  await writeFile(
    join(output, `${name}.inventory.json`),
    JSON.stringify(inventory, null, 2),
  );
}
export async function onboarding(page: Page, app: App): Promise<void> {
  await page.goto(base);
  await page
    .getByLabel("Setup token", { exact: true })
    .fill("AAAA-AAAA-AAAA-AAAA-AAAA-AAAA-AAAA-AAAA");
  await page.getByLabel("Username", { exact: true }).fill(username);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Password again", { exact: true }).fill(password);
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await page
    .getByRole("alert")
    .filter({ hasText: "wrong setup token" })
    .waitFor();
  await page
    .getByLabel("Setup token", { exact: true })
    .fill(await app.setupToken());
  await capture(page, "setup-account-before");
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Use these resolvers", exact: true })
    .click();
  await page
    .getByRole("button", {
      name: "Skip — I'll set up blocking later",
      exact: true,
    })
    .click();
  await capture(page, "setup-finish");
  await page
    .getByRole("button", { name: "Open the dashboard", exact: true })
    .click();
  await page.getByRole("link", { name: "Dashboard", exact: true }).waitFor();
  // The shipped wizard presents public presets; replace them through the actual
  // Config UI before querying. --network=none prevents those presets escaping.
  await page.goto(base + "/config");
  await page
    .getByRole("button", { name: "Remove upstream", exact: true })
    .first()
    .waitFor();
  while (
    await page
      .getByRole("button", { name: "Remove upstream", exact: true })
      .count()
  ) {
    const previousCount = await page
      .getByRole("button", { name: "Remove upstream", exact: true })
      .count();
    const done = page.waitForResponse(
      (response) =>
        response.url().includes("/api/upstreams/") &&
        response.request().method() === "DELETE",
    );
    await page
      .getByRole("button", { name: "Remove upstream", exact: true })
      .first()
      .click();
    assert.ok((await done).ok());
    await until(
      async () =>
        (await page
          .getByRole("button", { name: "Remove upstream", exact: true })
          .count()) < previousCount,
      "upstream removal rendered",
    );
  }
  const upstream = page.getByPlaceholder(
    "9.9.9.9:53 or https://dns.quad9.net/dns-query",
  );
  await upstream.fill("127.0.0.1:50053");
  await upstream.press("Enter");
  await page.getByText("127.0.0.1:50053", { exact: true }).waitFor();
  await dns("onboarding.example", "NOERROR", "onboarding");
  await request(page, "POST", "/api/auth/logout");
  await page.goto(base);
  await page.getByPlaceholder("Enter username").fill(username);
  await page
    .getByPlaceholder("Enter password")
    .fill("incorrect-fixture-password");
  await page.getByRole("button", { name: "Sign In", exact: true }).click();
  await page.locator(".login-error").waitFor();
  await page.getByPlaceholder("Enter password").fill(password);
  await page.getByRole("button", { name: "Sign In", exact: true }).click();
  await page.getByRole("link", { name: "Dashboard", exact: true }).waitFor();
}
export async function configurePolicy(page: Page): Promise<number> {
  await page.goto(base + "/filter-lists");
  await page
    .getByPlaceholder(
      "Paste Blocklist URL (e.g. https://raw.githubusercontent.com/...)",
    )
    .fill("http://127.0.0.1:50800/list.txt");
  await page.getByPlaceholder("Name (optional)").fill("E2E controlled list");
  await page.getByRole("button", { name: "Import List", exact: true }).click();
  await page
    .locator(".filters-grid-row")
    .filter({ hasText: "E2E controlled list" })
    .waitFor();
  const lists = schema.parseGetApiBlocklistsData(
    await request(page, "GET", "/api/blocklists"),
  );
  const list = lists?.find((entry) => entry.alias === "E2E controlled list");
  assert.ok(list);
  await until(
    async () =>
      schema
        .parseGetApiBlocklistsData(
          await request(page, "GET", "/api/blocklists"),
        )
        ?.some((entry) => entry.id === list.id && entry.domain_count > 0) ??
      false,
    "local list download",
  );
  const range = schema.parsePostApiRangesData(
    await request(page, "POST", "/api/ranges", {
      name: "E2E network",
      cidr: "192.0.2.0/24",
    }),
  );
  await request(
    page,
    "POST",
    `/api/ranges/${String(range.id)}/blocklists/${String(list.id)}`,
  );
  await dns("allowed.example", "NOERROR", "allowed");
  await dns("blocked.example", "NXDOMAIN", "blocked");
  await dns("ads.wildcard.example", "NXDOMAIN", "wildcard");
  await dns("safe.wildcard.example", "NOERROR", "exception");
  return range.id;
}
export async function policyInteractions(
  page: Page,
  rangeID: number,
): Promise<void> {
  await page.goto(base + "/filter-lists");
  const row = page
    .locator(".filters-grid-row")
    .filter({ hasText: "E2E controlled list" });
  for (const enabled of [false, true]) {
    const changed = page.waitForResponse(
      (response) =>
        response.url().endsWith("/toggle") &&
        response.request().method() === "POST",
    );
    await row.locator(".slider").click();
    assert.ok((await changed).ok());
    await page.reload();
    await row.locator("input[type=checkbox]").waitFor({ state: "attached" });
    assert.equal(
      await row.locator("input[type=checkbox]").isChecked(),
      enabled,
    );
    await dns(
      "blocked.example",
      enabled ? "NXDOMAIN" : "NOERROR",
      `toggle-${String(enabled)}`,
    );
  }
  await page.goto(
    `${base}/assignments?tab=networks&selected=${String(rangeID)}`,
  );
  await page.getByPlaceholder("domain.com").fill("browser-rule.example");
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await page
    .getByRole("button", {
      name: "Remove browser-rule.example custom block rule",
    })
    .waitFor();
  await dns("browser-rule.example", "NXDOMAIN", "manual-rule");
  await page.reload();
  await page
    .getByRole("button", {
      name: "Remove browser-rule.example custom block rule",
    })
    .click();
  await page
    .getByRole("button", {
      name: "Remove browser-rule.example custom block rule",
    })
    .waitFor({ state: "detached" });
  await dns("browser-rule.example", "NOERROR", "manual-rule-removed");
  await capture(page, "network-rule-after");
}
export async function backup(page: Page, app: App): Promise<void> {
  await request(page, "POST", "/api/clients/192.0.2.10/block-domain", {
    domain: "manual-backup.example",
  });
  const seed = schema.parseConfigExport(
    await request(page, "GET", "/api/config/export"),
  );
  assert.ok(seed.blocklists && seed.ranges);
  seed.blocklists.push({
    alias: "Disabled preserved rules",
    url: "",
    enabled: false,
    refresh_interval: 0,
    domains: ["disabled-backup.example"],
  });
  const range = seed.ranges.find((entry) => entry.name === "E2E network");
  assert.ok(range?.blocklists);
  range.blocklists.push("Disabled preserved rules");
  await request(page, "POST", "/api/config/import", seed);
  await page.goto(base + "/config");
  await page
    .getByRole("button", { name: "Download Configuration", exact: true })
    .scrollIntoViewIfNeeded();
  await capture(page, "backup-before");
  const downloaded = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Download Configuration", exact: true })
    .click();
  await (await downloaded).saveAs(join(output, "downloaded-config.json"));
  const text = await readFile(join(output, "downloaded-config.json"), "utf8");
  assert.ok(!text.includes(password));
  const saved = schema.parseConfigExport(JSON.parse(text));
  await request(page, "PUT", "/api/settings/cache_ttl", { value: "1234" });
  await request(
    page,
    "DELETE",
    "/api/clients/192.0.2.10/block-domain/manual-backup.example",
  );
  await dns("manual-backup.example", "NOERROR", "backup-deleted");
  await page
    .getByLabel("Configuration JSON file")
    .setInputFiles(join(output, "downloaded-config.json"));
  await page
    .getByRole("button", { name: "Import Configuration", exact: true })
    .click();
  await page
    .getByRole("status")
    .filter({ hasText: "Import verified:" })
    .waitFor();
  const restored = schema.parseConfigExport(
    await request(page, "GET", "/api/config/export"),
  );
  assert.deepEqual(
    { ...restored, exported_at: "" },
    { ...saved, exported_at: "" },
  );
  await dns("manual-backup.example", "NXDOMAIN", "backup-restored");
  assert.deepEqual(
    sql(
      app.database,
      "SELECT r.name,b.alias,b.enabled FROM range_blocklists rb JOIN ip_ranges r ON r.id=rb.range_id JOIN blocklists b ON b.id=rb.blocklist_id WHERE b.alias='Disabled preserved rules'",
    ),
    [{ name: "E2E network", alias: "Disabled preserved rules", enabled: 0 }],
  );
  await page.getByLabel("Configuration JSON file").setInputFiles({
    name: "invalid.json",
    mimeType: "application/json",
    buffer: Buffer.from("{invalid"),
  });
  await page
    .getByRole("alert")
    .filter({ hasText: "Cannot read this backup:" })
    .waitFor();
  const invalid = await page.request.post(base + "/api/config/import", {
    data: {
      ...saved,
      upstreams: [{ upstream: "malformed://bad", enabled: true }],
    },
    headers: { Origin: base },
  });
  assert.equal(invalid.status(), 400);
  assert.deepEqual(
    {
      ...schema.parseConfigExport(
        await request(page, "GET", "/api/config/export"),
      ),
      exported_at: "",
    },
    { ...saved, exported_at: "" },
  );
  await capture(page, "backup-invalid-preserved");
}

export async function inventory(page: Page): Promise<void> {
  await mkdir(join(output, "inventory"), { recursive: true });
  const routes = [
    ["Dashboard", "/"],
    ["Logs", "/logs"],
    ["Filter Lists", "/filter-lists"],
    ["Assignments", "/assignments"],
    ["Rewrites", "/rewrites"],
    ["Analysis", "/analysis"],
    ["Investigation", "/investigation"],
    ["Config", "/config"],
    ["Admin", "/admin"],
  ] as const;
  for (const viewport of [
    { name: "desktop", width: 1440, height: 1100 },
    { name: "mobile", width: 390, height: 844 },
    { name: "4k", width: 3840, height: 2160 },
  ]) {
    await page.setViewportSize(viewport);
    for (const [label, path] of routes) {
      await page.getByRole("link", { name: label, exact: true }).click();
      await page.waitForURL(base + path);
      await capture(
        page,
        `inventory/${viewport.name}-${label.replaceAll(" ", "-")}`,
      );
      if (label === "Assignments") {
        for (const tab of ["Networks", "Groups", "Clients", "Bundles"]) {
          await page.goto(`${base}/assignments?tab=${tab.toLowerCase()}`);
          await page
            .locator(".tab-item.active")
            .filter({ hasText: tab })
            .waitFor();
          await capture(page, `inventory/${viewport.name}-${tab}`);
          if (await page.locator(".entity-row").count()) {
            await page.locator(".entity-row").first().click();
            await capture(page, `inventory/${viewport.name}-${tab}-detail`);
          }
          if (tab !== "Clients") {
            const singular =
              tab === "Networks"
                ? "network"
                : tab === "Groups"
                  ? "group"
                  : "bundle";
            await page
              .getByRole("button", { name: `Create ${singular}`, exact: true })
              .click();
            await capture(page, `inventory/${viewport.name}-${tab}-create`);
          }
        }
      }
      if (label === "Filter Lists")
        for (const [tab, title] of [
          ["history", "History & Changelog"],
          ["details", "List Details"],
        ] as const) {
          await page.goto(`${base}/filter-lists?tab=${tab}`);
          await page
            .locator(".tab-item.active")
            .filter({ hasText: title })
            .waitFor();
          await capture(page, `inventory/${viewport.name}-filters-${tab}`);
        }
      if (label === "Analysis") {
        await page.goto(base + "/analysis?tab=sim");
        await page
          .locator(".tab-item.active")
          .filter({ hasText: "Policy Simulator" })
          .waitFor();
        await capture(page, `inventory/${viewport.name}-simulator`);
      }
      if (label === "Admin")
        for (const name of [
          "New User",
          "New Token",
          "Confirm Pair",
          "Manual",
        ]) {
          await page.getByRole("button", { name, exact: true }).click();
          await page
            .getByRole("button", { name: "Cancel", exact: true })
            .waitFor();
          await page
            .locator(".create-panel")
            .filter({
              has: page.getByRole("button", { name: "Cancel", exact: true }),
            })
            .scrollIntoViewIfNeeded();
          await capture(
            page,
            `inventory/${viewport.name}-admin-${name.replaceAll(" ", "-")}`,
          );
          await page.goto(base + "/admin");
        }
    }
  }
  for (const [old, target] of [
    ["/filters", "/filter-lists"],
    ["/tiers?tab=policies&search=E2E", "/assignments?tab=policies&search=E2E"],
    ["/tiers?tab=ranges&selected=1", "/assignments?tab=ranges&selected=1"],
  ] as const) {
    await page.goto(base + old);
    await page.waitForURL(base + target);
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
}

export async function clickMutation(
  page: Page,
  control: Locator,
  method: string,
  path: string,
): Promise<unknown> {
  const pending = page.waitForResponse(
    (response) =>
      response.request().method() === method &&
      new URL(response.url()).pathname.startsWith(path),
  );
  await control.click();
  const response = await pending;
  const body: unknown = await response.json();
  assert.ok(response.ok(), JSON.stringify(body));
  assert.ok(body && typeof body === "object" && "data" in body);
  return body.data;
}
