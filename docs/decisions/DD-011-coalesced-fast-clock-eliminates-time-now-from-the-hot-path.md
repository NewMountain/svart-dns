# DD-011: Coalesced fast clock eliminates time.Now() from the hot path

**Decision**: Replaced all `time.Now()` calls on the DNS hot path with a coalesced `clock.Now()` backed by an `atomic.Value`. A background goroutine updates the shared timestamp every 500μs. This trades ~500μs time resolution (irrelevant for TTL checks on the order of minutes/hours) for a ~35ns savings per call. On the hot path (cache get + cache set + request timing + upstream latency = 4 calls per query), this saves ~140ns per DNS query.

**Implementation**: `fastClock` struct in cache.go. Initialized via `newFastClock()` at package init time (always running, including in tests). Hot-path callers in cache.go, dns.go, and resolver.go use `clock.Now()` instead of `time.Now()`. Background cleanup, bootstrap cache, and server startup still use `time.Now()` directly since they're not on the hot path.

**Why**: DD-006 says speed is first-class. `time.Now()` costs ~40ns per call (5x slower than `rand.Float64()` at 7.6ns), and accounted for ~9% of cache hit latency. The fix is 20 lines of code. There's no reason not to do it.

**Sources**: Direct measurement on Ryzen 9 5950X. See [BENCHMARKS.md](../benchmarks.md).
