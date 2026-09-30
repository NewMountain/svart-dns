# DD-016: Direct Loki push, no Promtail/Alloy dependency

**Historical delivery mechanics superseded:** the current implementation uses a
durable SQLite outbox, preserves every admitted event, and retries until receiver
acknowledgement. It never uses the historical drop-on-overflow behavior described
below. Optional Alloy collection is now supported; direct delivery remains. See
[asynchronous query journal](asynchronous-query-journal.md) and
[self-hosted observability](../observability.md).

**Original decision**: svart-dns ships its own logs to Loki via a custom `slog.Handler` wrapper (~200 lines, zero new dependencies). When `LOKI_URL` is set, the handler wraps the existing text/json handler: each `Handle()` call delegates to the inner handler for stdout AND queues a JSON-rendered entry to a 100k-entry buffered channel. A background goroutine batches entries by component and POSTs to `/loki/api/v1/push` every 1s or 1000 entries. One Loki stream per component logger (`{job="svart-dns", instance="<NODE_ID>", component="dns|admin|cache|..."}`). When `LOKI_URL` is empty (default), no handler is installed — zero overhead.

**Why**: The reference deployment runs in Docker inside two LXC containers, not on the NAS
Docker daemon managed by a container manager. Alloy's Docker-socket discovery on the NAS cannot
see containers inside those guests, and adding a separate log agent to each LXC
would add another privileged component. The Loki push API is simple and stable
(`POST /loki/api/v1/push` with JSON body). Channel-buffered delivery with silent
drop on overflow matches the logwriter's design priority (DNS availability over
log completeness).

**Implementation**: `lokiHandler` implements `slog.Handler`. `WithAttrs()` captures the `component` value when present — this is how each of the 12 component loggers gets its own Loki stream label. `lokiRun()` background goroutine reads from the shared channel, groups by component, and flushes with a single retry + 500ms backoff on failure. `initLoki()` wraps the default handler and re-creates all component loggers. `shutdownLoki(ctx)` closes the channel and waits for drain with timeout.

**Revisit if**: Alloy is deliberately deployed into each LXC, or if we need
more sophisticated retry/backpressure than "try once more then drop."
