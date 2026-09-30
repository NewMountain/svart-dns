# TODO

Historical checklist; checked entries record implementation at the time and
are not evidence that a current release passes every gate. Current commands
are in [Getting started](getting-started.md); validation requirements are in
[Contributing](../CONTRIBUTING.md).

Work items derived from the [VISION](overview.md) and [DECISIONS](decisions/README.md). Organized by phase. Items within a phase can often be parallelized. Each item is broken into concrete sub-tasks.

---

## Phase 1: Solid Foundation

Get the existing codebase tested, the API complete, and the hot path fast. Nothing else matters until DNS queries are fast and reliable, and the API is a first-class citizen.

### 1.1 Test infrastructure

- [x] Create `testutil_test.go` with a helper that spins up a temp SQLite DB, runs `createTables()` and `migrateSchema()`, and returns a cleanup function
- [x] Seed helper that populates realistic test data: 3-4 upstreams (mix of UDP/DoH/DoT), 2-3 blocklists with real domain sets (use subsets of actual Hagezi/OISD lists), a handful of clients, groups, and rewrites
- [x] Ensure the global `db` variable can be swapped per-test without races (or refactor to pass DB as parameter)

### 1.2 Core unit tests

- [x] **Cache tests** (`cache_test.go`): set/get round-trip, TTL expiry (set with short TTL, sleep, verify miss), cleanup removes expired entries only, clear resets stats, concurrent read/write safety
- [x] **Resolver tests** (`resolver_test.go`): `parseUpstreamAddress` for all protocol variants (bare IP, `udp://`, `tcp://`, `tls://`, `https://`, with/without ports, with domain-specific prefix `[/domain/]`), `calculateWeight` with known stats, `weightedRandomSelect` distribution sanity check
- [x] **Blocklist tests** (extend `blocklist_test.go`): `isBlockedForClient` with DB-backed client/group lookups using temp DB — exact match, subdomain match, wildcard, reverse ARPA, IP-based response blocking. Test that a client with no blocklist assignments passes everything. Test that disabling a blocklist stops blocking
- [x] **Rewrite tests** (`rewrites_test.go`): `checkRewrite` with A and AAAA queries, domains not in cache return nil, disabled rewrites ignored, multiple IPs per domain
- [x] **Database CRUD tests** (`database_test.go`): create/read/update/delete for upstreams, blocklists, rewrites, clients, groups, settings. Verify foreign key cascades (delete group removes members and blocklist assignments)

### 1.3 API completeness

- [x] Add `GET /api/blocklists` — returns all blocklists with id, url, alias, enabled, domain_count, last_updated
- [x] Add `GET /api/rewrites` — returns all rewrites with id, domain, ip_addresses, enabled
- [x] Add `GET /api/groups` — returns all groups with id, name, member_count, blocklist_count
- [x] Add `GET /api/clients` — returns all known clients with ip, alias, query_count, last_seen, group memberships
- [x] Add `GET /api/bootstrap` — returns list of bootstrap servers
- [x] Add `GET /api/settings` — returns all settings as key-value pairs
- [x] Fix `handleStats` — currently returns hardcoded `{"queries":0,"blocked":0,"uptime":"0s"}`. Wire it up to real `getDashboardStats()` plus uptime and cache stats
- [x] Standardize JSON response envelope: `{"data": ..., "error": null}` for success, `{"data": null, "error": "message"}` for failure. Apply across all endpoints
- [x] Add `GET /api/query-logs?limit=N&offset=N&client_ip=X&domain=X&blocked=bool` — paginated, filterable query log access. This is critical for the analytics vision

### 1.4 `/health` endpoint

- [x] `GET /health` — public, no auth. Returns only `status`; node identity, operational detail, and topology stay behind authenticated APIs (DD-028)
- [x] Must respond in <1ms (no DB queries on the hot path — read from in-memory state only)

### 1.5 API handler tests

- [x] `handlers_test.go` using `httptest.NewServer` — test every API endpoint, both happy and sad paths
- [x] Test GET/POST/PUT/DELETE for each resource
- [x] Test invalid methods return 405
- [x] Test malformed JSON returns 400
- [x] Test that mutations actually persist (POST then GET verifies data)

### 1.6 Hot path benchmark

- [x] `bench_test.go` with `BenchmarkCacheGet`, `BenchmarkIsBlockedForClient`, `BenchmarkHandleDNSRequest` (using a mock upstream)
- [x] Load the blocklist store with 500k+ domains, benchmark lookup time
- [x] Establish baseline numbers. If cache lookups exceed 1μs or blocklist checks exceed 10μs, optimize before moving on
- [x] Profile with `go tool pprof` — identify any allocations on the hot path

### 1.7 End-to-end benchmark suite

- [x] Custom Go DNS load tester (`benchmarks/dnsbench/main.go`) — p50/p95/p99 latency, throughput, block rates
- [x] Docker Compose setup with svart-dns, Pi-hole, AdGuard Home — all loaded with Hagezi Ultimate
- [x] Orchestration script (`benchmarks/run.sh`) — build → start → wait → benchmark → compare → teardown
- [x] `make bench-compare` target for the current four-product comparison
- [x] Run first benchmark and record baseline numbers in [the benchmark history](benchmarks.md)

### 1.8 SQLite tuning + batch log writer (DD-012)

- [x] SQLite WAL pragmas on all connections (`_journal_mode=WAL`, `_synchronous=NORMAL`, `_mmap_size=256MB`, `_cache_size=64MB`)
- [x] Batch log writer (`logwriter.go`): channel-based (10M buffer), batched transactions (5k rows or 100ms timer), dedicated writer DB connection
- [x] Replace goroutine-per-query `logQuery()` with channel send to batch writer
- [x] Graceful shutdown: channel drain + final flush before close, zero data loss
- [x] Tests: basic write, batch flush, timer flush, graceful shutdown with 50k entries

---

## Phase 2: Policy Engine

Rework blocking from "union of all lists" to the three-tier cascading policy system. This is the biggest differentiator from PH/AG. See [DD-002](decisions/DD-002-three-tier-policy-cascade-range-group-ip.md).

### 2.1 Allowlist support

