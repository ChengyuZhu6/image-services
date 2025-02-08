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

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"cri-image-service/pkg/server"

	"google.golang.org/grpc"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	listen = "unix:///var/run/cri-image.sock"
)

func setupSocket(socketPath string) (net.Listener, func(), error) {
	// Clean up existing socket file
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("failed to remove existing socket: %v", err)
	}

	// Create unix socket listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen: %v", err)
	}

	// Create cleanup function
	cleanup := func() {
		listener.Close()
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to cleanup socket: %v", err)
		}
	}

	return listener, cleanup, nil
}

func main() {
	// Remove unix socket prefix
	endpoint := listen[7:]

	// Setup socket and get cleanup function
	listener, cleanup, err := setupSocket(endpoint)
	if err != nil {
		log.Fatalf("Failed to setup socket: %v", err)
	}
	defer cleanup()

	// Create gRPC server
	s := grpc.NewServer()

	// Register image service
	imageServer := server.NewImageServer()
	runtime.RegisterImageServiceServer(s, imageServer)

	// Setup signal handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server
	fmt.Printf("Starting CRI image service on %s\n", listen)
	go s.Serve(listener)

	// Wait for interrupt
	<-stop
	fmt.Println("\nShutting down...")

	// Stop server
	s.GracefulStop()
	fmt.Println("Server stopped")
}
