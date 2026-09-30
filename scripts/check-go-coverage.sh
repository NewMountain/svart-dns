#!/usr/bin/env bash
set -euo pipefail

# go tool cover merges duplicate blocks emitted by -coverpkg across test binaries.
# Run from the module that produced this complete compiler-generated profile.
if [[ $# -lt 1 || $# -gt 2 || ! ${2:-0} =~ ^[0-9]+$ ]]; then
	echo 'usage: check-go-coverage.sh profile [uncompiled-statements]' >&2
	exit 2
fi
report=$(go tool cover -func="$1")
printf '%s\n' "$report"
awk -v uncompiled="${2:-0}" '
NR > 1 {
  key = $1 SUBSEP $2
  statements[key] = $2
  if ($3 > 0) covered[key] = 1
}
END {
  total = uncompiled
  for (key in statements) {
    total += statements[key]
    if (covered[key]) executed += statements[key]
  }
  if (total == 0) { print "Go coverage denominator is empty" > "/dev/stderr"; exit 1 }
  printf "Complete owned Go coverage: %d/%d statements (%.4f%%); %d platform-inactive statements count as uncovered\n", executed, total, 100 * executed / total, uncompiled
  if (100 * executed < 80 * total) {
    print "Go coverage is below required 80%" > "/dev/stderr"
    exit 1
  }
}' "$1"