- [x] New `allowlists` table (mirrors `blocklists`: id, url, alias, enabled, domain_count, last_updated)
- [x] New `allowed_domains` table (mirrors `blocked_domains`: id, allowlist_id, domain)
- [x] New `client_allowlists` and `group_allowlists` junction tables
- [x] In-memory store: `allowlistStorage` mirroring `blocklistStorage` — per-client allowed domain sets
- [x] API endpoints: full CRUD for allowlists, assign/unassign to clients and groups
- [x] Allowlist refresh (fetch from URL, parse, same as blocklists)
- [x] Manual per-client and per-group domain allow entries (not just URL-based lists) — for one-off exceptions like "Sophie needs proctoring-app.edu"

### 2.2 VLAN/Range tier

- [x] New `ip_ranges` table: id, name, cidr, created_at (e.g. name="IOT Jail", cidr="10.42.3.0/24")
- [x] New `range_blocklists` and `range_allowlists` junction tables
- [x] `matchRange(clientIP)` function using `net.IPNet.Contains` — returns the matching range (or nil)
- [x] API endpoints: CRUD for ranges, assign/unassign blocklists and allowlists to ranges
- [x] Handle overlapping CIDRs: all matching ranges evaluated, block wins across entities. If a client matches both `10.42.0.0/16` and `10.42.3.0/24`, both are consulted and block wins the tie

### 2.3 Cascading resolution engine

- [x] New `evaluatePolicy(clientIP, domain)` function replacing `isBlockedForClient`:
  1. Find matching range (most specific CIDR match)
  2. Find client's groups
  3. Collect block/allow decisions at each tier (range, group, IP)
  4. Walk narrowest-to-broadest: if an explicit decision exists at a narrower tier, it wins
  5. Return: `{blocked bool, tier string, source string, rule string}` — enough detail for debugging and logging
- [x] Update `handleDNSRequest` to use `evaluatePolicy` instead of `isBlockedForClient`
- [x] Update query logging to record which tier/rule caused the block (useful for analytics)
- [x] Rebuild in-memory stores to support the new structure — per-tier data (range/group/IP) with `loadPolicyData()` consolidating group-level data and client-group memberships

### 2.4 Policy evaluation API

- [x] `GET /api/policy/evaluate?client_ip=X&domain=Y` — returns the full decision chain with `PolicyResult` including blocked, allowed, tier, source, list_name, rule, and full chain of `TierResult` entries
- [x] This is the debugger for "why is this being blocked?" — essential for sanity

### 2.5 Cascading policy tests

- [x] Test matrix with temp DB:
  - Range blocks + Group allows → allowed (narrower wins)
  - Group blocks + IP allows → allowed (narrower wins)
  - Group allows + IP blocks → blocked (narrower wins)
  - No policy at any tier → default allow (pass to upstream)
  - Overlapping CIDR ranges → most specific wins
  - Client in multiple groups with conflicting policies → block wins across entities within tier (documented in DD-002)
  - Allow + block same tier same client → allowed (allow wins)
  - Cache hit verification
  - Chain contains all tiers
  - Range block only
  - Unknown client outside any range
  - Backward compatibility (isBlockedForClient wrapper)
- [x] Policy evaluation benchmarks: cached hit ~130ns, cold miss ~1.3μs, matchRange ~28ns

### 2.6 Published list attribution + decision audit trail

- [x] Add `attribution` map to `clientBlockData`/`clientAllowData` — tracks which published list each domain came from
- [x] `blocklistNameCache`/`allowlistNameCache` (`atomic.Value`) for list ID → alias lookups
- [x] `sortListIDsByCount` + `addBlockDomainsWithAttribution`/`addAllowDomainsWithAttribution` helpers — smallest list gets attribution for overlapping domains
- [x] Update all 6 loading sites (loadBlocklistsFromDB, loadAllowlistsFromDB, loadGroupPolicyData, loadRangesFromDB) to iterate per-list with attribution
- [x] `checkBlockDomainMatch`/`checkAllowDomainMatch` return list ID as 3rd value
- [x] Restructured `PolicyResult`: named tier fields (`range_evaluation`, `group_evaluation`, `ip_evaluation`), `PublishedHit`/`CustomHit` types, single `result` string replacing `Blocked`/`Allowed` booleans
- [x] `buildEntityResult` evaluates both block+allow per entity (no short-circuit), categorizes published (listID>0) vs custom (listID==0)
- [x] Decision audit trail: 3 new `query_logs` columns (`block_source`, `block_list_id`, `block_list_name`), logged for every DNS query via async batch writer
- [x] `logQuery` accepts `*PolicyResult` directly, extracts all fields from `ResultSource`
- [x] Attribution + audit trail tests: SmallestListWins, ManualBlockNoAttribution, DecisionAuditTrail

### 2.7 Query log archival (DD-013, DD-025)

- [x] Default `log_retention_days` changed from 7 to 1095 (3 years hard limit)
- [x] `archiver.go`: background daily worker, monthly Parquet export for data >8 days old (was 730)
- [x] Streaming export in 10k-row batches, atomic write (temp file + rename), Snappy compression
- [x] `ARCHIVE_PATH` env var (default `./archives`)
- [x] API: `POST /api/archive` (manual trigger), `GET /api/archive/status`
- [x] Idempotent: existing Parquet file skips re-export
- [x] Graceful shutdown: `closeArchiver()` called from `closeDatabase()`
- [x] Tests: archive month, idempotent, status
- [x] Daily at midnight (user timezone → server local → UTC). VACUUM after archive
- [x] `coalesced_count` column in Parquet schema

### 2.8 Same-tier conflict prevention

- [x] When adding a custom allow domain to a client/group/range, check if a custom deny for the same domain already exists at that tier entity. Reject with 409 Conflict if so
- [x] When adding a custom deny domain, same check against existing custom allows
- [x] Wildcard overlap detection: `domainConflicts()` checks exact match, subdomain relationship (both directions), and `*.X` covering `Y.X` patterns. Prevents adding `block app.tiktok.com` when `allow *.tiktok.com` exists (and vice versa)
- [x] `block-domain` convenience endpoints for clients (`POST/DELETE /api/clients/{ip}/block-domain`) and groups (`POST/DELETE /api/groups/{id}/block-domain`), mirroring the existing `allow-domain` endpoints
- [x] Policy evaluation endpoint enriched with per-entity breakdown (`EntityResult`) showing every entity consulted, not just the winner

---

## Phase 3: Blocklist Intelligence

Move from "would this domain be blocked?" to "what would this list have done to my traffic last week?" See [DD-001](decisions/DD-001-blocklist-analysis-is-on-demand-not-materialized.md).

