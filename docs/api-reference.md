# Svart API reference

Generated from the Go request and response types. Run `make docs`; do not edit.

The interactive reference is at `/docs`; the OpenAPI 3.1 schema is at `/api/openapi.json`. Documentation is public. Protected API calls require a session cookie or X-Api-Key header; mutations require administrator privileges.

Request object schemas permit unknown properties, matching the server decoder. Omitted request fields use Go zero values unless marked required by handler validation.

Each success envelope has `data` and a null `error`. Every error envelope has `data: null`, a safe human-readable `error`, and a stable `error_code`. A 503 means data is unavailable; it never represents an empty or partially read success.

## GET /api/allowlists

List all allowlists

Returns all configured allowlists with their ID, URL, alias, enabled status, domain count, and last updated timestamp

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiAllowlistsResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/allowlists

Create an allowlist

Creates a new allowlist with a URL and alias, then triggers an async fetch of the allowlist domains if URL is provided

Request: `PostApiAllowlistsBody`.

```json
{"alias":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiAllowlistsResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"alias":"Example","id":1},"error":null}
```

## DELETE /api/allowlists/{id}

DELETE /api/allowlists/{id}

DELETE /api/allowlists/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiAllowlistsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## PUT /api/allowlists/{id}

PUT /api/allowlists/{id}

PUT /api/allowlists/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |

Request: `PutApiAllowlistsIdBody`.

```json
{"alias":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiAllowlistsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/allowlists/{id}/compatibility

GET /api/allowlists/{id}/compatibility

GET /api/allowlists/{id}/compatibility.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiAllowlistsIdCompatibilityResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"diagnostics":[],"limit":1,"offset":1,"summary":{"applied":1,"assessed":true,"assessed_at":"","diagnostic_count":1,"invalid":1,"lines":1,"unsupported":1},"total":1},"error":null}
```

## GET /api/allowlists/{id}/domains

GET /api/allowlists/{id}/domains

GET /api/allowlists/{id}/domains.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiAllowlistsIdDomainsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"allowlist_id":1,"domains":[],"limit":1,"offset":1,"total":1},"error":null}
```

## POST /api/allowlists/{id}/refresh

POST /api/allowlists/{id}/refresh

POST /api/allowlists/{id}/refresh.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiAllowlistsIdRefreshResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/allowlists/{id}/toggle

POST /api/allowlists/{id}/toggle

POST /api/allowlists/{id}/toggle.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiAllowlistsIdToggleResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/analysis/compare

Compare blocklists pairwise

Computes pairwise domain overlap between 2 to 64 distinct enabled blocklists (duplicate IDs are ignored), returning overlap matrix, unique counts, and union size. Requests whose lists exceed the per-request work budget are rejected with 400; 429 when the analysis capacity is busy.

Request: `PostApiAnalysisCompareBody`.

```json
{"blocklist_ids":[1,2]}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiAnalysisCompareResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"lists":[],"overlap":[],"union_size":1,"unique_count":[]},"error":null}
```

## GET /api/analysis/domains

Get domain prefill sources

Returns domain lists from various sources for use in analysis tools. Sources: top-sites (operator-provided ranking CSV), top-queried, top-blocked, top-allowed (from query logs, last 7 days).

| Parameter | Location | Required |
| --- | --- | --- |
| `client_ip` | query | false |
| `limit` | query | false |
| `source` | query | false |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiAnalysisDomainsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"count":1,"domains":[],"source":""},"error":null}
```

## POST /api/analysis/matrix

Domain x blocklist membership matrix

Returns a matrix showing which domains are blocked by which blocklists, with matched rule details. At most 10,000 domains and 64 distinct enabled lists per request, and at most 250,000 work units (domains x (lists + their complex wildcard patterns)); larger requests are rejected with 400, never truncated. 429 when the analysis capacity is busy.

Request: `PostApiAnalysisMatrixBody`.

```json
{"domains":["example.com"]}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiAnalysisMatrixResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"domains":[],"lists":[],"matched_rules":[],"matrix":[],"record_type":1},"error":null}
```

## POST /api/analysis/simulate

Simulate policy evaluation for a batch of domains

Runs the full three-tier policy evaluation (Range, Group, IP) for each domain against a specific client IP. Maximum 10,000 domains per request; 429 when the analysis capacity is busy.

Request: `PostApiAnalysisSimulateBody`.

```json
{"client_ip":"192.0.2.10","domains":["example.com"]}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiAnalysisSimulateResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"client_ip":"192.0.2.10","results":[]},"error":null}
```

## POST /api/archive

Trigger manual archive

Manually triggers an archive cycle to export old query logs to Parquet files and returns the archive status

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiArchiveResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"archive_age_days":1,"archive_files":[],"archive_path":"","live_rows":1,"oldest_data_days":1},"error":null}
```

## GET /api/archive/status

Get archive status

Returns archive status including list of Parquet archive files and their metadata

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiArchiveStatusResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"archive_age_days":1,"archive_files":[],"archive_path":"","live_rows":1,"oldest_data_days":1},"error":null}
```

## GET /api/auth/check

GET /api/auth/check

GET /api/auth/check.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiAuthCheckResponse` | OK |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"authenticated":true,"role":"readonly","username":"operator"},"error":null}
```

## POST /api/auth/login

POST /api/auth/login

POST /api/auth/login.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

Request: `PostApiAuthLoginBody`.

```json
{"password":"example-password-not-a-secret","username":"operator"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiAuthLoginResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"authenticated":true,"role":"readonly","username":"operator"},"error":null}
```

## POST /api/auth/logout

POST /api/auth/logout

POST /api/auth/logout.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiAuthLogoutResponse` | OK |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 500 | `APIError` | Internal Server Error |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/auth/sessions/revoke

Revoke all browser sessions

Deletes every browser session on this node (sessions are node-local); every browser must log in again. API tokens are unaffected.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiAuthSessionsRevokeResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/blocklists

List all blocklists

Returns all configured blocklists with their ID, URL, alias, enabled status, domain count, and last updated timestamp

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiBlocklistsResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/blocklists

Create a blocklist

Creates a new blocklist with a URL and alias, then triggers an async fetch of the blocklist domains

Request: `PostApiBlocklistsBody`.

```json
{"alias":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiBlocklistsResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"alias":"Example","id":1},"error":null}
```

## GET /api/blocklists/history

GET /api/blocklists/history

GET /api/blocklists/history.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiBlocklistsHistoryResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## GET /api/blocklists/history/{historyId}/domains

GET /api/blocklists/history/{historyId}/domains

GET /api/blocklists/history/{historyId}/domains.

| Parameter | Location | Required |
| --- | --- | --- |
| `historyId` | path | true |
| `limit` | query | false |
| `offset` | query | false |
| `search` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiBlocklistsHistoryHistoryIdDomainsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"blocklist_id":1,"domains":[],"history_id":1,"limit":1,"offset":1,"refreshed_at":"","total":1},"error":null}
```

## GET /api/blocklists/unique-domains

GET /api/blocklists/unique-domains

GET /api/blocklists/unique-domains.

| Parameter | Location | Required |
| --- | --- | --- |
| `ids` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiBlocklistsUniqueDomainsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"lists":[],"unique_total":1},"error":null}
```

## DELETE /api/blocklists/{id}

DELETE /api/blocklists/{id}

DELETE /api/blocklists/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |
| `search` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiBlocklistsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## PUT /api/blocklists/{id}

PUT /api/blocklists/{id}

PUT /api/blocklists/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |
| `search` | query | false |

Request: `PutApiBlocklistsIdBody`.

```json
{"alias":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiBlocklistsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/blocklists/{id}/compatibility

GET /api/blocklists/{id}/compatibility

GET /api/blocklists/{id}/compatibility.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |
| `search` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiBlocklistsIdCompatibilityResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"diagnostics":[],"limit":1,"offset":1,"summary":{"applied":1,"assessed":true,"assessed_at":"","diagnostic_count":1,"invalid":1,"lines":1,"unsupported":1},"total":1},"error":null}
```

## GET /api/blocklists/{id}/domains

GET /api/blocklists/{id}/domains

GET /api/blocklists/{id}/domains.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |
| `search` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiBlocklistsIdDomainsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"blocklist_id":1,"domains":[],"limit":1,"offset":1,"total":1},"error":null}
```

## POST /api/blocklists/{id}/refresh

POST /api/blocklists/{id}/refresh

POST /api/blocklists/{id}/refresh.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |
| `search` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiBlocklistsIdRefreshResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/blocklists/{id}/toggle

POST /api/blocklists/{id}/toggle

POST /api/blocklists/{id}/toggle.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `limit` | query | false |
| `offset` | query | false |
| `search` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiBlocklistsIdToggleResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/bootstrap

List bootstrap DNS servers

Returns all configured bootstrap DNS servers used for resolving upstream hostnames

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiBootstrapResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/bootstrap

POST /api/bootstrap

POST /api/bootstrap.

Request: `PostApiBootstrapBody`.

```json
{"server":"192.0.2.53"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiBootstrapResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## PUT /api/bootstrap

PUT /api/bootstrap

PUT /api/bootstrap.

Request: `PutApiBootstrapBody`.

```json
{}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiBootstrapResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/cache/clear

Clear DNS cache

Clears the entire DNS response cache

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiCacheClearResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/clients

List all known clients

Returns all known DNS clients with their IP, alias, group memberships, and list assignments

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiClientsResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## GET /api/clients/{ip}

GET /api/clients/{ip}

