#!/usr/bin/env bash
set -euo pipefail

echo "==> Go"
(cd go-server && go mod tidy && go run main.go &) ; sleep 2; ./bench.sh; pkill -f "go-server" || true

echo "==> TypeScript"
(cd ts-server && npm install && npm start &) ; sleep 5; ./bench.sh; pkill -f "tsx server.ts" || true

echo "==> Rust"
(cd rust-server && cargo run --release &) ; sleep 8; ./bench.sh; pkill -f "rust-server" || true
