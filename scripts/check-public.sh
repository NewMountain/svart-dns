#!/usr/bin/env bash
# Fails if the tree contains details of the maintainer's own network that must
# not ship in the public repository (hostnames, addresses, internal services,
# family names in fixtures). Run in CI and before every public export.
#
# Usage: scripts/check-public.sh [path...]   (default: the whole repository)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
patterns=(
	'nyberg'
	'10\.10\.[0-9]{1,3}\.[0-9]{1,3}'
	'forgejo\.'
	'container-registry\.'
	'infisical'
	'proxmox'
	'truenas'
	'svart-(blue|red)'
	'\b[Dd]aria\b'
	'christopher\.nyberg'
	'2600:1700:'
	'/home/yeti/'
	'/tmp/claude-'
)
regex=$(
	IFS='|'
	echo "${patterns[*]}"
)
paths=("$@")
[ ${#paths[@]} -gt 0 ] || paths=(.)
found=0
if hits=$(git grep -nIiE "$regex" -- "${paths[@]}" ':!scripts/check-public.sh' ':!.forgejo/'); then
	echo "$hits"
	found=1
else
	status=$?
	if [ "$status" -ne 1 ]; then
		echo "check-public: git grep failed (exit $status); scan is unavailable" >&2
		exit "$status"
	fi
fi
if python3 scripts/check-public-archives.py "$regex" "${paths[@]}"; then
	:
else
	status=$?
	if [ "$status" -ne 1 ]; then
		echo "check-public: archive scan failed (exit $status); scan is unavailable" >&2
		exit "$status"
	fi
	found=1
fi
if [ "$found" -ne 0 ]; then
	echo "check-public: private-network markers found; replace them with documentation values." >&2
	exit 1
fi
echo "check-public: clean"
