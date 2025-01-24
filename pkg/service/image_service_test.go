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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	service := &ImageService{
		images: map[string]*imageMetadata{
			"test:latest": {
				ID:       "sha256:test",
				RepoTags: []string{"test:latest"},
				Size:     1000,
			},
		},
	}

	tests := []struct {
		name     string
		imageRef string
		wantErr  bool
	}{
		{
			name:     "get existing image status",
			imageRef: "test:latest",
			wantErr:  false,
		},
		{
			name:     "get non-existent image status",
			imageRef: "nonexistent:latest",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img, err := service.ImageStatus(context.Background(), tt.imageRef)
			if (err != nil) != tt.wantErr {
				t.Errorf("ImageStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && img == nil {
				t.Error("ImageStatus() returned nil image for existing image")
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
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	service := &ImageService{
		client: server.Client(),
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
			name: "valid auth",
			auth: &runtime.AuthConfig{
				Username: "test",
				Password: "test",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.checkRegistry(context.Background(), server.URL, tt.auth)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkRegistry() error = %v, wantErr %v", err, tt.wantErr)
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
