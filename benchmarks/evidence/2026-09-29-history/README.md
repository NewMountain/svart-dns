# Exact 50-million-row live history measurements

These measurements do **not** establish an instant dashboard over 50 million live
SQLite rows. Initial seven-day summary and chart scans took tens of seconds;
immediate repeats used the existing response cache and returned in milliseconds.
All 144 dashboard responses matched an independent exact arithmetic oracle. The
native count and grouped count also returned all 50 million physical rows/events.

The unchanged production baseline is `b67c6f44974220c8c60c8874521d2795adec6663`;
the security-patched candidate is `ab901e3f41d6cf3a52e8bbf5a106f432d3fdef03`.
Both were built with Go 1.26.8, CGO, trimpath and production stripped linker flags.
Candidate executable SHA256:
`ee54d8dec7c7c5d281d21c56941835c0380be444603136bd5a075d16f8d5466e`.
The baseline used its source-matched frontend; candidate frontend source is
unchanged from its source-matched 45047b6 build.

The host was an AMD Ryzen 9 5950X, Linux x86-64, Btrfs. Each isolated network-none
world had 4 CPUs and 8 GiB RAM with no swap; server affinity was cores 2–3,
GOMAXPROCS=4. Other cooperating agents held builds for this window. Unrelated
user jobs were not stopped. Full host CPU/I/O/memory pressure samples and
server/worker `/proc` RSS/I/O samples are retained with the raw evidence.

Each process received a fresh copy of the same 15,310,344,192-byte SQLite fixture:
50 million actual rows spanning 2026-09-21 06:14:04 through2026-09-29 06:14:03 UTC,
10 million blocked rows, 64 clients, 100,000 names, one original per row, and the six
normal indexes. The exact construction formula, indexes and counts are in
[results.json](results.json). Before startup, POSIX_FADV_DONTNEED was requested
only for that owned copy. It is advisory; no claim of a globally cold disk cache
is made. No forced garbage collection or modified query limits were used.

Baseline ran first, then candidate. For each 1h/24h/7d window, each of eight
actual dashboard panel endpoints was requested three consecutive times: one
response-cache miss followed immediately by two cache hits. Headers preserve
actual cache state and SQL compute duration. These are sequential authenticated
HTTP panel measurements, **not** a browser's total page-paint time. Relative
window cutoffs advance with wall time, so candidate windows contain slightly
fewer rows than baseline windows. Each result records exact request time and
matched cutoff; small deltas must not be attributed to code changes.

The oracle computes counts from row-number arithmetic, independently of product
SQL. It requires an exact result at an integer-second cutoff inside the HTTP
request interval. Cached bodies must equal the corresponding validated miss.
Core count/latency formulas passed 1,000 brute-force small-interval checks.
[All 144 responses and oracle verdicts](results.json) and the
[uncached panel table](panel-table.md) are retained here.

| Native live-history operation | Baseline (s) | Candidate (s) |
|---|---:|---:|
| COUNT/SUM/MIN/MAX over all 50M rows |25.139|23.029|
| Blocked/unblocked exact grouping |18.836|13.576|
| Requested 1s deadline, HTTP 504 |3.649|1.007|
| Schema |7.446|8.435|
| Archive status, empty archive |0.210|4.327|

Native timings include complete request work; they are not just SQL-engine
execution timers. Both counts were exactly 50,000,000 rows/events, with 40,000,000
allowed and 10,000,000 blocked. A single ordered pair is not a confidence interval
or a causal speedup estimate. Full archive-backed native measurements and memory
analysis are separate evidence. Complete local raw logs, databases, fixture
sources and resource samples remain under the preserved final-performance
evidence directory; this public subset omits fixture login credentials.

The 100 ms process samples recorded a peak aggregate RSS of 332.7 MiB for the
baseline and 389.9 MiB for the candidate across this complete dashboard/native
sequence. The candidate's main process accounted for the aggregate peak during
dashboard work; its largest separately observed native worker was 120.6 MiB.
The kernel-reported main-process high-water RSS was 332.7 MiB for baseline and
391.9 MiB for candidate, including peaks between sampler ticks.
Four native worker processes were observed (count, grouping, cancellation and
schema). Baseline native SQL executes in the main process. These are sampled
process-resident peaks, excluding filesystem cache and OS memory; they do not
establish a system RAM ceiling. [Resource and I/O counters](resources.json)
retain each process's first/last counters. Do not sum independently occurring
main-process and worker maxima as if they were simultaneous.