GET /api/clients/{ip}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiClientsIpResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"alias":"Example","avg_latency_microseconds":1,"blocklist_stats":{"lists":[],"unique_total":1},"blocklists":[],"custom_allowed":[],"custom_blocked":[],"first_seen":"","groups":[],"ip":"192.0.2.10","last_seen":"","total_queries":1},"error":null}
```

## PUT /api/clients/{ip}/alias

PUT /api/clients/{ip}/alias

PUT /api/clients/{ip}/alias.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |

Request: `PutApiClientsIpAliasBody`.

```json
{"alias":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiClientsIpAliasResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/clients/{ip}/allow-domain

POST /api/clients/{ip}/allow-domain

POST /api/clients/{ip}/allow-domain.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |

Request: `PostApiClientsIpAllowDomainBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiClientsIpAllowDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/clients/{ip}/allow-domain/{domain}

DELETE /api/clients/{ip}/allow-domain/{domain}

DELETE /api/clients/{ip}/allow-domain/{domain}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `domain` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiClientsIpAllowDomainDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/clients/{ip}/allowlists/{listId}

DELETE /api/clients/{ip}/allowlists/{listId}

DELETE /api/clients/{ip}/allowlists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiClientsIpAllowlistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/clients/{ip}/allowlists/{listId}

POST /api/clients/{ip}/allowlists/{listId}

POST /api/clients/{ip}/allowlists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiClientsIpAllowlistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/clients/{ip}/block-domain

POST /api/clients/{ip}/block-domain

POST /api/clients/{ip}/block-domain.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |

Request: `PostApiClientsIpBlockDomainBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiClientsIpBlockDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/clients/{ip}/block-domain/{domain}

DELETE /api/clients/{ip}/block-domain/{domain}

DELETE /api/clients/{ip}/block-domain/{domain}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `domain` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiClientsIpBlockDomainDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/clients/{ip}/blocklists/{listId}

DELETE /api/clients/{ip}/blocklists/{listId}

DELETE /api/clients/{ip}/blocklists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiClientsIpBlocklistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/clients/{ip}/blocklists/{listId}

POST /api/clients/{ip}/blocklists/{listId}

POST /api/clients/{ip}/blocklists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiClientsIpBlocklistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/clients/{ip}/groups/{groupId}

DELETE /api/clients/{ip}/groups/{groupId}

DELETE /api/clients/{ip}/groups/{groupId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `groupId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiClientsIpGroupsGroupIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/clients/{ip}/groups/{groupId}

POST /api/clients/{ip}/groups/{groupId}

POST /api/clients/{ip}/groups/{groupId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `groupId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiClientsIpGroupsGroupIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/clients/{ip}/policy

DELETE /api/clients/{ip}/policy

DELETE /api/clients/{ip}/policy.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiClientsIpPolicyResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/clients/{ip}/policy/{policyId}

POST /api/clients/{ip}/policy/{policyId}

POST /api/clients/{ip}/policy/{policyId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `ip` | path | true |
| `policyId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiClientsIpPolicyPolicyIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/config/export

Export full configuration

Exports the complete server configuration including upstreams, blocklists, allowlists, rewrites, groups, ranges, settings, bootstrap servers, and clients

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiConfigExportResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"allowlists":[],"blocklists":[],"bootstrap_servers":[],"clients":[],"exported_at":"","groups":[],"policies":[],"ranges":[],"rewrites":[],"settings":{},"upstreams":[],"version":""},"error":null}
```

## POST /api/config/import

Import configuration

Validates and applies an exported configuration, merging with existing data. Reloads all in-memory stores after import.

Request: `PostApiConfigImportBody`.

```json
{}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiConfigImportResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/groups

List all groups

Returns all client groups with their members and assigned lists

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiGroupsResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/groups

POST /api/groups

POST /api/groups.

Request: `PostApiGroupsBody`.

```json
{"name":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiGroupsResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"id":1,"name":"Example"},"error":null}
```

## DELETE /api/groups/{id}

DELETE /api/groups/{id}

DELETE /api/groups/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiGroupsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/groups/{id}

GET /api/groups/{id}

GET /api/groups/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiGroupsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"avg_latency_microseconds":1,"blocklist_stats":{"lists":[],"unique_total":1},"blocklists":[],"custom_allowed":[],"custom_blocked":[],"first_seen":"","id":1,"last_seen":"","members":[],"name":"Example","recent_logs":[],"total_queries":1},"error":null}
```

## PUT /api/groups/{id}

PUT /api/groups/{id}

PUT /api/groups/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PutApiGroupsIdBody`.

```json
{"name":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiGroupsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/groups/{id}/allow-domain

POST /api/groups/{id}/allow-domain

POST /api/groups/{id}/allow-domain.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PostApiGroupsIdAllowDomainBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiGroupsIdAllowDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/groups/{id}/allow-domain/{domain}

DELETE /api/groups/{id}/allow-domain/{domain}

DELETE /api/groups/{id}/allow-domain/{domain}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `domain` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiGroupsIdAllowDomainDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/groups/{id}/allowlists/{listId}

DELETE /api/groups/{id}/allowlists/{listId}

DELETE /api/groups/{id}/allowlists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiGroupsIdAllowlistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/groups/{id}/allowlists/{listId}

POST /api/groups/{id}/allowlists/{listId}

POST /api/groups/{id}/allowlists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiGroupsIdAllowlistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/groups/{id}/block-domain

POST /api/groups/{id}/block-domain

POST /api/groups/{id}/block-domain.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PostApiGroupsIdBlockDomainBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiGroupsIdBlockDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/groups/{id}/block-domain/{domain}

DELETE /api/groups/{id}/block-domain/{domain}

DELETE /api/groups/{id}/block-domain/{domain}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `domain` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiGroupsIdBlockDomainDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/groups/{id}/blocklists/batch

POST /api/groups/{id}/blocklists/batch

POST /api/groups/{id}/blocklists/batch.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PostApiGroupsIdBlocklistsBatchBody`.

```json
{}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiGroupsIdBlocklistsBatchResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"assigned":1},"error":null}
```

## DELETE /api/groups/{id}/blocklists/{listId}

DELETE /api/groups/{id}/blocklists/{listId}

DELETE /api/groups/{id}/blocklists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiGroupsIdBlocklistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/groups/{id}/blocklists/{listId}

POST /api/groups/{id}/blocklists/{listId}

POST /api/groups/{id}/blocklists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiGroupsIdBlocklistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/groups/{id}/members/batch

POST /api/groups/{id}/members/batch

POST /api/groups/{id}/members/batch.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PostApiGroupsIdMembersBatchBody`.

```json
{}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiGroupsIdMembersBatchResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"added":1},"error":null}
```

## DELETE /api/groups/{id}/members/{ip}

DELETE /api/groups/{id}/members/{ip}

DELETE /api/groups/{id}/members/{ip}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `ip` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiGroupsIdMembersIpResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/groups/{id}/members/{ip}

POST /api/groups/{id}/members/{ip}

POST /api/groups/{id}/members/{ip}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `ip` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiGroupsIdMembersIpResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/groups/{id}/policy

DELETE /api/groups/{id}/policy

DELETE /api/groups/{id}/policy.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiGroupsIdPolicyResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/groups/{id}/policy/{policyId}

POST /api/groups/{id}/policy/{policyId}

POST /api/groups/{id}/policy/{policyId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `policyId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiGroupsIdPolicyPolicyIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/investigate

Execute investigation SQL query

Runs a constrained read-only SELECT query against the unified query_logs view (live SQLite + archived Parquet). Only one query runs at a time.

Request: `PostApiInvestigateBody`.

```json
{"sql":"SELECT COUNT(*) AS queries FROM query_logs"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiInvestigateResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 408 | `APIError` | Request Timeout |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 422 | `APIError` | Unprocessable Entity |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |
| 504 | `APIError` | Gateway Timeout |

Example response (200):

```json
{"data":{"columns":[],"duration_ms":1,"row_count":1,"rows":[]},"error":null}
```

## GET /api/investigate/schema

Get investigation schema

Returns column metadata for the query_logs unified view (live + archived)

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiInvestigateSchemaResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"tables":[]},"error":null}
```

## GET /api/openapi.json

Generated OpenAPI 3.1 specification

Generated OpenAPI 3.1 specification. No authentication required.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `Document` | OK |
| 421 | `APIError` | Misdirected Request |

## GET /api/peers

GET /api/peers

GET /api/peers.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiPeersResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"has_secret":true,"node_id":"","node_name":"","peers":[],"replicate_identity":true,"sync_interval":"","tls_configured":true},"error":null}
```

## POST /api/peers

POST /api/peers

POST /api/peers.

Request: `PostApiPeersBody`.

```json
{"url":"https://example.com/list.txt"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiPeersResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"success":true},"error":null}
```

## POST /api/peers/confirm

POST /api/peers/confirm

POST /api/peers/confirm.

Request: `PostApiPeersConfirmBody`.

```json
{"pairing_code":"EXAMPLE-NOT-A-VALID-CREDENTIAL","peer_url":"https://example.com/list.txt"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiPeersConfirmResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 502 | `APIError` | Bad Gateway |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"peer_url":"https://example.com/list.txt","remote_node":"","success":true},"error":null}
```

## POST /api/peers/pair

POST /api/peers/pair

POST /api/peers/pair.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiPeersPairResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"expires_in":"","node_id":"","pairing_code":"EXAMPLE-NOT-A-VALID-CREDENTIAL","self_url":"https://example.com/list.txt"},"error":null}
```

## DELETE /api/peers/{url}

Get peer sync status or manage peers

GET returns sync status. POST adds a peer. DELETE /api/peers/{url} removes a peer.

| Parameter | Location | Required |
| --- | --- | --- |
| `url` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiPeersUrlResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/policies

GET /api/policies

GET /api/policies.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiPoliciesResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/policies

POST /api/policies

POST /api/policies.

Request: `PostApiPoliciesBody`.

```json
{"name":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiPoliciesResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"id":1,"name":"Example"},"error":null}
```

## DELETE /api/policies/{id}

DELETE /api/policies/{id}

DELETE /api/policies/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiPoliciesIdResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/policies/{id}

GET /api/policies/{id}

GET /api/policies/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiPoliciesIdResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"allowlists":[],"assigned_to":{"clients":[],"groups":[],"ranges":[]},"blocklist_stats":{"lists":[],"unique_total":1},"blocklists":[],"custom_allowed":[],"custom_blocked":[],"description":"","id":1,"name":"Example"},"error":null}
```

## PUT /api/policies/{id}

PUT /api/policies/{id}

PUT /api/policies/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PutApiPoliciesIdBody`.

```json
{"name":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiPoliciesIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/policies/{id}/allow-domain

POST /api/policies/{id}/allow-domain

POST /api/policies/{id}/allow-domain.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PostApiPoliciesIdAllowDomainBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiPoliciesIdAllowDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/policies/{id}/allow-domain/{domain}

DELETE /api/policies/{id}/allow-domain/{domain}

DELETE /api/policies/{id}/allow-domain/{domain}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `domain` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiPoliciesIdAllowDomainDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/policies/{id}/allowlists/{listId}

DELETE /api/policies/{id}/allowlists/{listId}

DELETE /api/policies/{id}/allowlists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiPoliciesIdAllowlistsListIdResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/policies/{id}/allowlists/{listId}

POST /api/policies/{id}/allowlists/{listId}

POST /api/policies/{id}/allowlists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiPoliciesIdAllowlistsListIdResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/policies/{id}/block-domain

POST /api/policies/{id}/block-domain

POST /api/policies/{id}/block-domain.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PostApiPoliciesIdBlockDomainBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiPoliciesIdBlockDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/policies/{id}/block-domain/{domain}

DELETE /api/policies/{id}/block-domain/{domain}

DELETE /api/policies/{id}/block-domain/{domain}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `domain` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiPoliciesIdBlockDomainDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/policies/{id}/blocklists/{listId}

DELETE /api/policies/{id}/blocklists/{listId}

DELETE /api/policies/{id}/blocklists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiPoliciesIdBlocklistsListIdResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/policies/{id}/blocklists/{listId}

POST /api/policies/{id}/blocklists/{listId}

POST /api/policies/{id}/blocklists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiPoliciesIdBlocklistsListIdResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/policy/evaluate

Evaluate policy for a client and domain

Debug endpoint that shows the full three-tier policy evaluation chain (Range, Group, IP) without caching

| Parameter | Location | Required |
| --- | --- | --- |
| `client_ip` | query | false |
| `domain` | query | false |
| `type` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiPolicyEvaluateResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"client_ip":"192.0.2.10","domain":"example.com","record_type":1,"result":""},"error":null}
```

## GET /api/query-logs

GET /api/query-logs

GET /api/query-logs.

| Parameter | Location | Required |
| --- | --- | --- |
| `blocked` | query | false |
| `client_ip` | query | false |
| `domain` | query | false |
| `group_id` | query | false |
| `limit` | query | false |
| `offset` | query | false |
| `query_type` | query | false |
| `range_id` | query | false |
| `result_reason` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiQueryLogsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"limit":1,"logs":[],"offset":1,"total":1},"error":null}
```

## GET /api/query-logs/{id}

Get query log detail with full policy evaluation

Returns a single query log entry including nested policy evaluation data

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiQueryLogsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"blocked":true,"client_ip":"192.0.2.10","client_name":"","group_entity":"","group_is_published":true,"group_list_id":1,"group_list_name":"","group_result":"","group_rule":"","id":1,"ip_entity":"","ip_is_published":true,"ip_list_id":1,"ip_list_name":"","ip_result":"","ip_rule":"","latency_microseconds":1,"query_name":"example.com","query_type":"","range_entity":"","range_is_published":true,"range_list_id":1,"range_list_name":"","range_result":"","range_rule":"","response_code":"","result":"","result_entity":"","result_is_published":true,"result_list_id":1,"result_list_name":"","result_reason":"","result_rule":"","result_tier":"","timestamp":"2026-01-01T00:00:00Z","upstream":"192.0.2.53"},"error":null}
```

## GET /api/ranges

List all IP ranges

Returns all configured IP ranges (CIDRs) with their ID, name, CIDR, and creation timestamp

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiRangesResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/ranges

Create an IP range

Creates a new IP range with a name and CIDR notation. The CIDR is validated before creation.

Request: `PostApiRangesBody`.

```json
{"cidr":"192.0.2.0/24","name":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiRangesResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"id":1},"error":null}
```

## DELETE /api/ranges/{id}

DELETE /api/ranges/{id}

DELETE /api/ranges/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiRangesIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/ranges/{id}

GET /api/ranges/{id}

GET /api/ranges/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiRangesIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"allowlists":[],"avg_latency_microseconds":1,"blocklist_stats":{"lists":[],"unique_total":1},"blocklists":[],"cidr":"192.0.2.0/24","created_at":"","custom_allowed":[],"custom_blocked":[],"first_seen":"","id":1,"last_seen":"","name":"Example","recent_logs":[],"total_queries":1},"error":null}
```

## PUT /api/ranges/{id}

PUT /api/ranges/{id}

PUT /api/ranges/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PutApiRangesIdBody`.

```json
{"name":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiRangesIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/ranges/{id}/allow-domain

POST /api/ranges/{id}/allow-domain

POST /api/ranges/{id}/allow-domain.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PostApiRangesIdAllowDomainBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiRangesIdAllowDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/ranges/{id}/allow-domain/{domain}

DELETE /api/ranges/{id}/allow-domain/{domain}

DELETE /api/ranges/{id}/allow-domain/{domain}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `domain` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiRangesIdAllowDomainDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/ranges/{id}/allowlists/{listId}

DELETE /api/ranges/{id}/allowlists/{listId}

DELETE /api/ranges/{id}/allowlists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiRangesIdAllowlistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/ranges/{id}/allowlists/{listId}

POST /api/ranges/{id}/allowlists/{listId}

POST /api/ranges/{id}/allowlists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiRangesIdAllowlistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/ranges/{id}/block-domain

POST /api/ranges/{id}/block-domain

POST /api/ranges/{id}/block-domain.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PostApiRangesIdBlockDomainBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiRangesIdBlockDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/ranges/{id}/block-domain/{domain}

DELETE /api/ranges/{id}/block-domain/{domain}

DELETE /api/ranges/{id}/block-domain/{domain}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `domain` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiRangesIdBlockDomainDomainResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/ranges/{id}/blocklists/{listId}

DELETE /api/ranges/{id}/blocklists/{listId}

DELETE /api/ranges/{id}/blocklists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiRangesIdBlocklistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/ranges/{id}/blocklists/{listId}

POST /api/ranges/{id}/blocklists/{listId}

POST /api/ranges/{id}/blocklists/{listId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `listId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiRangesIdBlocklistsListIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## DELETE /api/ranges/{id}/policy

DELETE /api/ranges/{id}/policy

DELETE /api/ranges/{id}/policy.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiRangesIdPolicyResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/ranges/{id}/policy/{policyId}

POST /api/ranges/{id}/policy/{policyId}

POST /api/ranges/{id}/policy/{policyId}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |
| `policyId` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiRangesIdPolicyPolicyIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/rewrites

List all rewrites

Returns all DNS rewrite rules with domain, IP addresses, and enabled status

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiRewritesResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/rewrites

Create a DNS rewrite rule

Creates a new DNS rewrite rule mapping a domain to specific IP addresses

Request: `PostApiRewritesBody`.

```json
{"domain":"example.com","ip_addresses":"192.0.2.10"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiRewritesResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"domain":"example.com","enabled":true,"id":1,"ip_addresses":"192.0.2.10"},"error":null}
```

## POST /api/rewrites/batch

Batch create DNS rewrite rules

Creates multiple DNS rewrite rules at once (maximum 1000 per request)

Request: `PostApiRewritesBatchBody`.

```json
{}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiRewritesBatchResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":[],"error":null}
```

## GET /api/rewrites/stats

Get rewrite hit statistics

Returns hit counts and unique client counts per rewrite domain

| Parameter | Location | Required |
| --- | --- | --- |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiRewritesStatsResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## DELETE /api/rewrites/{id}

DELETE /api/rewrites/{id}

DELETE /api/rewrites/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiRewritesIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## PUT /api/rewrites/{id}

PUT /api/rewrites/{id}

PUT /api/rewrites/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PutApiRewritesIdBody`.

```json
{"domain":"example.com"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiRewritesIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"domain":"example.com","enabled":true,"id":1,"ip_addresses":"192.0.2.10"},"error":null}
```

## GET /api/settings

List all settings

Returns all configuration settings as key-value pairs

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiSettingsResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{},"error":null}
```

## PUT /api/settings/{key}

PUT /api/settings/{key}

PUT /api/settings/{key}.

| Parameter | Location | Required |
| --- | --- | --- |
| `key` | path | true |

Request: `PutApiSettingsKeyBody`.

```json
{"value":""}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiSettingsKeyResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"key":"","updated":true},"error":null}
```

## GET /api/setup

First-run setup

GET reports whether first-run setup is pending (and, while it is, the addresses devices should use as their DNS server). POST creates the first administrator using the one-time setup token printed in the server log, and signs that administrator in. Only available until an administrator exists.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiSetupResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"required":true},"error":null}
```

