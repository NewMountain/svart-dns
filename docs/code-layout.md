# Code layout

The DNS application lives in `internal/svart`, with its Go tests beside the
implementation they exercise. `cmd/svart-dns/main.go` is the small executable
entrypoint. Root `assets.go` embeds the React UI, API documentation, and local
DuckDB extension from their existing source directories.

The backend filenames below are relative to `internal/svart` unless another
directory is shown. Files group related features within one application process.

| Area | Start here |
|---|---|
| Process startup, listeners, routing, embedded assets | `main.go`, `tls.go`, `pprof.go` |
| DNS request/response path and client guards | `dns.go`, `dnsguard.go` |
| Upstream parsing, selection, transport, bootstrap | `upstreams.go`, `resolver.go`, `upstream_transport.go`, `bootstrap.go` |
| Answer and policy caches | `cache.go`, `policycache.go`, `runtime_settings.go` |
| Policy cascade and immutable snapshots | `policy.go`, `policy_snapshot.go`, `policies.go` |
| Shared list index, parsing, refresh | `internal/policycore/`, `list_refresh.go`, `list_store.go`, `autorefresh.go`, `internal/listparse/` |
| Lists, clients, groups, ranges, rewrites APIs | `blocklist.go`, `allowlist.go`, `api.go`, `handlers.go`, `ranges.go`, `rewrites.go` |
| SQLite schema and initialization | `database.go`, `database_schema.go` |
| SQLite query views and client/group history | `database_queries.go` |
| Query-log filtering, listing and detail API | `api_query_logs.go` |
| Query logging, overflow spool, log visibility | `logwriter.go`, `logspool.go`, `query_log_policy.go` |
| Summary Parquet archival and SQL investigation | `archiver.go`, `investigate.go` |
| Raw event archive ownership, streaming, retention and verification | `rawarchive.go`, `rawarchive_stream.go`, `rawarchive_worker.go`, `rawarchive_verify.go` |
| List comparison and current-policy simulation | `analysis.go` |
| Dashboard and stats | `dashboard_stats.go`, `dashboard_http.go`, `stats_api.go`, `system_stats.go` |
| First-run setup, users, sessions, tokens | `setup.go`, `auth.go`, `session.go`, `login_limiter.go` |
| HTTP security and configuration import/export | `http_middleware.go`, `config.go` |
| Peer transport, lifecycle and settings | `sync.go` |
| Sync wire models, snapshots and admission | `sync_types.go`, `sync_response.go`, `sync_admission.go` |
| Peer discovery, status and pairing | `sync_peers.go`, `sync_pairing.go` |
| Transactional sync merges | `sync_merge.go`, `sync_merge_rows.go` |
| Logs, metrics, optional Loki export | `logging.go`, `metrics.go`, `loki.go` |

## Web interface

`frontend/src/App.tsx` and `routeComponents.ts` connect routes to pages.
`frontend/src/components/Sidebar.tsx` defines the navigation names. Filter Lists
lives in `pages/Filters.tsx`; Assignments lives in `pages/Tiers.tsx`. Those older
internal filenames do not change the user-facing names.

`frontend/src/api/client.ts` wraps API calls, `hooks/` contains shared hooks,
`lib/setupWizard.ts` holds setup state and presets, and `styles/` contains local
CSS. Tests sit beside the components/helpers they exercise. `frontend/dist/`
is generated and embedded at build time; do not hand-edit it.

The API contract lives in `internal/svart/api_contract_test.go` and the concrete Go request/response
DTOs. `internal/apigen/` uses those types and handler metadata to generate
`docs/swagger.json` (OpenAPI 3.1), `docs/api-reference.md`, and the frontend
`api/generated.ts` runtime validators/types and `api/operations.ts` transport.
Run `make docs` to regenerate, and `make check-api` to check drift and coverage.
The generator is committed with the repository and uses the Go dependencies
locked by `go.mod` and `go.sum`. These targets first build the embedded frontend,
so they also work in a fresh checkout.
No network generator or global CLI installation is required. The self-hosted
endpoints are `/docs`, `/api/openapi.json`, and `/docs.md`; the previous JSON
URLs remain aliases. Unknown request object fields remain accepted for forward
compatibility, while malformed JSON, wrong field types, null bodies, and trailing
JSON values fail at the boundary. `web/static/` contains the local reference UI assets. `web/duckdb-extensions/` contains the bundled SQLite scanner
extension used by Investigation.

## Build, tests, and operations

- `Makefile` contains the local build and verification commands.
- `internal/svart/*_test.go` exercises application behavior and contains unit benchmarks.
- `cmd/svart-dns/` contains the executable entrypoint; `assets.go` owns embedded assets.
- `benchmarks/` contains runnable throughput, footprint, and comparison harnesses.
- `cmd/healthcheck/` builds the container's small health-check binary.
- `Dockerfile` builds the frontend and CGO backend, then packages the runtime.
- `compose.yaml` is the standalone local deployment example.
- `scripts/` contains security, license, and public-content checks.
- Production release tooling lives in the operator’s infrastructure repository;
  application workflows call a pinned version. See [Operations](operations.md).
- `docs/decisions/` records design rationale; `docs/security/` records security
  review context.

For a DNS behavior change, follow `handleDNSRequest` into the policy evaluator
and resolver first. For a UI/API change, follow the page's request through the
routes in `main.go` to its handler and database operation. The existing tests
next to those files are the starting point for preserving behavior.

`internal/listparse` owns list parsing, normalization, and stored-rule conversion.
It takes explicit input and limits and imports only the Go standard library; HTTP,
SQLite, environment configuration, logging, and refresh publication stay in the
application. Parser fixtures and normalization tests run directly in that package.

`internal/policycore` owns the immutable domain index, policy configuration model,
snapshot construction, and evaluation. Inputs are explicit values and rule feeds;
it has no HTTP, SQLite, clock, logger, cache, or runtime-global dependency. The
application owns database transactions, cache generations, and atomic publication.
Policy result JSON fields are unchanged, and callers use the core model directly.
