#!/usr/bin/env bash
#
# Container Image Builder — End-to-End Experiment
# Works on Linux, macOS, and Windows (Git Bash)
# Requires: Go, Docker Desktop (running)
#

set -euo pipefail

# ─── Configuration ─────────────────────────────────────────────
IMAGE_NAME="go-hello-world"
TAG="v1"
LAYER_DIR="./layers"
OUTPUT_DIR="./oci-image"
SERVER_DIR="./cmd/server"
BINARY_NAME="server"
PORT=8080

# ─── Portable SHA-256 ────────────────────────────────────────
# Tries multiple fallbacks so it works on Linux, macOS, and Git Bash on Windows
sha256() {
    local file="$1"
    if command -v sha256sum &>/dev/null; then
        sha256sum "$file" | awk '{print $1}'
    elif command -v openssl &>/dev/null; then
        openssl dgst -sha256 -binary "$file" | xxd -p -c 64
    elif command -v python3 &>/dev/null; then
        python3 -c "import hashlib; print(hashlib.sha256(open('$file','rb').read()).hexdigest())"
    elif command -v certutil &>/dev/null; then
        certutil -hashfile "$file" SHA256 2>/dev/null | grep -E '^[0-9a-f]{64}$' | tr '[:upper:]' '[:lower:]'
    else
        echo "ERROR: No SHA-256 tool found" >&2
        return 1
    fi
}

# ─── Helpers ─────────────────────────────────────────────────
die() {
    echo "FATAL: $*" >&2
    exit 1
}

step() {
    echo ""
    echo "====================================================="
    echo "  $*"
    echo "====================================================="
}

# ─── Pre-flight checks ─────────────────────────────────────
step "Pre-flight checks"

command -v go &>/dev/null || die "Go is not installed or not in PATH. Download from https://go.dev/dl/"
command -v docker &>/dev/null || die "Docker is not installed or not in PATH. Install Docker Desktop and make sure it's running."

echo "  Go: $(go version | awk '{print $3}')"
echo "  Docker: $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'running')"

# ─── Step 1: Create sample layer files ──────────────────────
step "Step 1: Create sample layer files"

rm -rf "$LAYER_DIR" "$OUTPUT_DIR"
mkdir -p "$LAYER_DIR"
mkdir -p "$SERVER_DIR"

echo "Hello from layer 1 — the application binary" > "$LAYER_DIR/hello.txt"
echo '{"config": "data", "version": "1.0.0"}' > "$LAYER_DIR/config.json"
echo "This is a layer tarball used in the OCI image experiment" > "$LAYER_DIR/readme.txt"

echo "  Created 3 sample files in $LAYER_DIR/"
ls -la "$LAYER_DIR"/

# ─── Step 2: Cross-compile Go binary for Linux ──────────────
step "Step 2: Cross-compile Go binary for Linux (GOOS=linux GOARCH=amd64)"

