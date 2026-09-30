# DD-017: Skip OpenTelemetry traces for DNS

**Decision**: No OTel tracing for svart-dns. DNS queries are single-hop, leaf-node operations with no trace propagation.

**Why**: There's no `traceparent` header in DNS — you can't connect "user resolved reddit.com" to "user then called reddit.com's API." A trace with one span is a fancy log line. The structured per-query logs already in Loki (from Phase 5.1) contain every field a trace would: client_ip, domain, query_type, rcode, latency, policy tier, list name. Adding OTel would mean a new dependency (`go.opentelemetry.io/otel` + exporter), context propagation plumbing on the hot path, and a Tempo backend to store single-span traces — all for data that's already in the structured logs.

**Revisit if**: We add cross-service HTTP APIs (e.g., multi-instance sync in Phase 6) where trace propagation across nodes would actually be useful. For the HTTP admin API specifically, the existing `instrumentHandler(mux)` pattern from `promhttp` is a 5-line drop-in if we ever want it.

## Control-plane revisit

The documented revisit condition now applies to mesh sync and the admin API.
Optional OpenTelemetry spans cover HTTP, outbound sync/list requests and
background/storage operations. The DNS path still has no per-query spans.
See [Self-hosted observability](../observability.md) for configuration and proof.
