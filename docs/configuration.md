# Configuration

Svart is configured in two places:

- **Environment variables** (or a `.env` file next to the binary) for things
  that must be known at startup: ports, paths, who may query, hardening. All
  of them are optional. An invalid value stops startup with a message naming
  the variable, rather than silently falling back to a default. The optional
  Analysis ranking file is validated when requested, as described below.
- **The web UI / API** for everything you change while running: upstreams,
  blocklists and allowlists, ranges, groups, clients, policies, rewrites,
  users, tokens, retention. These live in the database and replicate between
  nodes when [high availability](operations.md#high-availability) is on.

[`.env.example`](../.env.example) lists every variable with its default.

## Editing settings

The Config page keeps unsaved edits when a previous save refreshes its settings.
You can continue editing while a save is in progress. “Submitted changes saved”
confirms the values submitted with that click; later edits remain drafts until
you save again. A save confirms its submitted edits with a subsequent server read
and publishes that confirmed result immediately into the displayed settings;
older refreshes cannot acknowledge newer edits, even when their values match.
A failed or partially completed save preserves the draft for retry.

## Listeners and storage

| Variable | Default | Meaning |
|---|---|---|
| `DNS_PORT` | `5353` | UDP and TCP DNS listener. Use `53` for a bare-metal install (needs `CAP_NET_BIND_SERVICE`); containers publish host 53 to it. |
| `DNS_PUBLISHED_PORT` | `DNS_PORT` | The port devices actually use when a container maps it; only affects the setup page's advice. |
| `ADMIN_PORT` | `3000` | Web UI and API. |
| `EXTERNAL_IP` | detected | This host's LAN address: shown on the setup page, and used to record queries from `127.0.0.1` under the host's real address. Required in containers for either to be right. |
| `DB_PATH` | `./svart-dns.db` | SQLite database: configuration plus the last 8 days of queries. |
| `ARCHIVE_PATH` | `./archives` | Daily Parquet files holding older queries. |

## Analysis top sites

| Variable | Default | Meaning |
|---|---|---|
| `TOP_SITES_PATH` | unset | Operator-provided local ranking CSV used by Analysis → Top 1k sites and `GET /api/analysis/domains?source=top-sites`. |

Svart ships no ranking dataset and makes no network requests for this feature.
Obtain a dataset you are licensed to use, store it on your own infrastructure,
and point `TOP_SITES_PATH` at its local path. In a container, mount the containing directory
read-only and use the file’s path **inside** that directory. A single-file bind
mount keeps the original inode, so atomic replacement of that host file requires
recreating the container instead. Set the environment variable
before starting Svart. The file is read on demand, so replace it atomically to
update the ranking without restarting. Configure each node separately.

Use UTF-8 CSV with no header, one `rank,domain` record per line:

```csv
1,example.com
2,example.org
3,example.net
```

Ranks must be positive integers in strictly increasing order (gaps are allowed).
Domains must be ASCII hostnames with at least two labels; use punycode for
international names. Case and a trailing dot are normalized. Blank lines are
ignored. The complete source must fit within 64 MiB, with each record below
1024 bytes. These are rejection limits: an oversized or malformed file is
never accepted partially, even if the requested top count was already read.

The API returns up to the requested `limit` (1–10000, default 1000) in file
order after validating every row. Missing configuration, an unreadable or
invalid source returns **503** with an actionable error; the UI displays it
and retains the existing domain input. DNS serving and the other Analysis
sources remain available. Concurrent heavy Analysis requests may return 429.

## First administrator

| Variable | Default | Meaning |
|---|---|---|
| `ADMIN_USER` | `admin` | Username of the administrator created from `ADMIN_PASSWORD`. |
| `ADMIN_PASSWORD` | unset | When set and no administrator exists yet, creates one at startup (for automation). When unset, Svart prints a one-time setup token and the setup page creates the administrator. Never read again once an administrator exists. |

## Who may query

| Variable | Default | Meaning |
|---|---|---|
| `ALLOWED_CLIENTS` | loopback, `10/8`, `172.16/12`, `192.168/16`, `100.64/10`, link-local, `fc00::/7` | Comma-separated CIDRs. Anyone else is answered `REFUSED` before any work is done, so an exposed port 53 is not an open resolver. |
| `DNS_RATE_LIMIT_QPS` | `0` (off) | Per-client token bucket, per /24 (IPv4) or /56 (IPv6), burst 2× the rate. Over-limit UDP is dropped, TCP gets `REFUSED`. |
| `DNS_TCP_MAX_CONNS` | `1000` | Concurrent DNS-over-TCP connections. |
| `DNS_TCP_MAX_CONNS_PER_IP` | `32` | Concurrent TCP connections per client address. |

## Resolver behavior

| Variable | Default | Meaning |
|---|---|---|
| `DNS_CACHE_SIZE_MB` | `64` | Answer cache budget (about 140,000 answers). Least-recently-used answers are evicted beyond it. |
| `POLICY_CACHE_SIZE_MB` | `64` | Policy decision cache budget (about 100,000 client and name decisions). A miss re-evaluates the policy in a few microseconds, so raising it mostly costs memory. |
| `DNS_REBIND_PROTECTION` | `false` | Refuse answers that point a public name at a private, loopback or link-local address (DNS rebinding). Rewrites, `localhost`, `local`, `lan`, `home.arpa` and `internal` are exempt. |
| `DNS_REBIND_ALLOW_DOMAINS` | empty | More exemptions, comma-separated (for example `plex.direct`). |

Upstream resolvers are configured in the UI. Entries can be plain DNS
(`9.9.9.9`), DNS-over-TCP (`tcp://…`), DNS-over-TLS (`tls://dns.quad9.net`) or
DNS-over-HTTPS (`https://dns.quad9.net/dns-query`). Prefix an entry with
`[/lan/]` to send only `*.lan` there (conditional forwarding, for names your
router's DHCP server knows). With no upstream configured at all, Svart uses
Quad9 over DNS-over-HTTPS.

## Blocklist downloads

| Variable | Default | Meaning |
|---|---|---|
| `ALLOW_PRIVATE_LIST_URLS` | `false` | List URLs may not reach loopback, private, link-local or cloud-metadata addresses (server-side request forgery). Set to `true` to use a list server on your own network. |
| `LIST_MAX_DOWNLOAD_BYTES` | `268435456` | A list larger than this fails to refresh and the previous version stays active. |
| `LIST_MAX_LINES` | `5000000` | Same, by line count. |

### List syntax

Lists can be plain domains, hosts files (`0.0.0.0 ads.example.com`) or
AdGuard/Adblock Plus syntax. Plain domains and hosts entries retain Svart's
subtree matching. Explicit anchors and regexes follow their written scope.

| Rule | Blocks |
|---|---|
| `ads.example.com`, `\|\|ads.example.com^` | `ads.example.com` and its subdomains |
| `\|\|*.top^` | every name under `.top` |
| `\|\|glthub.` | `glthub.` followed by any labels, at any subdomain level (`glthub.com`, `cdn.glthub.net`) |
| `\|\|ads*.example^` | names starting `ads` and ending `.example`, at any subdomain level |
| `\|ads.example^`, `://ads.example^` | `ads.example` only (explicit hostname start and end) |
| `://ww4.` | names starting `ww4.` |
| `.example.com` | subdomains of `example.com`, not the name itself |
| `*shop.` | names containing `shop.` |
| `192.0.2.7`, `://[2001:db8::7]^` | answers containing that address |
| `@@\|\|nation.africa^` | nothing: an exception to *this list's* rules (other lists still block) |
| `\|\|*.africa^$denyallow=nation.africa` | `*.africa` except `nation.africa` and its subdomains |
| `\|\|example.com^$badfilter` | nothing: cancels this list's `\|\|example.com^` |

Additional supported rules include `/regex/` and `@@/regex/`,
`$dnstype=TXT`, `$dnstype=~CNAME`, and combinations such as
`|google.com|$dnstype=TXT,important`. List modifiers accept recognized DNS type
names; `ANY` means an actual ANY query, not all record types. Analysis and the
policy API additionally accept `TYPE<number>` for uncommon query types.
Mixed positive and negative type lists use the positive entries minus any
explicit exclusions. A set with every included type excluded is invalid. Questions use
their Qtype; response aliases use their actual CNAME/DNAME/SVCB/HTTPS type,
and returned addresses use A or AAAA. Cache decisions include the record type.

`$denyallow` accepts domain names, including their subdomains, and excludes
them from its own rule only. Wildcard exclusions reject the whole rule.
`$badfilter` cancels the same pattern and constraints within its own list;
modifier order and DNS type order do not change that identity.
Priority within a list is important exception, important block, ordinary
exception, ordinary block. These priorities do not bypass Svart Assignments
or an explicit allow. Browser modifiers such as `$all` and `$document` are
unsupported and cause the complete rule to be skipped.

Rules with unsupported modifiers are skipped in full, even exclusion-only
`$client=~...` or `$ctag=~...` restrictions. Unsupported features include client
and tag restrictions, browser contexts, address ranges, named URL schemes such
as `https://example.com/`, URL paths, and
`$dnsrewrite`. Go-compatible regexes are supported; recognized regexes that
fail compilation reject the refresh, preserving the previous list. Malformed
lines that are not recognized as complete regex rules are counted as invalid.

Filter Lists shows compatibility counts and complete rejected source rules
with reasons. Old downloaded generations remain usable but unassessed until a
successful refresh. See [list compatibility](list-compatibility.md) for API and
Analysis details.

## Web UI hardening

| Variable | Default | Meaning |
|---|---|---|
| `TLS_CERT`, `TLS_KEY` | unset | PEM certificate chain and key; serves the UI over HTTPS and reloads them when the files change. |
| `ADMIN_ALLOWED_HOSTS` | unset (any) | Comma-separated `host[:port]` values; requests for any other `Host` get 421. Hardens against DNS rebinding of the UI. |
| `TRUST_PROXY_HEADERS` | `false` | Trust `X-Forwarded-Proto`/`X-Forwarded-For` from a reverse proxy that terminates TLS. Only enable it when the proxy overwrites those headers. |

## Logs, metrics and profiling

| Variable | Default | Meaning |
|---|---|---|
| `LOG_FORMAT` | `text` | `text` or `json` (one object per line). |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `LOG_QUERIES` | `false` | Also write every DNS query to the log. The SQLite query log always has them; this is for pipelines that alert on log streams. |
| `LOKI_URL` | unset | Push logs directly to Grafana Loki (e.g. `http://loki:3100`). |
| `NODE_ID` | hostname | Instance name in logs, metrics and sync. |
| `METRICS_PER_CLIENT` | `false` | Per-client and per-group Prometheus series. Off by default because `/metrics` is unauthenticated and these reveal who is on the network. |
| `METRICS_CLIENT_LIMIT` | `256` | With per-client metrics on, clients beyond this (named ones first) are counted as `other`. |
| `PPROF_ADDR` | unset | Go runtime profiler on a loopback address, e.g. `127.0.0.1:6060`. Refuses non-loopback addresses. |

## High availability

| Variable | Default | Meaning |
|---|---|---|
| `PEERS` | unset | Comma-separated peer URLs (`https://svart-b.example.lan:3000`), seeding the peer list on first start. |
| `SYNC_PEER_ALLOWLIST` | unset | Exact set of peer origins (including this node) allowed to sync. Required for any sync. |
| `SYNC_SECRET` | unset | Shared secret, at least 32 random bytes, identical on every node. |
| `SYNC_INTERVAL` | `2s` | How often each peer is polled (1 s to 1 h). |
| `SYNC_TLS_SERVER_NAME` | unset | Certificate name to verify when peer URLs are IP addresses. |
| `TLS_CA` | unset | Extra CA for verifying peers' certificates (self-signed setups). |
| `SYNC_REPLICATE_IDENTITY` | `false` | Also replicate administrators and API tokens. Convenient, but it means any node holding the sync secret can create administrators on every node; read [DD-029](decisions/DD-029-per-node-certificates-mutual-tls-for-sync-proposed-identity-replicatio.md) first. |

Setup is described in [operations.md](operations.md#high-availability).
