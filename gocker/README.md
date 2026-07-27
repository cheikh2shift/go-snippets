# Container Image Builder — OCI Image Builder in Go

A minimal OCI-compliant container image builder built entirely with Go's standard library. Zero external dependencies.

## What It Does

1. **Builds a Go binary on the host** (cross-compiled to linux/amd64)
2. **Packages that binary into an OCI image layout** (content-addressed blobs, manifest, index)
3. **Loads the image into Docker** and runs it to prove compatibility
4. **Saves and reloads the image** to verify the binary survives the round-trip

## The Experiment

The experiment (`experiment.sh`) demonstrates that a Go binary built on the host system runs correctly inside a Docker container, proving cross-platform compatibility and OCI image integrity.

### Experiment Flow

```
┌─────────────────────────────────────────────────────────┐
│  1. Cross-compile Go binary for linux/amd64            │
│     GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build     │
├─────────────────────────────────────────────────────────┤
│  2. Create OCI layer tarball containing the binary     │
│     tar cf layer.tar -C <dir> hello-server             │
├─────────────────────────────────────────────────────────┤
│  3. Build OCI image layout (blobs, manifest, index)    │
│     Compute SHA-256 digests, store content-addressed   │
├─────────────────────────────────────────────────────────┤
│  4. Load OCI image into Docker                         │
│     docker import / skopeo copy / buildah load         │
├─────────────────────────────────────────────────────────┤
│  5. Run the container and verify the response          │
│     curl http://localhost:8080/                        │
├─────────────────────────────────────────────────────────┤
│  6. Save, reload, and re-run to verify round-trip      │
│     docker save → docker load → docker run             │
└─────────────────────────────────────────────────────────┘
```

### Running the Experiment

```bash
# 1. Build the Go binary and create the OCI image
chmod +x experiment.sh
./experiment.sh
```

## Project Structure

```
gocker/
├── experiment.sh         # End-to-end experiment script
├── go.mod                # Module definition
├── main.go               # CLI entry point (build/push/pull)
├── builder.go            # OCI image builder
├── registry.go           # Registry push/pull client
├── cmd/server/
│   └── main.go           # Simple HTTP server (the thing we containerize)
└── README.md             # This file
```

## Key Design Decisions

### Why Cross-Compile?

The experiment demonstrates that a binary built on **any host** (macOS, Windows, Linux) can be packaged into a container and run on Linux. This is the core value proposition of Go's cross-compilation: write once, run anywhere — even inside containers.

### Why OCI Layout?

OCI Image Layout is a simple directory format that can be:
- Inspected with `cat` and `jq`
- Loaded into Docker with `docker load` or `skopeo`
- Pushed to a registry with `skopeo copy`
- Used as a build artifact in CI pipelines

### Why Zero Dependencies?

Using only the standard library means:
- No `go mod download` needed
- No supply chain risk from third-party packages
- The binary compiles fast and is statically linked
- Easy to audit and understand every line of code

## Security

- **Content-addressed storage** — every blob is keyed by its SHA-256 digest, making tampering detectable
- **Static linking** — no dynamic dependencies, reducing attack surface
- **No root required** — the binary runs as a non-root user in the container

## What's Next

- Add multi-architecture manifest lists
- Implement layer compression with `compress/flate`
- Add image signing with Cosign
- Support multi-stage builds
- Add a registry server (push/pull to a local OCI registry)

## License

MIT
