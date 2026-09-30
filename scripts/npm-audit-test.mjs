import assert from "node:assert/strict";
import { evaluateAuditReport } from "./npm-audit.mjs";

const allowedReport = {
  vulnerabilities: {
    "react-router": {
      name: "react-router",
      severity: "high",
      via: [
        {
          source: 1124282,
          url: "https://github.com/advisories/GHSA-qwww-vcr4-c8h2",
          severity: "high",
        },
      ],
    },
    "react-router-dom": {
      name: "react-router-dom",
      severity: "high",
      via: ["react-router"],
    },
  },
};

assert.deepEqual(
  evaluateAuditReport(allowedReport, {
    reactRouterVersion: "7.18.2",
    rscMatches: [],
    now: new Date("2026-07-31T00:00:00Z"),
  }),
  { excepted: ["react-router", "react-router-dom"] },
);

assert.throws(
  () =>
    evaluateAuditReport(
      {
        vulnerabilities: {
          lodash: {
            name: "lodash",
            severity: "critical",
            via: [
              {
                source: 999,
                url: "https://example.invalid/new-advisory",
                severity: "critical",
              },
            ],
          },
        },
      },
      {
        reactRouterVersion: "7.18.2",
        rscMatches: [],
        now: new Date("2026-07-31T00:00:00Z"),
      },
    ),
  /unapproved high\/critical npm findings: lodash/,
);

assert.throws(
  () =>
    evaluateAuditReport(allowedReport, {
      reactRouterVersion: "7.18.2",
      rscMatches: ["src/rsc.ts: unstable_RSCStaticRouter"],
      now: new Date("2026-07-31T00:00:00Z"),
    }),
  /RSC APIs are present/,
);

assert.throws(
  () =>
    evaluateAuditReport(allowedReport, {
      reactRouterVersion: "7.18.2",
      rscMatches: [],
      now: new Date("2026-08-15T00:00:00Z"),
    }),
  /expired/,
);

console.log("npm audit policy checks passed");
