import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import * as api from "../frontend/src/api/operations";

const base = process.env.SVART_REVIEW_BASE;
const token = process.env.SVART_REVIEW_TOKEN;
const phase = process.env.SVART_REVIEW_PHASE;
assert.ok(base, "The Go fixture must provide its loopback HTTP URL");
assert.ok(token, "The Go fixture must provide its disposable API token");
assert.ok(phase === "create" || phase === "delete", "Unknown fixture phase");

// Only adapt the network boundary. Every request, validator and operation comes
// from the same generated client used by the application.
const rawFetch: typeof fetch = globalThis.fetch;
globalThis.fetch = (input, options = {}): Promise<Response> => {
  const path =
    typeof input === "string"
      ? input
      : input instanceof URL
        ? input.href
        : input.url;
  const headers = new Headers(options.headers);
  headers.set("X-Api-Key", token);
  return rawFetch(new URL(path, base), { ...options, headers });
};

if (phase === "delete") {
  await api.deleteApiRangesIdBlockDomainDomain(1, "ads.example.com");
  await api.deleteApiRangesIdAllowDomainDomain(1, "docs.example.com");
  const detail = await api.getApiRangesId(1);
  assert.deepEqual(detail.custom_blocked, []);
  assert.deepEqual(detail.custom_allowed, []);
  console.log("Generated clients: DELETE and exact empty GET arrays passed");
} else {
  // Go runs this fixture from the repository root; compare complete published
  // bytes, including anonymous aliases, against the same source build.
  for (const path of [
    "/api/openapi.json",
    "/openapi.json",
    "/docs/swagger.json",
  ]) {
    const response: Response = await rawFetch(new URL(path, base));
    assert.equal(response.status, 200);
    assert.equal(
      await response.text(),
      readFileSync("docs/swagger.json", "utf8"),
    );
  }
  const markdown = await rawFetch(new URL("/docs.md", base));
  assert.equal(markdown.status, 200);
  assert.equal(
    await markdown.text(),
    readFileSync("docs/api-reference.md", "utf8"),
  );
  for (const path of ["/docs", "/static/rapidoc-min.js"]) {
    assert.equal((await rawFetch(new URL(path, base))).status, 200);
  }
  assert.equal((await rawFetch(new URL("/api/stats", base))).status, 401);

  const listsBefore = await api.getApiBlocklists();
  assert.ok(listsBefore, "The Go fixture must expose blocklists");
  const before = listsBefore.find((entry) => entry.id === 900);
  assert.ok(before, "The Go fixture must seed the checkpoint list");
  await api.postApiBlocklistsIdToggle(900);
  const listsAfter = await api.getApiBlocklists();
  assert.ok(listsAfter, "The toggled blocklists must remain available");
  const after = listsAfter.find((entry) => entry.id === 900);
  assert.ok(after, "The toggled list must remain present");
  assert.equal(after.enabled, !before.enabled);
  const page = await api.getApiBlocklistsHistoryHistoryIdDomains(901, {
    limit: "2",
    offset: "1",
  });
  assert.deepEqual(page.domains, ["readded.example", "removed.example"]);
  assert.equal(page.total, 3);
  await api.postApiRangesIdBlockDomain(1, { domain: "ads.example.com" });
  await api.postApiRangesIdAllowDomain(1, { domain: "docs.example.com" });
  const detail = await api.getApiRangesId(1);
  assert.deepEqual(detail.custom_blocked, ["ads.example.com"]);
  assert.deepEqual(detail.custom_allowed, ["docs.example.com"]);
  console.log(
    "Generated clients: toggle, checkpoint, range POST/GET and complete public docs passed",
  );
}
