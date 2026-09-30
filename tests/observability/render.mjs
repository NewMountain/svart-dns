/* Real browser verification of the provisioned operator panels. */
import fs from "node:fs";
import path from "node:path";
import assert from "node:assert/strict";
import playwright from "../../scripts/node_modules/playwright-core/index.js";
const { chromium } = playwright;

async function main() {
  const root = process.env.EVIDENCE;
  assert(root, "EVIDENCE is required");
  const browserPath = process.env.PLAYWRIGHT_BROWSER;
  assert(browserPath, "PLAYWRIGHT_BROWSER is required");
  const urls = /** @type {unknown} */ (
    JSON.parse(fs.readFileSync(path.join(root, "urls.json"), "utf8"))
  );
  assert(
    urls &&
      typeof urls === "object" &&
      "grafana" in urls &&
      typeof urls.grafana === "string",
    "invalid Grafana fixture URL",
  );
  const browser = await chromium.launch({
    headless: true,
    executablePath: browserPath,
    // Grafana requires CacheStorage, which browsers expose only in secure contexts.
    // Trust only this disposable internal origin, matching production HTTPS behavior.
    args: [
      "--disable-dev-shm-usage",
      `--unsafely-treat-insecure-origin-as-secure=${urls.grafana}`,
    ],
  });
  try {
    const page = await browser.newPage({
      viewport: { width: 3840, height: 2160 },
    });
    /** @type {{message: string, stack: string, url: string}[]} */
    const errors = [];
    page.on("pageerror", (error) =>
      errors.push({
        message: error.message,
        stack: error.stack ?? "",
        url: page.url(),
      }),
    );
    /** @type {Array<[number, string]>} */
    const panels = [
      [23, "HTTP requests and failures"],
      [27, "Durable queue records"],
      [31, "Service logs (trace_id opens Tempo)"],
      [32, "Control-plane traces"],
      [33, "Continuous CPU profiles"],
    ];
    for (const [id, title] of panels) {
      await page.goto(
        `${urls.grafana}/d/svart-dns/svart-dns?orgId=1&from=now-15m&to=now&viewPanel=${String(id)}`,
        { waitUntil: "networkidle" },
      );
      assert(
        await page.evaluate(() => globalThis.isSecureContext),
        "Grafana requires a secure browser context",
      );
      await page.getByText(title, { exact: true }).first().waitFor();
      const body = await page.locator("body").innerText();
      assert(!body.includes("No data"), `Panel ${String(id)} has no data`);
      if (id === 31)
        assert(body.includes("trace_id"), "Logs lack trace correlation");
      if (id === 32)
        assert(body.includes("svart-dns"), "Trace rows are absent");
      if (id === 33)
        assert(body.includes("runtime."), "CPU stack samples are absent");
      await page.screenshot({
        path: path.join(root, `panel-${String(id)}.png`),
      });
      fs.writeFileSync(path.join(root, `panel-${String(id)}.txt`), body);
      console.log(`Rendered populated panel ${String(id)}: ${title}`);
    }
    fs.writeFileSync(
      path.join(root, "browser-errors.json"),
      JSON.stringify(errors),
    );
    assert.deepEqual(errors, []);
  } finally {
    await browser.close();
  }
}
try {
  await main();
} catch (error) {
  console.error(error);
  process.exitCode = 1;
}
