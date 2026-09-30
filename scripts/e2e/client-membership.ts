import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import * as schema from "../../frontend/src/api/generated";
import { capture, request } from "./browser";
import { base } from "./world";

export async function clientMembershipControls(
  page: Page,
  suffix: string,
): Promise<void> {
  const ip = "192.0.2.118";
  await request(page, "PUT", `/api/clients/${ip}/alias`, {
    alias: "NAS fixture",
  });
  const groupIDs: number[] = [];
  const names = [`Personal devices ${suffix}`, `Development ${suffix}`];
  for (const name of names) {
    const group = schema.parsePostApiGroupsData(
      await request(page, "POST", "/api/groups", { name }),
    );
    groupIDs.push(group.id);
    await request(
      page,
      "POST",
      `/api/groups/${String(group.id)}/members/192.0.2.99`,
    );
  }
  const path = `/assignments?tab=clients&selected=${ip}`;
  const widget = page.locator(".widget").filter({
    has: page.locator(".widget-title").getByText("Groups", { exact: true }),
  });
  await page.goto(base + path);
  await widget.getByText("Not in any groups", { exact: true }).waitFor();
  assert.equal(await widget.locator(".chip").count(), 0);
  const detail = schema.parseGetApiClientsIpData(
    await request(page, "GET", `/api/clients/${ip}`),
  );
  assert.deepEqual(
    detail.groups?.filter((group) => group.is_member),
    [],
  );
  await capture(page, `${suffix}-client-membership-none`);
  const first = groupIDs[0];
  assert.ok(first);
  await request(page, "POST", `/api/groups/${String(first)}/members/${ip}`);
  await page.reload();
  await widget.getByText(names[0] ?? "", { exact: true }).waitFor();
  assert.equal(await widget.locator(".chip").count(), 1);
  const groupDetail = schema.parseGetApiGroupsIdData(
    await request(page, "GET", `/api/groups/${String(first)}`),
  );
  assert.ok(groupDetail.members?.some((client) => client.ip_address === ip));
  await capture(page, `${suffix}-client-membership-assigned`);
  await request(page, "DELETE", `/api/groups/${String(first)}/members/${ip}`);
  await page.reload();
  await widget.getByText("Not in any groups", { exact: true }).waitFor();
  assert.equal(await widget.locator(".chip").count(), 0);
  await capture(page, `${suffix}-client-membership-removed`);
  for (const id of groupIDs)
    await request(page, "DELETE", `/api/groups/${String(id)}`);
}
