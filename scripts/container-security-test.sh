#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

test -x scripts/container-security.sh
test -f scripts/container-security.Dockerfile

# Supported upstream toolchains and runtime, with lifecycle scripts disabled.
grep -F 'ARG NODE_IMAGE=node:24-alpine' Dockerfile >/dev/null
grep -F 'ARG GO_IMAGE=golang:1.26-trixie' Dockerfile >/dev/null
grep -F 'ARG RUNTIME_IMAGE=gcr.io/distroless/base-nossl-debian13:nonroot' Dockerfile >/dev/null
grep -F 'npm@12.0.2' Dockerfile >/dev/null
grep -F 'RUN npm ci --ignore-scripts --audit=false' Dockerfile >/dev/null
grep -F 'RUN npm audit signatures' Dockerfile >/dev/null
grep -F 'RUN node /usr/local/lib/npm-audit.mjs' Dockerfile >/dev/null
grep -F '"audit", "--json", "--audit-level=high"' scripts/npm-audit.mjs >/dev/null
grep -F 'golang.org/x/vuln/cmd/govulncheck@v1.6.0' Dockerfile >/dev/null
grep -F 'RUN CGO_ENABLED=1 govulncheck -test ./...' Dockerfile >/dev/null
grep -F 'apt-get download gcc-14-base libgcc-s1 libstdc++6' Dockerfile >/dev/null
grep -F '/runtime-libs/var/lib/dpkg/status.d' Dockerfile >/dev/null
grep -F "grep -Eq 'libssl|libcrypto|not found'" Dockerfile >/dev/null
grep -F 'COPY --from=builder /runtime-libs/ /' Dockerfile >/dev/null
grep -F 'RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /svart-healthcheck ./cmd/healthcheck' Dockerfile >/dev/null
grep -F 'HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD ["/usr/local/bin/svart-healthcheck"]' Dockerfile >/dev/null
grep -F 'apt-get upgrade -y' Dockerfile >/dev/null

if grep -nE 'node:20|golang:1\.24|debian:bookworm' Dockerfile; then
	printf 'unsupported or old container base remains in Dockerfile\n' >&2
	exit 1
fi
if grep -F 'gcr.io/distroless/cc-debian13' Dockerfile scripts/container-security.sh; then
	printf 'OpenSSL-bearing cc runtime remains configured\n' >&2
	exit 1
fi

# The scanner binary is checksum-pinned and runs in an isolated build stage.
grep -F 'ARG TRIVY_VERSION=0.72.0' scripts/container-security.Dockerfile >/dev/null
grep -F 'ARG TRIVY_SHA256=bbb64b9695866ce4a7a8f5c9592002c5961cab378577fa3f8a040df362b9b2ea' \
	scripts/container-security.Dockerfile >/dev/null
grep -F 'sha256sum -c -' scripts/container-security.Dockerfile >/dev/null
grep -F -- '--severity HIGH,CRITICAL' scripts/container-security.Dockerfile >/dev/null
if grep -F -- '--ignore-unfixed' scripts/container-security.Dockerfile; then
	printf 'HIGH/CRITICAL findings must fail closed even when no vendor fix exists\n' >&2
	exit 1
fi
grep -F -- '--format cyclonedx' scripts/container-security.Dockerfile >/dev/null
if grep -nE 'docker\.sock|/var/run|aquasec/trivy:(latest|[0-9])' scripts/container-security.Dockerfile; then
	printf 'scanner must not receive Docker access or execute a floating scanner image\n' >&2
	exit 1
fi

# The private CI workflow is deliberately omitted from local public exports.
# Check its wiring when present; runtime and scanner checks apply to both trees.
if [[ -f .forgejo/workflows/ci.yml ]]; then
	grep -F 'apk add --no-cache bash docker-cli docker-cli-compose git jq make nodejs' .forgejo/workflows/ci.yml >/dev/null
	grep -F 'bash scripts/container-security.sh' .forgejo/workflows/ci.yml >/dev/null
fi
grep -F -- '--pull' scripts/container-security.sh >/dev/null
grep -F -- '--no-cache' scripts/container-security.sh >/dev/null
node scripts/npm-audit-test.mjs

stub_dir="$(mktemp -d)"
calls_file="$(mktemp)"
cleanup() {
	rm -rf "$stub_dir"
	rm -f "$calls_file"
}
trap cleanup EXIT

cat >"$stub_dir/docker" <<'EOF'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >>"$SECURITY_TEST_CALLS"
if [[ "$1" == "pull" && "${SECURITY_TEST_MODE:-}" == "pull-failure" ]]; then
  exit 31
