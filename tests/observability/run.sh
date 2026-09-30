#!/usr/bin/env bash
set -euo pipefail
: "${PLAYWRIGHT_BROWSER:?Set the path to its Chromium executable}"
INFRA_ROOT="$(realpath "${1:?infra checkout path}")"
export EVIDENCE
EVIDENCE="$(realpath -m "${2:?new evidence directory}")"
if [[ -e "$EVIDENCE" ]]; then
	echo 'Use a fresh evidence directory; existing evidence is never overwritten.' >&2
	exit 1
fi
mkdir -p "$EVIDENCE"/{config,logs,libs,tmp,provisioning/{alerting,dashboards,datasources},dashboards}
root="$(cd "$(dirname "$0")/../.." && pwd)"
export TMPDIR="$EVIDENCE/tmp" GOMAXPROCS=4
export COMPOSE_PROJECT_NAME="svart-observe-$$"
export SVART_TEST_PASSWORD
SVART_TEST_PASSWORD="$(openssl rand -hex 24)"
for name in APP_IMAGE FIXTURE_IMAGE ALLOY_IMAGE PROMETHEUS_IMAGE LOKI_IMAGE TEMPO_IMAGE PYROSCOPE_IMAGE GRAFANA_IMAGE; do
	value="${!name:?Set an explicit locally available image reference or digest}"
	id="$(docker image inspect "$value" --format '{{.Id}}')"
	export "$name=$id"
	printf '%s %s\n' "$name" "$id" >>"$EVIDENCE/images.txt"
done
for service in loki tempo pyroscope; do
	cp "$INFRA_ROOT/lgtm/$service/$service.yaml" "$EVIDENCE/config/$service.yaml"
done
cp "$INFRA_ROOT/lgtm/grafana/dashboards/svart-dns.json" "$EVIDENCE/dashboards/"
cp "$INFRA_ROOT/lgtm/grafana/provisioning/datasources/datasources.yaml" "$EVIDENCE/provisioning/datasources/"
cp "$INFRA_ROOT/lgtm/grafana/provisioning/alerting/svart-dns-alerts.yaml" "$EVIDENCE/provisioning/alerting/"
cp "$root/tests/observability/dashboards.yaml" "$EVIDENCE/provisioning/dashboards/"
node -p 'require(process.argv[1]).version' "$root/scripts/node_modules/playwright-core/package.json" >"$EVIDENCE/playwright-version.txt"
git -C "$root" rev-parse HEAD >"$EVIDENCE/service-commit.txt"
git -C "$INFRA_ROOT" rev-parse HEAD >"$EVIDENCE/infra-commit.txt"
git -C "$root" diff >"$EVIDENCE/service.diff"
git -C "$INFRA_ROOT" diff >"$EVIDENCE/infra.diff"
(cd "$root/frontend" && npm ci --ignore-scripts --no-audit --no-fund && npm run build) >"$EVIDENCE/frontend.log" 2>&1
(cd "$root" && go build -p 2 -o "$EVIDENCE/svart-dns" .) >"$EVIDENCE/build.log" 2>&1
go version -m "$EVIDENCE/svart-dns" >"$EVIDENCE/build-provenance.txt"
sha256sum "$EVIDENCE/svart-dns" >"$EVIDENCE/binary.sha256"
ldd "$EVIDENCE/svart-dns" >"$EVIDENCE/runtime-linkage.txt"
while read -r library; do
	cp -L "$library" "$EVIDENCE/libs/"
done < <(awk '/=> \// {print $3} /^[[:space:]]*\// {print $1}' "$EVIDENCE/runtime-linkage.txt")
compose=(docker compose -f "$root/tests/observability/compose.yaml")
log_pid=''
cleanup() {
	"${compose[@]}" logs --no-color >"$EVIDENCE/collectors.log" 2>&1 || true
	"${compose[@]}" down --volumes >"$EVIDENCE/teardown.log" 2>&1 || true
	if [[ -n "$log_pid" ]]; then wait "$log_pid" || true; fi
}
trap cleanup EXIT
touch "$EVIDENCE/logs/app.log"
"${compose[@]}" up -d >"$EVIDENCE/start.log" 2>&1
"${compose[@]}" logs --no-color --no-log-prefix --follow app >"$EVIDENCE/logs/app.log" 2>&1 &
log_pid=$!
python3 "$root/tests/observability/verify.py" >"$EVIDENCE/verification.log" 2>&1
python3 "$root/tests/observability/verify.py" --no-data >"$EVIDENCE/no-data.log" 2>&1
node "$root/tests/observability/render.mjs" >"$EVIDENCE/render.log" 2>&1
printf 'Verified disposable LGTM signals, rendered panels and absence recovery: %s\n' "$EVIDENCE"
