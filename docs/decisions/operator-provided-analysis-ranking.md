# Operator-provided Analysis ranking data

Status: accepted.

The embedded third-party ranking dataset blocked an unrestricted public source
release. Analysis still needs ranked domains for its matrix and simulator.

Use an operator-provided local `TOP_SITES_PATH` CSV, read on demand. The endpoint
validates the entire source before returning the requested prefix. Missing or
invalid sources return 503 and the UI shows the error without replacing the
operator's input. File and record budgets reject oversized sources in full.

An on-demand internet download was considered. The repository permits runtime
network downloads only for operator-configured filter lists, and choosing a
ranking publisher on an operator's behalf would also choose its licensing
terms. A local file preserves the feature and the self-hosting rule: operators
obtain an appropriately licensed source themselves and can replace it atomically.

The CSV is removed from the shipped tree. Its original blob remains recoverable
in private development history at `4f20de8:web/data/tranco-top10k.csv`; the public
release must export a fresh history. Tests use an independently generated 10,000
name corpus. Policy golden results were recorded against the unchanged engine
at `4f20de8` before changing ranking-source behavior, then checked again after.

Validation covers valid rankings, normalization, missing and unreadable sources,
malformed records beyond the requested count and beyond row 10,000, oversized
sources and records, read failures, and replacement without restart. A UI test
checks visible errors, preserved input and a successful retry. An authenticated
HTTP check against the built server in a disposable network namespace confirms
absent, valid, malformed and atomically replaced source behavior.
