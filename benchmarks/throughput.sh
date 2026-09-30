#!/usr/bin/env bash
# Throughput and latency of a real svart-dns binary under several query mixes,
# against a local stub upstream. Server and load generator are pinned to
# disjoint CPU sets so they don't compete.
#
# Usage: benchmarks/throughput.sh <svart-dns-binary> <lists-dir> [queries]
set -euo pipefail
BIN=$(realpath "$1")
LISTS=$(realpath "$2")
QUERIES=${3:-500000}
SVART_CPUS=${SVART_CPUS:-0-7}
BENCH_CPUS=${BENCH_CPUS:-16-31}
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

svart_start
read -r _ count < <(add_list hagezi-pro.txt)
echo "loaded hagezi-pro.txt ($count domains) on 0.0.0.0/0; svart cpus=$SVART_CPUS bench cpus=$BENCH_CPUS"

bench() {
	taskset -c "$BENCH_CPUS" "$WORK/dnsbench" -server 127.0.0.1 -port "$DNS_PORT" -queries "$QUERIES" \
		-blocklist "$LISTS/hagezi-pro.txt" -json "$@"
}
printf '%-26s %9s %8s %8s %8s %9s %6s\n' scenario qps p50_us p99_us p999_us rss_mib errs
for scenario in "hits:-blocked-pct 0 -miss-pct 0" "mixed-20blk:-blocked-pct 20 -miss-pct 0" \
	"blocked:-blocked-pct 100 -miss-pct 0" "misses-50:-blocked-pct 10 -miss-pct 50" \
	"hits-c200:-blocked-pct 20 -miss-pct 0 -clients 200"; do
	name=${scenario%%:*}
	# shellcheck disable=SC2086 # flags are intentionally word-split
	bench ${scenario#*:} -label "$name" | python3 -c "
import json,sys
s=json.load(sys.stdin)
print(f\"{s['label']:<26} {s['qps']:>9.0f} {s['p50_us']:>8.0f} {s['p99_us']:>8.0f} {s['p999_us']:>8.0f} {'$(rss_mb)':>9} {s['timeouts']+s['errors']:>6}\")"
done
