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
echo "==> Running: go fix -diff ./..."
go fix -diff ./...

echo
echo "==> Applying fixes for real"
go fix ./...

echo
echo "==> AFTER: legacy.go (modernized)"
cat legacy/legacy.go

echo
echo "==> Build + run to prove behavior is unchanged"
go run .
