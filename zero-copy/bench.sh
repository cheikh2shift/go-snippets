#!/usr/bin/env bash
# Requires: go (1.21+) and wrk (https://github.com/wg/wrk)
#
# Terminal 1 — start the server:
#   go run zerocopy-bench.go
#
# Terminal 2 — run this script:
#   bash bench.sh

set -e

echo "=== RAW (zero-copy path) ==="
wrk -t4 -c200 -d30s http://localhost:8080/raw

echo
echo "=== WRAPPED (copy path) ==="
wrk -t4 -c200 -d30s http://localhost:8080/wrapped

echo
echo "--- How to capture CPU + confirm zero-copy ---"
echo "1) CPU during the run (Terminal 3):"
echo "     mpstat 1          # or: top -p \$(pgrep zerocopy-bench)"
echo
echo "2) Confirm syscalls with strace (Terminal 3):"
echo "     strace -f -e trace=sendfile,splice,read,write -p \$(pgrep zerocopy-bench)"
echo "   /raw should show sendfile/splice; /wrapped shows read/write pairs."
echo
echo "3) To prove the FIX: uncomment the ReadFrom method in zerocopy-bench.go,"
echo "   re-run, and /wrapped should match /raw."
