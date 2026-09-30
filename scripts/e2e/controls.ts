import { listControls } from "./list-controls";
import { configScenarios } from "./config-controls";
import assert from "node:assert/strict";
import type { Browser, Page } from "playwright-core";
import * as schema from "../../frontend/src/api/generated";
import { capture, clickMutation, request } from "./browser";
import { App, base, dns, password, until, username } from "./world";

async function adminControls(
  browser: Browser,
  page: Page,
  suffix: string,
): Promise<void> {
  await page.goto(base + "/admin");
  const name = "control-user-" + suffix;
  const card = page
    .locator(".admin-card")
    .filter({ hasText: "User Management" });
  await card.getByRole("button", { name: "New User", exact: true }).click();
  await card.getByPlaceholder("e.g. sam").fill(name);
  await card.getByPlaceholder("Enter password", { exact: true }).fill(password);
  await card.locator(".create-panel select").selectOption("readonly");
  await capture(page, suffix + "-user-create-before");
  await clickMutation(
    page,
    card.getByRole("button", { name: "Create", exact: true }),
    "POST",
    "/api/users",
  );
  const row = card.getByRole("row").filter({ hasText: name });
  await row.getByText("Read Only", { exact: true }).waitFor();
  const other = await browser.newContext({ ignoreHTTPSErrors: true });
  try {
    const login = await other.request.post(base + "/api/auth/login", {
      data: { username: name, password },
      headers: { Origin: base },
    });
    assert.ok(login.ok(), await login.text());
    const denied = await other.request.post(base + "/api/groups", {
      data: { name: "Must not create" },
      headers: { Origin: base },
    });
    assert.equal(denied.status(), 403);
  } finally {
    await other.close();
  }
  await row.getByRole("button", { name: "Edit user", exact: true }).click();
  await row.locator("select").selectOption("admin");
  await row
    .getByPlaceholder("New password (optional)")
    .fill(password + "-rotated");
  await clickMutation(
    page,
    row.getByRole("button", { name: "Save", exact: true }),
    "PUT",
    "/api/users/",
  );
  await row.getByText("Admin", { exact: true }).waitFor();
  const users = schema.parseGetApiUsersData(
    await request(page, "GET", "/api/users"),
  );
  assert.equal(users?.find((user) => user.username === name)?.role, "admin");
  const rotated = await browser.newContext({ ignoreHTTPSErrors: true });
  try {
    const old = await rotated.request.post(base + "/api/auth/login", {
      data: { username: name, password },
      headers: { Origin: base },
    });
    assert.equal(old.status(), 401);
    const current = await rotated.request.post(base + "/api/auth/login", {
      data: { username: name, password: password + "-rotated" },
      headers: { Origin: base },
    });
    assert.ok(current.ok());
  } finally {
    await rotated.close();
  }
  await capture(page, suffix + "-user-updated");
  await row.getByRole("button", { name: "Delete user", exact: true }).click();
  await row.getByRole("button", { name: "No", exact: true }).click();
  await row.getByRole("button", { name: "Delete user", exact: true }).click();
  await clickMutation(
    page,
    row.getByRole("button", { name: "Yes", exact: true }),
    "DELETE",
    "/api/users/",
  );
  await row.waitFor({ state: "detached" });

  const tokens = page.locator(".admin-card").filter({ hasText: "API Tokens" });
  const tokenName = "control-token-" + suffix;
  await tokens.getByRole("button", { name: "New Token", exact: true }).click();
  await tokens.getByPlaceholder("e.g. Grafana Dashboard").fill(tokenName);
  await tokens.locator(".create-panel select").selectOption("readonly");
  const token = schema.parsePostApiTokensData(
    await clickMutation(
      page,
      tokens.getByRole("button", { name: "Generate", exact: true }),
      "POST",
      "/api/tokens",
    ),
  );
  const tokenClient = await browser.newContext({
    ignoreHTTPSErrors: true,
    extraHTTPHeaders: { "X-Api-Key": token.token },
  });
  try {
    const read = await tokenClient.request.get(base + "/api/settings");
    assert.ok(read.ok());
    const denied = await tokenClient.request.post(base + "/api/groups", {
      data: { name: "Must not create" },
      headers: { Origin: base },
    });
    assert.equal(denied.status(), 403);
    await page
      .context()
      .grantPermissions(["clipboard-read", "clipboard-write"], {
        origin: base,
      });
    await page.getByRole("button", { name: "Copy", exact: true }).click();
    assert.equal(
      await page.evaluate(() => navigator.clipboard.readText()),
      token.token,
    );
    await page
      .getByRole("button", { name: "I have copied it", exact: true })
      .click();
    const tokenRow = tokens.getByRole("row").filter({ hasText: tokenName });
    await tokenRow
      .getByRole("button", { name: "Revoke token", exact: true })
      .click();
    await tokenRow.getByRole("button", { name: "No", exact: true }).click();
    await tokenRow
      .getByRole("button", { name: "Revoke token", exact: true })
      .click();
    await clickMutation(
      page,
      tokenRow.getByRole("button", { name: "Yes", exact: true }),
      "DELETE",
      "/api/tokens/",
    );
    await tokenRow.waitFor({ state: "detached" });
    assert.equal(
      (await tokenClient.request.get(base + "/api/settings")).status(),
      401,
    );
  } finally {
    await tokenClient.close();
  }
  await capture(page, suffix + "-admin-deleted-revoked");
}

