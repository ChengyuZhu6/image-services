/*
 * Copyright 2025 ChengyuZhu6 <hudson@cyzhu.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type imageMetadata struct {
	ID          string          `json:"id"`
	RepoTags    []string        `json:"repo_tags"`
	RepoDigests []string        `json:"repo_digests"`
	Size        int64           `json:"size"`
	Layers      []LayerMetadata `json:"layers"`
}

type ImageService struct {
	client       *http.Client
	imageRoot    string
	images       map[string]*imageMetadata
	mu           sync.RWMutex
	metadataFile string
	layerCache   *LayerCache
	gc           *GarbageCollector
}

func NewImageService() *ImageService {
	// Create image storage directory
	imageRoot := "/var/lib/image-service"
	if err := os.MkdirAll(imageRoot, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create image root directory: %v", err))
	}

	// Set default cache size limit to 10GB
	const defaultMaxCacheSize = 10 * 1024 * 1024 * 1024

	// Create HTTP client with insecure HTTPS support
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	service := &ImageService{
		client:       &http.Client{Transport: tr},
		imageRoot:    imageRoot,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(imageRoot, "metadata.json"),
		layerCache:   NewLayerCache(defaultMaxCacheSize),
	}

	// Load existing metadata
	if err := service.loadMetadata(); err != nil {
		panic(fmt.Sprintf("Failed to load metadata: %v", err))
	}

	// Initialize and start garbage collector
	service.gc = NewGarbageCollector(service, 1*time.Hour)
	service.gc.Start()

	return service
}

// PullImage implements image pulling functionality
func (s *ImageService) PullImage(ctx context.Context, imageRef string, auth *runtime.AuthConfig) (string, error) {
	return s.pullImage(ctx, imageRef, auth)
}

// RemoveImage implements image removal functionality
func (s *ImageService) RemoveImage(ctx context.Context, imageRef string) error {
	return s.removeImage(ctx, imageRef)
}

// ImageStatus implements image status retrieval functionality
func (s *ImageService) ImageStatus(ctx context.Context, imageRef string) (*runtime.Image, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if image exists in our metadata
	if img, ok := s.images[imageRef]; ok {
		return &runtime.Image{
			Id:          img.ID,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Size_:       uint64(img.Size),
		}, nil
	}

	// Image not found
	return nil, fmt.Errorf("image not found: %s", imageRef)
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

// AddImage safely adds an image to the service
func (s *ImageService) AddImage(imageRef string, img *imageMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.images[imageRef] = img
	return s.saveMetadata()
}

// Close stops the image service and its components
func (s *ImageService) Close() error {
	if s.gc != nil {
		s.gc.Stop()
	}
	return nil
}
