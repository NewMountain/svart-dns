# DD-008: Blocklists auto-refresh with change tracking

**Decision**: Each blocklist has a configurable refresh interval (default 3-7 days). A background worker re-fetches stale lists automatically. On each refresh, the system diffs against the previous domain set and logs domains added/removed. Change history is persisted in a `blocklist_changelog` table.

**Why**: Blocklists are living documents — maintainers add and remove domains constantly. You want to know what changed, not just that "it refreshed." Tracking adds/removes lets you spot lists that are getting more aggressive (lots of adds) or abandoned (no changes). It also feeds into the blocklist intelligence features — you can correlate "list X added domain Y three days ago, and that's when client Z started getting blocked."

**Not a catalog**: We originally discussed shipping a curated JSON of well-known lists. That's not the right model. You add blocklist URLs via the UI/API (which already works), and the system maintains them. Discovery of new lists is a human activity, not something to hardcode.

Discovery uses the read pool, so downloading or storing a large list does not
hold up stale-list discovery behind the single writer. A discovery failure aborts
the cycle and exposes an unavailable (`NaN`) stale-list gauge, never a healthy
zero or a partial set of candidates.

Failed automatic refreshes retry after 1, 2, 4, 8, 16, 32, then at most 60 minutes.
The worker owns retry state; a restart retries promptly. A changed source URL or
successful manual refresh resets its backoff. Newly discovered empty lists are
eligible immediately, even when their remote timestamp is recent or the ordinary
refresh interval is disabled. Manual refresh remains available during backoff.

A local generation marker commits with each successful download, including an
empty or entirely unsupported list. Empty-list recovery applies only when this
node has neither local rules nor a successful generation for the current source
URL. Markers persist across restarts, are removed with their lists, and never
replicate: a new node still fetches lists whose metadata arrived through sync.
Changing the URL makes an empty list eligible again; a download that finished
against a superseded URL cannot replace the current generation.
