# Embedded extension and container inputs

`web/duckdb-extensions/provenance.json` records the SQLite scanner's official
HTTPS distribution, DuckDB ABI version, extension revision, architecture and
MIT license source. `LICENSE.sqlite-scanner` is copied from that revision.
`SHA256SUMS` is the authoritative checksum of the uncompressed binary.

On 2026-09-26 the official distribution at
<https://extensions.duckdb.org/v1.1.3/linux_amd64/sqlite_scanner.duckdb_extension.gz>
was downloaded over HTTPS, decompressed and compared with the existing vendored
binary. Both have SHA-256
`f328688782aa5eddb85e68f837113c9620e3dc377f7be385a6fc178d6fc8b98a`.
The binary's metadata identifies `v1.1.3`, `linux_amd64` and extension revision
`d5d6265`; its ELF header identifies 64-bit x86-64. DuckDB's signed-extension
loader remains enabled. The provenance records this observed identity; it does
not claim a reproducible source-to-binary build.

Every Docker build runs `scripts/check-extension.sh` before compiling Go.
Run `scripts/check-extension-test.sh` to verify an original copy succeeds and
a deliberately changed copy fails. Updating the binary requires reviewing its
upstream identity, license and checksum together. Runtime autoinstall/autoload
remain disabled: no download occurs while serving investigation requests.

The default `NODE_IMAGE`, `GO_IMAGE` and `RUNTIME_IMAGE` arguments in Dockerfile
pin official image indexes by SHA-256. The scanner Dockerfile also pins its
Debian defaults; the refresh script overrides the scanner base with a resolved
current digest. They retain the readable tags and are
resolved for the existing linux/amd64 deployment. These pins stabilize base
image inputs; they do **not** make the entire build bit-for-bit reproducible.
The builder intentionally refreshes Debian security packages, vulnerability
databases and npm signature checks at build time. npm and Go dependency versions
remain locked by their existing lockfiles, and tooling versions are explicit.

The separate `scripts/container-security.sh` is an intentional security refresh
path. It resolves current supported upstream tags to immutable digests, then
passes those digests as build arguments to a clean build, performs a loader
smoke test and requires the isolated vulnerability/SBOM stage to pass. Thus a
scheduled security refresh can consume newer supported base images even while
ordinary builds use the reviewed defaults. This behavior is preserved; it is
not evidence that a default image pin is floating. Record the resulting image
digest and SBOM when promoting a refresh. No production deployment is performed
by the local provenance checks.

## Dependency advisory follow-up (2026-09-26)

The complete image report, including lower severities, prompted three minimal
module upgrades; `go 1.26.0` remains the supported minimum.

