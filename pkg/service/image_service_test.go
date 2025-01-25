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
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

func TestImageService_PullImage(t *testing.T) {
	// Use fixed content that matches the expected digest
	fixedContent := []byte("fixed layer content for testing")
	expectedDigest := "sha256:86c354b41b3e24f565001dea1f4f9b460dfb08de45baea0f4b111afeed87d9dc"

	// Setup mock registry server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			t.Logf("Request took: %v", time.Since(start))
		}()

		// Add request logging
		t.Logf("Received request: %s %s", r.Method, r.URL.Path)
		defer t.Logf("Completed request: %s %s", r.Method, r.URL.Path)

		switch r.URL.Path {
		case "/v2/":
			t.Log("Handling /v2/ request")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
			return
		case "/v2/library/test/blobs/" + expectedDigest:
			t.Log("Handling blob request")
			// Mock layer download
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fixedContent)))
			w.WriteHeader(http.StatusOK)
			w.Write(fixedContent)
			return
		case "/v2/library/test/manifests/latest":
			t.Log("Handling manifest request")
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			w.Header().Set("Docker-Content-Digest", expectedDigest)
			manifestContent := []byte(`{
				"schemaVersion": 2,
				"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
				"config": {
					"mediaType": "application/vnd.docker.container.image.v1+json",
					"size": 1000,
					"digest": "sha256:test"
				},
				"layers": [
					{
						"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
						"size": 1000,
						"digest": "` + expectedDigest + `"
					}
				]
			}`)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(manifestContent)))
			w.WriteHeader(http.StatusOK)
			w.Write(manifestContent)
			return
		default:
			t.Log("Handling unknown request")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[{"code":"NOT_FOUND"}]}`))
			return
		}
	}))
	defer server.Close()

	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "image-service-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create service instance
	service := &ImageService{
		client:       server.Client(),
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
	}

	// Test cases
	tests := []struct {
		name     string
		imageRef string
		auth     *runtime.AuthConfig
		wantErr  bool
		wantID   string
	}{
		{
			name:     "valid image pull",
			imageRef: server.URL[8:] + "/library/test:latest", // Remove https:// prefix
			auth:     nil,
			wantErr:  false,
			wantID:   fmt.Sprintf("sha256:%x", digest.FromString(server.URL[8:]+"/library/test:latest").Hex()),
		},
		{
			name:     "invalid image reference",
			imageRef: "invalid::",
			auth:     nil,
			wantErr:  true,
			wantID:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := service.PullImage(context.Background(), tt.imageRef, tt.auth)
			if (err != nil) != tt.wantErr {
				t.Errorf("PullImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && id != tt.wantID {
				t.Errorf("PullImage() got ID = %v, want %v", id, tt.wantID)
			}
		})
	}
}

func TestImageService_RemoveImage(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "image-service-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create metadata directory
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatalf("Failed to create metadata directory: %v", err)
	}

	// Create service instance
	service := &ImageService{
		client:       http.DefaultClient,
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
	}

	// Create test image directory
	imageDir := filepath.Join(tmpDir, digest.FromString("test:latest").Hex())
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		t.Fatalf("Failed to create image dir: %v", err)
	}

	// Add test image
	service.images["test:latest"] = &imageMetadata{
		ID:       "sha256:test",
		RepoTags: []string{"test:latest"},
		Size:     1000,
	}

	// Save initial metadata
	if err := service.saveMetadata(); err != nil {
		t.Fatalf("Failed to save initial metadata: %v", err)
	}

	// Test cases
	tests := []struct {
		name     string
		imageRef string
		wantErr  bool
	}{
		{
			name:     "remove existing image",
			imageRef: "test:latest",
			wantErr:  false,
		},
		{
			name:     "remove non-existent image",
			imageRef: "nonexistent:latest",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.RemoveImage(context.Background(), tt.imageRef)
			if (err != nil) != tt.wantErr {
				t.Errorf("RemoveImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestImageService_ImageStatus(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "image-status-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	service := &ImageService{
		client:       http.DefaultClient,
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
	}

	// Add test image
	testImage := &imageMetadata{
		ID:          "sha256:test",
		RepoTags:    []string{"test:latest"},
		RepoDigests: []string{"test@sha256:digest"},
		Size:        1000,
	}
	service.images["test:latest"] = testImage

	tests := []struct {
		name     string
		imageRef string
		want     *runtime.Image
		wantErr  bool
	}{
		{
			name:     "existing image",
			imageRef: "test:latest",
			want: &runtime.Image{
				Id:          "sha256:test",
				RepoTags:    []string{"test:latest"},
				RepoDigests: []string{"test@sha256:digest"},
				Size_:       1000,
			},
			wantErr: false,
		},
		{
			name:     "non-existent image",
			imageRef: "nonexistent:latest",
			want:     nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.ImageStatus(context.Background(), tt.imageRef)
			if (err != nil) != tt.wantErr {
				t.Errorf("ImageStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ImageStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestImageService_ListImages(t *testing.T) {
	service := &ImageService{
		images: map[string]*imageMetadata{
			"test1:latest": {
				ID:       "sha256:test1",
				RepoTags: []string{"test1:latest"},
				Size:     1000,
			},
			"test2:latest": {
				ID:       "sha256:test2",
				RepoTags: []string{"test2:latest"},
				Size:     2000,
			},
		},
	}

	images, err := service.ListImages(context.Background(), nil)
	if err != nil {
		t.Errorf("ListImages() error = %v", err)
	}

	if len(images) != 2 {
		t.Errorf("ListImages() returned %d images, want 2", len(images))
	}
}

// Test layer download verification
func TestImageService_downloadLayer(t *testing.T) {
	// Use fixed content that matches the expected digest
	fixedContent := []byte("fixed layer content for testing")
	expectedDigest := "sha256:86c354b41b3e24f565001dea1f4f9b460dfb08de45baea0f4b111afeed87d9dc"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(fixedContent)
	}))
	defer server.Close()

	tmpDir, err := os.MkdirTemp("", "layer-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	service := &ImageService{
		client: server.Client(),
	}

	tests := []struct {
		name           string
		url            string
		expectedDigest string
		wantErr        bool
	}{
		{
			name:           "valid layer download",
			url:            server.URL,
			expectedDigest: expectedDigest,
			wantErr:        false,
		},
		{
			name:           "digest mismatch",
			url:            server.URL,
			expectedDigest: "sha256:invalid",
			wantErr:        true,
		},
		{
			name:           "invalid url",
			url:            "https://invalid.url",
			expectedDigest: "sha256:test",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.downloadLayer(context.Background(), tt.url, tmpDir, tt.expectedDigest, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("downloadLayer() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test concurrent operations
func TestImageService_ConcurrentOperations(t *testing.T) {
	service := &ImageService{
		images: make(map[string]*imageMetadata),
		mu:     sync.RWMutex{},
	}

	// Add test image
	service.images["test:latest"] = &imageMetadata{
		ID:       "sha256:test",
		RepoTags: []string{"test:latest"},
		Size:     1000,
	}

	// Run concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = service.ImageStatus(context.Background(), "test:latest")
		}()
		go func() {
			defer wg.Done()
			_, _ = service.ListImages(context.Background(), nil)
		}()
	}
	wg.Wait()
}

// Test authentication handling
func TestImageService_AuthHandling(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "auth-handling-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Setup mock registry server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		// First check authentication
		if auth == "" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED"}]}`))
			return
		}
		if auth != "Basic dGVzdHVzZXI6dGVzdHBhc3M=" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":[{"code":"FORBIDDEN"}]}`))
			return
		}

		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))

		case "/v2/test/manifests/latest":
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			manifestContent := []byte(`{
				"schemaVersion": 2,
				"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
				"config": {
					"mediaType": "application/vnd.docker.container.image.v1+json",
					"size": 1000,
					"digest": "sha256:2189176b26e9f608c27104f31fbbaa3e8342b2230d804a21568afea057689391"
				},
				"layers": [
					{
						"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
						"size": 19,
						"digest": "sha256:2189176b26e9f608c27104f31fbbaa3e8342b2230d804a21568afea057689391"
					}
				]
			}`)
			w.Header().Set("Docker-Content-Digest", "sha256:2189176b26e9f608c27104f31fbbaa3e8342b2230d804a21568afea057689391")
			w.Write(manifestContent)

		case "/v2/test/blobs/sha256:2189176b26e9f608c27104f31fbbaa3e8342b2230d804a21568afea057689391":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte("test layer content"))

		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[{"code":"NOT_FOUND"}]}`))
		}
	}))
	defer server.Close()

	service := &ImageService{
		client:       server.Client(),
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
	}

	tests := []struct {
		name    string
		auth    *runtime.AuthConfig
		wantErr bool
	}{
		{
			name:    "no auth",
			auth:    nil,
			wantErr: true,
		},
		{
			name: "invalid auth",
			auth: &runtime.AuthConfig{
				Username: "wrong",
				Password: "wrong",
			},
			wantErr: true,
		},
		{
			name: "valid auth",
			auth: &runtime.AuthConfig{
				Username: "testuser",
				Password: "testpass",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.PullImage(context.Background(), server.URL[8:]+"/test:latest", tt.auth)
			if err != nil {
				t.Logf("PullImage() error: %v", err)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("PullImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestImageService_MetadataPersistence tests the metadata persistence functionality
func TestImageService_MetadataPersistence(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "metadata-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create initial service instance
	service := &ImageService{
		client:       http.DefaultClient,
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
	}

	// Add test data
	testImage := &imageMetadata{
		ID:          "sha256:test",
		RepoTags:    []string{"test:latest"},
		RepoDigests: []string{"test@sha256:digest"},
		Size:        1000,
	}
	service.images["test:latest"] = testImage

	// Test saving metadata
	if err := service.saveMetadata(); err != nil {
		t.Errorf("saveMetadata() error = %v", err)
	}

	// Verify metadata file exists
	if _, err := os.Stat(service.metadataFile); os.IsNotExist(err) {
		t.Error("Metadata file was not created")
	}

	// Create new service instance to test loading
	newService := &ImageService{
		client:       http.DefaultClient,
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
	}

	// Test loading metadata
	if err := newService.loadMetadata(); err != nil {
		t.Errorf("loadMetadata() error = %v", err)
	}

	// Verify loaded data
	loadedImage, ok := newService.images["test:latest"]
	if !ok {
		t.Error("Failed to load image metadata")
	}
	if loadedImage.ID != testImage.ID {
		t.Errorf("Loaded image ID = %v, want %v", loadedImage.ID, testImage.ID)
	}
}

// TestImageService_MetadataConsistency tests metadata consistency during operations
func TestImageService_MetadataConsistency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "consistency-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	service := &ImageService{
		client:       http.DefaultClient,
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
		mu:           sync.RWMutex{},
	}

	// Test concurrent metadata operations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			imageRef := fmt.Sprintf("test%d:latest", index)
			err := service.AddImage(imageRef, &imageMetadata{
				ID:       fmt.Sprintf("sha256:test%d", index),
				RepoTags: []string{imageRef},
				Size:     int64(index * 1000),
			})
			if err != nil {
				t.Errorf("saveMetadata() error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	// Verify final state
	if len(service.images) != 10 {
		t.Errorf("Expected 10 images, got %d", len(service.images))
	}

	// Test metadata corruption resistance
	if err := os.WriteFile(service.metadataFile+".tmp", []byte("invalid json"), 0644); err != nil {
		t.Fatalf("Failed to write corrupt metadata: %v", err)
	}

	// Verify service can still save metadata
	if err := service.saveMetadata(); err != nil {
		t.Errorf("saveMetadata() error after corruption = %v", err)
	}
}

func TestImageService_LayerReuse(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "layer-reuse-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test service instance
	service := &ImageService{
		client:       http.DefaultClient,
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
	}

	// Create test layer
	layerContent := []byte("test layer content")
	layerDigest := "sha256:testlayer"
	layerPath := filepath.Join(tmpDir, "test-layer")
	if err := os.WriteFile(layerPath, layerContent, 0644); err != nil {
		t.Fatalf("Failed to create test layer: %v", err)
	}

	// Add layer to cache
	metadata := LayerMetadata{
		Digest: layerDigest,
		Path:   layerPath,
		Size:   int64(len(layerContent)),
	}
	service.layerCache.Add(layerDigest, metadata)

	// Test layer reuse
	destPath := filepath.Join(tmpDir, "reused-layer")
	if err := reuseLayer(layerPath, destPath); err != nil {
		t.Errorf("reuseLayer() error = %v", err)
	}

	// Verify reused layer content
	reusedContent, err := os.ReadFile(destPath)
	if err != nil {
		t.Errorf("Failed to read reused layer: %v", err)
	}
	if !bytes.Equal(reusedContent, layerContent) {
		t.Errorf("Reused layer content = %v, want %v", reusedContent, layerContent)
	}

	// Verify layer cache
	if metadata, exists := service.layerCache.Get(layerDigest); !exists {
		t.Error("Layer not found in cache")
	} else if metadata.Path != layerPath {
		t.Errorf("Layer path = %v, want %v", metadata.Path, layerPath)
	}
}

func TestImageService_LayerCleanup(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "layer-cleanup-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test service instance
	service := &ImageService{
		client:       http.DefaultClient,
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(),
	}

	// Create two test layers
	layer1 := LayerMetadata{
		Digest: "sha256:layer1",
		Path:   filepath.Join(tmpDir, "layer1"),
		Size:   100,
	}
	layer2 := LayerMetadata{
		Digest: "sha256:layer2",
		Path:   filepath.Join(tmpDir, "layer2"),
		Size:   200,
	}

	// Create layer files
	for _, layer := range []LayerMetadata{layer1, layer2} {
		if err := os.WriteFile(layer.Path, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create layer file: %v", err)
		}
	}

	// Add two shared layer images
	service.images["image1"] = &imageMetadata{
		ID:     "sha256:image1",
		Layers: []LayerMetadata{layer1, layer2},
	}
	service.images["image2"] = &imageMetadata{
		ID:     "sha256:image2",
		Layers: []LayerMetadata{layer1},
	}

	// Remove first image
	if err := service.RemoveImage(context.Background(), "image1"); err != nil {
		t.Errorf("RemoveImage() error = %v", err)
	}

	// Verify shared layer (layer1) still exists
	if _, err := os.Stat(layer1.Path); os.IsNotExist(err) {
		t.Error("Shared layer was incorrectly removed")
	}

	// Verify non-shared layer (layer2) was removed
	if _, err := os.Stat(layer2.Path); !os.IsNotExist(err) {
		t.Error("Unshared layer was not removed")
	}
}

func TestImageService_LayerCache(t *testing.T) {
	cache := NewLayerCache()

	// Test adding and getting
	metadata := LayerMetadata{
		Digest: "sha256:test",
		Path:   "/test/path",
		Size:   100,
	}
	cache.Add(metadata.Digest, metadata)

	if got, exists := cache.Get(metadata.Digest); !exists {
		t.Error("Layer not found in cache")
	} else if got != metadata {
		t.Errorf("Layer metadata = %v, want %v", got, metadata)
	}

	// Test deleting
	cache.Remove(metadata.Digest)
	if _, exists := cache.Get(metadata.Digest); exists {
		t.Error("Layer still exists after removal")
	}

	// Test concurrent safety
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			digest := fmt.Sprintf("sha256:test%d", i)
			metadata := LayerMetadata{
				Digest: digest,
				Path:   fmt.Sprintf("/test/path%d", i),
				Size:   int64(i * 100),
			}
			cache.Add(digest, metadata)
			if _, exists := cache.Get(digest); !exists {
				t.Errorf("Layer %s not found after concurrent add", digest)
			}
		}(i)
	}
	wg.Wait()
}
