# DD-025: Hot/cold query log architecture

**Decision**: Split query logs into a hot tier (SQLite, 8 days) and cold tier (Parquet, 3 years), with embedded DuckDB-Go for unified querying across both tiers. Three SQLite connection pools: `db` (write, `MaxOpenConns(1)`, `_busy_timeout=5000`), `readDB` (read, `MaxOpenConns(4)`, opened with `mode=ro` via file: URI), and `writerDB` (log inserts, `MaxOpenConns(1)`). Daily archiving at midnight (user timezone setting, falls back to server local then UTC) with VACUUM after archive to reclaim disk space.

**Why**: A DNS doom loop incident caused the SQLite database to grow to 19GB, overwhelming the admin UI which was performing `COUNT(*)` aggregations over millions of rows on a single connection. The previous architecture (2-year archive threshold, single connection pool for reads and writes) couldn't handle the combination of write-heavy log inserts competing with read-heavy dashboard queries. Moving to an 8-day hot window (7 days for dashboard + 1 day buffer) keeps SQLite lean, while Parquet cold storage preserves the full 3-year history for investigation. The read-only pool (`readDB`, 4 connections) eliminates contention between dashboard queries and config writes.

**Archive threshold**: 8 days, down from 730 (2 years). Dashboard queries only look back 7 days maximum, so 8 days provides a clean 1-day buffer. Older data moves to daily Parquet files, queryable via the Investigation endpoint's DuckDB engine.

**Revisit if**: DuckDB memory usage becomes problematic (currently capped at 256MB), or if query performance degrades with many Parquet files (consider partitioning or compaction).
