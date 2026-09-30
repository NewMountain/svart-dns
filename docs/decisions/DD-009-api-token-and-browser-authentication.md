# DD-009: API-token and browser authentication

**Status**: Superseded and expanded by the React SPA implementation.

**Historical decision**: The placeholder Go-template UI used one shared
environment password while automation used bcrypt-hashed readonly/admin API
tokens.

**Current decision**: Browser authentication is DB-backed and multi-user.
`admin_users` stores a unique username, bcrypt password hash, and
readonly/admin role; the hash is never serialized. The SPA uses an HttpOnly
`svart_session` cookie signed by a random DB-persisted secret that survives
deploys and can be rotated to revoke every browser session. API tokens remain
independent show-once automation credentials accepted through `X-Api-Key`.

An empty database seeds its first browser user only when `ADMIN_PASSWORD` is
explicitly nonempty. Missing or empty bootstrap credentials create no account
and leave protected endpoints locked. `/health`, `/metrics`, documentation,
login, auth-check, and static documentation assets are public; application APIs
require a valid token or session. Replication is a separate `X-Sync-Key`
boundary and never accepts an admin token or browser session as a substitute.

## Bounded token verification (2026-09-26)

Cold API-token validation shares the four-slot bcrypt concurrency limit with
browser login, and admits at most four cache misses per one-second window
across the process. It never queues behind busy bcrypt work. Rejected requests
receive HTTP 429 with `Retry-After: 1` and a credential-free error. Valid cached
tokens bypass these limits; browser sessions keep their existing path.
The five-minute successful-token cache remains revoked immediately on token
deletion or identity refresh. An in-flight comparison cannot repopulate a
cache invalidated by a credential change.

Token metadata and hashes are read completely and the SQLite rows are closed
before bcrypt runs, leaving the sole write-pool connection available for
administration. Database failures produce HTTP 503 rather than pretending
the supplied credential is invalid. No token values or hashes are logged.

Regression coverage includes shared-slot saturation, exact window boundaries,
and 32 concurrent HTTP wrong-suffix requests against a real bcrypt hash.
The concurrent test requires exactly four 401 responses and 28 immediate 429
responses while database writes and ten real UDP DNS requests finish within
200 ms. These are isolated-host regression budgets, not a production capacity
claim.
