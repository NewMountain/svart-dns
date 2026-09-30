# DD-001: Blocklist analysis is on-demand, not materialized

**Decision**: Blocklist retroactive analysis ("replay last week of traffic against these 40 lists") runs as an on-demand batch job against query_logs, not as a continuously maintained materialized view.

**Why**: This is a "few times a month" feature, not a hot path. A materialized view that reprocesses every query against every list on every insert would be expensive for something rarely consumed. On-demand means we pay the cost only when we ask the question.

**Revisit if**: The analysis turns out to be cheap enough to maintain continuously, or if we find ourselves running it frequently enough that the latency of on-demand is annoying.
