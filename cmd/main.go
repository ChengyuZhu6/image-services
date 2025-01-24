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

	"cri-image-service/pkg/server"

	"google.golang.org/grpc"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	listen = "unix:///var/run/cri-image.sock"
)

func main() {
	// Remove unix socket prefix
	endpoint := listen[7:]

	// Create unix socket listener
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// Create gRPC server
	s := grpc.NewServer()

	// Register image service
	imageServer := server.NewImageServer()
	runtime.RegisterImageServiceServer(s, imageServer)

	fmt.Printf("Starting CRI image service on %s\n", listen)
	if err := s.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
