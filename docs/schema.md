# API Schema Reference

Complete TypeScript types and JSON examples for every svart-dns API endpoint.
Intended as a contract for frontend development.

## Response Envelope

Every JSON API response, including `/health`, uses this envelope. `/metrics` and documentation/static responses use their native formats:

```typescript
interface ApiResponse<T> {
  data: T | null;
  error: string | null;
}
```

```json
// Success
{ "data": { "id": 1, "alias": "Hagezi Pro" }, "error": null }

// Error
{ "data": null, "error": "blocklist not found" }
```

## Authentication

Protected application APIs accept either a DB-backed browser session cookie or
an `X-Api-Key`. Three middleware tiers apply the authenticated principal's
readonly/admin role:

| Middleware | Behavior |
|---|---|
| `apiAuth` | GET/HEAD = readonly principal OK; mutations require an admin principal |
| `readonlyAuth` | Any valid readonly/admin principal passes, all methods |
| `adminAuth` | Admin principal required, all methods |

Public endpoints (no auth): `/health`, `/metrics`, `/docs`,
`/docs/swagger.json`, `/static/rapidoc-min.js`, `/api/auth/login`,
`/api/auth/check`, and `/api/auth/logout`. The SPA shell and hashed assets are
also served publicly; its data requests remain protected.

Browser users live in `admin_users` with a unique username, bcrypt password
hash, and readonly/admin role. Password hashes are never serialized. The
`svart_session` cookie is HttpOnly, SameSite=Lax, secure on direct HTTPS (or an
explicitly trusted HTTPS proxy), valid for 30 days, and signed by a random
DB-persisted secret. Rotating that secret revokes every browser session.

An empty database seeds its first administrator only when `ADMIN_PASSWORD` is
explicitly nonempty; there is no fallback password. If bootstrap credentials
are absent, browser login returns unavailable and protected endpoints return
401 until an administrator is created. API tokens work independently of
whether a browser user exists.

Replication has a separate trust boundary: `GET /api/sync` requires the shared `X-Sync-Key` and does not accept an admin API token or session as fallback. Its settings payload omits `sync_secret` and `session_secret`; receivers also ignore either key defensively. `POST /api/sync/pair/complete` is authenticated by the short-lived pairing code plus a domain-separated HMAC-SHA256 proof. Pairing verifies possession of the same secret in both directions without ever transmitting or returning the raw secret.

---

## Types

### Core Entities

```typescript
interface Upstream {
  id: number;
  upstream: string;       // "tls://dns.quad9.net", "https://cloudflare-dns.com/dns-query"
  enabled: boolean;
}

interface Client {
  ip_address: string;     // "10.42.1.42"
  alias: string;          // "Sam Phone" or ""
  query_count: number;
  last_seen: string;      // "2026-03-04 10:30:00"
  is_member: boolean;
}

interface Group {
  id: number;
  name: string;
  member_count: number;
  blocklist_count: number;
  is_member: boolean;
}

interface Blocklist {
  id: number;
  url: string;            // URL or "" for manual lists
  alias: string;          // "Hagezi Pro"
  enabled: boolean;
  domain_count: number;
  last_updated: string;   // "2026-03-01 12:00:00" or ""
}

interface Allowlist {
  id: number;
  url: string;
  alias: string;
  enabled: boolean;
  domain_count: number;
  last_updated: string;
}

interface RewriteView {
  id: number;
  domain: string;         // "myapp.local"
  ip_addresses: string;   // "10.0.0.100" (comma-separated if multiple)
  enabled: boolean;
}

interface IPRange {
  id: number;
  name: string;           // "IOT Jail"
  cidr: string;           // "10.42.2.0/24"
  created_at: string;
}

interface BootstrapServer {
  id: number;
  server: string;         // "9.9.9.9:53"
}

interface APIToken {
  id: number;
  name: string;           // "grafana"
  token_prefix: string;   // first 8 chars: "a1b2c3d4"
  role: string;           // "readonly" | "admin"
  created_at: string;
  last_used_at?: string;  // omitted if never used
}
```

### Policy Engine Types

```typescript
interface PolicyResult {
  client_ip: string;
  domain: string;
  result: "block" | "allow" | "";
  result_source?: EntityResult;
  range_evaluation?: TierEvaluation;
  group_evaluation?: TierEvaluation;
  ip_evaluation?: TierEvaluation;
}

interface TierEvaluation {
  entities: EntityResult[];
  result: "block" | "allow" | "";
  result_source?: EntityResult;
}

interface EntityResult {
  tier: "range" | "group" | "ip";
  name: string;           // entity name: "IOT Jail", "Kids Devices", "10.42.1.42"
  result: "block" | "allow" | "";
  published_list?: PublishedHit;
  custom_rule?: CustomHit;
}

interface PublishedHit {
  action: "block" | "allow";
  rule: string;           // "tiktok.com", "*.doubleclick.net"
  list_id: number;
  list_name: string;      // "Hagezi Pro"
}

interface CustomHit {
  action: "block" | "allow";
  rule: string;
}
```

### Analysis Types