async function analysisControls(page: Page, suffix: string): Promise<void> {
  await page.goto(base + "/analysis?tab=sim");
  await page.getByPlaceholder("Search clients...").fill("192.0.2.10");
  await page
    .locator(".client-search-item")
    .filter({ hasText: "192.0.2.10" })
    .click();
  for (const name of [
    "Top Blocked",
    "Recent Queries",
    "Top 250 Local",
    "Your Top 250",
    "Load Domains",
  ]) {
    await clickMutation(
      page,
      page.getByRole("button", { name, exact: true }),
      "GET",
      "/api/analysis/domains",
    );
    assert.ok((await page.locator("textarea").inputValue()).length > 0);
  }
  await page.locator("textarea").fill("blocked.example\nallowed.example");
  const result = schema.parsePostApiAnalysisSimulateData(
    await clickMutation(
      page,
      page.getByRole("button", { name: "Simulate Policy", exact: true }),
      "POST",
      "/api/analysis/simulate",
    ),
  );
  assert.deepEqual(
    result.results?.map((item) => [item?.domain, item?.result]),
    [
      ["blocked.example", "block"],
      ["allowed.example", "allow"],
    ],
  );
  await capture(page, suffix + "-simulator-results");
  await page
    .getByRole("button", { name: "Blocklist Matrix", exact: true })
    .click();
  const matrix = schema.parsePostApiAnalysisMatrixData(
    await clickMutation(
      page,
      page.getByRole("button", { name: "Run Matrix", exact: true }),
      "POST",
      "/api/analysis/matrix",
    ),
  );
  assert.ok(matrix);
  await capture(page, suffix + "-matrix-results");
}

