# Summary archive recovery and rollback

Summary Parquet stores presentation rows. Original opaque query events, exact
nanoseconds, and unknown fields remain in the independent raw journal/archive.
The [ownership decision](decisions/summary-history-ownership.md) describes the
bounded publication and retirement protocol.

## Files and ownership

Back up the main SQLite database consistently with its WAL, the entire archive
root including `.sources`, `.pending`, and recoverable `.orphan-*` files, and the
separate raw journals and `raw-v1` tree. Stop/drain the service for a coordinated
filesystem snapshot, or use a storage snapshot protocol that preserves all these
owners together. Copying only final Parquet files cannot recover pending source
retirement. Never restore a stale database over a live newer database to roll
back code: it loses all changes made since the backup.

The additive `summary_archive_files` table has filename, UTC day, exact source
count, SHA-256 hashes for both files, and pending/ready/expiring/expired state.
Do not edit it or delete sidecars to silence an error. Startup/manual archive
cycles resume pending publication and retirement; retention resumes expiring
entries. A failed delete leaves hot originals and a pending cold file, and query
APIs report unavailable until recovery restores one query-visible owner.

A ready entry whose storage is missing is unavailable even if the entire archive
directory vanished. A truly fresh installation without expected cold owners may
still query hot data. Restore the expected mount or exact backed-up file, then
retry. Native storage diagnostics stay in server logs; API errors are generic.

If a pre-catalog daily Parquet file overlaps hot rows, neither source is removed
and unified queries report unavailable. Preserve both and compare complete
records and multiplicities, including null/timestamp provenance where available.
Only exact per-source evidence can establish which hot IDs may be retired.
Ambiguous records require operator reconciliation; no date/count-based bulk
repair is provided. Unregistered pre-intent staging copies can be inspected from
`.orphan-*` names and do not appear in the Parquet query glob.

## Compatibility and rollback gate

No existing `query_logs` columns or Parquet fields change. Prior images can read
ready hot rows and the unchanged summary Parquet schema, including suffixed
segments through their existing `*.parquet` reader. Additive tables are ignored
by those readers. This is a data-readability guarantee, not permission to run an
old archiver against unresolved ownership state.

Before a code rollback, stop admission and drain the current writer, finish all
pending/expiring summary transitions, preserve a coordinated backup, and verify
exact hot+cold counts. Keep current durable journal/outbox/raw files and the new
manifest untouched. The old image does not understand pending source ownership,
raw archive ownership, or legacy-import provenance; its background mutation
behavior needs an explicit compatibility gate in the deployment pipeline.
Production pipeline adaptation is a separate required rollout change. These
storage tests do not claim a production deployment or a safe arbitrary downgrade.

The old JSONL spool may receive new appends while an older image runs. On upgrade,
`legacy_provenance_v1` compares the complete previously observed prefix (size,
SHA-256, and last complete-record cursor), then imports only the new tail with
exact retry-token/payload equality. A partial line remains quarantined under its
observed source digest; completing it later imports the entire complete record.
Original files are never truncated or rewritten by migration.

A truncated/replaced file, reused offset with different bytes, or an older
`legacy_imported` marker without exact prefix provenance fails startup explicitly.
Preserve the JSONL, offset, SQLite journal and archives, then reconcile all
originals before restarting. Do not remove the marker or journal to force a
reimport: archived originals may no longer have hot token rows, and blind replay
can duplicate presentation and delivery. This fail-closed case must be exercised
in rollout/downgrade automation alongside the append-only recovery case.

## Executable evidence

`TestIndependentSummaryArchive*` covers the originally reproduced failed delete,
late replay, invalid timestamp, and SQLite iteration error. The additional
`TestSummaryArchiveAbruptCrashMatrix` kills subprocesses at seven publication/
retirement boundaries and three retention boundaries, then checks exact owners
and real Investigation results. `TestSummaryArchiveOwnershipFailuresAndRetry`
checks ignored mutations, failed commit, changed source, and damaged/missing
files. The bounded fixture produces two files for 65,539 small source rows, then
one additional file for a later historical arrival. Parquet groups target 512
rows, separate from the 65,536-row/64-MiB segment target.

`TestLegacyRollbackTailAndReusedOffset` and
`TestLegacyPartialCompletionAndMissingProvenance` exercise real JSONL reopen,
append, offset reuse, partial completion, and missing provenance. The independent
raw-originals test still compares every byte of 60 full rich source events after
coalescing, raw archival, retirement and restart.

## Ready-content integrity on read

A ready manifest is evidence of the exact expected Parquet bytes, not merely
that a filename should exist. Query execution, schema inspection and archive
status freshly hash every cataloged ready file with SHA-256 and compare the
complete digest before succeeding. There is no metadata/content cache: replacing
valid Parquet with fewer rows or different values is unavailable even when file
size, modification time, and row count are unchanged. In-place changes and file
renames are both checked. Restoring the exact original bytes restores access;
no rewrite, forced deduplication or source/sidecar deletion occurs.

One canonical verifier uses a 64 KiB read buffer, checks cancellation between
reads, and also checks file identity, size and modification time across the read
to reject ordinary concurrent changes. Metadata is only a change detector and
never substitutes for the full digest. Query and schema paths verify before
opening the unified view and again after consuming their result, before exposing
success. Status performs one final pass. Parent query dispatch does not duplicate
the worker's verification. All these surfaces share the existing Investigation
serialization lock so simultaneous parent metadata requests cannot multiply the
hashing workload. Request cancellation and archive-worker shutdown remain
interruptible while hashing or waiting for serialization.

This adds real I/O: query and schema integrity each read twice the total bytes
of all ready summaries, in addition to native query I/O; status reads that total
once. Query worker memory/OS limits and its existing request deadline remain in
force. Schema-worker and parent status operations have a 30-second budget, shortened
by any earlier caller deadline. Failed or incomplete validation cannot report complete
history. Large histories or cold/slow storage may exceed these budgets. There is
no production-scale latency guarantee or cached fast path claimed here.

One warm local disposable fixture with 8,192 source rows produced one 8,366,584-
byte file. Instrumented hashing read all 8,366,584 bytes in 129 reads, including
EOF, with a maximum 65,536-byte buffer. A non-race sample measured a 4.59 ms single
hash and complete query/schema/status operations of 51.4/17.9/4.98 ms. These are
one-host warm-cache observations, not throughput benchmarks or estimates for
cold storage or a multi-year archive.

The pre/post checks are not a filesystem transaction with an unrelated writer.
Archive files must remain immutable while served; stop the service before an
external restore or repair. A non-cooperating writer that changes and restores
bytes during the same operation is outside that immutability contract. Legacy
files without manifest digests retain their existing conservative provenance
rules; the service never fabricates a digest from untrusted current content.

`TestRecheckReadySummaryRejectsValidReplacement` preserves the independent
partial-file and same-count replacement reproductions across query/schema/status.
`TestSummaryIntegrityRejectsSameSizeSameTimeReplacements` additionally exercises
warmed surfaces, equal-size/equal-mtime files, both inode behaviors, private HTTP
errors and exact restoration. Streaming/cancellation tests read real files and
verify that cancellation after the first chunk returns no digest and leaves all
original bytes intact.
