# Performance Benchmarks

## Qualified list-rule cost — 2026-09-29

The supported AdGuard syntax adds work on an uncached policy lookup. Ordinary
rules retain the compact tables; qualified masks and regexes compile once per
snapshot and run once per queried name/type, shared across policy entities.
A required-literal check rejects impossible regex matches before evaluation.

These microbenchmarks compare baseline `1de8112` with matcher `d6b89af` in the
frozen clean candidate tree `e65b49c`. Both used Go **1.26.8**, the same Ryzen
5950X/Linux host, `GOMAXPROCS=2`, a two-CPU quota and a 4 GiB memory limit.
Builds were measured sequentially, with **five 300 ms samples** per case;
the table reports medians. Frozen source files, fixtures and raw samples have
SHA-256 manifests. These are policy timings, not DNS response latency or a
repeat of the end-to-end comparisons below.

| Operation | Baseline | Qualified-rule build | Allocations per operation |
|---|---:|---:|---:|
| Cached policy evaluation | 64.31 ns | 68.61 ns (+6.7%) | 0 → 0 |
| Uncached ordinary policy evaluation | 647.3 ns | 644.4 ns (−0.4%) | 3 → 3 |
| Ordinary exact-name index probe | 45.67 ns | 49.00 ns | 0 → 0 |
| Ordinary parent-name index probe | 68.62 ns | 72.09 ns | 0 → 0 |
| Ordinary index miss | 56.57 ns | 51.33 ns | 0 → 0 |
| Build 10,000 ordinary rows | 1.65 ms | 1.85 ms | 21,984 → 21,983 |

A frozen published-list fixture contained **574 qualified rules**: 483 masks,
69 regexes, 10 subtree rules and 12 exact rules, including 9 type restrictions,
183 rule-local exclusions and 6 exceptions. With 10,000 ordinary rows alongside
it, seven representative matching/nonmatching name/type cases took
**4.18–5.21 µs**, all with **zero allocations**. An adversarial valid 247-byte
name took **51.61 µs**, also without allocation. Cost varies with rule patterns,
name length and successful candidates; this does not promise a constant-time
lookup or a universal 10 µs ceiling. A separate 70-rule TXT-only fixture rejected
A queries in 0.32–0.33 µs. The initial implementation took 90–256 µs on the
ordinary-length real-fixture cases; profiling identified regexp evaluation,
and the literal prefilter reduced that cost without dropping supported rules.

Three fresh-process measurements, after warm-up and forced garbage collection,
found median retained Go-heap increments of **373,328 bytes** for the baseline
10,000-row index and **375,248 bytes** for the candidate. Adding the 574 qualified
rules raised the candidate to **1,618,400 bytes**, an additional **1.19 MiB**.
This measures retained Go heap, not RSS, whole-service RAM or Raspberry Pi
requirements. Building the mixed fixture took **11.63 ms** and allocated
**11.97 MB** in total; that operation includes fixture decoding, stored-rule
encoding and index construction, not just regexp compilation.

The frozen mixed fixture's SHA-256 is
`4e460a72e6a148146b4dd70ce0799c92e9ac33cfae9e90eeac657f13c6644340`.
The later parser identity fix changes refresh-time cancellation, not the measured
stored-rule matcher. Its separate 690-case comparison against pinned AdGuard
`urlfilter v0.23.4` found no unexpected differences; the five intentional legacy
bare-wildcard differences remain outside the parity claim.

## Backend measurements — 2026-09-29

The final backend adds exact raw-history retention and faster policy edits; it
does not establish equal DNS throughput to the older build. The measurements
below use backend `ab901e3` and production baseline `b67c6f4`, both built with
Go 1.26.8, on the same x86-64 host. Subsequent mobile layout and CI edits do not
change the measured backend. Full methods, immutable fixture identities and
limitations are in [the final evidence](../benchmarks/evidence/2026-09-29-final/README.md).

### DNS throughput, latency and retained logging

With 500,000 names loaded, the paired baseline/final sequence measured 163,150
versus 85,766 replies/s for popular names. After the preceding saturation
scenarios, fixed 10,000 offered QPS produced p99 **3.18 ms versus 18.89 ms**.
Both builds had timeouts in the 200-worker saturation case: 85 versus 94 out of
300,000 requests. The other paired scenarios had zero timeouts or DNS errors.
The exact raw retention path has material cost and bounded queues can backpressure
DNS replies; these results do not establish equivalent performance.