## POST /api/setup

First-run setup

GET reports whether first-run setup is pending (and, while it is, the addresses devices should use as their DNS server). POST creates the first administrator using the one-time setup token printed in the server log, and signs that administrator in. Only available until an administrator exists.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

Request: `PostApiSetupBody`.

```json
{"password":"example-password-not-a-secret","token":"EXAMPLE-NOT-A-VALID-CREDENTIAL","username":"operator"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiSetupResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"authenticated":true,"role":"readonly","username":"operator"},"error":null}
```

## GET /api/stats

Get dashboard statistics

Returns total queries, blocked queries, average latency, uptime, and cache statistics

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"avg_latency_microseconds":1,"blocked_queries":1,"cache":{"entries":1,"estimated_bytes":1,"hit_rate":1,"hits":1,"misses":1,"peak_bytes":1,"peak_entries":1},"total_queries":1,"uptime_seconds":1},"error":null}
```

## GET /api/stats/block-sources

Get block sources breakdown

Returns which blocklists are responsible for blocks

| Parameter | Location | Required |
| --- | --- | --- |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsBlockSourcesResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## GET /api/stats/dashboard

GET /api/stats/dashboard

GET /api/stats/dashboard.

| Parameter | Location | Required |
| --- | --- | --- |
| `buckets` | query | false |
| `client_limit` | query | false |
| `domain_limit` | query | false |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsDashboardResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"block_sources":[],"latency":[],"servfails":[],"summary":{"active_clients":1,"allowed_queries":1,"avg_latency_microseconds":1,"avg_latency_ms":1,"blocked_queries":1,"cache_hits":1,"custom_allows":1,"custom_blocks":1,"rewrite_count":1,"rewrite_hits":1,"servfail_count":1,"total_queries":1},"system":[],"timeseries":[],"top_blocked":{"domains":[],"total":1},"top_clients":[],"top_permitted":{"domains":[],"total":1},"upstream_usage":[]},"error":null}
```

## GET /api/stats/latency

Get latency timeseries

Returns time-bucketed avg and max latency for charting

| Parameter | Location | Required |
| --- | --- | --- |
| `buckets` | query | false |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsLatencyResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## GET /api/stats/servfails

Get SERVFAIL breakdown by client

Returns SERVFAIL counts per client with top failed domains

| Parameter | Location | Required |
| --- | --- | --- |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsServfailsResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## GET /api/stats/system

Get system resource stats

Get system resource stats.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsSystemResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## GET /api/stats/timeseries

Get query timeseries data

Returns time-bucketed query counts for charting

| Parameter | Location | Required |
| --- | --- | --- |
| `buckets` | query | false |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsTimeseriesResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"active_clients":1,"allowed_queries":1,"avg_latency_microseconds":1,"avg_latency_ms":1,"blocked_queries":1,"cache_hits":1,"custom_allows":1,"custom_blocks":1,"rewrite_count":1,"rewrite_hits":1,"servfail_count":1,"total_queries":1},"error":null}
```

## GET /api/stats/top-clients

Get top clients by query count

Returns clients ranked by query volume with allowed/blocked breakdown

| Parameter | Location | Required |
| --- | --- | --- |
| `limit` | query | false |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsTopClientsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## GET /api/stats/top-domains

Get top queried domains

Returns top domains by query count, optionally filtered by blocked status

| Parameter | Location | Required |
| --- | --- | --- |
| `blocked` | query | false |
| `limit` | query | false |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsTopDomainsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"domains":[],"total":1},"error":null}
```

## GET /api/stats/upstream-usage

Get upstream DNS usage breakdown

Returns which upstream resolvers handled queries

| Parameter | Location | Required |
| --- | --- | --- |
| `window` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiStatsUpstreamUsageResponse` | OK |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## GET /api/sync

Get sync changes since timestamp

Returns all config changes and tombstones since the given timestamp for peer-to-peer sync

| Parameter | Location | Required |
| --- | --- | --- |
| `since` | query | false |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiSyncResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 426 | `APIError` | Upgrade Required |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"changes":{},"node_id":"","server_time":"","tombstones":[]},"error":null}
```

## POST /api/sync/pair/complete

POST /api/sync/pair/complete

POST /api/sync/pair/complete.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

Request: `PostApiSyncPairCompleteBody`.

```json
{"pairing_code":"EXAMPLE-NOT-A-VALID-CREDENTIAL","peer_url":"https://example.com/list.txt","proof":"EXAMPLE-NOT-A-VALID-CREDENTIAL"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiSyncPairCompleteResponse` | OK |
| 400 | `APIError` | Bad Request |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 426 | `APIError` | Upgrade Required |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"node_id":"","proof":"EXAMPLE-NOT-A-VALID-CREDENTIAL","success":true},"error":null}
```

## GET /api/tokens

List or create API tokens

GET returns API token metadata only. POST creates a new API token and returns the plaintext token (shown once).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiTokensResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/tokens

List or create API tokens

GET returns API token metadata only. POST creates a new API token and returns the plaintext token (shown once).

Request: `PostApiTokensBody`.

```json
{"name":"Example"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiTokensResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"id":1,"name":"Example","role":"readonly","token":"EXAMPLE-NOT-A-VALID-CREDENTIAL"},"error":null}
```

## DELETE /api/tokens/{id}

Revoke an API token

Deletes an API token by its ID, permanently revoking access

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiTokensIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/upstreams

List or create upstream DNS servers

GET returns all configured upstream DNS servers. POST adds a new upstream server.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiUpstreamsResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/upstreams

List or create upstream DNS servers

GET returns all configured upstream DNS servers. POST adds a new upstream server.

Request: `PostApiUpstreamsBody`.

```json
{"upstream":"192.0.2.53"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiUpstreamsResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"enabled":true,"id":1,"upstream":"192.0.2.53"},"error":null}
```

## DELETE /api/upstreams/{id}

DELETE /api/upstreams/{id}

DELETE /api/upstreams/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiUpstreamsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## PUT /api/upstreams/{id}

PUT /api/upstreams/{id}

PUT /api/upstreams/{id}.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PutApiUpstreamsIdBody`.

```json
{"upstream":"192.0.2.53"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiUpstreamsIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## POST /api/upstreams/{id}/toggle

POST /api/upstreams/{id}/toggle

POST /api/upstreams/{id}/toggle.

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PostApiUpstreamsIdToggleResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 500 | `APIError` | Internal Server Error |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /api/users

List or create admin users

GET returns all admin users (without hashes). POST creates a new admin user.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetApiUsersResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":[],"error":null}
```

## POST /api/users

List or create admin users

GET returns all admin users (without hashes). POST creates a new admin user.

Request: `PostApiUsersBody`.

```json
{"password":"example-password-not-a-secret","username":"operator"}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 201 | `PostApiUsersResponse` | Created |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (201):

```json
{"data":{"created_at":"","id":1,"role":"readonly","username":"operator"},"error":null}
```

## DELETE /api/users/{id}

Update or delete an admin user

PUT updates role and/or password. DELETE removes the user (self-deletion prevented).

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `DeleteApiUsersIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## PUT /api/users/{id}

Update or delete an admin user

PUT updates role and/or password. DELETE removes the user (self-deletion prevented).

| Parameter | Location | Required |
| --- | --- | --- |
| `id` | path | true |

Request: `PutApiUsersIdBody`.

```json
{}
```

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `PutApiUsersIdResponse` | OK |
| 400 | `APIError` | Bad Request |
| 401 | `APIError` | Unauthorized |
| 403 | `APIError` | Forbidden |
| 404 | `APIError` | Not Found |
| 405 | `APIError` | Method Not Allowed |
| 409 | `APIError` | Conflict |
| 413 | `APIError` | Request Entity Too Large |
| 421 | `APIError` | Misdirected Request |
| 429 | `APIError` | Too Many Requests |
| 503 | `APIError` | Service Unavailable |

Example response (200):

```json
{"data":{"success":true},"error":null}
```

## GET /docs

Self-hosted interactive API reference

Self-hosted interactive API reference. No authentication required.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `text/html (string)` | OK |
| 421 | `APIError` | Misdirected Request |

## GET /docs.md

Generated Markdown API reference

Generated Markdown API reference. No authentication required.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `text/markdown (string)` | OK |
| 421 | `APIError` | Misdirected Request |

## GET /docs/swagger.json

Legacy OpenAPI specification URL

Legacy OpenAPI specification URL. No authentication required.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `Document` | OK |
| 421 | `APIError` | Misdirected Request |

## GET /health

Health check

Returns only public liveness status. All node and operational details require authentication.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `GetHealthResponse` | OK |
| 421 | `APIError` | Misdirected Request |

Example response (200):

```json
{"data":{"status":"ok"},"error":null}
```

## GET /metrics

Prometheus metrics exposition

Prometheus metrics exposition. No authentication required.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `text/plain (string)` | OK |
| 421 | `APIError` | Misdirected Request |

## GET /openapi.json

OpenAPI specification alias

OpenAPI specification alias. No authentication required.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `Document` | OK |
| 421 | `APIError` | Misdirected Request |

## GET /static/rapidoc-min.js

Self-hosted documentation renderer

Self-hosted documentation renderer. No authentication required.

Authentication: public endpoint (setup and pairing enforce their own one-time proof).

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | `application/javascript (string)` | OK |
| 421 | `APIError` | Misdirected Request |

## Schemas

### APIError

```typescript
{ "data": null; "error": string; "error_code": "conflict" | "forbidden" | "internal_error" | "invalid_request" | "method_not_allowed" | "not_found" | "rate_limited" | "request_canceled" | "request_too_large" | "resource_limit" | "timeout" | "unauthorized" | "unavailable" }
```

### APIToken

```typescript
{ "created_at": string; "id": number; "last_used_at"?: string; "name": string; "role": string; "token"?: string; "token_prefix": string }
```

### AddGroupMembersResponse

```typescript
{ "added": number }
```

### AdminUser

```typescript
{ "created_at": string; "id": number; "role": string; "username": string }
```

### AllowlistDomainsPage

```typescript
{ "allowlist_id": number; "domains": (Array<string> | null); "limit": number; "offset": number; "total": number }
```

### AllowlistView

```typescript
{ "alias": string; "compatibility": ListCompatibilitySummary; "domain_count": number; "enabled": boolean; "id": number; "last_updated": string; "refresh_interval": number; "url": string }
```

### ArchiveFileView

```typescript
{ "modified": string; "name": string; "size_mb": number }
```

### ArchiveStatusView

```typescript
{ "archive_age_days": number; "archive_files": (Array<ArchiveFileView> | null); "archive_path": string; "live_rows": number; "oldest_data_days": number }
```

### AssignGroupBlocklistsResponse

```typescript
{ "assigned": number }
```

### AssignedClient

```typescript
{ "alias": string; "ip": string }
```

### AssignedEntities

```typescript
{ "clients": (Array<AssignedClient> | null); "groups": (Array<PolicyReference> | null); "ranges": (Array<AssignedRange> | null) }
```

### AssignedRange

```typescript
{ "cidr": string; "id": number; "name": string }
```

### AuthenticatedIdentity

```typescript
{ "authenticated": boolean; "role": string; "username": string }
```

### BlocklistAssignment

```typescript
{ "alias": string; "domain_count": number; "id": number; "is_assigned": boolean; "source": string }
```

### BlocklistDomainsPage

```typescript
{ "blocklist_id": number; "domains": (Array<string> | null); "limit": number; "offset": number; "total": number }
```

### BlocklistHistoryView

```typescript
{ "added_count": number; "blocklist_alias": string; "blocklist_id": number; "id": number; "new_count": number; "previous_count": number; "refreshed_at": string; "removed_count": number; "sample_added": (Array<string> | null); "sample_removed": (Array<string> | null) }
```

### BlocklistUniqueEntry

```typescript
{ "alias": string; "id": number; "total": number; "unique_to_set": number }
```

### BlocklistUniqueStats

```typescript
{ "lists": (Array<BlocklistUniqueEntry> | null); "unique_total": number }
```

### BlocklistView

```typescript
{ "alias": string; "compatibility": ListCompatibilitySummary; "domain_count": number; "enabled": boolean; "id": number; "last_updated": string; "refresh_interval": number; "url": string }
```

### BootstrapServerView

```typescript
{ "id": number; "server": string }
```

