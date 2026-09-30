import assert from "node:assert/strict";
import { createWriteStream } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { networkInterfaces } from "node:os";
import { chromium } from "playwright-core";
import {
  backup,
  configurePolicy,
  inventory,
  onboarding,
  policyInteractions,
  request,
} from "./browser";
import {
  investigation,
  peerSync,
  restartAndArchive,
  securityAndRetries,
  transports,
  verifyTransportCounts,
  verifyOwnership,
} from "./checks";
import { App, base, fixtures, output } from "./world";
import { controls } from "./controls";
import { compatibilityControls } from "./compatibility-controls";
import { clientMembershipControls } from "./client-membership";

await mkdir(output, { recursive: true });
assert.ok(
  Object.values(networkInterfaces())
    .flat()
    .every((entry) => entry?.internal),
  "E2E requires an isolated loopback-only network",
);
const closeFixtures = await fixtures();
const primary = new App("primary", 50300, 50153);
const peer = new App("peer", 50301, 50154);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env["SVART_E2E_CHROMIUM"] ?? "/usr/bin/chromium",
  args: ["--no-sandbox"],
});
const context = await browser.newContext({
  viewport: { width: 1440, height: 1100 },
  reducedMotion: "reduce",
  ignoreHTTPSErrors: true,
  acceptDownloads: true,
  recordHar: { path: join(output, "browser.har"), content: "embed" },
});
const page = await context.newPage();
page.setDefaultTimeout(15000);
const errors: string[] = [];
const consoleLog = createWriteStream(join(output, "browser-console.jsonl"));
const networkLog = createWriteStream(join(output, "browser-network.jsonl"));
page.on("pageerror", (error) => errors.push(error.message));
page.on("console", (message) =>
  consoleLog.write(
    JSON.stringify({ type: message.type(), text: message.text() }) + "\n",
  ),
);
page.on("requestfailed", (item) =>
  networkLog.write(
    JSON.stringify({
      event: "failed",
      url: item.url(),
      method: item.method(),
      failure: item.failure(),
    }) + "\n",
  ),
);
page.on("response", (item) =>
  networkLog.write(
    JSON.stringify({
      event: "response",
      url: item.url(),
      status: item.status(),
      method: item.request().method(),
    }) + "\n",
  ),
);
let completed = false;
try {
  await primary.start();
  await peer.start();
  await onboarding(page, primary);
  console.log("PASS fresh setup and login");
  await transports(page);
  console.log("PASS UDP/TCP/DoH/DoT transports");
  const range = await configurePolicy(page);
  await policyInteractions(page, range);
  console.log("PASS list, assignment, policy mutation and DNS");
  await request(page, "POST", "/api/policies", {
    name: "E2E bundle",
    description: "Disposable browser fixture",
  });
  await peerSync(browser, page, peer);
  console.log("PASS real TLS peer sync");
  await backup(page, primary);
  console.log("PASS exact backup download/upload and atomic rejection");
  await restartAndArchive(page, primary);
  await investigation(page);
  console.log(
    "PASS restart, history, archives and all four Investigation templates",
  );
  await securityAndRetries(page);
  console.log("PASS literal injection DOM and failed read/save retry");
  await controls(browser, page, primary, peer);
  console.log("PASS desktop/mobile admin, config, simulator and log controls");
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1100 });
    await compatibilityControls(page, `compat-${String(width)}`, primary);
    await clientMembershipControls(page, `membership-${String(width)}`);
  }
  await inventory(page);
  console.log("PASS desktop/mobile page, tab, detail and form inventory");
  assert.deepEqual(errors, []);
  completed = true;
} catch (error) {
  await page
    .screenshot({ path: join(output, "failure.png"), fullPage: true })
    .catch(() => undefined);
  await writeFile(
    join(output, "failure.txt"),
    String(error) +
      "\n" +
      (await page
        .locator("body")
        .innerText()
        .catch(() => "Browser unavailable")),
  );
  throw error;
} finally {
  await context.close();
  await browser.close();
  await primary.stop();
  await peer.stop();
  await closeFixtures();
  consoleLog.end();
  networkLog.end();
  if (completed) {
    await verifyTransportCounts();
    await verifyOwnership(primary);
    await verifyOwnership(peer);
    await writeFile(
      join(output, "result.json"),
      JSON.stringify(
        {
          result: "PASS",
          sourceRevision: process.env["SVART_E2E_REVISION"],
          baseURL: base,
          pageErrors: errors,
        },
        null,
        2,
      ),
    );
  }
}
