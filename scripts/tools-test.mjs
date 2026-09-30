import assert from "node:assert/strict";
import { spawnSync, execFileSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  writeFileSync,
} from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const scripts = path.dirname(fileURLToPath(import.meta.url));
/** @param {string} name */
function world(name) {
  return mkdtempSync(path.join(os.tmpdir(), `svart-${name}-`));
}
/** @param {string} name @param {string} cwd @param {string[]} [args] @param {NodeJS.ProcessEnv} [extraEnv] */
function run(name, cwd, args = [], extraEnv = {}) {
  return spawnSync(process.execPath, [path.join(scripts, name), ...args], {
    cwd,
    encoding: "utf8",
    env: { ...process.env, ...extraEnv },
  });
}

await test("license CLI checks production dependencies, OR alternatives and missing licenses", () => {
  const root = world("licenses");
  const lock = path.join(root, "lock.json");
  writeFileSync(
    lock,
    JSON.stringify({
      packages: {
        "": { license: "private" },
        "node_modules/build": { dev: true },
        "node_modules/optional-build": { devOptional: true },
        "node_modules/react": { version: "18.3.1", license: "MIT" },
        "node_modules/parser": {
          version: "2.0.0",
          license: "(Unknown OR Apache-2.0)",
        },
      },
    }),
  );
  const good = run("check-licenses.mjs", root, [lock]);
  assert.equal(good.status, 0);
  assert.equal(
    good.stdout,
    "check-licenses: 2 production dependencies, all permissively licensed\n",
  );
  writeFileSync(
    lock,
    JSON.stringify({
      packages: {
        "node_modules/restricted": { version: "1.0.0", license: "Proprietary" },
        "node_modules/missing": { version: "2.0.0" },
      },
    }),
  );
  const bad = run("check-licenses.mjs", root, [lock]);
  assert.equal(bad.status, 1);
  assert.equal(
    bad.stderr,
    "check-licenses: 2 production dependencies are not under an allowed license:\n  restricted@1.0.0: Proprietary\n  missing@2.0.0: no license field\nPin a permissively licensed version or replace the dependency.\n",
  );
});

await test("malformed lockfile cannot be accepted as an empty dependency set", () => {
  const root = world("license-invalid");
  for (const payload of [
    { packages: [] },
    { packages: { "node_modules/bad": null } },
  ]) {
    const lock = path.join(root, "lock.json");
    writeFileSync(lock, JSON.stringify(payload));
    assert.equal(run("check-licenses.mjs", root, [lock]).status, 1);
  }
});

await test("document CLI checks real tracked links, fences, encoded paths and escapes", () => {
  const root = world("docs");
  execFileSync("git", ["init", "-q"], { cwd: root });
  mkdirSync(path.join(root, "docs"));
  writeFileSync(path.join(root, "docs", "space name.md"), "target\n");
  writeFileSync(
    path.join(root, "README.md"),
    "[encoded](docs/space%20name.md#title)\n[angle](<docs/space name.md>)\n[root](/docs/space%20name.md)\n[external](https://example.org)\n[anchor](#intro)\n[protocol](//example.org)\n```md\n[skip](missing.md)\n```\n~~~md\n[skip](missing.md)\n~~~\n",
  );
  execFileSync("git", ["add", "."], { cwd: root });
  const good = run("check-doc-links.mjs", root);
  assert.equal(good.status, 0);
  assert.equal(
    good.stdout,
    "check-doc-links: 3 local file links exist (anchors and external URLs are not checked)\n",
  );
  writeFileSync(
    path.join(root, "README.md"),
    "[broken](missing.md)\n[escape](../outside.md)\n",
  );
  const bad = run("check-doc-links.mjs", root);
  assert.equal(bad.status, 1);
  assert.equal(
    bad.stderr,
    "README.md:1: missing local target missing.md\nREADME.md:2: missing local target ../outside.md\n",
  );
});

await test("secret-review CLI recognizes exact fixtures and never prints rejected secret values", () => {
  const root = world("secrets");
  const fixtureDir = path.join(root, "scripts");
  mkdirSync(fixtureDir);
  copyFileSync(
    path.join(scripts, "check-public-secrets.mjs"),
    path.join(fixtureDir, "check-public-secrets.mjs"),
  );
  writeFileSync(
    path.join(fixtureDir, "public-secret-fixtures.json"),
    JSON.stringify([
      {
        path: "fixture.txt",
        rule: "test",
        value_sha256:
          "2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b",
        reason: "deliberate fixture",
      },
    ]),
  );
  const report = path.join(root, "report.json");
  const secretCLI = path.join(fixtureDir, "check-public-secrets.mjs");
  writeFileSync(
    report,
    JSON.stringify([
      {
        File: path.join(root, "fixture.txt"),
        RuleID: "test",
        Secret: "secret",
        StartLine: 7,
      },
    ]),
  );
  const good = spawnSync(process.execPath, [secretCLI, report, root], {
    encoding: "utf8",
  });
  assert.equal(good.status, 0);
  assert.equal(
    good.stdout,
    "fixture: fixture.txt:7 test: deliberate fixture\ncheck-public-secrets: 1 findings, all exact reviewed fixtures\n",
  );
  writeFileSync(
    report,
    JSON.stringify([
      {
        File: path.join(root, "unknown.txt"),
        RuleID: "test",
        Secret: "do-not-print-this",
        StartLine: 9,
      },
    ]),
  );
  const bad = run("check-public-secrets.mjs", root, [report, root]);
  assert.equal(bad.status, 1);
  assert.equal(bad.stdout, "");
  assert.equal(
    bad.stderr,
    "unreviewed: unknown.txt:9 test; inspect the protected raw report\n",
  );
});

