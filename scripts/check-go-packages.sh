#!/usr/bin/env bash
set -euo pipefail

# Reconcile every directory containing owned Go source with the actual package
# set. Installed Node dependencies must never become project Go packages, and a
# newly owned package outside these roots must fail instead of silently escaping.
cd "$(dirname "${BASH_SOURCE[0]}")/.."
root=$PWD
inventory=$(mktemp -d)
trap 'rm -rf "$inventory"' EXIT
git ls-files '*.go' | while IFS= read -r source; do
	dirname "$source"
done | sort -u >"$inventory/owned"
go list -f '{{.Dir}}' . ./internal/... ./internal/telemetry/testdata/version ./cmd/... >"$inventory/root"
(cd benchmarks && go list -f '{{.Dir}}' ./...) >"$inventory/benchmarks"
cat "$inventory/root" "$inventory/benchmarks" | while IFS= read -r directory; do
	realpath --relative-to="$root" "$directory"
done | sort -u >"$inventory/selected"
if ! diff -u "$inventory/owned" "$inventory/selected"; then
	echo 'Owned Go package inventory differs from quality gate package selection' >&2
	exit 1
fi
cat "$inventory/selected"
