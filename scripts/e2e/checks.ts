import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { Browser, Page } from "playwright-core";
import * as schema from "../../frontend/src/api/generated";
import { capture, request } from "./browser";
import {
  acceptedQueries,
  rawSnapshots,
  snapshotRaw,
  binary,
  App,
  base,
  dns,
  output,
  password,
  sql,
  until,
  username,
} from "./world";

export async function transports(page: Page): Promise<void> {
  const variants = [
    ["udp", "127.0.0.1:50053"],
    ["tcp", "tcp://127.0.0.1:50053"],
    ["doh", "https://127.0.0.1:50443/dns-query"],
    ["dot", "tls://127.0.0.1:50853"],
  ] as const;
  for (const [name, upstream] of variants) {
    const current = schema.parseGetApiUpstreamsData(
      await request(page, "GET", "/api/upstreams"),
    );
    for (const item of current ?? [])
      await request(page, "DELETE", `/api/upstreams/${String(item.id)}`);
    await request(page, "POST", "/api/upstreams", { upstream, enabled: true });
    await dns(`transport-${name}.example`, "NOERROR", `transport-${name}`);
  }
  const final = schema.parseGetApiUpstreamsData(
    await request(page, "GET", "/api/upstreams"),
  );
  for (const item of final ?? [])
    await request(page, "DELETE", `/api/upstreams/${String(item.id)}`);
  await request(page, "POST", "/api/upstreams", {
    upstream: "127.0.0.1:50053",
    enabled: true,
  });
}
export async function peerSync(
  browser: Browser,
  page: Page,
  peer: App,
): Promise<void> {
  const peerBase = `https://127.0.0.1:${String(peer.adminPort)}`;
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  try {
    const setup = await context.request.post(peerBase + "/api/setup", {
      data: { token: await peer.setupToken(), username, password },
      headers: { Origin: peerBase },
    });
    assert.ok(setup.ok(), await setup.text());
    const named = await context.request.put(
      peerBase + "/api/settings/node_name",
      { data: { value: "E2E peer" }, headers: { Origin: peerBase } },
    );
    assert.ok(named.ok(), await named.text());
    await request(page, "PUT", "/api/settings/node_name", {
      value: "E2E primary",
    });
    const addPeer = await context.request.post(peerBase + "/api/peers", {
      data: { url: base },
      headers: { Origin: peerBase },
    });
    assert.ok(addPeer.ok(), await addPeer.text());
    await request(page, "POST", "/api/peers", { url: peerBase });
    await request(page, "POST", "/api/groups", {
      name: "E2E synchronized group",
    });
    await until(
      () => {
        const value = sql(
          peer.database,
          "SELECT name FROM client_groups WHERE name='E2E synchronized group'",
        );
        return (
          JSON.stringify(value) ===
          JSON.stringify([{ name: "E2E synchronized group" }])
        );
      },
      "real peer configuration replication",
      30000,
    );
    await until(
      async () =>
        schema.parseGetApiPeersData(await request(page, "GET", "/api/peers"))
          .sync_tls_ready === true,
      "reciprocal verified TLS peer sync",
      30000,
    );
    const peers = schema.parseGetApiPeersData(
      await request(page, "GET", "/api/peers"),
    );
    assert.equal(peers.has_secret, true);
    assert.equal(peers.sync_tls_ready, true);
    assert.ok(
      peers.peers?.some((entry) => entry.healthy && entry.last_sync_at),
    );
    await writeFile(
      join(output, "peer-sync.json"),
      JSON.stringify(peers, null, 2),
    );
    const listing = await context.request.get(peerBase + "/api/blocklists");
    assert.ok(listing.ok());
    const peerLists = schema.parseGetApiBlocklistsResponse(
      await listing.json(),
    ).data;
    const controlled = peerLists?.find(
      (entry) => entry.alias === "E2E controlled list",
    );
    assert.ok(controlled);
    const refreshed = await context.request.post(
      peerBase + "/api/blocklists/" + String(controlled.id) + "/refresh",
      { headers: { Origin: peerBase } },
    );
    assert.ok(refreshed.ok(), await refreshed.text());
    await until(
      () =>
        JSON.stringify(
          sql(
            peer.database,
            "SELECT count(*) AS n FROM blocked_domains WHERE domain='blocked.example'",
          ),
        ) === JSON.stringify([{ n: 1 }]),
      "peer local published-list contents",
    );
    await dns("peer-allowed.example", "NOERROR", "peer-allowed", peer.dnsPort);
    await dns("blocked.example", "NXDOMAIN", "peer-blocked", peer.dnsPort);
    // Keep later fixtures deterministic: remove only this test's reciprocal peers.
    await request(page, "DELETE", `/api/peers/${encodeURIComponent(peerBase)}`);
    const removed = await context.request.delete(
      peerBase + "/api/peers/" + encodeURIComponent(base),
      { headers: { Origin: peerBase } },
    );
    assert.ok(removed.ok());
  } finally {
    await context.close();
  }
}
let coldDay = "";
export async function restartAndArchive(page: Page, app: App): Promise<void> {
  await dns("history.example.com", "NOERROR", "history-original");
  await until(() => {
    const rows = sql(
      app.database,
      "SELECT count(*) AS n FROM query_logs WHERE query_name='history.example.com.'",
    );
    return JSON.stringify(rows) === JSON.stringify([{ n: 2 }]);
  }, "exact UDP/TCP history rows");
  await app.stop();
  const before = sql(
    app.database,
    "SELECT * FROM query_logs WHERE query_name='history.example.com.' ORDER BY id",
  );
  const journal = sql(
    app.database + ".spool.sqlite",
    "SELECT id,hex(payload) AS payload FROM journal_records ORDER BY id",
  );
  await writeFile(
    join(output, "history-before-restart.json"),
    JSON.stringify({ before, journal }, null, 2),
  );
  // Seed explicitly historical fixture rows, then drive the real archive API.
  // Production traffic and durable raw originals are never rewritten.
  sql(
    app.database,
    "INSERT INTO query_logs(timestamp,client_ip,query_name,query_type,response_code,blocked,coalesced_count) VALUES(datetime('now','-10 days'),'192.0.2.22','cold.example.com.','A','NOERROR',0,7)",
  );
  const coldRows = sql(
    app.database,
    "SELECT date(timestamp) AS day FROM query_logs WHERE query_name='cold.example.com.'",
  );
  assert.ok(Array.isArray(coldRows));
  const coldRow: unknown = coldRows[0];
  assert.ok(
    coldRow &&
      typeof coldRow === "object" &&
      "day" in coldRow &&
      typeof coldRow.day === "string",
  );
  coldDay = coldRow.day;
  await app.start();
  assert.deepEqual(
    sql(
      app.database,
      "SELECT * FROM query_logs WHERE query_name='history.example.com.' ORDER BY id",
    ),
    before,
  );
  await page.goto(base + "/investigation");
  const archived = schema.parsePostApiArchiveData(
    await request(page, "POST", "/api/archive"),
  );
  await writeFile(
    join(output, "archive-status.json"),
    JSON.stringify(archived, null, 2),
  );
  assert.deepEqual(
    sql(
      app.database,
      "SELECT count(*) AS n FROM query_logs WHERE query_name='cold.example.com.'",
    ),
    [{ n: 0 }],
  );
  const result = schema.parsePostApiInvestigateData(
    await request(page, "POST", "/api/investigate", {
      sql: "SELECT client_ip,query_name,coalesced_count FROM query_logs WHERE query_name IN ('cold.example.com.','history.example.com.') ORDER BY client_ip,query_name",
      timeout: 10,
    }),
  );
  assert.equal(result.row_count, 3);
  assert.ok(
    result.rows?.some(
      (row) =>
        row?.[0] === "192.0.2.22" &&
        row[1] === "cold.example.com." &&
        row[2] === 7,
    ),
  );
  await writeFile(
    join(output, "hot-cold-history.json"),
    JSON.stringify(result, null, 2),
  );
}
export async function investigation(page: Page): Promise<void> {
  await page.goto(base + "/investigation");
  await page
    .locator(".schema-table-header")
    .filter({ hasText: "query_logs" })
    .waitFor();
  const choices = await page
    .locator(".template-select option")
    .allTextContents();
  const labels = choices.filter((entry) => entry !== "Templates...");
  assert.ok(labels.includes("Client activity over time"));
  const results: schema.InvestigationView[] = [];
  for (const label of [
    "Client activity over time",
    "Domain investigation",
    "Behavioral change detection",
    "Rogue device check",
  ]) {
    await page.locator(".template-select").selectOption({ label });
    const response = page.waitForResponse(
      (item) =>
        item.url().endsWith("/api/investigate") &&
        item.request().method() === "POST",
    );
    await page.getByRole("button", { name: /Run Ctrl/ }).click();
    const completed = await response;
    assert.ok(completed.ok(), await completed.text());
    const body: unknown = await completed.json();
    assert.ok(body && typeof body === "object" && "data" in body);
    const result = schema.parsePostApiInvestigateData(body.data);
    assert.ok(result.row_count >= 1, label);
    assert.ok(
      result.rows?.some((row) => row?.[0] === "192.0.2.22"),
      `${label} omitted archived client`,
    );
    await page.locator(".results-toolbar").waitFor();
    results.push(result);
    await capture(page, `template-${String(results.length)}`);
  }
  assert.ok(
    results[0]?.rows?.some(
      (row) =>
        row?.[0] === "192.0.2.22" &&
        row[1] === coldDay + "T00:00:00Z" &&
        row[2] === 7,
    ),
  );
  assert.ok(
    results[1]?.rows?.some(
      (row) => row?.[1] === "cold.example.com." && row[2] === 7,
    ),
  );
  assert.ok(
    results[2]?.rows?.some(
      (row) =>
        row?.[0] === "192.0.2.22" &&
        row[1] === coldDay.split("-").slice(0, 2).join("-") &&
        row[2] === 1,
    ),
  );
  assert.ok(
    results[3]?.rows?.some(
      (row) => row?.[0] === "192.0.2.22" && row[1] === 1 && row[2] === 7,
    ),
  );
  await writeFile(
    join(output, "all-four-templates.json"),
    JSON.stringify(results, null, 2),
  );
  const editor = page.locator(".cm-content");
  await editor.click();
  await page.keyboard.press("Control+A");
  await page.keyboard.insertText(
    "SELECT 42 AS keyboard_result FROM query_logs LIMIT 1",
  );
  await page.keyboard.press("Control+Enter");
  await page
    .locator(".results-table")
    .getByRole("cell", { name: "42", exact: true })
    .waitFor();
  await editor.click();
  await page.keyboard.press("Control+A");
  await page.keyboard.insertText("SELECT missing_column FROM query_logs");
  await page.getByRole("button", { name: /Run Ctrl/ }).click();
  await page.locator(".query-error").waitFor();
  assert.ok(
    (await editor.innerText()).includes(
      "SELECT missing_column FROM query_logs",
    ),
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await editor.click();
  await page.keyboard.press("Control+A");
  await page.keyboard.insertText(
    "SELECT 73 AS mobile_result FROM query_logs LIMIT 1",
  );
  await page.getByRole("button", { name: /Run Ctrl/ }).click();
  await page
    .locator(".results-table")
    .getByRole("cell", { name: "73", exact: true })
    .waitFor();
  await capture(page, "investigation-mobile-result");
  await page.setViewportSize({ width: 1440, height: 1100 });
}
export async function securityAndRetries(page: Page): Promise<void> {
  const payload = '<img src=x onerror="document.body.dataset.e2eInjected=1">';
  await request(page, "PUT", "/api/clients/192.0.2.10/alias", {
    alias: payload,
  });
  await page.goto(base + "/logs");
  await page.getByText(payload, { exact: true }).first().waitFor();
  assert.equal(await page.locator('img[src="x"]').count(), 0);
  assert.equal(
    await page.locator("body").getAttribute("data-e2e-injected"),
    null,
  );
  await capture(page, "injection-literal-dom");
  await request(page, "PUT", "/api/clients/192.0.2.10/alias", {
    alias: "E2E client",
  });
  // Inject failures at the browser/network boundary; the server is still real.
  await page.route("**/api/blocklists", async (route) => {
    if (route.request().method() === "GET")
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ data: null, error: "fixture unavailable" }),
      });
    else await route.continue();
  });
  await page.goto(base + "/filter-lists");
  await page.getByRole("alert").waitFor();
  await capture(page, "read-failure");
  await page.unroute("**/api/blocklists");
  await page
    .getByRole("button", { name: "Dismiss error", exact: true })
    .click();
  await page.reload();
  await page
    .locator(".filters-grid-row")
    .filter({ hasText: "E2E controlled list" })
    .waitFor();
  await capture(page, "read-retried");
  await page.goto(base + "/rewrites");
  await page.getByPlaceholder("app.local").fill("retry-rewrite.example");
  await page.getByPlaceholder("192.168.1.100").fill("203.0.113.42");
  await page.route("**/api/rewrites", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ data: null, error: "fixture unavailable" }),
    }),
  );
  await page.getByRole("button", { name: "+ Add Record", exact: true }).click();
  await page.getByRole("alert").waitFor();
  assert.equal(
    await page.getByPlaceholder("app.local").inputValue(),
    "retry-rewrite.example",
  );
  await page.unroute("**/api/rewrites");
  await page.getByRole("button", { name: "+ Add Record", exact: true }).click();
  await page.getByText("retry-rewrite.example", { exact: true }).waitFor();
  await dns("retry-rewrite.example", "NOERROR", "save-retried");
  await capture(page, "save-retried");
}
export async function verifyTransportCounts(): Promise<void> {
  const raw: unknown = JSON.parse(
    await readFile(join(output, "fixture-counts.json"), "utf8"),
  );
  assert.ok(raw && typeof raw === "object");
  for (const key of ["udp", "tcp", "doh", "dot", "lists"]) {
    assert.ok(
      key in raw &&
        typeof Reflect.get(raw, key) === "number" &&
        Reflect.get(raw, key) > 0,
      `fixture ${key} never exercised`,
    );
  }
}

