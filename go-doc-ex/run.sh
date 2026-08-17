#!/usr/bin/env sh
# run.sh — prove that examples are BOTH tests AND documentation.
set -euo pipefail

echo "============================================================"
echo "STEP 1: go test -v — the examples run as TESTS"
echo "============================================================"
(cd calc && go test -v ./...)

echo
echo "============================================================"
echo "STEP 2: go doc -ex — the examples render as DOCUMENTATION"
echo "============================================================"
(cd calc && go doc -ex .)

echo
echo "============================================================"
echo "PUNCHLINE: one function, two jobs."
echo "  go test  -> it is a TEST"
echo "  go doc -ex -> it is DOCUMENTATION"
echo "============================================================"
