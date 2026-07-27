// registry.go provides push and pull operations for OCI images.
package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// RegistryClient handles communication with OCI registries.
type RegistryClient struct {
	baseURL   string
	repo      string
	username  string
	password  string
	insecure  bool
	httpClient *http.Client
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient(baseURL, repo, username, password string, insecure bool) *RegistryClient {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	return &RegistryClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		repo:       repo,
		username:   username,
		password:   password,
		insecure:   insecure,
		httpClient: &http.Client{Transport: transport},
	}
}

// CheckConnectivity verifies the registry is reachable and supports OCI.
func (rc *RegistryClient) CheckConnectivity() error {
	resp, err := rc.httpClient.Head(rc.baseURL + "/v2/")
	if err != nil {
		return fmt.Errorf("registry unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	return nil
}

// GetAuthToken retrieves a bearer token from the registry.
func (rc *RegistryClient) GetAuthToken() (string, error) {
	// Try to get a token from the registry's auth challenge
	resp, err := rc.httpClient.Head(rc.baseURL + "/v2/" + rc.repo + "/manifests/latest")
	if err != nil {
		return "", fmt.Errorf("check manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Parse WWW-Authenticate header
		authHeader := resp.Header.Get("WWW-Authenticate")
		if authHeader == "" {
			return "", fmt.Errorf("registry requires authentication but no challenge provided")
		}

		// Parse the challenge (simplified)
		// Format: Bearer realm="https://auth.example.com/token",service="registry.example.com",scope="repository:myrepo:pull"
		tokenURL := parseAuthChallenge(authHeader, rc.repo)
		if tokenURL == "" {
			return "", nil // No token URL found, proceed without auth
		}

		// Fetch the token
		tokenResp, err := rc.httpClient.Get(tokenURL)
		if err != nil {
			return "", fmt.Errorf("fetch token: %w", err)
		}
		defer tokenResp.Body.Close()

		var tokenRespData struct {
			Token string `json:"token"`
		}
		json.NewDecoder(tokenResp.Body).Decode(&tokenRespData)
		return tokenRespData.Token, nil
	}

	return "", nil
}

// parseAuthChallenge extracts the token URL from a WWW-Authenticate header.
func parseAuthChallenge(header, repo string) string {
	// Simplified parsing - look for realm= URL
	// This is a basic implementation
	parts := strings.Split(header, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "realm=") {
			url := strings.Trim(part[6:], "\"")
			return url + "?service=" + strings.Split(header, "service=")[1] + "&scope=repository:" + repo + ":pull"
		}
	}
	return ""
}

// PushBlob uploads a blob to the registry.
func (rc *RegistryClient) PushBlob(digest string, data []byte) error {
	// Check if blob already exists
	headURL := fmt.Sprintf("%s/v2/%s/blobs/%s", rc.baseURL, rc.repo, digest)
	resp, err := rc.httpClient.Head(headURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil // Blob already exists
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Initiate upload
	initURL := fmt.Sprintf("%s/v2/%s/blobs/uploads/", rc.baseURL, rc.repo)
	req, err := http.NewRequest("POST", initURL, nil)
	if err != nil {
		return fmt.Errorf("initiate upload: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err = rc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("initiate blob upload: %w", err)
	}
	resp.Body.Close()

	// Upload blob with digest
	uploadURL := fmt.Sprintf("%s/v2/%s/blobs/%s", rc.baseURL, rc.repo, digest)
	req, err = http.NewRequest("PUT", uploadURL, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("create put request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Docker-Content-Digest", digest)

	resp, err = rc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload blob: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload failed with status %d", resp.StatusCode)
	}

	return nil
}

// PullBlob downloads a blob from the registry.
func (rc *RegistryClient) PullBlob(digest, outputPath string) error {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", rc.baseURL, rc.repo, digest)

	resp, err := rc.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("fetch blob: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("blob %s not found", digest)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch blob failed with status %d", resp.StatusCode)
	}

	// Write to file
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write blob: %w", err)
	}

	return nil
}

// PushManifest uploads a manifest to the registry.
func (rc *RegistryClient) PushManifest(tag string, manifestBytes []byte, mediaType string) error {
	digest := "sha256:" + computeSHA256(manifestBytes)

	url := fmt.Sprintf("%s/v2/%s/manifests/%s", rc.baseURL, rc.repo, tag)
	req, err := http.NewRequest("PUT", url, strings.NewReader(string(manifestBytes)))
	if err != nil {
		return fmt.Errorf("create manifest request: %w", err)
	}
	req.Header.Set("Content-Type", mediaType)
	req.Header.Set("Docker-Content-Digest", digest)

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push manifest failed with status %d", resp.StatusCode)
	}

	return nil
}

// PullManifest downloads a manifest from the registry.
func (rc *RegistryClient) PullManifest(tag string) ([]byte, string, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", rc.baseURL, rc.repo, tag)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create manifest request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", fmt.Errorf("manifest %s not found", tag)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch manifest failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read manifest: %w", err)
	}

	mediaType := resp.Header.Get("Content-Type")
	return data, mediaType, nil
}

// PushImage pushes an OCI image layout to the registry.
func (rc *RegistryClient) PushImage(layoutDir string) error {
	// Read manifest
	manifestBytes, err := os.ReadFile(filepath.Join(layoutDir, "index.json"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// Push blobs
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
		data, err := os.ReadFile(blobPath)
		if err != nil {
			return fmt.Errorf("read blob %s: %w", entry.Name(), err)
		}

		digest := "sha256:" + entry.Name()
		if err := rc.PushBlob(digest, data); err != nil {
			return fmt.Errorf("push blob %s: %w", digest, err)
		}
	}

	// Push manifest
	return rc.PushManifest("latest", manifestBytes, "application/vnd.oci.image.manifest.v1+json")
}

// PullImage pulls an image from the registry into an OCI layout directory.
func (rc *RegistryClient) PullImage(tag, outputDir string) error {
	// Fetch manifest
	manifestBytes, _, err := rc.PullManifest(tag)
	if err != nil {
		return fmt.Errorf("pull manifest: %w", err)
	}

	// Parse manifest
	var manifest struct {
		Layers []struct {
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	// Create output directory structure
	os.MkdirAll(filepath.Join(outputDir, "blobs", "sha256"), 0o755)

	// Download each blob
	for _, layer := range manifest.Layers {
		blobPath := filepath.Join(outputDir, "blobs", "sha256",
			strings.TrimPrefix(layer.Digest, "sha256:"))
		if err := rc.PullBlob(layer.Digest, blobPath); err != nil {
			return fmt.Errorf("pull blob %s: %w", layer.Digest, err)
		}
	}

	// Write manifest
	os.WriteFile(filepath.Join(outputDir, "index.json"), manifestBytes, 0o644)

	// Write oci-layout
	ociLayout := `{"imageLayoutVersion":"1.0.0"}`
	os.WriteFile(filepath.Join(outputDir, "oci-layout"), []byte(ociLayout), 0o644)

	return nil
}

// computeSHA256 computes the SHA-256 digest of data.
func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