### CheckpointPage

```typescript
{ "blocklist_id": number; "domains": (Array<string> | null); "history_id": number; "limit": number; "offset": number; "refreshed_at": string; "total": number }
```

### Client

```typescript
{ "alias": string; "ip_address": string; "is_member": boolean; "last_seen": string; "query_count": number }
```

### ClientDetailView

```typescript
{ "alias": string; "avg_latency_microseconds": number; "blocklist_stats": BlocklistUniqueStats; "blocklists": (Array<BlocklistAssignment> | null); "custom_allowed": (Array<string> | null); "custom_blocked": (Array<string> | null); "first_seen": string; "groups": (Array<Group> | null); "ip": string; "last_seen": string; "policy"?: PolicyReference; "total_queries": number }
```

### ClientExport

```typescript
{ "alias"?: string; "allowlists"?: Array<string>; "blocklists"?: Array<string>; "groups"?: Array<string>; "ip": string; "policy"?: string }
```

### ClientExportInput

```typescript
{ "alias"?: (string | null); "allowlists"?: (Array<string> | null); "blocklists"?: (Array<string> | null); "groups"?: (Array<string> | null); "ip"?: (string | null); "policy"?: (string | null) }
```

### CompareListInfo

```typescript
{ "alias": string; "domain_count": number; "id": number }
```

### CompareResponse

```typescript
{ "lists": (Array<CompareListInfo> | null); "overlap": (Array<(Array<number> | null)> | null); "union_size": number; "unique_count": (Array<number> | null) }
```

### CompletePairingResponse

```typescript
{ "node_id": string; "proof": string; "success": boolean }
```

### Components

```typescript
{ "schemas": ({ [key: string]: (Schema | null) } | null); "securitySchemes": ({ [key: string]: SecurityScheme } | null) }
```

### ConfigExport

```typescript
{ "allowlists": (Array<ListExport> | null); "blocklists": (Array<ListExport> | null); "bootstrap_servers": (Array<string> | null); "clients": (Array<ClientExport> | null); "exported_at": string; "groups": (Array<GroupExport> | null); "policies": (Array<PolicyExport> | null); "ranges": (Array<RangeExport> | null); "rewrites": (Array<RewriteExport> | null); "settings": ({ [key: string]: string } | null); "upstreams": (Array<UpstreamExport> | null); "version": string }
```

### ConfirmPeerResponse

```typescript
{ "peer_url": string; "remote_node": string; "success": boolean }
```

### CreateAllowlistResponse

```typescript
{ "alias": string; "id": number }
```

### CreateBlocklistResponse

```typescript
{ "alias": string; "id": number }
```

### CreateGroupResponse

```typescript
{ "id": number; "name": string }
```

### CreatePolicyResponse

```typescript
{ "id": number; "name": string }
```

### CreateRangeResponse

```typescript
{ "id": number }
```

### CreateRewriteRequest

```typescript
{ "domain": string; "enabled": boolean; "ip_addresses": string }
```

### CreateRewriteRequestInput

```typescript
{ "domain": string; "enabled"?: (boolean | null); "ip_addresses": string }
```

### CreateRewriteResponse

```typescript
{ "domain": string; "enabled": boolean; "id": number; "ip_addresses": string }
```

### CreatedAPITokenResponse

```typescript
{ "id": number; "name": string; "role": string; "token": string }
```

### CreatedRewriteView

```typescript
{ "domain": string; "enabled": boolean; "id": number; "ip_addresses": string }
```

### CustomHit

```typescript
{ "action": string; "rule": string }
```

### DNSCacheStats

```typescript
{ "entries": number; "estimated_bytes": number; "hit_rate": number; "hits": number; "misses": number; "peak_bytes": number; "peak_entries": number }
```

### DNSStatsResponse

```typescript
{ "avg_latency_microseconds": number; "blocked_queries": number; "cache": DNSCacheStats; "total_queries": number; "uptime_seconds": number }
```

### DashboardBlockSource

```typescript
{ "count": number; "list_name": string; "percentage": number }
```

### DashboardLatencyPoint

```typescript
{ "avg_latency_us": number; "max_latency_us": number; "timestamp": string }
```

### DashboardServfailClient

```typescript
{ "alias": string; "client_ip": string; "count": number; "top_domains": (Array<DashboardTopDomain> | null) }
```

### DashboardSnapshot

```typescript
{ "block_sources": (Array<DashboardBlockSource> | null); "latency": (Array<DashboardLatencyPoint> | null); "servfails": (Array<DashboardServfailClient> | null); "summary": DashboardSummary; "system": (Array<SystemSample> | null); "timeseries": (Array<DashboardTimeseriesPoint> | null); "top_blocked": DashboardTopDomains; "top_clients": (Array<DashboardTopClient> | null); "top_permitted": DashboardTopDomains; "upstream_usage": (Array<DashboardUpstreamUsage> | null) }
```

### DashboardSummary

```typescript
{ "active_clients": number; "allowed_queries": number; "avg_latency_microseconds": number; "avg_latency_ms": number; "blocked_queries": number; "cache_hits": number; "custom_allows": number; "custom_blocks": number; "rewrite_count": number; "rewrite_hits": number; "servfail_count": number; "total_queries": number }
```

### DashboardTimeseriesPoint

```typescript
{ "blocked": number; "queries": number; "timestamp": string }
```

### DashboardTopClient

```typescript
{ "alias": string; "allowed": number; "blocked": number; "client_ip": string }
```

### DashboardTopDomain

```typescript
{ "count": number; "domain": string }
```

### DashboardTopDomains

```typescript
{ "domains": (Array<DashboardTopDomain> | null); "total": number }
```

### DashboardUpstreamUsage

```typescript
{ "count": number; "percentage": number; "upstream": string }
```

### DeleteApiAllowlistsIdData

```typescript
SuccessResponse
```

### DeleteApiAllowlistsIdResponse

```typescript
{ "data": DeleteApiAllowlistsIdData; "error": null }
```

### DeleteApiAllowlistsIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiBlocklistsIdData

```typescript
SuccessResponse
```

### DeleteApiBlocklistsIdResponse

```typescript
{ "data": DeleteApiBlocklistsIdData; "error": null }
```

### DeleteApiBlocklistsIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiClientsIpAllowDomainDomainData

```typescript
SuccessResponse
```

### DeleteApiClientsIpAllowDomainDomainResponse

```typescript
{ "data": DeleteApiClientsIpAllowDomainDomainData; "error": null }
```

### DeleteApiClientsIpAllowDomainDomainVariant1Data

```typescript
SuccessResponse
```

### DeleteApiClientsIpAllowlistsListIdData

```typescript
SuccessResponse
```

### DeleteApiClientsIpAllowlistsListIdResponse

```typescript
{ "data": DeleteApiClientsIpAllowlistsListIdData; "error": null }
```

### DeleteApiClientsIpAllowlistsListIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiClientsIpBlockDomainDomainData

```typescript
SuccessResponse
```

### DeleteApiClientsIpBlockDomainDomainResponse

```typescript
{ "data": DeleteApiClientsIpBlockDomainDomainData; "error": null }
```

### DeleteApiClientsIpBlockDomainDomainVariant1Data

```typescript
SuccessResponse
```

### DeleteApiClientsIpBlocklistsListIdData

```typescript
SuccessResponse
```

### DeleteApiClientsIpBlocklistsListIdResponse

```typescript
{ "data": DeleteApiClientsIpBlocklistsListIdData; "error": null }
```

### DeleteApiClientsIpBlocklistsListIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiClientsIpGroupsGroupIdData

```typescript
SuccessResponse
```

### DeleteApiClientsIpGroupsGroupIdResponse

```typescript
{ "data": DeleteApiClientsIpGroupsGroupIdData; "error": null }
```

### DeleteApiClientsIpGroupsGroupIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiClientsIpPolicyData

```typescript
SuccessResponse
```

### DeleteApiClientsIpPolicyResponse

```typescript
{ "data": DeleteApiClientsIpPolicyData; "error": null }
```

### DeleteApiClientsIpPolicyVariant1Data

```typescript
SuccessResponse
```

### DeleteApiGroupsIdAllowDomainDomainData

```typescript
SuccessResponse
```

### DeleteApiGroupsIdAllowDomainDomainResponse

```typescript
{ "data": DeleteApiGroupsIdAllowDomainDomainData; "error": null }
```

### DeleteApiGroupsIdAllowDomainDomainVariant1Data

```typescript
SuccessResponse
```

### DeleteApiGroupsIdAllowlistsListIdData

```typescript
SuccessResponse
```

### DeleteApiGroupsIdAllowlistsListIdResponse

```typescript
{ "data": DeleteApiGroupsIdAllowlistsListIdData; "error": null }
```

### DeleteApiGroupsIdAllowlistsListIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiGroupsIdBlockDomainDomainData

```typescript
SuccessResponse
```

### DeleteApiGroupsIdBlockDomainDomainResponse

```typescript
{ "data": DeleteApiGroupsIdBlockDomainDomainData; "error": null }
```

### DeleteApiGroupsIdBlockDomainDomainVariant1Data

```typescript
SuccessResponse
```

### DeleteApiGroupsIdBlocklistsListIdData

```typescript
SuccessResponse
```

### DeleteApiGroupsIdBlocklistsListIdResponse

```typescript
{ "data": DeleteApiGroupsIdBlocklistsListIdData; "error": null }
```

### DeleteApiGroupsIdBlocklistsListIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiGroupsIdData

```typescript
SuccessResponse
```

### DeleteApiGroupsIdMembersIpData

```typescript
SuccessResponse
```

### DeleteApiGroupsIdMembersIpResponse

```typescript
{ "data": DeleteApiGroupsIdMembersIpData; "error": null }
```

### DeleteApiGroupsIdMembersIpVariant1Data

```typescript
SuccessResponse
```

### DeleteApiGroupsIdPolicyData

```typescript
SuccessResponse
```

### DeleteApiGroupsIdPolicyResponse

```typescript
{ "data": DeleteApiGroupsIdPolicyData; "error": null }
```

### DeleteApiGroupsIdPolicyVariant1Data

```typescript
SuccessResponse
```

### DeleteApiGroupsIdResponse

```typescript
{ "data": DeleteApiGroupsIdData; "error": null }
```

### DeleteApiGroupsIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiPeersUrlData

```typescript
SuccessResponse
```

### DeleteApiPeersUrlResponse

```typescript
{ "data": DeleteApiPeersUrlData; "error": null }
```

### DeleteApiPeersUrlVariant1Data

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdAllowDomainDomainData

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdAllowDomainDomainResponse

```typescript
{ "data": DeleteApiPoliciesIdAllowDomainDomainData; "error": null }
```

### DeleteApiPoliciesIdAllowDomainDomainVariant1Data

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdAllowlistsListIdData

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdAllowlistsListIdResponse

```typescript
{ "data": DeleteApiPoliciesIdAllowlistsListIdData; "error": null }
```

### DeleteApiPoliciesIdAllowlistsListIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdBlockDomainDomainData

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdBlockDomainDomainResponse

```typescript
{ "data": DeleteApiPoliciesIdBlockDomainDomainData; "error": null }
```

### DeleteApiPoliciesIdBlockDomainDomainVariant1Data

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdBlocklistsListIdData

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdBlocklistsListIdResponse

```typescript
{ "data": DeleteApiPoliciesIdBlocklistsListIdData; "error": null }
```

### DeleteApiPoliciesIdBlocklistsListIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdData

```typescript
SuccessResponse
```

### DeleteApiPoliciesIdResponse

```typescript
{ "data": DeleteApiPoliciesIdData; "error": null }
```

### DeleteApiPoliciesIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiRangesIdAllowDomainDomainData

```typescript
SuccessResponse
```

### DeleteApiRangesIdAllowDomainDomainResponse

```typescript
{ "data": DeleteApiRangesIdAllowDomainDomainData; "error": null }
```

### DeleteApiRangesIdAllowDomainDomainVariant1Data

```typescript
SuccessResponse
```

### DeleteApiRangesIdAllowlistsListIdData

```typescript
SuccessResponse
```

### DeleteApiRangesIdAllowlistsListIdResponse

```typescript
{ "data": DeleteApiRangesIdAllowlistsListIdData; "error": null }
```

### DeleteApiRangesIdAllowlistsListIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiRangesIdBlockDomainDomainData

```typescript
SuccessResponse
```

### DeleteApiRangesIdBlockDomainDomainResponse

```typescript
{ "data": DeleteApiRangesIdBlockDomainDomainData; "error": null }
```

### DeleteApiRangesIdBlockDomainDomainVariant1Data

```typescript
SuccessResponse
```

### DeleteApiRangesIdBlocklistsListIdData

```typescript
SuccessResponse
```

### DeleteApiRangesIdBlocklistsListIdResponse

```typescript
{ "data": DeleteApiRangesIdBlocklistsListIdData; "error": null }
```

### DeleteApiRangesIdBlocklistsListIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiRangesIdData

```typescript
SuccessResponse
```

### DeleteApiRangesIdPolicyData

```typescript
SuccessResponse
```

### DeleteApiRangesIdPolicyResponse

```typescript
{ "data": DeleteApiRangesIdPolicyData; "error": null }
```

### DeleteApiRangesIdPolicyVariant1Data

```typescript
SuccessResponse
```

### DeleteApiRangesIdResponse

```typescript
{ "data": DeleteApiRangesIdData; "error": null }
```

### DeleteApiRangesIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiRewritesIdData

```typescript
SuccessResponse
```

### DeleteApiRewritesIdResponse

```typescript
{ "data": DeleteApiRewritesIdData; "error": null }
```

### DeleteApiRewritesIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiTokensIdData

```typescript
SuccessResponse
```

