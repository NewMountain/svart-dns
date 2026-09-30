#!/usr/bin/env bash
set -euo pipefail

# An optional directory lets the regression test check an isolated tampered copy.
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
extension_dir="${1:-${script_dir}/../web/duckdb-extensions}"
cd -- "$extension_dir"
sha256sum --check --strict SHA256SUMS
