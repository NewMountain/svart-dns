# Raw event archive ownership

The raw journal introduced for asynchronous DNS logging preserves information
that doom-loop presentation summaries cannot reconstruct. Keeping every processed
original in SQLite forever would create an indefinitely growing duplicate store.

Raw history therefore has its own versioned Parquet schema and subdirectory.
Immutable segments retain exact opaque payload bytes, integer nanoseconds and
stable journal/sequence identities. A durable manifest plans each bounded write;
file and directory sync plus complete read-back equality precede transactional
source retirement. SQLite freelist reuse avoids whole-file compaction on the
query journal. This does not change asynchronous DNS replies or the accepted
volatile crash tail.

Presentation ownership and historical ownership are separate invariants. The
presentation cursor limits eligible raw retirement, and the independently durable
query Loki outbox continues owning delivery after that cursor advances. Summary
Parquet files never establish raw ownership.

Configured retention is the operator's explicit expiry policy. Invalid or
unreadable policy prevents destructive work. Mixed-age files use a durable
rewrite plan and verified replacement before retiring the old file. The bounded
manifest and segments make both archival and retention recoverable across every
publish/transaction boundary. Original legacy files and quarantine are preserved.

See [raw archive operations](../raw-archives.md) for the on-disk protocol,
verification command, failure recovery and executable regression coverage.
