# Working on Svart

Guidance for anyone changing this repository, human or coding agent.

## What this is

Svart is a DNS sinkhole (a Pi-hole / AdGuard Home alternative) written in Go:
one binary serving DNS over UDP/TCP and a web UI/API, SQLite for state,
Parquet + embedded DuckDB for long-term query analytics, and a React UI
embedded in the binary. Start with [docs/overview.md](docs/overview.md) and
[docs/architecture.md](docs/architecture.md).

## Commands

| Command | Use |
|---|---|
| `make verify` | the gate every change must pass: gofmt, frontend lint/tests/build, `go vet`, Go tests |
| `make test-go-race` | race detector; run it for anything touching concurrency, caches, the resolver or sync |
| `make e2e` | fresh offline browser, DNS, peer and persistence world; see [scripts/e2e/README.md](scripts/e2e/README.md) |
| `make test-hermetic` | unit tests with no network (they must never need one) |
| `make security` | npm and Go vulnerability audits, license gate |
| `make bench-throughput`, `make bench-footprint`, `make bench-compare` | performance evidence; see [docs/benchmarks.md](docs/benchmarks.md) |
| `make docs` | regenerate OpenAPI, typed frontend clients/parsers, and Markdown reference |
| `scripts/check-public.sh` | fails if private-network details slipped into the tree |

## Rules

- **The DNS path is the product.** Measure before and after any change on it
  (benchmarks in `*_bench_test.go`, `benchmarks/`), record the numbers in the
  commit message or [docs/benchmarks.md](docs/benchmarks.md), and keep
  allocations out of it. Use `clock.Now()` rather than `time.Now()` there.
- **Never lose or truncate data.** Query log rows spill to disk instead of
  being dropped; list downloads that exceed a limit fail and keep the previous
  version instead of keeping a partial list; a failed read is reported as
  unavailable, never as zero.
- **Parse at the boundary, trust inside.** Validate HTTP bodies, environment
  variables, sync payloads and list contents once, where they enter; fail
  loudly with a message that says what to fix.
- **Fail closed.** New network-facing behavior defaults to the safe choice
  (the client ACL, SSRF guard, identity replication off, per-client metrics
  off); opening it up is an explicit setting.
- **Tests use realistic data** (real domain names, list syntax, addresses from
  private or documentation ranges), cover the failure paths, assert exact
  values, and never reach the network. A bug fix lands with a regression test
  that fails without it.
- **Self-host everything.** No CDNs, remote fonts or telemetry in the UI; no
  runtime downloads except blocklists the operator configured.
- **Keep docs next to the code.** A change that alters behavior updates the
  matching page in `docs/` in the same commit; a significant decision gets a
  record in [docs/decisions/](docs/decisions/README.md).

## Deliberate exceptions

- The application package and its same-package tests live in `internal/svart`;
  `cmd/svart-dns` is the executable entrypoint. Pure list parsing, domain
  matching and policy evaluation live in `internal/listparse` and
  `internal/policycore`. Further application decomposition is tracked in
  [docs/roadmap.md](docs/roadmap.md).
- Row IDs are SQLite integer rowids. They never cross node boundaries: sync
  identifies rows by natural keys (alias, address, CIDR), so there is no
  collision or enumeration concern that UUIDs would solve.
