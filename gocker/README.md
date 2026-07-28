# Container Image Builder — OCI Image Builder in Go

A minimal OCI-compliant container image builder built entirely with Go's standard library. Zero external dependencies.

## What It Does

1. **Cross-compiles a Go binary** for linux/amd64 on any host
2. **Creates layer tarballs** with correct Unix permissions
3. **Builds an OCI image layout** — content-addressed blobs, manifest, index.json
4. **Loads the image into Docker** and runs it
5. **Saves and reloads** to verify the binary survives the round-trip

## The Experiment

`experiment.sh` cross-compiles a Go server binary, then calls the Go builder to create the OCI image and a Docker-importable tarball. All image construction — tar creation, SHA-256 hashing, manifest generation — is pure Go.

```
┌─────────────────────────────────────────────────────────┐
│  Shell: Cross-compile Go binary (GOOS=linux/amd64)      │
├─────────────────────────────────────────────────────────┤
│  Go: Create OCI image layout from source directory      │
│     archive/tar + crypto/sha256 + encoding/json         │
├─────────────────────────────────────────────────────────┤
│  Go: Create flat tarball for docker import              │
├─────────────────────────────────────────────────────────┤
│  Shell: docker import → docker run → curl verify        │
├─────────────────────────────────────────────────────────┤
│  Shell: docker save → docker load → docker run          │
└─────────────────────────────────────────────────────────┘
```

### Running

```bash
# Linux / macOS
chmod +x experiment.sh
./experiment.sh

# Windows (PowerShell)
& "C:\Program Files\Git\bin\bash.exe" experiment.sh
```

Requires: Go, Docker Desktop (running).

## Project Structure

```
gocker/
├── experiment.sh         # Orchestrator shell script
├── go.mod
├── main.go               # CLI entry point
├── builder.go            # OCI image builder + tar creation
├── cmd/server/
│   └── main.go           # HTTP server (cross-compiled for containers)
├── .gitignore
└── README.md
```

## CLI Usage

```bash
# Build OCI image from a directory
go run . --source ./myapp --output ./oci-image --verbose

# Create a flat tarball from a directory
go run . --source ./myapp --tar ./output.tar
```

## Key Design Decisions

### Why Pure Go?

No Python, no platform-specific tar flags. Tar creation, SHA-256 hashing, and OCI layout construction all use Go's standard library. Works identically on Linux, macOS, and Windows.

### Why Cross-Compile?

Demonstrates that a binary built on **any host** can be packaged into a container and run on Linux. Write once, run anywhere.

### Why OCI Layout?

OCI Image Layout is a simple directory format that can be inspected with `cat`/`jq`, loaded into Docker with `docker load`, or pushed to a registry with `skopeo copy`.

### Why Zero Dependencies?

No `go mod download`, no supply chain risk, fast compilation, statically linked binaries.

## License

MIT
