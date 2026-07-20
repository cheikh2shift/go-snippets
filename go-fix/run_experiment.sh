#!/usr/bin/env sh
# go fix modernizers — experiment runner
# Requires Go 1.26+ (go.dev/blog/gofix, Feb 2026)
set -e

echo "==> Go version"
go version

echo
echo "==> BEFORE: legacy.go (old idioms)"
cat legacy/legacy.go

echo
echo "==> Capture output BEFORE go fix"
go run . > /tmp/before_output.txt 2>&1
cat /tmp/before_output.txt

echo
echo "==> Running: go fix -diff ./..."
go fix -diff ./... || true

echo
echo "==> Applying fixes for real"
go fix ./...

echo
echo "==> AFTER: legacy.go (modernized)"
cat legacy/legacy.go

echo
echo "==> Capture output AFTER go fix"
go run . > /tmp/after_output.txt 2>&1
cat /tmp/after_output.txt

echo
echo "==> Comparing outputs"
if diff /tmp/before_output.txt /tmp/after_output.txt > /dev/null 2>&1; then
    echo "PASS: Output is identical before and after go fix"
else
    echo "FAIL: Output differs after go fix!"
    diff /tmp/before_output.txt /tmp/after_output.txt
    exit 1
fi
