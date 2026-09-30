#!/usr/bin/env bash
set -Eeuo pipefail

readonly SECURITY_TARGET_PLATFORM="linux/amd64"
readonly SECURITY_NODE_BASE="node:24-alpine"
readonly SECURITY_GO_BASE="golang:1.26-trixie"
readonly SECURITY_RUNTIME_BASE="gcr.io/distroless/base-nossl-debian13:nonroot"
readonly SECURITY_SCANNER_BASE="debian:trixie-slim"

security_die() {
	printf 'container security build failed: %s\n' "$*" >&2
	return 1
}

validate_security_revision() {
	[[ "$1" =~ ^[0-9a-f]{40}$ ]] || security_die "revision must be 40 lowercase hexadecimal characters"
}

validate_security_image() {
	[[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._:/@-]+$ ]] || security_die "image reference has an invalid format"
}

resolve_base_digest() {
	local tag="$1"
	local repository="${tag%@*}"
	local digests digest checksum
	if [[ "${repository##*/}" == *:* ]]; then
		repository="${repository%:*}"
	fi

	# This function runs in command substitution, where errexit is not reliable.
	# Docker may list mirror aliases before the requested repository's digest.
	docker pull --platform "$SECURITY_TARGET_PLATFORM" "$tag" >&2 || {
		security_die "failed to pull ${tag}"
		return 1
	}
	digests="$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$tag")" || {
		security_die "failed to inspect ${tag}"
		return 1
	}
	while IFS= read -r digest; do
		[[ "$digest" == "$repository@"* ]] || continue
		checksum="${digest#"$repository@"}"
		if [[ "$checksum" =~ ^sha256:[0-9a-f]{64}$ ]]; then
			printf '%s\n' "$digest"
			return 0
		fi
	done <<<"$digests"
	security_die "${tag} did not resolve to an immutable digest"
	return 1
}

smoke_runtime_image() {
	local image="$1"
	local output

	# An intentionally unwritable database path makes the real application exit
	# after the dynamic loader has resolved every runtime dependency. The known
	# application error distinguishes a healthy executable from a missing DSO.
	if output="$(docker run \
		--rm \
		--network none \
		--read-only \
		--env DB_PATH=/proc/svart-dns.db \
		--entrypoint /usr/local/bin/svart-dns \
		"$image" 2>&1)"; then
		security_die "runtime smoke probe unexpectedly started the application"
		return 1
	fi
	grep -F 'failed to initialize database' <<<"$output" >/dev/null || {
		printf '%s\n' "$output" >&2
		security_die "runtime smoke probe did not reach application startup"
		return 1
	}
}

build_secure_image() {
	local image="$1"
	local revision="$2"
	local sbom_dir="$3"
	local node_image go_image runtime_image scanner_image

	validate_security_image "$image" || return 1
	validate_security_revision "$revision" || return 1
	[[ -n "$sbom_dir" ]] || {
		security_die "SBOM directory is required"
		return 1
	}
	mkdir -p "$sbom_dir" || return 1
	local existing
	existing="$(find "$sbom_dir" -mindepth 1 -maxdepth 1 -print -quit)" || return 1
	[[ -z "$existing" ]] || {
		security_die "SBOM directory must be empty"
		return 1
	}

	node_image="$(resolve_base_digest "$SECURITY_NODE_BASE")" || return 1
	go_image="$(resolve_base_digest "$SECURITY_GO_BASE")" || return 1
	runtime_image="$(resolve_base_digest "$SECURITY_RUNTIME_BASE")" || return 1
	scanner_image="$(resolve_base_digest "$SECURITY_SCANNER_BASE")" || return 1

	docker image rm "$image" >/dev/null 2>&1 || true
	docker build \
		--pull \
		--no-cache \
		--platform "$SECURITY_TARGET_PLATFORM" \
		--build-arg "NODE_IMAGE=${node_image}" \
		--build-arg "GO_IMAGE=${go_image}" \
		--build-arg "RUNTIME_IMAGE=${runtime_image}" \
		--build-arg "SOURCE_REVISION=${revision}" \
		--label "org.opencontainers.image.revision=${revision}" \
		--tag "$image" \
		. || return 1

	smoke_runtime_image "$image" || return 1

	docker build \
		--no-cache \
		--platform "$SECURITY_TARGET_PLATFORM" \
		--build-arg "TARGET_IMAGE=${image}" \
		--build-arg "SCANNER_BASE_IMAGE=${scanner_image}" \
		--file scripts/container-security.Dockerfile \
		--target sbom \
		--output "type=local,dest=${sbom_dir}" \
		. || return 1

	test -s "$sbom_dir/sbom.cdx.json" || {
		security_die "CycloneDX SBOM was not produced"
		return 1
	}
	test -s "$sbom_dir/vulnerabilities.json" || {
		security_die "vulnerability report was not produced"
		return 1
	}
}

container_security_main() {
	local image=""
	local revision=""
	local sbom_dir=""

	while (($#)); do
		case "$1" in
		--image)
			[[ $# -ge 2 ]] || security_die "--image requires a value"
			image="$2"
			shift 2
			;;
		--revision)
			[[ $# -ge 2 ]] || security_die "--revision requires a value"
			revision="$2"
			shift 2
			;;
		--sbom-dir)
			[[ $# -ge 2 ]] || security_die "--sbom-dir requires a value"
			sbom_dir="$2"
			shift 2
			;;
		*)
			security_die "unknown argument: $1"
			;;
		esac
	done

	[[ -n "$image" ]] || security_die "--image is required"
	[[ -n "$revision" ]] || security_die "--revision is required"
	[[ -n "$sbom_dir" ]] || security_die "--sbom-dir is required"
	build_secure_image "$image" "$revision" "$sbom_dir"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	container_security_main "$@"
fi