async function rewriteControls(page: Page, suffix: string): Promise<void> {
  await page.goto(base + "/rewrites");
  const domain = "rewrite-control-" + suffix + ".example";
  await page.getByPlaceholder("app.local").fill(domain);
  await page.getByPlaceholder("192.168.1.100").fill("203.0.113.99");
  await clickMutation(
    page,
    page.getByRole("button", { name: "+ Add Record", exact: true }),
    "POST",
    "/api/rewrites",
  );
  await page.getByPlaceholder("Search domains...").fill(domain);
  const row = page.locator(".rewrites-grid-row").filter({ hasText: domain });
  await row.waitFor();
  await dns(
    domain,
    "NOERROR",
    suffix + "-rewrite-created",
    50153,
    "203.0.113.99",
  );
  for (const enabled of [false, true]) {
    await clickMutation(page, row.locator("label"), "PUT", "/api/rewrites/");
    await dns(
      domain,
      "NOERROR",
      suffix + "-rewrite-enabled-" + String(enabled),
      50153,
      enabled ? "203.0.113.99" : "203.0.113.42",
    );
    assert.equal(
      schema
        .parseGetApiRewritesData(await request(page, "GET", "/api/rewrites"))
        ?.find((item) => item.domain === domain)?.enabled,
      enabled,
    );
  }
  const editingRow = page
    .locator(".rewrites-grid-row")
    .filter({ has: page.locator(".rewrite-ip input") });
  await row.getByRole("button", { name: "Edit rewrite", exact: true }).click();
  await editingRow.locator(".rewrite-ip input").fill("203.0.113.77");
  await editingRow.getByRole("button", { name: "Cancel", exact: true }).click();
  await dns(
    domain,
    "NOERROR",
    suffix + "-rewrite-cancelled",
    50153,
    "203.0.113.99",
  );
  await row.getByRole("button", { name: "Edit rewrite", exact: true }).click();
  await editingRow.locator(".rewrite-ip input").fill("203.0.113.42");
  await clickMutation(
    page,
    editingRow.getByRole("button", { name: "Save", exact: true }),
    "PUT",
    "/api/rewrites/",
  );
  await dns(domain, "NOERROR", suffix + "-rewrite-edited");
  await capture(page, suffix + "-rewrite-edited");
  await clickMutation(
    page,
    row.getByRole("button", { name: "Delete rewrite", exact: true }),
    "DELETE",
    "/api/rewrites/",
  );
  await row.waitFor({ state: "detached" });
  assert.ok(
    !schema
      .parseGetApiRewritesData(await request(page, "GET", "/api/rewrites"))
      ?.some((item) => item.domain === domain),
  );
  await dns(domain, "NOERROR", suffix + "-rewrite-deleted");
}

async function logFilters(page: Page): Promise<void> {
  await page.waitForLoadState("networkidle");
  for (const value of ["5000", "30000", "60000", "300000"])
    await page
      .getByRole("combobox", { name: "Refresh interval", exact: true })
      .selectOption(value);
  for (const name of [
    "Client",
    "Group",
    "Network",
    "Result",
    "Query type",
    "Rows per page",
  ]) {
    const select = page.getByRole("combobox", { name, exact: true });
    const original = await select.inputValue();
    const values = await select.locator("option").evaluateAll((options) =>
      options.map((option) => {
        if (!(option instanceof HTMLOptionElement))
          throw new Error("Expected option");
        return option.value;
      }),
    );
    for (const value of [
      ...values.filter((value) => value !== original),
      original,
    ]) {
      if ((await select.inputValue()) === value) continue;
      const pending = page.waitForResponse(
        (response) =>
          response.request().method() === "GET" &&
          new URL(response.url()).pathname === "/api/query-logs",
      );
      await select.selectOption(value);
      const response = await pending;
      assert.ok(response.ok());
      const body: unknown = await response.json();
      assert.ok(body && typeof body === "object" && "data" in body);
      const data = schema.parseGetApiQueryLogsData(body.data);
      if (name === "Query type" && (value === "AAAA" || value === "HTTPS"))
        assert.equal(data.total, 0);
      if (name === "Result" && value === "blocked")
        assert.ok(data.logs?.every((row) => row.blocked));
      if (name === "Result" && value === "allowed")
        assert.ok(data.logs?.every((row) => !row.blocked));
      if (name === "Client" && value)
        assert.ok(data.logs?.every((row) => row.client_ip === value));
      if (name === "Rows per page") assert.equal(data.limit, Number(value));
    }
  }
}