### DeleteApiTokensIdResponse

```typescript
{ "data": DeleteApiTokensIdData; "error": null }
```

### DeleteApiTokensIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiUpstreamsIdData

```typescript
SuccessResponse
```

### DeleteApiUpstreamsIdResponse

```typescript
{ "data": DeleteApiUpstreamsIdData; "error": null }
```

### DeleteApiUpstreamsIdVariant1Data

```typescript
SuccessResponse
```

### DeleteApiUsersIdData

```typescript
SuccessResponse
```

### DeleteApiUsersIdResponse

```typescript
{ "data": DeleteApiUsersIdData; "error": null }
```

### DeleteApiUsersIdVariant1Data

```typescript
SuccessResponse
```

### Diagnostic

```typescript
{ "line": number; "reason": string; "rule": string }
```

### Document

```typescript
{ "components": Components; "info": Info; "openapi": string; "paths": ({ [key: string]: ({ [key: string]: Endpoint } | null) } | null) }
```

### DomainsResponse

```typescript
{ "count": number; "domains": (Array<string> | null); "source": string }
```

### Endpoint

```typescript
{ "description": string; "operationId": string; "parameters"?: Array<Parameter>; "requestBody"?: RequestBody; "responses": ({ [key: string]: Response } | null); "security": (Array<({ [key: string]: (Array<string> | null) } | null)> | null); "summary": string }
```

### EntityResult

```typescript
{ "custom_rule"?: CustomHit; "name": string; "published_list"?: PublishedHit; "result": string; "tier": string }
```

### GetApiAllowlistsData

```typescript
(Array<AllowlistView> | null)
```

### GetApiAllowlistsIdCompatibilityData

```typescript
ListCompatibilityPage
```

### GetApiAllowlistsIdCompatibilityResponse

```typescript
{ "data": GetApiAllowlistsIdCompatibilityData; "error": null }
```

### GetApiAllowlistsIdCompatibilityVariant1Data

```typescript
ListCompatibilityPage
```

### GetApiAllowlistsIdDomainsData

```typescript
AllowlistDomainsPage
```

### GetApiAllowlistsIdDomainsResponse

```typescript
{ "data": GetApiAllowlistsIdDomainsData; "error": null }
```

### GetApiAllowlistsIdDomainsVariant1Data

```typescript
AllowlistDomainsPage
```

### GetApiAllowlistsResponse

```typescript
{ "data": GetApiAllowlistsData; "error": null }
```

### GetApiAllowlistsVariant1Data

```typescript
(Array<AllowlistView> | null)
```

### GetApiAnalysisDomainsData

```typescript
DomainsResponse
```

### GetApiAnalysisDomainsResponse

```typescript
{ "data": GetApiAnalysisDomainsData; "error": null }
```

### GetApiAnalysisDomainsVariant1Data

```typescript
DomainsResponse
```

### GetApiArchiveStatusData

```typescript
ArchiveStatusView
```

### GetApiArchiveStatusResponse

```typescript
{ "data": GetApiArchiveStatusData; "error": null }
```

### GetApiArchiveStatusVariant1Data

```typescript
ArchiveStatusView
```

### GetApiAuthCheckData

```typescript
(AuthenticatedIdentity | UnauthenticatedIdentity)
```

### GetApiAuthCheckResponse

```typescript
{ "data": GetApiAuthCheckData; "error": null }
```

### GetApiAuthCheckVariant1Data

```typescript
AuthenticatedIdentity
```

### GetApiAuthCheckVariant2Data

```typescript
UnauthenticatedIdentity
```

### GetApiBlocklistsData

```typescript
(Array<BlocklistView> | null)
```

### GetApiBlocklistsHistoryData

```typescript
(Array<BlocklistHistoryView> | null)
```

### GetApiBlocklistsHistoryHistoryIdDomainsData

```typescript
CheckpointPage
```

### GetApiBlocklistsHistoryHistoryIdDomainsResponse

```typescript
{ "data": GetApiBlocklistsHistoryHistoryIdDomainsData; "error": null }
```

### GetApiBlocklistsHistoryHistoryIdDomainsVariant1Data

```typescript
CheckpointPage
```

### GetApiBlocklistsHistoryResponse

```typescript
{ "data": GetApiBlocklistsHistoryData; "error": null }
```

### GetApiBlocklistsHistoryVariant1Data

```typescript
(Array<BlocklistHistoryView> | null)
```

### GetApiBlocklistsIdCompatibilityData

```typescript
ListCompatibilityPage
```

### GetApiBlocklistsIdCompatibilityResponse

```typescript
{ "data": GetApiBlocklistsIdCompatibilityData; "error": null }
```

### GetApiBlocklistsIdCompatibilityVariant1Data

```typescript
ListCompatibilityPage
```

### GetApiBlocklistsIdDomainsData

```typescript
BlocklistDomainsPage
```

### GetApiBlocklistsIdDomainsResponse

```typescript
{ "data": GetApiBlocklistsIdDomainsData; "error": null }
```

### GetApiBlocklistsIdDomainsVariant1Data

```typescript
BlocklistDomainsPage
```

### GetApiBlocklistsResponse

```typescript
{ "data": GetApiBlocklistsData; "error": null }
```

### GetApiBlocklistsUniqueDomainsData

```typescript
BlocklistUniqueStats
```

### GetApiBlocklistsUniqueDomainsResponse

```typescript
{ "data": GetApiBlocklistsUniqueDomainsData; "error": null }
```

### GetApiBlocklistsUniqueDomainsVariant1Data

```typescript
BlocklistUniqueStats
```

### GetApiBlocklistsVariant1Data

```typescript
(Array<BlocklistView> | null)
```

### GetApiBootstrapData

```typescript
(Array<BootstrapServerView> | null)
```

### GetApiBootstrapResponse

```typescript
{ "data": GetApiBootstrapData; "error": null }
```

### GetApiBootstrapVariant1Data

```typescript
(Array<BootstrapServerView> | null)
```

### GetApiClientsData

```typescript
(Array<Client> | null)
```

### GetApiClientsIpData

```typescript
ClientDetailView
```

### GetApiClientsIpResponse

```typescript
{ "data": GetApiClientsIpData; "error": null }
```

### GetApiClientsIpVariant1Data

```typescript
ClientDetailView
```

### GetApiClientsResponse

```typescript
{ "data": GetApiClientsData; "error": null }
```

### GetApiClientsVariant1Data

```typescript
(Array<Client> | null)
```

### GetApiConfigExportData

```typescript
ConfigExport
```

### GetApiConfigExportResponse

```typescript
{ "data": GetApiConfigExportData; "error": null }
```

### GetApiConfigExportVariant1Data

```typescript
ConfigExport
```

### GetApiGroupsData

```typescript
(Array<Group> | null)
```

### GetApiGroupsIdData

```typescript
GroupDetailView
```

### GetApiGroupsIdResponse

```typescript
{ "data": GetApiGroupsIdData; "error": null }
```

### GetApiGroupsIdVariant1Data

```typescript
GroupDetailView
```

### GetApiGroupsResponse

```typescript
{ "data": GetApiGroupsData; "error": null }
```

### GetApiGroupsVariant1Data

```typescript
(Array<Group> | null)
```

### GetApiInvestigateSchemaData

```typescript
InvestigationSchemaView
```

### GetApiInvestigateSchemaResponse

```typescript
{ "data": GetApiInvestigateSchemaData; "error": null }
```

### GetApiInvestigateSchemaVariant1Data

```typescript
InvestigationSchemaView
```

### GetApiPeersData

```typescript
PeersView
```

### GetApiPeersResponse

```typescript
{ "data": GetApiPeersData; "error": null }
```

### GetApiPeersVariant1Data

```typescript
PeersView
```

### GetApiPoliciesData

```typescript
(Array<PolicyView> | null)
```

### GetApiPoliciesIdData

```typescript
PolicyDetailView
```

### GetApiPoliciesIdResponse

```typescript
{ "data": GetApiPoliciesIdData; "error": null }
```

### GetApiPoliciesIdVariant1Data

```typescript
PolicyDetailView
```

### GetApiPoliciesResponse

```typescript
{ "data": GetApiPoliciesData; "error": null }
```

### GetApiPoliciesVariant1Data

```typescript
(Array<PolicyView> | null)
```

### GetApiPolicyEvaluateData

```typescript
PolicyResult
```

### GetApiPolicyEvaluateResponse

```typescript
{ "data": GetApiPolicyEvaluateData; "error": null }
```

### GetApiPolicyEvaluateVariant1Data

```typescript
PolicyResult
```

### GetApiQueryLogsData

```typescript
QueryLogPage
```

### GetApiQueryLogsIdData

```typescript
QueryLogDetailView
```

### GetApiQueryLogsIdResponse

```typescript
{ "data": GetApiQueryLogsIdData; "error": null }
```

### GetApiQueryLogsIdVariant1Data

```typescript
QueryLogDetailView
```

### GetApiQueryLogsResponse

```typescript
{ "data": GetApiQueryLogsData; "error": null }
```

### GetApiQueryLogsVariant1Data

```typescript
QueryLogPage
```

### GetApiRangesData

```typescript
(Array<RangeView> | null)
```

### GetApiRangesIdData

```typescript
RangeDetailView
```

### GetApiRangesIdResponse

```typescript
{ "data": GetApiRangesIdData; "error": null }
```

### GetApiRangesIdVariant1Data

```typescript
RangeDetailView
```

### GetApiRangesResponse

```typescript
{ "data": GetApiRangesData; "error": null }
```

### GetApiRangesVariant1Data

```typescript
(Array<RangeView> | null)
```

### GetApiRewritesData

```typescript
(Array<RewriteView> | null)
```

### GetApiRewritesResponse

```typescript
{ "data": GetApiRewritesData; "error": null }
```

### GetApiRewritesStatsData

```typescript
(Array<RewriteStatsView> | null)
```

### GetApiRewritesStatsResponse

```typescript
{ "data": GetApiRewritesStatsData; "error": null }
```

### GetApiRewritesStatsVariant1Data

```typescript
(Array<RewriteStatsView> | null)
```

### GetApiRewritesVariant1Data

```typescript
(Array<RewriteView> | null)
```

### GetApiSettingsData

```typescript
({ [key: string]: string } | null)
```

### GetApiSettingsResponse

```typescript
{ "data": GetApiSettingsData; "error": null }
```

### GetApiSettingsVariant1Data

```typescript
({ [key: string]: string } | null)
```

### GetApiSetupData

```typescript
SetupStatus
```

### GetApiSetupResponse

```typescript
{ "data": GetApiSetupData; "error": null }
```

### GetApiSetupVariant1Data

```typescript
SetupStatus
```

### GetApiStatsBlockSourcesData

```typescript
(Array<DashboardBlockSource> | null)
```

### GetApiStatsBlockSourcesResponse

```typescript
{ "data": GetApiStatsBlockSourcesData; "error": null }
```

### GetApiStatsBlockSourcesVariant1Data

```typescript
(Array<DashboardBlockSource> | null)
```

### GetApiStatsDashboardData

```typescript
DashboardSnapshot
```

### GetApiStatsDashboardResponse

```typescript
{ "data": GetApiStatsDashboardData; "error": null }
```

### GetApiStatsDashboardVariant1Data

```typescript
DashboardSnapshot
```

### GetApiStatsData

```typescript
DNSStatsResponse
```

### GetApiStatsLatencyData

```typescript
(Array<DashboardLatencyPoint> | null)
```

### GetApiStatsLatencyResponse

```typescript
{ "data": GetApiStatsLatencyData; "error": null }
```

### GetApiStatsLatencyVariant1Data

```typescript
(Array<DashboardLatencyPoint> | null)
```

### GetApiStatsResponse

```typescript
{ "data": GetApiStatsData; "error": null }
```

### GetApiStatsServfailsData

```typescript
(Array<DashboardServfailClient> | null)
```

### GetApiStatsServfailsResponse

```typescript
{ "data": GetApiStatsServfailsData; "error": null }
```

### GetApiStatsServfailsVariant1Data

```typescript
(Array<DashboardServfailClient> | null)
```

### GetApiStatsSystemData

```typescript
(Array<SystemSample> | null)
```

### GetApiStatsSystemResponse

```typescript
{ "data": GetApiStatsSystemData; "error": null }
```

### GetApiStatsSystemVariant1Data

```typescript
(Array<SystemSample> | null)
```

### GetApiStatsTimeseriesData

```typescript
(DashboardSummary | (Array<DashboardTimeseriesPoint> | null))
```

### GetApiStatsTimeseriesResponse

```typescript
{ "data": GetApiStatsTimeseriesData; "error": null }
```

### GetApiStatsTimeseriesVariant1Data

```typescript
DashboardSummary
```

### GetApiStatsTimeseriesVariant2Data

```typescript
(Array<DashboardTimeseriesPoint> | null)
```

### GetApiStatsTopClientsData

```typescript
(Array<DashboardTopClient> | null)
```

### GetApiStatsTopClientsResponse

```typescript
{ "data": GetApiStatsTopClientsData; "error": null }
```

### GetApiStatsTopClientsVariant1Data

```typescript
(Array<DashboardTopClient> | null)
```

### GetApiStatsTopDomainsData

```typescript
DashboardTopDomains
```

### GetApiStatsTopDomainsResponse

```typescript
{ "data": GetApiStatsTopDomainsData; "error": null }
```

### GetApiStatsTopDomainsVariant1Data

```typescript
DashboardTopDomains
```

### GetApiStatsUpstreamUsageData

```typescript
(Array<DashboardUpstreamUsage> | null)
```

### GetApiStatsUpstreamUsageResponse

```typescript
{ "data": GetApiStatsUpstreamUsageData; "error": null }
```

