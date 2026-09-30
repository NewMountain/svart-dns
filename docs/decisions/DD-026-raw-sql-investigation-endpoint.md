# DD-026: Raw SQL investigation endpoint

**Decision**: `POST /api/investigate` accepts arbitrary SQL and executes it against a read-only DuckDB engine that unifies SQLite hot data and Parquet cold archives into a single `query_logs` view. Admin-only auth, 60s maximum timeout (30s default), 429 on concurrent access, 256MB DuckDB memory limit.

**Why**: The 8-day SQLite hot window (DD-025) means the dashboard can only show recent data. Long-term analysis (seasonal patterns, year-over-year comparisons, device profiling) requires querying the Parquet cold tier. Rather than building bespoke API endpoints for every possible question, a raw SQL interface lets admins answer unknown future questions about DNS traffic. DuckDB is purpose-built for analytical queries over Parquet files with columnar processing.

**Read-only by construction**: Parquet files are immutable. SQLite is attached via DuckDB's `ATTACH` with read-only mode. The `query_logs` view is a UNION ALL of both sources. There is no write path — even a malicious SQL statement cannot modify data.

**Concurrency**: Single-mutex access. DuckDB attaches and detaches SQLite per query (WAL checkpoint before attach ensures fresh data). Only one investigation query runs at a time — the 429 response signals the client to retry. This is acceptable because investigation is a low-frequency admin activity, not a hot path.

**Frontend**: Investigation page with CodeMirror 6 SQL editor (syntax highlighting, autocomplete), schema explorer sidebar showing all columns with types, and a results table with row count and execution time.

**Revisit if**: Injection concerns arise despite the read-only design, or if concurrent investigation queries become a real need (would require connection pooling or multiple DuckDB instances).

## Starter query contract (2026-09-26)

The UI imports `frontend/src/pages/investigation-templates.json`. The Go API
contract test embeds that same file and exercises every template over live
SQLite and both current and pre-DD027 archived Parquet rows, with exact
expected results. Templates count a legacy row with no `coalesced_count`
column as one query using `COALESCE(coalesced_count, 1)`. A new template
requires its own result contract. Client activity uses `CAST(timestamp AS DATE)`
because DuckDB 1.1.3 does not implement the SQLite-style `DATE(timestamp)` call.

A CTE-amplified four-way cross join is covered by the real HTTP timeout test
while twenty local UDP requests exercise the DNS handler. The test requires
a 504 within three seconds for a one-second query budget, DNS responses below
200 ms, and a successful follow-up query over all seeded records. CTE aliases
remain legal; the direct source-reference cap is not a total work estimate.
