# API failures

Responses preserve the `data` and `error` envelope. Failed responses also include a stable `error_code`, so clients can distinguish failures without matching English text. Success payloads and existing human-readable errors remain compatible.

| HTTP status | Code |
|---|---|
| 400 | `invalid_request` |
| 401 | `unauthorized` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 405 | `method_not_allowed` |
| 408 | `request_canceled` |
| 409 | `conflict` |
| 413 | `request_too_large` |
| 422 | `resource_limit` |
| 429 | `rate_limited` |
| 503 | `unavailable` |
| 504 | `timeout` |
| Other server error | `internal_error` |

Database read failures return HTTP 503 with `data: null`, `error_code: "unavailable"`, and a safe retry message. Internal database details are logged server-side and are not returned to the caller. A failed query, scan, or row iteration never publishes a partial collection or a fabricated zero aggregate. A missing requested record remains 404; inaccessible storage is 503. Empty successful aggregate queries still return zero.

The same rule applies to dashboard sections, list/history pages, client/group/range/bundle details and assignments, query log filters and detail, analysis domain sources, rewrite statistics, users and tokens. Failed login database reads report unavailable rather than invalid credentials. A failed conflict lookup rejects a proposed custom rule instead of treating the opposing rule set as empty.

`api_read_errors_test.go` exercises these boundaries against disposable real SQLite databases: removed tables, malformed values, and view expressions that fail during iteration. `checkpoint_contract_test.go` covers bounded historical reconstruction separately. These errors are unavailable responses, not permission to drop stored data or silently skip rows.
