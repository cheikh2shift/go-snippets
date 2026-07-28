package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Layer struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	DiffID string `json:"diffIds"`
}

type Config struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	RootFS       struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diffIds"`
	} `json:"rootfs"`
}

type Manifest struct {
	SchemaVersion int     `json:"schemaVersion"`
	MediaType     string  `json:"mediaType"`
	Config        Config  `json:"config"`
	Layers        []Layer `json:"layers"`
}

type Builder struct {
	outputDir string
	verbose   bool
	layers    []Layer
	config    Config
}

func NewBuilder(outputDir string, verbose bool) *Builder {
	return &Builder{
		outputDir: outputDir,
		verbose:   verbose,
		config: Config{
			Architecture: "amd64",
			OS:           "linux",
		},
	}
}

func (b *Builder) SetDiffIDs(ids []string) {
	b.config.RootFS.Type = "layers"
	b.config.RootFS.DiffIDs = ids
}

// CreateTarball creates a tar archive from a directory with mode 0755 on all entries.
func CreateTarball(sourceDir, outputPath string) error {
	src, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create tarball: %w", err)
	}
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		header.Mode = 0755

		if info.IsDir() {
			header.Name += "/"
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		_, err = io.Copy(tw, in)
		return err
	})
}

// AddLayerFromDir tars a directory and adds it as an OCI layer.
func (b *Builder) AddLayerFromDir(sourceDir string) error {
	tmpTar, err := os.CreateTemp("", "layer-*.tar")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpTar.Name()
	tmpTar.Close()
	defer os.Remove(tmpPath)

	if err := CreateTarball(sourceDir, tmpPath); err != nil {
		return fmt.Errorf("create tarball: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read tarball: %w", err)
	}

	h := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(h[:])

	blobDir := filepath.Join(b.outputDir, "blobs", "sha256")
	os.MkdirAll(blobDir, 0755)
	blobPath := filepath.Join(blobDir, hex.EncodeToString(h[:]))
	if err := os.WriteFile(blobPath, data, 0644); err != nil {
		return fmt.Errorf("write blob: %w", err)
	}

	layer := Layer{
		Digest: digest,
		Size:   int64(len(data)),
		DiffID: digest,
	}
	b.layers = append(b.layers, layer)

	if b.verbose {
		fmt.Printf("  [layer] %s (%d bytes)\n", digest, layer.Size)
	}

	return nil
}

// Build writes the OCI image layout: manifest, config, index.json, oci-layout.
func (b *Builder) Build() error {
	for i := range b.layers {
		b.config.RootFS.DiffIDs = append(b.config.RootFS.DiffIDs, b.layers[i].DiffID)
	}

	manifest := Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config:        b.config,
		Layers:        b.layers,
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	manifestH := sha256.Sum256(manifestBytes)
	manifestDigest := "sha256:" + hex.EncodeToString(manifestH[:])

	os.MkdirAll(filepath.Join(b.outputDir, "blobs", "sha256"), 0755)

	// manifest blob
	manifestBlob := filepath.Join(b.outputDir, "blobs", "sha256", hex.EncodeToString(manifestH[:]))
	os.WriteFile(manifestBlob, manifestBytes, 0644)

	// config blob
	configBytes, _ := json.MarshalIndent(manifest.Config, "", "  ")
	configH := sha256.Sum256(configBytes)
	configBlob := filepath.Join(b.outputDir, "blobs", "sha256", hex.EncodeToString(configH[:]))
	os.WriteFile(configBlob, configBytes, 0644)

	// index.json
	indexJSON, _ := json.MarshalIndent([]map[string]interface{}{
		{
			"mediaType": manifest.MediaType,
			"digest":    manifestDigest,
			"size":      len(manifestBytes),
		},
	}, "", "  ")
	os.WriteFile(filepath.Join(b.outputDir, "index.json"), indexJSON, 0644)

	// oci-layout
	os.WriteFile(filepath.Join(b.outputDir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0644)

	if b.verbose {
		fmt.Printf("  [manifest] %s (%d bytes)\n", manifestDigest, len(manifestBytes))
		fmt.Printf("  [layers]   %d\n", len(b.layers))
	}

	return nil
}