export async function verifyOwnership(app: App): Promise<void> {
  snapshotRaw(app);
  const originals = rawSnapshots.get(app.name);
  assert.ok(originals);
  const expected = new Map<string, number>();
  for (const query of acceptedQueries.filter(
    (entry) => entry.port === app.dnsPort,
  ))
    expected.set(query.domain, (expected.get(query.domain) ?? 0) + 1);
  const actual = new Map<string, number>();
  for (const hex of originals.values()) {
    const value: unknown = JSON.parse(Buffer.from(hex, "hex").toString("utf8"));
    assert.ok(
      value &&
        typeof value === "object" &&
        "query_name" in value &&
        typeof value.query_name === "string",
    );
    actual.set(value.query_name, (actual.get(value.query_name) ?? 0) + 1);
  }
  assert.deepEqual(actual, expected, app.name + " exact accepted raw events");
  const identityRows = sql(
    app.database + ".spool.sqlite",
    "SELECT value FROM journal_meta WHERE key='identity'",
  );
  assert.ok(Array.isArray(identityRows));
  const identityRow: unknown = identityRows[0];
  assert.ok(
    identityRow &&
      typeof identityRow === "object" &&
      "value" in identityRow &&
      typeof identityRow.value === "string",
  );
  const identity = identityRow.value;
  const retained = sql(
    app.database + ".spool.sqlite",
    "SELECT id,hex(payload) AS payload FROM journal_records ORDER BY id",
  );
  assert.ok(Array.isArray(retained));
  const covered = new Set<number>();
  for (const item of retained) {
    const row: unknown = item;
    assert.ok(
      row &&
        typeof row === "object" &&
        "id" in row &&
        typeof row.id === "number" &&
        "payload" in row &&
        typeof row.payload === "string",
    );
    assert.equal(row.payload, originals.get(row.id));
    covered.add(row.id);
  }
  const manifests = sql(
    app.database + ".spool.sqlite",
    "SELECT first_id,last_id,count,digest FROM raw_archive_files WHERE state='ready' ORDER BY first_id",
  );
  assert.ok(Array.isArray(manifests));
  for (const item of manifests) {
    const manifest: unknown = item;
    assert.ok(
      manifest &&
        typeof manifest === "object" &&
        "first_id" in manifest &&
        typeof manifest.first_id === "number" &&
        "last_id" in manifest &&
        typeof manifest.last_id === "number" &&
        "count" in manifest &&
        typeof manifest.count === "number" &&
        "digest" in manifest &&
        typeof manifest.digest === "string",
    );
    const hash = createHash("sha256");
    let count = 0;
    for (
      let sequence = manifest.first_id;
      sequence <= manifest.last_id;
      sequence++
    ) {
      const hex: string | undefined = originals.get(sequence);
      assert.ok(hex);
      const payload = Buffer.from(hex, "hex");
      const timestamp = /"ts":([0-9]+)/.exec(payload.toString("utf8"));
      assert.ok(timestamp?.[1]);
      for (const value of [
        BigInt(Buffer.byteLength(identity)),
        BigInt(sequence),
        BigInt(timestamp[1]),
        BigInt(payload.length),
      ]) {
        const number = Buffer.alloc(8);
        number.writeBigUInt64LE(value);
        hash.update(number);
      }
      hash.update(identity);
      hash.update(payload);
      covered.add(sequence);
      count++;
    }
    assert.equal(count, manifest.count);
    assert.equal(hash.digest("hex"), manifest.digest);
  }
  assert.deepEqual(
    [...covered].sort((a, b) => a - b),
    [...originals.keys()].sort((a, b) => a - b),
  );
  const verified = execFileSync(
    binary,
    [
      "verify-raw-archives",
      app.database + ".spool.sqlite",
      join(app.directory, "archives"),
    ],
    { encoding: "utf8" },
  );
  await writeFile(join(output, app.name + "-raw-verifier.log"), verified);
  await writeFile(
    join(output, app.name + "-raw-ownership.json"),
    JSON.stringify(
      {
        identity,
        expected: [...expected],
        actual: [...actual],
        originals: [...originals],
        manifests,
      },
      null,
      2,
    ),
  );
}