### GetApiStatsUpstreamUsageVariant1Data

```typescript
(Array<DashboardUpstreamUsage> | null)
```

### GetApiStatsVariant1Data

```typescript
DNSStatsResponse
```

### GetApiSyncData

```typescript
SyncResponse
```

### GetApiSyncResponse

```typescript
{ "data": GetApiSyncData; "error": null }
```

### GetApiSyncVariant1Data

```typescript
SyncResponse
```

### GetApiTokensData

```typescript
(Array<APIToken> | null)
```

### GetApiTokensResponse

```typescript
{ "data": GetApiTokensData; "error": null }
```

### GetApiTokensVariant1Data

```typescript
(Array<APIToken> | null)
```

### GetApiUpstreamsData

```typescript
(Array<Upstream> | null)
```

### GetApiUpstreamsResponse

```typescript
{ "data": GetApiUpstreamsData; "error": null }
```

### GetApiUpstreamsVariant1Data

```typescript
(Array<Upstream> | null)
```

### GetApiUsersData

```typescript
(Array<AdminUser> | null)
```

### GetApiUsersResponse

```typescript
{ "data": GetApiUsersData; "error": null }
```

### GetApiUsersVariant1Data

```typescript
(Array<AdminUser> | null)
```

### GetHealthData

```typescript
HealthResponse
```

### GetHealthResponse

```typescript
{ "data": GetHealthData; "error": null }
```

### GetHealthVariant1Data

```typescript
HealthResponse
```

### Group

```typescript
{ "blocklist_count": number; "id": number; "is_member": boolean; "member_count": number; "name": string }
```

### GroupDetailView

```typescript
{ "avg_latency_microseconds": number; "blocklist_stats": BlocklistUniqueStats; "blocklists": (Array<BlocklistAssignment> | null); "custom_allowed": (Array<string> | null); "custom_blocked": (Array<string> | null); "first_seen": string; "id": number; "last_seen": string; "members": (Array<Member> | null); "name": string; "policy"?: PolicyReference; "recent_logs": (Array<RecentLogView> | null); "total_queries": number }
```

### GroupExport

```typescript
{ "allowlists": (Array<string> | null); "blocklists": (Array<string> | null); "members": (Array<string> | null); "name": string; "policy"?: string }
```

### GroupExportInput

```typescript
{ "allowlists"?: (Array<string> | null); "blocklists"?: (Array<string> | null); "members"?: (Array<string> | null); "name"?: (string | null); "policy"?: (string | null) }
```

### HealthResponse

```typescript
{ "status": string }
```

### Info

```typescript
{ "description": string; "title": string; "version": string }
```

### InvestigationColumn

```typescript
{ "name": string; "type": string }
```

### InvestigationSchemaTable

```typescript
{ "columns": (Array<InvestigationColumn> | null); "name": string; "type": string }
```

### InvestigationSchemaView

```typescript
{ "tables": (Array<InvestigationSchemaTable> | null) }
```

### InvestigationView

```typescript
{ "columns": (Array<string> | null); "duration_ms": number; "row_count": number; "rows": (Array<(Array<JSONValue> | null)> | null) }
```

### JSONValue

```typescript
(null | boolean | number | string | Array<JSONValue> | { [key: string]: JSONValue })
```

### ListCompatibilityPage

```typescript
{ "diagnostics": (Array<Diagnostic> | null); "limit": number; "offset": number; "summary": ListCompatibilitySummary; "total": number }
```

### ListCompatibilitySummary

```typescript
{ "applied": number; "assessed": boolean; "assessed_at": string; "diagnostic_count": number; "invalid": number; "lines": number; "unsupported": number }
```

### ListExport

```typescript
{ "alias": string; "domains"?: Array<string>; "enabled": boolean; "refresh_interval": number; "url": string }
```

### ListExportInput

```typescript
{ "alias"?: (string | null); "domains"?: (Array<string> | null); "enabled"?: (boolean | null); "refresh_interval"?: (number | null); "url"?: (string | null) }
```

### MatrixListInfo

```typescript
{ "alias": string; "domain_count": number; "id": number }
```

### MatrixResponse

```typescript
{ "domains": (Array<string> | null); "lists": (Array<MatrixListInfo> | null); "matched_rules": (Array<(Array<(string | null)> | null)> | null); "matrix": (Array<(Array<boolean> | null)> | null); "record_type": number }
```

### Media

```typescript
{ "example"?: JSONValue; "schema": (Schema | null) }
```

### Member

```typescript
{ "alias": string; "ip_address": string; "last_seen": string; "query_count": number }
```

### PairingCodeResponse

```typescript
{ "expires_in": string; "node_id": string; "pairing_code": string; "self_url": string }
```

### Parameter

```typescript
{ "in": string; "name": string; "required": boolean; "schema": (Schema | null) }
```

### PeerView

```typescript
{ "changes_24h": number; "consecutive_errors": number; "healthy": boolean; "last_error"?: string; "last_sync_at": string; "node_name"?: string; "url": string }
```

### PeersView

```typescript
{ "has_secret": boolean; "node_id": string; "node_name": string; "peers": (Array<PeerView> | null); "replicate_identity": boolean; "sync_interval": string; "sync_tls_error"?: string; "sync_tls_ready"?: boolean; "tls_configured": boolean }
```

### PolicyAllowlistView

```typescript
{ "alias": string; "domain_count": number; "id": number; "is_assigned": boolean; "url": string }
```

### PolicyBlocklistView

```typescript
{ "alias": string; "domain_count": number; "id": number; "is_assigned": boolean; "url": string }
```

### PolicyDetailView

```typescript
{ "allowlists": (Array<PolicyAllowlistView> | null); "assigned_to": AssignedEntities; "blocklist_stats": BlocklistUniqueStats; "blocklists": (Array<PolicyBlocklistView> | null); "custom_allowed": (Array<string> | null); "custom_blocked": (Array<string> | null); "description": string; "id": number; "name": string }
```

### PolicyExport

```typescript
{ "allowlists": (Array<string> | null); "blocklists": (Array<string> | null); "description": string; "name": string }
```

### PolicyExportInput

```typescript
{ "allowlists"?: (Array<string> | null); "blocklists"?: (Array<string> | null); "description"?: (string | null); "name"?: (string | null) }
```

### PolicyReference

```typescript
{ "id": number; "name": string }
```

### PolicyResult

```typescript
{ "client_ip": string; "domain": string; "group_evaluation"?: TierEvaluation; "ip_evaluation"?: TierEvaluation; "range_evaluation"?: TierEvaluation; "record_type": number; "result": string; "result_source"?: EntityResult }
```

### PolicyView

```typescript
{ "allowlist_count": number; "blocklist_count": number; "description": string; "id": number; "name": string; "usage_count": number }
```

### PostApiAllowlistsBody

```typescript
{ "alias"?: (string | null); "enabled"?: (boolean | null); "refresh_interval"?: (number | null); "url"?: (string | null) }
```

### PostApiAllowlistsData

```typescript
CreateAllowlistResponse
```

### PostApiAllowlistsIdRefreshData

```typescript
SuccessResponse
```

### PostApiAllowlistsIdRefreshResponse

```typescript
{ "data": PostApiAllowlistsIdRefreshData; "error": null }
```

### PostApiAllowlistsIdRefreshVariant1Data

```typescript
SuccessResponse
```

### PostApiAllowlistsIdToggleData

```typescript
SuccessResponse
```

### PostApiAllowlistsIdToggleResponse

```typescript
{ "data": PostApiAllowlistsIdToggleData; "error": null }
```

### PostApiAllowlistsIdToggleVariant1Data

```typescript
SuccessResponse
```

### PostApiAllowlistsResponse

```typescript
{ "data": PostApiAllowlistsData; "error": null }
```

### PostApiAllowlistsVariant1Data

```typescript
CreateAllowlistResponse
```

### PostApiAnalysisCompareBody

```typescript
{ "blocklist_ids": Array<number> }
```

### PostApiAnalysisCompareData

```typescript
CompareResponse
```

### PostApiAnalysisCompareResponse

```typescript
{ "data": PostApiAnalysisCompareData; "error": null }
```

### PostApiAnalysisCompareVariant1Data

```typescript
CompareResponse
```

### PostApiAnalysisMatrixBody

```typescript
{ "blocklist_ids"?: (Array<number> | null); "domains": Array<string>; "type"?: (string | null) }
```

### PostApiAnalysisMatrixData

```typescript
MatrixResponse
```

### PostApiAnalysisMatrixResponse

```typescript
{ "data": PostApiAnalysisMatrixData; "error": null }
```

### PostApiAnalysisMatrixVariant1Data

```typescript
MatrixResponse
```

### PostApiAnalysisSimulateBody

```typescript
{ "client_ip": string; "domains": Array<string>; "type"?: (string | null) }
```

### PostApiAnalysisSimulateData

```typescript
SimulateResponse
```

### PostApiAnalysisSimulateResponse

```typescript
{ "data": PostApiAnalysisSimulateData; "error": null }
```

### PostApiAnalysisSimulateVariant1Data

```typescript
SimulateResponse
```

### PostApiArchiveData

```typescript
ArchiveStatusView
```

### PostApiArchiveResponse

```typescript
{ "data": PostApiArchiveData; "error": null }
```

### PostApiArchiveVariant1Data

```typescript
ArchiveStatusView
```

### PostApiAuthLoginBody

```typescript
{ "password": string; "username": string }
```

### PostApiAuthLoginData

```typescript
AuthenticatedIdentity
```

### PostApiAuthLoginResponse

```typescript
{ "data": PostApiAuthLoginData; "error": null }
```

### PostApiAuthLoginVariant1Data

```typescript
AuthenticatedIdentity
```

### PostApiAuthLogoutData

```typescript
SuccessResponse
```

### PostApiAuthLogoutResponse

```typescript
{ "data": PostApiAuthLogoutData; "error": null }
```

### PostApiAuthLogoutVariant1Data

```typescript
SuccessResponse
```

### PostApiAuthSessionsRevokeData

```typescript
SuccessResponse
```

### PostApiAuthSessionsRevokeResponse

```typescript
{ "data": PostApiAuthSessionsRevokeData; "error": null }
```

### PostApiAuthSessionsRevokeVariant1Data

```typescript
SuccessResponse
```

### PostApiBlocklistsBody

```typescript
{ "alias"?: (string | null); "enabled"?: (boolean | null); "refresh_interval"?: (number | null); "url"?: (string | null) }
```

### PostApiBlocklistsData

```typescript
CreateBlocklistResponse
```

### PostApiBlocklistsIdRefreshData

```typescript
SuccessResponse
```

### PostApiBlocklistsIdRefreshResponse

```typescript
{ "data": PostApiBlocklistsIdRefreshData; "error": null }
```

### PostApiBlocklistsIdRefreshVariant1Data

```typescript
SuccessResponse
```

### PostApiBlocklistsIdToggleData

```typescript
SuccessResponse
```

### PostApiBlocklistsIdToggleResponse

```typescript
{ "data": PostApiBlocklistsIdToggleData; "error": null }
```

### PostApiBlocklistsIdToggleVariant1Data

```typescript
SuccessResponse
```

### PostApiBlocklistsResponse

```typescript
{ "data": PostApiBlocklistsData; "error": null }
```

### PostApiBlocklistsVariant1Data

```typescript
CreateBlocklistResponse
```

### PostApiBootstrapBody

```typescript
{ "server": string }
```

### PostApiBootstrapData

```typescript
SuccessResponse
```

### PostApiBootstrapResponse

```typescript
{ "data": PostApiBootstrapData; "error": null }
```

### PostApiBootstrapVariant1Data

```typescript
SuccessResponse
```

### PostApiCacheClearData

```typescript
SuccessResponse
```

### PostApiCacheClearResponse

```typescript
{ "data": PostApiCacheClearData; "error": null }
```

### PostApiCacheClearVariant1Data

```typescript
SuccessResponse
```

### PostApiClientsIpAllowDomainBody

```typescript
{ "domain": string }
```

### PostApiClientsIpAllowDomainData

```typescript
SuccessResponse
```

### PostApiClientsIpAllowDomainResponse

```typescript
{ "data": PostApiClientsIpAllowDomainData; "error": null }
```

### PostApiClientsIpAllowDomainVariant1Data

```typescript
SuccessResponse
```

### PostApiClientsIpAllowlistsListIdData

```typescript
SuccessResponse
```

### PostApiClientsIpAllowlistsListIdResponse

```typescript
{ "data": PostApiClientsIpAllowlistsListIdData; "error": null }
```

### PostApiClientsIpAllowlistsListIdVariant1Data

```typescript
SuccessResponse
```

### PostApiClientsIpBlockDomainBody

```typescript
{ "domain": string }
```

### PostApiClientsIpBlockDomainData

```typescript
SuccessResponse
```

### PostApiClientsIpBlockDomainResponse

```typescript
{ "data": PostApiClientsIpBlockDomainData; "error": null }
```

### PostApiClientsIpBlockDomainVariant1Data

```typescript
SuccessResponse
```

### PostApiClientsIpBlocklistsListIdData

```typescript
SuccessResponse
```

### PostApiClientsIpBlocklistsListIdResponse

```typescript
{ "data": PostApiClientsIpBlocklistsListIdData; "error": null }
```

### PostApiClientsIpBlocklistsListIdVariant1Data

```typescript
SuccessResponse
```

### PostApiClientsIpGroupsGroupIdData

```typescript
SuccessResponse
```

### PostApiClientsIpGroupsGroupIdResponse

```typescript
{ "data": PostApiClientsIpGroupsGroupIdData; "error": null }
```

### PostApiClientsIpGroupsGroupIdVariant1Data

```typescript
SuccessResponse
```

