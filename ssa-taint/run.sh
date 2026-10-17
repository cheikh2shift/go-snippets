#!/usr/bin/env bash
# One-shot setup + run for leakcheck.
#
#   bash run.sh              # full report
#   bash run.sh -json out.json
#   bash run.sh -http-source=false
set -euo pipefail

cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "go not found on PATH" >&2
  exit 1
fi

echo "==> go version"
go version

echo
echo "==> fetching golang.org/x/tools"
go get golang.org/x/tools/go/packages@latest
go get golang.org/x/tools/go/ssa@latest
go mod tidy

echo
echo "==> go vet"
go vet ./... || true

echo
echo "==> go test"
go test ./... -v

echo
echo "==> go run ."
go run . "$@"

echo
echo "==> benchmarks"
go test ./... -run XXX -bench . -benchmem -benchtime=300ms
