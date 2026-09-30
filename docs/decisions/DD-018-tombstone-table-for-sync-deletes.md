# DD-018: Tombstone table for sync deletes

**Decision**: Use a separate `sync_tombstones` table for propagating deletes across peers, instead of soft deletes on config tables. Hard deletes + CASCADEs stay as-is. Before deleting a parent entity, record tombstones for it and its junction rows.

**Why**: Soft deletes would require filtering every SELECT and every in-memory loader to exclude `deleted_at IS NOT NULL` rows — dozens of sites across the codebase, each a bug waiting to happen. Tombstones are write-once, append-only, and queried only by the sync protocol. No changes to SELECTs, loaders, or the hot path.

**Schema**: `sync_tombstones(table_name TEXT, natural_key TEXT, deleted_at DATETIME, node_id TEXT, PRIMARY KEY(table_name, natural_key))`. Natural keys use the same patterns as config export/import: alias for lists, upstream address for upstreams, CIDR for ranges, group name for groups, `"|"` separator for junction table composite keys (e.g., `"10.42.1.42|Hagezi Pro"` for client_blocklists).

**GC**: Hourly ticker. Tombstones older than 24h AND older than every peer's last successful sync are hard-deleted. If no peers configured, clean unconditionally after 24h.

**Revisit if**: Tombstone volume becomes a concern (unlikely — config changes are rare, ~few/month).

**Merge safety**: Retained tombstones participate in last-write-wins comparisons
for all live rows, including junctions and manual domains. Equal timestamps favor
deletion; strictly newer writes may re-add a row. Deleting a junction applies the
same timestamp rule as deleting a simple entity. Read, scan, iteration, and write
errors abort the transaction together with its new tombstones. Missing required
parents fail explicitly so the cursor cannot skip unresolved relationships.
