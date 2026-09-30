#!/usr/bin/env bash
# Head-to-head benchmark: svart-dns vs Pi-hole vs AdGuard Home vs Technitium.
#
# Every server gets the same blocklist (served from a local directory), the
# same deterministic local upstream (benchmarks/stubupstream) and the same
# CPU set, and is benchmarked alone. Nothing touches the internet once the
# images are present, so results measure the servers, not a public resolver.
#
# Usage:
#   benchmarks/compare.sh <lists-dir> [results-dir]
#
# Environment:
#   SVART_BIN      run this svart-dns binary natively (default: build from repo)
#   SERVERS        subset to run, e.g. "svart adguard" (default: all four)
#   QUERIES        measured queries per scenario (default 300000)
#   SERVER_CPUS    CPUs for the server under test (default 0-3)
#   BENCH_CPUS     CPUs for the load generator (default 4-7)
#   STUB_CPUS      CPUs for the stub upstream (default: BENCH_CPUS)
#   LIST           list file name inside <lists-dir> (default hagezi-pro.txt)
#
# Settings changed from each product's defaults, and why:
#   Pi-hole    dns.rateLimit.count=0   default 1000 queries/60 s per client
#              --shm-size=1g           FTL's query store outgrows Docker's 64 MiB /dev/shm
#   AdGuard    ratelimit=0             default 20 qps per /24
#   Technitium empty qpmPrefixLimitsIPv4/IPv6 tables (verified by API readback)
#              dnssecValidation=false unsigned synthetic upstream has no DNSKEYs
# A failed setup exits nonzero; partial results and logs remain available.
# Without these the benchmark measures each product's rate limiter, not its
# resolver path. Everything else (caching and logging) stays at defaults.
set -euo pipefail

LISTS=$(realpath "$1")
RESULTS=$(realpath -m "${2:-benchmarks/results/$(date -u +%Y%m%dT%H%M%SZ)}")
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SERVERS=${SERVERS:-svart pihole adguard technitium}
QUERIES=${QUERIES:-300000}
SERVER_CPUS=${SERVER_CPUS:-0-3}
BENCH_CPUS=${BENCH_CPUS:-4-7}
STUB_CPUS=${STUB_CPUS:-$BENCH_CPUS}
LIST=${LIST:-hagezi-pro.txt}
SOURCE_NET=${SOURCE_NET:-127.0.0.0}
UP_PORT=40053
LIST_PORT=40080
PASS="bench-$(date +%s)-Pass"

PIHOLE_IMAGE=${PIHOLE_IMAGE:-pihole/pihole:2026.09.0}
ADGUARD_IMAGE=${ADGUARD_IMAGE:-adguard/adguardhome:v0.107.79}
TECHNITIUM_IMAGE=${TECHNITIUM_IMAGE:-technitium/dns-server:15.5.0}

DOCKER_NETWORK=${DOCKER_NETWORK:-host}
CONTAINER_PREFIX=${CONTAINER_PREFIX:-bench}
SERVER_MEMORY=${SERVER_MEMORY:-16g}
# Every run retains its database, archives, cookies, configurations and raw samples.
# Callers choose a new directory; never merge a fresh run into old evidence.
if [ -e "$RESULTS" ]; then
	echo "result directory already exists: $RESULTS" >&2
	exit 1
fi
mkdir -p "$RESULTS/work"
WORK=$RESULTS/work
pids=()
containers=()
cleanup() {
	for c in "${containers[@]}"; do
		if docker inspect "$c" >/dev/null 2>&1; then
			docker stop --time 60 "$c" >/dev/null 2>&1 || true
			if retain_container "$c"; then
				docker rm "$c" >/dev/null
			else
				log "retention failed; container $c remains for recovery"
			fi
		fi
	done
	# Only still-running children belong to this shell; completed PIDs can be reused.
	for p in $(jobs -pr); do kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	chmod -R go-rwx "$RESULTS"
	if [ "$(id -u)" = 0 ]; then chown -R "$(stat -c %u:%g "$LISTS")" "$RESULTS"; fi
}
trap cleanup EXIT

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

# A domain that is in the list and resolves at the stub, used to prove each
# server is actually blocking before it is measured.
probe_domain() { grep -v '^[#!]' "$LISTS/$LIST" | sed -n '1000p' | awk '{print $NF}' | sed 's/^\*\.//'; }

