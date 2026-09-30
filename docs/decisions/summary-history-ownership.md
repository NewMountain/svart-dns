# Recoverable summary history ownership

This decision supersedes the existence-only publication and whole-day deletion
protocol in DD-013. The Parquet presentation schema and base daily filename stay
compatible. Raw original events remain governed by the separate raw archive
protocol; presentation summaries cannot replace those originals.

A day is captured with an upper source ID. Each SQLite read snapshot streams at
most 65,536 rows or 64 MiB of source identity JSON into an immutable segment.
Parquet row groups target 512 rows; Go processes one source record at a time.
A single large record remains whole even if it exceeds the byte target. New IDs
outside the captured upper bound remain hot for the next run. The first segment
uses `svart-dns-YYYY-MM-DD.parquet`; subsequent segments use a unique suffix before
`.parquet`, which existing wildcard readers already support.

A `.sources` JSON stream records each exact SQLite source ID and all its
presentation values, including nulls and original timestamp text, plus its
Parquet projection. The file and sidecar are synced before an additive
`summary_archive_files` manifest commits the publication intent. A dedicated
SQLite connection uses FULL synchronous mode; DNS handlers retain asynchronous
admission with the accepted newest volatile crash tail.

Publication renames the staged file, syncs the directory and reads every Parquet
record back against the source projection. Both complete file digests must match
the manifest. Retirement takes a bounded SQLite writer transaction, compares
all exact source identities again, deletes only those IDs with checked affected
rows, and changes the manifest to ready in that same transaction. Every read,
iteration, close, mutation and commit failure retains a recoverable owner.
Restart completes pending intents. A late event for a previously archived day
creates another segment; it cannot be deleted by an earlier day-wide predicate.

Investigation's existing process mutex serializes a complete query with each
bounded archive transition and retention transition. Pending ownership, missing
expected files, expired files that reappear, and legacy overlap return unavailable
rather than hot-only or double-counted results. Legacy files without manifest
provenance remain readable when there are no same-day hot rows. Ambiguous overlap
requires explicit exact reconciliation; count equality never authorizes deletion.

Retention records expiring intent before unlinking and fsync, then records
expired state. Sidecar cleanup resumes after interruption. Hot retention waits
while pending summary ownership needs recovery. Pre-intent crash copies can be
renamed to recoverable `.orphan-*` files; they are never query-visible originals.

The catalog and legacy import provenance are additive storage changes, not
schema-neutral changes. See [summary recovery and rollback](../summary-archives.md)
for backup, prior-image readability and rollback restrictions.

Ready-file read verification now compares fresh complete SHA-256 digests on every
query/schema/status operation. File existence or metadata equality is insufficient:
an independently reproduced valid replacement returned incomplete totals after
retirement. Query and schema use pre/post verification; status uses one final
pass. No metadata cache authorizes mutable bytes. The additional O(total ready
archive bytes) I/O and parent 30-second metadata budget are explicit in
[summary operations](../summary-archives.md#ready-content-integrity-on-read).
