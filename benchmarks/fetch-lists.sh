#!/usr/bin/env bash
# Download the published blocklists the benchmarks use into a local
# directory. This is the only benchmark step that needs the internet; every
# run afterwards reads these files from disk.
#
# Usage: benchmarks/fetch-lists.sh [dir]   (default: benchmarks/lists)
set -euo pipefail
DIR=${1:-$(dirname "${BASH_SOURCE[0]}")/lists}
mkdir -p "$DIR"
while read -r name url; do
	[ -f "$DIR/$name" ] && continue
	curl -fsSL --retry 3 -m 300 -o "$DIR/$name.tmp" "$url"
	mv "$DIR/$name.tmp" "$DIR/$name"
	echo "$name: $(grep -vc '^[#!]' "$DIR/$name") lines"
done <<'LISTS'
stevenblack.txt https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts
hagezi-pro.txt https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/pro-onlydomains.txt
oisd-big.txt https://big.oisd.nl/domainswild2
hagezi-ultimate.txt https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/ultimate-onlydomains.txt
hagezi-tif.txt https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/tif-onlydomains.txt
LISTS
