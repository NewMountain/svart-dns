# Historical list pages

`GET /api/blocklists/history/{id}/domains` reconstructs an exact historical set and returns a sorted page. `search` is a case-sensitive literal substring (SQL wildcard characters have no special meaning). `total` is the complete matching count, including rows outside the page. Limits are 1–10,000; the default is 1,000. An offset beyond the result returns an empty array with the unchanged total.

A read transaction pins the checkpoint, current rules and changes to one SQLite snapshot. Per-request temporary B-trees store the earliest later change for each domain and the reconstructed set. SQLite uses disk-backed temporary storage with two 2 MiB page-cache budgets and memory mapping disabled during reconstruction. Go retains only the requested page. Disk exhaustion, cancellation, query, scan and iteration failures fail the whole request with unavailable; no partial page is published. Scratch tables and connection settings are cleaned up before the read connection returns to its pool. Reconstruction never uses the application write connection.

The first change after the requested history ID determines whether a changed domain existed at that checkpoint: a removal means it existed; an addition means it did not. Unchanged domains come from the current set. History IDs establish refresh commit order, including refreshes with equal timestamp seconds and imported legacy timestamp representations.

## Writer invariant

Every refresh must persist its current-domain replacement, history row and complete changelog in the **same transaction**, before publishing the new policy. All history for a list must use commit-ordered IDs. A transaction containing only part of a refresh violates the reconstruction invariant even though a reader has a coherent snapshot. The core-performance worker owns this writer repair in `blocklist.go`; the reader does not change refresh persistence. History is preserved in full.

## Evidence

`TestCheckpointSameTimestampReference` and `TestCheckpointMissingChangelogUnavailable` both fail against the preceding implementation. Additional tests cover literal search, late pages, request cancellation, connection reuse, and a real SQLite iteration failure via a malformed expression in a view.

`TestCheckpointMillionRulesBounded` inserts one million current rules and one million change rows, checks an exact cross-boundary page and total of 1,000,000, and samples process RSS during reconstruction. It enforces a 64 MiB growth ceiling. A local run measured 4,345,856 bytes of RSS growth (baseline 148,762,624; peak 153,108,480). This is incremental reconstruction memory, not an application-wide sizing claim.
