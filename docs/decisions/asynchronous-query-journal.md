# Asynchronous DNS replies with a recoverable raw query journal

Status: accepted, 2026-09-26.

Svart must keep complete per-query originals while preserving DNS response
latency. A durable-before-reply prototype made local storage flush latency part
of every answer and substantially reduced measured throughput. After reviewing
that tradeoff, Chris chose: “Keep asynchronous replies; prioritize DNS latency.”

A query first enters bounded volatile memory, then the DNS response is sent.
Background batches persist every original field into a FULL SQLite WAL journal.
Queue saturation applies bounded handler backpressure; excess handlers reject
new requests while storage failures retain and retry admitted records. Clean shutdown drains accepted events and
cannot claim success if storage remains unavailable. An abrupt crash can lose
the newest volatile events, including answered queries; that exception to crash
durability is explicit and accepted.

Presentation rows and their raw replay cursor commit atomically. Doom-loop
summaries remain useful UI behavior but do not own or replace original history.
Raw records remain until a verified lossless archive or explicitly configured
original-history expiry owns their retirement. Query-line delivery enters a transactional outbox alongside the presentation
cursor. Direct Loki uses its own durable delivery journal and retires records only after successful receiver acknowledgement.

See [logging durability](../logging-durability.md) for boundaries, metrics,
recovery, measured costs and the separate archive integration requirement.
