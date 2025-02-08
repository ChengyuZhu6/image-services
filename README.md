# image-services

A Container Runtime Interface (CRI) compatible image service for container image management.

## Features

### Image Management
- **Layer Caching**: Efficiently caches image layers to avoid redundant downloads
- **Garbage Collection**: Automatically removes unused layers to optimize storage
- **Concurrent Operations**: Supports parallel image operations with thread safety
- **Registry Authentication**: Handles private registry authentication
- **Layer Deduplication**: Reuses identical layers across different images
- **Compressed Layer Support**: Handles gzipped tar layers with automatic decompression
- **Metadata Persistence**: Maintains image and layer metadata across service restarts

## Quick Start

### Build
```bash
make # or make build
```

### Install
```bash
sudo make install
```

### Test
```bash
make test
```

## Usage with crictl

1. Configure crictl:
```bash
cat > /etc/crictl.yaml <<EOF
runtime-endpoint: ""  # Empty since we only provide image service
image-endpoint: unix:///var/run/cri-image.sock
timeout: 10
debug: false
EOF
```

2. Basic operations:
```bash
# Pull image
crictl pull registry.domain.local/app:latest

# Pull from private registry
crictl pull --creds username:password registry.domain.local/private/app:latest

# Pull using docker config credentials
crictl pull --auth ~/.docker/config.json registry.domain.local/private/app:latest

# List images
crictl images

# Remove image
crictl rmi registry.domain.local/app:latest
```

Note: For private registries, you can either:
- Use `--creds` flag to provide username and password directly
- Use `--auth` flag to specify path to docker config file containing registry credentials
- Configure default credentials in `~/.docker/config.json`

## Service Management

Start service:
```bash
sudo /usr/local/bin/cri-image-service
```

Stop service:
```bash
# Use Ctrl+C if running in foreground
# Or send SIGTERM if running in background:
kill -TERM $(pgrep cri-image-service)
```

Note: Always use SIGTERM to stop the service gracefully.

## Development

```bash
# Format code
make fmt

# Clean build artifacts
make clean

# Run all tests
make test
```

## License

Apache License 2.0