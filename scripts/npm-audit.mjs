import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ALLOWED_RSC_ADVISORY =
  "https://github.com/advisories/GHSA-qwww-vcr4-c8h2";
const EXCEPTION_EXPIRES_AT = new Date("2026-08-15T00:00:00Z");
const BLOCKING_SEVERITIES = new Set(["high", "critical"]);
const RSC_MARKERS = [
  "react-server-dom",
  "unstable_RSC",
  "RSCStaticRouter",
  "RSCHydratedRouter",
  "createCallServer",
  "routeRSCServerRequest",
];

/** @param {string} version */
function atLeast718(version) {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:-|$)/.exec(version);
  if (!match) return false;
  const major = Number(match[1]);
  const minor = Number(match[2]);
  return major > 7 || (major === 7 && minor >= 18);
}

/** @typedef {{url: string, severity: string}} Advisory */
/** @typedef {{severity: string, via: (string | Advisory)[]}} Finding */
/** @param {string} name @param {Finding} finding */
function isAllowedRouterFinding(name, finding) {
  if (name === "react-router") {
    return (
      finding.via.length > 0 &&
      finding.via.every(
        (via) =>
          typeof via === "object" &&
          via.url === ALLOWED_RSC_ADVISORY &&
          BLOCKING_SEVERITIES.has(via.severity),
      )
    );
  }
  return (
    name === "react-router-dom" &&
    finding.via.length > 0 &&
    finding.via.every((via) => via === "react-router")
  );
}

/** @param {unknown} report @returns {[string, Finding][]} */
function parseFindings(report) {
  if (
    typeof report !== "object" ||
    report === null ||
    !("vulnerabilities" in report) ||
    typeof report.vulnerabilities !== "object" ||
    report.vulnerabilities === null ||
    Array.isArray(report.vulnerabilities)
  ) {
    throw new Error("npm audit did not return a valid vulnerability report");
  }
  return Object.entries(report.vulnerabilities).map(([name, raw]) => {
    /** @type {unknown} */
    const finding = raw;
    if (
      typeof finding !== "object" ||
      finding === null ||
      !("severity" in finding) ||
      typeof finding.severity !== "string" ||
      !["info", "low", "moderate", "high", "critical"].includes(
        finding.severity,
      ) ||
      !("via" in finding) ||
      !Array.isArray(finding.via)
    ) {
      throw new Error(
        `npm audit returned invalid finding metadata for ${name}`,
      );
    }
    const via = finding.via.map((/** @type {unknown} */ item) => {
      if (typeof item === "string") return item;
      if (
        typeof item !== "object" ||
        item === null ||
        !("url" in item) ||
        typeof item.url !== "string" ||
        !("severity" in item) ||
        typeof item.severity !== "string"
      ) {
        throw new Error(
          `npm audit returned invalid advisory metadata for ${name}`,
        );
      }
      return { url: item.url, severity: item.severity };
    });
    return [name, { severity: finding.severity, via }];
  });
}

/** @param {unknown} report @param {{reactRouterVersion: string, rscMatches: string[], now: Date}} context */
export function evaluateAuditReport(
  report,
  { reactRouterVersion, rscMatches, now },
) {
  const blocking = parseFindings(report).filter(([, finding]) =>
    BLOCKING_SEVERITIES.has(finding.severity),
  );
  const unapproved = blocking
    .filter(([name, finding]) => !isAllowedRouterFinding(name, finding))
    .map(([name]) => name)
    .sort();
  if (unapproved.length > 0) {
    throw new Error(
      `unapproved high/critical npm findings: ${unapproved.join(", ")}`,
    );
  }

  if (blocking.length === 0) return { excepted: [] };
  if (!atLeast718(reactRouterVersion)) {
    throw new Error(
      "the temporary RSC exception requires React Router 7.18.0 or newer",
    );
  }
  if (rscMatches.length > 0) {
    throw new Error(
      `RSC APIs are present; advisory is applicable: ${rscMatches.join(", ")}`,
    );
  }
  if (now >= EXCEPTION_EXPIRES_AT) {
    throw new Error(
      "the temporary React Router RSC advisory exception expired",
    );
  }

  return { excepted: blocking.map(([name]) => name).sort() };
}

/** @param {string} root @returns {string[]} */
function findRscMarkers(root) {
  /** @type {string[]} */
  const matches = [];
  if (!fs.existsSync(root)) return matches;
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const item = path.join(root, entry.name);
    if (entry.isDirectory()) {
      matches.push(...findRscMarkers(item));
      continue;
    }
    if (!/\.(?:js|jsx|ts|tsx)$/.test(entry.name)) continue;
    const source = fs.readFileSync(item, "utf8");
    for (const marker of RSC_MARKERS) {
      if (source.includes(marker)) matches.push(`${item}: ${marker}`);
    }
  }
  return matches;
}

function main() {
  const result = spawnSync("npm", ["audit", "--json", "--audit-level=high"], {
    cwd: process.cwd(),
    encoding: "utf8",
  });
  if (result.error) throw result.error;

  if (result.status !== 0 && result.status !== 1) {
    throw new Error(
      `npm audit subprocess failed (exit ${String(result.status)}, signal ${String(result.signal)})`,
    );
  }

  /** @type {unknown} */
  let report;
  try {
    report = JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(
      `npm audit returned invalid JSON: ${error instanceof Error ? error.message : String(error)}`,
      { cause: error },
    );
  }

  /** @type {unknown} */
  const lock = JSON.parse(fs.readFileSync("package-lock.json", "utf8"));
  let reactRouterVersion = "";
  if (
    typeof lock !== "object" ||
    lock === null ||
    !("packages" in lock) ||
    typeof lock.packages !== "object" ||
    lock.packages === null ||
    Array.isArray(lock.packages)
  ) {
    throw new Error("npm audit requires a lockfile with package metadata");
  }
  if ("node_modules/react-router" in lock.packages) {
    const router = lock.packages["node_modules/react-router"];
    if (
      typeof router !== "object" ||
      router === null ||
      !("version" in router) ||
      typeof router.version !== "string"
    ) {
      throw new Error("npm audit requires a string React Router version");
    }
    reactRouterVersion = router.version;
  }
  const evaluation = evaluateAuditReport(report, {
    reactRouterVersion,
    rscMatches: findRscMarkers(path.resolve("src")),
    now: new Date(),
  });

  if (evaluation.excepted.length > 0) {
    console.warn(
      `Temporarily excepted ${ALLOWED_RSC_ADVISORY} for non-RSC Svart; expires ${EXCEPTION_EXPIRES_AT.toISOString()}`,
    );
  } else {
    console.log("npm audit found no high or critical vulnerabilities");
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
