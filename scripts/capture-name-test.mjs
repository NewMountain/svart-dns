import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { captureName } from "./capture-name.mjs";

await test("root and nested repeated captures preserve all earlier artifacts", async () => {
  const directory = await mkdtemp(join(tmpdir(), "svart-capture-"));
  try {
    await mkdir(join(directory, "inventory"));
    for (const prefix of ["", "inventory/"]) {
      const requested = prefix + "config";
      assert.equal(await captureName(directory, requested), requested);
      await writeFile(join(directory, requested + ".png"), "first image");
      await writeFile(
        join(directory, requested + "-2.inventory.json"),
        "second inventory",
      );
      await writeFile(
        join(directory, requested + "-3.bounds.json"),
        "third bounds",
      );
      const selected = await captureName(directory, requested);
      assert.equal(selected, requested + "-4");
      await writeFile(join(directory, selected + ".png"), "fourth image", {
        flag: "wx",
      });
      assert.equal(
        await readFile(join(directory, requested + ".png"), "utf8"),
        "first image",
      );
      assert.equal(
        await readFile(
          join(directory, requested + "-2.inventory.json"),
          "utf8",
        ),
        "second inventory",
      );
      assert.equal(
        await readFile(join(directory, requested + "-3.bounds.json"), "utf8"),
        "third bounds",
      );
      assert.equal(await captureName(directory, requested), requested + "-5");
    }
    await assert.rejects(captureName(directory, "missing/capture"), {
      code: "ENOENT",
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