### 3.1 Domain × List matrix

- [x] `POST /api/analysis/matrix` — batch domain check against all/specified blocklists
- [x] Request: `{"domains": [...], "blocklist_ids": [1,2,3]}` (blocklist_ids optional, defaults to all enabled)
- [x] Response: lists sorted by domain_count ascending, boolean matrix, matched_rules with the specific rule that triggered each hit
- [x] `domainMatchesClassified` + `findMatchedRule` shared helpers for exact/subdomain/wildcard matching against `classifiedDomains`
- [x] Max 10,000 domains per request
- [x] Tests: basic, defaults-to-all-lists, sorted-by-size, subdomain-match, empty-domains (400), cap (400)

### 3.2 Blocklist comparison

- [x] `POST /api/analysis/compare` — pairwise overlap matrix for selected blocklists
- [x] Request: `{"blocklist_ids": [1,2,3]}` (minimum 2)
- [x] Response: per-list info, symmetric overlap matrix (diagonal = self count), unique-to-list counts, union size
- [x] Implementation: iterate smaller set per pair, O(min(|A|,|B|)) lookups
- [x] Tests: overlap, unique, union-size, requires-two (400), unknown-list (400)

### 3.3 Policy simulator

- [x] `POST /api/analysis/simulate` — batch `evaluatePolicyFull` for a client IP
- [x] Request: `{"client_ip": "10.42.1.42", "domains": [...]}`
- [x] Response: full `PolicyResult` per domain with range/group/ip tier breakdowns
- [x] Uses `evaluatePolicyFull` directly (bypasses LRU cache for fresh policy state)
- [x] Max 10,000 domains per request
- [x] Tests: basic, tier-breakdown, unknown-client, missing-ip (400)

### 3.4 Domain prefill sources

- [x] `GET /api/analysis/domains?source=X&limit=N` — domain lists for prefilling matrix/simulator
- [x] `source=top-sites` — operator-provided local ranking CSV (`TOP_SITES_PATH`), validated on demand; unavailable sources return 503
- [x] `source=top-queried` — top N most-queried domains from query_logs (last 7 days)
- [x] `source=top-blocked` — top N most-blocked domains (last 7 days)
- [x] `source=top-allowed` — top N most-queried non-blocked domains (last 7 days)
- [x] Tests: top-sites, top-queried, top-blocked, invalid-source (400)

### 3.5 What-If per-client

- [x] Extended `GET /whatif?domain=X` to accept optional `client_ip=Y` parameter
- [x] Refactored domain matching to use `domainMatchesClassified` + `findMatchedRule` (replaces manual raw domain map iteration)
- [x] When client_ip is provided, runs `evaluatePolicyFull` and displays policy result card in the template
- [x] Template updated with client_ip input field and policy result display (tier, source, rule)

### 3.6 Retroactive traffic analysis (future)

- [x] `POST /api/analysis/replay` — replay queries against different blocklists
- [x] Response: per-list breakdown (total queries, blocks, unique domains, overlap, marginal gain)

### 3.7 Blocklist auto-refresh with change tracking

- [x] Configurable `refresh_interval` per blocklist/allowlist (0=off, seconds)
- [x] Background worker (`autorefresh.go`) — 60s ticker, stale list detection, sequential refresh
- [x] Prometheus metrics: successes/errors counters, stale gauge
- [x] Sync + config export/import support for refresh_interval
- [x] Frontend: per-list refresh interval dropdown (Off/12h/Daily/3d/Weekly)
- [x] Track added/removed domains per refresh (existing `blocklist_history` table)
- [x] New `blocklist_changelog` table — stores full diffs (every added/removed domain per refresh)
- [x] `GET /api/blocklists/history/{historyId}/domains` — checkpoint domain browser with backward reconstruction from current state through changelog diffs. Search, pagination supported

---

## Phase 4: API-First + Auth

Lock down the API and make it genuinely useful for automation.

### 4.1 Authentication

- [x] `api_tokens` table: id, name, token_prefix, token_hash (bcrypt), role (readonly/admin), created_at, last_used_at
- [x] Auth middleware: `apiAuth` (GET=readonly, mutations=admin), `readonlyAuth` (POST analysis endpoints), `adminAuth` (tokens, config, cache clear). Check `X-Api-Key` header → prefix lookup → bcrypt verify. Reject with 401 if invalid, 403 if wrong role
- [x] `/health`, `/metrics`, `/docs`, `/api/auth/login`, and `/api/auth/check` exempt from auth
- [x] DB-backed multi-user browser auth with bcrypt passwords and a persisted, rotatable HMAC session key; API tokens remain independent credentials
- [x] Fail-closed bootstrap: an empty database seeds its first administrator only when `ADMIN_PASSWORD` is explicitly nonempty; otherwise protected endpoints remain locked
- [x] Admin user CRUD with role and password updates, self-delete prevention, sync/tombstone support, and no password hash in API JSON

### 4.2 Role-based access

- [x] Two roles: `readonly` (GET on all endpoints, POST on analysis/policy/evaluate) and `admin` (everything)
- [x] Token management API (admin only): `GET /api/tokens` (list), `POST /api/tokens` (create, returns plaintext once), `DELETE /api/tokens/{id}` (revoke)
- [x] Role check in middleware based on token lookup. Prefix-based pre-filtering before bcrypt (~100ms per comparison)

### 4.3 OpenAPI spec

- [x] swaggo annotations on all ~30 API handlers (auto-generated `docs/swagger.json` via `swag init`)
- [x] `GET /docs` serves RapiDoc viewer (self-hosted, no CDN) pointing at `/docs/swagger.json`
- [x] `make docs` target to regenerate spec
- [x] Spec documents request/response schemas, auth requirements (`@Security ApiKeyAuth`), tags by resource

### 4.4 Rewrite CRUD API

- [x] Rewrites API fully RESTful: GET (list all), POST (create), PUT (update), DELETE
- [x] `POST /api/rewrites/batch` — batch create up to 1000 rewrites at once
- [x] POST/PUT return the created/updated rewrite object (not just `{success: true}`)

### 4.5 Bulk operations

- [x] `POST /api/groups/{id}/blocklists/batch` — assign multiple blocklists at once
- [x] `POST /api/groups/{id}/members/batch` — add multiple clients at once
- [x] `GET /api/config/export` — full config dump as JSON (upstreams, blocklists, allowlists, rewrites, clients, groups, ranges, settings, bootstrap servers). References lists by alias for portability
- [x] `POST /api/config/import` — restore from export. Validates JSON, applies in transaction, reloads all in-memory stores. Idempotent (update on conflict by alias)

