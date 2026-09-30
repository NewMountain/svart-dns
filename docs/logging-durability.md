# Durable query and Loki logging

Query logging uses bounded asynchronous admission. A DNS answer waits only for
space in the in-memory queue, not a disk commit. The background writer batches
original events into `DB_PATH.spool.sqlite`, a SQLite WAL database using
`synchronous=FULL`. **A sudden process or host crash can lose the newest events
still in memory, including events for answers already sent.** This tradeoff was
explicitly chosen to prioritize DNS latency. A normal DNS response is not a
durable acknowledgement. Once the background commit completes, event recovery
relies on the filesystem and device honoring flush requests.

The existing query-log UI and analytics continue to use `query_logs`, including
its doom-loop summary rows. Those summaries are a presentation, not a lossless
archive. `journal_records.payload` holds the complete original event JSON,
including nanosecond timestamp, query type, latency and every flattened policy
field. Raw events are not removed when their summary reaches `query_logs`.

## Admission and recovery

The queue holds 65,536 events, with up to 5,000 additional events in the active
writer batch. At most 1,024 DNS handlers may wait for queue space; this gate also
covers cache hits, blocked queries, rewrites and forwarded answers. Full memory
applies backpressure rather than dropping events or spawning unbounded logging
goroutines. The gate rejects excess requests with SERVFAIL before resolution;
transient storage failures continue buffering while capacity remains. Rejections
increment the existing overload counter and are not accepted query-log events.
With query logging and direct Loki logging disabled, this gate does not run.

A failed commit retains its batch and retries until storage recovers. Previously
committed events stay on disk. During an outage the bounded volatile queue can
hold accepted events, but does not become crash durable. Clean shutdown drains
all accepted events and waits if their storage is unavailable; it must not be
reported as successful while blocked. Forced termination forfeits the volatile
tail under the chosen asynchronous contract. Restore writable storage to resume
admission. Do not delete journal databases, WAL files or unprocessed legacy files
to clear an alert. Presentation database outages retain the raw journal backlog
for replay, even when the volatile queue has drained.

`query_journal_progress` in the application database stores the raw journal
identity and committed sequence. Presentation rows and that cursor commit in
one transaction, so dying immediately after the database commit cannot cause
those events to be inserted twice. There is no second filesystem checkpoint or
truncate/reset window. A new journal has its own persisted identity, so moving
or reopening the database does not change event identity.

Legacy `DB_PATH.spool` JSONL and its `.offset` are read without modification.
Exact durable prefix provenance allows a later upgrade to import new appends
made during code rollback. Complete unprocessed records are imported
transactionally; changed prefixes or missing provenance fail explicitly. See
[legacy spool migration](legacy-spool-migration.md). An interrupted final
line remains in the original file and in `journal_quarantine`, with its byte
position and reason. A corrupt complete record fails startup with its position;
it is not silently skipped. The old empty-file/nonzero-offset crash state is
recognized as a previously completed legacy journal. Details already discarded
by older versions cannot be reconstructed.

## Direct Loki delivery

With `LOKI_URL` configured, stdout keeps receiving the same records and direct
Loki delivery uses `DB_PATH.loki.sqlite`. Non-DNS `Handle` returns after its complete JSON record is durable. DNS and
resolver warning/debug records use a 4,096-entry asynchronous queue with one
bounded background batch, so storage cannot reintroduce a synchronous DNS reply
wait. Queue saturation backpressures the bounded DNS handlers; its volatile tail
has the same accepted crash risk and clean-shutdown drain requirement.

Configured query-line payloads enter `query_loki_outbox` in the same transaction
as their presentation rows and replay cursor. A crash after that commit resumes
the outbox handoff on restart, independently of the already advanced query
cursor. Stable retry tokens deduplicate repeated admission while records remain
in the Loki journal. The receiver protocol remains at least once, including a
crash after remote acknowledgement but before the source outbox retires its
payload. Query batches use grouped admission instead of per-row fsync. Handler
attributes, groups and concurrent rendering are preserved.

A receiver timeout, transport failure, HTTP 400, 429 or 5xx leaves the original
record pending. Successful receiver responses and the local acknowledged cursor
permit transactional removal of the uploaded records. New row identities keep
increasing after compaction. Shutdown cancels the HTTP request and leaves any
unacknowledged rows for restart.

The push protocol provides **at least once** delivery: a receiver may accept a
request whose response is lost. Retrying preserves the original timestamp and
line; this client cannot promise exactly-once behavior at a remote receiver
without receiver-side idempotency. A permanently rejected payload remains
recoverable in the journal and requires correcting the receiver/configuration.
It is never discarded after an arbitrary retry count.

## Monitoring and backups

The raw event timestamp marks the DNS decision, before queue admission. Its
`latency_us`, the existing query-log/dashboard latency, and DNS query-duration
metric describe processing time up to that point; they exclude queue saturation
wait and the final socket write. Client-observed UDP measurements include both.
Full response-duration instrumentation remains an observability integration
requirement.

