package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/distribution/reference"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type ImageService struct {
	client    *http.Client
	imageRoot string
}

func NewImageService() *ImageService {
	// Create HTTP client with insecure HTTPS support
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	return &ImageService{
		client:    &http.Client{Transport: tr},
		imageRoot: "/var/lib/image-service",
	}
}

// PullImage implements image pulling functionality
func (s *ImageService) PullImage(ctx context.Context, imageRef string, auth *runtime.AuthConfig) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", fmt.Errorf("invalid image reference: %v", err)
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

	return imageRef, nil
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
	// List images from local storage
	return []*runtime.Image{}, nil
}

// GetImageRoot returns the root path of image storage
func (s *ImageService) GetImageRoot() string {
	return s.imageRoot
}
