#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-8080}"
URL="http://localhost:$PORT/"

echo "=== Benchmarking $URL ==="

if command -v hey &>/dev/null; then
	hey -z 10s -c 50 "$URL"
elif command -v autocannon &>/dev/null; then
	autocannon -d 10 -c 50 "$URL"
elif command -v ab &>/dev/null; then
	ab -n 10000 -c 50 "$URL"
else
	echo "Install hey, autocannon, or ab (apache2-utils) to run load tests."
	echo "Quick smoke test:"
	curl -s -o /dev/null -w "HTTP %{http_code} in %{time_total}s\n" "$URL"
fi