#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root"
image=${SVART_E2E_IMAGE:-svart-e2e:local}
revision=$(git rev-parse HEAD)
if [[ -n $(git status --porcelain) ]]; then
	printf 'E2E requires a clean committed checkout so image provenance is exact.\n' >&2
	exit 1
fi
if [[ ${1:-} == build ]]; then
	docker build --file scripts/e2e/Dockerfile --build-arg "SOURCE_REVISION=$revision" --tag "$image" .
	exit 0
fi
if [[ $# -ne 0 ]]; then
	printf 'usage: %s [build]\n' "$0" >&2
	exit 2
fi
if ! docker image inspect "$image" >/dev/null 2>&1; then
	printf 'E2E image unavailable; run make e2e-build first.\n' >&2
	exit 1
fi
image_revision=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
if [[ "$image_revision" != "$revision" ]]; then
	printf 'E2E image revision %s differs from checkout %s; rebuild with make e2e.\n' "$image_revision" "$revision" >&2
	exit 1
fi
image_id=$(docker image inspect --format '{{.Id}}' "$image")
output=${SVART_E2E_OUTPUT:-$(mktemp -d "${TMPDIR:-/tmp}/svart-e2e.XXXXXXXX")}
mkdir -p "$output"
if [[ -n $(find "$output" -mindepth 1 -maxdepth 1 -print -quit) ]]; then
	printf 'E2E output must be empty; preserve the previous run and choose a fresh directory.\n' >&2
	exit 1
fi
output=$(cd "$output" && pwd)
name="svart-e2e-$(date +%s)-$$"
docker create --name "$name" --user "$(id -u):$(id -g)" --network none --cpus 4 --shm-size 1g "$image_id" >/dev/null
# docker cp works with local and remote daemons. Never remove the only evidence
# if collection fails: retain this owned container and print its recovery name.
trap 'status=$?; trap - EXIT INT TERM
if docker cp "$name:/evidence/." "$output/" 2>>"$output/collection.log"; then
  docker rm --force "$name" >>"$output/collection.log" 2>&1 || status=1
else
  printf "Evidence collection failed; preserved container %s. Retry docker cp before removing it.\n" "$name" >&2
  status=1
fi
exit "$status"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
printf 'E2E evidence: %s\n' "$output"
docker inspect "$name" >"$output/container.json"
docker image inspect "$image_id" >"$output/image.json"
git status --short >"$output/source-status.txt"
git diff --binary HEAD >"$output/source.patch"
docker start --attach "$name" 2>&1 | tee "$output/run.log"
status=$(docker inspect --format '{{.State.ExitCode}}' "$name")
exit "$status"
