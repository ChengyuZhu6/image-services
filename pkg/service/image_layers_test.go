package service

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLayerCache_SizeLimit(t *testing.T) {
	// Create cache with 100 byte limit
	cache := NewLayerCache(int64(100))

	// Add layers that fit within limit
	cache.Add("layer1", LayerMetadata{
		Digest: "layer1",
		Path:   "/path/1",
		Size:   40,
	})
	cache.Add("layer2", LayerMetadata{
		Digest: "layer2",
		Path:   "/path/2",
		Size:   30,
	})

	// Verify both layers are cached
	if _, exists := cache.Get("layer1"); !exists {
		t.Error("layer1 should be in cache")
	}
	if _, exists := cache.Get("layer2"); !exists {
		t.Error("layer2 should be in cache")
	}
	if cache.totalSize != 70 {
		t.Errorf("Expected total size 70, got %d", cache.totalSize)
	}

	// Add layer that exceeds limit
	cache.Add("layer3", LayerMetadata{
		Digest: "layer3",
		Path:   "/path/3",
		Size:   50,
	})

	// Verify oldest layer was evicted
	if _, exists := cache.Get("layer1"); exists {
		t.Error("layer1 should have been evicted")
	}
	if _, exists := cache.Get("layer2"); !exists {
		t.Error("layer2 should still be in cache")
	}
	if _, exists := cache.Get("layer3"); !exists {
		t.Error("layer3 should be in cache")
	}
	if cache.totalSize != 80 {
		t.Errorf("Expected total size 80, got %d", cache.totalSize)
	}
}

func TestLayerCache_LRU(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add initial layers
	cache.Add("layer1", LayerMetadata{Size: 40})
	cache.Add("layer2", LayerMetadata{Size: 30})
	time.Sleep(time.Millisecond) // Ensure different timestamps

	// Access layer1 to make it most recently used
	cache.Get("layer1")
	time.Sleep(time.Millisecond)

	// Add layer that exceeds limit
	cache.Add("layer3", LayerMetadata{Size: 50})

	// Verify least recently used layer (layer2) was evicted
	if _, exists := cache.Get("layer1"); !exists {
		t.Error("layer1 should still be in cache")
	}
	if _, exists := cache.Get("layer2"); exists {
		t.Error("layer2 should have been evicted")
	}
	if _, exists := cache.Get("layer3"); !exists {
		t.Error("layer3 should be in cache")
	}
}

func TestLayerCache_UpdateExisting(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add initial layer
	cache.Add("layer1", LayerMetadata{
		Digest: "layer1",
		Size:   40,
	})

	// Update same layer with different size
	cache.Add("layer1", LayerMetadata{
		Digest: "layer1",
		Size:   60,
	})

	// Verify size was updated correctly
	if cache.totalSize != 60 {
		t.Errorf("Expected total size 60, got %d", cache.totalSize)
	}
}

func TestLayerCache_Remove(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add layers
	cache.Add("layer1", LayerMetadata{Size: 40})
	cache.Add("layer2", LayerMetadata{Size: 30})

	// Remove layer
	cache.Remove("layer1")

	// Verify layer was removed
	if _, exists := cache.Get("layer1"); exists {
		t.Error("layer1 should have been removed")
	}
	if cache.totalSize != 30 {
		t.Errorf("Expected total size 30, got %d", cache.totalSize)
	}

	// Verify lastUsed entry was removed
	if _, exists := cache.lastUsed["layer1"]; exists {
		t.Error("lastUsed entry for layer1 should have been removed")
	}
}

func TestLayerCache_ZeroLimit(t *testing.T) {
	// Cache with 0 limit should not evict layers
	cache := NewLayerCache(int64(0))

	cache.Add("layer1", LayerMetadata{Size: 1000})
	cache.Add("layer2", LayerMetadata{Size: 2000})

	if _, exists := cache.Get("layer1"); !exists {
		t.Error("layer1 should be in cache")
	}
	if _, exists := cache.Get("layer2"); !exists {
		t.Error("layer2 should be in cache")
	}
}

func TestLayerCache_ConcurrentAccess(t *testing.T) {
	cache := NewLayerCache(int64(1000))
	const goroutines = 10

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // 3 operations per goroutine

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			// Add
			cache.Add(fmt.Sprintf("layer%d", id), LayerMetadata{
				Digest: fmt.Sprintf("layer%d", id),
				Size:   int64(10 * id),
			})
		}(i)

		go func(id int) {
			defer wg.Done()
			// Get
			cache.Get(fmt.Sprintf("layer%d", id))
		}(i)

		go func(id int) {
			defer wg.Done()
			// Remove
			cache.Remove(fmt.Sprintf("layer%d", id))
		}(i)
	}

	wg.Wait()
}

func TestLayerCache_NegativeSize(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Try to add layer with negative size
	cache.Add("layer1", LayerMetadata{
		Digest: "layer1",
		Size:   -10,
	})

	// Verify total size is not negative
	if cache.totalSize < 0 {
		t.Errorf("Total size should not be negative, got %d", cache.totalSize)
	}
}

