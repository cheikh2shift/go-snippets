#!/usr/bin/env bash
#
# Container Image Builder — End-to-End Experiment
# Requires: Go, Docker Desktop (running)
#

set -euo pipefail

IMAGE_NAME="go-hello-world"
TAG="v1"
SERVER_DIR="./cmd/server"
BINARY_NAME="server"
PORT=8080

die() { echo "FATAL: $*" >&2; exit 1; }
step() { echo ""; echo "====================================================="; echo "  $*"; echo "====================================================="; }

# ─── Pre-flight checks ─────────────────────────────────────
step "Pre-flight checks"
command -v go &>/dev/null || die "Go is not installed."
command -v docker &>/dev/null || die "Docker is not installed."
docker version --format '{{.Server.Version}}' &>/dev/null || die "Docker daemon is not running."
echo "  Go: $(go version | awk '{print $3}')"
echo "  Docker: $(docker version --format '{{.Server.Version}}')"

# ─── Step 1: Create sample files ────────────────────────────
step "Step 1: Create sample files"
rm -rf ./layer-content ./docker-import ./docker-import.tar ./oci-image

mkdir -p ./layer-content/app
echo "Hello from layer 1 — the application binary" > ./layer-content/hello.txt
echo '{"config": "data", "version": "1.0.0"}' > ./layer-content/config.json
echo "  Created sample files in ./layer-content/"

# ─── Step 2: Cross-compile Go binary for Linux ──────────────
step "Step 2: Cross-compile server binary (GOOS=linux GOARCH=amd64)"
GOOS=linux GOARCH=amd64 go build -o "./layer-content/app/server" "./$SERVER_DIR/main.go"
echo "  ✅ Binary compiled: ./layer-content/app/server"

# ─── Step 3: Build OCI image layout ─────────────────────────
step "Step 3: Build OCI image layout"
go run . --source ./layer-content --output ./oci-image --verbose
echo "  ✅ OCI image built in ./oci-image/"

# ─── Step 4: Verify OCI layout ──────────────────────────────
step "Step 4: Verify OCI layout"
echo "  Directory structure:"
find ./oci-image -type f | sort
echo ""
echo "  Manifest:"
cat ./oci-image/index.json

# ─── Step 5: Import into Docker ─────────────────────────────
step "Step 5: Import into Docker and run"
mkdir -p ./docker-import/app
cp ./layer-content/app/server ./docker-import/app/server
cp ./layer-content/hello.txt ./docker-import/
cp ./layer-content/config.json ./docker-import/
go run . --source ./docker-import --tar ./docker-import.tar

docker import ./docker-import.tar "$IMAGE_NAME:$TAG"
echo "  ✅ Image imported: $IMAGE_NAME:$TAG"
docker images "$IMAGE_NAME" --format "  {{.Repository}}:{{.Tag}}  {{.Size}}"

docker rm -f "$IMAGE_NAME-test" 2>/dev/null || true
MSYS_NO_PATHCONV=1 docker run -d --name "$IMAGE_NAME-test" -p "$PORT:$PORT" "$IMAGE_NAME:$TAG" /app/server
sleep 3

echo ""
echo "  Testing / endpoint:"
curl -s http://localhost:$PORT/
echo ""
echo "  Testing /health endpoint:"
curl -s http://localhost:$PORT/health
echo ""
echo "  Container logs:"
docker logs "$IMAGE_NAME-test" 2>&1 | tail -3

# ─── Step 6: Round-trip test ────────────────────────────────
step "Step 6: Save → Reload → Re-run (round-trip test)"
SAVE_TAR="/tmp/${IMAGE_NAME}-${TAG}.tar"
docker save "$IMAGE_NAME:$TAG" -o "$SAVE_TAR"
echo "  ✅ Saved: $SAVE_TAR ($(wc -c < "$SAVE_TAR") bytes)"

docker rm -f "$IMAGE_NAME-test" 2>/dev/null || true
docker rmi "$IMAGE_NAME:$TAG"
echo "  ✅ Original image removed"

docker load -i "$SAVE_TAR"
echo "  ✅ Image reloaded"

docker rm -f "$IMAGE_NAME-test" 2>/dev/null || true
MSYS_NO_PATHCONV=1 docker run -d --name "$IMAGE_NAME-test" -p "$PORT:$PORT" "$IMAGE_NAME:$TAG" /app/server
sleep 3

echo ""
echo "  Re-testing / endpoint:"
curl -s http://localhost:$PORT/
echo ""
echo "  ✅ Round-trip passed"

# ─── Step 7: Cleanup ────────────────────────────────────────
step "Step 7: Cleanup"
docker stop "$IMAGE_NAME-test" 2>/dev/null || true
docker rm "$IMAGE_NAME-test" 2>/dev/null || true
rm -rf ./layer-content ./docker-import ./docker-import.tar
echo "  ✅ Done"

echo ""
echo "====================================================="
echo "  ✅ Experiment complete!"
echo "====================================================="
echo ""
echo "  - Cross-compiled Go binary (linux/amd64)"
echo "  - Built OCI image layout (pure Go)"
echo "  - Imported into Docker and ran successfully"
echo "  - Saved, reloaded, re-ran — binary survived round-trip"
echo ""
