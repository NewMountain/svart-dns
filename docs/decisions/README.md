# Decision records

Each record captures one significant decision: the context, what was decided, why, and what else was considered. Records are append-only; a decision that changes gets a new record that supersedes the old one.

| # | Decision | Status |
|---|---|---|
| [DD-001](DD-001-blocklist-analysis-is-on-demand-not-materialized.md) | Blocklist analysis is on-demand, not materialized | Accepted |
| [DD-002](DD-002-three-tier-policy-cascade-range-group-ip.md) | Three-tier policy cascade (Range > Group > IP) | Accepted |
| [DD-003](DD-003-multi-instance-sync-via-lww-registers-no-leader-election.md) | Multi-instance sync via LWW registers, no leader election | Accepted |
| [DD-004](DD-004-container-level-dns-granularity-is-out-of-scope-for-now.md) | Container-level DNS granularity is out of scope (for now) | Accepted |
| [DD-005](DD-005-no-recursive-dns-resolution.md) | No recursive DNS resolution | Accepted |
| [DD-006](DD-006-speed-is-a-first-class-design-constraint.md) | Speed is a first-class design constraint | Accepted |
| [DD-007](DD-007-sync-polling-interval-defaults-to-2-seconds.md) | Sync polling interval defaults to 2 seconds | Accepted |
| [DD-008](DD-008-blocklists-auto-refresh-with-change-tracking.md) | Blocklists auto-refresh with change tracking | Accepted |
| [DD-009](DD-009-api-token-and-browser-authentication.md) | API-token and browser authentication | Accepted |
| [DD-010](DD-010-separate-wildcard-patterns-from-exact-domains-at-load-time.md) | Separate wildcard patterns from exact domains at load time | Accepted |
| [DD-011](DD-011-coalesced-fast-clock-eliminates-time-now-from-the-hot-path.md) | Coalesced fast clock eliminates time.Now() from the hot path | Accepted |
| [DD-012](DD-012-batch-log-writer-with-dedicated-wal-connection.md) | Batch log writer with dedicated WAL connection | Accepted |
| [DD-013](DD-013-query-log-archival-with-parquet-export.md) | Query log archival with Parquet export | Accepted |
| [DD-014](DD-014-structured-logging-with-slog-and-latency-microseconds.md) | Structured logging with slog and latency_microseconds | Accepted |
| [DD-015](DD-015-prometheus-metrics-everything-off-the-hot-path.md) | Prometheus metrics — everything off the hot path | Accepted |
| [DD-016](DD-016-direct-loki-push-no-promtail-alloy-dependency.md) | Direct Loki push, no Promtail/Alloy dependency | Accepted |
| [DD-017](DD-017-skip-opentelemetry-traces-for-dns.md) | Skip OpenTelemetry traces for DNS | Accepted |
| [DD-018](DD-018-tombstone-table-for-sync-deletes.md) | Tombstone table for sync deletes | Accepted |
| [DD-019](DD-019-rich-policyresult-storage-in-query-logs.md) | Rich PolicyResult storage in query_logs | Accepted |
| [DD-020](DD-020-named-policies.md) | Named Policies | Accepted |
| [DD-021](DD-021-system-resource-monitoring-via-in-memory-ring-buffer.md) | System resource monitoring via in-memory ring buffer | Accepted |
| [DD-022](DD-022-peer-pairing-gossip-based-mesh-discovery.md) | Peer pairing + gossip-based mesh discovery | Accepted |
| [DD-023](DD-023-rewrite-nodata-response-prevents-internal-hostname-leaks.md) | Rewrite NODATA response prevents internal hostname leaks | Accepted |
| [DD-024](DD-024-tls-insecureskipverify-for-trusted-sync-peers.md) | TLS InsecureSkipVerify for trusted sync peers | Superseded by [DD-028](DD-028-verified-sync-identity-and-non-disclosure-of-authentication-material.md) |
| [DD-025](DD-025-hot-cold-query-log-architecture.md) | Hot/cold query log architecture | Accepted |
| [DD-026](DD-026-raw-sql-investigation-endpoint.md) | Raw SQL investigation endpoint | Accepted |
| [DD-027](DD-027-dns-doom-loop-defense.md) | DNS doom loop defense | Accepted |
| [DD-028](DD-028-verified-sync-identity-and-non-disclosure-of-authentication-material.md) | Verified sync identity and non-disclosure of authentication material | Accepted |
| [DD-029](DD-029-per-node-certificates-mutual-tls-for-sync-proposed-identity-replicatio.md) | Per-node certificates + mutual TLS for sync (proposed); identity replication opt-in and LWW timestamp admission (implemented) | Accepted |
| [DD-030](DD-030-rebuild-application-images-from-supported-upstream-bases-and-gate-the.md) | Rebuild application images from supported upstream bases and gate the final runtime | Accepted |
| [DD-031](DD-031-operational-documentation-is-an-executable-release-contract.md) | Operational documentation is an executable release contract | Accepted |
| [DD-032](DD-032-conditional-forwarding-with-domain-upstream.md) | Conditional forwarding with `[/domain/]upstream` | Accepted |
| [DD-033](DD-033-upstream-exchange-hardening.md) | Upstream exchange hardening | Accepted |
| [DD-034](DD-034-ui-names-filter-lists-and-assignments.md) | UI names follow the two concepts: filter lists and assignments | Accepted |
| [DD-035](DD-035-one-immutable-policy-snapshot-over-a-shared-list-index.md) | One immutable policy snapshot over a shared list index | Accepted |

Process notes:

- [Open-source release plan](open-source-release-plan.md) — the phased plan and decision log for the first public release.

- [Operator-provided Analysis ranking data](operator-provided-analysis-ranking.md) — local ranking input preserves Analysis without bundling third-party datasets.

- [Asynchronous query journal](asynchronous-query-journal.md) — bounded DNS admission, explicit volatile crash tail, and recoverable raw history.

- [Raw event archive ownership](raw-event-archive-ownership.md) — streaming original-event Parquet, verified retirement, and recoverable retention.

- [Summary history ownership](summary-history-ownership.md) — bounded exact source snapshots, durable recovery, additive storage compatibility and rollback gates.

- [Core list compatibility](core-list-compatibility.md) — typed DNS rules, list-local priorities and complete diagnostics with preserved Assignments.

- [Reader-first list storage](list-storage-reader-first.md) — qualify a compatible recovery reader before activating typed list writes.

- [Application package layout](application-package-layout.md) — application code and same-package tests together, with a small executable entrypoint.
