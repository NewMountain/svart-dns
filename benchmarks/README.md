# Reproducible comparison and footprint measurements

`make test-benchmarks` runs the separate Go module's vet and unit tests; it is
also part of `make verify`. The current comparison is `compare.sh`: Svart, Pi-hole, AdGuard Home and
Technitium, sequentially, using cached list files and a deterministic local DNS
upstream. See [the measured results](../docs/benchmarks.md) for versions,
resource use, limitations, and Raspberry Pi guidance.

```bash
# Build before moving the binary to a compatible isolated Linux host.
go build -o bin/svart-dns ./cmd/svart-dns
SERVER_CPUS=0-3 BENCH_CPUS=4-7 STUB_CPUS=8-9 \
  SVART_BIN="$PWD/bin/svart-dns" benchmarks/compare.sh /path/to/lists /path/to/results
PROFILE_DIR=/path/to/footprint benchmarks/footprint.sh bin/svart-dns /path/to/lists
```

Use a disposable host or container with loopback networking and cached images.
Never benchmark production DNS. `compare.sh` requires Docker, Go (or prebuilt
`bin/dnsbench` and `bin/stubupstream`), curl, Python 3, `dig`, `ss`, and `taskset`.
For footprint runs without Go, set `BENCH_BIN_DIR` to the prebuilt tools.
The CPU sets must exist and must not overlap. List files must already exist;
`fetch-lists.sh` is an explicit preparation step with network access.

`results.jsonl` retains counts, response latency percentiles, successful-response
throughput, and RSS for every scenario. Timeouts and DNS errors are separate
counts; latency percentiles exclude failed requests. Fixed-rate latency starts
when a query is sent and excludes load-generator scheduling delay. It is not a
coordinated-omission-corrected open-loop latency measurement. The seeded query
sequence is identical across products. The popular-domain pool includes names
present in the blocklist, so a scenario named `hits` can legitimately block some
queries. `expected_blocked`, `unexpected_allowed`, and `unexpected_blocked` use
exact names in the supplied list; this validation is for the benchmark's plain
domain lists, not a general filter-syntax or wildcard validator. `REFUSED` and
`SERVFAIL` count as errors, never successful blocking or answers.

Footprint output reports RSS and Go heap-in-use before a forced collection.
With `PROFILE_DIR`, each row also saves a forced-GC heap profile, metrics, and
process status; live heap from that profile is a different measurement from
heap-in-use. RSS includes native DuckDB/SQLite and runtime memory. Profiles
and final server logs survive cleanup. Set `PROFILE_GC=false` to collect
metrics/status without forced collection when comparing older binaries that
lack the profiling endpoint. `cache-load.json` records whether all
200,000 cache-filling queries actually succeeded.

## Historical harness notice

Older measurement tables remain in [the benchmark history](../docs/benchmarks.md).
Their Compose harness, `make bench-e2e*` targets, `benchmarks/run.sh`, and single-file
`dnsbench.go` entry point no longer exist. Those historical measurements cannot be
reproduced with the current harness as if they were the same experiment. Use the
current commands above for new measurements; preserve old result archives as evidence.

The current command packages are `benchmarks/dnsbench`, `benchmarks/stubupstream`,
and `benchmarks/dnsdiff`, each inside the separate `benchmarks` Go module.

Comparison runs require a new result directory and retain complete disposable
state under `work/`, per-scenario JSON, startup phase timestamps, every sampled
process status and I/O record, final steady RSS, and competitor data/configuration.
Container filesystems are also exported; declared data volumes are copied separately.
The retained fixture state includes generated benchmark credentials and must stay
private. Publish only inspected result summaries and scrubbed metadata.

Set `BENCH_BIN_DIR` to prebuilt helper binaries so compilation cannot overlap timing.
Set `DOCKER_NETWORK=container:<isolated-world>` when the harness runs inside that
same network namespace; it must contain the local list and DNS servers. The default
is host networking for compatibility and should only be used on a disposable host.
`CONTAINER_PREFIX` chooses unique disposable names; `SERVER_MEMORY` defaults to 16g
with swap disabled for competitors. Image variables accept immutable cached IDs or
digests; retain their resolved identities in the run manifest. CPU sets must include
only disjoint physical cores when comparing capacity. The generator's 64-source
scenario uses `SOURCE_NET` (default `127.0.0.0`). Svart remaps loopback clients
to one effective external identity, so loopback aliases alone do not measure
64-client policy cardinality. For that measurement, provision a local route
for `10.20.0.0/24` inside the disposable network namespace, set
`SOURCE_NET=10.20.0.0`, and verify 64 effective client identities in raw records.

Pi-hole starts with the local URL already in `adlists.list`. Its gravity script
checks resolution of `raw.githubusercontent.com` even for a literal-IP list URL,
so this name maps to loopback inside the disposable container. This satisfies the
bootstrap prerequisite without enabling an external download or upstream. The
local blocklist remains the only configured list. Startup phase timestamps are
observations of initial API/DNS readiness and confirmed blocking; Pi-hole loads
gravity before DNS readiness, whereas the other products add their list after
initial setup. Compare launch-to-confirmed-blocking for total cold setup.

For a queue observation immediately after DNS workers finish, pass
`-json -completion-url http://127.0.0.1:PORT/metrics` to `bin/dnsbench`.
The helper performs this GET before sorting latency samples. Its JSON retains
all DNS results, the full response body and status, the client completion time,
and the HTTP observation's start/end timestamps. Report that observation lag;
the metrics are not an atomic snapshot at the last packet. The HTTP request is
excluded from DNS latency and throughput. A failed snapshot retains the DNS
summary and diagnostic response but exits unsuccessfully. Later SQLite counts
must be labeled with their own observation times.

Technitium 15.5 uses prefix-based QPM tables. The comparison sends
`qpmPrefixLimitsIPv4=false&qpmPrefixLimitsIPv6=false`, then checks the effective
settings before sending traffic. The obsolete `qpmLimitRequests` and
`qpmLimitErrors` fields are silently ignored by that API. Full settings readback
is retained with each run. In this UDP-only benchmark, a TC-bit response is
counted as an error: it asks the client to retry over TCP and is not a complete
successful UDP answer. The generator does not silently change transports.
