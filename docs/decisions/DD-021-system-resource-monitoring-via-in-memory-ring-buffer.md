# DD-021: System resource monitoring via in-memory ring buffer

**Decision**: Add CPU, memory, and disk monitoring to the dashboard via a lightweight in-memory ring buffer (120 samples × 30s = 60 min history). No database writes. Data is lost on restart, which is acceptable for live monitoring.

**Metrics collected every 30s**:
- **CPU %**: Process CPU via `syscall.Getrusage` diff (user+system time between consecutive samples), normalized to per-core percentage
- **RSS**: Resident set size from `/proc/self/statm` — actual physical memory the kernel has allocated, matching what `htop` shows
- **Heap**: Go heap allocation from `runtime.MemStats.Alloc` — live objects on the Go heap. Notably higher than RSS when the kernel compresses or swaps pages (e.g., 6 GB heap vs 900 MB RSS with 4.9M blocklist domains loaded across 19 lists)
- **DB size**: Combined size of SQLite DB + WAL + SHM files via `os.Stat`

**Why RSS and Heap separately**: `MemStats.Alloc` reports Go's view of live heap objects, which can be dramatically larger than RSS when the kernel uses zswap/compressed memory or hasn't faulted pages in. For capacity planning (the primary use case), both numbers matter: RSS tells you actual physical memory pressure, heap tells you how much the GC is managing.

**Why not `MemStats.Sys`**: `Sys` reports total virtual address space obtained from the OS, including memory the Go runtime reserved but never uses. It's misleading for capacity planning — a process showing 10 GB `Sys` might only need 1 GB RAM.

**Frontend**: Three area charts (CPU, Memory, Disk) in a `row-3-col` layout. Memory chart shows two series (RSS blue, Heap purple) with a Y-axis that auto-switches between MB and GB. Charts appear after the DNS analytics rows (Traffic/Latency, donuts, clients, domains, overrides/failures) and before the live query log.

**Prometheus**: 4 scrape-time GaugeFuncs reading the latest ring buffer sample: `svart_dns_cpu_percent`, `svart_dns_memory_rss_bytes`, `svart_dns_memory_heap_alloc_bytes`, `svart_dns_disk_db_size_bytes`.

**Revisit if**: Need for persistent resource history (would require DB storage or external metrics), or if `ReadMemStats` stop-the-world pauses (~1µs every 30s) become noticeable under extreme load.
