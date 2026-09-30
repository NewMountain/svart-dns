# DD-031: Operational documentation is an executable release contract

**Status**: Accepted

**Decision**: `design/SECURITY.md` is the canonical matrix of implemented
quality, supply-chain, runtime, release, host-maintenance, and observability
controls. `design/RUNBOOK.md` owns operator procedure. A change to any of those
boundaries must update both documents in the same pull request.

`deploy/documentation-contract-test.sh` runs as part of every branch CI and
production release-script gate. It rejects the known stale bootstrap password,
the removed shell-based insecure health check, missing canonical links, and
local Compose commands that no longer match the repository layout. New
operational invariants require new assertions.

**Why**: Several older documents continued to describe a dev workstation,
three-node mesh, known bootstrap credential, shell-based liveness probe, absent
dashboard, and Arcane-centered deployment after the implementation had moved
to a two-LXC, distroless, git-triggered release. An untested runbook can become
an unsafe alternate implementation. Treating key documentation facts as code
turns drift into a failing release gate while keeping unresolved risks visible
instead of describing aspirations as controls.
