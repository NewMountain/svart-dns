# Paired optional telemetry overhead — 2026-09-29

Application source `45047b6`, unchanged Go 1.26.8 production build, SHA256
`4c312b2ab4be419d825603f64b4c6594557899f337ee18aae553d1f9192c41b3`.
Optional OTLP tracing and Pyroscope profiling were disabled/enabled in three
alternating pairs. Each run sent 300,000 actual UDP queries at a requested
10,000 QPS, using 64 effective client addresses and 5% unique misses. Query
persistence stayed enabled and `LOG_QUERIES` stayed disabled in both modes.

| Pair | Telemetry | Replies/s | Median µs | p99 µs | Median workload RSS MiB | Peak RSS MiB |
|---|---|---:|---:|---:|---:|---:|
| 1 | Off | 9,993 | 105.5 | 2,600.3 | 180.1 | 239.2 |
| 1 | On | 9,994 | 102.8 | 2,511.1 | 219.8 | 258.7 |
| 2 | On | 9,996 | 58.4 | 1,657.2 | 225.5 | 264.3 |
| 2 | Off | 9,996 | 58.2 | 1,722.7 | 217.1 | 256.8 |
| 3 | Off | 9,996 | 59.8 | 1,652.9 | 217.1 | 256.0 |
| 3 | On | 9,998 | 58.7 | 1,677.4 | 221.6 | 258.2 |

All 1.8 million queries received successful answers; there were zero DNS
errors or timeouts. Every enabled run delivered seven OTLP HTTP batches and
six profile uploads to the local protocol collectors. At least five trace
batches and two profile uploads occurred during each measured workload,
excluding a one-second margin at each end. Every recorded collector response
was HTTP 200, and complete request bodies and byte digests are retained.

This test found no consistent response-latency increase from enabling these
exporters at this load. It does not establish maximum throughput, statistical
equivalence, or behavior at exporter saturation. RSS varied across runs; the
first pair especially must not be used alone as a causal memory estimate.
The profiler's production configuration disables forced GC.

The isolated network namespace used server cores 2–3, generator core 4, and
local stub core 5, with a total four-CPU limit and 4 GiB/no-swap service limit.
Cooperating local builds were paused for all six runs. Other user workloads
were not stopped. Full `/proc/stat`, memory, load, and pressure samples record
remaining shared-host conditions. The fixed-rate generator measures latency
from actual send time, excluding its own scheduling delay.

`results.json` retains every DNS result and collector/resource summary. Full
private raw evidence, databases, exact pre-archive journals, process samples,
HTTP bodies, controller commands and build manifests remain in the retained
private `final-telemetry-*` evidence directories.
Completion durability snapshots were taken after the generator serialized its
summary; they are not exact observations at the last packet and are not used
as a last-packet durability claim in this telemetry result.
