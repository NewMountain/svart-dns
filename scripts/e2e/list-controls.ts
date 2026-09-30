import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import * as schema from "../../frontend/src/api/generated";
import { capture, clickMutation, request } from "./browser";
import { base, fixtureLists, until } from "./world";

export async function listControls(page: Page, suffix: string): Promise<void> {
  const path = "/control-" + suffix + ".txt";
  const url = "http://127.0.0.1:50800" + path;
  const original = "Control list " + suffix;
  const renamed = "Renamed list " + suffix;
  fixtureLists.set(path, "old.control.example\nkept.control.example\n");
  await page.goto(base + "/filters");
  await page
    .getByPlaceholder("Paste Blocklist URL", { exact: false })
    .fill(url);
  await page.getByPlaceholder("Name (optional)").fill(original);
  await clickMutation(
    page,
    page.getByRole("button", { name: "Import List", exact: true }),
    "POST",
    "/api/blocklists",
  );
  const row = page.locator(".filters-grid-row").filter({ hasText: url });
  const editingRow = page
    .locator(".filters-grid-row")
    .filter({ has: page.getByRole("textbox") });
  await row.waitFor();
  let lists = schema.parseGetApiBlocklistsData(
    await request(page, "GET", "/api/blocklists"),
  );
  const created = lists?.find((list) => list.url === url);
  assert.ok(created);
  const id = String(created.id);
  await until(
    async () =>
      schema.parseGetApiBlocklistsIdDomainsData(
        await request(page, "GET", "/api/blocklists/" + id + "/domains"),
      ).total === 2,
    "initial local list download",
  );
  await row.getByRole("button", { name: "Rename list", exact: true }).click();
  await editingRow.getByRole("textbox").fill("Cancelled name");
  await editingRow.getByRole("button", { name: "Cancel", exact: true }).click();
  assert.equal(await row.locator(".list-name").innerText(), original);
  await row.getByRole("button", { name: "Rename list", exact: true }).click();
  await editingRow.getByRole("textbox").fill(renamed);
  await clickMutation(
    page,
    editingRow.getByRole("button", { name: "Save", exact: true }),
    "PUT",
    "/api/blocklists/" + id,
  );
  await row.getByText(renamed, { exact: true }).waitFor();
  const options = await row.locator("select option").evaluateAll((items) =>
    items.map((item) => {
      if (!(item instanceof HTMLOptionElement))
        throw new Error("Invalid refresh option");
      return item.value;
    }),
  );
  for (const value of options) {
    const saved = page.waitForResponse(
      (response) =>
        response.request().method() === "PUT" &&
        new URL(response.url()).pathname === "/api/blocklists/" + id,
    );
    await row.locator("select").selectOption(value);
    assert.ok((await saved).ok());
    lists = schema.parseGetApiBlocklistsData(
      await request(page, "GET", "/api/blocklists"),
    );
    assert.equal(
      lists?.find((list) => list.id === created.id)?.refresh_interval,
      Number(value),
    );
  }
  fixtureLists.set(path, "new.control.example\nkept.control.example\n");
  await clickMutation(
    page,
    row.getByRole("button", { name: "Refresh list", exact: true }),
    "POST",
    "/api/blocklists/" + id + "/refresh",
  );
  await until(async () => {
    const current = schema.parseGetApiBlocklistsIdDomainsData(
      await request(page, "GET", "/api/blocklists/" + id + "/domains"),
    );
    return current.domains?.includes("new.control.example") === true;
  }, "updated local list content");
  await capture(page, suffix + "-list-renamed-refreshed");
  const history =
    schema
      .parseGetApiBlocklistsHistoryData(
        await request(page, "GET", "/api/blocklists/history"),
      )
      ?.filter((entry) => entry.blocklist_id === created.id) ?? [];
  const first = history.find(
    (entry) => entry.added_count === 2 && entry.removed_count === 0,
  );
  const changed = history.find(
    (entry) => entry.added_count === 1 && entry.removed_count === 1,
  );
  assert.ok(first);
  assert.ok(changed);
  await page
    .getByRole("button", { name: "History & Changelog", exact: true })
    .click();
  await page.locator(".history-toolbar select").selectOption(id);
  await page
    .getByPlaceholder("Search domain history", { exact: false })
    .fill("new.control.example");
  await page.getByText("+ new.control.example", { exact: true }).waitFor();
  assert.equal(await page.locator(".timeline-item").count(), 1);
  await capture(page, suffix + "-list-history-diff");
  await page.getByRole("button", { name: "List Details", exact: true }).click();
  await page.locator(".browser-header select").first().selectOption(id);
  await page.getByText("new.control.example", { exact: true }).waitFor();
  await page
    .locator(".browser-header select")
    .nth(1)
    .selectOption(String(first.id));
  await page
    .getByText("Viewing historical checkpoint", { exact: true })
    .waitFor();
  await page.getByText("old.control.example", { exact: true }).waitFor();
  assert.equal(
    await page.getByText("new.control.example", { exact: true }).count(),
    0,
  );
  await page.getByPlaceholder("Search domains within list...").fill("kept");
  await page.getByText("1 domains matching", { exact: true }).waitFor();
  assert.deepEqual(await page.locator(".domain-row").allTextContents(), [
    "kept.control.example",
  ]);
  await capture(page, suffix + "-list-historical-search");
  await page.locator(".browser-header select").nth(1).selectOption("");
  await page.getByPlaceholder("Search domains within list...").fill("new");
  await page.getByText("new.control.example", { exact: true }).waitFor();
  await page
    .getByRole("button", { name: "Published Lists", exact: true })
    .click();
  await clickMutation(
    page,
    row.getByRole("button", { name: "Delete list", exact: true }),
    "DELETE",
    "/api/blocklists/" + id,
  );
  await row.waitFor({ state: "detached" });
  lists = schema.parseGetApiBlocklistsData(
    await request(page, "GET", "/api/blocklists"),
  );
  assert.equal(
    lists?.some((list) => list.id === created.id),
    false,
  );
}
