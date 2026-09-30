# Backend performance evidence — 2026-09-29

The application backend is `ab901e3f41d6cf3a52e8bbf5a106f432d3fdef03`, built with
Go 1.26.8 and production CGO/stripped linker settings. Subsequent mobile layout
and CI changes do not alter these backend measurements. The retained production
baseline is `b67c6f44974220c8c60c8874521d2795adec6663`, built with the same Go
version. These are comparisons of complete builds, not estimates of a single
change's effect.

Binary SHA256 identities:

- Baseline: `2c18fc990d190c39407713db0c812cccf743d7a10bff1bbbac6985cfe9dd4100`.
- Final: `ee54d8dec7c7c5d281d21c56941835c0380be444603136bd5a075d16f8d5466e`.
- DNS generator: `39374d03e521d2f1ff20106fbcda7dcb6e2eb390e34775721cad74f81a40af81`.

The comparison harness is repository revision `2d6ce71`; see
[harness instructions](../../README.md). Cached competitor images were pinned
to these immutable identities:

| Product | Image identity |
|---|---|
| Pi-hole 2026.09.0 | `pihole/pihole@sha256:5b9c8cf51de7d6d3f2240dbe72baf5f06e1fd39cb4d77a99fab4fa13e23bd1be` |
| AdGuard Home v0.107.79 | `adguard/adguardhome@sha256:aba9e3bf0613be3ba3755e1fc311b126e2c24bec25e18b6483894a88283074f0` |
| Technitium 15.5 | `technitium/dns-server@sha256:2502fd4993d18a8c6665ce6eefb20c76ca21918c5435a015d9485b36e8275b5e` |

The host is an AMD Ryzen 9 5950X, Linux x86-64, Btrfs. All workloads ran
sequentially in isolated network namespaces with local cached fixtures and a
local deterministic UDP upstream. Server affinity was physical cores 2–3,
generator core 4 and upstream core 5. Coordinators were capped at four CPUs;
competitors were constrained to server cores 2–3. Cooperating local builds and
other tests were paused. Unrelated user processes were not stopped. Host CPU,
memory and I/O pressure records remain with the private evidence; this is a
shared workstation, not dedicated benchmark hardware.

An unrelated CPU-intensive workload started at **08:02:16 UTC**, overlapping
server cores 2–3. It affected the end of the first Technitium fixed-rate scenario
and the reverse-order product run. Those results are retained and labeled as
contended observations; they must not be averaged with the preceding runs to
claim a controlled product ranking. The earlier paired logging, telemetry,
profiles, three-million-rule and 50-million-row measurements finished before
that workload started. No unrelated process was paused or modified.

These are ordinary application containers. No privileged container, systemd,
host console, graphics device, or host network was used. Comparison coordinators
read host process IDs solely to collect competitor RSS and I/O samples. Logging
and native-query processes ran as UID 1000. Svart in the cross-product comparison
ran as a native process inside its coordinator; competitors used immutable
cached container images.

The plain-domain list contains exactly 500,000 unique DNS names, SHA256
`95bf24064a27985fa6e19a7d2db60fd768315696a77c1ccf046cd9f8d01ef4b0`.
Earlier attempts included two address literals; those attempts and the original
fixture remain preserved but are excluded from the final comparison. The
corrected fixture excludes those literals and replenishes the same list scan
deterministically. No result is removed because it is slow or has errors.

Each comparison scenario uses 300,000 measured queries after 5,000 warmup
queries. The 50/200-worker scenarios use one source address; the separate
64-source scenario uses 64 non-loopback addresses. Query sequences and list
snapshots are identical across products. Each product keeps its process and
history across the seven scenarios, followed by a 15-second settled RSS sample.
Two full product orders run forward and backward. Effective rate limits are
read back for every competitor. Technitium DNSSEC is disabled because the local
synthetic upstream cannot provide signed DNS delegations. Query-history settings
otherwise retain each product's defaults: Svart keeps exact raw originals, whereas
Technitium's effective `logQueries` is false. This is a comparison of configured
products, not equal-durability storage contracts or isolated DNS-engine efficiency.

