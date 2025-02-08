# image-services

A Container Runtime Interface (CRI) compatible image service for container image management.

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

# List images
crictl images

# Remove image
crictl rmi registry.domain.local/app:latest
```

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