- `svart_dns_log_writer_batch_duration_seconds` measures background encoding,
  journal batching and durable commit, without labels.
- `svart_dns_log_writer_queue_length` counts volatile events waiting for durable
  commit, including the active batch and bounded waiting handlers. Its high-water
  counterpart tracks the peak since process start.
- `svart_dns_log_writer_spool_pending_bytes` and `_pending_records` count events
  not yet represented in `query_logs`. They reach zero after replay even while
  the raw history remains retained. A failed storage read yields NaN, not zero.
- `svart_dns_log_writer_journal_retained_bytes` includes the journal database,
  WAL and shared-memory files, including already replayed raw history.
- `svart_dns_log_writer_loki_handoff_failed_total` counts failed transactional
  outbox transfers; their original payloads remain recoverable.
- The compatibility dropped counter remains zero; overload rejections are
  recorded at the DNS admission boundary.

Back up the application database and raw journals together while stopped, or
use SQLite's backup API with an ordering that includes all journal events
referenced by the backed-up presentation cursor. Copying only the main SQLite
file while a WAL is active is not a complete backup.

Processed raw events move into separate, verified Parquet segments under
`ARCHIVE_PATH/raw-v1/<journal identity>`. Exact payload bytes, nanosecond times
and sequence identities survive. Publication and full read-back equality precede
transactional source retirement; SQLite reuses the freed pages. Configured age
retention expires raw history with recoverable verified rewrites. See
[Raw query history archives](raw-archives.md) for ownership, failure recovery,
separate metrics and the read-only operator verification command. Include that
raw archive directory in every complete backup.

## Validation and performance

Regression tests exercise child-process exits immediately after durable
acknowledgement and after the destination transaction and before its Loki handoff, death during an
uncommitted SQLite write, actual `SQLITE_FULL` and recovery, legacy incomplete
writes, complete per-event round trips, failed Loki receivers with restart,
concurrent saturation and real UDP replies while the journal is deliberately
blocked. Clean shutdown waits for the blocked journal and then drains every
accepted event. The test processes own their temporary SQLite files and loopback
receivers.

`BenchmarkDurableDNSUDP` measures real UDP replies with 64 concurrent clients,
logging enabled/disabled, p50/p99 latency, throughput and peak RSS. Use at least
262,144 requests (four times queue capacity); diagnostics distinguish response
completion, queue peak/end, durable records at response completion and subsequent
drain duration. The final exact raw count includes the warmup query.

### Final backend measurements — 2026-09-29

The production baseline `b67c6f4` and final backend `ab901e3` used the same
Go 1.26.8 toolchain, local upstream, 64 effective client addresses and 5% unique
misses. Each fresh run sent 262,144 queries; three pairs alternated build order.
Direct query-line logging and Loki were disabled for this first comparison.

| Query persistence | Build | Median replies/s | p50 range | p99 range |
|---|---|---:|---:|---:|
| Off | Baseline | 159,419 | 228–279 µs | 2.97–4.12 ms |
| Off | Final | 138,818 | 252–302 µs | 3.04–4.66 ms |
| On | Baseline | 89,032 | 449–839 µs | 3.79–6.71 ms |
| On | Final | 64,595 | 494–605 µs | 4.65–5.14 ms |

Every request in these twelve runs received a successful answer: zero DNS
errors or timeouts. The final build's median throughput was about 13% lower
with persistence off and 27% lower with it on. These are observed build-level
differences, not a claim that one change causes those percentages. A separate
longer, profiled one-million-query off-path comparison had nearly equal CPU
consumption and throughput; shared-host and short-run variation remain relevant.
The measurements do **not** establish performance equivalence.

The three final persistence-enabled runs reached volatile high-water marks
of 32,938–70,600 events. The largest includes the buffered queue, active batch
and waiting admissions. At the immediate post-reply metrics observation,
16,561–63,269 events remained volatile; another 5.0–8.1 seconds drained the
remaining presentation work. The request and response times of that HTTP
snapshot are retained; it is not an atomic observation at the last DNS packet.
All three clean shutdowns retained exactly 262,144 originals each.

Direct query-line delivery was then tested against healthy, slow and unavailable
Loki receivers with 262,144 requests each. A separate sustained outage sent
1,000,000 requests at 10,000 offered QPS. It achieved 9,999 replies/s, p50
123 µs and p99 6.05 ms, with zero errors or timeouts; receiver recovery drained
the durable outbox in another 46.8 seconds. Across all seven persistence runs,
the combined journal/archive checks cover 2,572,864 records: sequence uniqueness,
counts, seeded request names and selected field invariants. Independent journal
snapshots provide complete payload-byte comparisons for 1,589,824 records.
However, the sustained run had already archived 983,040 records before its first
snapshot, and the verifier reads the same archive files on both passes. Their
payload hash comparison is therefore a self-comparison, not independent proof
of unchanged bytes through archival. The retained evidence explicitly records
this limitation. An independent receiver oracle confirms every stdout DNS line
appears byte-identically in acknowledged Loki pushes; those lines are a subset
of the raw event and cannot establish complete raw-payload identity.