Timeouts and DNS errors remain explicit. A UDP response with the TC bit counts
as an error, because this generator does not retry over TCP. Exact-name blocking
expectations are compared for every completed response. Latency percentiles
exclude failed requests and begin at actual send time, excluding generator
scheduling delay. Fixed-rate latency is not corrected for coordinated omission.
Repeated observations are not confidence intervals or capacity guarantees;
the contended reverse-order run is reported separately.

Fresh logging comparisons use three alternating baseline/candidate pairs,
262,144 requests each, 64 effective client addresses and 5% unique misses.
The generator snapshots complete metrics immediately after reply workers finish
and before sorting latency samples. The logging/telemetry harness also polls
full `/metrics` once per second, including pending-journal counts and bytes.
That observation work adds CPU and storage scans while backlogged; production
monitoring uses a longer scrape interval. Results include this cost and must not
be attributed solely to DNS handling. HTTP observation lag is retained; the
snapshot is not atomic at the last packet. Afterward every accepted event drains
and the process shuts down normally. Direct Loki delivery is separately measured
with healthy, slow, and unavailable receivers, plus a 1,000,000-request outage
at 10,000 offered QPS followed by receiver recovery. Raw checks enumerate
the complete union of journal and archive segments, not just the journal tail.
Their independent payload-byte scope is limited as described below.

Optional telemetry uses three alternating off/on pairs, each 300,000 requests
at 10,000 offered QPS with query persistence enabled and query-line logging
disabled. Actual trace/profile request bodies and successful collector responses
are retained, including exports occurring during the measured interval.
Separate CPU/allocation/goroutine diagnostic profiles are not included as
unprofiled performance measurements.

Complete private databases, logs, response bodies, original-byte oracles, process
samples, snapshots, commands and failed attempts remain in the retained final
performance evidence directories. This public subset contains inspected result
summaries and fixture/build identities; it does not publish benchmark credentials
or blocklist contents. The earlier 50-million-row live dashboard measurement is
retained separately in [the history evidence](../2026-09-29-history/README.md).

## Recorded results

- [Complete result tables](results.md) retain forward and contended reverse order,
  every nonzero timeout/policy exception, fresh logging pairs and receiver trials.
- [Machine-readable final results](serial-summary.json) contain all 70 DNS
  scenario records, sixteen logging runs and their seven scope-qualified raw-record/four Loki
  oracles, three-million-rule timings, and native archive response bodies.
- [Telemetry results](telemetry-summary.json) contain all six DNS results,
  observation intervals, resident memory and actual collector delivery counts.
- [Full diagnostic profile reports](diagnostic-profiles.json.gz) contain all 24
  CPU, allocation and heap reports for four separately profiled runs, together
  with DNS results, drain times and original binary-profile hashes. Decompress
  with `gzip -dc`; CPU sampling covers thirty seconds, not necessarily full drain.
- [Provenance manifest](provenance.json) records original/public SHA256 hashes and
  exact archive byte counts. The machine-specific archive directory in the public
  serial summary is replaced with `${ARCHIVE_FIXTURE}`. Raw-oracle claims are
  corrected with explicit scope metadata; their original wording is retained as
  superseded metadata. Private originals remain unchanged. No timing, response,
  failure or record count is removed from these selected summaries.

Seven raw checks found 2,572,864 records with unique sequences, expected seeded
request-name multisets, positive timestamps, type A, NOERROR responses and
coalesced_count=1. All 1,589,824 records present in the independent before/after
journal snapshots had unchanged complete payload digests.

The million-event case had already archived 983,040 records before its first
journal snapshot, leaving 16,960 in the journal. Both verifier passes were given
**the same `archives-stopped` directory**. Archived records therefore have
count/name/selected-field checks, but their complete-payload digest comparison
is a self-comparison. No independent prearchive payload witness was retained;
this benchmark does not prove unchanged complete bytes through archival for
those 983,040 records. The original oracle's `exact_originals` label must be
read only as its record count, with the corrected `verification_scope` metadata.

Four receiver checks verified all 1,786,432 stdout query lines byte-identically
in acknowledged requests, with zero missing or duplicate lines observed. These
lines omit some raw-event fields and use emission timestamps, so they are not
an independent complete raw-payload witness. The protocol remains at least once;
clean drains do not eliminate the accepted asynchronous crash-tail risk.
