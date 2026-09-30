# Operations

## What to back up

A full recovery backup must preserve one consistent generation of all owned
storage, not just the main database:

- `DB_PATH`: configuration, presentation query history, replay cursors and the
  query-to-Loki outbox.
- `<DB_PATH>.spool.sqlite`: durable original query events, journal identity,
  legacy provenance, quarantine and raw-archive manifests.
- `<DB_PATH>.loki.sqlite`, when direct Loki delivery is configured: pending
  complete records and their delivery ownership.
- Every database's existing `-wal` and `-shm` companions; legacy `.spool` and
  `.spool.offset` files; and any other recovery sidecars in the data directory.
- The complete `ARCHIVE_PATH` tree, including raw and summary Parquet files,
  manifests, source identity sidecars and recoverable partial files.
- Deployment configuration and the exact compatible image or binary. Store
  secret-bearing deployment files in the operator's protected backup system.

Stop Svart cleanly and confirm it has exited before copying the complete data
and archive directories together. Fence automatic restarts and keep every writer
stopped until the copy and its file checksums are complete. A shutdown waiting
for unavailable storage has not finished draining: repair that storage first.
Keep the originals and test recovery on a separate copy with an image that
understands the same storage generation. Do not restore an old snapshot over
newer accepted events to roll back application code.

SQLite's online backup (for example, `sqlite3 svart-dns.db ".backup backup.db"`)
produces a consistent copy of that one database. It does **not** coordinate the
other journals, archive publication or ownership cursors, and is not a complete
Svart recovery backup. Configuration alone can also be exported as JSON with
`GET /api/config/export` and merged with `POST /api/config/import`; secrets and
query history are excluded. See [raw archive verification](raw-archives.md) and
[summary recovery](summary-archives.md).

### Configuration backup in the web interface

Open **Config** or **Admin**, then **Configuration Backup**. **Download
Configuration** saves the complete configuration export as JSON, including
disabled list assignments and manual rules. Accounts, API tokens, shared
secrets, query logs, and downloaded list contents are excluded; retain the
SQLite/archive backup above for full recovery.

Choose the JSON file and click **Import Configuration**. Import merges with
existing records; it does not delete entries absent from the file, and
existing upstreams/rewrites can remain unchanged. The UI re-exports the
persisted configuration and compares every field (except the export timestamp)
before reporting a verified import. Ordering of lists and set members is not
significant; bootstrap server fallback order must match exactly. A mismatch or failed readback is reported explicitly; download
the current configuration to inspect it. Invalid files and server errors
retain the selected backup and never claim success. Successful imports refresh
open configuration resource views.

## Upgrading