wait_blocking() {
	local name=$1 port=$2 domain
	domain=$(probe_domain)
	for _ in $(seq 1 360); do
		local out
		out=$(dig +time=1 +tries=1 @127.0.0.1 -p "$port" "$domain" A 2>/dev/null || true)
		if grep -q 'status: NXDOMAIN' <<<"$out" || grep -qE $'\tA\t(0\\.0\\.0\\.0)$' <<<"$out"; then
			log "$name blocks $domain"
			return 0
		fi
		"${REARM:-true}"
		sleep 1
	done
	log "FAIL: $name never blocked $domain"
	return 1
}

wait_port() {
	for _ in $(seq 1 120); do
		dig +time=1 +tries=1 @127.0.0.1 -p "$1" localhost A >/dev/null 2>&1 && return 0
		sleep 1
	done
	return 1
}

server_rss_mb() {
	local pid=$1
	awk '/VmRSS/ {printf "%.1f", $2/1024}' "/proc/$pid/status" 2>/dev/null || echo "?"
}

# Sum RSS of every process in a container (docker stats includes page cache,
# which would penalize servers that write logs).
container_rss_mb() {
	local total=0 p
	for p in $(docker top "$1" -eo pid 2>/dev/null | tail -n +2); do
		total=$((total + $(awk '/VmRSS/ {print $2}' "/proc/$p/status" 2>/dev/null || echo 0)))
	done
	awk -v k="$total" 'BEGIN {printf "%.1f", k/1024}'
}

# Preserve every resource sample. Container peak RSS is a sampled sum, not
# cgroup memory (which includes page cache); process VmHWM remains in each sample.
monitor_processes() {
	local name=$1 pid=$2
	while kill -0 "$pid" 2>/dev/null; do
		printf 'SAMPLE %s PID %s\n' "$(date +%s%N)" "$pid"
		cat "/proc/$pid/status" 2>/dev/null || true
		cat "/proc/$pid/io" 2>/dev/null || true
		sleep 0.1
	done >"$RESULTS/$name-process-samples.txt"
}
monitor_container() {
	local container=$1
	while docker inspect "$container" >/dev/null 2>&1; do
		printf 'SAMPLE %s\n' "$(date +%s%N)"
		for pid in $(docker top "$container" -eo pid 2>/dev/null | tail -n +2); do
			printf 'PID %s\n' "$pid"
			cat "/proc/$pid/status" 2>/dev/null || true
			cat "/proc/$pid/io" 2>/dev/null || true
		done
		sleep 0.1
	done >"$RESULTS/$container-process-samples.txt"
}
retain_container() {
	local container=$1
	docker logs "$container" >"$RESULTS/$container.log" 2>&1 || return
	docker export "$container" >"$RESULTS/$container-filesystem.tar" || return
	case "$container" in
	*-pihole) docker cp "$container:/etc/pihole" "$RESULTS/$container-data" || return ;;
	*-adguard)
		docker cp "$container:/opt/adguardhome/work" "$RESULTS/$container-data" || return
		docker cp "$container:/opt/adguardhome/conf" "$RESULTS/$container-config" || return
		;;
	*-technitium) docker cp "$container:/etc/dns" "$RESULTS/$container-data" || return ;;
	esac
}
phase() {
	local name=$1 label=$2 start=$3
	printf '%s %s start_ns=%s observed_ns=%s\n' "$name" "$label" "$start" "$(date +%s%N)" >>"$RESULTS/startup-phases.txt"
}