async function dashboardAndLogs(page: Page, suffix: string): Promise<void> {
  await page.goto(base);
  for (const range of ["5m", "1h", "24h", "7d"]) {
    await page.getByRole("button", { name: range, exact: true }).click();
    await page.locator(".time-btn.active").filter({ hasText: range }).waitFor();
  }
  await capture(page, suffix + "-dashboard-range");
  await page.goto(base + "/logs");
  await logFilters(page);
  await page
    .getByRole("textbox", { name: "Search query logs", exact: true })
    .fill("history.example.com");
  await until(
    async () =>
      (await page.locator("tbody tr").allTextContents()).every((text) =>
        text.includes("history.example.com"),
      ),
    "log search matches exact fixture",
  );
  await page
    .getByRole("button", { name: "Show policy evaluation", exact: true })
    .first()
    .click();
  await page.locator(".eval-panel").waitFor();
  await capture(page, suffix + "-logs-policy-detail");
  await page
    .getByRole("button", { name: "More actions", exact: true })
    .first()
    .click();
  await page.getByText("Copy Domain", { exact: true }).click();
  assert.equal(
    await page.evaluate(() => navigator.clipboard.readText()),
    "history.example.com.",
  );
  await page
    .getByRole("button", { name: "More actions", exact: true })
    .first()
    .click();
  await clickMutation(
    page,
    page.getByText("Block for E2E client", { exact: true }),
    "POST",
    "/api/clients/",
  );
  await dns("history.example.com", "NXDOMAIN", suffix + "-logs-block");
  await page.goto(base + "/assignments?tab=clients&selected=192.0.2.10");
  await clickMutation(
    page,
    page.getByRole("button", {
      name: /Remove history\.example\.com\.? custom block rule/,
    }),
    "DELETE",
    "/api/clients/",
  );
  await page.goto(base + "/logs");
  await page
    .getByRole("textbox", { name: "Search query logs", exact: true })
    .fill("history.example.com");
  await until(
    async () =>
      (await page.locator("tbody tr").allTextContents()).every((text) =>
        text.includes("history.example.com"),
      ),
    "log search after removing custom block",
  );
  await page
    .getByRole("button", { name: "More actions", exact: true })
    .first()
    .click();
  await clickMutation(
    page,
    page.getByText("Allow for E2E client", { exact: true }),
    "POST",
    "/api/clients/",
  );
  await dns("history.example.com", "NOERROR", suffix + "-logs-allow");
  await capture(page, suffix + "-logs-action-applied");
  await page.goto(base + "/assignments?tab=clients&selected=192.0.2.10");
  await clickMutation(
    page,
    page.getByRole("button", {
      name: /Remove history\.example\.com\.? custom allow rule/,
    }),
    "DELETE",
    "/api/clients/",
  );
}

