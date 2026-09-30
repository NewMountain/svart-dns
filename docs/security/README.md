# Security model

Svart sits on the most sensitive path in a home or small-office network: it
sees every name every device looks up, and it can send those devices
anywhere. This page says what it defends against and how, so the defaults can
be judged rather than trusted.

## Assets

- **The query history**: who looked up what, and when. Kept in SQLite (8
  days) and Parquet (up to the retention period).
- **Answers**: a poisoned or rewritten answer redirects every device.
- **The administrator's control**: whoever controls Svart controls the above.
- **The upstream resolvers' trust**: Svart must not become someone else's
  open resolver or amplifier.

## Adversaries and defaults

| Adversary | What they try | Default defense |
|---|---|---|
| Internet hosts reaching an exposed port 53 | use Svart as an open resolver or DDoS amplifier | Only private, CGNAT, loopback, link-local and ULA clients are answered (`ALLOWED_CLIENTS`); UDP answers are truncated to 512 bytes, or the client's EDNS size capped at 1232; `ANY` gets the minimal RFC 8482 answer; optional per-client rate limit |
| Off-path spoofers | poison the cache | Every upstream query is a fresh message with a random ID and Svart's own EDNS options; replies must match ID, opcode and question or are discarded; identical concurrent misses are coalesced; only complete NOERROR/NXDOMAIN answers are cached |
| On-path attackers between Svart and its upstreams | read or rewrite lookups | DNS-over-HTTPS/TLS with certificate verification against the configured hostname (bootstrap resolution only finds the address) |
| Trackers hiding behind CNAMEs | escape blocklists | Every CNAME/DNAME/SVCB target in an answer is checked against the client's policy |
| Web pages in a user's browser | rebind a name to the admin UI, or submit forms to it | Same-origin and JSON-only checks on every state-changing request; `SameSite=Strict`, `HttpOnly` sessions; CSP, `X-Frame-Options: DENY`; optional `ADMIN_ALLOWED_HOSTS`; optional DNS rebinding filter for everything else on the network |
| Anyone on the LAN | take over a fresh install, or guess passwords | No default password: the first administrator needs a one-time token printed in the server log; logins are rate limited per user and per address with exponential backoff, bcrypt work is capped, unknown usernames cost the same as known ones |
| A holder of a read-only API token | exhaust memory or CPU, or read secrets | Every request body is size-capped; analysis endpoints bound the work per request and run two at a time; read-only callers see list and upstream URLs with credentials and query strings redacted |
| An administrator's browser session being stolen | keep access | Sessions are random 256-bit server-side tokens stored hashed, expire after 7 days idle or 30 days, and end on logout, password change or role change |
| A malicious or compromised list publisher | reach internal services through Svart, or feed it garbage | List URLs may only reach public addresses (checked at connect time and on every redirect) unless `ALLOW_PRIVATE_LIST_URLS=true`; size and line limits; a list that fails parsing or limits keeps its previous version |
| An analyst using the SQL investigation page | read secrets from the database | Queries are validated on DuckDB's own parse tree: only `query_logs` and CTEs can be read, only allow-listed functions called, no file or extension access; the application database is not attached |
| A compromised peer (high availability) | take over every node | Peers need TLS with verified certificates, an exact origin allowlist and a 32-byte shared secret; incoming rows are validated like API input and refused if timestamped more than 5 minutes ahead; administrators and API tokens do not replicate unless `SYNC_REPLICATE_IDENTITY=true` |
| Someone reading `/metrics` (unauthenticated) | learn who is on the network | Per-client and per-group series are off unless `METRICS_PER_CLIENT=true`; upstream labels drop URL paths (DoH account IDs) and credentials |

## Not defended (yet)

- **A shared sync secret proves membership, not identity.** Any node with the
  secret can impersonate any other. Per-node certificates with mutual TLS are
  the planned fix
  ([DD-029](../decisions/DD-029-per-node-certificates-mutual-tls-for-sync-proposed-identity-replicatio.md)).
- **The web UI is HTTP unless you set `TLS_CERT`/`TLS_KEY`** or put it behind a
  TLS-terminating proxy. On an untrusted LAN, do one of those.
- **DNSSEC is not validated by Svart.** It forwards the DO bit and relies on
  the upstream resolver's validation, which the encrypted upstream transport
  protects in transit.
- **Query history is readable by every administrator.** That is the product;
  restrict who is an administrator, and use retention to limit how far back
  it goes.

## Reviews

- [2026-09 pre-release review](2026-09-review.md): three adversarial reviews
  (DNS protocol; authentication and API; sync, SQL and supply chain) and the
  benchmark harness found 38 issues. Each fix landed with a regression test
  that failed before it; the review lists the status of every finding.