```typescript
interface MatrixListInfo {
  id: number;
  alias: string;
  domain_count: number;
}

interface MatrixResponse {
  lists: MatrixListInfo[];          // sorted smallest → largest by domain_count
  domains: string[];                // normalized input domains
  matrix: boolean[][];              // [domain_idx][list_idx]
  matched_rules: (string | null)[][]; // [domain_idx][list_idx], null if no match
}

interface CompareListInfo {
  id: number;
  alias: string;
  domain_count: number;
}

interface CompareResponse {
  lists: CompareListInfo[];
  overlap: number[][];              // [i][j] = shared domain count, diagonal = self
  unique_count: number[];           // domains only in that list
  union_size: number;
}

interface SimulateResponse {
  client_ip: string;
  results: PolicyResult[];          // one per input domain
}

interface DomainsResponse {
  source: "top-sites" | "top-queried" | "top-blocked" | "top-allowed";
  count: number;
  domains: string[];
}
```

### Query Log Types

```typescript
interface QueryLogEntry {
  id: number;
  timestamp: string;
  client_ip: string;
  client_alias?: string;            // only if alias exists
  query_name: string;               // "google.com."
  query_type: string;               // "A", "AAAA", "CNAME", "MX", etc.
  response_code: string;            // "NOERROR", "NXDOMAIN", "SERVFAIL"
  blocked: boolean;
  upstream: string;                 // "tls://dns.quad9.net"
  latency_microseconds: number;
  block_tier?: string;              // "range" | "group" | "ip" — only if blocked
  block_rule?: string;              // only if blocked
  block_source?: string;            // "published" | "custom" — only if blocked
  block_list_id?: number;           // only if blocked by published list
  block_list_name?: string;         // only if blocked by published list
}

interface QueryLogResponse {
  logs: QueryLogEntry[];
  total: number;
  limit: number;
  offset: number;
}
```

### Config Export/Import Types

```typescript
interface ConfigExport {
  version: string;                  // "1.0"
  exported_at: string;              // RFC3339
  upstreams: UpstreamExport[];
  blocklists: ListExport[];
  allowlists: ListExport[];
  rewrites: RewriteExport[];
  groups: GroupExport[];
  ranges: RangeExport[];
  settings: Record<string, string>; // excludes sync_secret and session_secret
  bootstrap_servers: string[];
  clients: ClientExport[];
}

interface UpstreamExport {
  upstream: string;
  enabled: boolean;
}

interface ListExport {
  url: string;
  alias: string;
  enabled: boolean;
}

interface RewriteExport {
  domain: string;
  ip_addresses: string;
  enabled: boolean;
}

interface GroupExport {
  name: string;
  members: string[];                // client IPs
  blocklists: string[];             // by alias
  allowlists: string[];             // by alias
}

interface RangeExport {
  name: string;
  cidr: string;
  blocklists: string[];             // by alias
  allowlists: string[];             // by alias
}

interface ClientExport {
  ip: string;
  alias?: string;
  groups?: string[];                // by group name
  blocklists?: string[];            // by alias
  allowlists?: string[];            // by alias
}
```

### Health & Stats Types

```typescript
interface HealthResponse {
  status: "ok";
}

interface StatsResponse {
  total_queries: number;
  blocked_queries: number;
  avg_latency_microseconds: number;
  uptime_seconds: number;
  cache: {
    hits: number;
    misses: number;
    hit_rate: number;
    entries: number;
  };
}
```

### Peer Types

```typescript
interface PeersResponse {
  node_id: string;
  node_name: string;
  sync_interval: string;
  has_secret: boolean;
  tls_configured: boolean;
  sync_tls_ready: boolean;
  sync_tls_error?: string;
  peers: PeerDetail[];
}

interface PeerDetail {
  url: string;
  node_name?: string;
  healthy: boolean;
  last_sync_at: string;
  changes_24h: number;
  consecutive_errors: number;
  last_error?: string;
}
```

### Archive Types

```typescript
interface ArchiveStatus {
  archive_path: string;
  archive_age_days: number;
  oldest_data_days: number;
  live_rows: number;
  archive_files: ArchiveFile[];
}

interface ArchiveFile {
  name: string;                     // "svart-dns-2024-01-31.parquet"
  size_mb: number;
  modified: string;                 // RFC3339
}
```

---

## Endpoints

### Public (No Auth)

#### GET /health

Returns only public liveness. Node identity, ports, upstream, cache, log-writer, and peer state require an authenticated API; the frontend obtains `node_name` from `/api/peers` after authentication.

```json
{
  "data": {
    "status": "ok"
  },
  "error": null
}
```

#### GET /metrics

Prometheus exposition format (plain text). Not JSON. Sync-series peer labels are opaque (`peer-1`, `peer-2`, ...) and do not disclose peer URLs.

#### POST /api/auth/login

Accepts `{ "username": "...", "password": "..." }`. On success, sets the
HttpOnly `svart_session` cookie and returns `authenticated`, `role`, and
`username`. Invalid credentials return 401. An installation with no configured
browser users returns 503 instead of opening protected APIs.

#### GET /api/auth/check

Returns the authenticated role and username for a valid API token or session;
otherwise returns `authenticated: false`. It never returns a hash, cookie
secret, or token.

#### POST /api/auth/logout

Expires the caller's `svart_session` cookie. API tokens are unaffected.

---

### Dashboard & Stats

#### GET /api/stats

**Auth:** `apiAuth` (readonly)

