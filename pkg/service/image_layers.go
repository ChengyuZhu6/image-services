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
	c.mu.Lock() // Use full lock to ensure consistency
	defer c.mu.Unlock()

	metadata, exists := c.layers[digest]
	if exists {
		c.lastUsed[digest] = time.Now()
	}
	return metadata, exists
}

// Add adds a layer to the cache
func (c *LayerCache) Add(digest string, metadata LayerMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate inputs
	if digest == "" || metadata.Size < 0 {
		return
	}

	// Check if layer already exists
	if existing, exists := c.layers[digest]; exists {
		c.totalSize -= existing.Size
	}

	// Add new layer
	c.layers[digest] = metadata
	c.lastUsed[digest] = time.Now()
	c.totalSize += metadata.Size

	// If we exceed maxSize, evict layers
	if c.maxSize > 0 && c.totalSize > c.maxSize {
		c.evictLRULocked()
	}
}

// evictLRULocked removes least recently used layers until cache size is under maxSize
// Caller must hold the lock
func (c *LayerCache) evictLRULocked() {
	// Create a slice of all layers sorted by last used time
	type layerInfo struct {
		digest string
		used   time.Time
		size   int64
	}

	layers := make([]layerInfo, 0, len(c.layers))
	for digest, metadata := range c.layers {
		lastUsed, ok := c.lastUsed[digest]
		if !ok {
			lastUsed = time.Now() // Default to now if no last used time
		}
		layers = append(layers, layerInfo{
			digest: digest,
			used:   lastUsed,
			size:   metadata.Size,
		})
	}

	// Sort by last used time
	sort.Slice(layers, func(i, j int) bool {
		return layers[i].used.Before(layers[j].used)
	})

	// Remove oldest layers until we're under maxSize
	for _, layer := range layers {
		if c.totalSize <= c.maxSize {
			break
		}
		if metadata, exists := c.layers[layer.digest]; exists {
			c.totalSize -= metadata.Size
			delete(c.layers, layer.digest)
			delete(c.lastUsed, layer.digest)
		}
	}
}

// Remove removes a layer from the cache
func (c *LayerCache) Remove(digest string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if metadata, exists := c.layers[digest]; exists {
		c.totalSize -= metadata.Size
		delete(c.layers, digest)
		delete(c.lastUsed, digest)
	}
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
