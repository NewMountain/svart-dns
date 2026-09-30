# Investigation archive discovery validation

## Result

**VERIFIED for the bounded local archive-discovery repair at `6b7bd1f`.**
Both Investigation query execution and schema inspection reject real ENOTDIR
and EACCES discovery failures with HTTP 503 and `error_code: unavailable`.
They recover to the exact complete result after archive access is restored.
No production deployment or broader release-gate claim is made here.

## Reproduced failure and repair

The unchanged independent regression (`4de892c`, followed by diagnostics test
`9972b9b`) was cherry-picked as `501bfa9` and `2f72efc`. Before the repair,
five hot SQLite rows plus one cold Parquet row returned the expected count 6.
Preserving the archive directory by renaming it and replacing its configured
path with a regular file then produced:

```text
HTTP200 {"data":{"columns":["count_star()"],"duration_ms":31,"row_count":1,"rows":[[5]]},"error":null}
schema inspection silently omitted unavailable archive
```

The separate tests-first commit `420094f` also reproduced this silent omission
with actual directory permission denial. It pins query and schema HTTP results,
exact recovery, unchanged cold-file bytes, and legitimate empty/fresh behavior.

`os.ReadDir` failures now propagate through view construction, worker transport,
and both HTTP handlers. Only a missing directory remains the legitimate fresh
installation case. Other failures stop the whole result. Server diagnostics
retain the underlying filesystem error; HTTP receives only:

```json
{"data":null,"error":"investigation archive unavailable; check server storage and retry","error_code":"unavailable"}
```

No memory budget, SQL allowlist, cancellation mechanism, source archive,
success response shape, or DNS execution path changed.

## Observed checks

- Host and packaged UID/GID 999: healthy count **6**, real ENOTDIR and EACCES
  failures return **503** from query and schema, restored count **6**, cold file
  byte-for-byte unchanged. Permission tests ran and passed; they were not skipped.
- Empty directory, readable directory with only a non-Parquet file, and a fresh
  missing directory: query and schema succeed with exactly **5** hot rows.
  Inspection does not create the missing directory.
- Full selected Investigation suite under the race detector: **42 top-level
  tests pass, 20.241 seconds**. The same 42 pass in the runtime package. These
  include all four actual frontend templates, current/legacy Parquet, exact
  numeric/date/list/struct transport, SQL denial corpus, private native errors,
  resource limits, deadline recovery, active cancellation, and HTTP disconnect.
- Fresh small-parent standalone host run: original 300 MB scalar returns 422,
  parent peak **140100 KiB unchanged**. Eight intermediate-memory attempts
  return 422; parent peak at most **165248 KiB**, cumulative child peak
  **228760 KiB**, worst concurrent UDP/TCP DNS **1.781993 ms**, no live worker
  after each attempt, exact seeded-client recovery.
- Equivalent UID 999 standalone run: scalar parent peak **136752 KiB unchanged**;
  eight attempts all 422, parent peak at most **162900 KiB**, cumulative child
  peak **229004 KiB**, worst concurrent DNS **1.749982 ms**. No workers remain.
- Packaged active cancellation takes **2.20–2.86 ms**; actual HTTP disconnect
  releases worker and admission in **2.70 ms**.
- `go vet ./...` and `git diff --check` pass.

## Reproduction and provenance

Use an executable, writable scratch directory with sufficient space, outside
capacity-constrained temporary mounts. Runs used Go 1.27.1, `GOMAXPROCS=4`,
`GOFLAGS=-p=4`, offline modules, and existing frontend dist whose source tree
matched this checkout exactly. No frontend source was changed or rebuilt.

```sh
export TMPDIR="$SVART_SCRATCH" GOTMPDIR="$SVART_SCRATCH"
export GOMAXPROCS=4 GOFLAGS=-p=4 GOPROXY=off GOSUMDB=off
go test -run '^TestInvestigateArchive|^TestIndependentInvestigation' -v -count=1
go test -race -run 'Investigate|Investigation|InitDuckDB' -v -count=1 -timeout=180s
go test -run '^TestIndependentInvestigationOversizedScalarIsRejected$|^TestInvestigateWorkerBoundsIntermediateMemoryAndReaps$' -v -count=1
go vet ./...
```

Fresh service and tests were built in the cached Debian Go 1.27.1 image
`69a7b9788769` with no network, then run inside the existing runtime image
`sha256:b9352c1aecf2e6fcbcf20f7c114ceb918d8e07e125e07d051f40b958875b18a5`.
The runtime used UID/GID 999, no network, all capabilities dropped,
no-new-privileges, read-only root, and private filesystem-backed scratch.
`SVART_TEST_INVESTIGATE_WORKER=/usr/local/bin/svart-dns` selected the newly built
ordinary installed service executable for query execution.

- Service SHA256: `99425389eb9c5cda4d6cd86c2146fbaf2a81fd69829f8c8dc59ed5a16a44f07c`
- Test binary SHA256: `16b5efd2663bab8eaabac60ec924b25d941040e29302b4958008459b7ca9632a`

Complete red/green, race, standalone-memory, container-suite, build, and vet
logs are retained with the integration handoff. Full-suite RSS includes repeated
metadata-engine initialization and fork inheritance; use the separate
small-parent standalone runs for memory evidence.

## Limits

EIO uses the same unconditional non-ENOENT failure path by source inspection;
no real or injected EIO experiment was performed. ENOTDIR and unprivileged
EACCES were directly exercised through both real request paths. A missing
directory cannot distinguish an installation that has never archived from
external removal of an entire directory; preserving the existing fresh-directory
contract is deliberate in this bounded repair.

This verifies newly built binaries in the existing runtime image, not a freshly
assembled/scanned application image. Full-project verify/coverage, generated API
integration, deployment, and production observability remain integration gates.
