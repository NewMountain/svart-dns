#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
fixture_dir="$(mktemp -d)"
trap 'rm -rf -- "$fixture_dir"' EXIT
cp "${script_dir}/../web/duckdb-extensions/"{SHA256SUMS,sqlite_scanner.duckdb_extension} "$fixture_dir/"
"${script_dir}/check-extension.sh" "$fixture_dir"
printf '\001' | dd of="${fixture_dir}/sqlite_scanner.duckdb_extension" bs=1 seek=4096 conv=notrunc status=none
if "${script_dir}/check-extension.sh" "$fixture_dir"; then
	echo 'ERROR: tampered extension passed checksum verification' >&2
	exit 1
fi
echo 'Verified: original passes; tampered extension fails.'