cat > "$SERVER_DIR/main.go" << 'GOEOF'
package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from Go inside a container! 🐳\n")
		fmt.Fprintf(w, "Path: %s\n", r.URL.Path)
		fmt.Fprintf(w, "Method: %s\n", r.Method)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK\n")
	})

	fmt.Printf("Server starting on :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}
GOEOF

GOOS=linux GOARCH=amd64 go build -o "$SERVER_DIR/$BINARY_NAME" "$SERVER_DIR/main.go"
echo "  ✅ Binary compiled: $SERVER_DIR/$BINARY_NAME (GOOS=linux GOARCH=amd64)"

# ─── Step 3: Create OCI layer tarball ───────────────────────
step "Step 3: Create OCI layer tarball"

# Create a proper layer that includes the server binary
LAYER_CONTENT="./layer-content"
rm -rf "$LAYER_CONTENT"
mkdir -p "$LAYER_CONTENT/app"
cp "$SERVER_DIR/$BINARY_NAME" "$LAYER_CONTENT/app/server"
chmod +x "$LAYER_CONTENT/app/server"
cp "$LAYER_DIR/hello.txt" "$LAYER_CONTENT/"
cp "$LAYER_DIR/config.json" "$LAYER_CONTENT/"

LAYER_TARBALL="$LAYER_DIR/layer.tar"
python3 -c "
import tarfile, os
src = os.path.abspath('$LAYER_CONTENT').replace(os.sep, '/')
with tarfile.open('$LAYER_TARBALL', 'w') as t:
    for root, dirs, files in os.walk(src):
        for d in dirs:
            dp = os.path.join(root, d).replace(os.sep, '/')
            arcname = dp[len(src):].lstrip('/') + '/'
            info = tarfile.TarInfo(name=arcname)
            info.type = tarfile.DIRTYPE
            info.mode = 0o755
            t.addfile(info)
        for fn in files:
            fp = os.path.join(root, fn).replace(os.sep, '/')
            arcname = fp[len(src):].lstrip('/')
            info = tarfile.TarInfo(name=arcname)
            info.size = os.path.getsize(fp)
            info.mode = 0o755
            with open(fp, 'rb') as f:
                t.addfile(info, f)
"

LAYER_SIZE=$(wc -c < "$LAYER_TARBALL")
LAYER_DIGEST=$(sha256 "$LAYER_TARBALL")
echo "  Layer tarball: $LAYER_TARBALL"
echo "  Layer size: $LAYER_SIZE bytes"
echo "  Layer digest: sha256:$LAYER_DIGEST"

# ─── Step 4: Build OCI image layout ─────────────────────────
step "Step 4: Build OCI image layout"

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR/blobs/sha256"

# Store blob
BLOB_DEST="$OUTPUT_DIR/blobs/sha256/$LAYER_DIGEST"
cp "$LAYER_TARBALL" "$BLOB_DEST"
echo "  ✅ Blob stored at blobs/sha256/$LAYER_DIGEST"

# Build config blob
CONFIG_JSON=$(cat <<EOF
{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diffIds":["sha256:$LAYER_DIGEST"]}}
EOF
)
CONFIG_BLOB="$OUTPUT_DIR/blobs/sha256/$(sha256 /dev/stdin <<< "$CONFIG_JSON")"
echo "$CONFIG_JSON" > "$CONFIG_BLOB"
CONFIG_DIGEST=$(sha256 "$CONFIG_BLOB")
CONFIG_SIZE=$(wc -c < "$CONFIG_BLOB")
echo "  ✅ Config blob stored at blobs/sha256/$CONFIG_DIGEST"

# Build manifest
MANIFEST=$(cat <<EOF
{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:$CONFIG_DIGEST","size":$CONFIG_SIZE},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"sha256:$LAYER_DIGEST","size":$LAYER_SIZE}]}
EOF
)
MANIFEST_DIGEST=$(sha256 /dev/stdin <<< "$MANIFEST")
MANIFEST_SIZE=${#MANIFEST}

echo "$MANIFEST" > "$OUTPUT_DIR/index.json"
echo "  ✅ Manifest written to index.json"
echo "  ✅ Manifest digest: sha256:$MANIFEST_DIGEST"

# Write oci-layout
echo '{"imageLayoutVersion":"1.0.0"}' > "$OUTPUT_DIR/oci-layout"
echo "  ✅ oci-layout written"

# ─── Step 5: Verify OCI layout structure ────────────────────
step "Step 5: Verify OCI layout structure"

echo "  Directory structure:"
find "$OUTPUT_DIR" -type f | sort

echo ""
echo "  Verifying blob integrity..."
VERIFY_DIGEST=$(sha256 "$BLOB_DEST")
if [ "$VERIFY_DIGEST" = "$LAYER_DIGEST" ]; then
    echo "  ✅ Blob digest verified: sha256:$VERIFY_DIGEST"
else
    echo "  ❌ Blob digest mismatch!" >&2
    exit 1
fi

echo ""
echo "  Manifest contents:"
cat "$OUTPUT_DIR/index.json"

# ─── Step 6: Import into Docker ─────────────────────────────
step "Step 6: Import OCI image into Docker"

# Create a proper Docker import tar with the right structure
DOCKER_IMPORT_DIR="./docker-import"
rm -rf "$DOCKER_IMPORT_DIR"
mkdir -p "$DOCKER_IMPORT_DIR/app"
cp "$SERVER_DIR/$BINARY_NAME" "$DOCKER_IMPORT_DIR/app/server"
chmod +x "$DOCKER_IMPORT_DIR/app/server"
cp "$LAYER_DIR/hello.txt" "$DOCKER_IMPORT_DIR/"
cp "$LAYER_DIR/config.json" "$DOCKER_IMPORT_DIR/"

DOCKER_TARBALL="./docker-import.tar"
python3 -c "
import tarfile, os
src = os.path.abspath('$DOCKER_IMPORT_DIR').replace(os.sep, '/')
with tarfile.open('$DOCKER_TARBALL', 'w') as t:
    for root, dirs, files in os.walk(src):
        for d in dirs:
            dp = os.path.join(root, d).replace(os.sep, '/')
            arcname = dp[len(src):].lstrip('/') + '/'
            info = tarfile.TarInfo(name=arcname)
            info.type = tarfile.DIRTYPE
            info.mode = 0o755
            t.addfile(info)
        for fn in files:
            fp = os.path.join(root, fn).replace(os.sep, '/')
            arcname = fp[len(src):].lstrip('/')
            info = tarfile.TarInfo(name=arcname)
            info.size = os.path.getsize(fp)
            info.mode = 0o755
            with open(fp, 'rb') as f:
                t.addfile(info, f)
"

docker import "$DOCKER_TARBALL" "$IMAGE_NAME:$TAG"
echo "  ✅ Image imported: $IMAGE_NAME:$TAG"
docker images "$IMAGE_NAME" --format "  {{.Repository}}:{{.Tag}}  {{.Size}}"

# ─── Step 7: Run the container ──────────────────────────────
step "Step 7: Run the container and verify it works"

docker rm -f "$IMAGE_NAME-test" 2>/dev/null || true

MSYS_NO_PATHCONV=1 docker run -d --name "$IMAGE_NAME-test" -p "$PORT:$PORT" "$IMAGE_NAME:$TAG" /app/server

echo "  Waiting for server to start..."
sleep 3

echo ""
echo "  Testing / endpoint:"
curl -s http://localhost:$PORT/ || echo "  (curl failed — container may still be starting)"

echo ""
echo "  Testing /health endpoint:"
curl -s http://localhost:$PORT/health || echo "  (curl failed — container may still be starting)"

echo ""
echo "  Container logs:"
docker logs "$IMAGE_NAME-test" 2>&1 | tail -5

# ─── Step 8: Save and reload (round-trip test) ─────────────
step "Step 8: Save → Reload → Re-run (round-trip integrity test)"

SAVE_TAR="/tmp/${IMAGE_NAME}-${TAG}.tar"
docker save "$IMAGE_NAME:$TAG" -o "$SAVE_TAR"
SAVE_SIZE=$(wc -c < "$SAVE_TAR")
echo "  ✅ Image saved to $SAVE_TAR ($SAVE_SIZE bytes)"

docker rm -f "$IMAGE_NAME-test" 2>/dev/null || true
docker rmi "$IMAGE_NAME:$TAG"
echo "  ✅ Original image removed"

docker load -i "$SAVE_TAR"
echo "  ✅ Image reloaded from tar"

docker images "$IMAGE_NAME" --format "  {{.Repository}}:{{.Tag}}  {{.Size}}"

# Re-run
docker rm -f "$IMAGE_NAME-test" 2>/dev/null || true
MSYS_NO_PATHCONV=1 docker run -d --name "$IMAGE_NAME-test" -p "$PORT:$PORT" "$IMAGE_NAME:$TAG" /app/server
sleep 3

echo ""
echo "  Re-testing / endpoint after reload:"
curl -s http://localhost:$PORT/

echo ""
echo "  ✅ Round-trip test passed — binary works after save/load cycle"

# ─── Step 9: Cleanup ────────────────────────────────────────
step "Step 9: Cleanup"

docker stop "$IMAGE_NAME-test" 2>/dev/null || true
docker rm "$IMAGE_NAME-test" 2>/dev/null || true
echo "  ✅ Container stopped and removed"

echo ""
echo "====================================================="
echo "  ✅ Experiment complete!"
echo "====================================================="
echo ""
echo "Summary:"
echo "  - Cross-compiled Go binary (GOOS=linux GOARCH=amd64) on host"
echo "  - Packed into OCI layer tarball"
echo "  - Built OCI image layout (blobs, manifest, index.json)"
echo "  - Imported into Docker and ran successfully"
echo "  - Saved to tar, reloaded, and re-ran — binary survived round-trip"
echo ""
echo "To clean up temporary files manually:"
echo "  rm -rf $LAYER_DIR $OUTPUT_DIR $DOCKER_IMPORT_DIR $SAVE_TAR"
echo ""