| Module | Previous → selected | Primary advisory |
|---|---|---|
| `github.com/go-viper/mapstructure/v2` | 2.3.0 → 2.4.0 | [GO-2025-3900](https://pkg.go.dev/vuln/GO-2025-3900), [upstream fix](https://github.com/go-viper/mapstructure/commit/742921c9ba2854d27baa64272487fc5075d2c39c): malformed-value error disclosure |
| `golang.org/x/crypto` | 0.55.0 → 0.56.0 | [GO-2026-6354](https://pkg.go.dev/vuln/GO-2026-6354) and [GO-2026-6355](https://pkg.go.dev/vuln/GO-2026-6355): SSH connection deadlocks |
| `github.com/klauspost/compress` | 1.18.0 → 1.18.7 | [GO-2026-5841](https://pkg.go.dev/vuln/GO-2026-5841): crafted S2 dictionary out-of-bounds read; found by the source scan, absent from the earlier image report |

Fresh `govulncheck -json -test ./...` before the upgrades recorded all four
fixable advisories; afterward they are absent at module, package and symbol
levels. Neither scan identified a reachable vulnerable symbol. This is
dependency remediation, not a claim that those paths were exploitable through
Svart. Existing auth, DuckDB, archive and SQL sandbox tests exercise the actual
consumers; version-string assertion tests would not establish compatibility.

[GO-2026-5932](https://vuln.go.dev/ID/GO-2026-5932.json) remains a module-level
finding with no fixed version: the upstream `openpgp` packages are unmaintained
and unsafe. The fresh source scan has no package or symbol trace for it.
`go list -deps -test ./...` imports only `bcrypt` and `blowfish` from x/crypto,
with no OpenPGP or SSH package. Thus **observed:** the vulnerable OpenPGP packages
are absent from this source dependency graph. Keep the module finding in the
complete report and rerun reachability analysis whenever imports change.

### Remaining Debian findings

The runtime's `libc6` is `2.41-12+deb13u4`. On the review date, the
[Debian security tracker](https://security-tracker.debian.org/tracker/source-package/glibc)
and its [complete JSON data](https://security-tracker.debian.org/tracker/data/json)
list all 20 reported libc6 findings as open for trixie, with no fixed trixie
version. Debian marks the 13 medium findings `no-dsa` / minor and the seven low
findings unimportant. These classifications describe vendor triage, not absence
of risk. Some fixes exist upstream or in unstable; that does not constitute a
supported trixie package update. No findings are suppressed and the supported
base distribution and DuckDB ABI are retained.

| Findings | Affected interface / review scope |
|---|---|
| [CVE-2026-18374](https://security-tracker.debian.org/tracker/CVE-2026-18374) | `fopen` with attacker-controlled `,ccs=` mode |
| [CVE-2026-19499](https://security-tracker.debian.org/tracker/CVE-2026-19499) | `strfmon` / `strfmon_l` right-justified padding |
| [CVE-2026-19542](https://security-tracker.debian.org/tracker/CVE-2026-19542) | `tdelete` on a sufficiently deep tree |
| [CVE-2026-5435](https://security-tracker.debian.org/tracker/CVE-2026-5435), [CVE-2026-6238](https://security-tracker.debian.org/tracker/CVE-2026-6238) | Deprecated DNS debug printers, not glibc's DNS resolver path according to the vendor |
| [CVE-2026-6368](https://security-tracker.debian.org/tracker/CVE-2026-6368), [CVE-2026-6791](https://security-tracker.debian.org/tracker/CVE-2026-6791) | `wordexp` append / tilde expansion |
| [CVE-2026-77117](https://security-tracker.debian.org/tracker/CVE-2026-77117), [CVE-2026-80489](https://security-tracker.debian.org/tracker/CVE-2026-80489) | SHIFT_JISX0213 / EUC_JISX0213 conversion |
| [CVE-2026-8674](https://security-tracker.debian.org/tracker/CVE-2026-8674) | glibc resolver initialization with a long search domain in `resolv.conf` / `LOCALDOMAIN`; deployment configuration remains relevant |
| [CVE-2026-86805](https://security-tracker.debian.org/tracker/CVE-2026-86805), [CVE-2026-95818](https://security-tracker.debian.org/tracker/CVE-2026-95818) | Secure-execution dynamic loader paths for setuid/setgid executables |
| [CVE-2026-89092](https://security-tracker.debian.org/tracker/CVE-2026-89092) | `nscd` processing oversized DNS records |
| [CVE-2010-4756](https://security-tracker.debian.org/tracker/CVE-2010-4756) | Resource exhaustion in POSIX glob expressions |
| [CVE-2018-20796](https://security-tracker.debian.org/tracker/CVE-2018-20796), [CVE-2019-9192](https://security-tracker.debian.org/tracker/CVE-2019-9192) | Regex recursion |
| [CVE-2019-1010022](https://security-tracker.debian.org/tracker/CVE-2019-1010022), [CVE-2019-1010023](https://security-tracker.debian.org/tracker/CVE-2019-1010023), [CVE-2019-1010024](https://security-tracker.debian.org/tracker/CVE-2019-1010024), [CVE-2019-1010025](https://security-tracker.debian.org/tracker/CVE-2019-1010025) | Stack protection, malicious-ELF `ldd`, ASLR / thread address disclosure; upstream does not classify these as security issues, per Debian's notes |

**Unknown:** a complete native-code reachability proof across Go cgo, embedded
DuckDB, the SQLite extension and the C++ runtime. Go's call-graph scan cannot
provide that proof. The native findings remain residual risk until a supported
vendor fix lands or a claim-specific analysis establishes a narrower scope.
Passing the HIGH/CRITICAL gate must never be described as zero vulnerabilities.
