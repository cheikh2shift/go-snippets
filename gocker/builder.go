// builder.go contains helper functions for OCI image construction.
// The core Builder type is defined in main.go.
// This file provides additional utilities for layer management.

package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CreateLayerTarball creates a tarball from a directory,
// suitable for use as an OCI image layer.
func CreateLayerTarball(sourceDir, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create tarball: %w", err)
	}
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return fmt.Errorf("tar header for %s: %w", path, err)
		}

		// Make paths relative to sourceDir
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("write header for %s: %w", path, err)
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer file.Close()

		if _, err := io.Copy(tw, file); err != nil {
			return fmt.Errorf("copy %s to tar: %w", path, err)
		}

		return nil
	})
}

// VerifyLayerDigest computes the SHA-256 digest of a tarball
// and compares it to the expected digest.
func VerifyLayerDigest(tarballPath, expectedDigest string) (bool, error) {
	data, err := os.ReadFile(tarballPath)
	if err != nil {
		return false, fmt.Errorf("read tarball: %w", err)
	}

	h := sha256.Sum256(data)
	computed := "sha256:" + hex.EncodeToString(h[:])

	return computed == expectedDigest, nil
}

// ExtractLayer extracts an OCI layer tarball to a destination directory.
func ExtractLayer(tarballPath, destDir string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return fmt.Errorf("open tarball: %w", err)
	}
	defer f.Close()

	tr := tar.NewReader(f)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		target := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("create dir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent dirs: %w", err)
			}
			dst, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.Copy(dst, tr); err != nil {
				dst.Close()
				return fmt.Errorf("extract file %s: %w", target, err)
			}
			dst.Close()
		}
	}

	return nil
}

// LayerInfo returns a summary of a layer tarball.
type LayerInfo struct {
	Digest string
	Size   int64
	Files  []string
}

// InspectLayer reads a tarball and returns its contents and digest.
func InspectLayer(tarballPath string) (*LayerInfo, error) {
	f, err := os.Open(tarballPath)
	if err != nil {
		return nil, fmt.Errorf("open tarball: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat tarball: %w", err)
	}

	h := sha256.New()
	tee := io.TeeReader(f, h)

	var files []string
	tr := tar.NewReader(tee)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}
		files = append(files, header.Name)
	}

	// Read the rest to compute full digest
	remaining, _ := io.Copy(h, tr)
	_ = remaining

	digest := "sha256:" + hex.EncodeToString(h.Sum(nil))

	return &LayerInfo{
		Digest: digest,
		Size:   stat.Size(),
		Files:  files,
	}, nil
}

// MergeLayers combines multiple tarballs into a single layer.
// The later tarballs take precedence (overwrite files from earlier ones).
func MergeLayers(outputPath string, tarballs ...string) error {
	// Collect all files from all tarballs
	fileMap := make(map[string][]byte)
	fileInfoMap := make(map[string]os.FileInfo)

	for _, tb := range tarballs {
		f, err := os.Open(tb)
		if err != nil {
			return fmt.Errorf("open %s: %w", tb, err)
		}

		tr := tar.NewReader(f)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				f.Close()
				return fmt.Errorf("read tar entry: %w", err)
			}

			if header.Typeflag == tar.TypeReg {
				data, err := io.ReadAll(tr)
				if err != nil {
					f.Close()
					return fmt.Errorf("read file %s: %w", header.Name, err)
				}
				fileMap[header.Name] = data
				fileInfoMap[header.Name] = header.FileInfo()
			}
		}
		f.Close()
	}

	// Write merged tarball
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer out.Close()

	tw := tar.NewWriter(out)
	defer tw.Close()

	for name, data := range fileMap {
		info := fileInfoMap[name]
		header, err := tar.FileInfoHeader(info, name)
		if err != nil {
			return fmt.Errorf("tar header for %s: %w", name, err)
		}
		header.Name = name

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("write header for %s: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return fmt.Errorf("write data for %s: %w", name, err)
		}
	}

	return nil
}

// LayerStats returns statistics about a layer tarball.
func LayerStats(tarballPath string) (map[string]int64, error) {
	f, err := os.Open(tarballPath)
	if err != nil {
		return nil, fmt.Errorf("open tarball: %w", err)
	}
	defer f.Close()

	stats := make(map[string]int64)
	tr := tar.NewReader(f)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}

		if header.Typeflag == tar.TypeReg {
			stats[header.Name] = header.Size
		}
	}

	return stats, nil
}

// DiffLayers compares two layer tarballs and returns the files that differ.
func DiffLayers(tarball1, tarball2 string) ([]string, error) {
	files1, err := LayerStats(tarball1)
	if err != nil {
		return nil, fmt.Errorf("inspect layer 1: %w", err)
	}

	files2, err := LayerStats(tarball2)
	if err != nil {
		return nil, fmt.Errorf("inspect layer 2: %w", err)
	}

	var diffs []string
	for name, size1 := range files1 {
		if size2, ok := files2[name]; !ok || size2 != size1 {
			diffs = append(diffs, name)
		}
	}

	for name := range files2 {
		if _, ok := files1[name]; !ok {
			diffs = append(diffs, name)
		}
	}

	return diffs, nil
}

// LayerToReader returns a reader for a layer tarball with a progress callback.
func LayerToReader(tarballPath string, progress func(bytesRead int64)) (io.Reader, error) {
	f, err := os.Open(tarballPath)
	if err != nil {
		return nil, fmt.Errorf("open tarball: %w", err)
	}

	pr := &progressReader{reader: f, progress: progress}
	return pr, nil
}

type progressReader struct {
	reader   *os.File
	progress func(bytesRead int64)
	bytesRead int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.bytesRead += int64(n)
	if pr.progress != nil {
		pr.progress(pr.bytesRead)
	}
	return n, err
}

// Close closes the underlying file.
func (pr *progressReader) Close() error {
	return pr.reader.Close()
}

// ComputeLayerDiff computes the binary diff between two layers
// and returns the bytes that differ.
func ComputeLayerDiff(tarball1, tarball2 string) (*bytes.Buffer, error) {
	data1, err := os.ReadFile(tarball1)
	if err != nil {
		return nil, fmt.Errorf("read layer 1: %w", err)
	}

	data2, err := os.ReadFile(tarball2)
	if err != nil {
		return nil, fmt.Errorf("read layer 2: %w", err)
	}

	var buf bytes.Buffer
	// Simple byte-level diff
	maxLen := len(data1)
	if len(data2) > maxLen {
		maxLen = len(data2)
	}

	for i := 0; i < maxLen; i++ {
		b1 := byte(0)
		b2 := byte(0)
		if i < len(data1) {
			b1 = data1[i]
		}
		if i < len(data2) {
			b2 = data2[i]
		}
		if b1 != b2 {
			buf.WriteByte(b1)
			buf.WriteByte(b2)
		}
	}

	return &buf, nil
}