async function assignmentControls(page: Page, suffix: string): Promise<void> {
  const entities = [
    ["bundles", "bundle", "policies"],
    ["groups", "group", "groups"],
    ["networks", "network", "ranges"],
  ] as const;
  let bundleId = 0;
  for (const [tab, singular, endpoint] of entities) {
    const name = "Control " + singular + " " + suffix;
    await page.goto(base + "/assignments?tab=" + tab);
    await page
      .getByRole("button", { name: "Create " + singular, exact: true })
      .click();
    await page.getByPlaceholder("Name", { exact: true }).fill(name);
    if (singular === "network")
      await page
        .getByPlaceholder("CIDR (e.g. 10.0.0.0/24)")
        .fill("198.51.100.0/24");
    const created = await clickMutation(
      page,
      page.getByRole("button", { name: "Create", exact: true }),
      "POST",
      "/api/" + endpoint,
    );
    assert.ok(
      created &&
        typeof created === "object" &&
        "id" in created &&
        typeof created.id === "number",
    );
    const id = created.id;
    if (singular === "bundle") bundleId = id;
    await page.locator(".entity-row").filter({ hasText: name }).click();
    const rules = page
      .locator(".widget")
      .filter({ has: page.getByText("Custom Rules", { exact: true }) });
    const domain = singular + "-control-" + suffix + ".example";
    await rules.getByPlaceholder("domain.com").fill(domain);
    await rules.locator("select").selectOption("block");
    await clickMutation(
      page,
      rules.getByRole("button", { name: "Add", exact: true }),
      "POST",
      "/api/" + endpoint + "/",
    );
    await rules
      .getByRole("button", {
        name: "Remove " + domain + " custom block rule",
        exact: true,
      })
      .waitFor();
    if (singular === "group") {
      const members = page
        .locator(".widget")
        .filter({ has: page.getByText("Members", { exact: true }) });
      await members.locator("select").selectOption("192.0.2.10");
      await clickMutation(
        page,
        members.getByRole("button", { name: "Add", exact: true }),
        "POST",
        "/api/groups/",
      );
      await members
        .locator(".chip")
        .filter({ hasText: "E2E client" })
        .waitFor();
      await dns(domain, "NXDOMAIN", suffix + "-group-block");
      await page.locator(".policy-selector").selectOption(String(bundleId));
      await until(
        async () =>
          schema.parseGetApiGroupsIdData(
            await request(page, "GET", "/api/groups/" + String(id)),
          ).policy?.id === bundleId,
        "bundle assigned to group",
      );
      await dns(
        "bundle-control-" + suffix + ".example",
        "NXDOMAIN",
        suffix + "-bundle-block",
      );
      await page.locator(".policy-selector").selectOption("");
      await until(
        async () =>
          !schema.parseGetApiGroupsIdData(
            await request(page, "GET", "/api/groups/" + String(id)),
          ).policy,
        "bundle removed from group",
      );
      await dns(
        "bundle-control-" + suffix + ".example",
        "NOERROR",
        suffix + "-bundle-removed",
      );
      await clickMutation(
        page,
        members
          .locator(".chip")
          .filter({ hasText: "E2E client" })
          .getByRole("button", { name: "x", exact: true }),
        "DELETE",
        "/api/groups/",
      );
      await members.getByText("No members", { exact: true }).waitFor();
      await dns(domain, "NOERROR", suffix + "-group-member-removed");
    }
    await capture(page, suffix + "-" + singular + "-mutated");
    if (singular === "bundle") continue;
    await clickMutation(
      page,
      rules.getByRole("button", {
        name: "Remove " + domain + " custom block rule",
        exact: true,
      }),
      "DELETE",
      "/api/" + endpoint + "/",
    );
    await page.getByRole("button", { name: "Delete", exact: true }).click();
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await page.getByRole("button", { name: "Delete", exact: true }).click();
    await clickMutation(
      page,
      page.getByRole("button", { name: "Confirm", exact: true }),
      "DELETE",
      "/api/" + endpoint + "/",
    );
    await page
      .locator(".entity-row")
      .filter({ hasText: name })
      .waitFor({ state: "detached" });
  }
  await page.goto(
    base + "/assignments?tab=bundles&selected=" + String(bundleId),
  );
  await page
    .locator(".entity-row")
    .filter({ hasText: "Control bundle " + suffix })
    .click();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await clickMutation(
    page,
    page.getByRole("button", { name: "Confirm", exact: true }),
    "DELETE",
    "/api/policies/",
  );
  await page
    .locator(".entity-row")
    .filter({ hasText: "Control bundle " + suffix })
    .waitFor({ state: "detached" });
}

