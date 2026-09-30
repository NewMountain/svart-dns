# DD-010: Separate wildcard patterns from exact domains at load time

**Decision**: When loading blocklist domains into memory, classify each domain into one of three buckets at load time using `classifyDomain()`:

1. **`exactDomains`** (`map[string]bool`): No wildcards. O(1) exact match + subdomain walk.
2. **`wildcardSuffixes`** (`map[string]bool`): `*.suffix` patterns (e.g. `*.facebook.com` → stores `facebook.com`). At query time, do the same subdomain walk starting from label index 1 (because `*.facebook.com` must NOT match `facebook.com` itself). O(label_count).
3. **`complexWildcards`** (`[]string`): Rare patterns like `ad*.tracker*.com`. Tiny linear scan of typically 0 entries.

These are stored in a `clientBlockData` struct per client, replacing the old flat `map[string]bool`.

**Why**: Benchmarking against Hagezi Ultimate (267,802 domains, zero wildcards) revealed that every cold cache miss triggered a linear scan of ALL domains calling `strings.Contains(pattern, "*")`, costing **4.15ms per lookup**. This was 50,000x slower than the cached path (74ns).

**Implementation**: The flat `map[string]map[string]bool` in `blocklistStore.clientDomains` was replaced with `map[string]*clientBlockData`. Classification happens once in `loadBlocklistsFromDB()`. Real-world wildcard lists (e.g. Hagezi wildcard list, ~182k entries) are overwhelmingly `*.suffix` form — complex patterns are essentially nonexistent.

**Measured impact**: Cold miss dropped from **4.15ms → ~125ns** (33,000x faster). Wildcard scan with 1,000 `*.suffix` wildcards: **~870ns** constant regardless of count (was linear in domain count). See [BENCHMARKS.md](../benchmarks.md) for full numbers.
