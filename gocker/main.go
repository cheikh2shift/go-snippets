package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Layer represents an OCI image layer.
type Layer struct {
	Digest   string `json:"digest"`
	Size     int64  `json:"size"`
	DiffID   string `json:"diffId"`
	Path     string `json:"-"`
	BlobPath string `json:"-"`
}

// Config is the OCI image configuration blob.
type Config struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	RootFS       RootFS `json:"rootfs"`
}

// RootFS describes the root filesystem layers.
type RootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diffIds"`
}

// Manifest is the OCI image manifest.
type Manifest struct {
	SchemaVersion int     `json:"schemaVersion"`
	MediaType     string  `json:"mediaType"`
	Config        Config  `json:"config"`
	Layers        []Layer `json:"layers"`
	Digest        string  `json:"-"`
}

// Builder constructs OCI images from layer tarballs.
type Builder struct {
	outputDir string
	verbose   bool
	layers    []Layer
	config    Config
}

// NewBuilder creates a new Builder.
func NewBuilder(outputDir string, verbose bool) *Builder {
	return &Builder{
		outputDir: outputDir,
		verbose:   verbose,
		config: Config{
			Architecture: "amd64",
			OS:           "linux",
			RootFS: RootFS{
				Type:    "layers",
				DiffIDs: []string{},
			},
		},
	}
}

// AddLayerFromFile reads a tarball, computes its digest, and stores it in the OCI blob store.
func (b *Builder) AddLayerFromFile(path string, dryRun bool) (Layer, error) {
	f, err := os.Open(path)
	if err != nil {
		return Layer{}, fmt.Errorf("open layer: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return Layer{}, fmt.Errorf("stat layer: %w", err)
	}

	// Compute SHA-256 digest while reading
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Layer{}, fmt.Errorf("hash layer: %w", err)
	}

	digest := "sha256:" + hex.EncodeToString(h.Sum(nil))
	hexDigest := hex.EncodeToString(h.Sum(nil))

	// Reset file pointer for copying
	f.Seek(0, io.SeekStart)

	layer := Layer{
		Digest:   digest,
		Size:     stat.Size(),
		DiffID:   digest,
		Path:     path,
		BlobPath: filepath.Join(b.outputDir, "blobs", "sha256", hexDigest),
	}

	if !dryRun {
		b.storeBlob(layer)
		b.config.RootFS.DiffIDs = append(b.config.RootFS.DiffIDs, layer.DiffID)
	}

	return layer, nil
}

// storeBlob copies the layer file into the OCI blob store.
func (b *Builder) storeBlob(layer Layer) {
	os.MkdirAll(filepath.Dir(layer.BlobPath), 0o755)
	data, err := os.ReadFile(layer.Path)
	if err != nil {
		log.Fatalf("read layer %s: %v", layer.Path, err)
	}
	if err := os.WriteFile(layer.BlobPath, data, 0o644); err != nil {
		log.Fatalf("write blob %s: %v", layer.BlobPath, err)
	}
	if b.verbose {
		fmt.Printf("  [blob] %s → %s (%d bytes)\n", layer.Path, layer.BlobPath, layer.Size)
	}
}

// Build generates the manifest and writes the OCI layout files.
func (b *Builder) Build(dryRun bool) (*Manifest, error) {
	manifest := Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config:        b.config,
		Layers:        b.layers,
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	// Compute manifest digest
	h := sha256.Sum256(manifestBytes)
	manifest.Digest = "sha256:" + hex.EncodeToString(h[:])

	// Write index.json
	indexJSON, _ := json.MarshalIndent([]map[string]interface{}{
		{
			"mediaType": manifest.MediaType,
			"digest":    manifest.Digest,
			"size":      len(manifestBytes),
		},
	}, "", "  ")
	os.WriteFile(filepath.Join(b.outputDir, "index.json"), indexJSON, 0o644)

	// Write oci-layout descriptor
	layoutJSON, _ := json.MarshalIndent(map[string]string{
		"imageLayoutVersion": "1.0.0",
	}, "", "  ")
	os.WriteFile(filepath.Join(b.outputDir, "oci-layout"), layoutJSON, 0o644)

	// Write manifest blob
	manifestHex := hex.EncodeToString(h[:])
	os.WriteFile(filepath.Join(b.outputDir, "blobs", "sha256", manifestHex), manifestBytes, 0o644)

	// Write config blob
	configBytes, _ := json.MarshalIndent(manifest.Config, "", "  ")
	configH := sha256.Sum256(configBytes)
	configHex := hex.EncodeToString(configH[:])
	os.MkdirAll(filepath.Join(b.outputDir, "blobs", "sha256"), 0o755)
	os.WriteFile(filepath.Join(b.outputDir, "blobs", "sha256", configHex), configBytes, 0o644)

	if b.verbose {
		fmt.Printf("  [manifest] %s (%d bytes)\n", manifest.Digest, len(manifestBytes))
		fmt.Printf("  [layers]   %d\n", len(b.layers))
	}

	return &manifest, nil
}

