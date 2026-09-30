# Raw query history archives

The raw archive worker checks at startup and once per minute. Small tails flush hourly; a full
segment can flush at the next check. It moves processed
original events out of `DB_PATH.spool.sqlite` into
`ARCHIVE_PATH/raw-v1/<journal identity>/YYYY/MM/DD/`. Every Parquet row contains the stable
journal identity, integer sequence, integer nanosecond timestamp and **exact
original payload bytes**. Unknown JSON fields and imported trailing newlines
survive. These files are separate from the summary Parquet glob; `query_logs`,
Investigation, existing summary archives and the UI keep their current schema.

A DNS response still uses the explicitly chosen asynchronous admission contract.
Archiving adds no filesystem work to a DNS handler. The newest volatile events
can be lost on abrupt termination; a durable journal commit remains the raw
ownership boundary. Storage failure retains durable source events and causes the
archive worker to retry on its next run. A clean shutdown joins the archive
worker before closing the query journal, then drains accepted query events.
Events persisted after the archive worker stops stay in the journal for the next
startup. Shutdown need not archive every event to preserve durable ownership.

## Ownership protocol

1. Read the durable `query_journal_progress` cursor from the application DB.
   The worker never retires an event beyond it. The transactional Loki outbox
   owns query-line delivery separately and is never deleted by raw archiving.
2. Plan a segment in `raw_archive_files` inside the journal DB. Each plan has
   exact first/last sequence, count, min/max nanosecond timestamp and a SHA-256
   digest framing every identity, sequence, timestamp and payload byte. Segments target 65,536 events or 64 MiB of payload, whichever comes first.
   Iterators target 4 MiB and at most 512 events per batch and Parquet row group.
   The segment never resides wholly in memory. One unusually large event is
   indivisible; the byte target can be exceeded by that event. There is no
   whole-history read or fixed-size truncation.
3. Write a mode-0600 `.partial`, finish Parquet, fsync the file, atomically rename
   it, and fsync its directory. Newly created directory ancestors are synced.
4. Read the published file completely. Validate schema, count, identity/order,
   nanosecond timestamp, digest and full field/byte equality with every original.
5. In one FULL-WAL transaction, delete only the verified immutable sequence range of
   source rows, mark the manifest ready, and advance `raw_archived_through`.
   Failure rolls back. The sequence allocator is never reset.

A pending plan survives a crash. Recovery verifies the complete originals
against it before reusing the plan or replacing an incomplete derived `.partial`.
An already published file is read and checked, never overwritten to conceal
corruption. A damaged published file stops that cycle and keeps any remaining
originals. Preserve it and restore/repair from a consistent backup; do not delete
journal rows to clear the error. Legacy spool files and `journal_quarantine`
are untouched. New late events receive new sequences and segments even if their
calendar day was archived earlier.

Deleted SQLite pages go onto SQLite's freelist and are reused by subsequent
admissions. The raw journal does not run whole-file `VACUUM`; its allocated
high-water size can remain on disk. WAL checkpointing retains SQLite's normal
behavior. An outage can still grow the journal until storage fills: no bounded
storage guarantee can coexist with unlimited unprocessed traffic and no loss.
Configured retention bounds retained history when delivery, storage and the
archive worker are healthy.

## Retention

`log_retention_days` must be an integer in `1..3650000`. A missing/invalid value
or failed settings/cursor read skips destructive archive work. The cutoff is
`now.UTC().AddDate(0, 0, -days)`. An event is expired only when its exact timestamp
is strictly before that cutoff; the event at the cutoff remains. Calendar
arithmetic is compared safely even when the configured horizon exceeds the
integer nanosecond range.

Expiration checks an indexed minimum timestamp, so unexpired segments do not
need to be reread on each cycle. A partially expired segment gets a durable
rewrite intent recording its source and the validated policy cutoff. Its
retained rows are published and fully verified in a new immutable segment.
One transaction makes the replacement ready and the original obsolete. Only
then is the obsolete file unlinked and its directory synced. The intent lets
restarts finish the same rewrite without abandoning an untracked replacement.
An already committed expiry decision can complete after restart; extending
retention cannot resurrect events already expired under an earlier valid policy.

