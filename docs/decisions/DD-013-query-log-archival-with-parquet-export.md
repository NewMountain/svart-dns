# DD-013: Query log archival with Parquet export

**Decision**: Query logs older than 2 years (730 days) are automatically archived to Parquet files on a daily granularity. The hard retention limit is 3 years (1095 days) — `cleanupOldLogs()` enforces this as a safety net. In normal operation, the archiver runs at the 2-year mark so the server only ever holds ~24 months of live SQLite data.

**Archive format**: Parquet with Snappy compression, written by `github.com/parquet-go/parquet-go`. Each file covers one calendar day: `svart-dns-YYYY-MM-DD.parquet` (named by that day's date). Schema matches `query_logs` columns exactly, including the 3 audit trail columns (block_source, block_list_id, block_list_name). Parquet is natively readable by DuckDB, Spark, Pandas — ideal for ad-hoc analytics on old data.

**Background worker**: `startArchiveWorker()` runs a daily tick (24h interval). Each tick scans for complete days older than the cutoff, exports to Parquet (streamed in 10k-row batches, not all-in-memory), fsyncs, then DELETEs the archived rows from SQLite. Writes to a temp file first, then atomic rename. Idempotent — if the Parquet file already exists, the day is skipped.

**Configuration**: `ARCHIVE_PATH` environment variable (default `./archives`). Same pattern as `DNS_PORT`, `ADMIN_PORT`, `DB_PATH`.

**API**:

- `POST /api/archive` — manual archive trigger, returns status after running
- `GET /api/archive/status` — oldest data age, archive path, list of existing archive files with sizes, live row count

**Storage math**: At 300 bytes/row in SQLite, Parquet with Snappy achieves 3-5x compression on string-heavy columnar data. 500k queries/day × 365 days ≈ 182M rows/year → ~55GB SQLite → ~11-18GB Parquet. Well within the 2TB budget.

**Lifecycle**: `closeArchiver()` stops the ticker and waits for any in-progress archive to finish. Called from `closeDatabase()` before closing the log writer and main DB.

**Why**: Rich query analytics (the whole point of this project per the VISION) require long-term log retention. The previous 7-day default was a placeholder. 3 years of data enables seasonal analysis, year-over-year comparisons, and long-tail device profiling. Parquet archival keeps the live SQLite lean while preserving full history for offline analysis.
