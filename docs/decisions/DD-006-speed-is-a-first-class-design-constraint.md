# DD-006: Speed is a first-class design constraint

**Decision**: DNS resolution latency is treated as a critical performance metric, not an afterthought. This means aggressive in-memory caching, honoring TTLs (with a floor for sanity), and minimizing external round-trips. Hardware is abundant (Threadrippers, plenty of RAM) — trade memory for speed where it makes sense.

**Why**: Slow DNS makes the entire internet feel broken. This is the hottest path on the home network. Users notice DNS latency immediately and viscerally. The whole point of self-hosting is control, and that control shouldn't come at a performance cost.

**Implications**: It's fine to burn 2GB of RAM on a hashmap if that's what fast lookups require. Optimize the hot path (query → cache check → blocklist check → upstream resolve) ruthlessly. Don't be stupid about resources, but don't be stingy either.
