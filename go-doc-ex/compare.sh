#!/usr/bin/env sh
# compare.sh — show the before/after: plain `go doc` vs `go doc -ex`.
set -euo pipefail

echo "============================================================"
echo "BEFORE: plain 'go doc' (signatures + doc comments only)"
echo "============================================================"
(cd calc && go doc .)

echo
echo "============================================================"
echo "AFTER: 'go doc -ex' (examples now visible)"
echo "============================================================"
(cd calc && go doc -ex .)

echo
echo "============================================================"
echo "DIFF: the -ex flag adds the Example* functions you can't see"
echo "without it. Those same functions are what 'go test' executes."
echo "============================================================"