func TestLayerCache_LargeNumberOfLayers(t *testing.T) {
	cache := NewLayerCache(int64(1000))
	const numLayers = 100

	// Add many layers
	for i := 0; i < numLayers; i++ {
		cache.Add(fmt.Sprintf("layer%d", i), LayerMetadata{
			Digest: fmt.Sprintf("layer%d", i),
			Size:   10,
		})
	}

	// Verify eviction works with large numbers
	if cache.totalSize > cache.maxSize {
		t.Errorf("Cache size %d exceeds limit %d", cache.totalSize, cache.maxSize)
	}
}

func TestLayerCache_UpdateLastUsed(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add a layer
	cache.Add("layer1", LayerMetadata{Size: 40})
	firstAccess := cache.lastUsed["layer1"]

	time.Sleep(time.Millisecond)

	// Access the layer
	cache.Get("layer1")
	secondAccess := cache.lastUsed["layer1"]

	// Verify lastUsed time was updated
	if !secondAccess.After(firstAccess) {
		t.Error("lastUsed time should have been updated")
	}
}

func TestLayerCache_EvictionOrder(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add layers in sequence with delays
	cache.Add("layer1", LayerMetadata{Size: 40})
	time.Sleep(time.Millisecond)

	cache.Add("layer2", LayerMetadata{Size: 40})
	time.Sleep(time.Millisecond)

	cache.Add("layer3", LayerMetadata{Size: 40})

	// Verify layers were evicted in correct order (oldest first)
	if _, exists := cache.Get("layer1"); exists {
		t.Error("layer1 should have been evicted first")
	}
	if _, exists := cache.Get("layer2"); !exists {
		t.Error("layer2 should still be in cache")
	}
	if _, exists := cache.Get("layer3"); !exists {
		t.Error("layer3 should still be in cache")
	}
}

func TestLayerCache_RaceConditions(t *testing.T) {
	cache := NewLayerCache(int64(1000))
	const goroutines = 10

	// Test concurrent read/write on the same layer
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			cache.Add("shared-layer", LayerMetadata{
				Digest: "shared-layer",
				Size:   100,
			})
		}()

		go func() {
			defer wg.Done()
			cache.Get("shared-layer")
		}()
	}

	wg.Wait()
}

func TestLayerCache_MultipleEvictions(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add layers that will require multiple evictions
	for i := 0; i < 5; i++ {
		cache.Add(fmt.Sprintf("layer%d", i), LayerMetadata{
			Digest: fmt.Sprintf("layer%d", i),
			Size:   30, // Each layer is 30% of cache size
		})
	}

	// Verify only the most recent layers remain
	totalLayers := 0
	for i := 0; i < 5; i++ {
		if _, exists := cache.Get(fmt.Sprintf("layer%d", i)); exists {
			totalLayers++
		}
	}

	// Should only have space for 3 layers (90 bytes)
	if totalLayers != 3 {
		t.Errorf("Expected 3 layers after eviction, got %d", totalLayers)
	}
}

func TestLayerCache_GetNonExistent(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Try to get non-existent layer
	metadata, exists := cache.Get("non-existent")
	if exists {
		t.Error("Get should return false for non-existent layer")
	}
	if metadata != (LayerMetadata{}) {
		t.Error("Get should return empty metadata for non-existent layer")
	}
}

func TestLayerCache_RemoveNonExistent(t *testing.T) {
	cache := NewLayerCache(int64(100))
	initialSize := cache.totalSize

	// Add a layer
	cache.Add("layer1", LayerMetadata{Size: 50})

	// Try to remove non-existent layer
	cache.Remove("non-existent")

	// Verify cache state wasn't affected
	if cache.totalSize != initialSize+50 {
		t.Error("Remove of non-existent layer should not affect totalSize")
	}
}

func TestLayerCache_ExactSizeLimit(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add layer that exactly matches size limit
	cache.Add("layer1", LayerMetadata{Size: 100})

	// Verify layer was added
	if _, exists := cache.Get("layer1"); !exists {
		t.Error("Layer matching exact size limit should be accepted")
	}

	// Try to add another layer
	cache.Add("layer2", LayerMetadata{Size: 1})

	// Verify first layer was evicted
	if _, exists := cache.Get("layer1"); exists {
		t.Error("First layer should be evicted")
	}
	if _, exists := cache.Get("layer2"); !exists {
		t.Error("Second layer should be present")
	}
}

func TestLayerCache_ConcurrentEviction(t *testing.T) {
	cache := NewLayerCache(int64(100))
	const goroutines = 5

	// Fill cache to near limit
	cache.Add("base", LayerMetadata{Size: 90})

	// Concurrently add layers that will trigger eviction
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			cache.Add(fmt.Sprintf("layer%d", id), LayerMetadata{
				Size: 20, // Each addition should trigger eviction
			})
		}(i)
	}

	wg.Wait()

	// Verify cache size is still within limits
	if cache.totalSize > cache.maxSize {
		t.Errorf("Cache size %d exceeds limit %d after concurrent evictions",
			cache.totalSize, cache.maxSize)
	}
}

