# DD-027: DNS doom loop defense

**Decision**: Three-layer defense against DNS doom loops (clients rapidly re-querying blocked domains): (1) SOA negative TTL on blocked NXDOMAIN responses, (2) logwriter per-`(client_ip, query_name)` dedup at configurable TPS thresholds, (3) DNS response throttling via 1s sleep for active doom loops.

**Why**: A production incident saw a single client generating 260 blocked queries/sec for the same domain, creating millions of rows in `query_logs` and inflating the database to 19GB. Normal peak blocked rate is 14 TPS. The three layers address different aspects: SOA negative TTL (3600s) tells well-behaved clients to cache the NXDOMAIN response and stop asking. Logwriter dedup prevents database bloat when clients ignore the TTL. DNS throttling (1s sleep) actively slows down misbehaving clients to reduce network and CPU load.

**Thresholds**: Blocked traffic triggers dedup at 25 TPS (nearly 2x the observed 14 TPS peak). Allowed traffic triggers at 100 TPS (much higher threshold since legitimate traffic bursts are common). Once a `(client_ip, query_name)` pair enters coalesced mode, it stays sticky — the existing entry's `coalesced_count` is incremented rather than inserting new rows. The `doomLoopActive` sync.Map is shared between the logwriter and DNS handler for cross-goroutine coordination.

**`coalesced_count` column**: Added to `query_logs` with DEFAULT 1. All stats queries (`stats_api.go`, `analysis.go`, Prometheus counters) use `SUM(coalesced_count)` instead of `COUNT(*)` to ensure metrics remain accurate even when rows represent multiple coalesced queries. Parquet schema updated to include the column.

**Anomaly alerting**: When a doom loop is detected, the logwriter emits a structured log entry via the `logLW` component logger. With Loki push enabled, this flows to Grafana for alerting.

**Revisit if**: Threshold needs adjustment based on new traffic patterns, or if the 1s DNS sleep causes issues with legitimate rapid queries (unlikely given the per-domain+per-client scoping).
