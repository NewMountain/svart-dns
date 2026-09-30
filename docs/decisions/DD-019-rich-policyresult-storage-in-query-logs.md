# DD-019: Rich PolicyResult storage in query_logs

**Decision**: Store the full `PolicyResult` evaluation tree in query_logs via two layers: (1) explicit SQL columns for the winning result, per-tier evaluations, and `result_reason` for fast querying/aggregation, and (2) a `policy_json` TEXT column containing the complete serialized `PolicyResult` as JSON for per-row detail views.

**Why**: The original 5-column flattening (`block_tier`, `block_source`, `block_rule`, `block_list_id`, `block_list_name`) was lossy — it discarded `CustomRule.Action` (can't distinguish custom allows from custom blocks), dropped per-tier evaluations, and when both published and custom rules matched at the same entity, only one was kept. The `result_reason` column enables trivial queries like `WHERE result_reason = 'custom_block'` instead of the fragile `WHERE blocked=1 AND block_list_id=0 AND block_rule != ''` workaround. Per-tier columns (`range_result`, `group_result`, `ip_result`) let the Logs UI show which tiers participated in each decision without parsing JSON.

**Schema**: 28 new columns total. Top-level: `result`, `result_reason`, `client_name`, `policy_json`. Winning source: `result_tier`, `result_entity`, `result_is_published`, `result_rule`, `result_list_id`, `result_list_name`. Per-tier (×3): `{range,group,ip}_{result,entity,is_published,rule,list_id,list_name}`. Legacy columns kept for backward compat with old rows. Indexes on `result` and `result_reason`.

**Hot path impact**: `json.Marshal` of a small `PolicyResult` struct is ~1µs, acceptable since it runs in `logQuery()` before the entry is queued to the async log writer channel. The `clientAliasCache` (atomic.Value, map lookup) denormalizes `client_name` in sub-100ns.

**Prometheus** (all incremented in the logwriter flush loop — off hot path):

| Metric | Labels | Purpose |
|--------|--------|---------|
| `svart_dns_result_reasons_total` | `reason` | Result reason distribution (published_block, custom_block, custom_allow, published_allow, default_allow, rewrite) |
| `svart_dns_decisions_total` | `tier`, `result`, `source` | Winning policy decisions — which tier decided, what result, published vs custom |
| `svart_dns_list_decisions_total` | `list_name`, `result` | Published list attribution for winning decisions globally — "Hagezi Pro blocked 12k queries" |
| `svart_dns_tier_evaluations_total` | `tier`, `result` | Per-tier evaluation outcomes including non-winning tiers — "range wanted to block but IP-level allowed" |
| `svart_dns_tier_list_hits_total` | `tier`, `list_name`, `result` | Published list hits per tier — which list won at which tier |
| `svart_dns_tier_custom_hits_total` | `tier`, `action` | Custom rule hits per tier and action (block/allow) |
| `svart_dns_entity_decisions_total` | `tier`, `entity`, `result` | Entity-level decisions for range and group tiers only (named ranges/groups) |
| `svart_dns_client_list_hits_total` | `client_ip`, `list_name`, `result` | Per-client list attribution — "Hagezi Pro blocked Chris's Macbook 500× but Sam's iPhone only 12×" |
| `svart_dns_client_custom_hits_total` | `client_ip`, `action` | Per-client custom rule impact — spot users disproportionately hitting custom rules |

**Cardinality**: Highest-cardinality metric is `dnsClientListHits` (~300 IPs × ~10 lists × 2 results ≈ 6,000 series). Trivial for Prometheus. `dnsEntityDecisions` only emits for range (~10) and group (~10) tiers — IP tier uses the targeted per-client metrics instead. Domain-level analysis (e.g., "how often is google.com blocked vs allowed") is too high cardinality for Prometheus counters — use SQL queries against `query_logs` in Grafana instead.

**Revisit if**: The 28 extra columns cause measurable INSERT slowdown in the logwriter (unlikely — SQLite WAL batch inserts are I/O-bound, not schema-bound).