func TestLayerCache_UpdateSizeDuringEviction(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add initial layer
	cache.Add("layer1", LayerMetadata{Size: 60})

	// Update size of existing layer while adding new one
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		cache.Add("layer1", LayerMetadata{Size: 80}) // Update size
	}()

	go func() {
		defer wg.Done()
		cache.Add("layer2", LayerMetadata{Size: 30}) // Should trigger eviction
	}()

	wg.Wait()

	// Verify cache remains consistent
	if cache.totalSize > cache.maxSize {
		t.Errorf("Cache size %d exceeds limit %d", cache.totalSize, cache.maxSize)
	}
}

func TestLayerCache_ConcurrentSizeUpdate(t *testing.T) {
	cache := NewLayerCache(int64(1000))
	const iterations = 100

	// Add initial layer
	cache.Add("layer", LayerMetadata{Size: 100})
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: repeatedly increase size
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cache.Add("layer", LayerMetadata{Size: 100 + int64(i)})
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 2: repeatedly decrease size
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cache.Add("layer", LayerMetadata{Size: 100 - int64(i)})
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()

	// Verify total size is still accurate
	if metadata, exists := cache.Get("layer"); exists {
		if cache.totalSize != metadata.Size {
			t.Errorf("Cache total size %d doesn't match layer size %d",
				cache.totalSize, metadata.Size)
		}
	}
}

func TestLayerCache_EvictionWithZeroSizeLayer(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add some normal layers
	cache.Add("layer1", LayerMetadata{Size: 40})
	cache.Add("layer2", LayerMetadata{Size: 40})

	// Add a zero size layer
	cache.Add("zero", LayerMetadata{Size: 0})

	// Add layer that triggers eviction
	cache.Add("layer3", LayerMetadata{Size: 30})

	// Verify zero size layer wasn't affected by eviction
	if _, exists := cache.Get("zero"); !exists {
		t.Error("Zero size layer should not be evicted")
	}

	// Verify total size is correct
	expectedSize := int64(70) // layer2(40) + layer3(30)
	if cache.totalSize != expectedSize {
		t.Errorf("Expected total size %d, got %d", expectedSize, cache.totalSize)
	}
}

func TestLayerCache_MaxSizeUpdate(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Fill cache
	cache.Add("layer1", LayerMetadata{Size: 60})
	cache.Add("layer2", LayerMetadata{Size: 30})

	// Update maxSize to smaller value
	cache.maxSize = 50

	// Add new layer to trigger eviction
	cache.Add("layer3", LayerMetadata{Size: 20})

	// Verify cache adjusted to new limit
	if cache.totalSize > cache.maxSize {
		t.Errorf("Cache size %d exceeds new limit %d", cache.totalSize, cache.maxSize)
	}
}

func TestLayerCache_EvictionPriority(t *testing.T) {
	cache := NewLayerCache(int64(100))

	// Add layers with different access patterns
	cache.Add("frequent", LayerMetadata{Size: 30})
	cache.Add("rare", LayerMetadata{Size: 30})
	cache.Add("medium", LayerMetadata{Size: 30})

	// Access patterns
	for i := 0; i < 10; i++ {
		cache.Get("frequent")
		if i%5 == 0 {
			cache.Get("medium")
		}
	}

	// Add layer to trigger eviction
	cache.Add("new", LayerMetadata{Size: 30})

	// Verify least accessed layer was evicted
	if _, exists := cache.Get("rare"); exists {
		t.Error("Least accessed layer should be evicted first")
	}
	if _, exists := cache.Get("frequent"); !exists {
		t.Error("Most accessed layer should be retained")
	}
}

func TestLayerCache_ConcurrentReadDuringEviction(t *testing.T) {
	cache := NewLayerCache(int64(100))
	const readers = 5

	// Fill cache
	cache.Add("target", LayerMetadata{Size: 90})

	// Start concurrent readers
	var wg sync.WaitGroup
	wg.Add(readers + 1)
	readErrors := make(chan error, readers)

	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, exists := cache.Get("target"); exists {
					// Layer still exists, continue reading
					time.Sleep(time.Microsecond)
				} else {
					// Layer was evicted, stop reading
					return
				}
			}
		}()
	}

	// Trigger eviction while readers are active
	go func() {
		defer wg.Done()
		cache.Add("new", LayerMetadata{Size: 90})
	}()

	wg.Wait()
	close(readErrors)

	// Check for any errors during concurrent reads
	for err := range readErrors {
		if err != nil {
			t.Errorf("Error during concurrent read: %v", err)
		}
	}
}

func TestLayerCache_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	cache := NewLayerCache(int64(1000))
	const (
		goroutines = 10
		operations = 1000
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				switch j % 3 {
				case 0:
					// Add
					cache.Add(fmt.Sprintf("layer-%d-%d", id, j), LayerMetadata{
						Size: int64(j % 100),
					})
				case 1:
					// Get
					cache.Get(fmt.Sprintf("layer-%d-%d", id, j-1))
				case 2:
					// Remove
					cache.Remove(fmt.Sprintf("layer-%d-%d", id, j-2))
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("Stress test completed in %v", duration)
	t.Logf("Final cache size: %d", cache.totalSize)
	t.Logf("Number of layers: %d", len(cache.layers))
}
