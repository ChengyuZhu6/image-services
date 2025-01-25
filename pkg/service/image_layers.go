package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// LayerMetadata stores layer metadata information
type LayerMetadata struct {
	Digest string `json:"digest"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
}

// LayerCache manages image layer caching
type LayerCache struct {
	mu     sync.RWMutex
	layers map[string]LayerMetadata // digest -> layer metadata
}

// NewLayerCache creates a new layer cache
func NewLayerCache() *LayerCache {
	return &LayerCache{
		layers: make(map[string]LayerMetadata),
	}
}

// Get retrieves layer metadata
func (c *LayerCache) Get(digest string) (LayerMetadata, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	layer, exists := c.layers[digest]
	return layer, exists
}

// Add adds a layer to the cache
func (c *LayerCache) Add(digest string, metadata LayerMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.layers[digest] = metadata
}

// Remove removes a layer from the cache
func (c *LayerCache) Remove(digest string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.layers, digest)
}

// reuseLayer reuses an existing layer
func reuseLayer(srcPath, destPath string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Try to create a hard link first
	if err := os.Link(srcPath, destPath); err == nil {
		return nil
	}

	// If hard link fails, copy the file
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %v", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("failed to copy file: %v", err)
	}

	return nil
}
