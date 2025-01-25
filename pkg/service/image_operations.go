package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// DockerManifest represents a Docker image manifest
type DockerManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

// LayerInfo stores layer download information
type LayerInfo struct {
	MediaType string
	Size      int64
	Digest    string
	Path      string
}

func (s *ImageService) pullImage(ctx context.Context, imageRef string, auth *runtime.AuthConfig) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", fmt.Errorf("invalid image reference: %v", err)
	}

	// Check if image already exists
	s.mu.RLock()
	if img, ok := s.images[imageRef]; ok {
		defer s.mu.RUnlock()
		return img.ID, nil
	}
	s.mu.RUnlock()

	// Build Registry API URL
	registry := reference.Domain(named)
	repository := reference.Path(named)
	tag := "latest"
	if tagged, ok := named.(reference.Tagged); ok {
		tag = tagged.Tag()
	}

	// First check API version
	checkURL := fmt.Sprintf("https://%s/v2/", registry)
	if err := s.checkRegistry(ctx, checkURL, auth); err != nil {
		return "", fmt.Errorf("failed to check registry: %v", err)
	}

	// Get manifest and download layers
	dgst, totalSize, err := s.downloadImage(ctx, registry, repository, tag, imageRef, auth)
	if err != nil {
		return "", err
	}

	// Create image ID and save metadata
	imageID := fmt.Sprintf("sha256:%x", dgst.Hex())
	s.mu.Lock()
	s.images[imageRef] = &imageMetadata{
		ID:          imageID,
		RepoTags:    []string{imageRef},
		RepoDigests: []string{fmt.Sprintf("%s@%s", imageRef, dgst)},
		Size:        totalSize,
	}
	s.mu.Unlock()

	if err := s.saveMetadata(); err != nil {
		return "", fmt.Errorf("failed to save metadata: %v", err)
	}

	fmt.Printf("Successfully pulled image: %s\n", imageRef)
	return imageID, nil
}

func (s *ImageService) downloadImage(ctx context.Context, registry, repository, tag, imageRef string, auth *runtime.AuthConfig) (digest.Digest, int64, error) {
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, tag)
	manifest, err := s.getManifest(ctx, manifestURL, auth)
	if err != nil {
		return "", 0, err
	}

	// Create image directory
	dgst := digest.FromString(imageRef)
	imageDir := filepath.Join(s.imageRoot, dgst.Hex())
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return "", 0, fmt.Errorf("failed to create image directory: %v", err)
	}

	// Download layers
	var totalSize int64
	for i, layer := range manifest.Layers {
		layerDir := filepath.Join(imageDir, fmt.Sprintf("layer-%d", i))
		if err := os.MkdirAll(layerDir, 0755); err != nil {
			return "", 0, fmt.Errorf("failed to create layer directory: %v", err)
		}

		layerURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repository, layer.Digest)
		if err := s.downloadLayer(ctx, layerURL, layerDir, layer.Digest, auth); err != nil {
			return "", 0, fmt.Errorf("failed to download layer %d: %v", i, err)
		}

		totalSize += layer.Size
	}

	return dgst, totalSize, nil
}

func (s *ImageService) getManifest(ctx context.Context, url string, auth *runtime.AuthConfig) (*DockerManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if auth != nil {
		req.SetBasicAuth(auth.GetUsername(), auth.GetPassword())
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get manifest: %s", resp.Status)
	}

	var manifest DockerManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %v", err)
	}

	return &manifest, nil
}

func (s *ImageService) downloadLayer(ctx context.Context, url, destDir string, expectedDigest string, auth *runtime.AuthConfig) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	if auth != nil {
		req.SetBasicAuth(auth.GetUsername(), auth.GetPassword())
	}

	fmt.Printf("Downloading layer from: %s\n", url)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download layer: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download layer: %s", resp.Status)
	}

	return s.saveLayer(destDir, resp.Body, expectedDigest)
}

func (s *ImageService) saveLayer(destDir string, reader io.Reader, expectedDigest string) error {
	layerPath := filepath.Join(destDir, "layer.tar")
	tempPath := layerPath + ".tmp"
	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create layer file: %v", err)
	}
	defer f.Close()

	digester := digest.Canonical.Digester()
	writer := io.MultiWriter(f, digester.Hash())

	fmt.Printf("Saving layer to: %s\n", tempPath)
	size, err := io.Copy(writer, reader)
	f.Close()
	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to save layer: %v", err)
	}

	actualDigest := digester.Digest().String()
	if actualDigest != expectedDigest {
		os.Remove(tempPath)
		return fmt.Errorf("layer digest mismatch: expected %s, got %s", expectedDigest, actualDigest)
	}

	if err := os.Rename(tempPath, layerPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to move verified layer: %v", err)
	}

	fmt.Printf("Layer verified and saved: size=%d, digest=%s\n", size, actualDigest)
	return nil
}

func (s *ImageService) checkRegistry(ctx context.Context, url string, auth *runtime.AuthConfig) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	if auth != nil {
		req.SetBasicAuth(auth.GetUsername(), auth.GetPassword())
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("registry check failed: %s", resp.Status)
	}

	if resp.StatusCode == http.StatusUnauthorized && auth == nil {
		return fmt.Errorf("authentication required")
	}

	return nil
}

func (s *ImageService) removeImage(ctx context.Context, imageRef string) error {
	// First check if image exists with read lock
	s.mu.RLock()
	_, exists := s.images[imageRef]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("image not found: %s", imageRef)
	}

	// Remove image directory
	imageDir := filepath.Join(s.imageRoot, digest.FromString(imageRef).Hex())
	if err := os.RemoveAll(imageDir); err != nil {
		return fmt.Errorf("failed to remove image directory: %v", err)
	}

	// Now acquire write lock for metadata update
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double check image still exists
	if _, ok := s.images[imageRef]; !ok {
		return fmt.Errorf("image was removed by another operation: %s", imageRef)
	}

	delete(s.images, imageRef)
	if err := s.saveMetadata(); err != nil {
		return fmt.Errorf("failed to save metadata: %v", err)
	}

	fmt.Printf("Successfully removed image: %s\n", imageRef)
	return nil
}

func (s *ImageService) saveMetadata() error {
	if s.images == nil {
		s.images = make(map[string]*imageMetadata)
	}

	data, err := json.MarshalIndent(s.images, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata (len=%d): %v", len(s.images), err)
	}

	if err := os.MkdirAll(filepath.Dir(s.metadataFile), 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %v", err)
	}

	tempFile := filepath.Join(filepath.Dir(s.metadataFile), filepath.Base(s.metadataFile)+".tmp")
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %v", err)
	}

	if err := os.Rename(tempFile, s.metadataFile); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to save metadata: %v", err)
	}

	fmt.Printf("Successfully saved metadata for %d images\n", len(s.images))
	return nil
}

func (s *ImageService) loadMetadata() error {
	data, err := os.ReadFile(s.metadataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read metadata: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := json.Unmarshal(data, &s.images); err != nil {
		return fmt.Errorf("failed to unmarshal metadata: %v", err)
	}

	return nil
}