---

## Phase 5: Observability (LGTM)

Make svart-dns a first-class citizen in the homelab monitoring stack.

### 5.1 Structured logging ✅

- [x] Switch from `log.Printf` to a structured logger (slog from stdlib, available since Go 1.21)
- [x] JSON output mode (enabled via `LOG_FORMAT=json` env var, default text for development)
- [x] Consistent fields on every log line: `timestamp`, `level`, `component` (dns/admin/cache/sync), `msg`
- [x] DNS query logs include: `client_ip`, `domain`, `query_type`, `action` (allow/block/rewrite/cache_hit), `upstream`, `latency_microseconds`, `tier`, `rule`
- [x] 12 component loggers: dns, admin, cache, policy, logwriter, archiver, resolver, auth, db, blocklist, allowlist, config
- [x] Hot path guards: `dnsDebugEnabled` flag + `LogAttrs` for zero allocation at Info level
- [x] Full `PolicyResult` serialization in per-query slog emission (off hot path, in logwriter flush)
- [x] `latency_ms` → `latency_microseconds` everywhere (DB column, struct fields, Parquet, API JSON keys)
- [x] Always-present tier evaluations + default allow result source in PolicyResult

### 5.2 Prometheus metrics ✅

- [x] `GET /metrics` endpoint in Prometheus exposition format (exempt from auth, like `/health`)
- [x] Counters (logwriter flush — background goroutine): `svart_dns_queries_total{action}`, `svart_dns_client_queries_total{client_ip,action}`, `svart_dns_blocks_by_tier_total{tier}`, `svart_dns_blocks_by_list_total{list_name}`, `svart_dns_query_types_total{type}`, `svart_dns_response_codes_total{rcode}`
- [x] Histograms: `svart_dns_query_duration_seconds` (14 buckets, 50μs–1s), `svart_dns_upstream_duration_seconds{upstream}` (11 buckets, 1ms–2.5s)
- [x] Upstream counters (resolver.go — cache miss only): `svart_dns_upstream_queries_total{upstream}`, `svart_dns_upstream_errors_total{upstream}`
- [x] Scrape-time gauges: cache entries/hits/misses, policy cache entries/bytes/hits/misses, uptime, log writer queue length + dropped
- [x] Per-list inventory gauges: `svart_dns_blocklist_domains{list_name}`, `svart_dns_allowlist_domains{list_name}` — updated on list reload
- [x] `github.com/prometheus/client_golang` library (self-contained, no telemetry). See DD-015

### 5.3 OpenTelemetry traces — SKIPPED (DD-017)

DNS queries are single-hop leaf-node operations with no trace propagation. A trace with one span is a fancy log line. Structured per-query logs in Loki already contain every field a trace would. See DD-017.

### 5.4 Log shipping ✅

- [x] Custom `slog.Handler` wrapper (`loki.go`) — delegates to inner handler for stdout, queues JSON entries for Loki push
- [x] Direct Loki push via `LOKI_URL` env var — eliminates need for Promtail/Alloy sidecar on bare-metal Hypervisor nodes
- [x] One Loki stream per component: `{job="svart-dns", instance="<NODE_ID>", component="dns|admin|cache|..."}`
- [x] Background goroutine batches by component, flushes every 1s or 1000 entries
- [x] 100k-entry buffered channel, silent drop on overflow, single retry with 500ms backoff
- [x] `NODE_ID` env var (defaults to hostname) for instance label
- [x] Zero overhead when disabled (`LOKI_URL` empty — no handler installed)
- [x] Graceful shutdown: channel drain + final flush before exit. See DD-016

### 5.5 Grafana dashboard ✅

- [x] Version-controlled dashboard at `infra-definitions/lgtm/grafana/dashboards/svart-dns.json`
- [x] Red/Blue availability, peer health, query rate, latency, cache, resource, query type, response code, and upstream panels
- [x] Alloy static HTTPS scrape targets for both Red and Blue
- [ ] Replace Alloy's `insecure_skip_verify = true` with certificate-valid scrape identities
- [ ] Change the Svart alert so no data and rule execution errors do not resolve to `OK`
- [ ] Authenticate or privacy-minimize `/metrics`; several non-peer labels still expose client, upstream, or policy identity

### 5.6 HTTP request logging ✅

- [x] `requestLoggingMiddleware` wraps admin server mux — logs method, path, status, duration_ms, client_ip
- [x] Skips noise: `/health`, `/metrics`, `/api/sync`, static assets (`/assets/`, `/static/`)
- [x] `statusCapture` ResponseWriter wrapper captures response status code
- [x] Sync merge logs per-table change counts (e.g., `settings=2 rewrites=5`)
- [x] DNS query slog includes `upstream` field (resolver name or "cache" for cache hits)

---

## Phase 6: Multi-Instance HA ✅

Active/passive failover. Run on every Hypervisor node. See [DD-003](decisions/DD-003-multi-instance-sync-via-lww-registers-no-leader-election.md) and [DD-018](decisions/DD-018-tombstone-table-for-sync-deletes.md).

### 6.1 Schema changes for sync

- [x] Add `updated_at DATETIME` and `node_id TEXT` to all 17 config tables via ALTER TABLE migrations
- [x] `syncNow()` helper returns `(RFC3339Nano timestamp, nodeID)` tuple — every INSERT/UPDATE sets both
- [x] `sync_tombstones` table for propagating deletes (DD-018) — separate from config tables, no changes to SELECTs
- [x] `recordTombstone()` and `recordTombstonesForParent()` helpers for delete propagation with junction rows

### 6.2 TLS + peer discovery

- [x] `TLS_CERT` + `TLS_KEY` env vars — admin server uses `ListenAndServeTLS` when set
- [x] `PEERS` requires TLS — binary refuses to start if PEERS set without certs
- [x] Peer URLs must use `https://`; `TLS_CA` for optional self-signed cert verification
- [x] Verify sync certificate chains and hostnames; `SYNC_TLS_SERVER_NAME` supplies the certificate identity when the peer transport URL uses an IP (DD-028)
- [x] Canonicalize peer URLs to exact HTTPS host+port origins; reject userinfo, non-root paths, queries, and fragments
- [x] Require a nonempty `SYNC_PEER_ALLOWLIST` across manual add, pairing, gossip, and sync transport so only pre-authorized destinations can receive peer traffic
- [x] Config via env var: `PEERS=https://10.42.1.5:3000,https://10.42.1.6:3000`
- [x] `GET /api/peers` — returns list of configured peers with health status
- [x] Reuses `NODE_ID` env var from Phase 5.4 Loki (defaults to hostname)

