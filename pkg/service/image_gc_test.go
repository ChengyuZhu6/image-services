package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGarbageCollector(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gc-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test service
	service := &ImageService{
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(int64(100)),
	}

	// Create test layers
	layers := []struct {
		path      string
		reference bool
	}{
		{filepath.Join(tmpDir, "layer1", "layer.tar"), true},  // Referenced layer
		{filepath.Join(tmpDir, "layer2", "layer.tar"), false}, // Unreferenced layer
	}

	for _, layer := range layers {
		if err := os.MkdirAll(filepath.Dir(layer.path), 0755); err != nil {
			t.Fatalf("Failed to create layer directory: %v", err)
		}
		if err := os.WriteFile(layer.path, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create layer file: %v", err)
		}
	}

	// Add referenced layer to image metadata
	service.images["test-image"] = &imageMetadata{
		Layers: []LayerMetadata{
			{Path: layers[0].path},
		},
	}

	// Create and start garbage collector with short interval
	gc := NewGarbageCollector(service, 100*time.Millisecond)
	gc.Start()
	defer gc.Stop()

	// Wait for garbage collection to run
	time.Sleep(200 * time.Millisecond)

	// Verify referenced layer still exists
	if _, err := os.Stat(layers[0].path); err != nil {
		t.Errorf("Referenced layer was incorrectly removed: %v", err)
	}

	// Verify unreferenced layer was removed
	if _, err := os.Stat(layers[1].path); !os.IsNotExist(err) {
		t.Error("Unreferenced layer was not removed")
	}
}

func TestGarbageCollectorInAction(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gc-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test service
	service := &ImageService{
		imageRoot:    tmpDir,
		images:       make(map[string]*imageMetadata),
		metadataFile: filepath.Join(tmpDir, "metadata.json"),
		layerCache:   NewLayerCache(int64(100)),
	}

	// Create some test data (10MB each)
	testData := make([]byte, 10*1024*1024)
	for i := 0; i < len(testData); i++ {
		testData[i] = byte(i % 256)
	}

	// Create test layers
	layers := []struct {
		path      string
		reference bool
	}{
		{filepath.Join(tmpDir, "layer1", "layer.tar"), true},  // Referenced layer
		{filepath.Join(tmpDir, "layer2", "layer.tar"), false}, // Unreferenced layer
		{filepath.Join(tmpDir, "layer3", "layer.tar"), false}, // Unreferenced layer
	}

	for _, layer := range layers {
		if err := os.MkdirAll(filepath.Dir(layer.path), 0755); err != nil {
			t.Fatalf("Failed to create layer directory: %v", err)
		}
		if err := os.WriteFile(layer.path, testData, 0644); err != nil {
			t.Fatalf("Failed to create layer file: %v", err)
		}
	}

	// Add referenced layer to image metadata
	service.images["test-image"] = &imageMetadata{
		Layers: []LayerMetadata{
			{Path: layers[0].path},
		},
	}

	// Create and start garbage collector with short interval
	gc := NewGarbageCollector(service, 100*time.Millisecond)
	gc.Start()
	defer gc.Stop()

	// Wait for garbage collection to run
	time.Sleep(200 * time.Millisecond)

	// Check GC stats
	stats := gc.GetStats()
	if stats.TotalLayersRemoved != 2 {
		t.Errorf("Expected 2 layers to be removed, got %d", stats.TotalLayersRemoved)
	}
	if stats.LastCollectionSize != 20*1024*1024 { // 2 layers * 10MB
		t.Errorf("Expected 20MB to be removed, got %.2f MB",
			float64(stats.LastCollectionSize)/1024/1024)
	}

	// Verify disk space was actually freed
	var totalSize int64
	err = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk directory: %v", err)
	}

	// Should only have one 10MB layer left
	expectedSize := int64(10 * 1024 * 1024)
	if totalSize != expectedSize {
		t.Errorf("Expected %d bytes remaining, got %d", expectedSize, totalSize)
	}
}