```json
{
  "data": {
    "total_queries": 154320,
    "blocked_queries": 21500,
    "avg_latency_microseconds": 450,
    "uptime_seconds": 86400,
    "cache": {
      "hits": 120000,
      "misses": 34320,
      "hit_rate": 0.778,
      "entries": 14832
    }
  },
  "error": null
}
```

---

### Upstreams

#### GET /api/upstreams

**Auth:** `apiAuth` (readonly)

```json
{
  "data": [
    { "id": 1, "upstream": "tls://dns.quad9.net", "enabled": true },
    { "id": 2, "upstream": "https://cloudflare-dns.com/dns-query", "enabled": true },
    { "id": 3, "upstream": "tls://dns.mullvad.net", "enabled": false }
  ],
  "error": null
}
```

#### POST /api/upstreams

**Auth:** `apiAuth` (admin) | **Status:** 201

```typescript
// Request
{ upstream: string; enabled: boolean }
```

```json
{ "data": { "id": 4, "upstream": "tls://dns.mullvad.net", "enabled": true }, "error": null }
```

#### POST /api/upstreams/:id/toggle

**Auth:** `apiAuth` (admin)

```json
{ "data": { "success": true }, "error": null }
```

#### PUT /api/upstreams/:id

**Auth:** `apiAuth` (admin)

```typescript
// Request
{ upstream: string }
```

```json
{ "data": { "success": true }, "error": null }
```

#### DELETE /api/upstreams/:id

**Auth:** `apiAuth` (admin)

```json
{ "data": { "success": true }, "error": null }
```

---

### Blocklists

#### GET /api/blocklists

**Auth:** `apiAuth` (readonly)

```json
{
  "data": [
    {
      "id": 1,
      "url": "https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/hosts/pro.txt",
      "alias": "Hagezi Pro",
      "enabled": true,
      "domain_count": 245000,
      "last_updated": "2026-03-04 06:00:00"
    },
    {
      "id": 2,
      "url": "",
      "alias": "Manual (10.42.1.42)",
      "enabled": true,
      "domain_count": 3,
      "last_updated": ""
    }
  ],
  "error": null
}
```

#### POST /api/blocklists

**Auth:** `apiAuth` (admin) | **Status:** 201

```typescript
// Request
{ url: string; alias: string; enabled: boolean }
```

```json
{ "data": { "id": 3, "alias": "OISD Big" }, "error": null }
```

#### POST /api/blocklists/:id/toggle

**Auth:** `apiAuth` (admin)

```json
{ "data": { "success": true }, "error": null }
```

#### POST /api/blocklists/:id/refresh

**Auth:** `apiAuth` (admin) — triggers async re-fetch of list domains

```json
{ "data": { "success": true }, "error": null }
```

#### PUT /api/blocklists/:id

**Auth:** `apiAuth` (admin)

```typescript
// Request (all optional)
{ url?: string; alias?: string }
```

```json
{ "data": { "success": true }, "error": null }
```

#### DELETE /api/blocklists/:id

**Auth:** `apiAuth` (admin)

```json
{ "data": { "success": true }, "error": null }
```

#### GET /api/blocklists/:id/domains

**Auth:** `apiAuth` (readonly) — paginated domain listing for a blocklist

| Param | Type | Default | Description |
|---|---|---|---|
| `limit` | int | 1000 | 1-10000 |
| `offset` | int | 0 | Pagination offset |

```json
{
  "data": {
    "blocklist_id": 1,
    "domains": ["0.0.0.0.example.com", "1-click.spam.net", "..."],
    "total": 245000,
    "limit": 1000,
    "offset": 0
  },
  "error": null
}
```

---

### Allowlists

Same shape as blocklists. All endpoints mirror the blocklist pattern.

#### GET /api/allowlists/:id/domains

**Auth:** `apiAuth` (readonly) — paginated domain listing for an allowlist. Same shape as blocklist domains but with `allowlist_id` key.

#### GET /api/allowlists

**Auth:** `apiAuth` (readonly) — same shape as `GET /api/blocklists`

#### POST /api/allowlists

**Auth:** `apiAuth` (admin) | **Status:** 201

```typescript
// Request
{ url: string; alias: string; enabled: boolean }
```

```json
{ "data": { "id": 1, "alias": "My Allowlist" }, "error": null }
```

#### POST /api/allowlists/:id/toggle | POST /api/allowlists/:id/refresh | PUT /api/allowlists/:id | DELETE /api/allowlists/:id

Same patterns as blocklist equivalents.

---

### Rewrites (Custom DNS)

#### GET /api/rewrites

**Auth:** `apiAuth` (readonly)