## Operator verification and backups

Stop the instance, or use a consistent stopped backup of the journal and archive
directory. A live scan can race legitimate retention and report unavailable.
Run the same binary without starting DNS or reading its normal configuration:

```sh
svart-dns verify-raw-archives /absolute/DB_PATH.spool.sqlite /absolute/ARCHIVE_PATH
```

The command opens the existing journal read-only, checks SQLite integrity,
streams every ready segment, verifies full manifest digests and original JSON
nanosecond metadata, and reads every still-retained journal event. It rejects
missing/corrupt files, overlapping archived sequence identities, unknown files,
and incomplete recovery. It reports only identity and counts, never query
payloads. Successful output has this form:

```text
verified journal=<identity> archived_records=1234 journal_records=12 quarantine_records=0
```

Quarantine counts describe preserved recovery material, not validated DNS
records. Pending plans or rewrite intents require starting the service to resume
recovery before verification. A nonzero exit is not permission to delete sources.
Back up the journal (including its SQLite/WAL consistency), application database,
raw and summary archive directories, and original legacy recovery files together.
A copy of only the SQLite main file while its WAL is active is incomplete.

## Signals and verification evidence

- `svart_dns_raw_archive_retained_records` counts events owned by ready raw files.
- `svart_dns_raw_archive_retained_bytes` counts raw-directory disk bytes, including
  recoverable partial files, sampled at startup, hourly, and after failed
  cycles. Between inventories it keeps the previous sample; inventory failures
  yield NaN.
- `svart_dns_raw_archive_failures_total` and
  `svart_dns_raw_archive_cycle_seconds` expose failure count and cycle duration.
- Existing spool pending metrics describe presentation backlog; existing journal
  retained bytes describe journal/WAL storage. Neither is raw archive size nor
  an acknowledgement from Loki.

`TestRawArchiveAbruptCrashMatrix` kills a child process at eleven persistence
boundaries and verifies complete recovery. Other tests cover actual `ENOSPC`,
valid Parquet with altered payload bytes, corrupted/missing archives, invalid
retention/cursor reads, nanosecond expiry, late events, independent Loki outbox,
legacy quarantine, worker startup/shutdown/restart and SQLite page reuse. A
three-round fixture with 2,048 events per round retained 6,144 exact identities
in three streaming segments while the journal stayed at 2,094 pages, with 2,078
pages reusable after each pass. This fixture is storage-correctness evidence,
not a production throughput or compression claim.

## File cardinality and traversal

At 20 events/second and ordinary DNS payload sizes, a 65,536-event segment fills
in about 55 minutes: roughly 27 files/day or 9,900/year (about 29,600 across the
default three-year horizon). At lower rates, hourly tail flushes limit steady
small-file creation to 24/day. At 100 events/second the event threshold implies
about 132 files/day. These are arithmetic projections, not a production load
measurement; very large payloads can instead hit the 64 MiB target earlier.

Partitions use the archive publication date, not event date, so late timestamps
and clock corrections do not hide new events. Verification orders manifests by
sequence, independently of partition names. It reads one manifest at a time;
expiration selects the next eligible manifest using the indexed minimum event
time. The filesystem verifier and byte inventory use 64-entry directory batches
at each of three bounded partition levels and reject symlinks/unexpected nesting.
No complete history, manifest list or directory listing is materialized in Go.
The byte inventory still performs work proportional to retained files, so it
runs at startup, hourly, and after a failed cycle to account for partial files,
rather than every minute. Manifest record counts refresh each cycle. Inventory
is skipped during shutdown; neither scan is a DNS operation. The manifest retains one row per live
or interrupted segment; committed obsolete entries are removed after fsynced
unlink. Normal startup preserves the last successful flush time, avoiding one
small segment per restart.