### PostApiClientsIpPolicyPolicyIdData

```typescript
SuccessResponse
```

### PostApiClientsIpPolicyPolicyIdResponse

```typescript
{ "data": PostApiClientsIpPolicyPolicyIdData; "error": null }
```

### PostApiClientsIpPolicyPolicyIdVariant1Data

```typescript
SuccessResponse
```

### PostApiConfigImportBody

```typescript
{ "allowlists"?: (Array<ListExportInput> | null); "blocklists"?: (Array<ListExportInput> | null); "bootstrap_servers"?: (Array<string> | null); "clients"?: (Array<ClientExportInput> | null); "exported_at"?: (string | null); "groups"?: (Array<GroupExportInput> | null); "policies"?: (Array<PolicyExportInput> | null); "ranges"?: (Array<RangeExportInput> | null); "rewrites"?: (Array<RewriteExportInput> | null); "settings"?: ({ [key: string]: string } | null); "upstreams"?: (Array<UpstreamExportInput> | null); "version"?: (string | null) }
```

### PostApiConfigImportData

```typescript
SuccessResponse
```

### PostApiConfigImportResponse

```typescript
{ "data": PostApiConfigImportData; "error": null }
```

### PostApiConfigImportVariant1Data

```typescript
SuccessResponse
```

### PostApiGroupsBody

```typescript
{ "name": string }
```

### PostApiGroupsData

```typescript
CreateGroupResponse
```

### PostApiGroupsIdAllowDomainBody

```typescript
{ "domain": string }
```

### PostApiGroupsIdAllowDomainData

```typescript
SuccessResponse
```

### PostApiGroupsIdAllowDomainResponse

```typescript
{ "data": PostApiGroupsIdAllowDomainData; "error": null }
```

### PostApiGroupsIdAllowDomainVariant1Data

```typescript
SuccessResponse
```

### PostApiGroupsIdAllowlistsListIdData

```typescript
SuccessResponse
```

### PostApiGroupsIdAllowlistsListIdResponse

```typescript
{ "data": PostApiGroupsIdAllowlistsListIdData; "error": null }
```

### PostApiGroupsIdAllowlistsListIdVariant1Data

```typescript
SuccessResponse
```

### PostApiGroupsIdBlockDomainBody

```typescript
{ "domain": string }
```

### PostApiGroupsIdBlockDomainData

```typescript
SuccessResponse
```

### PostApiGroupsIdBlockDomainResponse

```typescript
{ "data": PostApiGroupsIdBlockDomainData; "error": null }
```

### PostApiGroupsIdBlockDomainVariant1Data

```typescript
SuccessResponse
```

### PostApiGroupsIdBlocklistsBatchBody

```typescript
{ "blocklist_ids"?: (Array<number> | null) }
```

### PostApiGroupsIdBlocklistsBatchData

```typescript
AssignGroupBlocklistsResponse
```

### PostApiGroupsIdBlocklistsBatchResponse

```typescript
{ "data": PostApiGroupsIdBlocklistsBatchData; "error": null }
```

### PostApiGroupsIdBlocklistsBatchVariant1Data

```typescript
AssignGroupBlocklistsResponse
```

### PostApiGroupsIdBlocklistsListIdData

```typescript
SuccessResponse
```

### PostApiGroupsIdBlocklistsListIdResponse

```typescript
{ "data": PostApiGroupsIdBlocklistsListIdData; "error": null }
```

### PostApiGroupsIdBlocklistsListIdVariant1Data

```typescript
SuccessResponse
```

### PostApiGroupsIdMembersBatchBody

```typescript
{ "client_ips"?: (Array<string> | null) }
```

### PostApiGroupsIdMembersBatchData

```typescript
AddGroupMembersResponse
```

### PostApiGroupsIdMembersBatchResponse

```typescript
{ "data": PostApiGroupsIdMembersBatchData; "error": null }
```

### PostApiGroupsIdMembersBatchVariant1Data

```typescript
AddGroupMembersResponse
```

### PostApiGroupsIdMembersIpData

```typescript
SuccessResponse
```

### PostApiGroupsIdMembersIpResponse

```typescript
{ "data": PostApiGroupsIdMembersIpData; "error": null }
```

### PostApiGroupsIdMembersIpVariant1Data

```typescript
SuccessResponse
```

### PostApiGroupsIdPolicyPolicyIdData

```typescript
SuccessResponse
```

### PostApiGroupsIdPolicyPolicyIdResponse

```typescript
{ "data": PostApiGroupsIdPolicyPolicyIdData; "error": null }
```

### PostApiGroupsIdPolicyPolicyIdVariant1Data

```typescript
SuccessResponse
```

### PostApiGroupsResponse

```typescript
{ "data": PostApiGroupsData; "error": null }
```

### PostApiGroupsVariant1Data

```typescript
CreateGroupResponse
```

### PostApiInvestigateBody

```typescript
{ "sql": string; "timeout"?: (number | null) }
```

### PostApiInvestigateData

```typescript
InvestigationView
```

### PostApiInvestigateResponse

```typescript
{ "data": PostApiInvestigateData; "error": null }
```

### PostApiInvestigateVariant1Data

```typescript
InvestigationView
```

### PostApiPeersBody

```typescript
{ "url": string }
```

### PostApiPeersConfirmBody

```typescript
{ "pairing_code": string; "peer_url": string }
```

### PostApiPeersConfirmData

```typescript
ConfirmPeerResponse
```

### PostApiPeersConfirmResponse

```typescript
{ "data": PostApiPeersConfirmData; "error": null }
```

### PostApiPeersConfirmVariant1Data

```typescript
ConfirmPeerResponse
```

### PostApiPeersData

```typescript
SuccessResponse
```

### PostApiPeersPairData

```typescript
PairingCodeResponse
```

### PostApiPeersPairResponse

```typescript
{ "data": PostApiPeersPairData; "error": null }
```

### PostApiPeersPairVariant1Data

```typescript
PairingCodeResponse
```

### PostApiPeersResponse

```typescript
{ "data": PostApiPeersData; "error": null }
```

### PostApiPeersVariant1Data

```typescript
SuccessResponse
```

### PostApiPoliciesBody

```typescript
{ "description"?: (string | null); "name": string }
```

### PostApiPoliciesData

```typescript
CreatePolicyResponse
```

### PostApiPoliciesIdAllowDomainBody

```typescript
{ "domain": string }
```

### PostApiPoliciesIdAllowDomainData

```typescript
SuccessResponse
```

### PostApiPoliciesIdAllowDomainResponse

```typescript
{ "data": PostApiPoliciesIdAllowDomainData; "error": null }
```

### PostApiPoliciesIdAllowDomainVariant1Data

```typescript
SuccessResponse
```

### PostApiPoliciesIdAllowlistsListIdData

```typescript
SuccessResponse
```

### PostApiPoliciesIdAllowlistsListIdResponse

```typescript
{ "data": PostApiPoliciesIdAllowlistsListIdData; "error": null }
```

### PostApiPoliciesIdAllowlistsListIdVariant1Data

```typescript
SuccessResponse
```

### PostApiPoliciesIdBlockDomainBody

```typescript
{ "domain": string }
```

### PostApiPoliciesIdBlockDomainData

```typescript
SuccessResponse
```

### PostApiPoliciesIdBlockDomainResponse

```typescript
{ "data": PostApiPoliciesIdBlockDomainData; "error": null }
```

### PostApiPoliciesIdBlockDomainVariant1Data

```typescript
SuccessResponse
```

### PostApiPoliciesIdBlocklistsListIdData

```typescript
SuccessResponse
```

### PostApiPoliciesIdBlocklistsListIdResponse

```typescript
{ "data": PostApiPoliciesIdBlocklistsListIdData; "error": null }
```

### PostApiPoliciesIdBlocklistsListIdVariant1Data

```typescript
SuccessResponse
```

### PostApiPoliciesResponse

```typescript
{ "data": PostApiPoliciesData; "error": null }
```

### PostApiPoliciesVariant1Data

```typescript
CreatePolicyResponse
```

### PostApiRangesBody

```typescript
{ "cidr": string; "name": string }
```

### PostApiRangesData

```typescript
CreateRangeResponse
```

### PostApiRangesIdAllowDomainBody

```typescript
{ "domain": string }
```

### PostApiRangesIdAllowDomainData

```typescript
SuccessResponse
```

### PostApiRangesIdAllowDomainResponse

```typescript
{ "data": PostApiRangesIdAllowDomainData; "error": null }
```

### PostApiRangesIdAllowDomainVariant1Data

```typescript
SuccessResponse
```

### PostApiRangesIdAllowlistsListIdData

```typescript
SuccessResponse
```

### PostApiRangesIdAllowlistsListIdResponse

```typescript
{ "data": PostApiRangesIdAllowlistsListIdData; "error": null }
```

### PostApiRangesIdAllowlistsListIdVariant1Data

```typescript
SuccessResponse
```

### PostApiRangesIdBlockDomainBody

```typescript
{ "domain": string }
```

### PostApiRangesIdBlockDomainData

```typescript
SuccessResponse
```

### PostApiRangesIdBlockDomainResponse

```typescript
{ "data": PostApiRangesIdBlockDomainData; "error": null }
```

### PostApiRangesIdBlockDomainVariant1Data

```typescript
SuccessResponse
```

### PostApiRangesIdBlocklistsListIdData

```typescript
SuccessResponse
```

### PostApiRangesIdBlocklistsListIdResponse

```typescript
{ "data": PostApiRangesIdBlocklistsListIdData; "error": null }
```

### PostApiRangesIdBlocklistsListIdVariant1Data

```typescript
SuccessResponse
```

### PostApiRangesIdPolicyPolicyIdData

```typescript
SuccessResponse
```

### PostApiRangesIdPolicyPolicyIdResponse

```typescript
{ "data": PostApiRangesIdPolicyPolicyIdData; "error": null }
```

### PostApiRangesIdPolicyPolicyIdVariant1Data

```typescript
SuccessResponse
```

### PostApiRangesResponse

```typescript
{ "data": PostApiRangesData; "error": null }
```

### PostApiRangesVariant1Data

```typescript
CreateRangeResponse
```

### PostApiRewritesBatchBody

```typescript
{ "rewrites"?: (Array<CreateRewriteRequestInput> | null) }
```

### PostApiRewritesBatchData

```typescript
(Array<CreatedRewriteView> | null)
```

### PostApiRewritesBatchResponse

```typescript
{ "data": PostApiRewritesBatchData; "error": null }
```

### PostApiRewritesBatchVariant1Data

```typescript
(Array<CreatedRewriteView> | null)
```

### PostApiRewritesBody

```typescript
{ "domain": string; "enabled"?: (boolean | null); "ip_addresses": string }
```

### PostApiRewritesData

```typescript
CreateRewriteResponse
```

### PostApiRewritesResponse

```typescript
{ "data": PostApiRewritesData; "error": null }
```

### PostApiRewritesVariant1Data

```typescript
CreateRewriteResponse
```

### PostApiSetupBody

```typescript
{ "password": string; "token": string; "username": string }
```

### PostApiSetupData

```typescript
AuthenticatedIdentity
```

### PostApiSetupResponse

```typescript
{ "data": PostApiSetupData; "error": null }
```

### PostApiSetupVariant1Data

```typescript
AuthenticatedIdentity
```

### PostApiSyncPairCompleteBody

```typescript
{ "pairing_code": string; "peer_url": string; "proof": string }
```

### PostApiSyncPairCompleteData

```typescript
CompletePairingResponse
```

### PostApiSyncPairCompleteResponse

```typescript
{ "data": PostApiSyncPairCompleteData; "error": null }
```

### PostApiSyncPairCompleteVariant1Data

```typescript
CompletePairingResponse
```

### PostApiTokensBody

```typescript
{ "name": string; "role"?: (string | null) }
```

### PostApiTokensData

```typescript
CreatedAPITokenResponse
```

### PostApiTokensResponse

```typescript
{ "data": PostApiTokensData; "error": null }
```

### PostApiTokensVariant1Data

```typescript
CreatedAPITokenResponse
```

### PostApiUpstreamsBody

```typescript
{ "enabled"?: (boolean | null); "upstream": string }
```

### PostApiUpstreamsData

```typescript
Upstream
```

### PostApiUpstreamsIdToggleData

```typescript
SuccessResponse
```

### PostApiUpstreamsIdToggleResponse

```typescript
{ "data": PostApiUpstreamsIdToggleData; "error": null }
```

### PostApiUpstreamsIdToggleVariant1Data

```typescript
SuccessResponse
```

### PostApiUpstreamsResponse

```typescript
{ "data": PostApiUpstreamsData; "error": null }
```

### PostApiUpstreamsVariant1Data

```typescript
Upstream
```

### PostApiUsersBody

```typescript
{ "password": string; "role"?: (string | null); "username": string }
```

### PostApiUsersData

```typescript
AdminUser
```

### PostApiUsersResponse

```typescript
{ "data": PostApiUsersData; "error": null }
```

### PostApiUsersVariant1Data

```typescript
AdminUser
```

### PublishedHit

```typescript
{ "action": string; "list_id": number; "list_name": string; "rule": string }
```

### PutApiAllowlistsIdBody

```typescript
{ "alias"?: (string | null); "refresh_interval"?: (number | null); "url"?: (string | null) }
```

### PutApiAllowlistsIdData

```typescript
SuccessResponse
```

### PutApiAllowlistsIdResponse

```typescript
{ "data": PutApiAllowlistsIdData; "error": null }
```

### PutApiAllowlistsIdVariant1Data

```typescript
SuccessResponse
```

### PutApiBlocklistsIdBody

```typescript
{ "alias"?: (string | null); "refresh_interval"?: (number | null); "url"?: (string | null) }
```

