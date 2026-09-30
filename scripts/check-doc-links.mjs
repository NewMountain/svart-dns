// Check repository-local Markdown links. External links are not fetched.
import { existsSync, readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import path from "node:path";

const files = execFileSync("git", ["ls-files", "-z"], { encoding: "utf8" })
  .split("\0")
  .filter((file) => file.endsWith(".md"));
const root = process.cwd();
let errors = 0;
let checked = 0;
for (const file of files) {
  let fence = "";
  const lines = readFileSync(file, "utf8").split("\n");
  for (const [index, line] of lines.entries()) {
    const marker = line.match(/^\s*(`{3,}|~{3,})/);
    if (marker?.[1]) {
      if (!fence) fence = marker[1].charAt(0);
      else if (marker[1].charAt(0) === fence) fence = "";
      continue;
    }
    if (fence) continue;
    for (const match of line.matchAll(
      /!?\[[^\]]*\]\((?:<([^>]+)>|([^\s)]+))(?:\s+"[^"]*")?\)/g,
    )) {
      const url = match[1] ?? match[2];
      if (url === undefined)
        throw new Error(
          "check-doc-links: link parser did not capture a target",
        );
      if (
        /^[a-z][a-z\d+.-]*:/i.test(url) ||
        url.startsWith("//") ||
        url.startsWith("#")
      )
        continue;
      const target = decodeURIComponent(url.split(/[?#]/)[0] ?? "");
      if (!target) continue;
      const local = target.startsWith("/")
        ? path.resolve(root, target.slice(1))
        : path.resolve(path.dirname(file), target);
      checked++;
      if (
        !(local === root || local.startsWith(`${root}${path.sep}`)) ||
        !existsSync(local)
      ) {
        console.error(
          `${file}:${String(index + 1)}: missing local target ${target}`,
        );
        errors++;
      }
    }
  }
}
if (errors) process.exit(1);
console.log(
  `check-doc-links: ${String(checked)} local file links exist (anchors and external URLs are not checked)`,
);
