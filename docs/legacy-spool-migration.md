# Legacy spool migration across code rollback

A boolean import marker cannot prove that a JSONL file stopped changing. An older
image may append to the legacy spool after a code rollback, then a later upgrade
must account for every new original byte.

Migration now records additive `journal_meta.legacy_provenance_v1` JSON containing
observed file size, complete-prefix SHA-256, and last complete-record cursor. On
reopen it verifies the entire previously observed prefix and imports the appended
tail transactionally. Retry token reuse compares exact stored payload bytes.
Unknown fields and newline bytes are retained. An incomplete line is quarantined
under its observed source digest and offset; later completion imports the entire
record without overwriting that original fragment. Reads are streamed one record
at a time from a bounded file-size snapshot. No original file is truncated.

A replaced/truncated file, changed prefix, conflicting token, or an existing
`legacy_imported` marker without exact provenance fails startup explicitly. Keep
the JSONL, offset, durable journal, archive catalog, and raw files together for
reconciliation. Never remove a marker or restore an old database to force replay:
raw originals may already be archived, and blindly reimporting them can duplicate
presentation or delivery.

The new metadata is additive and older images ignore it. A safe deployment
pipeline must preserve these files on code rollback and exercise both append-only
reupgrade recovery and explicit refusal of ambiguous offset reuse. Do not run two
binary versions concurrently against the same spool. Production pipeline changes
and actual downgrade validation are separate release gates.

`TestLegacyRollbackTailAndReusedOffset` proves real reopen/tail import and
non-destructive refusal of a replacement file.
`TestLegacyPartialCompletionAndMissingProvenance` proves complete-byte recovery
of a later-finished line, preserved quarantine bytes and fail-closed missing
provenance. Existing legacy empty-file/truncate-crash and partial-record tests
remain unchanged.
