# DNS-oriented list compatibility with preserved Assignments

Status: accepted, 2026-09-29.

List authors use record-type restrictions and regex exceptions to avoid
blocking legitimate DNS traffic. Removing a restriction changes the rule's
meaning; skipping a regex exception can also make a different rule too broad.
Svart therefore supports a defined DNS-oriented subset and skips unsupported
rules in full. It does not aim to implement a browser filtering engine.

Ordinary domains retain the compact shared index and existing subtree behavior.
Rules needing type predicates, exact anchors, regexes, importance or rule-local
exclusions use versioned stored records retaining the complete source text.
Immutable snapshots compile patterns and extract necessary literals before
publication. A missing required literal skips regex evaluation; the compiled
pattern still decides every possible match. A probe evaluates each candidate
pattern at most once, shares its candidates across entities and keeps overflow
candidates without dropping matches. Cache identity includes the DNS record
type. Response aliases retain their actual RR type; response addresses use A or
AAAA. Regex evaluation uses Go's bounded-backtracking-free engine; total work
still grows with the number of applicable patterns and must be measured.

Exceptions, important priorities and full-rule badfilter cancellation operate
inside their own list. Assignment precedence is unchanged: allow wins inside
one entity, block wins across entities at the same tier, and the narrowest
applicable tier wins. An important list rule cannot override an explicit allow.
Downloaded rule generations remain node-local; sync/import cannot inject
internal stored-rule envelopes through manual domain fields.

Rules, history and the complete compatibility assessment commit together.
An invalid supported regex compilation aborts refresh. Rejected source rules
and reasons are stored completely and paginated only in responses. Legacy
generations remain usable with an unknown assessment until refreshed. A
refresh by a previous binary invalidates the assessment rather than leaving
stale success counts. Original rule text is shown in list pages, searches,
history and policy explanations; internal encoded identity is retained for
checkpoint reconstruction.

This change does not alter asynchronous query logging. Client/tag restrictions,
list-provided DNS rewrites, browser contexts and cross-list cancellation remain
outside the supported subset. See [configuration](../configuration.md#list-syntax)
and [compatibility diagnostics](../list-compatibility.md).
