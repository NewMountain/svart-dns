// Fails when a production frontend dependency is not under an allowed
// permissive license. Svart is MIT; a dependency with usage restrictions
// (e.g. react-apexcharts >= 1.8, "free under $2M revenue") would silently
// bind everyone who runs the UI, so the lockfile is checked on every build.
//
// Usage: node scripts/check-licenses.mjs [frontend/package-lock.json]
import { readFileSync } from "node:fs";

const allowed = new Set([
  "MIT",
  "ISC",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "Apache-2.0",
  "0BSD",
  "CC0-1.0",
  "Unlicense",
  "BlueOak-1.0.0",
  "OFL-1.1",
]);
const lockPath = process.argv[2] ?? "frontend/package-lock.json";
/** @type {unknown} */
const lock = JSON.parse(readFileSync(lockPath, "utf8"));
if (typeof lock !== "object" || lock === null || Array.isArray(lock)) {
  throw new Error("check-licenses: lockfile must be an object");
}
const packages = "packages" in lock ? lock.packages : {};
if (
  typeof packages !== "object" ||
  packages === null ||
  Array.isArray(packages)
) {
  throw new Error("check-licenses: packages must be an object");
}
const bad = [];
let checked = 0;
for (const [path, value] of Object.entries(packages)) {
  /** @type {unknown} */
  const pkg = value;
  if (typeof pkg !== "object" || pkg === null || Array.isArray(pkg)) {
    throw new Error(`check-licenses: invalid package metadata for ${path}`);
  }
  if (
    path === "" ||
    ("dev" in pkg && pkg.dev === true) ||
    ("devOptional" in pkg && pkg.devOptional === true)
  )
    continue;
  checked++;
  const spdx =
    "license" in pkg && typeof pkg.license === "string" ? pkg.license : "";
  // "(MIT OR Apache-2.0)" style expressions pass if any alternative is allowed.
  const options = spdx
    .replace(/[()]/g, "")
    .split(/\s+OR\s+/)
    .map((s) => s.trim());
  if (!options.some((o) => allowed.has(o))) {
    bad.push(
      `${path.replace(/^.*node_modules\//, "")}@${"version" in pkg && typeof pkg.version === "string" ? pkg.version : "unknown"}: ${spdx || "no license field"}`,
    );
  }
}
if (bad.length > 0) {
  console.error(
    `check-licenses: ${String(bad.length)} production dependencies are not under an allowed license:\n  ${bad.join("\n  ")}`,
  );
  console.error(
    "Pin a permissively licensed version or replace the dependency.",
  );
  process.exit(1);
}
console.log(
  `check-licenses: ${String(checked)} production dependencies, all permissively licensed`,
);