CPU profiles show material replay, SQLite and allocation work when retaining
originals and updating presentation tables. This is a measured cost of the
implemented retention path, not a claim that it cannot be optimized. The harness
also polls complete `/metrics` once per second: journal backlog/count/byte scans
accounted for material observer cost under load. In the final enabled profile,
Prometheus gathering accounted for 12.8% cumulative CPU, while the per-DNS
histogram observation itself accounted for 0.16%. These call-tree shares overlap
and must not be added. Production's longer scrape interval differs from this
measurement. Queries still use asynchronous admission; they do not wait for
an individual durable disk commit, but queue saturation can apply backpressure.

All commands, result counts, observation times, resource samples, profile
diagnostics and original-byte checks are retained with
[the final evidence](../benchmarks/evidence/2026-09-29-final/README.md).

### Earlier asynchronous development measurements


Development measurement: AMD Ryzen 9 5950X, four Go workers, Btrfs,
64 UDP clients, 262,144 requests per repetition, two repetitions. Query logging
was enabled/disabled as listed; direct Loki and `LOG_QUERIES` were disabled.
Other host workloads were active, so these figures are observations, not a
controlled attribution or a claim of performance equivalence. The after runs
used the logical-batch implementation before the final transactional Loki outbox
and removal of preemptive storage-error admission checks. Those later changes
were race-tested; this workload had Loki/query lines disabled and healthy
storage. These are not measurements of the final committed source.

| Mode | Baseline replies/s | Async replies/s | Baseline p50/p99 | Async p50/p99 |
|---|---:|---:|---:|---:|
| Query logging off | 191,659–199,806 | 106,556–116,420 | 0.31–0.32 / 0.88–0.95 ms | 0.48–0.51 / 2.09–2.29 ms |
| Query logging on | 132,040–134,359 | 73,137–79,316 | 0.43 / 2.35–2.44 ms | 0.63–0.68 / 3.61–3.82 ms |

Both async repetitions recovered exactly 262,145 raw events including warmup.
At the last response, 7,103–42,104 events remained volatile; drain took another
0.12–0.42 seconds. Peak queued events were 15,496–45,536 and peak process RSS
was 90–123 MiB (baseline about 58–59 MiB). Earlier saturation of the same queue
reached 70,600 queued/active/waiting events and about 155 MiB RSS; the queue
applied backpressure, then drained every event. Neither peak is a guaranteed
application memory ceiling for arbitrary event payloads.

Preserving each ready 5,000-event logical batch avoids splitting it across the
journal's grouping timer. Final runs committed roughly 3,591–3,641 events per
commit on average, including timer-flushed small batches, with 18.6–20.1 ms
average commit time. Raw fixture payloads were about 287–297 bytes: a full batch
contains about 1.4 MiB of payload, excluding indexes and SQLite page overhead.
The independent stalled-worker test holds exactly 8 queued events and 1,024
waiting handlers, rejects 64 excess requests, then recovers exactly all 1,032
admitted events. Production uses the larger 65,536-event queue.

Those development measurements left the quiet system comparison and direct-Loki
workload unverified at the time. The final measurements above now cover them.
The reply contract remains asynchronous admission with the explicit volatile
crash tail described above.

### Rejected synchronous prototype

The following historical artifacts describe the rejected durable-before-reply
prototype, not current asynchronous behavior. Chris explicitly chose asynchronous
replies after reviewing this tradeoff. They are retained to explain that decision.

Measured development results (Btrfs, AMD Ryzen 9 5950X, four Go workers,
64 UDP clients, 4,096 replies per run, three reserved-window repetitions):

| Mode | Baseline replies/s | Durable replies/s | Baseline p50 | Durable p50 |
|---|---:|---:|---:|---:|
| Query logging off | 121,183–196,458 | 92,213–161,702 | 0.30–0.36 ms | 0.37–0.58 ms |
| Query logging on | 72,330–133,552 | 1,454–1,705 | 0.42–0.64 ms | 42.0–48.1 ms |

Enabled p99 changed from 2.25–4.79 ms to 73.5–82.6 ms. Peak process RSS was
about 53–55 MiB before and 54–56 MiB after. Raw fixture payloads were
287–297 bytes; observed commits held about 33–37 events (roughly 9.5–10.6 KiB
of payload, excluding SQLite pages/indexes) and averaged 21–23 ms. The worker
uses grouped commits, not a synchronous transaction per event under this load.
These measurements do **not** satisfy performance equivalence. Disabled-path
short runs also vary substantially; they do not establish an attributable
regression in its unchanged cache/lookup work.

A separate scratch-only storage experiment measured plain append+fsync of
16 KiB at 7.54 ms p50 and SQLite FULL commits at 6.69 ms p50; NOCOW did not
improve SQLite's median (6.78 ms). CPU/block profiles located most waiting in
durable-admission acknowledgements. WAL checkpoint latency was not isolated.
This prototype was rejected; it is not a deployable alternative or a performance
claim for the asynchronous implementation.