func main() {
	var (
		layerDir  = flag.String("layer", "", "Directory with layer tarballs")
		output    = flag.String("output", "oci-image", "Output OCI image directory")
		registry  = flag.String("registry", "", "Registry URL for push")
		repo      = flag.String("repo", "", "Repository name")
		tag       = flag.String("tag", "latest", "Image tag")
		pull      = flag.Bool("pull", false, "Pull image from registry")
		dryRun    = flag.Bool("dry-run", false, "Preview without writing files")
		verbose   = flag.Bool("verbose", false, "Enable verbose output")
	)
	flag.Parse()

	if *pull {
		fmt.Println("Pull not yet implemented in this version")
		return
	}

	if *layerDir == "" {
		fmt.Println("Usage: go run . --layer <dir> --output <dir> [--registry <url> --repo <name> --tag <tag>]")
		flag.PrintDefaults()
		return
	}

	builder := NewBuilder(*output, *verbose)

	// Process each tarball in the layer directory
	entries, err := os.ReadDir(*layerDir)
	if err != nil {
		log.Fatalf("read layer dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(*layerDir, entry.Name())
		layer, err := builder.AddLayerFromFile(path, *dryRun)
		if err != nil {
			log.Fatalf("add layer %s: %v", entry.Name(), err)
		}
		fmt.Printf("  [layer] %s → %s (%d bytes)\n", entry.Name(), layer.Digest, layer.Size)
		builder.layers = append(builder.layers, layer)
	}

	manifest, err := builder.Build(*dryRun)
	if err != nil {
		log.Fatalf("build manifest: %v", err)
	}

	fmt.Printf("✅ Image built: %d layers, manifest %s\n", len(builder.layers), manifest.Digest)

	if *registry != "" && *repo != "" {
		if err := PushImage(*registry, *repo, *tag, *output, *dryRun); err != nil {
			log.Fatalf("push image: %v", err)
		}
		fmt.Printf("✅ Image pushed: %s:%s\n", *repo, *tag)
	}
}

// PushImage uploads an OCI image layout to a registry.
func PushImage(registry, repo, tag, layoutDir string, dryRun bool) error {
	// Check registry connectivity
	registryURL := strings.TrimSuffix(registry, "/")
	resp, err := http.Head(registryURL + "/v2/")
	if err != nil {
		return fmt.Errorf("check registry: %w", err)
	}
	resp.Body.Close()

	// Read manifest
	manifestBytes, err := os.ReadFile(filepath.Join(layoutDir, "index.json"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// Upload blobs
	blobsDir := filepath.Join(layoutDir, "blobs", "sha256")
	entries, err := os.ReadDir(blobsDir)
	if err != nil {
		return fmt.Errorf("read blobs dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		blobPath := filepath.Join(blobsDir, entry.Name())
		if err := pushBlob(registryURL, repo, blobPath, dryRun); err != nil {
			return err
		}
	}

	// Upload manifest
	return pushManifest(registryURL, repo, tag, manifestBytes, dryRun)
}

func pushBlob(registry, repo, blobPath string, dryRun bool) error {
	if dryRun {
		return nil
	}

	data, err := os.ReadFile(blobPath)
	if err != nil {
		return fmt.Errorf("read blob: %w", err)
	}

	h := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(h[:])

	// Initiate upload
	uploadURL := fmt.Sprintf("%s/v2/%s/blobs/uploads/", registry, repo)
	req, err := http.NewRequest("POST", uploadURL, nil)
	if err != nil {
		return fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Docker-Content-Digest", digest)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("initiate blob upload: %w", err)
	}
	resp.Body.Close()

	// For simplicity, use the digest-based upload path
	// (some registries support PUT with digest directly)
	putURL := fmt.Sprintf("%s/v2/%s/blobs/%s", registry, repo, digest)
	req2, err := http.NewRequest("PUT", putURL, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("create put request: %w", err)
	}
	req2.Header.Set("Content-Type", "application/octet-stream")

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return fmt.Errorf("upload blob: %w", err)
	}
	resp2.Body.Close()

	return nil
}

func pushManifest(registry, repo, tag string, manifestBytes []byte, dryRun bool) error {
	if dryRun {
		return nil
	}

	h := sha256.Sum256(manifestBytes)
	digest := "sha256:" + hex.EncodeToString(h[:])
	putURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registry, repo, tag)

	req, err := http.NewRequest("PUT", putURL, strings.NewReader(string(manifestBytes)))
	if err != nil {
		return fmt.Errorf("create manifest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("Docker-Content-Digest", digest)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}
	resp.Body.Close()

	return nil
}
