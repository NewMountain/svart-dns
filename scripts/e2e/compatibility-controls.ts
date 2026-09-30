import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import * as schema from "../../frontend/src/api/generated";
import { capture, clickMutation, request } from "./browser";
import { base, fixtureLists, until, type App } from "./world";

// Real downloaded source, complete diagnostics, and record-type-aware analysis.
// No production data or external list server is involved.
export async function compatibilityControls(
  page: Page,
  suffix: string,
  app?: App,
): Promise<void> {
  const path = `/compatibility-${suffix}.txt`;
  const url = "http://127.0.0.1:50800" + path;
  const name = `Compatibility ${suffix}`;
  const rejected = Array.from(
    { length: 52 },
    (_, i) => `||ignored${String(i)}.example^$client=~192.0.2.7`,
  );
  fixtureLists.set(
    path,
    ["||typed.compat.example^$dnstype=TXT", ...rejected].join("\n") + "\n",
  );
  await page.goto(base + "/filter-lists");
  await page
    .getByPlaceholder("Paste Blocklist URL", { exact: false })
    .fill(url);
  await page.getByPlaceholder("Name (optional)").fill(name);
  await clickMutation(
    page,
    page.getByRole("button", { name: "Import List", exact: true }),
    "POST",
    "/api/blocklists",
  );
  const created = schema
    .parseGetApiBlocklistsData(await request(page, "GET", "/api/blocklists"))
    ?.find((list) => list.url === url);
  assert.ok(created);
  const id = String(created.id);
  await until(
    async () =>
      schema.parseGetApiBlocklistsIdCompatibilityData(
        await request(page, "GET", `/api/blocklists/${id}/compatibility`),
      ).summary.assessed,
    "assessed local compatibility generation",
  );
  await page.reload();
  const row = page.locator(".filters-grid-row").filter({ hasText: name });
  await row.locator("summary").click();
  await row.getByText(rejected[0] ?? "", { exact: true }).waitFor();
  const rejectNext = async (): Promise<void> => {
    await page.route(
      `**/api/blocklists/${id}/compatibility?*`,
      (route) =>
        route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({
            data: null,
            error: "Fixture diagnostic read failed",
            error_code: "unavailable",
          }),
        }),
      { times: 1 },
    );
  };
  await rejectNext();
  await row.getByRole("button", { name: "Next diagnostics" }).click();
  await row.getByRole("alert").waitFor();
  assert.ok(
    await row.getByText(rejected[0] ?? "", { exact: true }).isVisible(),
  );
  await row.getByRole("button", { name: "Retry diagnostics" }).click();
  await row.getByText(rejected[51] ?? "", { exact: true }).waitFor();
  await capture(page, `${suffix}-compatibility-full-diagnostics`);
  const first = schema.parseGetApiBlocklistsIdCompatibilityData(
    await request(
      page,
      "GET",
      `/api/blocklists/${id}/compatibility?limit=50&offset=0`,
    ),
  );
  const last = schema.parseGetApiBlocklistsIdCompatibilityData(
    await request(
      page,
      "GET",
      `/api/blocklists/${id}/compatibility?limit=50&offset=50`,
    ),
  );
  assert.deepEqual(
    [...(first.diagnostics ?? []), ...(last.diagnostics ?? [])].map(
      (item) => item.rule,
    ),
    rejected,
  );
  await request(page, "POST", `/api/clients/192.0.2.10/blocklists/${id}`);
  if (app) {
    await app.stop();
    await app.start();
    await page.reload();
    const persisted = schema.parseGetApiBlocklistsIdCompatibilityData(
      await request(
        page,
        "GET",
        `/api/blocklists/${id}/compatibility?limit=50&offset=0`,
      ),
    );
    assert.deepEqual(persisted, first);
    const persistedLast = schema.parseGetApiBlocklistsIdCompatibilityData(
      await request(
        page,
        "GET",
        `/api/blocklists/${id}/compatibility?limit=50&offset=50`,
      ),
    );
    assert.deepEqual(persistedLast, last);
  }
  await page.goto(base + "/analysis");
  await page
    .getByLabel("Domains", { exact: true })
    .fill("typed.compat.example");
  for (const type of ["A", "TXT"]) {
    await page.getByLabel("Record type", { exact: true }).fill(type);
    const result = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === "/api/analysis/matrix",
    );
    await page.getByRole("button", { name: "Run Matrix", exact: true }).click();
    const response = await result;
    assert.ok(response.ok());
    const envelope: unknown = await response.json();
    assert.ok(envelope && typeof envelope === "object" && "data" in envelope);
    const matrix = schema.parsePostApiAnalysisMatrixData(envelope.data);
    const column: number =
      matrix.lists?.findIndex((list): boolean => list.id === created.id) ?? -1;
    assert.ok(column >= 0);
    assert.equal(matrix.record_type, type === "TXT" ? 16 : 1);
    assert.equal(matrix.matrix?.[0]?.[column], type === "TXT");
    await page.getByText(`Record type: ${type}`, { exact: true }).waitFor();
    await capture(page, `${suffix}-compatibility-matrix-${type}`);
    await page
      .getByRole("region", { name: "Blocklist matrix results" })
      .scrollIntoViewIfNeeded();
    await capture(page, `${suffix}-compatibility-matrix-${type}-results`);
  }
  await page.goto(base + "/analysis?tab=sim");
  await page.getByPlaceholder("Search clients...").fill("192.0.2.10");
  await page
    .locator(".client-search-item")
    .filter({ hasText: "192.0.2.10" })
    .click();
  await page
    .getByLabel("Domains", { exact: true })
    .fill("typed.compat.example");
  for (const type of ["A", "TXT"]) {
    await page.getByLabel("Record type", { exact: true }).fill(type);
    const result = schema.parsePostApiAnalysisSimulateData(
      await clickMutation(
        page,
        page.getByRole("button", { name: "Simulate Policy", exact: true }),
        "POST",
        "/api/analysis/simulate",
      ),
    );
    assert.equal(result.results?.[0]?.record_type, type === "TXT" ? 16 : 1);
    assert.equal(result.results[0].result, type === "TXT" ? "block" : "allow");
    if (type === "TXT") {
      assert.equal(
        result.results[0].result_source?.published_list?.rule,
        "||typed.compat.example^$dnstype=TXT",
      );
      await page
        .getByText(`Rule: ||typed.compat.example^$dnstype=TXT (${name})`, {
          exact: true,
        })
        .waitFor();
    }
    await capture(page, `${suffix}-compatibility-simulator-${type}`);
    await page.locator(".sim-cell.final").scrollIntoViewIfNeeded();
    assert.ok(
      await page
        .locator(".sim-cell.final")
        .evaluate((cell) =>
          Array.from(cell.querySelectorAll(".cell-detail")).every(
            (detail) => detail.scrollWidth <= detail.clientWidth + 1,
          ),
        ),
      "complete decision attribution fits its cell without clipping",
    );
    await capture(page, `${suffix}-compatibility-simulator-${type}-results`);
  }
  await page.getByLabel("Record type", { exact: true }).fill("A");
  await request(page, "DELETE", `/api/blocklists/${id}`);
}
