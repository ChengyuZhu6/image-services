package service

import (
	"context"
	"crypto/tls"
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

type imageMetadata struct {
	ID          string
	RepoTags    []string
	RepoDigests []string
	Size        int64
}

type ImageService struct {
	client    *http.Client
	imageRoot string
	images    map[string]*imageMetadata
}

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

func NewImageService() *ImageService {
	// Create image storage directory
	if err := os.MkdirAll("/var/lib/image-service", 0755); err != nil {
		panic(fmt.Sprintf("Failed to create image root directory: %v", err))
	}

	// Create HTTP client with insecure HTTPS support
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	return &ImageService{
		client:    &http.Client{Transport: tr},
		imageRoot: "/var/lib/image-service",
		images:    make(map[string]*imageMetadata),
	}
}

// PullImage implements image pulling functionality
func (s *ImageService) PullImage(ctx context.Context, imageRef string, auth *runtime.AuthConfig) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", fmt.Errorf("invalid image reference: %v", err)
	}

	// Check if image already exists
	if _, ok := s.images[imageRef]; ok {
		return imageRef, nil
	}

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

	// Get manifest
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, tag)
	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return "", err
	}

	// Add manifest type headers
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Add("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Add("Accept", "application/vnd.oci.image.index.v1+json")

	if auth != nil {
		req.SetBasicAuth(auth.GetUsername(), auth.GetPassword())
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get manifest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get manifest: %s (URL: %s)", resp.Status, manifestURL)
	}

	// Parse manifest
	var manifest DockerManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return "", fmt.Errorf("failed to parse manifest: %v", err)
	}

	// Create image directory
	imageDir := filepath.Join(s.imageRoot, digest.FromString(imageRef).Hex())
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create image directory: %v", err)
	}

	// Download layers
	var totalSize int64
	for i, layer := range manifest.Layers {
		layerDir := filepath.Join(imageDir, fmt.Sprintf("layer-%d", i))
		if err := os.MkdirAll(layerDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create layer directory: %v", err)
		}

		// Download layer
		layerURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repository, layer.Digest)
		if err := s.downloadLayer(ctx, layerURL, layerDir, layer.Digest, auth); err != nil {
			return "", fmt.Errorf("failed to download layer %d: %v", i, err)
		}

		totalSize += layer.Size
	}

	// Save image metadata
	dgst := digest.FromString(imageRef)
	s.images[imageRef] = &imageMetadata{
		ID:          dgst.String(),
		RepoTags:    []string{imageRef},
		RepoDigests: []string{fmt.Sprintf("%s@%s", imageRef, dgst)},
		Size:        totalSize,
	}

	return imageRef, nil
}

// downloadLayer downloads a single layer from the registry
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

	// Save layer blob
	layerPath := filepath.Join(destDir, "layer.tar")
	tempPath := layerPath + ".tmp"
	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create layer file: %v", err)
	}
	defer f.Close()

	// Create digest writer to verify layer content
	digester := digest.Canonical.Digester()
	writer := io.MultiWriter(f, digester.Hash())

	fmt.Printf("Saving layer to: %s\n", tempPath)
	size, err := io.Copy(writer, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to save layer: %v", err)
	}

	// Verify digest
	actualDigest := digester.Digest().String()
	if actualDigest != expectedDigest {
		os.Remove(tempPath)
		return fmt.Errorf("layer digest mismatch: expected %s, got %s", expectedDigest, actualDigest)
	}

	// Move verified layer to final location
	if err := os.Rename(tempPath, layerPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to move verified layer: %v", err)
	}

	fmt.Printf("Layer verified and saved: size=%d, digest=%s\n", size, actualDigest)
	return nil
}

// checkRegistry checks if the registry is accessible
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

	return nil
}

// RemoveImage implements image removal functionality
func (s *ImageService) RemoveImage(ctx context.Context, imageRef string) error {
	// Delete image from local storage
	return nil
}

// ImageStatus implements image status retrieval functionality
func (s *ImageService) ImageStatus(ctx context.Context, imageRef string) (*runtime.Image, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return nil, fmt.Errorf("invalid image reference: %v", err)
	}

	return &runtime.Image{
		Id:          imageRef,
		RepoTags:    []string{named.String()},
		RepoDigests: []string{},
		Size_:       0,
	}, nil
}

// ListImages implements image listing functionality
func (s *ImageService) ListImages(ctx context.Context, filter *runtime.ImageFilter) ([]*runtime.Image, error) {
	var images []*runtime.Image

	for _, img := range s.images {
		images = append(images, &runtime.Image{
			Id:          img.ID,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Size_:       uint64(img.Size),
		})
	}

	return images, nil
}

// GetImageRoot returns the root path of image storage
func (s *ImageService) GetImageRoot() string {
	return s.imageRoot
}
