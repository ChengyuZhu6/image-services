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
