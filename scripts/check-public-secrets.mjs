// Review every finding from an unfiltered, pinned Gitleaks scan. Only exact
// fixture file/rule/value hashes are accepted; the complete raw report survives.
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";

const [reportPath, root] = process.argv.slice(2);
if (!reportPath || !root)
  throw new Error("usage: check-public-secrets.mjs report.json root");
/** @type {unknown} */
const report = JSON.parse(readFileSync(reportPath, "utf8"));
if (!Array.isArray(report)) throw new Error("secret report must be an array");
const findings = report.map((/** @type {unknown} */ finding) => {
  if (
    typeof finding !== "object" ||
    finding === null ||
    !("File" in finding) ||
    typeof finding.File !== "string" ||
    !("Secret" in finding) ||
    typeof finding.Secret !== "string" ||
    !("RuleID" in finding) ||
    typeof finding.RuleID !== "string" ||
    !("StartLine" in finding) ||
    typeof finding.StartLine !== "number" ||
    !Number.isInteger(finding.StartLine)
  ) {
    throw new Error("secret report contains invalid finding metadata");
  }
  return {
    File: finding.File,
    Secret: finding.Secret,
    RuleID: finding.RuleID,
    StartLine: finding.StartLine,
  };
});
/** @type {unknown} */
const fixtureData = JSON.parse(
  readFileSync(
    new URL("./public-secret-fixtures.json", import.meta.url),
    "utf8",
  ),
);
if (!Array.isArray(fixtureData))
  throw new Error("secret fixtures must be an array");
const fixtures = fixtureData.map((/** @type {unknown} */ entry) => {
  if (
    typeof entry !== "object" ||
    entry === null ||
    !("path" in entry) ||
    typeof entry.path !== "string" ||
    !("rule" in entry) ||
    typeof entry.rule !== "string" ||
    !("value_sha256" in entry) ||
    typeof entry.value_sha256 !== "string" ||
    !("reason" in entry) ||
    typeof entry.reason !== "string"
  ) {
    throw new Error("secret fixtures contain invalid metadata");
  }
  return {
    path: entry.path,
    rule: entry.rule,
    value_sha256: entry.value_sha256,
    reason: entry.reason,
  };
});
let rejected = 0;
for (const finding of findings) {
  const file = path.relative(root, finding.File);
  const hash = createHash("sha256").update(finding.Secret).digest("hex");
  const fixture = fixtures.find(
    (entry) =>
      entry.path === file &&
      entry.rule === finding.RuleID &&
      entry.value_sha256 === hash,
  );
  if (fixture) {
    console.log(
      `fixture: ${file}:${String(finding.StartLine)} ${finding.RuleID}: ${fixture.reason}`,
    );
  } else {
    console.error(
      `unreviewed: ${file}:${String(finding.StartLine)} ${finding.RuleID}; inspect the protected raw report`,
    );
    rejected++;
  }
}
if (rejected) process.exit(1);
console.log(
  `check-public-secrets: ${String(findings.length)} findings, all exact reviewed fixtures`,
);
