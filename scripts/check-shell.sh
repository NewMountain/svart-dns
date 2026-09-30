#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
expected_shellcheck=0.11.0
expected_shfmt=v3.14.1
actual_shellcheck=$(shellcheck --version | sed -n 's/^version: //p')
actual_shfmt=$(shfmt --version)
if [[ "$actual_shellcheck" != "$expected_shellcheck" || "$actual_shfmt" != "$expected_shfmt" ]]; then
	printf 'check-shell: require ShellCheck %s and shfmt %s; found %s and %s\n' \
		"$expected_shellcheck" "$expected_shfmt" "$actual_shellcheck" "$actual_shfmt" >&2
	exit 1
fi
mapfile -d '' scripts < <(git ls-files -z '*.sh')
shellcheck -x -P SCRIPTDIR "${scripts[@]}"
shfmt -d "${scripts[@]}"
