# List compatibility reports and record-type analysis

Filter Lists keeps its existing Published Lists, History & Changelog, and List
Details tabs. Each published list now shows applied, unsupported, and invalid
counts beneath its source. Expand those counts to read skipped source lines,
their original line numbers, and the reason each was skipped. Previous and Next
retrieve every diagnostic; complete rule text is preserved. A failed read keeps
the displayed page and offers Retry diagnostics.

Applied is the effective stored rule count, matching the list's domain count;
blocklist exceptions are excluded. It is not the number of source lines or the
number of names a regex could match. Unsupported means the entire rule was
skipped, including its restrictions. Invalid means the source could not be
interpreted as a valid supported rule.

A compatibility report belongs to the latest successful local refresh of that
list's current source URL. Older stored lists display “Compatibility not
assessed; refresh this list.” This does not mean all their rules are supported.
A changed source URL invalidates the old source's assessment. Failed refreshes
retain the previous active generation and its report. Each node assesses its own
download; reports do not replicate between peers.

The authenticated API exposes `compatibility` summaries in `GET /api/blocklists`
and `GET /api/allowlists`. Full diagnostics are available through
`GET /api/blocklists/{id}/compatibility` or
`GET /api/allowlists/{id}/compatibility`, using `offset` and `limit` (default 50,
maximum 1000). Read every page until the returned `total` is reached. The response
also identifies the assessment timestamp; refreshes can replace the generation
between requests. An unreadable report is an error, never an all-zero success.

Analysis adds a Record type field to both Blocklist Matrix and Policy Simulator.
It defaults to A, offers common DNS types, and accepts a named type or `TYPE<number>`
for types 1–65535. Responses identify the evaluated numeric `record_type`;
the matrix caption and simulator rows show the type associated with the displayed
result, even if the input has since changed. Simulator decisions include the
matched rule as well as its list and Assignment tier.

API clients may pass `type=TXT` to `GET /api/policy/evaluate`, or a `type` string
in the matrix/simulator JSON request. Omission defaults to A for existing clients.
The type is part of the policy decision and cache identity. These tools evaluate
a supplied name/type; they do not issue an upstream lookup or invent a CNAME chain.
