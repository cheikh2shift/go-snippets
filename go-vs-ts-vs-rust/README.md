# Go vs TypeScript vs Rust — JSON HTTP Server

Three minimal HTTP servers returning the same JSON payload. Measures whether
rewriting a Node/TypeScript server in Rust is worth the cost — or whether Go
gets you there with far less complexity.

## Layout

```
go-server/main.go       Go, net/http, encoding/json
ts-server/server.ts     TypeScript, node:http
rust-server/main.rs     Rust, axum + tokio
bench.sh                load test against :8080
```

## Run

```bash
# Go
cd go-server && go mod tidy && go run main.go

# TypeScript
cd ts-server && npm install && npm start

# Rust
cd rust-server && cargo run --release
```

Each listens on `:8080`. Run one at a time, then benchmark.

## Benchmark

```bash
chmod +x bench.sh
./bench.sh
```

Uses `hey` if installed, else `autocannon`, else `ab`. Install with:

```bash
# any one
go install github.com/rakyll/hey@latest
npm i -g autocannon
# or
sudo apt install apache2-utils
```

Sample output to capture for the article:

```
Requests/sec, Latency (p50/p99), Total requests
```

## Also measure

```bash
# binary / artifact size
ls -lh go-server/go-server rust-server/target/release/rust-server
du -sh ts-server/node_modules

# memory while under load
ps -o pid,rss,comm -C go-server,node,rust-server
```

## Expected numbers

Roughly: Rust >= Go > Node on throughput, Go and Rust close to each other.
The interesting part is not that Go is close to Rust — it is how much simpler
the Go source is, and that the team already knows Go.
