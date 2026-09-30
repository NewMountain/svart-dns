# Three-million-rule small-edit and admin-read measurement

The identical opt-in `TestListPerformanceThreeMillion` harness was run before and after the incremental-index change with Go `go1.27.1-X:nodwarf5 linux/amd64`, `GOMAXPROCS=4`, and the same SQLite dependencies. Host: AMD Ryzen 9 5950X (16 cores / 32 logical CPUs), 131,790,736 KiB total RAM, Linux x86_64. Each executable seeds three published lists of one million distinct synthetic rules, eight ranges, eight groups, and 64 clients, then builds the policy before measuring. Every rule is processed; there is no sampling of the stored fixture.

Baseline: `19708ce` plus the updated harness only, in an isolated worktree. After: the implementation accompanying this evidence, including API worker commits `89ff950` and `e9b9d43` (locally cherry-picked as `f71c3c7` and `981b45a`). The timing harness SHA-256 for both binaries was `caa9f80011d8641d9759a7ce2c6422c8d5038581725232e5a1fb0f241e6ee6a9`. The tracked harness subsequently gained optional CPU profiling of only the manual-edit phase; this was disabled in the timing runs. The after executable predates the final `ApproxBytes` immutable-read fix; the fixture contains no complex rules and does not exercise that fix.

| Operation | Before | After |
|---|---:|---:|
| Manual edit 1 | 2.962 s / 887,975,168 B / 6,005,895 allocations | 19.906 ms / 4,741,712 B / 2,567 allocations |
| Manual edit 2 | 2.470 s / 887,294,784 B / 6,005,144 allocations | 1.049 ms / 7,400,032 B / 2,431 allocations |
| Manual edit 3 | 2.346 s / 887,128,000 B / 6,004,989 allocations | 1.222 ms / 10,127,104 B / 2,439 allocations |
| Index lookup, three 1M-query samples | 116.856 / 111.622 / 114.586 ns/query | 106.029 / 104.484 / 105.085 ns/query |
| First domain-page GET while writer active | 5.956 s | 101.059 ms |
| First metadata GET while writer active | 218.103 us | 119.751 us |
| One-million-rule persistence refresh | 6.814 s | 6.136 s |
| Whole-process peak RSS | 985,924 KiB | 603,968 KiB |
| Final sampled RSS | 607,392 KiB | 607,716 KiB |

All three manual changes go through the real HTTP handler and assert the resulting policy/custom-rule attribution. All index samples assert 666,667 matching queries. After the fix all 20 admin samples began while the writer was active; before, the first domain request waited until the writer finished, so later baseline requests are explicitly marked inactive and are not concurrent-refresh evidence. Complete raw logs retain every sample. The domain-page responsiveness improvement includes the API worker's six reader-pool query changes, not just the index change.

The fixture's refresh measurement calls `storeListGeneration` and includes current rows, metadata, history, changelog and local-generation marker persistence. It does not include downloading/parsing or the subsequent policy-index reload. Large published-list changes still use a full index rebuild. Final RSS is essentially unchanged; only the measured peak fell (38.7%). The fixed-loop allocation counters include background application workers and must not be attributed to the matcher. Isolated matcher allocation assertions establish the zero-allocation lookup contract.

This is a shared workstation: other agent builds/tests were paused for both measurement windows, but unrelated user processes remained running. There are three edit/query samples in one process per version, no forced GC, random hash seeds, and natural scheduler/GC variation. The result supports responsive small edits and removal of writer-pool blocking. It does not establish a dedicated-host DNS throughput improvement. No lookup slowdown was observed on this corpus.

## Reproduction

Build each version with the identical tracked harness and embedded frontend assets:

```sh
bench_scratch=$(mktemp -d "${HOME}/svart-core-bench.XXXXXXXX")
GOMAXPROCS=4 TMPDIR="$bench_scratch" GOTMPDIR="$bench_scratch" go test -p 4 -c -o "$bench_scratch/core-performance.test" .
TMPDIR="$bench_scratch" python3 benchmarks/evidence/2026-09-28-incremental/run-performance.py "$bench_scratch/core-performance.test" "$bench_scratch/performance.log"
```

The runner retains stdout/stderr and child-process resource usage and refuses to overwrite existing output files. Without `TMPDIR`, it uses a scratch subdirectory beside the output. `three-million-before-v2.log` retains the failed prelaunch attempt because `/usr/bin/time` was unavailable; the Python runner replaced it. `three-million-before.log` retains the earlier successful baseline harness without the lookup loop; only the identical v2 runs support the table above.

Focused race validation passed for parser, core matcher, incremental storage/configuration, failed builds, immutable footprint reads, refresh and policy golden cases (`incremental-final-race.log`). `footprint-read-before.log` records the deterministic failure before fixing `ApproxBytes` to avoid appending into a published complex-rule backing array.

## Causal diagnostics

Separate CPU-profile runs enable `SVART_CORE_MANUAL_CPU_PROFILE=/absolute/path/manual.cpu` with the same runner. Profiling starts after seeding and lookup, immediately before the three manual edits, and stops before refresh. Both complete diagnostic logs and binary profiles are retained. These runs overlapped other validation and are not timing comparisons. The baseline's 6.35 seconds of sampled CPU attributed 98.11% cumulatively to `policycore.BuildIndex` and 97.64% to `listRuleFeed`; SQLite row iteration accounts for 42.68% cumulatively. This directly locates the repeated full index rebuild. The after phase is too short to support a sampled CPU percentage claim; absence of a profile stack alone proves nothing. Changed-list-only SQL assertions, unchanged-partition identity tests, and per-edit allocation counts provide the after-path evidence. Reproduce full profile summaries with `go tool pprof -top -nodecount=0 BINARY PROFILE`.
