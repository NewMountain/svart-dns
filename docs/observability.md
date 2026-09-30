# Self-hosted observability

Svart exposes Prometheus metrics and writes structured JSON to stdout by default.
Structured service logs have `service="svart-dns"`; control-plane request and operation logs also
carry the active `trace_id` and `span_id`. JSON remains compatible with the
optional durable direct-Loki outbox. `LOG_FORMAT=text` explicitly selects a
terminal presentation instead.

Tracing and continuous profiling are independently disabled until configured:

| Variable | Default | Meaning |
| --- | --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | Self-hosted Alloy HTTP origin, e.g. `http://alloy:4318`; the SDK sends OTLP traces to `/v1/traces`. |
| `PYROSCOPE_SERVER_ADDRESS` | unset | Self-hosted Alloy profile receiver origin, e.g. `http://alloy:9999`. |
| `LOG_QUERIES` | `false` | Export identifying DNS query lines and diagnostic fields. Owned SQLite/raw history is retained regardless. |

Collector URLs must be absolute HTTP(S) origins without credentials, paths,
queries, or fragments. Invalid configuration fails startup without echoing its
contents. No hosted telemetry backend, runtime asset download, or per-query DNS
span is introduced. Release builders that exclude `.git` must pass the immutable source SHA with
`-ldflags="-X github.com/yeti/svart-dns/internal/telemetry.buildRevision=SOURCE_SHA"`.
This compile-time value takes precedence over automatic Go VCS metadata.
Profiles identify the compiled VCS revision, including a
`-dirty` marker when appropriate. The profiler collects CPU, allocation, live
heap, and goroutine samples; it does not force garbage collections between
samples. Collector errors remain structured service logs. Trace batching applies
backpressure instead of deliberately dropping spans when its queue fills.

HTTP traces and metrics use registered mux patterns and a bounded method set.
They do not include raw paths, query strings, request/response bodies, credentials,
or client addresses. Outbound sync/list HTTP spans carry only the method and
status. Background spans cover sync, list discovery/download/storage, raw archive
cycles, and durable Loki delivery. Error logs at the existing operation owner
retain diagnosis; correlation logs contain no private payload.

`svart_dns_query_duration_seconds` retains its original processing semantics.
`svart_dns_response_duration_seconds` measures the complete DNS handler, including
log admission, asynchronous queue insertion, and socket write, using the existing
coalesced clock. It includes rejected requests. The separate
`svart_dns_log_admission_duration_seconds` and
`svart_dns_log_admission_total{outcome="accepted|rejected"}` distinguish admission
pressure from upstream overload. These observations do not add a filesystem
sync to the DNS reply path.

`svart_queue_pending_records` and `svart_queue_oldest_age_seconds` describe
persisted, unacknowledged presentation/Loki ownership. The age is that of the
first owned journal record. Invalid timestamps and failed storage reads produce
NaN, never healthy zero. `svart_journal_failures_total` and
`svart_journal_unavailable` expose commit failure and recovery; unavailable also reports unreadable or corrupt ownership state. Existing volatile
queue depth, journal bytes, raw archive ownership/integrity, reload failures,
and upstream metrics remain in place.

## Collector and operator surface

The infrastructure repository owns `lgtm/grafana/dashboards/svart-dns.json`, UID
`svart-dns`. Its original 20 panels remain unchanged; HTTP/background operations,
full DNS response/admission, durable queues, logs, traces, and a CPU flamegraph
extend that dashboard. Provisioned alerts cover service down, missing scrapes,
HTTP errors, failed jobs, stuck queues, and archive/reload failures. Unknown data
and evaluation errors stay actionable. The existing global blocked-query-rate
policy remains; it is a fleet-wide warning, not evidence of one client's loop.
The existing Murmur notification routing is unchanged.

For Docker discovery use `prometheus.scrape=true`, `prometheus.port=3000`,
`prometheus.path=/metrics`, `prometheus.scheme=http`, and `service.name=svart-dns`
(or the configured TLS port/scheme). The established infrastructure's two
remote Svart targets retain their static Alloy scrape configuration. The Alloy
profile receiver uses port 9999 and requires its `public-preview` component
stability level in collector versions where `pyroscope.receive_http` is preview.
Do not enable both stdout shipping and direct Loki delivery into identical
streams unless the deliberate duplicate ingestion is understood.

## Disposable proof

`tests/observability/run.sh INFRA_CHECKOUT NEW_EVIDENCE_DIRECTORY` builds the
source-matched frontend and executable, starts real Alloy, Prometheus, Loki,
Tempo, Pyroscope, Grafana and a local list fixture on an internal Docker network,
then tears down only its own Compose project. Aggregate CPU limits total four.
The host must already have the required container images; every image is resolved
to its immutable local ID and pulls are disabled. Override `APP_IMAGE`,
`FIXTURE_IMAGE`, `ALLOY_IMAGE`, `PROMETHEUS_IMAGE`, `LOKI_IMAGE`, `TEMPO_IMAGE`,
`PYROSCOPE_IMAGE`, or `GRAFANA_IMAGE` with the approved cached image reference.
Run `npm ci --prefix scripts` to install the pinned Playwright library and set
`PLAYWRIGHT_BROWSER` to its installed Chromium executable. The test grants only
the disposable Grafana HTTP origin a Chromium secure context, matching the
CacheStorage APIs available to production HTTPS; browser errors still fail.
It never accepts production endpoint arguments or mounts a production database.

The test proves real authenticated API mutations, a downloaded/persisted list,
400 UDP replies, exact fresh trace/log correlation, fresh `up == 1` and domain
metrics, service-scoped CPU profiles, populated browser-rendered Grafana panels,
and loaded normal alerts. It then stops its Alloy, deletes only synthetic `up`
samples from its disposable Prometheus, observes the real absence alert fire,
and verifies recovery after restarting collection. Logs, image/source/binary
provenance, complete query responses and screenshots remain in the evidence
directory even when verification fails. Never reuse an existing evidence folder.

After CI deploy, repeat the read-only signal queries from the evidence: fresh
Loki logs from the deployed version, `/metrics` and Prometheus `up`, a real
control request's exact trace in Tempo, service-version profiles, the rendered
dashboard, and loaded alert states. Production absence injection and load tests
are not part of that release check.