### 6.3 Sync API

- [x] `GET /api/sync?since=<rfc3339_timestamp>` — returns all config rows with `updated_at > since`, using natural keys (not IDs)
- [x] Response includes `server_time` as cursor for next request
- [x] Tombstones included for delete propagation
- [x] `syncAuth` middleware: peer-only `X-Sync-Key` header (shared `SYNC_SECRET`); admin tokens and sessions are not a fallback
- [x] Omit `sync_secret` and `session_secret` from replication payloads and ignore either key defensively during merge

### 6.4 Sync worker + merge logic

- [x] One goroutine per peer, polls at `SYNC_INTERVAL` (default 2s)
- [x] Validate `SYNC_INTERVAL` as empty/default or 1s–1h at API, env seed, config import, replication, and startup boundaries
- [x] LWW per row: incoming `updated_at > local updated_at` → UPDATE, not found → INSERT
- [x] Natural key resolution for junction tables (same pattern as config import)
- [x] Tombstone application: hard-delete locally, record tombstone for propagation
- [x] Full reload of all in-memory stores after merge
- [x] Track `last_sync_at` and `last_sync_rows` per peer

### 6.5 Peer health monitoring

- [x] Sync poll doubles as health check — single error marks peer unhealthy (immediate detection)
- [x] Peer health exposed through authenticated `/api/peers`; public `/health` remains minimal
- [x] Peer state tracks `NodeName` (from sync response) and rolling 24h change counter
- [x] Prometheus metrics: `svart_dns_sync_polls_total`, `svart_dns_sync_rows_received_total`, `svart_dns_sync_errors_total`, `svart_dns_sync_last_success_timestamp`, `svart_dns_peer_healthy`

### 6.6 Tombstone garbage collection

- [x] Hourly ticker, tombstones >24h old AND past all peers' last sync → hard-deleted
- [x] If no peers configured, clean unconditionally after 24h

### 6.7 Replication UI + runtime sync config

- [x] Sync config stored in `settings` table (`sync_peers`, `sync_secret`, `sync_interval`) — DB-backed, LWW sync for free
- [x] Env vars (`PEERS`, `SYNC_SECRET`, `SYNC_INTERVAL`) seed DB on first run, DB owns config after that
- [x] `restartSync()` for hot-reload: close old workers, re-read config from DB, start fresh workers
- [x] Settings change hook: accepted `PUT /api/settings/sync_*` updates trigger `restartSync()`
- [x] Sync merge restart: when synced settings include sync-related keys AND LWW actually changes them, workers restart with new config
- [x] Extended `GET /api/peers` with `node_name`, `changes_24h`, `sync_interval`, `has_secret`, `tls_configured`, and observed `sync_tls_ready` fields; rollout gates also require an advancing peer timestamp
- [x] `POST /api/peers` — add peer (validates HTTPS, appends to `sync_peers` setting)
- [x] `DELETE /api/peers/{url}` — remove peer
- [x] Admin page Replication card: live peer health table (5s polling), inline interval/secret edit, add/remove peers
- [x] Frontend `NodeNameProvider` loads node identity from authenticated `/api/peers`, not public `/health`

### 6.8 Peer pairing + gossip discovery (DD-022)

- [x] Pairing handshake: `POST /api/peers/pair` (generate code), `POST /api/peers/confirm` (confirm with code + URL), `POST /api/sync/pair/complete` (callback)
- [x] One-time pairing code (16 hex bytes, 10 min expiry) plus mutual role-separated HMAC-SHA256 proofs bound to the canonical confirmer URL; never transmit the raw sync secret
- [x] Gossip-based mesh discovery: `SyncResponse` includes `SelfURL` + `KnownPeers`, `discoverPeers()` auto-adds only unknown peers already authorized by `SYNC_PEER_ALLOWLIST`
- [x] External IP auto-detection (`detectExternalIP`) with Docker bridge interface filtering
- [x] `EXTERNAL_IP` env var override for containers
- [x] Historical: TLS `InsecureSkipVerify` workaround for IP peer URLs (DD-024; superseded by DD-028)
- [x] Strict sync TLS verification with IP dialing plus `SYNC_TLS_SERVER_NAME`; configuration errors fail sync closed without stopping DNS
- [x] Keep secrets out of read surfaces: omit both from settings/config export/replication, ignore both during config import/sync merge, and never transmit or return the raw sync secret during pairing
- [x] `PUT sync_secret` and pairing require at least 32 bytes; updates return acknowledgement only and direct `session_secret` updates are rejected
- [x] Reject weak environment/stored sync secrets instead of activating them; readiness reports false until a strong key is present
- [x] Discard peer-controlled HTTP/application error bodies and invalid timestamps; refuse redirects and cap sync/pairing bodies
- [x] `POST /api/auth/sessions/revoke` rotates the session-signing key and invalidates all browser sessions without returning secret material
- [x] Remove peer URLs from public Prometheus labels; use opaque `peer-N` labels
- [ ] Redesign the remaining unauthenticated metric labels (client IPs, upstreams, entity/list names) with authenticated scraping or privacy-preserving identifiers
- [x] Frontend: Pair/Confirm Pair/Manual buttons, pairing code display with click-to-copy
- [x] Peer table shows node names prominently, URL below in muted text, "Changes (24h)" column

### 6.9 Hardened production release

- [x] Split non-mutating branch CI from automatic main-only production release credentials
- [x] Bind each release to the exact source SHA and immutable image digest; never deploy mutable `latest`, and carry the same event SHA and canary digest through promotion and seal in one Action
- [x] Canary Blue while Red remains serving, verify strict TLS, exact topology, advancing reciprocal sync, UDP/TCP DNS, policy behavior, and non-disclosure, then record an attestation
- [x] Require at least 15 minutes before automatic Red promotion of the same attested digest
- [x] Serialize with renewable locks on both nodes, independently pin SSH host keys, create code/Compose/prior-image checkpoints without touching data volumes, and automatically roll back only the node just changed
- [x] Arm a one-hour rollback lease before mutation and install enabled boot recovery with idempotent crash/reboot handling and functional local DNS proof
- [x] Require schema-neutral releases on the code-only rollback path and automatically seal both nodes only after exact durable Compose, inactive rollback-unit, and strict two-node checks

