# Benchmark evidence, 2026-09-26

Application source: `4f20de8`. These files contain synthetic benchmark traffic,
not production queries. See [the report](../../../docs/benchmarks.md) for the
interpretation, limits and Raspberry Pi verdict.

- `comparison.jsonl`: 24 accepted measurements. Svart, Pi-hole and AdGuard Home
  are from the same sequential run; Technitium is from the subsequent rerun
  with DNSSEC validation disabled for the unsigned local upstream. Query
  generator, stub, lists and CPU placement were otherwise identical.
- `environment.txt`: kernel, CPU model, placement and image versions.
- `benchmark-sha256.txt`: hashes of both application binaries, benchmark tools,
  and the five cached list inputs. Lists are not redistributed here.
- `build-current.txt.gz`, `build-baseline.txt.gz`: full Go build metadata. The retained
  baseline lacks an embedded source revision; its SHA identifies the artifact.
  For publication, only the first line’s executable pathname is reduced to its
  basename; full original metadata remains in the local raw artifacts.
- `footprint-*-natural.txt` and matching directories: same-harness runs without
  forced GC. Each has nine process-status/metric snapshots, final server log,
  and the full 200,000-query cache-load summary.
- `footprint-final.txt` and `footprint-final/`: additional current-only run with
  a forced-GC heap profile after each row. Profiles are sampled Go live heap,
  not total RSS. In the console tables, `heap MiB` means heap-in-use sampled
  before forced GC, not the profile's live-heap total.
- Raw process status, build metadata and Technitium logs are losslessly gzip
  compressed to preserve CRLF/trailing whitespace bytes; build metadata has
  only the explicit pathname normalization described above.
- Product logs are complete outputs from the accepted comparison runs.
  Pi-hole's startup messages include a failed default-list fetch in the
  network-isolated guest; the harness replaces it with the cached list before
  measuring, and every blocking-only query was subsequently validated.

Reproduction on an isolated host with these cached images and list hashes:

```bash
SERVER_CPUS=12-15,36-39 BENCH_CPUS=18-23,42-47 STUB_CPUS=16-17,40-41 \
  SVART_BIN=/path/to/svart-dns benchmarks/compare.sh /path/to/lists /path/to/results
BENCH_BIN_DIR=/path/to/tools SVART_CPUS=12-15,36-39 \
  PROFILE_GC=false PROFILE_DIR=/path/to/footprint \
  benchmarks/footprint.sh /path/to/svart-dns /path/to/lists
# Repeat the second command with the retained baseline binary and a new output directory.
# Omit PROFILE_GC=false for the current-only forced-GC profile run.
go tool pprof -top -sample_index=inuse_space /path/to/svart-dns footprint/heap-9.pb.gz
```

The server CPU set has four physical cores/eight threads. The 50 or 200 load
workers each use a separate socket, all from `127.0.0.1`; they are not distinct
policy clients. No benchmark load was sent to production.

Validation: all 24 rows have 300,000 measured queries and their outcome counts
sum to that total. All have zero DNS errors, unexpected allows and unexpected
blocks. The four blocking-only rows each contain 300,000 blocked replies. Only
Pi-hole's 200-worker mixed row has timeouts (396). All three footprint cache
loads answered exactly 200,000 queries without errors/timeouts/blocks.

Rejected attempts and earlier raw evidence remain in the complete local handoff
archive, including both application binaries. The archive SHA-256 is recorded
in `raw-archive.sha256`. It includes the glibc-incompatible initial launch,
RA=false stub attempts, the Technitium DNSSEC failure run, and the old binary's
unavailable profiling endpoint. None was substituted with zeros or used to
claim a performance win. The archive filename is a handoff artifact identifier,
not a public download URL.
