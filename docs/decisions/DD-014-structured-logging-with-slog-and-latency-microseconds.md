# DD-014: Structured logging with slog and latency_microseconds

**Decision**: Migrate all logging from `log.Printf` to Go's `log/slog` (stdlib since 1.21) with 12 component loggers, `LOG_FORMAT` (text|json) and `LOG_LEVEL` (debug|info|warn|error) env vars. Rename `latency_ms` to `latency_microseconds` everywhere (DB column, struct fields, Parquet schema, API JSON keys).

**Component loggers**: 12 package-level `*slog.Logger` vars (`logDNS`, `logAdmin`, `logCache`, `logPolicy`, `logLW`, `logArchiver`, `logResolver`, `logAuth`, `logDB`, `logBlocklist`, `logAllowlist`, `logConfig`), each created once at startup as `slog.Default().With("component", name)`. Zero per-call overhead.

**Hot path protection**: DNS query debug logs use a cached `dnsDebugEnabled` flag checked before calling `LogAttrs` with pre-typed `slog.Attr` values. At Info level (production default), no allocations occur for debug logs — no `context.Background()` call, no attribute construction.

**Per-query structured audit trail**: The logwriter's `flush()` method emits a full structured log entry for every DNS query after the batch SQLite commit. Each entry includes the complete `PolicyResult` serialized as nested slog groups (`range_evaluation`, `group_evaluation`, `ip_evaluation`), with published list attribution and custom rule details. This runs in the background goroutine — zero hot-path cost.

**Always-present tier evaluations**: `evaluatePolicyFull` now always initializes all three `TierEvaluation` structs (never nil), and sets a default allow result source (`{Tier: "default", Name: "no matching rule"}`) when no tier produces a decision. This simplifies downstream serialization and ensures every log entry has a complete, parseable structure.

**Latency microseconds**: The p50 DNS latency is 566μs with min 62μs — millisecond resolution loses all sub-ms detail. One unit (`latency_microseconds`), one name, everywhere. DB column renamed via `ALTER TABLE query_logs RENAME COLUMN latency_ms TO latency_microseconds` migration.

**Why slog over zerolog/zap**: slog is stdlib since Go 1.21 (we're on 1.24.9), zero external dependencies, good enough performance for our needs. The JSON handler produces clean output ready for Loki/Promtail ingestion. Component loggers via `With("component", name)` give free filtering.

**Revisit if**: slog performance becomes a bottleneck on the hot path (unlikely given the `Enabled()` guard pattern), or if we need log shipping features that slog doesn't support natively.
