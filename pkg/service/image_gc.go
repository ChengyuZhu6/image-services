package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type GarbageCollector struct {
	imageService *ImageService
	interval     time.Duration
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

func NewGarbageCollector(imageService *ImageService, interval time.Duration) *GarbageCollector {
	return &GarbageCollector{
		imageService: imageService,
		interval:     interval,
		stopCh:       make(chan struct{}),
	}
}

func (gc *GarbageCollector) Start() {
	gc.wg.Add(1)
	go gc.run()
}

func (gc *GarbageCollector) Stop() {
	close(gc.stopCh)
	gc.wg.Wait()
}

func (gc *GarbageCollector) run() {
	defer gc.wg.Done()
	ticker := time.NewTicker(gc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-gc.stopCh:
			return
		case <-ticker.C:
			if err := gc.collectGarbage(); err != nil {
				fmt.Printf("Garbage collection failed: %v\n", err)
			}
		}
	}
}

func (gc *GarbageCollector) collectGarbage() error {
	fmt.Println("Starting garbage collection...")

	// Get all layer files in the image root
	layerFiles := make(map[string]bool)
	err := filepath.Walk(gc.imageService.imageRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Base(path) == "layer.tar" {
			layerFiles[path] = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk image directory: %v", err)
	}

	// Get all layers referenced by images
	gc.imageService.mu.RLock()
	referencedLayers := make(map[string]bool)
	for _, img := range gc.imageService.images {
		for _, layer := range img.Layers {
			referencedLayers[layer.Path] = true
		}
	}
	gc.imageService.mu.RUnlock()

	// Remove unreferenced layer files
	var removed int
	for path := range layerFiles {
		if !referencedLayers[path] {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				fmt.Printf("Failed to remove unreferenced layer %s: %v\n", path, err)
				continue
			}
			removed++
		}
	}

	fmt.Printf("Garbage collection completed: removed %d unreferenced layers\n", removed)
	return nil
}