### PutApiBlocklistsIdData

```typescript
SuccessResponse
```

### PutApiBlocklistsIdResponse

```typescript
{ "data": PutApiBlocklistsIdData; "error": null }
```

### PutApiBlocklistsIdVariant1Data

```typescript
SuccessResponse
```

### PutApiBootstrapBody

```typescript
{ "servers"?: (Array<string> | null) }
```

### PutApiBootstrapData

```typescript
SuccessResponse
```

### PutApiBootstrapResponse

```typescript
{ "data": PutApiBootstrapData; "error": null }
```

### PutApiBootstrapVariant1Data

```typescript
SuccessResponse
```

### PutApiClientsIpAliasBody

```typescript
{ "alias"?: (string | null) }
```

### PutApiClientsIpAliasData

```typescript
SuccessResponse
```

### PutApiClientsIpAliasResponse

```typescript
{ "data": PutApiClientsIpAliasData; "error": null }
```

### PutApiClientsIpAliasVariant1Data

```typescript
SuccessResponse
```

### PutApiGroupsIdBody

```typescript
{ "name": string }
```

### PutApiGroupsIdData

```typescript
SuccessResponse
```

### PutApiGroupsIdResponse

```typescript
{ "data": PutApiGroupsIdData; "error": null }
```

### PutApiGroupsIdVariant1Data

```typescript
SuccessResponse
```

### PutApiPoliciesIdBody

```typescript
{ "description"?: (string | null); "name"?: (string | null) }
```

### PutApiPoliciesIdData

```typescript
SuccessResponse
```

### PutApiPoliciesIdResponse

```typescript
{ "data": PutApiPoliciesIdData; "error": null }
```

### PutApiPoliciesIdVariant1Data

```typescript
SuccessResponse
```

### PutApiRangesIdBody

```typescript
{ "cidr"?: (string | null); "name"?: (string | null) }
```

### PutApiRangesIdData

```typescript
SuccessResponse
```

### PutApiRangesIdResponse

```typescript
{ "data": PutApiRangesIdData; "error": null }
```

### PutApiRangesIdVariant1Data

```typescript
SuccessResponse
```

### PutApiRewritesIdBody

```typescript
{ "domain"?: (string | null); "enabled"?: (boolean | null); "ip_addresses"?: (string | null) }
```

### PutApiRewritesIdData

```typescript
RewriteView
```

### PutApiRewritesIdResponse

```typescript
{ "data": PutApiRewritesIdData; "error": null }
```

### PutApiRewritesIdVariant1Data

```typescript
RewriteView
```

### PutApiSettingsKeyBody

```typescript
{ "value"?: (string | null) }
```

### PutApiSettingsKeyData

```typescript
(SensitiveSettingUpdateResponse | SettingValueResponse)
```

### PutApiSettingsKeyResponse

```typescript
{ "data": PutApiSettingsKeyData; "error": null }
```

### PutApiSettingsKeyVariant1Data

```typescript
SensitiveSettingUpdateResponse
```

### PutApiSettingsKeyVariant2Data

```typescript
SettingValueResponse
```

### PutApiUpstreamsIdBody

```typescript
{ "upstream": string }
```

### PutApiUpstreamsIdData

```typescript
SuccessResponse
```

### PutApiUpstreamsIdResponse

```typescript
{ "data": PutApiUpstreamsIdData; "error": null }
```

### PutApiUpstreamsIdVariant1Data

```typescript
SuccessResponse
```

### PutApiUsersIdBody

```typescript
{ "password"?: (string | null); "role"?: (string | null) }
```

### PutApiUsersIdData

```typescript
SuccessResponse
```

### PutApiUsersIdResponse

```typescript
{ "data": PutApiUsersIdData; "error": null }
```

### PutApiUsersIdVariant1Data

```typescript
SuccessResponse
```

### QueryLogDetailView

```typescript
{ "blocked": boolean; "client_ip": string; "client_name": string; "group_entity": string; "group_is_published": boolean; "group_list_id": number; "group_list_name": string; "group_result": string; "group_rule": string; "id": number; "ip_entity": string; "ip_is_published": boolean; "ip_list_id": number; "ip_list_name": string; "ip_result": string; "ip_rule": string; "latency_microseconds": number; "policy"?: QueryLogPolicySnapshot; "query_name": string; "query_type": string; "range_entity": string; "range_is_published": boolean; "range_list_id": number; "range_list_name": string; "range_result": string; "range_rule": string; "response_code": string; "result": string; "result_entity": string; "result_is_published": boolean; "result_list_id": number; "result_list_name": string; "result_reason": string; "result_rule": string; "result_tier": string; "timestamp": string; "upstream": string }
```

### QueryLogPage

```typescript
{ "limit": number; "logs": (Array<QueryLogView> | null); "offset": number; "total": number }
```

### QueryLogPolicySnapshot

```typescript
{ "group_evaluation"?: TierEvaluation; "ip_evaluation"?: TierEvaluation; "range_evaluation"?: TierEvaluation; "result": string; "result_source"?: EntityResult }
```

### QueryLogView

```typescript
{ "block_list_id"?: number; "block_list_name"?: string; "block_rule"?: string; "block_source"?: string; "block_tier"?: string; "blocked": boolean; "client_alias"?: string; "client_ip": string; "group_name"?: string; "group_result"?: string; "id": number; "ip_entity"?: string; "ip_result"?: string; "latency_microseconds": number; "query_name": string; "query_type": string; "range_name"?: string; "range_result"?: string; "response_code": string; "result"?: string; "result_entity"?: string; "result_is_published"?: boolean; "result_list_id"?: number; "result_list_name"?: string; "result_reason"?: string; "result_rule"?: string; "result_tier"?: string; "timestamp": string; "upstream": string }
```

### RangeDetailView

```typescript
{ "allowlists": (Array<BlocklistAssignment> | null); "avg_latency_microseconds": number; "blocklist_stats": BlocklistUniqueStats; "blocklists": (Array<BlocklistAssignment> | null); "cidr": string; "created_at": string; "custom_allowed": (Array<string> | null); "custom_blocked": (Array<string> | null); "first_seen": string; "id": number; "last_seen": string; "name": string; "policy"?: PolicyReference; "recent_logs": (Array<RecentLogView> | null); "total_queries": number }
```

### RangeExport

```typescript
{ "allowlists": (Array<string> | null); "blocklists": (Array<string> | null); "cidr": string; "name": string; "policy"?: string }
```

### RangeExportInput

```typescript
{ "allowlists"?: (Array<string> | null); "blocklists"?: (Array<string> | null); "cidr"?: (string | null); "name"?: (string | null); "policy"?: (string | null) }
```

### RangeView

```typescript
{ "cidr": string; "created_at": string; "id": number; "name": string }
```

### RecentLogView

```typescript
{ "block_list_name"?: string; "blocked": boolean; "client_alias"?: string; "client_ip": string; "group_name"?: string; "id": number; "latency_microseconds": number; "query_name": string; "query_type": string; "range_name"?: string; "result_entity"?: string; "result_list_name"?: string; "result_reason"?: string; "result_tier"?: string; "timestamp": string; "upstream": string }
```

### RequestBody

```typescript
{ "content": ({ [key: string]: Media } | null); "required": boolean }
```

### Response

```typescript
{ "content": ({ [key: string]: Media } | null); "description": string }
```

### RewriteExport

```typescript
{ "domain": string; "enabled": boolean; "ip_addresses": string }
```

### RewriteExportInput

```typescript
{ "domain"?: (string | null); "enabled"?: (boolean | null); "ip_addresses"?: (string | null) }
```

### RewriteStatsView

```typescript
{ "domain": string; "hits": number; "top_clients"?: Array<RewriteTopClient>; "unique_clients": number }
```

### RewriteTopClient

```typescript
{ "count": number; "ip": string }
```

### RewriteView

```typescript
{ "domain": string; "enabled": boolean; "id": number; "ip_addresses": string }
```

### Schema

```typescript
{ "$ref"?: string; "additionalProperties"?: Schema; "anyOf"?: Array<(Schema | null)>; "description"?: string; "enum"?: Array<string>; "format"?: string; "items"?: Schema; "properties"?: { [key: string]: (Schema | null) }; "required"?: Array<string>; "type"?: string }
```

### SecurityScheme

```typescript
{ "in"?: string; "name"?: string; "scheme"?: string; "type": string }
```

### SensitiveSettingUpdateResponse

```typescript
{ "key": string; "updated": boolean }
```

### SettingValueResponse

```typescript
{ "key": string; "value": string }
```

### SetupStatus

```typescript
{ "addresses"?: Array<string>; "dns_port"?: string; "in_container"?: boolean; "min_password_length"?: number; "required": boolean }
```

### SimulateResponse

```typescript
{ "client_ip": string; "results": (Array<(PolicyResult | null)> | null) }
```

### SuccessResponse

```typescript
{ "success": boolean }
```

### SyncAPIToken

```typescript
{ "created_at": string; "last_used_at": string; "name": string; "node_id": string; "role": string; "token_hash": string; "token_prefix": string; "updated_at": string }
```

### SyncAdminUser

```typescript
{ "created_at": string; "node_id": string; "password_hash": string; "role": string; "updated_at": string; "username": string }
```

### SyncAllowlist

```typescript
{ "alias": string; "enabled": boolean; "node_id": string; "refresh_interval": number; "updated_at": string; "url": string }
```

### SyncBlocklist

```typescript
{ "alias": string; "enabled": boolean; "node_id": string; "refresh_interval": number; "updated_at": string; "url": string }
```

### SyncBootstrap

```typescript
{ "node_id": string; "server": string; "updated_at": string }
```

### SyncChanges

```typescript
{ "admin_users"?: Array<SyncAdminUser>; "allowed_domains"?: Array<SyncManualDomain>; "allowlists"?: Array<SyncAllowlist>; "api_tokens"?: Array<SyncAPIToken>; "blocked_domains"?: Array<SyncManualDomain>; "blocklists"?: Array<SyncBlocklist>; "bootstrap_servers"?: Array<SyncBootstrap>; "client_aliases"?: Array<SyncClientAlias>; "client_allowlists"?: Array<SyncClientList>; "client_blocklists"?: Array<SyncClientList>; "client_policies"?: Array<SyncClientPolicy>; "group_allowlists"?: Array<SyncGroupList>; "group_blocklists"?: Array<SyncGroupList>; "group_members"?: Array<SyncGroupMember>; "groups"?: Array<SyncGroup>; "policies"?: Array<SyncPolicy>; "policy_allowlists"?: Array<SyncPolicyList>; "policy_blocklists"?: Array<SyncPolicyList>; "range_allowlists"?: Array<SyncRangeList>; "range_blocklists"?: Array<SyncRangeList>; "ranges"?: Array<SyncRange>; "rewrites"?: Array<SyncRewrite>; "settings"?: Array<SyncSetting>; "upstreams"?: Array<SyncUpstream> }
```

### SyncClientAlias

```typescript
{ "alias": string; "ip_address": string; "node_id": string; "updated_at": string }
```

### SyncClientList

```typescript
{ "client_ip": string; "list_alias": string; "node_id": string; "updated_at": string }
```

### SyncClientPolicy

```typescript
{ "client_ip": string; "node_id": string; "policy_name": string; "updated_at": string }
```

### SyncGroup

```typescript
{ "name": string; "node_id": string; "policy_name"?: string; "updated_at": string }
```

### SyncGroupList

```typescript
{ "group_name": string; "list_alias": string; "node_id": string; "updated_at": string }
```

### SyncGroupMember

```typescript
{ "client_ip": string; "group_name": string; "node_id": string; "updated_at": string }
```

### SyncManualDomain

```typescript
{ "domain": string; "list_alias": string; "node_id": string; "updated_at": string }
```

### SyncPolicy

```typescript
{ "description": string; "name": string; "node_id": string; "updated_at": string }
```

### SyncPolicyList

```typescript
{ "list_alias": string; "node_id": string; "policy_name": string; "updated_at": string }
```

### SyncRange

```typescript
{ "cidr": string; "name": string; "node_id": string; "policy_name"?: string; "updated_at": string }
```

### SyncRangeList

```typescript
{ "list_alias": string; "node_id": string; "range_cidr": string; "updated_at": string }
```

### SyncResponse

```typescript
{ "changes": SyncChanges; "known_peers"?: Array<string>; "node_id": string; "node_name"?: string; "self_url"?: string; "server_time": string; "tombstones": (Array<Tombstone> | null) }
```

### SyncRewrite

```typescript
{ "domain": string; "enabled": boolean; "ip_addresses": string; "node_id": string; "updated_at": string }
```

### SyncSetting

```typescript
{ "key": string; "node_id": string; "updated_at": string; "value": string }
```

### SyncUpstream

```typescript
{ "enabled": boolean; "node_id": string; "updated_at": string; "upstream": string }
```

### SystemSample

```typescript
{ "cpu_percent": number; "db_size_bytes": number; "heap_alloc": number; "rss_bytes": number; "timestamp": string }
```

### TierEvaluation

```typescript
{ "entities": (Array<EntityResult> | null); "result": string; "result_source"?: EntityResult }
```

### Tombstone

```typescript
{ "deleted_at": string; "natural_key": string; "node_id": string; "table_name": string }
```

### UnauthenticatedIdentity

```typescript
{ "authenticated": boolean; "role": string; "setup_required": boolean; "username": string }
```

### Upstream

```typescript
{ "enabled": boolean; "id": number; "upstream": string }
```

### UpstreamExport

```typescript
{ "enabled": boolean; "upstream": string }
```

### UpstreamExportInput

```typescript
{ "enabled"?: (boolean | null); "upstream"?: (string | null) }
```

