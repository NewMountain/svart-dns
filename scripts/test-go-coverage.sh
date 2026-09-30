#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
root=$PWD
bash "$root/scripts/check-go-packages.sh"
mkdir -p "${GO_COVERAGE_DIR:-coverage-go}"
evidence=$(mktemp -d "${GO_COVERAGE_DIR:-coverage-go}/run.XXXXXX")
evidence=$(realpath "$evidence")
printf 'Complete Go coverage evidence: %s\n' "$evidence"
git ls-files '*.go' >"$evidence/owned-go-files.txt"
status=0
for module in root benchmarks; do
	if [[ "$module" == root ]]; then
		cd "$root"
		go list . ./internal/... ./internal/telemetry/testdata/version ./cmd/... >"$evidence/$module-packages.txt"
	else
		cd "$root/benchmarks"
		go list ./... >"$evidence/$module-packages.txt"
	fi
	mapfile -t packages <"$evidence/$module-packages.txt"
	coverage_packages=$(
		IFS=,
		printf '%s' "${packages[*]}"
	)
	go list -json "${packages[@]}" >"$evidence/$module-package-files.json"
	# Include platform-inactive production statements conservatively as uncovered.
	# Their raw compiler instrumentation and exact paths remain available for audit.
	go list -f '{{range .IgnoredGoFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}' "${packages[@]}" >"$evidence/$module-ignored-files.txt"
	uncompiled=0
	index=0
	: >"$evidence/$module-uncompiled-statements.txt"
	while IFS= read -r source; do
		[[ -z "$source" || "$source" == *_test.go ]] && continue
		instrumented="$evidence/$module-ignored-$index.go"
		go tool cover -mode=count -var=ownedIgnoredCoverage -o "$instrumented" "$source"
		statements=$(awk '/^[[:space:]]*NumStmt:/ { counts=1; next } counts && /^[[:space:]]*},/ { counts=0 } counts { total += $1 + 0 } END { print total + 0 }' "$instrumented")
		printf '%s\t%s\n' "$source" "$statements" >>"$evidence/$module-uncompiled-statements.txt"
		uncompiled=$((uncompiled + statements))
		index=$((index + 1))
	done <"$evidence/$module-ignored-files.txt"
	if ! go test -timeout=30m -count=1 -coverpkg="$coverage_packages" -coverprofile="$evidence/$module.cover" "${packages[@]}"; then
		status=1
	fi
	if ! "$root/scripts/check-go-coverage.sh" "$evidence/$module.cover" "$uncompiled" >"$evidence/$module-functions.txt"; then
		status=1
	fi
	cat "$evidence/$module-functions.txt"
done
exit "$status"