Stop the container or process, replace the image or binary, start it. Schema
migrations run automatically at startup in a checked transaction; legacy column
renames preserve stored data. See the atomic startup migration procedure below.
Drain accepted query events on clean shutdown. Durable originals remain in
`<DB_PATH>.spool.sqlite` or verified raw archives and replay through their
transactional cursor. Preserve existing legacy `.spool` files and offsets; see
[Query log durability](#query-log-durability) and the
[summary rollback gate](summary-archives.md#compatibility-and-rollback-gate).

## Resource use

The final backend (`ab901e3`, Go 1.26.8, x86-64) has these measured peaks:

| Workload | Resident memory observed |
|---|---:|
| Three million rules, three manual edits and a million-rule refresh | 1,107 MiB main-process high-water RSS |
| Live 50-million-row dashboard and native-query sequence | 392 MiB main-process high-water RSS |
| Separate archive-backed 50-million-row Investigation sequence | 297 MiB peak simultaneous RSS, including native worker |

These are different workloads, not values to add together or guaranteed maximums.
The samples exclude OS and filesystem cache. Detailed fixtures, timing, raw-history
cost, and measurement limits are in [Benchmarks](benchmarks.md). The final raw
journal retains every durably accepted original; saturated logging costs more CPU,
storage and memory than the older build and can apply DNS admission backpressure.
Replies remain asynchronous and do not wait for a per-reply disk commit.

Earlier release measurements on x86-64 at `4f20de8`, with the default 64 MiB
answer cache and 64 MiB policy cache budgets, are preserved below. They do not
replace the final backend measurements above:

| Workload | Process RSS |
|---|---:|
| Empty startup | 115 MiB |
| One 76,514-domain list | 135 MiB |
| Four lists, 832,967 total rows | 282 MiB |
| Five lists, 3.12 million total rows, plus 20 ranges sharing lists | 591 MiB |
| Same lists after 200,000 unique DNS queries, settled 15 seconds | 804 MiB |

That earlier release run's peak resident memory was **832 MiB**. The retained older binary
used 1,468 MiB in the same final scenario. Both used the same corrected harness,
lists and host; the older binary's compiler and dependencies differ, so this
is a measured comparison of builds rather than an isolated optimization result.
Full results and caveats are in [Benchmarks](benchmarks.md).

Cache budgets are not total-process limits. Lists, temporary reload indexes,
query logging, Go's allocator, SQLite and native DuckDB memory add to them.
The OS, filesystem cache and analytical queries need additional memory. The
fixture does not establish a maximum over long-running or analytical workloads.

**Raspberry Pi verdict:** these are x86 measurements, not a tested Pi release.
The final three-million-rule refresh exceeded 1 GiB of process RSS by itself,
so a 1 GiB Pi is unsuitable for that workload. A 2 GiB model is a plausible starting point for a
modest list set; 4 GiB gives more room for large lists, reloads and analytics.
Those are sizing inferences, not ARM measurements or guarantees. Use a 64-bit
OS and reliable storage for query history if evaluating a Pi build.

There is also a concrete architecture blocker: although the pinned DuckDB
binding includes a Linux arm64 library, Svart embeds an **amd64**
`sqlite_scanner.duckdb_extension`, and `investigate.go` writes it under the
hardcoded `v1.1.3/linux_amd64` path. Full Investigation support needs an
ARM-compatible extension and platform-aware packaging first. This benchmark
and release CI exercised amd64 only; a Pi build, container startup and sustained
operation on real hardware remain unverified. There is no tested arm64 image
or complete Raspberry Pi support claim.

## Monitoring

`GET /health` (unauthenticated) answers `200` while the process serves.
`GET /metrics` exposes Prometheus metrics. The ones worth alerting on:

| Metric | Alert when |
|---|---|
| `svart_dns_queries_total` | rate drops to zero while clients are online |
| `svart_dns_upstream_errors_total` / `svart_dns_upstream_queries_total` | error ratio above a few percent |
| `svart_dns_upstream_mismatched_replies_total` | ever increases on a UDP upstream (spoofed answers) |
| `svart_dns_upstream_saturated_total`, `svart_dns_overload_shed_total` | increases: Svart is shedding load |
| `svart_dns_log_writer_spool_pending_bytes` | stays above zero for minutes: SQLite is not keeping up or is unavailable |
| `svart_dns_log_writer_flush_failed_total` | increases steadily |
| `svart_dns_auto_refresh_errors_total` | increases: a blocklist keeps failing to download |
| `svart_dns_policy_reload_errors_total` | increases: a configuration change could not be applied and the previous policy is still in effect |
| `svart_dns_sync_last_success_timestamp` | older than a minute for a peer (high availability only) |
| `svart_dns_sync_errors_total` | increases for a peer |
| `svart_dns_sync_rows_rejected_total` | increases: a peer sends rows this node refuses (clock skew or tampering) |
| `svart_dns_acl_refused_total` | sudden rise: something outside `ALLOWED_CLIENTS` is querying |

Per-client series (`svart_dns_client_*`) exist only with
`METRICS_PER_CLIENT=true`.

Logs go to stdout as JSON by default (`LOG_FORMAT=text` explicitly opts out), and optionally
straight to Loki with `LOKI_URL`. The SQLite query log is a presentation view
that may coalesce repeated queries; the durable journal preserves original
events. `LOG_QUERIES=true` additionally emits presentation query log lines.

## Query log durability

DNS replies follow admission to a bounded volatile queue. Background batches
persist full originals in `<DB_PATH>.spool.sqlite`, then commit presentation
rows, the replay cursor and optional Loki outbox ownership together. Storage
outages retain pending events and apply bounded backpressure. Clean shutdown
drains accepted events; abrupt termination may lose the newest volatile tail.
That latency tradeoff is explicit; DNS replies do not wait for a per-reply fsync.
See [logging durability](logging-durability.md) for the complete boundaries.

Old query rows move to Parquet after 8 days (daily at midnight in the
configured time zone) and are deleted after the retention period (**Config →
Retention & Archival**, default 3 years). Queries across both are available on the
**Investigation** page and through `POST /api/investigate`.

## High availability

Run two or more nodes and hand out all of their addresses in DHCP. Each node
serves DNS on its own and accepts configuration changes. Changes replicate
between nodes every `SYNC_INTERVAL` (2 s by default) with last-writer-wins per
row; there is no leader and no quorum, so a node keeps serving when its peers
are down.

Sync runs over HTTPS with certificate and hostname verification. Every node
needs:

1. `TLS_CERT` and `TLS_KEY` (and `TLS_CA` if the certificates are
   self-signed; `SYNC_TLS_SERVER_NAME` if peer URLs use IP addresses),
2. the same `SYNC_SECRET` (at least 32 random bytes: `openssl rand -base64 48`),
3. `SYNC_PEER_ALLOWLIST` listing every node's origin, including its own,
4. `PEERS` listing the other nodes (or pair them from **Admin → Replication**).

Clocks must agree to within 5 minutes; rows timestamped further ahead are
refused. Administrators and API tokens are managed per node unless
`SYNC_REPLICATE_IDENTITY=true`; see
[DD-029](decisions/DD-029-per-node-certificates-mutual-tls-for-sync-proposed-identity-replicatio.md)
for the trade-off.

## Failure modes and recovery

| Symptom | Cause | What to do |
|---|---|---|
| Everything answers `SERVFAIL` | no reachable upstream | Check **Config → Upstream DNS Servers** and `svart_dns_upstream_errors_total`. DoH/DoT upstreams need a working bootstrap resolver (**Config → Bootstrap DNS**, default `9.9.9.9`). |
| Some clients get `REFUSED` | they are outside `ALLOWED_CLIENTS` | Add their range to `ALLOWED_CLIENTS`. |
| A blocklist shows an old date | download failing | `svart_dns_auto_refresh_errors_total` and the log say why (size limit, private address, HTTP error). The previous version stays active. |
| A change answers "change saved but not applied" | the new policy could not be built (the error names why, e.g. more than 2048 enabled lists) | The change is stored and the previous policy keeps serving. Fix the cause (disable lists); the next change or restart applies everything. |
| Setup page asks for a token after a restart | no administrator exists yet | The token changes on every start while setup is pending; use the one from the latest start. |
| Forgot the administrator password | — | With another administrator: reset it under **Admin → User Management**. Otherwise stop Svart, run `sqlite3 svart-dns.db "DELETE FROM admin_users; DELETE FROM sessions;"`, start it, and create a new administrator with the setup token from the log. |
| `svart_dns_log_writer_spool_pending_bytes` keeps growing | SQLite is locked or the disk is slow | Look for write failures in the log. Committed journal records remain recoverable; the bounded volatile tail is drained after storage recovers. See the asynchronous crash tradeoff below. |
| Peers show unhealthy | TLS, secret, allowlist or clock mismatch | **Admin → Replication** shows the reason; `svart_dns_sync_rows_rejected_total{reason}` names refused rows. |

Retention accepts a whole number of days from 1 through 3,650,000 at the settings,
configuration-import, and peer-sync boundaries. A rejected value leaves the prior
configuration in place. If an old database already contains an invalid value, or
retention cannot be read, both log cleanup and the archive cycle stop and emit an
error explaining how to restore `log_retention_days`. They never substitute a
default deletion window after a failed read. Ordinary positive retention still
expires history outside the configured window.

Configuration imports validate addresses, CIDRs, rewrite records, list refresh
intervals, and all assignment references before writing. References may name an
existing record or a record in the same import. Every SQL failure rolls back the
whole import; an HTTP error never represents a partially committed import.

Sync compares parsed UTC instants after a coarse SQLite timestamp filter, so
legacy SQLite timestamps and RFC3339 fractions order consistently. A peer clock
more than five minutes ahead fails the poll before merging or advancing its
cursor. Correct that peer's clock and the next poll resumes from the last valid
cursor. Fresh database defaults have no modification timestamp; only explicit
configuration writes participate in mesh conflict resolution. For every entity,
relationship, and manual rule, retained tombstones reject older or equal-time
peer writes; a newer explicit write can re-add them. An older
deletion also leaves a newer relationship or rule intact. Compound keys retain
the existing wire format, including `client_ip|group_name` for group members.

Every admitted merge commits atomically: SQL reads, scans, iteration, writes,
and deletions must succeed (including affecting the expected rows) before runtime
caches are refreshed or the peer cursor advances. A required parent that is absent fails the poll with a `missing reference`
diagnostic rather than silently discarding the assignment or clearing its policy.
If that parent predates the delta cursor, the poll automatically retries one full
snapshot without moving its cursor first. If the full snapshot still lacks the
parent, the poll fails and keeps its previous cursor. Restore the parent on the
source peer; a subsequent poll recovers automatically. Actual database errors do
not trigger the missing-parent fallback. Malformed rows (including compound
tombstones without both key parts) and disabled identity rows
remain individually rejected and counted; other admitted rows can still commit.

Bootstrap server replacement through PUT or a nonempty configuration import and
its sync tombstones commit in one transaction. Retained or re-added servers have
no equal-time deletion tombstone.
Failed replacements preserve the previous servers and cached answers. Successful
API writes, imports, and peer updates invalidate bootstrap answers immediately;
lookups already in flight cannot repopulate the cache from the old configuration.
Concurrent cold lookups for the same name share one DNS request, while each
waiting caller can cancel independently, including the caller that starts the
lookup. Shared operations have a ten-second limit, with two seconds per server;
once all callers leave, their operation is canceled and cannot cache its result.
A short caller deadline never becomes a cached failure for a later caller.
Warm-cache lookups retain the existing allocation-free path.

Manual blocklists and allowlists include their complete rule text in the optional
`domains` array in configuration exports, including disabled lists. Imports merge
these rules without duplicating an existing identical rule. Published lists keep
their URL and settings; their downloaded contents are refreshed from that source.
Exports read every component from one SQLite snapshot, so concurrent edits cannot
attach rules or assignments to a different version of a list. An export fails if
any assignment or rule query fails or refers to a missing entity, preventing a
successful response containing an incomplete backup. Version 1.0 imports without `domains`
remain supported.

Re-running schema migration preserves explicit retention values, including seven
days. The three-year default applies only to a newly created setting. For manual
rule imports, omitted `domains`, `domains: []`, and `domains: null` all preserve
existing rules; nonempty arrays add missing exact rules. Import is a merge, so an
empty array is never an instruction to erase an existing rule collection.

For query admission, crash recovery, direct Loki delivery, retained raw history,
and backlog metrics, see [durable logging](logging-durability.md).

## Investigation resource isolation

On supported Linux amd64 hosts (kernel 4.7 or newer), each investigation executes
in a fresh child of the installed Svart binary. The child inherits no application
credentials or environment overrides. It loads the vendored SQLite extension,
then installs hard Linux allocation limits before parsing SQL or opening the
query-log view. The DNS process never executes user SQL. Schema inspection also
uses a disposable worker, including its fixed metadata query, so the embedded
SQLite implementations never share live WAL/SHM mappings within the DNS process.

Both query execution and schema inspection reject archive discovery failures
(such as a non-directory archive path, permission denial, or an I/O error) with
HTTP 503 and `error_code: unavailable`. They never return a successful hot-only
result when archive discovery fails. Restore readable archive storage and retry;
the request does not modify archived data. A missing directory on a fresh
installation without cataloged cold owners remains valid and uses the current
SQLite rows. Missing expected files or pending/ambiguous ownership returns
unavailable; see [summary recovery](summary-archives.md). Internal storage diagnostics remain in server
logs; HTTP responses do not expose paths. Ready cataloged summaries are verified against
their complete stored digest on each operation; valid but changed Parquet is
also unavailable. Query/schema verify before and after reading; status verifies
once. Schema and status operations are serialized and bounded to 30 seconds;
schema work runs in the child, while status verifies ownership in the parent.
See [integrity read costs and limits](summary-archives.md#ready-content-integrity-on-read).

The worker's `RLIMIT_DATA` is **384 MiB** for private writable allocations,
including native scalar/aggregate intermediates and Go result/serialization
copies. DuckDB's separate buffer-manager limit is **128 MB**. `RLIMIT_AS` also
limits additional virtual address space to **512 MiB** above initialized mappings;
Go and the native allocator reserve substantial nonresident address space.
These are allocation limits, not a promise that whole-process RSS is 384 MiB:
executable/shared-library pages and the main stack are additional. Extension
signature validation has a fixed trusted initialization phase before the limits;
no submitted SQL executes during it. See the [Linux resource-limit semantics](https://man7.org/linux/man-pages/man2/getrlimit.2.html).
Linux versions without mmap-aware DATA
limits, missing `/proc`, or failed resource-limit installation reject requests.
Other platforms fail closed; the existing extension is Linux amd64 only. The
worker needs a writable temporary directory that permits executable mappings of
the bundled native extension. With a read-only container root, provide such a
scratch mount (for example `--tmpfs /tmp:rw,exec,nosuid,nodev`). A `noexec` scratch
mount makes Investigation unavailable; it does not disable DNS. The extension
is bundled and checksum-verified, not downloaded at runtime.

A successful result contains every selected value and row. Queries exceeding
**4 MiB of JSON result data**, **10,000 rows**, or **100,000 cells** fail with HTTP
422 and guidance to select fewer columns, aggregate, or page a smaller result.
The worker-to-parent transfer has a separate **8 MiB** cap. No result is silently
truncated. Intermediate native memory exhaustion also produces 422, even when
the final scalar would have been small. Query spilling is disabled; failed
queries cannot fill disk with temporary spill files.

The HTTP deadline covers worker startup and execution. Timeout returns 504;
request cancellation kills the worker and releases admission after it has been
reaped. No abandoned DuckDB engine remains executing in the DNS process. Only
one investigation is admitted through response serialization, so concurrent
requests cannot multiply this result budget. The private worker temporary
directory is removed after every exit, including forced termination.

The race-test harness runs the parent under the race detector and builds an
ordinary service binary for workers, using the existing offline module cache.
ThreadSanitizer's huge writable shadow mappings cannot obey the production DATA
limit; the harness keeps that limit unchanged rather than inflating it for tests.

Raw per-event history has a separate [archive lifecycle and read-only verification command](raw-archives.md). Back up `DB_PATH.spool.sqlite` consistently with its WAL and `ARCHIVE_PATH/raw-v1/` in addition to summary archives.

### Atomic startup migrations

Schema creation, defaults, column upgrades, and local list-generation tables
commit together. Failed SQL inspection, DDL, data backfills (including ignored
writes), or commit stop startup. The application does not start its DNS/API
listeners after a migration error. Individual missing columns from interrupted
older upgrades are repaired independently. Explicit retention values and a
list's `refresh_interval=0` remain unchanged on restart.

Duplicate natural keys and orphan foreign-key relationships require operator
reconciliation. Startup reports the affected table/key or relationship without
printing token material; it does **not** choose a surviving row, delete dependent
rules, or discard orphan assignments. Keep a consistent backup and investigate
all affected rows and references in a disposable copy. Resolve the identity
conflict or recover the missing parent deliberately, then retry startup. Existing
integer IDs remain stable. Legacy `blocklists.pattern` becomes `url` in place,
retaining other legacy columns and dependent records. The approved legacy API
token plaintext scrub still runs transactionally; token hashes remain valid.

These releases add journal and local-generation schema. They are **not
schema-neutral**. Prior-binary compatibility of configuration reads, edits and
DNS does not establish query-history rollback safety. The separate
[legacy spool migration gate](legacy-spool-migration.md) and
[summary rollback gate](summary-archives.md#compatibility-and-rollback-gate)
require consistent backups and actual old/new writer continuity before any
production rollback. Never restore an older database over newer history to
roll back application code.

The prior `b67c6f4` binary also unconditionally resets a published list's
`refresh_interval=0` to `604800` during startup, even for a disabled list. A
rollback to that binary is therefore not configuration-preserving for those
lists. The rollout gate must account for this mutation as well as legacy spool
ownership; opening the additive schema successfully is insufficient.

Deployment recovery therefore requires a qualified, frozen recovery binary from
the compatible current generation. Qualify the candidate and recovery baseline
against a complete copy of persistent state in isolated development before release.
Rehearsals and full database scans do not run in CI or production deployment.
The [bounded release contract](decisions/bounded-production-release.md) requires
ten minutes from merge to both nodes, with a one-to-two-minute decision per node.
Recovery runs the qualified baseline against the current volumes,
preserving history written by the candidate. Preserved older code and data are
evidence, never automatic restore inputs. The infrastructure deployment's
`svart-dns/docs/compatible-recovery.md` defines the qualification, ownership fence
and recovery gates; passing those gates is required before production use.

## Local configuration mutations

Local list, group, range, bundle, membership, manual-domain and rewrite changes
prepare their resulting policy or rewrite cache inside the SQLite write
transaction. Entity renames/deletions and all natural-key relationship tombstones
commit together. If a statement, dependent read, snapshot build or commit fails,
the prior database and active cache remain intact; the API reports unavailable.
An operator can retry the same request after repairing storage. Missing rows in
idempotent deletes remain harmless; a failed read is never treated as absence.

Range detail includes complete `custom_blocked` and `custom_allowed` arrays,
including empty arrays. Its existing custom block/allow controls persist rules
through the same transaction path as client, group and bundle rules. A custom
rule deletion applies to every assigned manual list contributing that displayed
domain, retaining unrelated rules. Repeated additions keep one rule per list.

Authentication initialization and cache refresh require successful reads of both
user and token counts. A read failure preserves the previous authority state and
cannot enable first-run setup. Credential changes and session revocation commit
together. Alias reloads likewise publish only after every row has been read;
settings readers distinguish genuine absence from storage failure. Startup stops
if required authentication, alias, logging or sync-setting initialization fails.

Bootstrap server additions and replacement check every affected row, including
removal tombstones and retained-server tombstone cleanup. Failed writes leave
both persisted configuration and cached bootstrap answers intact. Successful
changes invalidate the previous answers; configuration import uses the same
checked replacement helper within its complete import transaction.

Configuration imports prepare their policy, upstream, rewrite and client-alias
snapshots inside the same SQLite transaction as the imported rows. Changed
runtime and sync settings are prepared there too. A failed snapshot read or
commit rejects the import and preserves both the saved configuration and the
previous live snapshots; a successful response means those snapshots have been
published. The merge remains idempotent for rules and configured entities.

For optional Alloy/Tempo tracing, Pyroscope profiles, correlation, queue metrics,
and the disposable collector proof, see [Self-hosted observability](observability.md).