```json
{
  "data": [
    { "id": 1, "domain": "plex.home", "ip_addresses": "10.42.1.50", "enabled": true },
    { "id": 2, "domain": "nas.home", "ip_addresses": "10.42.1.10,fd00::10", "enabled": true }
  ],
  "error": null
}

#### POST /api/rewrites

**Auth:** `apiAuth` (admin) | **Status:** 201

```typescript
// Request
{ domain: string; ip_addresses: string; enabled: boolean }
```

```json
{
  "data": { "id": 3, "domain": "app.home", "ip_addresses": "10.42.1.100", "enabled": true },
  "error": null
}
```

#### PUT /api/rewrites/:id

**Auth:** `apiAuth` (admin)

```typescript
// Request (all optional — enabled takes priority)
{ domain?: string; ip_addresses?: string; enabled?: boolean }
```

#### DELETE /api/rewrites/:id

**Auth:** `apiAuth` (admin)

```json
{ "data": { "success": true }, "error": null }
```

#### POST /api/rewrites/batch

**Auth:** `adminAuth` | **Status:** 201

```typescript
// Request (max 1000)
{ rewrites: Array<{ domain: string; ip_addresses: string; enabled: boolean }> }
```

```json
{
  "data": [
    { "id": 3, "domain": "app1.home", "ip_addresses": "10.42.1.100", "enabled": true },
    { "id": 4, "domain": "app2.home", "ip_addresses": "10.42.1.101", "enabled": true }
  ],
  "error": null
}
```

---

### IP Ranges

#### GET /api/ranges

**Auth:** `apiAuth` (readonly)

```json
{
  "data": [
    { "id": 1, "name": "IOT Jail", "cidr": "10.42.2.0/24", "created_at": "2026-01-15 08:00:00" },
    { "id": 2, "name": "Guest WiFi", "cidr": "10.42.3.0/24", "created_at": "2026-02-01 12:00:00" }
  ],
  "error": null
}
```

#### POST /api/ranges

**Auth:** `apiAuth` (admin) | **Status:** 201

```typescript
// Request
{ name: string; cidr: string }
```

```json
{ "data": { "id": 3 }, "error": null }
```

#### GET /api/ranges/:id

**Auth:** `apiAuth` (readonly) — full range detail with assigned lists

```json
{
  "data": {
    "id": 1,
    "name": "IOT Jail",
    "cidr": "10.42.2.0/24",
    "created_at": "2026-01-15 08:00:00",
    "blocklists": [
      { "id": 1, "alias": "Hagezi Pro", "domain_count": 245000, "is_assigned": false, "source": "range" }
    ],
    "allowlists": []
  },
  "error": null
}
```

#### PUT /api/ranges/:id

**Auth:** `apiAuth` (admin)

```typescript
// Request (all optional)
{ name?: string; cidr?: string }
```

#### DELETE /api/ranges/:id

**Auth:** `apiAuth` (admin)

#### POST /api/ranges/:id/blocklists/:blocklistId | DELETE /api/ranges/:id/blocklists/:blocklistId

**Auth:** `apiAuth` (admin) — assign/unassign a blocklist to a range

#### POST /api/ranges/:id/allowlists/:allowlistId | DELETE /api/ranges/:id/allowlists/:allowlistId

**Auth:** `apiAuth` (admin) — assign/unassign an allowlist to a range

All return `{ "data": { "success": true }, "error": null }`.

---

### Groups

#### GET /api/groups

**Auth:** `apiAuth` (readonly)

```json
{
  "data": [
    { "id": 1, "name": "Kids Devices", "member_count": 3, "blocklist_count": 2, "is_member": false },
    { "id": 2, "name": "Work Laptops", "member_count": 2, "blocklist_count": 1, "is_member": false }
  ],
  "error": null
}

#### GET /api/groups/:id

**Auth:** `apiAuth` (readonly) — full group detail with members, lists, custom rules

```json
{
  "data": {
    "id": 1,
    "name": "Kids Devices",
    "members": [
      { "ip_address": "10.42.1.42", "alias": "Sam Phone", "query_count": 15432, "last_seen": "2026-03-04 10:30" },
      { "ip_address": "10.42.1.43", "alias": "Sophie iPad", "query_count": 8200, "last_seen": "2026-03-04 10:29" }
    ],
    "blocklists": [
      { "id": 1, "alias": "Hagezi Pro", "domain_count": 245000, "is_assigned": false, "source": "group" }
    ],
    "custom_blocked": ["tiktok.com", "*.snapchat.com"],
    "custom_allowed": ["youtube.com"]
  },
  "error": null
}
```

#### POST /api/groups

**Auth:** `apiAuth` (admin) | **Status:** 201

```typescript
// Request
{ name: string }
```

```json
{ "data": { "id": 1, "name": "Kids Devices" }, "error": null }
```

#### PUT /api/groups/:id

**Auth:** `apiAuth` (admin)

```typescript
// Request
{ name: string }
```

#### DELETE /api/groups/:id

**Auth:** `apiAuth` (admin) — cascades: removes members, list assignments

#### POST /api/groups/:id/members/:clientIP | DELETE /api/groups/:id/members/:clientIP

**Auth:** `apiAuth` (admin) — add/remove client from group

#### POST /api/groups/:id/members/batch

**Auth:** `apiAuth` (admin)

```typescript
// Request
{ client_ips: string[] }
```

```json
{ "data": { "added": 3 }, "error": null }
```

#### POST /api/groups/:id/blocklists/:blocklistId | DELETE /api/groups/:id/blocklists/:blocklistId

**Auth:** `apiAuth` (admin) — assign/unassign blocklist

#### POST /api/groups/:id/blocklists/batch

**Auth:** `apiAuth` (admin)

```typescript
// Request
{ blocklist_ids: number[] }
```

```json
{ "data": { "assigned": 3 }, "error": null }
```

#### POST /api/groups/:id/allowlists/:allowlistId | DELETE /api/groups/:id/allowlists/:allowlistId

