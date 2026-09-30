import { readdir } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

/**
 * Select a free capture group in the requested directory. E2E captures run
 * sequentially; all three artifacts share the selected name.
 * @param {string} output
 * @param {string} requested
 * @returns {Promise<string>}
 */
export async function captureName(output, requested) {
  const parent = dirname(requested);
  const original = basename(requested);
  const existing = new Set(await readdir(join(output, parent)));
  let name = original;
  for (
    let attempt = 2;
    [".png", ".inventory.json", ".bounds.json"].some((extension) =>
      existing.has(name + extension),
    );
    attempt++
  ) {
    name = original + "-" + String(attempt);
  }
  return join(parent, name);
}