fi
if [[ "$1 $2" == "image inspect" ]]; then
  case "${SECURITY_TEST_MODE:-}" in
    inspect-failure) exit 32 ;;
    no-digests) exit 0 ;;
    wrong-repository) printf 'mirror.example/debian@sha256:%064d\n' 4; exit 0 ;;
    regex-lookalike) printf 'registryXexample:5443/debian@sha256:%064d\n' 4; exit 0 ;;
    malformed) printf 'debian@sha256:not-a-digest\n'; exit 0 ;;
    mirror-first) printf 'mirror.example/debian@sha256:%064d\ndebian@sha256:%064d\n' 4 4; exit 0 ;;
    registry-port) printf 'registry.example:5443/debian@sha256:%064d\n' 4; exit 0 ;;
  esac
  ref="${@: -1}"
  case "$ref" in
    node:24-alpine) printf 'node@sha256:%064d\n' 1 ;;
    golang:1.26-trixie) printf 'golang@sha256:%064d\n' 2 ;;
    gcr.io/distroless/base-nossl-debian13:nonroot) printf 'gcr.io/distroless/base-nossl-debian13@sha256:%064d\n' 3 ;;
    debian:trixie-slim) printf 'debian@sha256:%064d\n' 4 ;;
    *) exit 20 ;;
  esac
elif [[ "$1" == "run" ]]; then
  if [[ "${SECURITY_TEST_MODE:-}" == smoke-success ]]; then exit 0; fi
  if [[ "${SECURITY_TEST_MODE:-}" == smoke-wrong-error ]]; then printf 'loader failure\n'; exit 1; fi
  printf 'level=ERROR msg="failed to initialize database"\n'
  exit 1
elif [[ "$1" == "build" && "${SECURITY_TEST_MODE:-}" == candidate-build-failure ]]; then
  exit 33
elif [[ "$1" == "build" && "$*" == *"scripts/container-security.Dockerfile"* ]]; then
  if [[ "${SECURITY_TEST_MODE:-}" == scanner-reports-failure ]]; then
    for arg in "$@"; do
      if [[ "$arg" == type=local,dest=* ]]; then
        destination="${arg#type=local,dest=}"
        printf '{"report":"complete"}\n' >"$destination/sbom.cdx.json"
        printf '{"Vulnerabilities":[{"Severity":"HIGH"}]}\n' >"$destination/vulnerabilities.json"
      fi
    done
  fi
  exit 42
fi
EOF
chmod +x "$stub_dir/docker"

# Pull/inspection failures and untrusted aliases cannot produce a successful digest.
for mode in pull-failure inspect-failure no-digests wrong-repository malformed regex-lookalike; do
	tag=debian:trixie-slim
	if [[ "$mode" == regex-lookalike ]]; then tag=registry.example:5443/debian:trixie-slim; fi
	if SECURITY_TEST_MODE="$mode" SECURITY_TEST_CALLS="$stub_dir/$mode.calls" PATH="$stub_dir:$PATH" \
		bash -c 'source scripts/container-security.sh; resolve_base_digest "$1"' _ "$tag" \
		>"$stub_dir/$mode.out" 2>"$stub_dir/$mode.err"; then
		printf 'digest resolution incorrectly accepted %s\n' "$mode" >&2
		exit 1
	fi
	test ! -s "$stub_dir/$mode.out"
	grep -F 'container security build failed:' "$stub_dir/$mode.err" >/dev/null
	if SECURITY_TEST_MODE="$mode" SECURITY_TEST_CALLS="$stub_dir/$mode.build-calls" PATH="$stub_dir:$PATH" \
		bash scripts/container-security.sh --image registry.example/svart-dns:test \
		--revision 0123456789abcdef0123456789abcdef01234567 --sbom-dir "$stub_dir/$mode.sbom" \
		>"$stub_dir/$mode.build-out" 2>"$stub_dir/$mode.build-err"; then
		printf 'candidate build incorrectly continued after %s\n' "$mode" >&2
		exit 1
	fi
	if grep -E '^(build|run|image rm) ' "$stub_dir/$mode.build-calls"; then
		printf 'candidate build mutated images after failed digest resolution\n' >&2
		exit 1
	fi
done
for mode in mirror-first registry-port; do
	repository=debian
	if [[ "$mode" == registry-port ]]; then repository=registry.example:5443/debian; fi
	actual="$(SECURITY_TEST_MODE="$mode" SECURITY_TEST_CALLS="$stub_dir/$mode.calls" PATH="$stub_dir:$PATH" \
		bash -c 'source scripts/container-security.sh; resolve_base_digest "$1"' _ "$repository:trixie-slim")"
	expected="$(printf '%s@sha256:%064d' "$repository" 4)"
	test "$actual" = "$expected"
done

# Invalid revisions fail before Docker or network activity.
if SECURITY_TEST_CALLS="$calls_file" PATH="$stub_dir:$PATH" \
	bash scripts/container-security.sh --image registry.example/svart-dns:test \
	--revision not-a-sha --sbom-dir "$stub_dir/sbom" >/dev/null 2>&1; then
	printf 'container security build accepted an invalid revision\n' >&2
	exit 1
