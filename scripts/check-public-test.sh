#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/repo/scripts" "$WORK/bin"
cp "$ROOT/scripts/check-public.sh" "$ROOT/scripts/check-public-archives.py" "$WORK/repo/scripts/"
git -C "$WORK/repo" init -q
printf 'public fixture\n' >"$WORK/repo/README.md"
git -C "$WORK/repo" add .
REAL_GIT=$(command -v git)
export REAL_GIT
cat >"$WORK/bin/git" <<'SH'
#!/usr/bin/env bash
if [[ "$1" == grep ]]; then
 echo 'simulated Git read failure' >&2
 exit 2
fi
exec "$REAL_GIT" "$@"
SH
chmod +x "$WORK/bin/git"
if PATH="$WORK/bin:$PATH" "$WORK/repo/scripts/check-public.sh" >"$WORK/result" 2>&1; then
	cat "$WORK/result"
	echo 'FAIL: check-public accepted a Git read error as clean' >&2
	exit 1
fi
printf 'check-public failure-path test passed\n'

"$WORK/repo/scripts/check-public.sh" >"$WORK/clean-result" 2>&1
python3 - "$WORK/repo/evidence.txt.gz" <<'PYTEST'
import gzip, sys
with gzip.open(sys.argv[1], "wb") as out:
    out.write((".".join(["10", "10", "1", "9"]) + "\n").encode())
PYTEST
git -C "$WORK/repo" add evidence.txt.gz
if "$WORK/repo/scripts/check-public.sh" >"$WORK/archive-result" 2>&1; then
	echo 'FAIL: check-public ignored a compressed private marker' >&2
	exit 1
fi
grep -q "evidence.txt.gz:1: private markers: $(printf '%s.%s.%s.%s' 10 10 1 9)" "$WORK/archive-result"
printf 'check-public clean-tree and compressed-evidence tests passed\n'

printf 'invalid gzip fixture\n' >"$WORK/repo/evidence.txt.gz"
status=0
"$WORK/repo/scripts/check-public.sh" >"$WORK/invalid-archive-result" 2>&1 || status=$?
[[ "$status" -eq 2 ]] || {
	echo "FAIL: archive read failure must be unavailable, got $status" >&2
	exit 1
}
grep -q 'archive scan unavailable' "$WORK/invalid-archive-result"
printf 'check-public corrupt-archive test passed\n'
