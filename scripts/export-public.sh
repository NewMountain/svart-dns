#!/usr/bin/env bash
# Create a verified candidate with one initial commit and no source history.
# Usage: scripts/export-public.sh /absolute/new/destination
set -euo pipefail
umask 077
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
DEST=$(realpath -m "${1:?usage: scripts/export-public.sh /absolute/new/destination}")
EVIDENCE="${DEST}.evidence"
GITLEAKS_BIN=${GITLEAKS_BIN:-gitleaks}
fail() {
	echo "export-public: $*" >&2
	exit 1
}
[[ ! -e "$DEST" && ! -L "$DEST" && ! -e "$EVIDENCE" && ! -L "$EVIDENCE" ]] || fail 'destination or evidence directory already exists; choose a new path'
[[ "$DEST" != "$ROOT" && "$DEST" != "$ROOT/"* ]] || fail 'destination must be outside the source repository'
[[ -z "$(git -C "$ROOT" status --porcelain --untracked-files=all)" ]] || fail 'source is dirty; commit or preserve changes before exporting'
if git -C "$ROOT" ls-files --stage | awk '$1 == 160000 {found=1} END {exit !found}'; then
	fail 'tracked submodules require explicit export support; none were omitted'
fi
command -v "$GITLEAKS_BIN" >/dev/null || fail 'install pinned Gitleaks v8.30.1 and set GITLEAKS_BIN'
tool_version=$("$GITLEAKS_BIN" version)
if [[ "$tool_version" != 8.30.1 && "$tool_version" != v8.30.1 ]]; then
	go version -m "$(command -v "$GITLEAKS_BIN")" | awk '$1 == "mod" && $2 == "github.com/zricethezav/gitleaks/v8" && $3 == "v8.30.1" {ok=1} END {exit !ok}' || fail 'Gitleaks must identify v8.30.1 in its version or Go build metadata'
fi
mkdir -p "$DEST" "$EVIDENCE"
trap 'status=$?; echo "export-public: check failed at line $LINENO (exit $status); candidate and full evidence remain at $DEST and $EVIDENCE" >&2; exit "$status"' ERR
git -C "$ROOT" rev-parse HEAD >"$EVIDENCE/source-commit.txt"
git -C "$ROOT" ls-files -z >"$EVIDENCE/source-files.nul"
# Exclude only the private control plane and retired operator ranking data.
# git archive reads committed files and never includes .git or ignored caches.
git -C "$ROOT" archive --format=tar HEAD -- . ':(exclude)deploy' ':(exclude).forgejo' ':(exclude)web/data/tranco-top10k.csv' >"$EVIDENCE/source.tar"
tar -xf "$EVIDENCE/source.tar" -C "$DEST"
cd "$DEST"
[[ ! -e web/data/tranco-top10k.csv ]] || fail 'retired ranking data remains in candidate'
git init -q -b main
git add -A
scripts/check-public.sh >"$EVIDENCE/private-check.log" 2>&1
printf '[extend]\nuseDefault = true\n' >"$EVIDENCE/gitleaks-default.toml"
: >"$EVIDENCE/empty-gitleaks-ignore"
if "$GITLEAKS_BIN" dir --no-banner --max-archive-depth 10 --max-target-megabytes 0 --ignore-gitleaks-allow \
	--gitleaks-ignore-path "$EVIDENCE/empty-gitleaks-ignore" --config "$EVIDENCE/gitleaks-default.toml" \
	--exit-code 42 --report-format json --report-path "$EVIDENCE/gitleaks-all.json" "$DEST" >"$EVIDENCE/gitleaks.log" 2>&1; then
	:
else
	status=$?
	[[ "$status" -eq 42 ]] || fail "Gitleaks could not complete (exit $status); see protected evidence"
fi
node scripts/check-public-secrets.mjs "$EVIDENCE/gitleaks-all.json" "$DEST" >"$EVIDENCE/secret-review.log" 2>&1
node scripts/check-licenses.mjs frontend/package-lock.json >"$EVIDENCE/licenses.log" 2>&1
node scripts/check-doc-links.mjs >"$EVIDENCE/doc-links.log" 2>&1
make verify >"$EVIDENCE/verify.log" 2>&1
go build -trimpath -o "$EVIDENCE/svart-dns" ./cmd/svart-dns >"$EVIDENCE/build.log" 2>&1
# Build outputs stay ignored or outside the candidate. Never absorb generated
# content or a gate's source mutation into the initial commit.
git diff --exit-code >"$EVIDENCE/source-after-checks.diff"
[[ -z "$(git ls-files --others --exclude-standard)" ]] || fail 'verification created unexpected untracked files'
git -c user.name='Svart contributors' -c user.email='contributors@example.invalid' commit -q -m 'Initial public Svart source'
[[ "$(git rev-list --count HEAD)" -eq 1 ]] || fail 'candidate has more than one commit'
[[ -z "$(git remote)" ]] || fail 'candidate unexpectedly has a remote'
[[ -z "$(git status --porcelain)" ]] || fail 'candidate is dirty after verification'
git rev-parse HEAD >"$EVIDENCE/candidate-commit.txt"
printf 'export-public: verified local candidate %s\nevidence: %s\n' "$DEST" "$EVIDENCE"