fi
test ! -s "$calls_file"

# A vulnerability scanner failure must fail the whole candidate build.
if SECURITY_TEST_CALLS="$calls_file" PATH="$stub_dir:$PATH" \
	bash scripts/container-security.sh --image registry.example/svart-dns:test \
	--revision 0123456789abcdef0123456789abcdef01234567 \
	--sbom-dir "$stub_dir/sbom" >/dev/null 2>&1; then
	printf 'container build ignored a vulnerability scanner failure\n' >&2
	exit 1
fi
grep -F -- '--pull --no-cache --platform linux/amd64' "$calls_file" >/dev/null
grep -F -- '--build-arg NODE_IMAGE=node@sha256:' "$calls_file" >/dev/null
grep -F -- '--build-arg GO_IMAGE=golang@sha256:' "$calls_file" >/dev/null
grep -F -- '--build-arg RUNTIME_IMAGE=gcr.io/distroless/base-nossl-debian13@sha256:' "$calls_file" >/dev/null
grep -F -- '--build-arg SOURCE_REVISION=0123456789abcdef0123456789abcdef01234567' "$calls_file" >/dev/null
grep -F -- 'run --rm --network none --read-only --env DB_PATH=/proc/svart-dns.db --entrypoint /usr/local/bin/svart-dns registry.example/svart-dns:test' "$calls_file" >/dev/null
grep -F -- '--build-arg SCANNER_BASE_IMAGE=debian@sha256:' "$calls_file" >/dev/null

# Invalid sourced-function input stops before any Docker action even under if.
for mode in invalid-image invalid-revision empty-sbom occupied-sbom; do
	image=registry.example/svart-dns:test
	revision=0123456789abcdef0123456789abcdef01234567
	sbom="$stub_dir/$mode.sbom"
	case "$mode" in
	invalid-image) image='invalid image' ;;
	invalid-revision) revision=invalid ;;
	empty-sbom) sbom='' ;;
	occupied-sbom)
		mkdir "$sbom"
		printf original >"$sbom/retained"
		;;
	esac
	if SECURITY_TEST_CALLS="$stub_dir/$mode.input-calls" PATH="$stub_dir:$PATH" \
		bash -c 'source scripts/container-security.sh; if build_secure_image "$1" "$2" "$3"; then exit 0; else exit 42; fi' _ \
		"$image" "$revision" "$sbom" >"$stub_dir/$mode.input-out" 2>"$stub_dir/$mode.input-err"; then
		printf 'conditional build accepted %s\n' "$mode" >&2
		exit 1
	fi
	test ! -s "$stub_dir/$mode.input-calls"
done
test "$(cat "$stub_dir/occupied-sbom.sbom/retained")" = original

for mode in candidate-build-failure smoke-success smoke-wrong-error; do
	if SECURITY_TEST_MODE="$mode" SECURITY_TEST_CALLS="$stub_dir/$mode.pipeline-calls" PATH="$stub_dir:$PATH" \
		bash -c 'source scripts/container-security.sh; if build_secure_image "$1" "$2" "$3"; then exit 0; else exit 42; fi' _ \
		registry.example/svart-dns:test 0123456789abcdef0123456789abcdef01234567 "$stub_dir/$mode.pipeline-sbom" \
		>"$stub_dir/$mode.pipeline-out" 2>"$stub_dir/$mode.pipeline-err"; then
		printf 'conditional build ignored %s\n' "$mode" >&2
		exit 1
	fi
	if grep -F scripts/container-security.Dockerfile "$stub_dir/$mode.pipeline-calls"; then
		printf 'scanner ran after failed build or runtime startup\n' >&2
		exit 1
	fi
done

# The real infra controller calls the sourced function in a conditional. Bash
# disables errexit throughout that function, including its nested helpers.
if SECURITY_TEST_MODE=scanner-reports-failure SECURITY_TEST_CALLS="$stub_dir/conditional.calls" PATH="$stub_dir:$PATH" \
	bash -c 'source scripts/container-security.sh; if build_secure_image "$1" "$2" "$3"; then exit 0; else exit 42; fi' _ \
	registry.example/svart-dns:test 0123456789abcdef0123456789abcdef01234567 "$stub_dir/conditional.sbom" \
	>"$stub_dir/conditional.out" 2>"$stub_dir/conditional.err"; then
	printf 'sourced conditional build ignored failed scanner with complete reports\n' >&2
	exit 1
fi
test -s "$stub_dir/conditional.sbom/sbom.cdx.json"
test -s "$stub_dir/conditional.sbom/vulnerabilities.json"

printf 'container security checks passed\n'