async function pairingControls(
  browser: Browser,
  page: Page,
  primary: App,
  peer: App,
): Promise<void> {
  // Self URLs must be reachable inside the deliberately loopback-only world.
  // Existing originals are preserved; no DNS is generated with this identity.
  await primary.stop();
  await peer.stop();
  primary.externalIP = "127.0.0.1";
  peer.externalIP = "127.0.0.1";
  await primary.start();
  await peer.start();
  const remoteBase = "https://127.0.0.1:" + String(peer.adminPort);
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  try {
    const login = await context.request.post(remoteBase + "/api/auth/login", {
      data: { username, password },
      headers: { Origin: remoteBase },
    });
    assert.ok(login.ok());
    const remote = await context.newPage();
    for (const viewport of [
      { name: "desktop", width: 1440, height: 1100 },
      { name: "mobile", width: 390, height: 844 },
    ]) {
      await page.setViewportSize(viewport);
      await remote.setViewportSize(viewport);
      await page.goto(base + "/admin");
      const pairing = schema.parsePostApiPeersPairData(
        await clickMutation(
          page,
          page.getByRole("button", { name: "Pair", exact: true }),
          "POST",
          "/api/peers/pair",
        ),
      );
      assert.equal(pairing.self_url, base);
      await capture(page, viewport.name + "-pair-code-created");
      await remote.goto(remoteBase + "/admin");
      await remote
        .getByRole("button", { name: "Confirm Pair", exact: true })
        .click();
      await remote.getByPlaceholder("https://192.168.1.3:3000").fill(base);
      await remote
        .getByPlaceholder("Paste pairing code")
        .fill("wrong-fixture-pairing-code");
      await remote
        .getByRole("button", { name: "Confirm", exact: true })
        .click();
      await remote.getByText(/Pairing failed:/).waitFor();
      await remote
        .getByPlaceholder("Paste pairing code")
        .fill(pairing.pairing_code);
      await clickMutation(
        remote,
        remote.getByRole("button", { name: "Confirm", exact: true }),
        "POST",
        "/api/peers/confirm",
      );
      await remote
        .getByPlaceholder("Paste pairing code")
        .waitFor({ state: "detached" });
      await page.reload();
      const peerRow = page
        .getByRole("row")
        .filter({ hasText: "127.0.0.1:50301" });
      await peerRow.waitFor();
      await capture(page, viewport.name + "-pair-complete");
      await peerRow
        .getByRole("button", { name: "Remove peer", exact: true })
        .click();
      await clickMutation(
        page,
        peerRow.getByRole("button", { name: "Yes", exact: true }),
        "DELETE",
        "/api/peers/",
      );
      await peerRow.waitFor({ state: "detached" });
      const removed = await context.request.delete(
        remoteBase + "/api/peers/" + encodeURIComponent(base),
        { headers: { Origin: remoteBase } },
      );
      assert.ok(removed.ok());
      await page.getByRole("button", { name: "Manual", exact: true }).click();
      await page.getByPlaceholder("https://192.0.2.10:443").fill(remoteBase);
      await clickMutation(
        page,
        page.getByRole("button", { name: "Add", exact: true }),
        "POST",
        "/api/peers",
      );
      await peerRow.waitFor();
      await peerRow
        .getByRole("button", { name: "Remove peer", exact: true })
        .click();
      await clickMutation(
        page,
        peerRow.getByRole("button", { name: "Yes", exact: true }),
        "DELETE",
        "/api/peers/",
      );
      await peerRow.waitFor({ state: "detached" });
    }
  } finally {
    await context.close();
  }
}

export async function controls(
  browser: Browser,
  page: Page,
  primary: App,
  peer: App,
): Promise<void> {
  for (const viewport of [
    { name: "desktop", width: 1440, height: 1100 },
    { name: "mobile", width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await adminControls(browser, page, viewport.name);
    await configScenarios(page, viewport.name);
    await listControls(page, viewport.name);
    await analysisControls(page, viewport.name);
    await assignmentControls(page, viewport.name);
    await rewriteControls(page, viewport.name);
    await dashboardAndLogs(page, viewport.name);
  }
  await pairingControls(browser, page, primary, peer);
}
