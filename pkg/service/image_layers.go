package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// LayerMetadata stores layer metadata information
type LayerMetadata struct {
	Digest string `json:"digest"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
}

// LayerCache manages image layer caching
type LayerCache struct {
	mu        sync.RWMutex
	layers    map[string]LayerMetadata
	maxSize   int64                // Maximum total size of cached layers
	totalSize int64                // Current total size of cached layers
	lastUsed  map[string]time.Time // Track when each layer was last used
}

// NewLayerCache creates a new layer cache with size limit
func NewLayerCache(maxSize int64) *LayerCache {
	return &LayerCache{
		layers:   make(map[string]LayerMetadata),
		lastUsed: make(map[string]time.Time),
		maxSize:  maxSize,
	}
}

// Get retrieves a layer from the cache
func (c *LayerCache) Get(digest string) (LayerMetadata, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	metadata, exists := c.layers[digest]
	if !exists {
		return LayerMetadata{}, false
	}

	// Update last used time
	c.lastUsed[digest] = time.Now()
	return metadata, true
}

// Add adds a layer to the cache
func (c *LayerCache) Add(digest string, metadata LayerMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate inputs
	if digest == "" || metadata.Size < 0 {
		return
	}

	// If maxSize is 0, accept all layers
	if c.maxSize == 0 {
		if existing, exists := c.layers[digest]; exists {
			c.totalSize -= existing.Size
		}
		c.layers[digest] = metadata
		c.lastUsed[digest] = time.Now()
		c.totalSize += metadata.Size
		return
	}

	// If the new layer alone exceeds maxSize, don't add it
	if metadata.Size > c.maxSize {
		return
	}

	// First remove existing layer if it exists
	if existing, exists := c.layers[digest]; exists {
		c.totalSize -= existing.Size
	}

	// Check if adding this layer would exceed maxSize
	if c.totalSize+metadata.Size > c.maxSize {
		// Need to evict layers before adding new one
		c.evictLayers(c.totalSize + metadata.Size - c.maxSize)
	}

	// Now add the layer
	c.layers[digest] = metadata
	c.lastUsed[digest] = time.Now()
	c.totalSize += metadata.Size
}

// evictLayers removes least recently used layers until enough space is freed
// Caller must hold the lock
func (c *LayerCache) evictLayers(spaceNeeded int64) {
	if spaceNeeded <= 0 {
		return
	}

	// Create sorted slice of layers by last used time
	type layerInfo struct {
		digest string
		used   time.Time
		size   int64
	}

	layers := make([]layerInfo, 0, len(c.layers))
	for digest, metadata := range c.layers {
		// Skip zero-size layers from eviction
		if metadata.Size > 0 {
			lastUsed, ok := c.lastUsed[digest]
			if !ok {
				lastUsed = time.Now()
			}
			layers = append(layers, layerInfo{
				digest: digest,
				used:   lastUsed,
				size:   metadata.Size,
			})
		}
	}

	// Sort by last used time (oldest first)
	sort.Slice(layers, func(i, j int) bool {
		return layers[i].used.Before(layers[j].used)
	})

	// Remove oldest layers until we have enough space
	spaceFreed := int64(0)
	for _, layer := range layers {
		if spaceFreed >= spaceNeeded {
			break
		}
		if metadata, exists := c.layers[layer.digest]; exists {
			// Remove the layer file first
			if metadata.Path != "" {
				if err := os.Remove(metadata.Path); err != nil && !os.IsNotExist(err) {
					// Log error but continue with cache cleanup
					fmt.Printf("Failed to remove layer file %s: %v\n", metadata.Path, err)
				}
			}
			// Then update cache state
			spaceFreed += metadata.Size
			c.totalSize -= metadata.Size
			delete(c.layers, layer.digest)
			delete(c.lastUsed, layer.digest)
		}
	}
}

// Remove removes a layer from the cache and its file
func (c *LayerCache) Remove(digest string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if metadata, exists := c.layers[digest]; exists {
		// Update total size
		c.totalSize -= metadata.Size

		// Remove from maps
		delete(c.layers, digest)
		delete(c.lastUsed, digest)
	}
}

// reuseLayer reuses an existing layer
func reuseLayer(srcPath, destPath string) error {
	// Ensure source file exists and is accessible
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source file does not exist: %v", err)
		}
		return fmt.Errorf("failed to access source file: %v", err)
	}

	// Create destination directory
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Try to create hard link
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
		os.Remove(destPath) // Clean up failed file
		return fmt.Errorf("failed to copy file: %v", err)
	}

	return nil
}
