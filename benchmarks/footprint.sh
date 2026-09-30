#!/usr/bin/env bash
# Memory footprint of a real svart-dns binary as a function of blocklist size,
# entity count and cache volume. Fully local (stub upstream, lists served from
# a local directory); nothing touches the internet.
#
# Usage: benchmarks/footprint.sh <svart-dns-binary> <lists-dir>
#   Lists added in order when present: stevenblack.txt hagezi-pro.txt
#   oisd-big.txt hagezi-ultimate.txt hagezi-tif.txt
#   PROFILE_DIR=dir also saves a heap profile per row.
#   PROFILE_GC=false retains metrics/status without forcing GC (older binaries).
set -euo pipefail
BIN=$(realpath "$1")
LISTS=$(realpath "$2")
PROFILE_DIR=${PROFILE_DIR:-}
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

heap_mb() { curl -sf "$API/metrics" | awk '/^go_memstats_heap_inuse_bytes / {printf "%.1f", $2/1048576}'; }
# Let the GC return freed memory so RSS reflects steady state, not load spikes.
settle() { sleep "${1:-3}"; }
rownum=0
row() {
	printf '%-44s %10s %10s\n' "$1" "$(rss_mb)" "$(heap_mb)"
	rownum=$((rownum + 1))
	if [ -n "$PROFILE_DIR" ]; then
		mkdir -p "$PROFILE_DIR"
		curl -sf "$API/metrics" >"$PROFILE_DIR/metrics-$rownum.txt"
		cat "/proc/$SVART/status" >"$PROFILE_DIR/status-$rownum.txt"
		if [ "${PROFILE_GC:-true}" = true ]; then
			curl -sf -o "$PROFILE_DIR/heap-$rownum.pb.gz" "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap?gc=1"
		fi
	fi
}

printf '%-44s %10s %10s\n' "state" "RSS MiB" "heap MiB"
svart_start
settle 2
row "empty (no lists)"

total=0
list_ids=()
for f in stevenblack.txt hagezi-pro.txt oisd-big.txt hagezi-ultimate.txt hagezi-tif.txt; do
	[ -f "$LISTS/$f" ] || continue
	read -r id count < <(add_list "$f")
	list_ids+=("$id")
	total=$((total + count))
	settle
	row "+ $f (${count}; total ${total})"
done

# Twenty VLAN-style ranges that each reuse the first two lists: memory should
# not scale with the number of entities that share a list.
for i in $(seq 1 20); do
	rid=$(api -X POST -d "{\"name\":\"vlan-$i\",\"cidr\":\"10.77.$i.0/24\"}" "$API/api/ranges" | json_field '["data"]["id"]')
	for id in "${list_ids[@]:0:2}"; do api -X POST "$API/api/ranges/$rid/blocklists/$id" >/dev/null; done
done
settle
row "+ 20 ranges sharing 2 lists"

"$WORK/dnsbench" -server 127.0.0.1 -port "$DNS_PORT" -clients 50 -queries 200000 -warmup 0 \
	-blocked-pct 0 -miss-pct 100 -label footprint -json >"${PROFILE_DIR:-$WORK}/cache-load.json"
settle 5
row "+ 200k unique names cached"
settle 15
row "  ... 15s later"
