# Local transaction safety: three-million-rule check

Measured source: `86dd6efecfd05e5e22dda4e6d95772c3fc8c259f`, clean working tree. The existing opt-in `TestListPerformanceThreeMillion` harness was reused unchanged; no duplicate benchmark was added. Go `go1.27.1-X:nodwarf5 linux/amd64`, `GOMAXPROCS=4`, build `GOFLAGS=-p=4`, home-filesystem temporary directory. The fixture processes all three million rules, eight ranges, eight groups and 64 clients.

| Operation | Result |
|---|---:|
| Manual HTTP edit 1 |16.581ms /4,703,184 B /2,590 allocations |
| Manual HTTP edit 2 |1.660ms /7,398,064 B /2,455 allocations |
| Manual HTTP edit 3 |2.115ms /10,048,016 B /2,463 allocations |
| Three 1M-query index samples |111.809 /112.052 /112.842ns per query |
| First metadata read during refresh |7.210ms |
| First domain-page read during refresh |125.596ms |
| One-million-rule persistence refresh |7.153s |
| Child process resource peak RSS |679,180KiB |
| Final `/proc` sampled RSS/high-water mark |682,152kB |

All three edits assert exact policy/custom-rule attribution. Each lookup sample asserts 666,667 matches. All 20 admin reads began while the writer was active. The complete test passed in 22.97s; complete stdout/stderr is in `three-million.log`, and resource counters are in `resources.json`. The two RSS reporting mechanisms differ slightly; both raw measurements are retained.

This shows that preparing policy snapshots inside local write transactions retains small-edit behavior on the 3M fixture; it does not establish an overall DNS throughput or memory improvement. The DNS lookup implementation was unchanged. This is one post-change process with three edit/lookup samples, natural GC/scheduler variation, and whole-process allocation counters that include background workers. The refresh measurement includes persistence/history/changelog/local-generation writes, excluding download/parsing and subsequent policy reload. The parent's final controlled overall comparison is separate.

Other cooperating agents paused builds/tests for the measurement; unrelated user processes were untouched. A short read-only provenance-hash collection ran during the measurement. The host is the same shared AMD Ryzen 9 5950X workstation used for the [incremental-index evidence](../2026-09-28-incremental/README.md).

## Provenance and reproduction

- Test binary SHA-256: `8aacf4e43d2052be7a1a22a6aa93721b386e1f09241f482ac201c772fc8a6736`.
- Harness `list_performance_test.go` SHA-256: `869bbd7fbad18fdff62a8c5aa444c6d839a5bc7f5c21cd2a9134ad4d3167a305`.
- Python runner SHA-256: `df1b2ad7835d107488e51d7a8f2ef6ff9e0329474316ae9742d73a6860b317b9`; this is the TMPDIR-portable, exclusive-output runner from parent commit `1b39a41`.

Build embedded frontend assets before compiling Go, then use a dedicated scratch directory and a new output filename:

```sh
GOMAXPROCS=4 GOFLAGS=-p=4 go test -c -o "$TMPDIR/local-performance.test" .
SVART_CORE_LARGE_BENCH=1 GOMAXPROCS=4 "$TMPDIR/local-performance.test" -test.run '^TestListPerformanceThreeMillion$' -test.v
```

The Python runner additionally records child resource usage and refuses to overwrite earlier evidence. Earlier binaries from the local-mutation branch were not measured; this is the single final-source run.
