# DD-003: Multi-instance sync via LWW registers, no leader election

**Decision**: Each node runs its own SQLite instance. Config sync uses last-write-wins (LWW) with Lamport timestamps on each config row. Nodes periodically exchange diffs ("what changed since timestamp X?"). Any node can accept writes — no leader, no election, no write forwarding.

**Why**: The data being synced is tiny — a few KB of config that changes a few times a month at most (upstreams, blocklist assignments, rewrites, client/group definitions, settings). This isn't "thousands of concurrent writes" territory. At this volume, LWW registers are simpler than leader election: no promotion logic, no split-brain write refusal, no forwarding. The only conflict case is two admins changing the same setting on different nodes simultaneously, and LWW handles that correctly (latest timestamp wins). Different keys modified on different nodes merge trivially.

This is technically a CRDT (LWW-Register is the simplest one), but it's so simple that calling it "CRDT" oversells the complexity. It's really just "each row has an updated_at timestamp and highest timestamp wins on merge."

**What syncs**: Configuration only (upstreams, blocklists, rewrites, client/group assignments, allowlists, settings). Query logs stay local to each node — they're high-volume write-heavy data that the LGTM stack aggregates centrally. Blocklist domain data (the 500k+ entries) is derived — each node fetches from the same URLs independently.

**Sync protocol**: Nodes poll each other every 500ms-1s. Request: "give me everything with updated_at > X". Response: the changed rows. Receiver merges by comparing timestamps. Distributed? Yes. Complex? No.

**Real-world usage pattern**: In practice this is active/passive failover, not active/active load balancing. The router lists two DNS IPs but prefers one (node 1). Node 2 sits warm with synced config but a mostly-cold cache. During a Hypervisor restart (~10 minutes/month), the router fails over to node 2. When node 1 comes back, traffic returns to it. Concurrent writes on different nodes are theoretically possible but practically won't happen — you're not making config changes during a 10-minute maintenance window. LWW is still correct (it's too simple not to use), but we shouldn't over-engineer for a concurrent-writes scenario that's basically fictional.

**Revisit if**: We encounter a data type where LWW is genuinely wrong (e.g. a counter, or a set where concurrent add+remove needs to be order-independent). For now everything is key-value config and LWW is perfect.