Fresh-process logging trials separated persistence on/off across three alternating
pairs. Final median throughput was 138,818 replies/s with persistence off and
64,595 with it on, versus baseline 159,419 and 89,032. All twelve runs answered
every request. Longer off-path diagnostic profiles were nearly equal, so the
short-run 13% off-path difference is not a demonstrated causal CPU regression.
The enabled profiles showed additional SQLite, replay and allocation work;
full metrics polling at 1 Hz also consumed material CPU while backlogged.
[Logging measurements](logging-durability.md#final-backend-measurements--2026-09-29)
include queue sizes, drain times and the accepted volatile crash tail.

Seven persistence checks found **2,572,864 records** across the journal/archive
union, with the expected request-name multisets and selected field invariants.
**1,589,824 journal payloads** matched byte-for-byte between independent retained
snapshots. The million-query outage had already archived **983,040 records**
before the first snapshot; both verifier passes read the same archive files.
Those records have count/name/selected-field checks, but this benchmark does
**not** independently prove their complete payloads survived archival unchanged.
All **1,786,432** direct stdout query lines appeared byte-identically in
acknowledged Loki requests, with no missing lines or observed duplicates. These
lines omit some raw fields and are not a complete raw-payload witness. Clean
drain does not make asynchronous admission crash durable.

### Four configured products


Each cell is successful replies per second. Products retain their default history settings: Svart exact raw persistence is enabled, while Technitium query logging is disabled. These are not equal-durability engine comparisons.

| Scenario | Svart QPS | Pi-hole QPS | AdGuard Home QPS | Technitium QPS |
|---|---:|---:|---:|---:|
| Popular names, 50 workers | 85,300 | 12,901 | 46,626 | 120,989 |
| 20% list queries, 50 workers | 72,713 | 11,849 | 51,954 | 157,696 |
| 100% list queries | 54,160 | 9,014 | 54,645 | 129,941 |
| 10% list + 50% unique misses | 32,453 | 3,975 | 30,912 | 43,195 |
| 20% list queries, 200 workers | 44,933 | 8,783 | 61,018 | 156,535 |
| 64 effective clients, 5% misses | 37,261 | 4,699 | 59,074 | 166,337 |
| Fixed 10,000 offered QPS | 9,994 | 7,102 | 9,999 | 9,996 |

Fixed-rate scenario:

| Metric | Svart | Pi-hole | AdGuard Home | Technitium |
|---|---:|---:|---:|---:|
| p50 µs | 386.7 | 18,726.7 | 100.5 | 52.8 |
| p99 µs | 28,234.4 | 50,375.8 | 1,431.1 | 1,660.5 |
| RSS MiB after sequence | 748.5 | 712.7 | 446.9 | 509.7 |

The first Technitium fixed-rate scenario overlaps the unrelated workload from 08:02:16 UTC; its fixed-rate numbers are contended. Other first-order scenarios finished before that start time.

All nonzero failures/policy exceptions:

- svart/mixed-c200: timeouts 95, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.
- pihole/mixed-c200: timeouts 561, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.
- pihole/fixed-10k: timeouts 695, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.
- technitium/blocked: timeouts 0, errors 0, unexpected allowed 1, unexpected blocked 0 of 300000.

Technitium's single unexpected allow is `localhost.localdomain`, a built-in
local-zone exception. All other completed answers matched the exact-name policy
oracle; all other scenarios had zero timeouts/errors. Pi-hole achieved about
7,102 successful replies/s at 10,000 offered QPS, so that row is not an equal
achieved-load latency comparison. Svart's 28.23 ms p99 followed accumulated
query-history work in the same process; it must not be substituted with the
lower fresh-run latency. The reverse order was affected by a competing CPU
workload and is retained separately in the [complete tables](../benchmarks/evidence/2026-09-29-final/results.md).

### Optional telemetry at fixed load

Three alternating off/on pairs sent **1.8 million queries** at 10,000 offered
QPS, with query persistence enabled. Every query succeeded. Off/on median resident
memory during load was 212.5–215.7 / 218.1–219.7 MiB; peak RSS was at most
261.7 MiB. p99 was 2.63–6.02 ms off and 1.54–8.28 ms on, with no consistent
latency direction across the pairs. Each enabled run delivered seven trace and
six profile requests; both kinds were observed during DNS load, not just at
shutdown. This verifies telemetry delivery under fixed load, not its maximum
throughput cost. [Per-run timings and collector checks](../benchmarks/evidence/2026-09-29-final/telemetry-summary.json)
retain all six observations.

### Three million rules and concurrent edits

Both fresh processes loaded the same three lists of one million rows each.
Each manual edit was checked through the real client API and an actual blocked
UDP response. A changed million-row list was then refreshed while list APIs
continued serving; completion required the new rule to block DNS queries.

| Operation | Baseline | Final backend |
|---|---:|---:|
| Three manual-domain edits, each | 1.94–2.14 s | 1.61–1.93 ms |
| Refresh through verified DNS publication | 10.48 s | 10.35 s |
| Domain-page reads during refresh | 104 ms–7.29 s | 78–135 ms |
| Kernel main-process peak RSS over sequence | 1,142 MiB | 1,107 MiB |

This is one paired execution with three edits, not a confidence interval. The
final run served 38 polling iterations during refresh; baseline served 11.
Both finished with exactly one million rows in each original list and all
three custom rules. The refreshed list still requires complete parsing and
storage work; a small manual edit no longer rebuilds all three million rules.

### Fifty million events

The actual live SQLite dashboard endpoints returned 144 independently checked
responses across 1h/24h/7d windows. Uncached seven-day panels took tens of
seconds; immediate repeats returned cached responses in milliseconds. Peak
main-process RSS was 392 MiB for the final backend. This is HTTP panel timing,
not total browser paint time. The relative time windows advance between runs,
so their small timing differences must not be presented as causal speedups.
[Full live-history results](../benchmarks/evidence/2026-09-29-history/README.md)
retain exact counts, response bodies and resource measurements.

A separate fixture contained exactly 50 million archive-backed rows/events.
The real Investigation count/sum/timestamp query returned all 50 million in
20.18 s. The four actual UI templates took 28.19–40.88 s and matched independent
oracles, including all 576 grouped daily client counts. A one-second requested
deadline returned HTTP 504 in 1.01 s and left the service healthy. A modified
archive was rejected with HTTP 503; restoring its original byte restored the
exact 50-million-row result. Peak simultaneously sampled process RSS was
297 MiB, including the isolated native worker; this excludes filesystem cache
and OS memory and is not a machine RAM ceiling.

## Previous release comparison — 2026-09-26

Svart is competitive, but it is not the fastest or smallest in every workload.
In this single run, Technitium led cached/blocked throughput, Svart led the
cache-miss mix and had the lowest p99 at a shared 10,000 queries/sec, and AdGuard
Home used less RSS than Svart throughout the sequence. These are measurements
on x86-64, **not Raspberry Pi results**.

The application code was `4f20de8`, built with Go 1.27.1, CGO and a Debian
Bookworm C/C++ toolchain. The other versions were Pi-hole **2026.09.0**, AdGuard
Home **v0.107.79**, and Technitium **15.5.0**. All used the same cached Hagezi Pro
snapshot (228,217 domains), seeded query sequence, and loopback UDP stub upstream.
The disposable Debian 12 guest had 16 GiB RAM, no external network interface,
and ran on an AMD Ryzen Threadripper 7960X / Linux 7.0.14-17-pve. Each server got
four physical cores/eight threads (`12-15,36-39`); the generator used
`18-23,42-47` and upstream `16-17,40-41`. Products ran sequentially; the physical
host was shared, so these are indicative single-run results, not confidence
intervals or dedicated-hardware capacity guarantees. Svart ran as a native
process; competitors used cached containers with host networking.

Each scenario sent 300,000 measured queries after 5,000 warmup queries.
The 50/200 concurrent workers used separate UDP sockets but **one source IP**
(`127.0.0.1`), not 50/200 distinct policy clients. This throughput test does not
exercise high client-policy cardinality or per-client metrics. Scenarios
ran in the displayed order in one server process, so RSS includes accumulated
caches and query history. Defaults were retained except per-client rate limits
were disabled in all three competitors, Pi-hole received 1 GiB `/dev/shm`, and
Technitium DNSSEC validation was disabled because the synthetic upstream cannot
provide signed delegations/DNSKEYs. This comparison does not measure DNSSEC,
public-upstream latency, DoH/DoT, archival analytics, or long-term memory peaks.

### Successful-response throughput (queries/sec)

| Scenario | Svart | Pi-hole | AdGuard Home | Technitium |
|---|---:|---:|---:|---:|
| Popular names, 50 workers | 123,469 | 13,381 | 134,476 | 190,219 |
| 20% list queries, 50 workers | 122,876 | 12,597 | 128,313 | 246,480 |
| 100% list queries, 50 workers | 110,914 | 10,752 | 122,681 | 233,479 |
| 10% list + 50% unique misses | 95,700 | 5,803 | 84,083 | 54,009 |
| 20% list queries, 200 workers | 129,719 | 12,469 | 135,148 | 243,302 |

Pi-hole had **396 timeouts / 300,000 requests (0.132%)** in the 200-worker mixed
scenario. All other scenarios had zero timeouts; all had zero DNS error replies.
Across all four products, all 300,000 queries in each blocking-only scenario
were blocked. For every received response in all 24 scenarios, expected exact-name
blocking matched the list: zero unexpected allows or blocks. Popular names
include 2,948 blocked queries, so the first row is not literally 100% cache hits.

### Latency at the same offered load

The fixed-rate scenario offered 10,000 queries/sec, with 200 workers, 20% list
queries and 5% unique misses. Every product completed all 300,000 requests with
zero timeouts/errors. Latency begins at send time, excludes generator scheduling
delay, and is not corrected for coordinated omission.

| Metric | Svart | Pi-hole | AdGuard Home | Technitium |
|---|---:|---:|---:|---:|
| p50 (µs) | 56.5 | 587.2 | 77.4 | 62.2 |
| p95 (µs) | 103.4 | 1,911.3 | 141.8 | 91.9 |
| p99 (µs) | 130.4 | 3,853.8 | 299.3 | 241.5 |
| RSS after sequence (MiB) | 562.2 | 623.8 | 276.3 | 553.9 |

RSS is process resident memory (summed across processes for containers), not
`docker stats` or total machine memory. It is not a minimum RAM requirement;
it excludes the OS and filesystem cache. Percentiles throughout the raw data
are for successful responses and exclude timeouts/errors.

Raw [per-scenario results](../benchmarks/evidence/2026-09-26/comparison.jsonl),
[environment](../benchmarks/evidence/2026-09-26/environment.txt), and
[measurement decisions](../benchmarks/evidence/2026-09-26/decisions.md) are retained.
See [the harness instructions](../benchmarks/README.md) for reproduction.
Earlier attempts were rejected: the upstream omitted the DNS recursion-available
flag, the old classifier treated REFUSED as blocking, and Technitium's DNSSEC
validation correctly rejected the unsigned stub. No speedup percentage uses
those invalid attempts as a baseline.

## Previous release memory footprint — 2026-09-26

The same corrected footprint harness ran both retained binaries with natural
Go garbage collection (`PROFILE_GC=false`) on the guest described above.
Each started with fresh state, loaded the same five cached lists, created 20
ranges sharing the first two lists, and sent 200,000 unique names to the local
upstream. All 200,000 queries succeeded on both builds, with no blocks, DNS
errors or timeouts. The ranges measure sharing overhead; they do not represent
20 simultaneous traffic sources or a full three-tier policy workload.

| State | Retained baseline RSS (MiB) | Current `4f20de8` RSS (MiB) |
|---|---:|---:|
| Empty | 117.1 | 115.2 |
| + Steven Black (76,514 rows) | 187.5 | 135.2 |
| + Hagezi Pro (304,731 total) | 291.7 | 216.0 |
| + OISD Big (548,599 total) | 364.4 | 238.0 |
| + Hagezi Ultimate (832,967 total) | 409.2 | 281.7 |
| + Hagezi TIF (3,124,441 total) | 676.8 | 655.4 |
| + 20 ranges sharing two lists | 677.1 | 591.3 |
| + 200,000 unique queries | 1467.6 | 804.1 |
| 15 seconds later | 1467.6 | 804.1 |

Current peak RSS (`VmHWM`) was 831.7 MiB. Large-list rows can include an index
rebuild; the final row follows the completed cache workload and a 15-second
settle. This is one bounded run, not a long-term high-water guarantee. The
baseline is the retained pre-optimization binary, built with Go 1.24.13; it
has no embedded source revision. Binary hashes and build metadata identify it.
Current code was built with Go 1.27.1 and updated dependencies. Accordingly,
the numbers verify a lower footprint for these delivered builds under this
scenario, but do not isolate the causal contribution of one code change.

A separate current-only run forced GC after every sampled row to obtain heap
profiles. It settled at 710.8 MiB RSS and **214.8 MiB live Go heap** with lists
and caches. After lists plus shared ranges, live Go heap was 87.1 MiB. Live Go
heap excludes native allocations, unused heap arenas and mappings; it is not
a RAM requirement. The old binary lacks the profiling endpoint, so forced-GC
heap data is unavailable for it. Its failed profiling attempt is retained.

See [raw footprint evidence](../benchmarks/evidence/2026-09-26/README.md)
and [Raspberry Pi sizing and architecture caveats](operations.md#resource-use).
Neither these measurements nor an arm64 dependency library prove Pi runtime
support. The vendored SQLite scanner extension is amd64, and `investigate.go`
uses a hardcoded `v1.1.3/linux_amd64` extraction path. ARM-compatible extension
packaging is required for full Investigation support. A real arm64 build and
hardware soak test remain unverified.

## Historical measurements

The older measurements below are retained as historical evidence. Their product
feature assertions and architecture explanations were not verified by these
benchmarks and must not be treated as current product comparisons. They use
different code, machines, compilers and harnesses. In particular, old Pi-hole
rows with a zero block rate do not support a fair product ranking. Historical
claims about winning every metric do not describe the release comparison above.


Measured on AMD Ryzen 9 5950X (32 threads), Linux 6.8. All times are per-operation. These numbers are our baseline for [DD-006](decisions/DD-006-speed-is-a-first-class-design-constraint.md).

## Primitives: sysclock vs RNG

Your HFT friend was right. `time.Now()` is 5x slower than `math/rand.Float64()`.

| Operation | Latency | Allocs | Notes |
|---|---|---|---|
| `rand.Float64()` | **7.6ns** | 0 | Go's global rand, not crypto/rand |
| `rand.Intn(1M)` | **9.8ns** | 0 | |
| `time.Now()` | **39.8ns** | 0 | vDSO `clock_gettime(CLOCK_MONOTONIC)` — no actual syscall but still 5x RNG |
| `time.Now().UnixNano()` | **39.4ns** | 0 | Accessing the value adds nothing |
| `time.Now().After(deadline)` | **40.0ns** | 0 | The comparison is free, the clock read isn't |
| `time.Now().UnixNano() % 1M / 1M` | **40.4ns** | 0 | The old broken "RNG" — same cost as time.Now() but also not random |

**Where time.Now() lived on our hot path (now replaced by `clock.Now()`):**

- `cache.get()`: one call per lookup for TTL check. Was 40ns, now ~5ns (atomic.Value load)
- `cache.set()`: one call to compute expiry. Was 40ns, now ~5ns
- `handleDNSRequest()`: one call for request timing. Was 40ns, now ~5ns
- `resolveLoadBalanced()`/`resolveParallel()`: one call for upstream latency. Was 40ns, now ~5ns

**Fix implemented (DD-011):** `fastClock` struct with a background goroutine updating a shared `atomic.Value` every 500μs. Hot-path code calls `clock.Now()` instead of `time.Now()`. Saves ~35ns per call, ~140ns total per DNS query (4 calls on the hot path). The 500μs resolution is irrelevant for TTL checks (minutes/hours) and latency logging (millisecond precision).

## Blocklist Lookup: Hagezi Ultimate (267,802 domains)

Benchmarked against the real [Hagezi Ultimate](https://github.com/hagezi/dns-blocklists) list — 267,802 domains in adblock `||domain^` format, zero wildcards.

| Scenario | Latency | Allocs | Notes |
|---|---|---|---|
| Cache hit (warm) | **74ns** | 1 | sync.Map Load fast-path |
| Direct match (hit) | **93ns** | 1 | O(1) map lookup, scales to millions |
| Direct miss (legit domain) | **74ns** | 1 | Same path — map miss is as fast as hit |
| Mixed traffic (90% legit / 10% blocked) | **85ns** | 1 | Realistic workload |
| Parallel (32 goroutines) | **5.8ns** | 1 | sync.Map shines under contention |
| Unknown client (no blocklist) | **64ns** | 1 | Fast exit, no map loaded |

### The wildcard scan problem (fixed — DD-010)

**Before (linear scan of all domains):**

| Scenario | Latency | Notes |
|---|---|---|
| Cold miss (no cache, no match) | **4.15ms** | Linear scan of 267k entries |
| Cold subdomain walk (deep nesting) | **4.29ms** | Same root cause |

**After (pre-separated at load time):**

| Scenario | Latency | Improvement | Notes |
|---|---|---|---|
| Cold miss (no wildcards, 100k exact) | **~125ns** | **33,000x** | No wildcard structures to check |
| Cold miss (no wildcards, 500k exact) | **~77ns** | **54,000x** | Faster due to cache line effects |
| Wildcard scan (10 `*.suffix` wildcards) | **~870ns** | **4,770x** | O(labels) suffix walk, not O(n) |
| Wildcard scan (100 `*.suffix` wildcards) | **~870ns** | **4,770x** | Constant — same suffix walk |
| Wildcard scan (1000 `*.suffix` wildcards) | **~870ns** | **4,770x** | Constant — same suffix walk |

**Root cause was**: `isBlockedForClient` fell through to a linear scan of ALL domains checking `matchWildcard()`. Even with zero wildcards, it iterated 267,802 entries calling `strings.Contains(pattern, "*")` on each. At ~15ns per call: `267802 * 15ns ≈ 4ms`.

**Fix (DD-010)**: `classifyDomain()` separates entries at load time into three buckets: `exactDomains` (map), `wildcardSuffixes` (map of suffixes for `*.suffix` patterns), and `complexWildcards` (slice for rare patterns like `ad*.foo*.com`). The wildcard suffix walk is O(label_count) = O(2-5), not O(n). Complex wildcards are typically 0 entries — even with 1000, it's a tiny slice scan.

The cost that remains in the "wildcard scan" benchmark (~870ns) is dominated by `sync.Map` creation in the benchmark loop (clearing blockCache each iteration) and the subdomain walk `strings.Split` + `strings.Join` allocations, not the wildcard check itself.

## Parsing

| Operation | Latency | Allocs |
|---|---|---|
| `parseDomain("\|\|doubleclick.net^")` | **40ns** | 0 |
| `parseUpstreamAddress("tls://dns.quad9.net:853")` | **58ns** | 0 |
| `getCacheKey("www.example.com", TypeA)` | **35ns** | 0 |

All zero-alloc. No optimization needed.

## DNS Cache

| Operation | Latency | Allocs | Notes |
|---|---|---|---|
| Cache hit | **463ns** | 5 | Dominated by `msg.Copy()` (deep-copies the DNS message) |
| Cache miss | **52ns** | 0 | sync.Map Load miss |
| Cache set | **774ns** | 11 | `msg.Copy()` + sync.Map Store |
| Parallel get (32 goroutines, 1k entries) | **77ns** | 6 | Excellent scaling |

The `msg.Copy()` in cache hits is the biggest cost on the hot path. This is intentional — returning a copy prevents mutations from corrupting the cached entry. If this ever matters, we could explore copy-on-write or immutable message wrappers, but 463ns per cache hit is still sub-microsecond.

## Upstream Selection

| Operation | Latency | Allocs |
|---|---|---|
| `calculateWeight` | **140ns** | 1 |
| `weightedRandomSelect` (5 upstreams) | **18ns** | 0 |
| `matchWildcard("*.doubleclick.net", ...)` | **87ns** | 1 |
| `matchWildcard` (no wildcard in pattern) | **5ns** | 0 |

### Upstream exchange hardening (2026-09-25, DD-033)

One `queryUpstream` round trip against in-process stubs on 127.0.0.1 (5950X under heavy shared load, load average ~138, so ns/op is indicative and allocs/op is the reliable column; allocs include the stub server's own). Before is `b698faa`; DoH/DoT before/after use the same scratch benchmark (IP upstream, test CA through `SSL_CERT_FILE`, `-benchtime=300x -count=6`, medians).

| Path | Before | After | Why |
|---|---|---|---|
| DoH query | ~6.0 ms, 1,182 allocs | ~0.49 ms, 137 allocs | one HTTP/2 client per upstream instead of a Transport + TLS handshake per query |
| DoT query | ~4.9 ms, 848 allocs | ~0.17 ms, 40 allocs | pooled connection instead of TCP + TLS handshake per query |
| `ResolveQuery_CacheMissLocalUDP` | ~156 µs, 65 allocs | ~130 µs, 72 allocs | fresh upstream message per exchange (+Msg, question, OPT, additional); CSPRNG ID made allocation-free; per-miss upstream filtering removed |
| `ParseUpstreamAddress` | ~158 ns, 24 B, 0 allocs | ~42 ns, 0 B, 0 allocs | `strings.Cut` instead of `SplitN` |

`BenchmarkUpstreamExchange/{UDP,TCP,DoT,DoH}` now tracks each transport in the suite (after: UDP ~130 µs/63 allocs, TCP ~55 µs/33, DoT ~90 µs/39, DoH ~310 µs/137).

## Policy Engine (Phase 2)

Three-tier cascading policy evaluation: Range (CIDR match) → Group → IP. Narrowest explicit decision wins. Allow beats block within same tier. Benchmarked with realistic multi-tier setup (2 ranges, 2 groups, direct client assignments, ~500 domains per tier).

| Operation | Latency | Allocs | Notes |
|---|---|---|---|
| `evaluatePolicy` cached hit | **169ns** | 1 | LRU Load (mutex + linked list promote) |
| `evaluatePolicy` cold miss | **1.8μs** | 23 | Full 3-tier walk with source-iteration across shared domain maps |
| `matchRange` (10 CIDRs) | **29ns** | 0 | Sorted by prefix length, first match wins |
| `policyCache` Load (LRU) | **208ns** | 1 | 16-shard LRU, FNV-1a hash, mutex per shard |
| `policyCache` Store (LRU) | **616ns** | 4 | Includes byte estimation + potential eviction |

**Key observations:**

- Cached hit (169ns) is ~43ns slower than the old `sync.Map` (126ns) due to mutex acquisition + linked list promotion on every Load. Still sub-microsecond and worth the trade-off: the LRU is bounded at 800MB vs the old `sync.Map` which grew without limit (130k+ one-shot entries under the full-policy benchmark).
- Cold miss (1.8μs) is higher than before (1.3μs) because source-iteration checks 1-3 lists per entity instead of a single merged map. The extra allocs (23 vs 10) come from `strings.Split`/`strings.Join` during subdomain walks across multiple sources. In practice, the cache absorbs this — repeat lookups hit the LRU.
- `matchRange` at 29ns/op is zero-alloc. Walking 10 sorted CIDRs with `net.IPNet.Contains` is negligible. Even 100 ranges would be ~290ns.
- The LRU cache replaces the old unbounded `sync.Map`. Under the full-policy benchmark (497k blocked domains × uniform random sampling), the `sync.Map` grew to ~130k entries that were never read again. The LRU evicts cold entries, keeping memory bounded at 800MB.

### Policy snapshot over one list index (2026-09-25, DD-035)

Before is `oss` at `9d1448a` (per-tier stores and `classifiedDomains` maps), after is the policy snapshot. 5950X shared with other work (load average 30-100 during the runs), so ns/op is indicative; allocations and memory are the reliable columns. Microbenchmarks ran interleaved (one before run, one after run, six times, medians); `BenchmarkEvaluatePolicy_Uncached` and `BenchmarkReloadPolicyRealLists` were added for this change and ran against both trees.

| Benchmark | Before | After |
|---|---|---|
| `EvaluatePolicy_Uncached` (cache miss: 6 lists + allowlist, 2 ranges, 2 groups, direct + custom rules) | 27.7 µs, 2467 B, 57 allocs | 1.19 µs, 640 B, 3 allocs |
| `EvaluatePolicy_ColdMiss` (dominated by the cache `Clear` it runs per iteration) | 2.7 µs, 1680 B, 25 allocs | 1.6 µs, 1552 B, 20 allocs |
| `EvaluatePolicy_CachedHit` | 105 ns, 0 allocs | 115 ns, 0 allocs (same code, noise) |
| `PolicyMatch` (probe + six-list match) | old matcher 1012 ns, 2 allocs | index 229 ns, 0 allocs |
| `FullLookupDecision` | 373 ns, 1 alloc | 422 ns, 1 alloc (noise) |
| `HandleDNSRequest_BlockedCachedPolicy` | 1044 ns, 5 allocs | 781 ns, 5 allocs |
| `ReloadPolicyRealLists` (full rebuild, five real lists, 3.12M rows) | 7.95 s CPU, 777 MB, 12.5M allocs | 3.62 s CPU, 488 MB, 6.25M allocs |

The old uncached evaluation read every group name from SQLite. The index is 80.7 MiB for 3.12M rules (2.62M distinct) against ~101 MiB of estimated map payload before, which did not count map overhead.

`benchmarks/footprint.sh` (RSS, and live heap from a heap profile taken after a forced GC). Rows taken right after a large list is added can catch its rebuild in progress; the last three rows are settled.

| State | Before RSS / live heap | After RSS / live heap |
|---|---|---|
| empty | 132 / 8.6 MiB | 134 / 6.6 MiB |
| + stevenblack (76k) | 157 / 16.3 | 155 / 9.9 |
| + hagezi-pro (228k) | 215 / 37.1 | 205 / 16.8 |
| + oisd-big (244k) | 294 / 73.2 | 253 / 24.6 |
| + hagezi-ultimate (284k) | 384 / 110.4 | 327 / 24.7 |
| + hagezi-tif (2.29M; 3.12M total) | 569 / 376.7 | 644 / 165.3 (rebuild in progress) |
| + 20 ranges sharing 2 lists | 729 / 279.7 | 623 / 90.8 |
| + 200k unique names cached | 1215 / 470.2 | 828 / 214.8 |
| ... 15 s later | 1088 / 445.5 | 616 / 214.8 |

With the old 800 MiB policy cache budget (`POLICY_CACHE_SIZE_MB=800`) the after binary ends at 765 MiB RSS against 783 MiB with the new 64 MiB default in the same run: this benchmark's 200k decisions from one client are small. A multi-tier decision measured ~560 B of heap (661 B accounted), so 64 MiB holds about 100k.

`benchmarks/loadtime.sh` (each list assigned to a client and to `0.0.0.0/0` while it loads; ms until a domain only that list holds is blocked for the client, and whether the range blocks it):

| List | Before | After |
|---|---|---|
| stevenblack | 1800 ms, range **allow** (B1) | 915 ms, range block |
| hagezi-pro | 5403 ms, range block | 2859 ms, range block |
| oisd-big | 6988 ms, range block | 3170 ms, range block |
| hagezi-ultimate | 8883 ms, range block | 4053 ms, range block |
| hagezi-tif | 54635 ms, range block | 21700 ms, range block |
| all five | 78.0 s | 32.9 s |

Before, the range only picked up lists 2-5 because the next list's range assignment happened to rebuild ranges, and admin requests waited behind the refresh on the single write connection. After, refreshes insert through one prepared statement and the rebuild reads through `readDB`.

## End-to-End Decision Path

| Scenario | Latency | Notes |
|---|---|---|
| Full lookup decision (cache check → rewrite check → blocklist check) | **833ns** | Mix of cache hits, misses, rewrites, and blocks |

This is the in-process decision overhead before any network round-trip to an upstream. Sub-microsecond.

## Hot Path Profiling (pprof)

Profiled with `go test -bench -cpuprofile -memprofile` on the hot-path benchmarks (cache get, blocklist miss, full lookup decision). Key findings:

**Allocation sources (by object count):**

| Source | % of allocs | What | Actionable? |
|---|---|---|---|
| `isBlockedForClient` | 40% | `strings.ToLower(domain)` + `clientIP + ":" + domain` cache key | Mitigated by blockCache — only on first lookup per domain. Could eliminate with pre-lowercased domains + byte buffer key, but marginal gain |
| `dns.Msg.Copy` | 43% | Deep copy on cache hit (intentional — prevents mutation of cached entries) | Known trade-off. Copy-on-write would help but adds complexity for sub-μs cost |
| `dns.Msg.SetQuestion` / `dns.Fqdn` | 6% | Benchmark harness creating test messages, not production | No |
| `getCacheKey` | 5% | `strings.ToLower` + string concat | Same pattern as blocklist — first-lookup allocation only |

**CPU hotspots:**

| Source | % CPU | Notes |
|---|---|---|
| GC (`gcBgMarkWorker`, `scanobject`) | ~40% | Driven by benchmark allocation churn, not production steady-state |
| `dns.Msg.Copy` / `dns.Msg.CopyTo` | ~11% | The dominant cost in cache hits |
| `crypto/rand.Read` (from `dns.id()`) | ~9% | miekg/dns generates crypto-random message IDs — syscall overhead. Not controllable |
| `strings.ToLower` | ~5% | In `getCacheKey` and `isBlockedForClient` |
| `sync.Map.Load` | ~7% | Expected for concurrent map lookups |

**Verdict:** No low-hanging fruit remaining on the hot path. The two biggest costs (`dns.Msg.Copy` at ~316ns and `isBlockedForClient` at ~84ns) are already documented and optimized. The allocation pattern in `isBlockedForClient` is mitigated by the block cache (repeat lookups allocate nothing). The GC pressure seen in benchmarks (~40% CPU) is benchmark-specific — production workloads with steady-state caching will see much lower GC.

## End-to-End: svart-dns vs Pi-hole vs AdGuard Home

Real DNS packets through the full network stack. All three servers run in Docker on the same machine, upstream Quad9 (9.9.9.9). Servers are benchmarked **sequentially** (not concurrently) so each gets full machine resources. CPU/memory captured via `docker stats`.

The benchmark now runs svart-dns **twice**: once with IP-only policy (1 entity per query — single blocklist assigned directly to the bench client), and once with full three-tier policy cascade (9 entities per query). This measures the real cost of the policy engine under worst-case-realistic load.

```bash
# Historical command (removed): make bench-e2e
# Current isolated comparison: see benchmarks/README.md
```

### Full Policy Stress Test Setup

The "full policy" run creates a worst-case-realistic scenario for the bench client (Docker bridge IP):

**10 published blocklists:** Hagezi Ultimate/Pro++/Pro/Normal/Light, Steven Black, Dan Pollock, Firebog BOG/BG/Green

**5 ranges (bench client matches 3):**

| Range | CIDR | Lists | Bench client? |
|---|---|---|---|
| Everything | 128.0.0.0/1 | Hagezi Light, Dan Pollock | YES (broadest) |
| Untrusted Net | 172.0.0.0/8 | Hagezi Pro++, Steven Black, Firebog BOG | YES (medium) |
| Docker Bridge | 172.16.0.0/12 | Hagezi Ultimate, Firebog BG, Firebog Green | YES (narrowest) |
| IOT Jail | 10.42.3.0/24 | Hagezi Ultimate, Firebog BOG, Firebog BG | No |
| Home LAN | 10.42.1.0/24 | Hagezi Normal, Steven Black | No |

**5 groups (bench client in all 5):**

| Group | Lists | Custom Allows | Custom Blocks |
|---|---|---|---|
| Family | Hagezi Pro, Firebog Green | youtube.com, netflix.com, spotify.com, reddit.com, amazon.com, wikipedia.org | — |
| Adults | Hagezi Normal, Dan Pollock | twitter.com, facebook.com, instagram.com, linkedin.com, pinterest.com | — |
| Parental Controls | Hagezi Pro++, Firebog BG | — | tiktok.com, snapchat.com, discord.com, twitch.tv, omegle.com |
| Lockdown | Hagezi Ultimate, Firebog BOG, Steven Black | — | youtube.com, netflix.com, reddit.com, twitter.com, instagram.com, facebook.com, twitch.tv, tiktok.com |
| Homelab Infra | Hagezi Light | grafana.com, prometheus.io, github.com, docker.com | — |

**IP tier (bench client):** Hagezi Ultimate + custom allows (google.com, github.com, stackoverflow.com) + custom block (twitch.tv)

**Per-query cost:** 9 entity evaluations — 3 ranges × (allow + block) + 5 groups × (allow + block) + 1 IP × (allow + block). This is the extreme edge of plausibly realistic: the admin who's in every group testing their own system.

**30 total clients** (1 bench + 29 filler) across varied group/range memberships to seed realistic data loading.

### Benchmark History

#### 2026-03-06 08:25 PST — 1M queries, 50 workers, 20% blocked (post Phase 10: named policies, multi-user auth, replication UI, TLS cert watcher)

Hardware: AMD Ryzen 9 5950X (32 threads), 128 GB RAM, Linux 6.8

| Metric | svart-dns (IP-only) | svart-dns (full policy) | Pi-hole | AdGuard Home |
|---|---|---|---|---|
| **p50** | **544μs** | **395μs** | 5.78ms | **460μs** |
| **p95** | **1.24ms** | **2.42ms** | 7.68ms | **1.89ms** |
| **p99** | **1.75ms** | **4.54ms** | 9.52ms | **3.59ms** |
| Min | 64μs | 59μs | 152μs | 78μs |
| Max | 23.62ms | 33.77ms | 1970.24ms | 1513.08ms |
| Mean | 587μs | 667μs | 5.93ms | 723μs |
| **Throughput** | **85,190 qps** | **74,968 qps** | **8,437 qps** | **69,155 qps** |
| Block rate | 21.2% | 24.4% | 0.0%* | 21.2% |
| Memory (idle) | 834 MiB | — | 8 MiB | 96 MiB |
| Memory (post-bench) | 5.58 GiB | 9.56 GiB | 16 MiB | 296 MiB |
| CPU (idle) | 3.50% | — | 2.33% | 1.97% |
| CPU (post-bench) | 100.07% | 100.40% | 2.33% | 1.41% |

**Changes since last run:** Named policies (DD-020: reusable templates with blocklist/allowlist selections + custom rules, assignable to ranges/groups/clients), multi-user auth (DB-backed with bcrypt, session cookies, API tokens, three middleware tiers), replication UI improvements, TLS certificate hot-reload via cert watcher, system resource monitoring dashboard, node name context, blocklist checkpoint browser, auto-refresh scheduling, Loki direct push logging. **Benchmark script fix:** added authentication to all API calls — the previous run's 0% block rate for svart was caused by 401 Unauthorized on client-to-blocklist assignment (auth was added but benchmark scripts weren't updated).

**Key takeaways:**

- **Blocking confirmed working** at 21.2% (IP-only) and 24.4% (full policy). Previous run's 0% svart block rate was a benchmark auth bug, not a DNS path regression.
- **IP-only: 85k qps** — 23% faster than AdGuard (69k). Down ~4% from previous run (88.5k) — within run-to-run noise given Docker networking variance.
- **Full policy: 75k qps** — 8.4% faster than AdGuard despite 9 entity evaluations per query vs AdGuard's flat filter list. Down ~3% from previous (76.9k) — noise.
- **p50 latency excellent**: IP-only 544μs, full policy 395μs (cache warming from run 1 benefits run 2). Both competitive with AdGuard's 460μs.
- **No performance regression from auth, named policies, TLS watcher, or Loki logging.** All new features are off the DNS hot path — auth only gates HTTP API calls, cert watcher runs on a 60s background ticker, Loki handler only wraps slog (disabled in benchmark since no LOKI_URL set).
- **Memory higher** (idle 834 MiB vs 519 MiB, post-bench 5.58/9.56 GiB vs 1.90/4.11 GiB). The increase is from additional in-memory structures for named policies, admin users, and the policy merge layer. 10 blocklists × shared domain maps remain the dominant cost.
- **CPU saturated at 100%** during benchmark (was 4-6% before). This is a docker stats sampling artifact — captured immediately post-benchmark while the logwriter is still flushing 1M buffered entries.
- **Pi-hole still can't load blocklists** via scripted gravity in Docker (0% block rate, known issue). AdGuard blocking works correctly at 21.2%.

#### 2026-03-03 21:21 PST — 1M queries, 50 workers, 20% blocked (shared domain maps + LRU policy cache)

Hardware: AMD Ryzen 9 5950X (32 threads), 128 GB RAM, Linux 6.8

| Metric | svart-dns (IP-only) | svart-dns (full policy) | Pi-hole | AdGuard Home |
|---|---|---|---|---|
| **p50** | **507μs** | **314μs** | 5.85ms | **484μs** |
| **p95** | **1.03ms** | **2.76ms** | 7.81ms | **1.72ms** |
| **p99** | **1.40ms** | **4.76ms** | 9.39ms | **2.58ms** |
| Min | 69μs | 60μs | 154μs | 83μs |
| Max | 26.23ms | 14.86ms | 1183.71ms | 366.98ms |
| Mean | 565μs | 650μs | 5.99ms | 639μs |
| **Throughput** | **88,514 qps** | **76,898 qps** | **8,351 qps** | **78,287 qps** |
| Block rate | 21.2% | 24.3% | 0.0%* | 21.2% |
| Memory (idle) | 519 MiB | — | 9 MiB | 94 MiB |
| Memory (post-bench) | 1.90 GiB | 4.11 GiB | 16 MiB | 292 MiB |
| CPU (idle) | 2.39% | — | 0.18% | 0.01% |
| CPU (post-bench) | 4.03% | 6.00% | 0.14% | 1.44% |

**Changes since last run:** Shared domain maps (per-blocklist `classifiedDomains` structs shared by pointer across all entities instead of per-entity map copies), LRU policy cache (16-shard byte-budgeted LRU at 800MB cap replacing unbounded `sync.Map`), removed dead `blockCache`/`allowCache` sync.Maps.

**Key takeaways:**

- **Full policy memory -18%** (5.03 GiB → 4.11 GiB). Shared domain maps eliminated per-entity map duplication. The remaining cost is the blocklist domain data itself (10 lists, ~500k domains each with pre-classified buckets) plus the LRU cache.
- **Idle memory -23%** (677 MiB → 519 MiB). Shared maps mean the baseline footprint of loaded blocklists is smaller.
- **CPU halved under full policy** (14.12% → 6.00%). The LRU doesn't grow unboundedly like `sync.Map` did (no GC pressure from 130k+ one-shot cached entries), and shared maps reduce allocation churn during cold evaluations.
- **Throughput within noise** (79.1k → 76.9k qps, -3%). The source-iteration approach (checking 1-3 lists per entity) costs ~60-90ns extra per miss vs the old merged map, but this is absorbed by the cache.
- **p50 improved 11%** (351μs → 314μs) under full policy. LRU promotion is faster than `sync.Map` type assertion for cached hits.
- **Tail latency slightly higher** (p99: 4.41ms → 4.76ms, +8%). LRU eviction under byte pressure adds occasional overhead on Store. Within acceptable bounds.
- **LRU microbenchmarks:** Load 208ns, Store 616ns. Comparable to the old `sync.Map` Load (179ns) with type-safe returns and bounded memory.

#### 2026-03-03 20:22 PST — 1M queries, 50 workers, 20% blocked (IP-only vs full policy)

Hardware: AMD Ryzen 9 5950X (32 threads), 128 GB RAM, Linux 6.8

First run with the IP-only vs full-policy comparison. The bench client (172.20.0.1) evaluates 1 entity in IP-only mode and 9 entities (3 ranges + 5 groups + 1 IP) in full-policy mode.

| Metric | svart-dns (IP-only) | svart-dns (full policy) | Pi-hole | AdGuard Home |
|---|---|---|---|---|
| **p50** | **499μs** | **351μs** | 5.83ms | **489μs** |
| **p95** | **1.01ms** | **2.47ms** | 7.06ms | **1.73ms** |
| **p99** | **1.39ms** | **4.41ms** | 7.92ms | **2.63ms** |
| Min | 68μs | 64μs | 154μs | 91μs |
| Max | 22.44ms | 15.04ms | 1032.62ms | 181.73ms |
| Mean | 553μs | 632μs | 5.92ms | 628μs |
| **Throughput** | **90,335 qps** | **79,107 qps** | **8,445 qps** | **79,562 qps** |
| Block rate | 21.2% | 24.4% | 0.0%* | 21.2% |
| Memory (idle) | 677 MiB | — | 8 MiB | 94 MiB |
| Memory (post-bench) | 1.95 GiB | 5.03 GiB | 16 MiB | 292 MiB |
| CPU (idle) | 3.15% | — | 0.12% | 0.01% |
| CPU (post-bench) | 4.37% | 14.12% | 2.41% | 0.01% |

*Pi-hole gravity didn't load the Hagezi list properly (known issue with scripted `pihole -g`).

**Full policy setup:** 10 published blocklists (Hagezi ×5, Steven Black, Dan Pollock, Firebog ×3), 5 ranges (bench client matches 3 overlapping CIDRs), 5 groups (bench client in all 5 with conflicting allow/block custom rules), 30 total clients.

**Key takeaways:**

- **Full policy cost: -12% throughput, +3.2x p99.** IP-only: 90.3k qps / 1.39ms p99. Full policy (9 entities): 79.1k qps / 4.41ms p99. The 12% throughput drop from evaluating 9× more entities is smaller than expected — the policy cache (sync.Map) absorbs repeat lookups after the first cold evaluation.
- **p50 improved under full policy** (499μs → 351μs). Counterintuitive, but the IP-only run warmed the DNS cache, so the full-policy run benefits from more cache hits. The cost shows up in the tail (p95/p99), not the median.
- **Still matches AdGuard Home at 9× the evaluation complexity.** Full policy throughput (79.1k qps) is within 0.6% of AdGuard (79.6k qps). Mean latency nearly identical (632μs vs 628μs). Historical, unverified claim retained for provenance: “AdGuard doesn't have tiers, groups, or cascading policy at all — it evaluates a flat filter list per query.” This benchmark does not establish that product-feature claim.
- **Block rate increased from 21.2% to 24.4%.** Expected: 10 stacked blocklists across 3 ranges + 5 groups + IP tier catch more domains than the single Hagezi Ultimate in IP-only mode.
- **Memory 2.6× higher under full policy** (1.95 GiB → 5.03 GiB). Ten blocklists × per-client domain maps × 30 clients × policy cache entries. This is the explicit RAM-for-speed trade-off from DD-006.
- **CPU 3.2× higher under full policy** (4.37% → 14.12%). More entity evaluations per query means more map lookups and allow/block checks. Still well under saturation on a 32-thread machine.
- **Zero failures or timeouts** across all 4M queries (4 runs × 1M).

#### 2026-03-03 19:55 PST — 1M queries, 50 workers, 20% blocked (attribution + audit trail + archiver)

Hardware: AMD Ryzen 9 5950X (32 threads), 128 GB RAM, Linux 6.8

| Metric | svart-dns | Pi-hole | AdGuard Home |
|---|---|---|---|
| **p50** | **507μs** | 5.87ms | **487μs** |
| **p95** | **1.05ms** | 6.90ms | **1.72ms** |
| **p99** | **1.43ms** | 7.88ms | **2.66ms** |
| Min | 66μs | 145μs | 85μs |
| Max | 20.10ms | 1175.28ms | 182.13ms |
| Mean | 565μs | 6.00ms | 627μs |
| **Throughput** | **88,439 qps** | **8,330 qps** | **79,724 qps** |
| Block rate | 21.2% | 0.0%* | 21.2% |
| Memory (idle) | 679 MiB | 8 MiB | 94 MiB |
| Memory (post-bench) | 1.9 GiB | 16 MiB | 291 MiB |
| CPU (idle) | 4.18% | 0.10% | 0.01% |
| CPU (post-bench) | 8.55% | 0.15% | 2.18% |

**Changes since last run:** Published list attribution (attribution maps, name caches, sorted list iteration), restructured PolicyResult (named tier fields, PublishedHit/CustomHit types), decision audit trail (3 new query_logs columns: block_source, block_list_id, block_list_name logged for every query), Parquet archival system, log retention extended from 7 to 1095 days, go.mod bumped to 1.24.9 (parquet-go dependency).

**Key takeaways:**

- **Zero performance regression** from attribution + audit trail changes. Throughput within noise (88.4k vs 89.5k qps), p99 identical (1.43ms vs 1.41ms)
- The 3 extra columns in the log writer (12 fields vs 9) go through the same async batched channel — DNS response is sent before the entry hits the channel
- Attribution map lookup (~5ns on match, 0ns on miss) is invisible at the e2e level
- Memory post-bench slightly higher (1.9 GiB vs 1.3 GiB) — parquet-go dependency adds some baseline overhead
- Micro benchmarks confirm: cached hit 124ns (was 126ns), cold miss 874ns (was 1.3μs — improved due to struct simplification)
- Still beats AdGuard Home on throughput (+11%), p95 (-39%), p99 (-46%)

#### 2026-03-03 16:14 PST — 1M queries, 50 workers, 20% blocked (DD-012: batch log writer + WAL)

Hardware: AMD Ryzen 9 5950X (32 threads), 128 GB RAM, Linux 6.8

| Metric | svart-dns | Pi-hole | AdGuard Home |
|---|---|---|---|
| **p50** | **497μs** | 5.85ms | **481μs** |
| **p95** | **1.04ms** | 6.69ms | **1.71ms** |
| **p99** | **1.41ms** | 7.96ms | **2.57ms** |
| Min | 68μs | 149μs | 86μs |
| Max | 24.37ms | 1477.05ms | 161.37ms |
| Mean | 559μs | 6.04ms | 617μs |
| **Throughput** | **89,463 qps** | **8,283 qps** | **81,071 qps** |
| Block rate | 21.2% | 0.0%* | 21.2% |
| Memory (idle) | 602 MiB | 34 MiB | 117 MiB |
| Memory (post-bench) | 1.3 GiB | 41 MiB | 320 MiB |
| CPU (idle) | 4.27% | 0.14% | 0.01% |
| CPU (post-bench) | 3.93% | 2.53% | 1.46% |

*Pi-hole's gravity update didn't properly load the Hagezi list in this run — the benchmark script's Pi-hole integration needs investigation. svart-dns and AdGuard both show the expected ~21% block rate.

**Key takeaways:**

- svart-dns now beats AdGuard Home on every metric: +10% throughput (89k vs 81k qps), faster p50/p95/p99
- Historical claim, invalid as a fair product comparison because gravity failed: “Pi-hole is 11x slower (dnsmasq/FTL architecture, single-threaded DNS).” The failed setup cannot establish the claimed architectural cause.
- DD-012 (batch log writer + WAL pragmas) had a massive impact: throughput +18% (75k → 89k), p99 -54% (3.09ms → 1.41ms), memory -76% (5.4 GiB → 1.3 GiB)
- The p99 improvement (3.09ms → 1.41ms) confirms the old goroutine-per-query pattern was causing write contention that bled into DNS handler latency — even though inserts were "async," 950k goroutines fighting for one SQLite connection still created GC pressure and scheduling overhead
- Idle memory is 602 MiB (up from 183 MiB) due to the 10M-entry channel pre-allocation (~2GB capacity). Post-bench is 1.3 GiB — the channel was never close to full
- Zero failed queries or timeouts for svart-dns and AdGuard across 1M requests

#### 2026-03-03 15:38 PST — 1M queries, 50 workers, 20% blocked (baseline, pre-DD-012)

Hardware: AMD Ryzen 9 5950X (32 threads), 128 GB RAM, Linux 6.8

| Metric | svart-dns | Pi-hole | AdGuard Home |
|---|---|---|---|
| **p50** | **566μs** | 5.85ms | **491μs** |
| **p95** | **1.48ms** | 7.28ms | **1.71ms** |
| **p99** | **3.09ms** | 8.46ms | **2.58ms** |
| Min | 62μs | 135μs | 84μs |
| Max | 27.86ms | 565.57ms | 170.01ms |
| Mean | 660μs | 6.04ms | 637μs |
| **Throughput** | **75,715 qps** | **8,272 qps** | **78,499 qps** |
| Block rate | 21.2% | 0.0%* | 21.2% |
| Memory (idle) | 183 MiB | 8 MiB | 95 MiB |
| Memory (post-bench) | 5.4 GiB | 16 MiB | 296 MiB |
| CPU (idle) | 1.92% | 0.15% | 0.01% |
| CPU (post-bench) | 19.82% | 0.16% | 1.46% |

*Pi-hole's gravity update didn't properly load the Hagezi list in this run — the benchmark script's Pi-hole integration needs investigation. svart-dns and AdGuard both show the expected ~21% block rate.

**Key takeaways (superseded by DD-012 results above):**

- svart-dns was within 4% of AdGuard Home on throughput and latency
- svart-dns used 5.4 GiB post-bench due to goroutine-per-query logging spawning ~950k goroutines waiting for single SQLite connection
- p99 was 3.09ms vs AdGuard's 2.58ms — caused by SQLite write contention / GC pressure from goroutine pileup

### Benchmark suite details

Custom Go-based DNS load tester in `benchmarks/` — real UDP DNS packets, p50/p95/p99 latency, throughput, block rates, and resource tracking. Servers are tested sequentially (one at a time) so results aren't polluted by cross-server contention.

```bash
# These historical runs used the removed Compose harness.
# For new runs, use benchmarks/compare.sh as documented in benchmarks/README.md.
```

**What it does:**

1. Starts svart-dns, Pi-hole, AdGuard Home in Docker containers
2. Loads Hagezi Ultimate (~497k domains) into all three + assigns to bench client
3. Waits for blocklists to actually load (polls until a known blocked domain returns NXDOMAIN/0.0.0.0)
4. Captures idle CPU/memory via `docker stats`
5. **Run 1**: Benchmarks svart-dns with IP-only policy (1 entity — single blocklist directly assigned)
6. **Policy setup**: Runs `setup-full-policy.sh` to create 5 ranges, 5 groups, 10 lists, 30 clients, custom allow/block rules — bench client evaluates 9 entities per query
7. **Run 2**: Benchmarks svart-dns with full three-tier policy cascade
8. **Runs 3-4**: Benchmarks Pi-hole and AdGuard Home
9. Prints 4-column comparison table (svart IP-only, svart full, Pi-hole, AdGuard) and tears down containers

**Why not dnsperf:** dnsperf doesn't report p50/p95/p99 natively. Our tool does exactly what we need and uses `github.com/miekg/dns` which we already depend on.

See `benchmarks/README.md` for full usage, port mappings, and result interpretation.

## Admin API: Hot/Cold Architecture (2026-03-19)

Measured after deploying DD-024 (hot/cold architecture), DD-026 (doom loop defense), DD-025 (DuckDB investigation engine). Blue had a 19GB database from a DNS doom loop (15.8M queries for `aplus.qwen.ai` in 8.5 hours). Red was at 1.3GB with normal traffic.

### Before (2026-03-19 morning, pre-deploy)

| Metric | Blue (10.42.1.6) | Red (10.42.1.7) |
|--------|-------------------|-------------------|
| DB file size | 19 GB | 1.3 GB |
| query_logs rows | 16.1M | 717K |
| `/health` | **TIMEOUT** | 3ms |
| `/api/stats/timeseries?window=24h` | **TIMEOUT** | 3.3ms |
| `/api/stats/timeseries?window=7d` | **TIMEOUT** | 3.3ms |
| `/api/stats/top-clients?window=24h` | **TIMEOUT** | 3.3ms |
| `/api/query-logs?limit=50` | **TIMEOUT** | 3.2ms |
| DNS resolution | **DEAD** | Sub-ms (cached) |
| `/metrics` (Prometheus scrape) | **TIMEOUT** | OK |

Blue was completely unresponsive on all endpoints. Container was "Up (unhealthy)". DNS resolution had also stopped.

### After (2026-03-19 evening, post-deploy)

| Metric | Blue | Red |
|--------|------|-----|
| DB file size | 864 MB (-95%) | 1.4 GB |
| `/api/stats/timeseries?window=24h` | **3.0ms** | 3.3ms |
| `/api/stats/timeseries?window=7d` | **2.8ms** | 3.2ms |
| `/api/stats/top-clients?window=24h` | **2.9ms** | 2.9ms |
| `/api/query-logs?limit=50` | **2.9ms** | 3.2ms |
| DNS resolution (cached) | 0ms | 0ms |
| Docker image size | 72.8 MB | 72.8 MB |
| Container status | Healthy | Healthy |

### What changed

| Change | Impact |
|--------|--------|
| `readDB` pool (4 concurrent readers) | Admin API no longer blocked by sync/writes |
| Archive threshold 730 → 8 days (daily) | SQLite stays lean, old data in Parquet |
| VACUUM at midnight | Reclaims disk after archiving |
| Doom loop dedup (25 TPS blocked, 100 allowed) | Future doom loops capped at ~30K rows vs 15.8M |
| SOA negative TTL (3600s) on blocked responses | Clients cache blocked answers for 1 hour |
| DNS response throttle (1s sleep) | Active doom loops limited to ~1 qps |
| DuckDB investigation engine | 3-year historical SQL queries, no SQLite impact |
| Binary size | ~25MB → ~114MB (+DuckDB + embedded sqlite_scanner) |
| Docker image | 72.8 MB (well under 800MB limit) |

### Key takeaway

Blue went from **completely dead** (all endpoints timing out, DNS unresponsive, 19GB database) to **3ms dashboard responses** in a single deploy. The root cause was a Qwen AI app doom-looping 260 queries/sec for 8.5 hours on a blocked domain, combined with `MaxOpenConns(1)` serializing all database access.

Bootstrap configuration safety (2026-09-26, Ryzen 5950X, GOMAXPROCS=4):
`go test -p 4 -run '^$' -bench '^BenchmarkBootstrapCacheHit$' -benchtime=1s -count=3`
measured cache hits before at 52.70/49.86/43.04 ns/op and after at
32.96/31.78/32.77 ns/op, all 0 B/op and 0 allocs/op. Shared-host scheduling makes
these short timings noisy; no speedup is claimed. The controlled 32-caller cold
lookup regression emitted five DNS requests before and exactly one after
coalescing. Three full bootstrap-suite repetitions passed after the change.
