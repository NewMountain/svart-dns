# DD-030: Rebuild application images from supported upstream bases and gate the final runtime

**Release procedure update:** The [bounded production release](bounded-production-release.md) supersedes the historical deployment timing, production rebuild and rehearsal procedure below. Security and runtime requirements remain in effect.

**Status**: Accepted

**Decision**: Do not maintain a privately repackaged Debian or Ubuntu base image.
Svart's authoritative build resolves the supported upstream `node:24-alpine`,
`golang:1.26-trixie`, and
`gcr.io/distroless/base-nossl-debian13:nonroot` tags to
immutable digests at the start of every run, then performs a `--pull --no-cache`
application build. The final runtime is Debian 13 distroless with only the
application and a tiny Go health-check helper. DuckDB's required GCC/C++
libraries are extracted from current Debian packages with scanner-visible
package metadata, and a build gate rejects OpenSSL or unresolved dependencies;
shells, package managers, OpenSSL, Node/npm, and the Go toolchain remain absent.
The same script is used by branch CI, the daily
non-mutating security refresh, and the production canary build so no weaker
release-only path exists.

**Security gates**: npm installation disables dependency lifecycle scripts and
verifies registry signatures/attestations. A pinned `govulncheck` performs Go
reachability analysis. The final filesystem is copied into a separate scanner
build stage and checked for fixable HIGH/CRITICAL findings by a checksum-pinned
Trivy release; neither the Docker socket nor source-control, registry, SSH, or
application credentials enter that stage. All HIGH/CRITICAL findings fail,
including unfixed findings. The stage also emits a CycloneDX SBOM and a complete
JSON report containing lower-severity findings. An isolated read-only startup
probe exercises the real runtime binary before the scanner stage.

**Temporary npm exception**: React Router 7.18 fixes the open-redirect advisory,
but is currently inside a newly published HIGH advisory range for unstable RSC
APIs whose stated fixed 8.3 release is not yet published. Svart does not use any
RSC API. The npm policy therefore permits only that exact advisory, only while
React Router is at least 7.18, only while the source scan proves the RSC markers
absent, and only before 2026-08-15 UTC. Any other HIGH/CRITICAL finding fails;
the exception also fails closed when it expires.

**Why**: Host `apt` maintenance cannot patch packages inside an already-built
container. A custom internal base would add another artifact and publishing
pipeline whose freshness could itself drift. Rebuilding the complete
application image from maintained upstream bases, minimizing the runtime, pinning the resolved inputs
for each run, and scanning what will actually ship closes that gap while keeping
the production rollout's existing canary, immutable-digest, and rollback trust
boundaries.

**Current limits**: The SBOM and JSON report are validated only in workflow
temporary storage and are not retained as Forgejo evidence. Images are not
signed and no independently verifiable provenance is published. The final
image is nonroot and distroless, but the live Compose runtime does not yet make
the root filesystem read-only or apply capability, no-new-privileges, PID, or
Docker memory controls. These facts are tracked in `SECURITY.md` and are not
implied by this decision.

---
