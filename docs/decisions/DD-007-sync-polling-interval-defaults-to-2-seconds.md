# DD-007: Sync polling interval defaults to 2 seconds

**Decision**: The multi-instance sync worker polls peers every 2 seconds by default (configurable via `SYNC_INTERVAL` env var).

**Why**: Config changes are rare (a few times a month after initial setup). A 2-second no-op poll is negligible network cost (a few hundred bytes round-trip returning an empty diff). Fast enough that config changes propagate almost instantly if you do happen to make one during failover. Slower intervals (30s+) would work for the failover use case but feel sluggish during initial setup when you're making rapid changes.