**Auth:** `apiAuth` (admin) — assign/unassign allowlist

All simple assign/unassign endpoints return `{ "data": { "success": true }, "error": null }`.

---

### Custom Domain Rules (Per-Client and Per-Group)

These add/remove individual domains to a client's or group's manual block/allow list.

#### POST /api/clients/:ip/block-domain

**Auth:** `apiAuth` (admin)

```typescript
// Request
{ domain: string }
```

```json
{ "data": { "success": true }, "error": null }
```

**Error 409:** `{ "data": null, "error": "conflicts with custom allow rule: youtube.com" }`

#### DELETE /api/clients/:ip/block-domain/:domain

**Auth:** `apiAuth` (admin)

#### POST /api/clients/:ip/allow-domain

Same pattern. **Error 409** if conflicts with a block rule.

#### DELETE /api/clients/:ip/allow-domain/:domain

Same pattern.

#### POST /api/groups/:id/block-domain | DELETE /api/groups/:id/block-domain/:domain

Same pattern as client, scoped to group.

#### POST /api/groups/:id/allow-domain | DELETE /api/groups/:id/allow-domain/:domain

Same pattern as client, scoped to group.

---

### Clients

#### GET /api/clients

**Auth:** `apiAuth` (readonly)

```json
{
  "data": [
    { "ip_address": "10.42.1.42", "alias": "Sam Phone", "query_count": 15432, "last_seen": "2026-03-04 10:30:00", "is_member": false },
    { "ip_address": "10.42.1.50", "alias": "", "query_count": 8200, "last_seen": "2026-03-04 10:29:00", "is_member": false }
  ],
  "error": null
}

#### GET /api/clients/:ip

**Auth:** `apiAuth` (readonly) — full client detail with groups, lists, custom rules

```json
{
  "data": {
    "ip": "10.42.1.42",
    "alias": "Sam Phone",
    "total_queries": 15432,
    "avg_latency_microseconds": 450,
    "first_seen": "2026-01-15 08:00:00",
    "last_seen": "2026-03-04 10:30:00",
    "groups": [
      { "id": 1, "name": "Kids Devices", "member_count": 0, "blocklist_count": 0, "is_member": true }
    ],
    "blocklists": [
      { "id": 1, "alias": "Hagezi Pro", "domain_count": 245000, "is_assigned": false, "source": "group" }
    ],
    "custom_blocked": ["tiktok.com"],
    "custom_allowed": ["youtube.com"]
  },
  "error": null
}
```

#### PUT /api/clients/:ip/alias

**Auth:** `apiAuth` (admin)

```typescript
// Request
{ alias: string }
```

#### POST /api/clients/:ip/groups/:groupId | DELETE /api/clients/:ip/groups/:groupId

**Auth:** `apiAuth` (admin) — add/remove client from group

#### POST /api/clients/:ip/blocklists/:blocklistId | DELETE /api/clients/:ip/blocklists/:blocklistId

**Auth:** `apiAuth` (admin) — assign/unassign blocklist directly to client (IP tier)

#### POST /api/clients/:ip/allowlists/:allowlistId | DELETE /api/clients/:ip/allowlists/:allowlistId

**Auth:** `apiAuth` (admin)

---

### Settings

#### GET /api/settings

**Auth:** `apiAuth` (readonly)

`sync_secret` and the internally managed `session_secret` are omitted for every role. Other sensitive settings are also omitted for readonly callers.

```json
{
  "data": {
    "cache_ttl": "3600",
    "bootstrap_ttl": "86400",
    "log_retention_days": "1095",
    "strategy": "weighted"
  },
  "error": null
}
```

#### PUT /api/settings/:key

**Auth:** `apiAuth` (admin)

```typescript
// Request
{ value: string }
```

```json
{ "data": { "key": "cache_ttl", "value": "7200" }, "error": null }
```

`sync_secret` accepts only a replacement of at least 32 bytes and returns acknowledgement without the submitted value:

```json
{ "data": { "key": "sync_secret", "updated": true }, "error": null }
```

A `sync_secret` shorter than 32 bytes is rejected with 400; legacy weak DB values and weak environment seeds are not activated, and `/api/peers.has_secret` stays false. `session_secret` is internally managed and direct updates are rejected with 400. `sync_interval` accepts 1 second through 1 hour; an empty stored/imported value means the safe 2-second default. The API, environment seed, config import, replication merge, and startup path all apply the same bound so an invalid peer value cannot panic a ticker.

---

### Bootstrap Servers

#### GET /api/bootstrap

**Auth:** `apiAuth` (readonly)

```json
{
  "data": [
    { "id": 1, "server": "9.9.9.9:53" },
    { "id": 2, "server": "1.1.1.1:53" }
  ],
  "error": null
}
```

#### POST /api/bootstrap

**Auth:** `apiAuth` (admin)

```typescript
// Request
{ server: string }
```

#### PUT /api/bootstrap

**Auth:** `apiAuth` (admin) — replaces all bootstrap servers

```typescript
// Request
{ servers: string[] }
```

---

### Query Logs

#### GET /api/query-logs

**Auth:** `apiAuth` (readonly)

| Param | Type | Default | Description |
|---|---|---|---|
| `limit` | int | 100 | 1-1000 |
| `offset` | int | 0 | Pagination offset |
| `client_ip` | string | — | Exact match |
| `domain` | string | — | Substring match (LIKE) |
| `blocked` | string | — | `"true"` / `"false"` |
| `query_type` | string | — | `"A"`, `"AAAA"`, `"CNAME"`, etc. |
| `group_id` | int | — | Filter by group membership (resolves to member IPs) |
| `range_id` | int | — | Filter by IP range (resolves CIDR to matching client IPs) |

```json
{
  "data": {
    "logs": [
      {
        "id": 98765,
        "timestamp": "2026-03-04 10:30:01",
        "client_ip": "10.42.1.42",
        "client_alias": "Sam Phone",
        "query_name": "tiktok.com.",
        "query_type": "A",
        "response_code": "NXDOMAIN",
        "blocked": true,
        "upstream": "",
        "latency_microseconds": 45,
        "block_tier": "group",
        "block_rule": "tiktok.com",
        "block_source": "published",
        "block_list_id": 1,
        "block_list_name": "Hagezi Pro"
      },
      {
        "id": 98764,
        "timestamp": "2026-03-04 10:30:00",
        "client_ip": "10.42.1.42",
        "query_name": "google.com.",
        "query_type": "A",
        "response_code": "NOERROR",
        "blocked": false,
        "upstream": "tls://dns.quad9.net",
        "latency_microseconds": 1200
      }
    ],
    "total": 154320,
    "limit": 100,
    "offset": 0
  },
  "error": null
}
```

> **Note:** `client_alias`, `block_tier`, `block_rule`, `block_source`, `block_list_id`, `block_list_name` are conditionally included — absent when not applicable.

---

### Policy Evaluation

#### GET /api/policy/evaluate

**Auth:** `readonlyAuth`

| Param | Type | Required | Description |
|---|---|---|---|
| `client_ip` | string | yes | Client IP to evaluate |
| `domain` | string | yes | Domain to check |

```json
{
  "data": {
    "client_ip": "10.42.1.42",
    "domain": "tiktok.com",
    "result": "block",
    "result_source": {
      "tier": "group",
      "name": "Kids Devices",
      "result": "block",
      "published_list": {
        "action": "block",
        "rule": "tiktok.com",
        "list_id": 1,
        "list_name": "Hagezi Pro"
      }
    },
    "range_evaluation": {
      "entities": [
        {
          "tier": "range",
          "name": "Home LAN",
          "result": "",
          "published_list": null,
          "custom_rule": null
        }
      ],
      "result": "",
      "result_source": null
    },
    "group_evaluation": {
      "entities": [
        {
          "tier": "group",
          "name": "Kids Devices",
          "result": "block",
          "published_list": {
            "action": "block",
            "rule": "tiktok.com",
            "list_id": 1,
            "list_name": "Hagezi Pro"
          }
        }
      ],
      "result": "block",
      "result_source": {
        "tier": "group",
        "name": "Kids Devices",
        "result": "block",
        "published_list": {
          "action": "block",
          "rule": "tiktok.com",
          "list_id": 1,
          "list_name": "Hagezi Pro"
        }
      }
    },
    "ip_evaluation": null
  },
  "error": null
}
```

---

### Analysis

#### POST /api/analysis/matrix

**Auth:** `readonlyAuth`

The "blocklist FOMO solver." Rows = domains, columns = published lists (sorted smallest to largest). Shows which lists block which domains.

```typescript
// Request
{
  domains: string[];           // max 10,000
  blocklist_ids?: number[];    // optional, defaults to all enabled
}
```

```json
{
  "data": {
    "lists": [
      { "id": 3, "alias": "Hagezi Light", "domain_count": 45000 },
      { "id": 1, "alias": "Hagezi Pro", "domain_count": 245000 },
      { "id": 2, "alias": "OISD Big", "domain_count": 310000 }
    ],
    "domains": ["google.com", "tiktok.com", "ads.doubleclick.net"],
    "matrix": [
      [false, false, false],
      [false, true, true],
      [true, true, true]
    ],
    "matched_rules": [
      [null, null, null],
      [null, "tiktok.com", "tiktok.com"],
      ["doubleclick.net", "doubleclick.net", "ads.doubleclick.net"]
    ]
  },
  "error": null
}
```

#### POST /api/analysis/compare

**Auth:** `readonlyAuth`

Pairwise overlap between blocklists. How redundant are your lists?

```typescript
// Request
{ blocklist_ids: number[] }    // minimum 2
```

```json
{
  "data": {
    "lists": [
      { "id": 1, "alias": "Hagezi Pro", "domain_count": 245000 },
      { "id": 2, "alias": "OISD Big", "domain_count": 310000 }
    ],
    "overlap": [
      [245000, 180000],
      [180000, 310000]
    ],
    "unique_count": [65000, 130000],
    "union_size": 375000
  },
  "error": null
}
```

#### POST /api/analysis/simulate

**Auth:** `readonlyAuth`

Full three-tier policy simulation for a client. The "what would happen" tool.

```typescript
// Request
{
  client_ip: string;           // required
  domains: string[];           // max 10,000
}
```

```json
{
  "data": {
    "client_ip": "10.42.1.42",
    "results": [
      {
        "client_ip": "10.42.1.42",
        "domain": "tiktok.com",
        "result": "block",
        "result_source": { "tier": "group", "name": "Kids Devices", "result": "block", "published_list": { "action": "block", "rule": "tiktok.com", "list_id": 1, "list_name": "Hagezi Pro" } },
        "group_evaluation": { "entities": [{ "tier": "group", "name": "Kids Devices", "result": "block", "published_list": { "action": "block", "rule": "tiktok.com", "list_id": 1, "list_name": "Hagezi Pro" } }], "result": "block", "result_source": { "tier": "group", "name": "Kids Devices", "result": "block", "published_list": { "action": "block", "rule": "tiktok.com", "list_id": 1, "list_name": "Hagezi Pro" } } }
      },
      {
        "client_ip": "10.42.1.42",
        "domain": "google.com",
        "result": "",
        "group_evaluation": { "entities": [{ "tier": "group", "name": "Kids Devices", "result": "" }], "result": "" }
      }
    ]
  },
  "error": null
}
```

#### GET /api/analysis/domains

**Auth:** `readonlyAuth`

Prefill domain lists for the matrix and simulator UIs.

| Param | Type | Required | Description |
|---|---|---|---|
| `source` | string | yes | `"top-sites"`, `"top-queried"`, `"top-blocked"`, `"top-allowed"` |
| `limit` | int | no | 1-10000, default 1000 |

`top-sites` reads the operator's `TOP_SITES_PATH` ranking CSV on demand. No dataset
is bundled or downloaded. The entire file is validated before selecting the
requested count; missing, unreadable, empty or malformed sources return 503
with an error and no partial domain list. See [configuration](configuration.md#analysis-top-sites).
Concurrent heavy analysis requests may return 429.

```json
{
  "data": {
    "source": "top-sites",
    "count": 1000,
    "domains": ["google.com", "youtube.com", "facebook.com", "amazon.com", "..."]
  },
  "error": null
}
```

---

### API Tokens

#### GET /api/tokens

**Auth:** `adminAuth`

```json
{
  "data": [
    {
      "id": 1,
      "name": "grafana",
      "token_prefix": "a1b2c3d4",
      "role": "readonly",
      "created_at": "2026-03-01T12:00:00Z",
      "last_used_at": "2026-03-04T10:30:00Z"
    }
  ],
  "error": null
}
```

#### POST /api/tokens

**Auth:** `adminAuth` | **Status:** 201

```typescript
// Request
{ name: string; role?: string }  // role defaults to "readonly"
```

```json
{
  "data": {
    "token": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
    "id": 2,
    "name": "ci-pipeline",
    "role": "admin"
  },
  "error": null
}
```

> **Important:** The plaintext `token` is only returned once at creation. Store it securely.

#### DELETE /api/tokens/:id

**Auth:** `adminAuth`

```json
{ "data": { "success": true }, "error": null }
```

---

### Config Export/Import

#### GET /api/config/export

**Auth:** `adminAuth`

Returns the full `ConfigExport` object (see type definition above).

`settings` deliberately omits `sync_secret` and `session_secret`; an exported backup is not a secret backup.

```json
{
  "data": {
    "version": "1.0",
    "exported_at": "2026-03-04T10:00:00Z",
    "upstreams": [
      { "upstream": "tls://dns.quad9.net", "enabled": true }
    ],
    "blocklists": [
      { "url": "https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/hosts/pro.txt", "alias": "Hagezi Pro", "enabled": true }
    ],
    "allowlists": [],
    "rewrites": [
      { "domain": "plex.home", "ip_addresses": "10.42.1.50", "enabled": true }
    ],
    "groups": [
      { "name": "Kids Devices", "members": ["10.42.1.42", "10.42.1.43"], "blocklists": ["Hagezi Pro"], "allowlists": [] }
    ],
    "ranges": [
      { "name": "IOT Jail", "cidr": "10.42.2.0/24", "blocklists": ["Hagezi Pro"], "allowlists": [] }
    ],
    "settings": { "cache_ttl": "3600", "strategy": "weighted", "log_retention_days": "1095" },
    "bootstrap_servers": ["9.9.9.9:53"],
    "clients": [
      { "ip": "10.42.1.42", "alias": "Sam Phone", "groups": ["Kids Devices"], "blocklists": ["Hagezi Pro"] }
    ]
  },
  "error": null
}
```

#### POST /api/config/import

**Auth:** `adminAuth`

Request body is the same shape as the `ConfigExport` data field above. Applies in a transaction and reloads all in-memory stores. Any `sync_secret` or `session_secret` keys supplied under `settings` are ignored, so import cannot overwrite the node's live authentication material.

```json
{ "data": { "success": true }, "error": null }
```

---

### Cache

#### POST /api/cache/clear

**Auth:** `adminAuth`

```json
{ "data": { "success": true }, "error": null }
```

---

### Archive

#### GET /api/archive/status

**Auth:** `apiAuth` (readonly)

```json
{
  "data": {
    "archive_path": "./archives",
    "archive_age_days": 730,
    "oldest_data_days": 45,
    "live_rows": 500000,
    "archive_files": [
      { "name": "svart-dns-2024-01-31.parquet", "size_mb": 12.5, "modified": "2026-02-01T00:00:00Z" }
    ]
  },
  "error": null
}
```

#### POST /api/archive

**Auth:** `adminAuth` — triggers archival, returns same shape as status.

---

### Peers (HA Sync)

#### GET /api/peers

**Auth:** `apiAuth` (readonly)

```json
{
  "data": {
    "node_id": "pve-blue",
    "node_name": "svart-a",
    "sync_interval": "2s",
    "has_secret": true,
    "tls_configured": true,
    "sync_tls_ready": true,
    "peers": [
      {
        "url": "https://10.42.1.7:443",
        "node_name": "svart-b",
        "healthy": true,
        "last_sync_at": "2026-03-04T10:30:00.123456789Z",
        "changes_24h": 0,
        "consecutive_errors": 0
      }
    ]
  },
  "error": null
}
```

The response never contains `sync_secret`. `sync_tls_ready` requires a strong sync secret, verified TLS client configuration, and at least one observed successful sync for every peer. It does not run a fresh probe, so rollout verification must also require `healthy`, an advancing `last_sync_at`, and empty error fields. When configuration or observed sync is unusable, it is `false` and `sync_tls_error` contains a diagnostic safe for authenticated operators. Peer-controlled HTTP/application response bodies and invalid timestamps are not reflected into `last_error`, logs, or this API; non-success responses use local generic messages such as `peer returned HTTP 503`. Sync responses are limited to 16 MiB and redirects are not followed.

#### POST /api/peers

**Auth:** `apiAuth` (admin)

Adds one peer from `{ "url": "https://10.42.1.7:443" }`. The URL is canonicalized to an HTTPS origin before any setting changes: hostnames become lowercase, a missing port becomes `443`, and a lone trailing `/` is removed. Userinfo, non-root paths, query strings, and fragments are rejected. A nonempty `SYNC_PEER_ALLOWLIST` is required, and the canonical URL must be an exact member; rejection does not mutate the peer list.

The same mandatory canonical destination check applies to the sync worker, pairing, and gossip. Every deployment that uses peers or pairing must configure every permitted node origin, including the local node's `self_url`, for example:

```text
SYNC_PEER_ALLOWLIST=https://10.42.1.6:443,https://10.42.1.7:443
```

Missing, empty, or malformed allowlist configuration disables peer sync while DNS continues to serve.

#### POST /api/peers/pair

**Auth:** `adminAuth`

Starts a 10-minute pairing window. Requires a local `sync_secret` of at least 32 bytes and returns a 16-byte hex `pairing_code`, canonical `self_url`, `node_id`, and `expires_in`. It never returns a secret or proof.

#### POST /api/peers/confirm

**Auth:** `adminAuth`

Accepts `{ "peer_url": "https://10.42.1.6:443", "pairing_code": "..." }`. The confirmer sends the initiator only its canonical URL, the one-time code, and a role-separated HMAC-SHA256 proof keyed by the local sync secret. The initiator returns a distinct proof, which the confirmer verifies before adding the peer. Proofs are bound to protocol version, role, code, and canonical confirmer URL; the raw sync secret is never present on the wire.

The initiating node's callback, `POST /api/sync/pair/complete`, accepts `{ "peer_url", "pairing_code", "proof" }` during the active pairing window. Pairing request bodies are capped at 64 KiB. It consumes the code after successful proof and allowlist validation. This is a protocol endpoint; operators use the two admin endpoints above rather than calling it manually.

### Users and API tokens

All endpoints in this section require an administrator token or administrator
browser session.

#### GET /api/users

Lists `{ id, username, role, created_at }` objects. Password hashes are never
returned.

#### POST /api/users

Creates a browser user from `{ "username", "password", "role" }`; role defaults
to `admin` and must be `readonly` or `admin`. Duplicate usernames return 409.

#### PUT /api/users/{id}

Updates `role`, `password`, or both. An empty request is rejected.

#### DELETE /api/users/{id}

Deletes the user and records a sync tombstone. A browser session cannot delete
its own account, and the final administrator cannot be removed; both return
409.

#### GET /api/tokens

Lists token metadata (`id`, `name`, `token_prefix`, `role`, `created_at`, and
optional `last_used_at`). Plaintext tokens and hashes are never returned.

#### POST /api/tokens

Creates a token from `{ "name", "role" }`; role defaults to `readonly`. The
response returns the `sv_...` plaintext exactly once. Store it immediately;
future list calls cannot recover it.

#### DELETE /api/tokens/{id}

Revokes the token, records a sync tombstone, and invalidates the validation
cache.

#### POST /api/auth/sessions/revoke

**Auth:** `adminAuth`

Rotates the persisted session-signing key, invalidates every browser session, and expires the caller's session cookie. Returns only `{ "success": true }`; the replacement key is never returned. API-token authentication is recommended for operational rotation because browser sessions are intentionally revoked.

---

## HTTP Status Codes

| Code | Meaning |
|---|---|
| 200 | Success (GET, PUT, DELETE, toggle, refresh) |
| 201 | Created (POST that creates a resource) |
| 400 | Bad input (missing fields, invalid CIDR, etc.) |
| 401 | No auth token or invalid token |
| 403 | Token lacks required role |
| 404 | Resource not found |
| 405 | Method not allowed |
| 409 | Conflict (custom domain rule overlap) |
| 500 | Internal server error |

All types use consistent `snake_case` JSON keys.
