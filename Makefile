# Variables
APP_NAME := vm-proxy-auth
DOCKER_IMAGE := edelwud/$(APP_NAME)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date +%Y-%m-%dT%H:%M:%S%z)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT := gofumpt

# Build flags
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT) -w -s"
BUILD_FLAGS := -a -installsuffix cgo

# Paths
BINARY_PATH := ./bin/$(APP_NAME)
CMD_PATH := ./cmd/gateway
CONFIG_PATH := ./config.example.yaml

.PHONY: all build clean test coverage deps fmt lint vet security vuln-check quality docker docker-build docker-push run dev help

# Default target
all: clean deps test build

# Build the application
build:
	@echo "Building $(APP_NAME)..."
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o $(BINARY_PATH) $(CMD_PATH)
	@echo "Built $(BINARY_PATH)"

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	mkdir -p bin
	# Linux AMD64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o bin/$(APP_NAME)-linux-amd64 $(CMD_PATH)
	# Linux ARM64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o bin/$(APP_NAME)-linux-arm64 $(CMD_PATH)
	# Darwin AMD64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o bin/$(APP_NAME)-darwin-amd64 $(CMD_PATH)
	# Darwin ARM64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o bin/$(APP_NAME)-darwin-arm64 $(CMD_PATH)
	# Windows AMD64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o bin/$(APP_NAME)-windows-amd64.exe $(CMD_PATH)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf bin/
	docker image prune -f

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	$(GOTEST) -v -race ./...

# Benchmark tests
benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -l -w -d ./..

# Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Run go vet
vet:
	@echo "Running go vet..."
	$(GOCMD) vet ./...

# Security scan (requires gosec)
security:
	@echo "Running security scan..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not installed. Install with: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest"; \
	fi

# Validate config
validate-config:
	@echo "Validating configuration..."
	@if [ -f $(BINARY_PATH) ]; then \
		$(BINARY_PATH) --validate-config --config $(CONFIG_PATH); \
	else \
		echo "Binary not found. Run 'make build' first."; \
	fi

# Run the application locally
run: build
	@echo "Running $(APP_NAME)..."
	$(BINARY_PATH) --config $(CONFIG_PATH)

# Run in development mode with auto-rebuild
dev:
	@echo "Starting development mode..."
	@if command -v air >/dev/null 2>&1; then \
		air; \
	else \
		echo "air not installed. Install with: go install github.com/cosmtrek/air@latest"; \
		echo "Falling back to normal run..."; \
		make run; \
	fi

# Docker targets
docker: docker-build

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest

# Push Docker image
docker-push: docker-build
	@echo "Pushing Docker image..."
	docker push $(DOCKER_IMAGE):$(VERSION)
	docker push $(DOCKER_IMAGE):latest

# Run with Docker Compose
docker-compose-up:
	@echo "Starting services with Docker Compose..."
	docker-compose up -d

# Stop Docker Compose services
docker-compose-down:
	@echo "Stopping services..."
	docker-compose down

# View Docker Compose logs
docker-compose-logs:
	docker-compose logs -f vm-proxy-auth

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	go install github.com/cosmtrek/air@latest

# Generate mocks (if using mockery)
mocks:
	@echo "Generating mocks..."
	@if command -v mockery >/dev/null 2>&1; then \
		mockery --all --output ./mocks; \
	else \
		echo "mockery not installed. Install with: go install github.com/vektra/mockery/v2@latest"; \
	fi

# Create release
release: clean deps test build-all
	@echo "Creating release $(VERSION)..."
	mkdir -p release
	cp bin/* release/
	cd release && \
	for binary in *; do \
		tar -czf $$binary.tar.gz $$binary; \
		rm $$binary; \
	done

# Check dependencies for vulnerabilities
vuln-check:
	@echo "Checking for vulnerabilities..."
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "govulncheck not installed. Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
	fi

# Comprehensive quality check - ALWAYS run before commit
quality: fmt lint vet security vuln-check test-race
	@echo "✅ All quality checks passed! Ready to commit."

# Enhanced clean - remove all generated files
clean-all: clean
	@echo "Deep cleaning..."
	rm -rf coverage.out coverage.html lint_output.json
	rm -rf .air_tmp_*
	find . -name "*.test" -delete
	find . -name "*.prof" -delete

# Help
help:
	@echo "Available targets:"
	@echo "  build          - Build the application"
	@echo "  build-all      - Build for multiple platforms"
	@echo "  clean          - Clean build artifacts"
	@echo "  test           - Run tests"
	@echo "  coverage       - Run tests with coverage"
	@echo "  test-race      - Run tests with race detector"
	@echo "  benchmark      - Run benchmark tests"
	@echo "  deps           - Download dependencies"
	@echo "  fmt            - Format code"
	@echo "  lint           - Run linter"
	@echo "  vet            - Run go vet"
	@echo "  security       - Run security scan"
	@echo "  validate-config- Validate configuration"
	@echo "  run            - Run the application"
	@echo "  dev            - Run in development mode"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-push    - Push Docker image"
	@echo "  docker-compose-up   - Start with Docker Compose"
	@echo "  docker-compose-down - Stop Docker Compose services"
	@echo "  install-tools  - Install development tools"
	@echo "  release        - Create release artifacts"
	@echo "  vuln-check     - Check for vulnerabilities"
	@echo "  quality        - ⭐ COMPREHENSIVE quality check (fmt+lint+test+security)"
	@echo "  clean-all      - Deep clean (removes all generated files)"
	@echo "  help           - Show this help"
