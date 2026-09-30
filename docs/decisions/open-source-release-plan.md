# Open-source release plan

Goal: take Svart from "Chris's homelab DNS" to a public project a stranger can
install in under five minutes, that holds up to a hostile security review, and
that measurably beats Pi-hole, AdGuard Home and Technitium where it claims to.

The original phase plan and dated measurements are retained below. Current
verification supersedes the historical gap list; packaging readiness and
production rollout are separate claims. GitHub publication is outside the
current scope.

## Current verification — 2026-09-29

At source `e6208c3`, the pinned verification toolchain passes strict Go and
frontend linting, formatting, types, generated API drift, race, dependency and
coverage gates. Complete owned Go statement coverage is **80.6520%**
(11,380/14,110); the separate benchmark module is **89.8089%** (282/314).
The frontend has **924 passing tests** and an enforced coverage gate. Supporting
JavaScript has 94.56% statement coverage and Python has 98% coverage. These
figures use the complete owned-file inventories, including platform-inactive Go
statements as uncovered.

A second container reuses prepared dependencies but runs fresh Go tests with
network access disabled, for both Go modules. The real application browser E2E
at `a9185df` covers first-run setup, DNS over UDP/TCP/DoH/DoT, policy edits,
replication, configuration backup/import, restart, Investigation, malicious-text
rendering, failed request/retry behavior, and desktop/mobile controls. Subsequent
changes through `e6208c3` affect CI, benchmark tooling and documentation rather
than application behavior.

The production image built from `a9185df` passed the runtime smoke check,
govulncheck and the container scanner's HIGH/CRITICAL gate. Native Investigation
and real local telemetry collectors have separate runtime evidence. A green
component gate does not by itself establish production deployment.

The final [benchmarks and resource verdict](../benchmarks.md) are recorded,
including adverse results, contention and the limits of the archived-payload
oracle. Full CI runs 328 and 329 passed at `e6208c3`. The actual `a9185df` image
also rendered the five new operator panels, opened a matching Tempo span from
its log row, and passed metrics-absence firing and recovery. All seven alert
rules were healthy after recovery; this does not claim seven fault injections.

Remaining release gates at this checkpoint are recovery fault injection,
verification of the final infrastructure pin, final integrated review/export,
and the staged private deployment. Recovery uses an independently pinned,
qualified compatible image against current storage, preserving the old code and
data as evidence rather than restoring an older database over newer history.
Recovery reboot tests run inside a headless VM; privileged systemd fixtures
must never run directly on a workstation. No GitHub repository or release is
published by these steps.

## Historical status and evidence — 2026-09-26

The following snapshot records what was measured on that date. Its test counts,
coverage shortfalls and benchmark conclusions are historical, not current status.

Completed or directly measured:

- Security/resolver/policy fixes, lossless query spooling, bounded caches,
  first-run setup, source-build onboarding, and generic architecture docs are
  integrated. The security review remains a dated review, not a certification.
- `make verify`, the full Go race suite and the hermetic Go suite pass on the
  integration branch. The frontend suite has 48 passing tests; UI changes have
  browser screenshots. These checks do not constitute comprehensive browser E2E.
  The separate benchmark Go module participates through `make test-benchmarks`;
  public-scanner failure tests participate through `make test-public-check`.
- The [corrected four-product benchmark](../benchmarks.md) retains all outcome
  counts. Technitium leads cached throughput; Svart leads the cache-miss mix and
  fixed-load p99 in one x86 run. Pi-hole had 396/300,000 timeouts at 200 workers.
  This is not evidence that Svart wins every workload.
