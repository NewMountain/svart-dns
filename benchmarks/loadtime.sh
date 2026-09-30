#!/usr/bin/env bash
# Time from adding a published list until it actually blocks, for each list
# in turn, on a real svart-dns binary. Fully local (stub upstream, lists
# served from a local directory); nothing touches the internet.
#
# Each list is assigned to a client and to a 0.0.0.0/0 range the moment it is
# created, i.e. while it is still loading (the B1 scenario in
# design/SECURITY-REVIEW-2026-09.md). "client ms" is the time until a domain
# that only this list (of those added so far) contains is blocked for the
# client; "range" says whether a range-only client is blocked too once the
# client is.
#
# Usage: benchmarks/loadtime.sh <svart-dns-binary> <lists-dir>
set -euo pipefail
BIN=$(realpath "$1")
LISTS=$(realpath "$2")
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CLIENT=10.99.0.1
RANGE_CLIENT=10.88.0.1

# probe_domains prints, per file, one domain that no earlier file contains.
probe_domains() {
	python3 - "$LISTS" "$@" <<'EOF'
import sys
def entries(path):
    for line in open(path, encoding="utf-8", errors="replace"):
        line = line.strip()
        if not line or line[0] in "#!":
            continue
        parts = line.split()
        if parts[0] in ("0.0.0.0", "127.0.0.1"):
            if len(parts) < 2:
                continue
            line = parts[1]
        line = line.removeprefix("||").removesuffix("^").lower().rstrip(".")
        if len(parts) > 2 or any(c in line for c in "*/$ \t:") or "." not in line or line.startswith("localhost") or not any(c.isalpha() for c in line):
            continue
        yield line
seen = set()
for f in sys.argv[2:]:
    mine = list(entries(sys.argv[1] + "/" + f))
    probe = next(d for d in mine if d not in seen)
    seen.update(mine)
    print(f, probe)
EOF
}

# blocked_for prints the decision, or "unavailable" when the API did not
# answer (the admin API can stall while a large list is being written).
blocked_for() {
	api "$API/api/policy/evaluate?client_ip=$1&domain=$2" 2>/dev/null | json_field '["data"]["result"]' 2>/dev/null || echo unavailable
}

files=()
for f in stevenblack.txt hagezi-pro.txt oisd-big.txt hagezi-ultimate.txt hagezi-tif.txt; do
	[ -f "$LISTS/$f" ] && files+=("$f")
done
declare -A probe
while read -r f d; do probe[$f]=$d; done < <(probe_domains "${files[@]}")

svart_start
printf '%-22s %10s %10s %8s  %s\n' "list" "count ms" "client ms" "range" "probe"
all_start=$(date +%s%N)
for f in "${files[@]}"; do
	start=$(date +%s%N)
	id=$(api -X POST -d "{\"url\":\"http://127.0.0.1:${LIST_PORT}/$f\",\"alias\":\"$f\",\"enabled\":true}" \
		"$API/api/blocklists" | json_field '["data"]["id"]')
	api -X POST "$API/api/clients/$CLIENT/blocklists/$id" >/dev/null
	api -X POST "$API/api/ranges/$RANGE_ID/blocklists/$id" >/dev/null
	count_ms=""
	for _ in $(seq 1 2400); do
		if [ -z "$count_ms" ]; then
			count=$(api "$API/api/blocklists" | python3 -c "
import json,sys
for b in json.load(sys.stdin)['data'] or []:
    if b['id']==$id: print(b['domain_count'])" 2>/dev/null) || count=0
			[ "${count:-0}" -gt 0 ] && count_ms=$((($(date +%s%N) - start) / 1000000))
		fi
		[ "$(blocked_for "$CLIENT" "${probe[$f]}")" = block ] && break
		sleep 0.05
	done
	client_ms=$((($(date +%s%N) - start) / 1000000))
	range=$(blocked_for "$RANGE_CLIENT" "${probe[$f]}")
	printf '%-22s %10s %10s %8s  %s\n' "$f" "${count_ms:-?}" "$client_ms" "$range" "${probe[$f]}"
done
printf 'all lists loaded in %d ms\n' $((($(date +%s%N) - all_start) / 1000000))
