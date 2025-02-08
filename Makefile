# Build parameters
BINARY_NAME=cri-image-service
GO=go
GOFMT=gofmt
GOFILES=$(shell find . -name "*.go")

# Build flags
LDFLAGS=-ldflags "-s -w"
BUILD_DIR=build
INSTALL_DIR=/usr/local/bin

.PHONY: all build clean fmt test install uninstall

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/main.go

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@go clean

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -w $(GOFILES)

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v ./...

# Install binary
install: build
	@echo "Installing $(BINARY_NAME)..."
	install -m 755 $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)

# Uninstall binary
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	@rm -f $(INSTALL_DIR)/$(BINARY_NAME)

all: clean fmt test build

# Show help
help:
	@echo "Available targets:"
	@echo "  all        - Clean, format, test and build"
	@echo "  build      - Build the binary"
	@echo "  clean      - Remove build artifacts"
	@echo "  fmt        - Format code"
	@echo "  test       - Run tests"
	@echo "  install    - Install binary"
	@echo "  uninstall  - Remove binary"
