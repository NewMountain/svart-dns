#!/usr/bin/env bash
# Real Git/Gitleaks/Go boundary checks in a disposable miniature repository.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$(mktemp -d)
GITLEAKS_BIN=${GITLEAKS_BIN:-gitleaks}
export GITLEAKS_BIN
fail() {
	echo "FAIL: $*; retained fixtures: $WORK" >&2
	exit 1
}
mkdir -p "$WORK/source/cmd/svart-dns" "$WORK/source/scripts" "$WORK/source/frontend" "$WORK/source/deploy" "$WORK/source/.forgejo" "$WORK/source/web/data" "$WORK/source/docs/images"
for name in export-public.sh check-public.sh check-public-archives.py check-public-secrets.mjs public-secret-fixtures.json check-licenses.mjs check-doc-links.mjs; do
	cp "$ROOT/scripts/$name" "$WORK/source/scripts/"
done
printf '# Fixture\n\n[Image](docs/images/dashboard.png)\n' >"$WORK/source/README.md"
printf 'image fixture bytes\n' >"$WORK/source/docs/images/dashboard.png"
printf 'private deployment fixture\n' >"$WORK/source/deploy/marker"
printf 'private CI fixture\n' >"$WORK/source/.forgejo/marker"
printf 'ranking fixture\n' >"$WORK/source/web/data/tranco-top10k.csv"
printf '{"packages":{}}\n' >"$WORK/source/frontend/package-lock.json"
printf 'module example.com/export-fixture\n\ngo 1.24\n' >"$WORK/source/go.mod"
printf 'package main\nfunc main() {}\n' >"$WORK/source/cmd/svart-dns/main.go"
printf 'verify:\n\tgo test ./...\n' >"$WORK/source/Makefile"
git -C "$WORK/source" init -q -b main
git -C "$WORK/source" add -A
git -C "$WORK/source" -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm first
printf '\nSecond source commit.\n' >>"$WORK/source/README.md"
git -C "$WORK/source" add README.md
git -C "$WORK/source" -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm second
mkdir "$WORK/existing"
printf 'must survive\n' >"$WORK/existing/sentinel"
if "$WORK/source/scripts/export-public.sh" "$WORK/existing" >"$WORK/existing.log" 2>&1; then fail 'existing destination accepted'; fi
[[ "$(cat "$WORK/existing/sentinel")" == 'must survive' ]] || fail 'existing destination changed'
printf 'dirty\n' >>"$WORK/source/README.md"
if "$WORK/source/scripts/export-public.sh" "$WORK/dirty" >"$WORK/dirty.log" 2>&1; then fail 'dirty source accepted'; fi
[[ ! -e "$WORK/dirty" ]] || fail 'dirty source created destination'
git -C "$WORK/source" restore README.md
"$WORK/source/scripts/export-public.sh" "$WORK/candidate" >"$WORK/success.log" 2>&1 || fail 'valid fixture export failed'
[[ "$(git -C "$WORK/candidate" rev-list --count HEAD)" -eq 1 ]] || fail 'history was inherited'
[[ -z "$(git -C "$WORK/candidate" remote)" ]] || fail 'remote was inherited'
[[ "$(git -C "$WORK/source" rev-list --count HEAD)" -eq 2 ]] || fail 'source history changed'
[[ ! -e "$WORK/candidate/deploy" && ! -e "$WORK/candidate/.forgejo" && ! -e "$WORK/candidate/web/data/tranco-top10k.csv" ]] || fail 'excluded data exported'
cmp "$WORK/source/docs/images/dashboard.png" "$WORK/candidate/docs/images/dashboard.png" || fail 'runtime/documentation asset lost'
[[ -x "$WORK/candidate.evidence/svart-dns" ]] || fail 'source was not built'
printf '\n[Broken](missing-page.md)\n' >>"$WORK/source/README.md"
git -C "$WORK/source" add README.md
git -C "$WORK/source" -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm 'broken documentation fixture'
if "$WORK/source/scripts/export-public.sh" "$WORK/broken" >"$WORK/broken.log" 2>&1; then fail 'broken links accepted'; fi
if git -C "$WORK/broken" rev-parse --verify HEAD >/dev/null 2>&1; then fail 'failed candidate received a commit'; fi
grep -q 'missing-page.md' "$WORK/broken.evidence/doc-links.log" || fail 'missing full link failure evidence'
git -C "$WORK/source" show HEAD~1:README.md >"$WORK/source/README.md"
printf 'api_key = "%s%s"\n' 0123456789abcdef 0123456789abcdef >"$WORK/source/unreviewed-token.txt"
git -C "$WORK/source" add README.md unreviewed-token.txt
git -C "$WORK/source" -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm 'unreviewed token fixture'
if "$WORK/source/scripts/export-public.sh" "$WORK/leak" >"$WORK/leak.log" 2>&1; then fail 'unreviewed token accepted'; fi
grep -q 'unreviewed-token.txt' "$WORK/leak.evidence/secret-review.log" || fail 'missing exact secret rejection evidence'
if git -C "$WORK/leak" rev-parse --verify HEAD >/dev/null 2>&1; then fail 'secret scan failure received a commit'; fi
printf 'export-public tests passed; complete fixtures and evidence retained at %s\n' "$WORK"
