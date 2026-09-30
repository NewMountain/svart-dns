#!/usr/bin/env bash
# Shared setup for the local benchmark scripts: a stub upstream, a local
# blocklist file server and a fresh svart-dns instance on throwaway state.
# Source it after setting BIN and LISTS; call svart_start, then use api().

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
WORK=$(mktemp -d)
DNS_PORT=${DNS_PORT:-25353}
ADMIN_PORT=${ADMIN_PORT:-23000}
LIST_PORT=${LIST_PORT:-28080}
UP_PORT=${UP_PORT:-25300}
PPROF_PORT=${PPROF_PORT:-26060}
SVART_CPUS=${SVART_CPUS:-}
API="http://127.0.0.1:${ADMIN_PORT}"
JAR="$WORK/cookies"
PASS="bench-$(date +%s)-password"
pids=()

cleanup() {
	for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	if [ -n "${PROFILE_DIR:-}" ] && [ -f "$WORK/svart.log" ]; then cp "$WORK/svart.log" "$PROFILE_DIR/svart.log"; fi
	rm -rf "$WORK"
}
trap cleanup EXIT

api() { curl -sf -b "$JAR" -H 'Content-Type: application/json' -H "Origin: $API" "$@"; }
json_field() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

svart_start() {
	if [ -n "${BENCH_BIN_DIR:-}" ]; then
		cp "$BENCH_BIN_DIR/stubupstream" "$BENCH_BIN_DIR/dnsbench" "$WORK/"
	else
		(cd "$HERE" && go build -o "$WORK/stubupstream" ./stubupstream && go build -o "$WORK/dnsbench" ./dnsbench)
	fi
	"$WORK/stubupstream" -addr "127.0.0.1:${UP_PORT}" 2>/dev/null &
	pids+=($!)
	python3 -m http.server "$LIST_PORT" --bind 127.0.0.1 --directory "$LISTS" >/dev/null 2>&1 &
	pids+=($!)
	local pin=()
	[ -n "$SVART_CPUS" ] && pin=(taskset -c "$SVART_CPUS")
	DNS_PORT=$DNS_PORT ADMIN_PORT=$ADMIN_PORT DB_PATH="$WORK/svart.db" ARCHIVE_PATH="$WORK/archives" \
		ADMIN_USER=admin ADMIN_PASSWORD="$PASS" LOG_LEVEL=error ALLOW_PRIVATE_LIST_URLS=true \
		PPROF_ADDR=${PROFILE_DIR:+127.0.0.1:$PPROF_PORT} "${pin[@]}" "$BIN" >"$WORK/svart.log" 2>&1 &
	SVART=$!
	pids+=("$SVART")
	for _ in $(seq 1 100); do
		curl -sf "$API/health" >/dev/null && break
		sleep 0.1
	done
	curl -sf -c "$JAR" -H 'Content-Type: application/json' \
		-d "{\"username\":\"admin\",\"password\":\"$PASS\"}" "$API/api/auth/login" >/dev/null
	api -X POST -d "{\"upstream\":\"127.0.0.1:${UP_PORT}\",\"enabled\":true}" "$API/api/upstreams" >/dev/null
	# Remove any seeded default upstreams so misses only reach the local stub.
	api "$API/api/upstreams" | python3 -c '
import json,sys
for u in json.load(sys.stdin)["data"] or []:
    if not u["upstream"].startswith("127.0.0.1:"): print(u["id"])' |
		while read -r id; do api -X DELETE "$API/api/upstreams/$id" >/dev/null; done
	RANGE_ID=$(api -X POST -d '{"name":"everyone","cidr":"0.0.0.0/0"}' "$API/api/ranges" | json_field '["data"]["id"]')
}

# add_list FILE -> prints "ID COUNT" once the list is loaded, assigned to RANGE_ID.
add_list() {
	local f=$1 id count=0
	id=$(api -X POST -d "{\"url\":\"http://127.0.0.1:${LIST_PORT}/$f\",\"alias\":\"$f\",\"enabled\":true}" \
		"$API/api/blocklists" | json_field '["data"]["id"]')
	for _ in $(seq 1 600); do
		count=$(api "$API/api/blocklists" | python3 -c "
import json,sys
for b in json.load(sys.stdin)['data'] or []:
    if b['id']==$id: print(b['domain_count'])")
		[ "${count:-0}" -gt 0 ] && break
		sleep 0.5
	done
	api -X POST "$API/api/ranges/$RANGE_ID/blocklists/$id" >/dev/null
	echo "$id $count"
}

rss_mb() { awk '/VmRSS/ {printf "%.1f", $2/1024}' "/proc/$SVART/status"; }
