# DD-012: Batch log writer with dedicated WAL connection

**Decision**: Replace the goroutine-per-query `logQuery()` pattern with a channel-based batch writer. A single writer goroutine reads from a buffered channel (10M entries, ~2GB), accumulates entries, and flushes in batched transactions (5,000 rows per `BEGIN`/`COMMIT`, or every 100ms). The writer uses its own dedicated `*sql.DB` connection with WAL pragmas, separate from the main connection pool used for admin reads and config writes.

**SQLite pragmas** applied to all connections (both main and writer):

- `_journal_mode=WAL` — concurrent readers + single writer without blocking
- `_synchronous=NORMAL` — fsync on checkpoint only (not every commit), safe with WAL
- `_mmap_size=268435456` (256MB) — memory-mapped I/O for reads
- `_cache_size=-64000` (64MB) — larger page cache

**Why**: At 75k qps benchmark load, the old pattern spawned ~950k goroutines all waiting for the single `MaxOpenConns(1)` connection, consuming ~5.4 GiB of transient memory. The inserts were already async (DNS responses sent before logging), so this wasn't a latency problem — it was unbounded memory growth. The batch writer caps memory at a known maximum (~100MB channel buffer), achieves 100k+ inserts/sec throughput via batched transactions, and guarantees all entries are flushed on shutdown (channel drain + final flush before close).

**Channel overflow**: If the channel fills (10M entries = ~133 seconds of burst at 75k qps), the log entry is dropped and an atomic counter incremented. This is the correct tradeoff — DNS availability matters more than log completeness, and the DNS response is already sent before `logQuery()` runs. Operators can detect sustained overflow through `svart_dns_log_writer_dropped_total`; public `/health` is now status-only. With WAL + batched inserts achieving 100k-500k rows/sec, the channel should never fill under normal load. A rogue container spamming millions of queries might cause drops, but the DNS server stays healthy.

**Lifecycle**: `initLogWriter()` called after `migrateSchema()` in `initDatabase()`. `closeLogWriter()` called before `db.Close()` in `closeDatabase()` — closes the channel, waits for the writer to drain all remaining entries, then closes the writer DB. Guarantees zero data loss on graceful shutdown.

**Revisit if**: Write throughput needs to exceed 500k rows/sec (would need multiple writer goroutines or sharded channels), or if the 100MB channel buffer is too much/little for the deployment.