/** @param {string} root @param {unknown} report @param {number} exitCode */
function auditWorld(root, report, exitCode) {
  mkdirSync(path.join(root, "bin"), { recursive: true });
  writeFileSync(
    path.join(root, "bin", "npm"),
    '#!/usr/bin/env node\nprocess.stdout.write(process.env.AUDIT_REPORT ?? ""); process.exit(Number(process.env.AUDIT_EXIT));\n',
  );
  chmodSync(path.join(root, "bin", "npm"), 0o700);
  writeFileSync(
    path.join(root, "package-lock.json"),
    JSON.stringify({
      packages: { "node_modules/react-router": { version: "7.18.2" } },
    }),
  );
  return {
    PATH: `${path.join(root, "bin")}:${process.env.PATH ?? ""}`,
    AUDIT_REPORT: typeof report === "string" ? report : JSON.stringify(report),
    AUDIT_EXIT: String(exitCode),
  };
}

await test("npm CLI rejects subprocess failures and malformed vulnerability boundaries", () => {
  const root = world("audit-errors");
  for (const [report, code] of [
    [{ vulnerabilities: {} }, 42],
    [{ vulnerabilities: { broken: { severity: 5 } } }, 0],
    ["{broken", 0],
  ]) {
    const result = run(
      "npm-audit.mjs",
      root,
      [],
      auditWorld(root, report, Number(code)),
    );
    assert.equal(result.status, 1);
  }
});

await test("npm CLI scans nested source trees and clean reports succeed", () => {
  const root = world("audit-clean");
  mkdirSync(path.join(root, "src", "nested"), { recursive: true });
  writeFileSync(
    path.join(root, "src", "nested", "view.tsx"),
    'export const component = "safe";\n',
  );
  writeFileSync(path.join(root, "src", "readme.md"), "ignored non-code\n");
  const clean = run(
    "npm-audit.mjs",
    root,
    [],
    auditWorld(root, { vulnerabilities: {} }, 0),
  );
  assert.equal(clean.status, 0);
  assert.equal(
    clean.stdout,
    "npm audit found no high or critical vulnerabilities\n",
  );
  const report = {
    vulnerabilities: {
      "react-router": {
        severity: "high",
        via: [
          {
            url: "https://github.com/advisories/GHSA-qwww-vcr4-c8h2",
            severity: "high",
          },
        ],
      },
    },
  };
  writeFileSync(
    path.join(root, "src", "nested", "view.tsx"),
    'const api = "unstable_RSC";\n',
  );
  const rsc = run("npm-audit.mjs", root, [], auditWorld(root, report, 1));
  assert.equal(rsc.status, 1);
  assert.equal(
    rsc.stderr,
    `RSC APIs are present; advisory is applicable: ${path.join(root, "src", "nested", "view.tsx")}: unstable_RSC\n`,
  );
});

await test("every maintained JavaScript tool is in the complete c8 denominator", () => {
  const files = execFileSync(
    "git",
    ["ls-files", "-z", "--", "*.js", "*.mjs", "*.cjs"],
    {
      cwd: path.dirname(scripts),
      encoding: "utf8",
    },
  )
    .split("\0")
    .filter(
      (file) =>
        file !== "" &&
        !new Set([
          "frontend/eslint.config.js",
          "scripts/eslint.config.mjs",
          "scripts/npm-audit-test.mjs",
          "scripts/capture-name-test.mjs",
          "scripts/tools-test.mjs",
          "tests/observability/render.mjs",
          "web/static/rapidoc-min.js",
        ]).has(file),
    )
    .sort();
  assert.deepEqual(files, [
    "scripts/capture-name.mjs",
    "scripts/check-doc-links.mjs",
    "scripts/check-licenses.mjs",
    "scripts/check-public-secrets.mjs",
    "scripts/npm-audit.mjs",
  ]);
});

await test("secret-review CLI fails closed on malformed report fields without disclosing contents", () => {
  const root = world("secrets-invalid");
  const report = path.join(root, "report.json");
  for (const value of [
    {},
    [null],
    [
      {
        File: "fixture",
        RuleID: "test",
        Secret: "do-not-print-this",
        StartLine: "7",
      },
    ],
  ]) {
    writeFileSync(report, JSON.stringify(value));
    const result = run("check-public-secrets.mjs", root, [report, root]);
    assert.equal(result.status, 1);
    assert.equal(result.stdout, "");
    assert.equal(result.stderr.includes("do-not-print-this"), false);
  }
  writeFileSync(report, "[]");
  const empty = run("check-public-secrets.mjs", root, [report, root]);
  assert.equal(empty.status, 0);
  assert.equal(
    empty.stdout,
    "check-public-secrets: 0 findings, all exact reviewed fixtures\n",
  );
});