### 6.10 Leave quorum

- [ ] **"Leave Network" button** in Admin → Replication — node announces departure to all peers before removing itself
- [ ] `POST /api/peers/leave` — iterates all configured peers, calls a new endpoint on each to request removal (e.g., `POST /api/sync/peer-left` with the departing node's URL)
- [ ] Receiving nodes: add the departing URL to `deleted_peers`, remove from `sync_peers`, restart sync workers
- [ ] Departing node: clears its own `sync_peers` and stops sync workers after all peers acknowledge (or timeout)
- [ ] Graceful degradation: if a peer is unreachable during leave, the departing node leaves anyway — unreachable peers will eventually mark it unhealthy and can be manually cleaned up

### 6.11 Cross-node client visibility

- [x] `getAllClients()` uses UNION ALL with `client_aliases` table to show synced clients without local queries
- [x] Client aliases already synced via LWW — clients named on one node appear on all nodes

### 6.11 Rewrite privacy fix (DD-023)

- [x] `checkRewrite` returns NODATA (empty answer, NOERROR) for query type mismatches (e.g., AAAA on IPv4-only rewrite)
- [x] Prevents internal hostname leaks to upstream resolvers when A and AAAA queries sent simultaneously
- [x] Rewrite path calls `evaluatePolicy` for policy context in query logs

---

## Phase 7: Containerization + CI/CD ✅

### 7.1 Dockerfile

- [x] Multi-stage: Node 24 Alpine frontend, Go 1.26/Debian 13 CGO builder, and OpenSSL-free Debian 13 distroless runtime. Clean builds resolve upstream tags to immutable digests, apply builder package upgrades, test, package DuckDB's GCC/C++ runtime dependencies with scanner metadata, reject OpenSSL or unresolved links, smoke-start the real runtime binary, scan, and copy only the application plus a tiny Go health helper into runtime
- [x] Non-root `svart` user, trimpath + stripped binary (-s -w), 105MB final image
- [x] Expose ports 5353/udp, 5353/tcp, 3000/tcp
- [x] Container HEALTHCHECK probes loopback HTTPS (with HTTP fallback for non-TLS development); production release verification performs a separate strict external TLS check
- [x] Volumes for `/data` (DB) and `/archives` (Parquet exports)

### 7.2 Forgejo Actions pipeline

- [x] `.forgejo/workflows/ci.yml`: non-mutating exact-SHA clean Docker build/test/security scan on push and pull request; no production credentials or deploy step
- [x] `.forgejo/workflows/security-refresh.yml`: daily no-cache rebuild against fresh supported bases with npm signatures/audit, govulncheck, Trivy, SBOM, and fail-closed HIGH/CRITICAL gates
- [x] `.forgejo/workflows/deploy.yml`: push/scheduled, main-only, concurrency-serialized automatic Blue canary, Red promotion, and seal; rollback is automatic and same-run only
- [x] Publish commit-SHA image and deploy by immutable registry digest; never release mutable `latest`
- [x] Canary Blue while Red remains serving, require a 15-minute attestation, then explicitly promote the same digest to Red
- [x] Verify strict TLS, minimal health, UDP/TCP DNS, allow/block behavior, exact peer topology, advancing sync, and non-disclosure after each mutation
- [x] Use pinned SSH host keys, node-local code/Compose/image checkpoints, durable lease plus reboot recovery, and automatic same-node rollback on failure

### 7.3 Environment config

- [x] All environment settings documented, including strict sync TLS identity and exact peer allowlist controls
- [x] `.env.example` with all vars, grouped and commented
- [x] `docker-compose.yaml` for local dev with volumes and env_file
- [x] `make docker` / `make docker-run` / `make docker-down` targets

### 7.4 Remaining SDLC and runtime hardening

- [ ] Configure Forgejo branch protection for `main`, including required checks and review policy
- [ ] Persist CycloneDX SBOM and Trivy JSON output as Forgejo artifacts or release evidence
- [ ] Sign production images and publish independently verifiable build provenance
- [ ] Add CI gates for formatting, race tests, coverage, static security analysis, secret scanning, and license policy
- [x] Resolve the frontend Fast Refresh warnings and make ESLint warnings fatal in CI
- [ ] Split or explicitly budget the oversized chart bundle and enforce the bundle-size limit in CI
- [ ] Mount the production root filesystem read-only and add capability drop, no-new-privileges, PID, and explicit container memory controls without breaking DNS or cert reload
- [ ] Remove the shared CI runner's Docker TCP dependency or place it behind an authenticated isolated boundary
- [x] Document the exact implemented and missing controls in `design/SECURITY.md`, enforced by `deploy/documentation-contract-test.sh`

---

## Phase 8: React SPA Frontend ✅

The Go-template UI has been replaced by a React SPA served from the same Go
binary. Dashboard, Logs, Filters, Tiers, Rewrites, Config, Analysis, Admin,
Investigation, and Login routes are implemented.

### 8.1 Frontend scaffolding

- [x] Vite + React 18 + TypeScript in `frontend/` directory
- [x] React Router v6 for client-side routing (browser history mode)
- [x] ApexCharts + react-apexcharts for dashboard charts
- [x] Plain CSS design system with CSS custom properties (Monokai Pro Spectrum palette)
- [x] Self-hosted fonts: Inter (400-700) and JetBrains Mono (400-500) as woff2
- [x] API client wrapper with cookie auth, auto-redirect to `/login` on 401
- [x] `useApi` and `usePolling` hooks for data fetching

### 8.2 Backend endpoints for dashboard and auth

- [x] `POST /api/auth/login` — JSON login, sets `svart_session` cookie
- [x] `GET /api/auth/check` — auth state check for SPA
- [x] `GET /api/stats/timeseries` — time-bucketed query/block counts
- [x] `GET /api/stats/top-clients` — client ranking by volume
- [x] `GET /api/stats/block-sources` — blocklist attribution
- [x] `GET /api/stats/upstream-usage` — resolver distribution
- [x] `GET /api/stats/top-domains` — domain ranking
- [x] `GET /api/stats/latency` — time-bucketed latency
- [x] `GET /api/rewrites/stats` — rewrite hit counts per domain
- [x] `GET /api/blocklists/history` — refresh diff history
- [x] `blocklist_history` table for tracking refresh diffs
- [x] `requireAuth` updated to accept `svart_session` cookie for API calls

### 8.3 Pages

- [x] Dashboard — 4 stat cards, 14 charts (traffic, latency, block/upstream donuts, client activity, top domains, overrides, servfails, CPU/memory/disk system monitoring), live query log (20 rows, 2s polling), time window selector
- [x] Logs — live/range mode, search + filters (client, group, range, result, type), context menu
- [x] Filters — 3 tabs: Published Lists, History, List Details (domain browser)
- [x] Tiers — master-detail split, 3 tabs (Ranges/Groups/Clients), octet visualizer
- [x] Rewrites — quick-add, data grid with inline edit, activity stats, client popover
- [x] Config — settings form with sections, cache clear
- [x] Login — auth check on load, redirect on success

### 8.4 Go integration

- [x] `//go:embed all:frontend/dist` — SPA served from Go binary
- [x] `newSPAHandler()` — serves static files, falls back to index.html for client-side routing
- [x] Old Go template handlers removed (handleUpstreams, handleBlocklists, handleAdminHome, renderPage, etc.)
- [x] `templateAuth` middleware removed (SPA uses cookie auth via API)
- [x] Makefile: `build-frontend`, `dev-frontend` targets, `build` depends on frontend
- [x] Dockerfile: 3-stage build (Node.js frontend → Go binary → runtime)
- [x] CLAUDE.md updated with frontend architecture and build commands

### 8.5 System resource monitoring (DD-021)

- [x] `system_stats.go`: ring buffer (120 × 30s), background collector, `GET /api/stats/system`
- [x] CPU % via `Getrusage` diff, RSS from `/proc/self/statm`, heap from `MemStats.Alloc`, DB size via `os.Stat`
- [x] 4 Prometheus GaugeFuncs: `svart_dns_cpu_percent`, `svart_dns_memory_rss_bytes`, `svart_dns_memory_heap_alloc_bytes`, `svart_dns_disk_db_size_bytes`
- [x] Dashboard: 3 area charts (CPU green, Memory blue+purple RSS/Heap, DB Size orange)
- [x] `NodeNameProvider` context + `useNodeName()` hook — node name fetched once, optimistically updated from Admin page

### 8.6 Remaining pages

- [x] Analysis page — blocklist intelligence UI (matrix, compare, simulate, replay)
- [x] Admin page — user management, API token management, replication card (peer management, sync config, live health)
- [x] Investigation page — CodeMirror 6 SQL editor + schema explorer + results table (raw SQL against unified hot+cold query_logs via DuckDB)

---

## Network Client Discovery + Integration

These are important for making svart-dns aware of the full network picture — not just DNS clients, but ALL devices.

### Client discovery: identify all devices on the network

- [ ] **Evaluate existing tools**: Look at [nmap](https://nmap.org/), [arp-scan](https://github.com/royhills/arp-scan), [Fing](https://www.fing.com/), [NetBox](https://netbox.dev/), [LanScan](https://github.com/iRevive/LanScan), [rumern/netdisco](https://github.com/netdisco/netdisco). Need: periodic scans, MAC address collection, hostname/mDNS/NetBIOS discovery, vendor OUI lookup
- [ ] **Integration with svart-dns**: Import discovered clients with human-readable names. Map MAC → IP → alias so the UI shows "Sophie's iPhone" not "10.42.1.43". Could be a periodic cron, an API endpoint that accepts client lists, or a built-in scanner
- [ ] **API endpoint**: `POST /api/clients/import` — accept a list of `{ip, mac, hostname, alias}` entries from an external scanner. Update `client_aliases` table, create client entries if new
- [ ] **Detect unknown devices**: Flag clients that appear on the network (via ARP/scan) but have never made a DNS query through svart-dns — these are either using a different DNS server or are DNS-silent

### OpnSense integration

- [ ] **Readonly API access for Claude**: Set up an OpnSense API key with readonly permissions so Claude can audit DHCP/DNS/firewall config, verify VLAN setups, and check for misconfigurations
- [ ] **DHCP lease import**: OpnSense knows every DHCP lease (IP ↔ MAC ↔ hostname). Pull from OpnSense API (`/api/dhcpv4/leases/searchLease`) on a schedule to populate svart-dns client aliases
- [ ] **ARP table import**: OpnSense's ARP table (`/api/diagnostics/interface/getArp`) shows all devices that have communicated on the network, even those with static IPs
- [ ] **DNS bypass detection**: Compare OpnSense's ARP/DHCP client list against svart-dns query_logs. Clients present in ARP but absent from query_logs are bypassing our DNS — flag them in the UI and optionally alert. This is critical for catching IoT devices that hardcode DNS (Google Home → 8.8.8.8, Ring → their own resolvers, etc.)
- [ ] **Firewall rule suggestion**: For clients caught bypassing DNS, suggest OpnSense NAT rules to redirect port 53 traffic to svart-dns (DNS hijacking / transparent proxy)

## Frontend API support

Implemented APIs originally identified by the frontend design audit.

### Already planned (Phase 3 leftovers)

- [x] `POST /api/analysis/replay` — retroactive traffic analysis (Phase 3.6)
- [x] Configurable refresh interval per blocklist (Phase 3.7)
- [x] `GET /api/analysis/domains?source=client-history&client_ip=X&window=Y` — per-client traffic domain source + "Your Traffic" UI card
- [x] `blocklist_changelog` table + checkpoint domain browser — full diff storage per refresh, backward reconstruction to browse any historical state (Phase 3.7)

### New gaps identified from frontend design

- [x] **Query log filtering by group/range**: `group_id` param uses subquery on `client_group_members`, `range_id` resolves CIDR to matching client IPs server-side
- [x] **Rewrite hit analytics**: `GET /api/rewrites/stats` returns per-rewrite hit count and unique client count from query_logs
- [x] **Timezone setting**: `timezone` key seeded as `UTC` default. Frontend reads via `GET /api/settings`, updates via `PUT /api/settings/timezone`
- [x] **Logging toggle setting**: `logging_enabled` key (default `true`). `logQuery()` checks `loggingEnabled` atomic before queueing. Setting update triggers immediate reload
- [x] **JSON tags on Client/Group/RewriteView/Member structs**: Added proper `json:"snake_case"` tags. All API responses now use consistent snake_case keys
- [x] **Blocklist/allowlist domain list endpoints**: `GET /api/blocklists/{id}/domains?limit=N&offset=N` and `GET /api/allowlists/{id}/domains?limit=N&offset=N` — paginated, up to 10k per page
- [x] **Group/range/client detail endpoints**: `GET /api/groups/{id}` (members, assigned lists, custom rules), `GET /api/ranges/{id}` (assigned block/allow lists), `GET /api/clients/{ip}` (groups, lists, custom rules, query stats)
- [x] **Named Policies**: Reusable policy templates bundling blocklist/allowlist selections + custom rules. Full CRUD (`/api/policies`), assignable to ranges/groups/clients via `policy_id`. Merged at load time — DNS hot path unchanged. Sync support (SyncPolicy, SyncPolicyList, SyncClientPolicy). Config export/import. Frontend Policies tab in Tiers page with entity policy selector. See [DD-020](decisions/DD-020-named-policies.md)

### Notes for frontend developers

- **API tokens are write-once**: The plaintext token is only returned at `POST /api/tokens` creation time. It cannot be revealed later (bcrypt hashed at rest). The UI must make this very clear — show-once modal with copy button
- **Browser users and API tokens are distinct**: `admin_users` contains DB-backed browser accounts with roles and bcrypt hashes; `api_tokens` contains show-once automation credentials with independent readonly/admin roles. The Admin UI manages both without ever revealing stored hashes
- **HTTPS is already built**: Phase 6 added `TLS_CERT`/`TLS_KEY` support. Self-signed certs work. No need for a separate "HTTPS admin dashboard" feature

---

## Future / Out of Scope (for now)

- [ ] **Container-level DNS granularity**: Investigate Docker/Podman API integration to map container IPs to names for hosts using macvlan. See [DD-004](decisions/DD-004-container-level-dns-granularity-is-out-of-scope-for-now.md)
- [x] **React SPA**: Vite + React 18 + TypeScript frontend in `frontend/`. 7 pages (Dashboard, Logs, Filters, Tiers, Rewrites, Config, Login). Embedded via `//go:embed all:frontend/dist`. Self-hosted Inter + JetBrains Mono fonts, ApexCharts for dashboard. Old Go template UI removed
- [x] **TLS certificate hot-reload**: `tls.Config.GetCertificate` callback with `atomic.Pointer[tls.Certificate]`. Background 60s mtime polling (NFS-safe, no fsnotify). Zero-downtime cert rotation — new handshakes pick up the new cert, existing connections unaffected. Follows autorefresh.go stop/done shutdown pattern. See `tls.go`
- [ ] **HTTPS admin dashboard**: ~~Auto-generated self-signed certs~~ TLS support built in Phase 6. Remaining: Let's Encrypt auto-renewal integration
- [ ] **Encrypted recursive resolution**: Revisit if upstream providers become untrustworthy. See [DD-005](decisions/DD-005-no-recursive-dns-resolution.md)
- [ ] **DNS-over-QUIC**: Protocol support exists in the UI dropdown but not in the resolver
- [ ] **Materialized blocklist analysis**: If on-demand replay proves too slow or gets used frequently, continuously maintain per-list block counts as a background job. See [DD-001](decisions/DD-001-blocklist-analysis-is-on-demand-not-materialized.md)

---

## Phase 9: Hot/Cold Architecture + Investigation (DD-025, DD-026, DD-027)

### 9.1 Read-only connection pool

- [x] `readDB` pool (`MaxOpenConns(4)`, `mode=ro` file: URI) for all stats/dashboard/admin reads
- [x] Migrated `stats_api.go`, `database.go` read paths, analysis endpoints to `readDB`
- [x] Write pool (`db`) reserved for config mutations only

### 9.2 Archive threshold + VACUUM

- [x] Archive threshold lowered from 730 days to 8 days (7-day dashboard + 1 day buffer)
- [x] Daily archiving at midnight (user timezone → server local → UTC)
- [x] VACUUM after archive to reclaim disk space
- [x] Startup archive run for immediate cleanup on deploy

### 9.3 Doom loop defense (DD-027)

- [x] SOA negative TTL (3600s) on blocked NXDOMAIN responses (RFC 2308)
- [x] Logwriter per-(client_ip, query_name) dedup: 25 TPS blocked, 100 TPS allowed
- [x] `coalesced_count` column (DEFAULT 1) on `query_logs` + Parquet schema
- [x] All `COUNT(*)` → `SUM(coalesced_count)` in stats_api.go, analysis.go, Prometheus counters
- [x] `doomLoopActive` sync.Map — DNS handler sleeps 1s for active doom loops
- [x] Anomaly alerting via Loki

### 9.4 DuckDB investigation engine (DD-026)

- [x] DuckDB-Go dependency + build verification (CGO compatible)
- [x] `initDuckDB`: in-memory instance, 256MB limit, SQLite extension
- [x] `investigateQuery`: mutex, WAL checkpoint, attach read-only, UNION ALL view, execute, detach
- [x] `investigateSchema`: column metadata for unified view
- [x] `POST /api/investigate` (admin-only, 30s/60s timeout, 429 concurrent)
- [x] `GET /api/investigate/schema` (admin-only)
- [x] Analysis 30-day window queries via DuckDB (unified hot+cold)

### 9.5 Investigation UI

- [x] CodeMirror 6 SQL editor with syntax highlighting
- [x] Schema explorer sidebar (column names + types)
- [x] Results table with row count and execution time
- [x] Investigation page in sidebar navigation

### 9.6 Follow-up items

- [ ] **Grafana dashboard for admin API performance**: Build dashboard panels from Prometheus metrics and Loki logs tracking HTTP request latency, error rates, and throughput for the admin API
- [ ] **Investigation UI: saved queries**: Persist frequently-used SQL queries (per-user or global) with names and descriptions for quick re-execution
- [ ] **Investigation UI: chart visualization**: Render query results as time-series charts, bar charts, or pie charts when the result shape is compatible
- [ ] **Blocklist changelog Parquet archival**: Archive old `blocklist_changelog` rows to Parquet (deferred — changelog table grows slowly compared to query_logs)

The bounded release timing and placement of preparation work are now governed by
[the current release decision](decisions/bounded-production-release.md); it supersedes
the historical canary/soak and scheduled-deployment entries above.