- Same-harness natural-GC footprint runs ended at 1,467.6 MiB RSS for the retained
  older build and 804.1 MiB for current code. Compiler/dependency differences
  prevent attribution to one optimization. [Pi sizing](../operations.md#resource-use)
  is an x86 inference; the amd64 SQLite scanner extension and hardcoded platform
  path block complete arm64 Investigation support until packaging changes.
- The bundled ranking CSV was removed from the application; operators supply
  their own ranking source. Public export also explicitly excludes the old path.
- Export tooling creates a new local repository from committed tracked files,
  with one initial commit and no remote. Real Git/Gitleaks/Go fixture tests prove
  dirty-source/existing-destination rejection, asset preservation, failed-link
  rejection, secret rejection and source-history preservation. Each application
  export runs against a committed source revision and retains its own evidence;
  source and candidate commit identifiers accompany the result.
- Gitleaks **v8.30.1** was run against the complete candidate source, including
  gzip members. Seven findings were reviewed as exact synthetic test/example
  tokens. The exporter retains the unfiltered findings and applies only specific
  file/rule/value-hash exceptions with written reasons.

Remaining at the 2026-09-26 checkpoint (superseded by current verification above):

- Final measured Go coverage is **67.9%** for the main package and **69.2%** for
  `cmd/healthcheck`, below the 80% standard; coverage enforcement,
  comprehensive browser E2E, strict type-aware frontend linting and the complete
  lint/decomposition work remain incomplete. Passing current `make verify`
  does not imply these stronger gates exist or pass.
- The private production-pipeline transfer has implementation and contract-test
  evidence. Verify production rollout separately before claiming the transferred
  pipeline has deployed; local public export does not perform that rollout.
- Each final export must pass build/tests/license/private-marker/secret/link
  checks, preserve every intended source blob and Git mode, and contain exactly
  one commit with no remote. First-run UI and real DNS behavior must be inspected
  against the exported binary, with evidence retained alongside that candidate.
- No public repository, prebuilt release artifact, arm64 image, or tested Pi
  support is implied. Publication is Chris's subsequent action.

### Reproducing the local public candidate

Commit all intended source changes first. The exporter rejects dirty source and
existing output paths. Use a new destination outside the source tree:

```bash
# Prerequisites: Git, Go, Node/npm, Python 3, make, and Gitleaks v8.30.1.
GITLEAKS_BIN=/path/to/gitleaks make test-export
GITLEAKS_BIN=/path/to/gitleaks scripts/export-public.sh /path/to/new-public-candidate
```

`git archive` carries committed runtime assets, screenshots, docs and tests;
it never copies the original `.git`, remotes or ignored build caches. Only the
private `.forgejo/`, `deploy/` and retired ranking CSV path are excluded.
Submodules are rejected instead of silently exporting empty directories.

The sibling `<destination>.evidence` directory retains the source archive,
source/initial commit identifiers, full private/secret/license/link/build/test
outputs and the built binary. Secret findings are protected by owner-only
permissions and are not committed to the candidate. Failed checks leave their
candidate and evidence for inspection, without a success commit. Never publish
the evidence directory without a separate review. The script does not create a
remote, push, install an image, or deploy. Local Markdown checks validate file
paths; external URLs and heading anchors are explicitly outside that check.

## Baseline (2026-09-25, commit 72d6b60)

| Check | Result |
|---|---|
| `go test .` | pass, 45 s |
| Go coverage | **50.1 %** (standard: ≥ 80 %) |
| Frontend | builds; eslint is not type-checked strict; no Prettier; no coverage gate |
| golangci-lint | not configured |
| gitleaks over full history | 5 hits, all test fixtures / doc examples, **no real secrets** |
| Homelab references in tree | ~60 files (IPs, `example.lan`, family names in tests, runbook, deploy pipeline) |
| Oversized files | `sync.go` 2549, `database.go` 1642, `blocklist.go` 1128, `Tiers.tsx` 1558 |
| First run with no `ADMIN_PASSWORD` | UI is locked with no in-app way to create the first admin |

## Revised execution order (after Phase 0 findings)

Phase 0 turned up more than expected: 2 Critical and 9 High security findings
(`design/SECURITY-REVIEW-2026-09.md`) and a correctness bug (B1: ranges never pick
up refreshed lists). yeti is also far too loaded to benchmark on (load avg 138 on
32 threads, 0 % idle), so every throughput number taken there is INCONCLUSIVE.
Linting and decomposing 35k lines that are about to be rewritten wastes effort, so
the order becomes:

1. **Security fixes** (Phase 2 content): four parallel fix branches, each with its
   regression tests first. Surfaces are split so that no two branches share a
   file owner: `investigate.go`; `sync.go`; auth/API/frontend; DNS server and
   cache; resolver/upstream.
2. **Core redesign** (Phase 3 content): one immutable policy snapshot rebuilt in
   one place (fixes B1 structurally), a compact shared list store (memory, and
   the blocklist/allowlist duplication), a lossless log queue that stays small
   when idle, and bounded caches. Measured on a quiet host: a network-less,
   CPU-pinned temporary LXC on blue.
3. **Standards pass** (Phase 1 content) over the new code: golangci-lint, strict
   TS, coverage ≥ 80 %, decomposition, typed API responses, docs.
4. Onboarding/UX, positioning, packaging (Phases 4–6).

## Phase 0: Baseline & harness (no behavior change)

- Pin tooling: golangci-lint, govulncheck, gitleaks, Prettier, typescript-eslint strict-type-checked.
- Record baselines: Go micro-benchmarks, `make bench-e2e` (Svart vs Pi-hole vs AdGuard), RSS with a ~500 k-domain list, list-load and startup time, dashboard endpoint latency on a synthetic 8-day/50 M-row database.
- Add **Technitium** to the E2E benchmark harness.
- Add a pprof endpoint (admin-only) and a load-profile script so every performance claim has a profile behind it.

## Phase 1: Standards foundation

- golangci-lint (errcheck, staticcheck, gosec, revive, …) to zero findings, fixing code rather than suppressing.
- Frontend: Prettier + strict type-checked ESLint + `tsc --strict`, coverage gate.
- Coverage to ≥ 80 % for Go and the frontend, with real behavioral tests (the policy engine, parsers, sync merge, auth), not padding.
- Decompose the >1000-line files. Pull the pure core (domain matching, list parsing, policy evaluation) into `internal/` packages with characterization tests pinning current behavior first.
- Delete dead code: the `resolveParallel` scaffolding, and the ~90 % duplicate blocklist/allowlist code, which becomes one generic list store.
- Standard docs set: `AGENTS.md`, `docs/{overview,architecture,getting-started,code-layout,operations}.md`, DD-xxx → `docs/decisions/`.
- OpenAPI drift gate in CI (swag regenerate + diff).

## Phase 2: Adversarial security review

Parallel reviewers, one per surface. Each finding gets a failing test, then the fix.

1. **DNS protocol:** open-resolver defaults, amplification (ANY, large EDNS), per-client rate limiting, TCP slowloris/pipelining limits, cache poisoning (ID/port randomization, bailiwick, case), malformed-packet fuzzing (go-fuzz on the handler), rebinding protection.
2. **Auth & sessions:** cookie = HMAC(passwordHash) with no server-side expiry, CSRF on cookie-auth mutations, login brute force, token handling, role matrix per route (generated table + test).
3. **HTTP API:** SSRF via blocklist URLs (admin fetches `http://169.254.169.254`, internal hosts, redirects), download size/decompression limits, config-import validation, path handling, headers (CSP, frame, HSTS).
4. **Sync mesh:** DD-029 admin backdoor. For a public release, default to **not replicating identity tables** unless opted in.
5. **Investigate / DuckDB:** re-attack the SQL sandbox; supply-chain check of the vendored `sqlite_scanner` binary (checksum, provenance, rebuild instructions).
6. **Supply chain & container:** govulncheck, npm audit, pinned base images by digest, non-root + `CAP_NET_BIND_SERVICE` for :53, read-only rootfs.

## Phase 3: Performance

Profile first, then change one thing at a time, recording before/after in `design/BENCHMARKS.md`. Candidate hypotheses, which only ship if the numbers move:

- Cache-hit fast path: serve pre-packed wire bytes with the ID patched (skip unpack/pack).
- `SO_REUSEPORT` multi-socket UDP listeners (one per core).
- Pooled message/buffer reuse to cut allocations per query.
- `singleflight` coalescing of concurrent identical cache misses.
- Prefetch-before-expiry + serve-stale (RFC 8767) so popular names never pay upstream latency.
- Blocklist memory/lookup layout (label-reversed suffix structure vs map-per-level), list load time.
- Log-writer throughput (prepared multi-row inserts), dashboard queries on large DBs.
- Measure on cold misses, hits, blocked, and mixed traffic, plus RSS, against all three competitors.
- **Memory footprint is a first-class target** (Chris: "it originally consumed a TON of memory"). Measure RSS vs list size (0, 100 k, 500 k, 1.5 M domains) and vs cache/log volume, then cut it: list representation, cache sizing, DuckDB/SQLite limits, log-writer channel. Publish a footprint table and an honest Raspberry Pi verdict (runs / struggles / doesn't) in the docs. Ship arm64 only if the numbers support it.

## Phase 4: Onboarding & UX for strangers

- **First-run setup wizard** (AdGuard-style): a one-time setup token printed to the logs → create admin → pick an upstream preset (privacy-spread DoH default) → pick a blocklist preset → "point your router here" screen with detected IP.
- Root `compose.yaml` + a `docker run` one-liner that listens on :53, with persistent volumes and sensible defaults. Every env var becomes optional.
- Multi-arch image (amd64 + arm64) and release binaries.
- UI reorganization using real screenshots of the current app, driven by the `impeccable` critique: clearer navigation, plain-language names (e.g. "Tiers" → "Policies", "Investigation" → "SQL Explorer"), empty states, inline help.
- Playwright E2E through the wizard → a blocked query shows up in the log → a policy change takes effect.

## Phase 5: Positioning & docs

- README: pitch, screenshot, 60-second quickstart, and an **honest** comparison table vs Pi-hole / AdGuard Home / Technitium, including where they win (DHCP, authoritative zones, community size, client-facing DoH), with reproducible benchmark numbers.
- The differentiators, stated plainly: three-tier policy cascade, analytics + SQL over years of history, blocklist intelligence (matrix/compare/simulate), leaderless multi-node HA sync, API-first with OpenAPI, doom-loop defense, speed.
- `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, issue/PR templates, third-party notices (Tranco, RapiDoc, fonts, DuckDB extension).

## Phase 6: Public packaging

- GitHub Actions: CI gate + release (image to GHCR by digest, binaries). Forgejo stays the source of truth.
- Strip homelab specifics (fixtures, docs, `.env.example`).
- Move the homelab deploy pipeline (`deploy/`, `.forgejo/workflows/deploy.yml`, `runner-smoke.yml`, `design/RUNBOOK.md`, infra notes) into `infra-definitions`, so this repo is generic. No production deploy is dispatched as part of this.
- Export script → clean public repo with **fresh history** (one initial commit) + a final gitleaks/grep gate on the export.
- Chris does the actual `git push` to GitHub (publishing is his call).

## Decision log

| Date | Decision | Why | Evidence |
|---|---|---|---|
| 2026-09-25 | Plan written; baseline recorded | user asked for a phased plan first | table above |
| 2026-09-25 | Public repo gets fresh history | old commits carry homelab IPs/hostnames/runbook; gitleaks found no real secrets | Chris's answer |
| 2026-09-25 | Homelab deploy moves to infra-definitions | repo must be generic; IaC rule | Chris's answer |
| 2026-09-25 | arm64 conditional on measured memory footprint; document Pi feasibility honestly | Chris: memory was the historical problem | Chris's answer |
| 2026-09-25 | Name stays Svart / svart-dns | Chris's answer | — |
| 2026-09-25 | Baseline footprint recorded: 150 MB RSS idle (76 MB = preallocated 10M-slot log channel), 703 MB with 3.1M list domains, +590 MB for 200k cached names | first-class memory target | `benchmarks/footprint.sh`, heap profiles |
| 2026-09-25 | Throughput on yeti marked INCONCLUSIVE (load 138/32 threads; the trivial stub itself only reaches 25k qps) | wrong surface | `uptime`, stub run |
| 2026-09-25 | Security fixes before lint/decomposition | don't polish code that's about to be rewritten | 2 Critical + 9 High findings |
