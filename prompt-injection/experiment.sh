#!/usr/bin/env bash
set -euo pipefail

echo ""
echo "====================================================="
echo "  Prompt Safety Escaper — 2026 Experiment"
echo "  Testing detection of modern prompt injection techniques"
echo "====================================================="
echo ""

echo "  Step 0: Verify Go is available"
command -v go &>/dev/null || { echo "  FATAL: Go is not installed."; exit 1; }
echo "  Go: $(go version | awk '{print $3}')"
echo ""

echo "  Step 1: Verify sample_injection.html exists"
if [ ! -f sample_injection.html ]; then
  echo "  FATAL: sample_injection.html not found"
  exit 1
fi
echo "  ✅ Found"
echo ""

echo "  Step 2: Run the experiment"
go run .
echo ""

echo "  ✅ Done"
echo ""
