#!/usr/bin/env bash
set -euo pipefail

# Every package executable and its children get an isolated loopback-only world.
# Compilation and dependency resolution finish before go test invokes this wrapper.
unshare -rn bash -ec 'ip link set lo up; exec "$@"' -- "$@"
