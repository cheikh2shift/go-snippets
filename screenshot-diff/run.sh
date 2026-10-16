#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

if [ ! -f go.mod ]; then
  go mod init screenshot-diff
fi

if [ ! -f go.sum ]; then
  go get golang.org/x/image
fi

if [ $# -lt 3 ]; then
  echo "Usage: $0 <frame1.png> <frame2.png> <output.png>"
  exit 1
fi

go run main.go "$1" "$2" "$3"