run_scenarios() {
	local name=$1 port=$2 rss_cmd=$3
	local scenario
	# A blocking probe alone can pass while every forwarded query SERVFAILs.
	local answer
	answer=$(dig +time=2 +tries=1 +short @127.0.0.1 -p "$port" example.com A)
	if ! grep -qE '^198\.(18|19)\.' <<<"$answer"; then
		log "FAIL: $name did not return the local upstream answer for example.com"
		return 1
	fi
	for scenario in \
		"hits:-blocked-pct 0 -miss-pct 0" \
		"mixed:-blocked-pct 20 -miss-pct 0" \
		"blocked:-blocked-pct 100 -miss-pct 0" \
		"misses:-blocked-pct 10 -miss-pct 50" \
		"mixed-c200:-blocked-pct 20 -miss-pct 0 -clients 200" \
		"clients64:-blocked-pct 20 -miss-pct 5 -clients 64 -sources 64 -source-net $SOURCE_NET" \
		"fixed-10k:-blocked-pct 20 -miss-pct 5 -clients 200 -rate 10000"; do
		local sname=${scenario%%:*}
		# shellcheck disable=SC2086 # flags are intentionally word-split
		taskset -c "$BENCH_CPUS" "$WORK/dnsbench" -server 127.0.0.1 -port "$port" -queries "$QUERIES" \
			-blocklist "$LISTS/$LIST" -label "$name/$sname" -json ${scenario#*:} >"$RESULTS/$name-$sname.json"
		cp "$RESULTS/$name-$sname.json" "$WORK/out.json"
		local rss
		rss=$($rss_cmd)
		python3 - "$WORK/out.json" "$rss" "$name" "$sname" >>"$RESULTS/results.jsonl" <<'EOF'
import json, sys
s = json.load(open(sys.argv[1]))
s.update(server=sys.argv[3], scenario=sys.argv[4], rss_mib=float(sys.argv[2]) if sys.argv[2] != "?" else None)
print(json.dumps(s))
EOF
		log "$name/$sname: $(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print("%.0f qps p50 %.0fus p99 %.0fus blocked %d timeouts %d errors %d" % (d["qps"], d["p50_us"], d["p99_us"], d["blocked"], d["timeouts"], d["errors"]))' "$WORK/out.json") rss ${rss}MiB"
	done
	sleep 15
	printf '%s %s\n' "$name" "$($rss_cmd)" >>"$RESULTS/steady-rss-mib.txt"
}

# ---------------------------------------------------------------- svart-dns
bench_svart() {
	local started
	started=$(date +%s%N)
	local bin=${SVART_BIN:-}
	if [ -z "$bin" ]; then
		(cd "$HERE/.." && go build -o "$WORK/svart-dns" ./cmd/svart-dns)
		bin=$WORK/svart-dns
	fi
	local api=http://127.0.0.1:41080 jar=$WORK/svart.jar
	DNS_PORT=41053 ADMIN_PORT=41080 DB_PATH="$WORK/svart.db" ARCHIVE_PATH="$WORK/archives" \
		ADMIN_USER=admin ADMIN_PASSWORD="$PASS" LOG_LEVEL=warn ALLOW_PRIVATE_LIST_URLS=true \
		taskset -c "$SERVER_CPUS" "$bin" >"$RESULTS/svart.log" 2>&1 &
	local pid=$!
	pids+=("$pid")
	monitor_processes svart "$pid" &
	pids+=($!)
	for _ in $(seq 1 100); do
		curl -sf "$api/health" >/dev/null && break
		sleep 0.2
	done
	kill -0 "$pid" 2>/dev/null || {
		log "FAIL: svart exited; see $RESULTS/svart.log"
		return 1
	}
	curl -sf -c "$jar" -H 'Content-Type: application/json' \
		-d "{\"username\":\"admin\",\"password\":\"$PASS\"}" "$api/api/auth/login" >/dev/null
	sv() { curl -sf -b "$jar" -H 'Content-Type: application/json' -H "Origin: $api" "$@"; }
	sv -X POST -d "{\"upstream\":\"127.0.0.1:${UP_PORT}\",\"enabled\":true}" "$api/api/upstreams" >/dev/null
	sv "$api/api/upstreams" | python3 -c '
import json,sys
for u in json.load(sys.stdin)["data"] or []:
    if not u["upstream"].startswith("127.0.0.1:"): print(u["id"])' |
		while read -r id; do sv -X DELETE "$api/api/upstreams/$id" >/dev/null; done
	phase svart started "$started"
	local rid lid
	rid=$(sv -X POST -d '{"name":"everyone","cidr":"0.0.0.0/0"}' "$api/api/ranges" | json 'd["data"]["id"]')
	lid=$(sv -X POST -d "{\"url\":\"http://127.0.0.1:${LIST_PORT}/$LIST\",\"alias\":\"$LIST\",\"enabled\":true}" \
		"$api/api/blocklists" | json 'd["data"]["id"]')
	sv -X POST "$api/api/ranges/$rid/blocklists/$lid" >/dev/null
	# Older builds only rebuild range policy on range mutations (B1), so
	# re-assert the assignment until the loaded list takes effect.
	# shellcheck disable=SC2329 # invoked indirectly as a benchmark callback
	svart_rearm() { sv -X POST "$api/api/ranges/$rid/blocklists/$lid" >/dev/null || true; }
	REARM=svart_rearm wait_blocking svart 41053
	# shellcheck disable=SC2329 # invoked indirectly as a benchmark callback
	svart_rss() { server_rss_mb "$pid"; }
	phase svart blocking "$started"
	run_scenarios svart 41053 svart_rss
	kill "$pid"
	wait "$pid" 2>/dev/null || true
}

# ------------------------------------------------------------------ Pi-hole
bench_pihole() {
	local started
	started=$(date +%s%N)
	local c=$CONTAINER_PREFIX-pihole
	# Bootstrap solely from the local list. Pi-hole's gravity checks getent for
	# raw.githubusercontent.com even for literal-IP local URLs; its hosts entry
	# satisfies that prerequisite without enabling external network access.
	# FTL keeps every query in /dev/shm; Docker's 64 MiB default runs out after
	# ~1M queries (realloc_shm: No space left on device) and FTL stops answering.
	docker run --pull=never -d --name "$c" --network "$DOCKER_NETWORK" --memory "$SERVER_MEMORY" --memory-swap "$SERVER_MEMORY" --cpuset-cpus "$SERVER_CPUS" --shm-size=1g \
		-e FTLCONF_dns_port=42053 -e FTLCONF_webserver_port=42080 \
		-e FTLCONF_dns_upstreams="127.0.0.1#${UP_PORT}" -e FTLCONF_dns_listeningMode=ALL \
		-e FTLCONF_dns_rateLimit_count=0 -e FTLCONF_webserver_api_password="$PASS" \
		-e FTLCONF_misc_etc_dnsmasq_d=false \
		--entrypoint /bin/bash "$PIHOLE_IMAGE" -c \
		'printf "127.0.0.1 raw.githubusercontent.com\n" >> /etc/hosts; printf "%s\n" "$1" > /etc/pihole/adlists.list; exec /usr/bin/start.sh' \
		benchmark "http://127.0.0.1:${LIST_PORT}/$LIST" >/dev/null
	containers+=("$c")
	monitor_container "$c" &
	pids+=($!)
	wait_port 42053
	phase pihole started "$started"
	REARM=true wait_blocking pihole 42053
	docker exec "$c" pihole-FTL --config dns.rateLimit.count >"$RESULTS/pihole-rate-limit-readback.txt"
	python3 - "$RESULTS/pihole-rate-limit-readback.txt" <<'EOF'
import sys
if open(sys.argv[1]).read().strip() != "0":
    raise SystemExit("Pi-hole rate limit is not disabled; inspect retained readback")
EOF
	# shellcheck disable=SC2329 # invoked indirectly as a benchmark callback
	pihole_rss() { container_rss_mb "$c"; }
	phase pihole blocking "$started"
	run_scenarios pihole 42053 pihole_rss
	docker logs "$c" >"$RESULTS/$c.log" 2>&1 || true
	docker stop --time 60 "$c" >/dev/null
	retain_container "$c"
	docker rm "$c" >/dev/null
}

# ------------------------------------------------------------- AdGuard Home
bench_adguard() {
	local started
	started=$(date +%s%N)
	local c=$CONTAINER_PREFIX-adguard api=http://127.0.0.1:43080 jar=$WORK/ag.jar
	docker run --pull=never -d --name "$c" --network "$DOCKER_NETWORK" --memory "$SERVER_MEMORY" --memory-swap "$SERVER_MEMORY" --cpuset-cpus "$SERVER_CPUS" "$ADGUARD_IMAGE" >/dev/null
	containers+=("$c")
	monitor_container "$c" &
	pids+=($!)
	for _ in $(seq 1 60); do
		curl -sf http://127.0.0.1:3000/control/install/get_addresses >/dev/null && break
		sleep 1
	done
	curl -sf -X POST http://127.0.0.1:3000/control/install/configure -H 'Content-Type: application/json' \
		-d "{\"web\":{\"ip\":\"127.0.0.1\",\"port\":43080},\"dns\":{\"ip\":\"127.0.0.1\",\"port\":43053},\"username\":\"bench\",\"password\":\"$PASS\"}" >/dev/null
	for _ in $(seq 1 60); do
		curl -sf -o /dev/null "$api/" && break
		sleep 1
	done
	curl -sf -c "$jar" -X POST "$api/control/login" -H 'Content-Type: application/json' \
		-d "{\"name\":\"bench\",\"password\":\"$PASS\"}" >/dev/null
	ag() { curl -sf -b "$jar" -H 'Content-Type: application/json' "$@"; }
	ag -X POST "$api/control/dns_config" -d "{\"upstream_dns\":[\"127.0.0.1:${UP_PORT}\"],\"ratelimit\":0}" >/dev/null
	ag "$api/control/dns_info" >"$RESULTS/adguard-dns-readback.json"
	python3 - "$RESULTS/adguard-dns-readback.json" "$UP_PORT" <<'EOF'
import json, sys
settings = json.load(open(sys.argv[1]))
if settings.get("ratelimit") != 0:
    raise SystemExit("AdGuard rate limit is not disabled; inspect retained readback")
if settings.get("upstream_dns") != ["127.0.0.1:" + sys.argv[2]]:
    raise SystemExit("AdGuard is not using the isolated local upstream")
EOF
	phase adguard started "$started"
	ag -X POST "$api/control/filtering/add_url" \
		-d "{\"name\":\"$LIST\",\"url\":\"http://127.0.0.1:${LIST_PORT}/$LIST\",\"whitelist\":false}" >/dev/null
	REARM=true wait_blocking adguard 43053
	# shellcheck disable=SC2329 # invoked indirectly as a benchmark callback
	adguard_rss() { container_rss_mb "$c"; }
	phase adguard blocking "$started"
	run_scenarios adguard 43053 adguard_rss
	docker logs "$c" >"$RESULTS/$c.log" 2>&1 || true
	docker stop --time 60 "$c" >/dev/null
	retain_container "$c"
	docker rm "$c" >/dev/null
}

# --------------------------------------------------------------- Technitium
bench_technitium() {
	local started
	started=$(date +%s%N)
	local c=$CONTAINER_PREFIX-technitium api=http://127.0.0.1:44080
	docker run --pull=never -d --name "$c" --network "$DOCKER_NETWORK" --memory "$SERVER_MEMORY" --memory-swap "$SERVER_MEMORY" --cpuset-cpus "$SERVER_CPUS" \
		-e DNS_SERVER_ADMIN_PASSWORD="$PASS" -e DNS_SERVER_WEB_SERVICE_HTTP_PORT=44080 \
		-e DNS_SERVER_FORWARDERS="127.0.0.1:${UP_PORT}" -e DNS_SERVER_FORWARDER_PROTOCOL=Udp \
		-e DNS_SERVER_RECURSION=AllowOnlyForPrivateNetworks -e DNS_SERVER_ENABLE_BLOCKING=true \
		-e DNS_SERVER_BLOCK_LIST_URLS="http://127.0.0.1:${LIST_PORT}/$LIST" \
		"$TECHNITIUM_IMAGE" >/dev/null
	containers+=("$c")
	monitor_container "$c" &
	pids+=($!)
	local token=""
	for _ in $(seq 1 90); do
		token=$(curl -sf "$api/api/user/login?user=admin&pass=$PASS" 2>/dev/null | json 'd.get("token","")' 2>/dev/null || true)
		[ -n "$token" ] && break
		sleep 1
	done
	[ -n "$token" ] || {
		log "FAIL: technitium login"
		return 1
	}
	# Version 15.5 uses prefix tables; the API silently ignores the old
	# qpmLimitRequests/qpmLimitErrors names. The shipped UI sends false for
	# an empty table. Preserve actual settings and require effective readback.
	curl -sf "$api/api/settings/set?token=$token&dnsServerLocalEndPoints=127.0.0.1:44053&qpmPrefixLimitsIPv4=false&qpmPrefixLimitsIPv6=false&dnssecValidation=false" >"$RESULTS/technitium-settings-set.json"
	curl -sf "$api/api/settings/get?token=$token" >"$RESULTS/technitium-settings-readback.json"
	python3 - "$RESULTS/technitium-settings-readback.json" <<'EOF'
import json, sys
result = json.load(open(sys.argv[1]))
if result.get("status") != "ok":
    raise SystemExit("Technitium settings readback failed; inspect retained response")
settings = result["response"]
for key in ("qpmPrefixLimitsIPv4", "qpmPrefixLimitsIPv6"):
    if key not in settings or settings[key] not in (None, []):
        raise SystemExit("Technitium rate limits remain enabled: " + key)
if settings.get("dnssecValidation") is not False:
    raise SystemExit("Technitium DNSSEC remains enabled against unsigned local stub")
EOF
	wait_port 44053
	phase technitium started "$started"
	curl -sf "$api/api/settings/forceUpdateBlockLists?token=$token" >/dev/null || true
	REARM=true wait_blocking technitium 44053
	# shellcheck disable=SC2329 # invoked indirectly as a benchmark callback
	technitium_rss() { container_rss_mb "$c"; }
	phase technitium blocking "$started"
	run_scenarios technitium 44053 technitium_rss
	docker logs "$c" >"$RESULTS/$c.log" 2>&1 || true
	docker stop --time 60 "$c" >/dev/null
	retain_container "$c"
	docker rm "$c" >/dev/null
}

# ------------------------------------------------------------------- main
# Refuse to start if anything already listens on our ports: otherwise a
# stray process would be benchmarked in place of the server under test.
for product in pihole adguard technitium; do
	if docker inspect "$CONTAINER_PREFIX-$product" >/dev/null 2>&1; then
		log "container name already in use: $CONTAINER_PREFIX-$product"
		exit 1
	fi
done
busy=$(ss -Hlnup 2>/dev/null | awk '{print $4}' | grep -oE ':(40053|40080|41053|41080|42053|42080|43053|43080|44053|44080|3000)$' || true)
busy+=$(ss -Hlntp 2>/dev/null | awk '{print $4}' | grep -oE ':(40080|41080|42080|43080|44080|3000)$' || true)
if [ -n "$busy" ]; then
	log "ports already in use: $busy"
	exit 1
fi
if [ -n "${BENCH_BIN_DIR:-}" ]; then
	cp "$BENCH_BIN_DIR/stubupstream" "$BENCH_BIN_DIR/dnsbench" "$WORK/"
else
	(cd "$HERE" && CGO_ENABLED=0 go build -o "$WORK/stubupstream" ./stubupstream && CGO_ENABLED=0 go build -o "$WORK/dnsbench" ./dnsbench) 2>/dev/null ||
		{ cp "$HERE/../bin/stubupstream" "$HERE/../bin/dnsbench" "$WORK/"; }
fi
taskset -c "$STUB_CPUS" "$WORK/stubupstream" -addr "127.0.0.1:${UP_PORT}" 2>/dev/null &
pids+=($!)
python3 -m http.server "$LIST_PORT" --bind 127.0.0.1 --directory "$LISTS" >/dev/null 2>&1 &
pids+=($!)

{
	echo "host: $(hostname) kernel $(uname -r) cpus $(nproc) model $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2)"
	echo "server cpus $SERVER_CPUS, load generator cpus $BENCH_CPUS, stub cpus $STUB_CPUS, queries/scenario $QUERIES, source network $SOURCE_NET, list $LIST ($(grep -vc '^[#!]' "$LISTS/$LIST") lines)"
	echo "images: $PIHOLE_IMAGE $ADGUARD_IMAGE $TECHNITIUM_IMAGE; svart ${SVART_BIN:-built from $(git -C "$HERE" rev-parse --short HEAD 2>/dev/null || echo source)}"
} >"$RESULTS/environment.txt"

for s in $SERVERS; do
	log "=== $s"
	"bench_$s"
done

python3 - "$RESULTS/results.jsonl" >"$RESULTS/summary.md" <<'EOF'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1])]
scen = []
for r in rows:
    if r["scenario"] not in scen: scen.append(r["scenario"])
servers = []
for r in rows:
    if r["server"] not in servers: servers.append(r["server"])
by = {(r["server"], r["scenario"]): r for r in rows}
print("| scenario | " + " | ".join(servers) + " |")
print("|---|" + "---|" * len(servers))
for s in scen:
    cells = []
    for sv in servers:
        r = by.get((sv, s))
        cells.append("—" if r is None else f'{r["qps"]:,.0f} qps · p50 {r["p50_us"]:.0f} µs · p99 {r["p99_us"]:.0f} µs' + (f' · {r["timeouts"]} timeouts' if r["timeouts"] else "") + (f' · {r["errors"]} errors' if r["errors"] else ""))
    print(f"| {s} | " + " | ".join(cells) + " |")
print()
print("| RSS after scenario (MiB) | " + " | ".join(servers) + " |")
print("|---|" + "---|" * len(servers))
for s in scen:
    print(f"| {s} | " + " | ".join(str(by.get((sv, s), {}).get("rss_mib", "—")) for sv in servers) + " |")
EOF
cat "$RESULTS/summary.md"
