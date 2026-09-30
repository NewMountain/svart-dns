# DD-015: Prometheus metrics — everything off the hot path

**Decision**: All Prometheus metric increments happen off the DNS hot path. Counter and histogram observations (queries total, per-client, per-tier, per-list, query types, response codes, query duration) are performed in the logwriter's background `flush()` goroutine, right next to the existing slog emission loop. Upstream metrics (per-upstream queries, errors, latency) are recorded in `recordSuccess`/`recordFailure` which only fire on cache misses. Gauge functions (cache stats, policy cache stats, uptime, log writer health) are evaluated lazily at Prometheus scrape time. Zero new instructions on the DNS hot path.

**Metrics lag**: At most ~100ms (the logwriter flush interval). Invisible to Prometheus's 15s default scrape interval.

**Cardinality choices**: `svart_dns_client_queries_total{client_ip, action}` has ~50 IPs × 4 actions = ~200 series in a homelab. `svart_dns_blocks_by_list_total{list_name}` has ~10-20 series (one per active blocklist). `svart_dns_upstream_queries_total{upstream}` has ~3-5 series. All well within Mimir's comfort zone.

**Per-list inventory gauges**: `svart_dns_blocklist_domains{list_name}` and `svart_dns_allowlist_domains{list_name}` (distinct rules per list) are GaugeVecs updated by every policy reload — `Reset()` then re-populate to remove stale list names. `svart_dns_list_index_bytes` is the shared list index's size, `svart_dns_policy_reload_errors_total` counts reloads that failed and left the previous policy active, and `svart_dns_policy_cache_max_bytes` is the policy cache budget.

**Library**: `github.com/prometheus/client_golang` — self-contained, no telemetry, the standard Go Prometheus library. `promhttp.Handler()` serves `/metrics` endpoint, exempt from auth (same as `/health`).

**Histogram buckets**: Query duration uses 14 buckets from 50μs to 1s, fine-grained at the low end because p50 is 566μs. Upstream duration uses 11 buckets from 1ms to 2.5s.

**What this powers**: Queries/sec by action, block rate %, latency p50/p95/p99, top clients by volume/block rate, upstream distribution pie chart (privacy spread), blocks by list (blocklist FOMO killer), cache hit rate, policy cache memory, log writer backpressure, query type distribution, response code distribution.

**Revisit if**: Cardinality grows beyond homelab scale (thousands of unique client IPs), or if the logwriter flush loop becomes a bottleneck from metric increments (unlikely — counter/histogram operations are ~100ns each).
