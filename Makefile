# Go1090 ADS-B Beast Mode Decoder Makefile

# Version info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS := -X go1090/internal/app.Version=$(VERSION) -X go1090/internal/app.BuildTime=$(BUILD_TIME) -X go1090/internal/app.GitCommit=$(GIT_COMMIT)
BUILD_FLAGS := -ldflags "$(LDFLAGS)"

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Binary names
BINARY_NAME := go1090
BINARY_UNIX := $(BINARY_NAME)_unix
BINARY_WINDOWS := $(BINARY_NAME).exe

# Directories
DIST_DIR := dist
LOG_DIR := logs

.PHONY: all build clean test deps help run install uninstall
.PHONY: build-linux build-darwin build-windows build-all
.PHONY: release release-prep check-deps
.PHONY: docker-build docker-run

# Default target
all: clean deps test build

help: ## Show this help message
	@echo "Go1090 ADS-B Beast Mode Decoder"
	@echo "================================"
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Dependencies and setup
deps: ## Download and install dependencies
	$(GOMOD) download
	$(GOMOD) tidy

# Function to get CGO flags
define get_cgo_flags
	$(shell if pkg-config --exists librtlsdr 2>/dev/null; then \
		echo "CGO_CFLAGS=\"$$(pkg-config --cflags librtlsdr)\" CGO_LDFLAGS=\"$$(pkg-config --libs librtlsdr)\""; \
	elif [ -f "/opt/homebrew/include/rtl-sdr.h" ]; then \
		echo "CGO_CFLAGS=\"-I/opt/homebrew/include\" CGO_LDFLAGS=\"-L/opt/homebrew/lib\""; \
	elif [ -f "/usr/local/include/rtl-sdr.h" ]; then \
		echo "CGO_CFLAGS=\"-I/usr/local/include\" CGO_LDFLAGS=\"-L/usr/local/lib\""; \
	elif [ -f "/usr/include/rtl-sdr.h" ]; then \
		echo "CGO_CFLAGS=\"-I/usr/include\" CGO_LDFLAGS=\"-L/usr/lib\""; \
	else \
		echo ""; \
	fi)
endef

check-deps: ## Check if required dependencies are installed
	@echo "Checking dependencies..."
	@which pkg-config >/dev/null || (echo "ERROR: pkg-config not found. Install it first." && exit 1)
	@echo "Checking for librtlsdr..."
	@if pkg-config --exists librtlsdr; then \
		echo "✅ Found librtlsdr via pkg-config"; \
	elif [ -f "/opt/homebrew/include/rtl-sdr.h" ]; then \
		echo "✅ Found librtlsdr at /opt/homebrew (Apple Silicon)"; \
	elif [ -f "/usr/local/include/rtl-sdr.h" ]; then \
		echo "✅ Found librtlsdr at /usr/local (Intel Homebrew/Linux)"; \
	elif [ -f "/usr/include/rtl-sdr.h" ]; then \
		echo "✅ Found librtlsdr at /usr (system installation)"; \
	else \
		echo "❌ ERROR: librtlsdr not found!"; \
		echo "Please install librtlsdr:"; \
		echo "  macOS: brew install librtlsdr"; \
		echo "  Ubuntu/Debian: sudo apt-get install librtlsdr-dev pkg-config"; \
		echo "  CentOS/RHEL/Fedora: sudo yum install rtl-sdr-devel pkgconfig"; \
		exit 1; \
	fi
	@echo "CGO flags will be set automatically during build"

# Building
build: check-deps ## Build the binary for current platform
	@echo "Building $(BINARY_NAME) v$(VERSION)..."
	$(call get_cgo_flags) $(GOBUILD) $(BUILD_FLAGS) -o $(BINARY_NAME) ./cmd/go1090
	@echo "✅ Build complete: $(BINARY_NAME)"

build-static: check-deps ## Build static binary (Linux only)
	@echo "Building static $(BINARY_NAME) v$(VERSION)..."
	CGO_ENABLED=1 $(GOBUILD) $(BUILD_FLAGS) -ldflags "$(LDFLAGS) -linkmode external -extldflags '-static'" -o $(BINARY_NAME) ./cmd/go1090
	@echo "✅ Static build complete: $(BINARY_NAME)"

# Cross-platform builds
build-linux: ## Build for Linux (amd64)
	@echo "Building for Linux..."
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/go1090

build-linux-arm64: ## Build for Linux ARM64
	@echo "Building for Linux ARM64..."
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/go1090

build-darwin: ## Build for macOS
	@echo "Building for macOS..."
	mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/go1090
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/go1090

build-windows: ## Build for Windows (no CGO)
	@echo "Building for Windows (limited RTL-SDR support)..."
	mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/go1090

build-all: build-linux build-linux-arm64 build-darwin build-windows ## Build for all platforms

# Testing
test: ## Run all tests
	$(GOTEST) -v ./...

test-unit: ## Run unit tests only
	$(GOTEST) -v -run "^Test.*(?:Beast|BaseStation|LogRotator).*" ./...

test-integration: ## Run integration tests only
	$(GOTEST) -v -run "^TestIntegration" ./...

test-coverage: ## Run tests with coverage report
	$(GOTEST) -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

test-race: ## Run tests with race detection
	$(GOTEST) -race -v ./...

test-short: ## Run short tests only
	$(GOTEST) -short -v ./...

test-verbose: ## Run tests with verbose output
	$(GOTEST) -v -x ./...

test-bench: ## Run benchmarks
	$(GOTEST) -bench=. -benchmem ./...

test-profile: ## Run tests with CPU and memory profiling
	$(GOTEST) -cpuprofile=cpu.prof -memprofile=mem.prof -bench=. ./...

test-timeout: ## Run tests with timeout
	$(GOTEST) -timeout=30s -v ./...

# Development
run: build ## Build and run the application
	./$(BINARY_NAME) --verbose

run-help: build ## Show application help
	./$(BINARY_NAME) --help

run-version: build ## Show version information
	./$(BINARY_NAME) --version

# Installation
install: build ## Install binary to $GOPATH/bin or $HOME/go/bin
	@echo "Installing $(BINARY_NAME)..."
	@if [ -n "$(GOPATH)" ]; then \
		cp $(BINARY_NAME) $(GOPATH)/bin/; \
		echo "✅ Installed to $(GOPATH)/bin/$(BINARY_NAME)"; \
	elif [ -d "$(HOME)/go/bin" ]; then \
		cp $(BINARY_NAME) $(HOME)/go/bin/; \
		echo "✅ Installed to $(HOME)/go/bin/$(BINARY_NAME)"; \
	else \
		echo "❌ Could not find Go bin directory. Set GOPATH or create $(HOME)/go/bin"; \
		exit 1; \
	fi

uninstall: ## Remove installed binary
	@echo "Uninstalling $(BINARY_NAME)..."
	@if [ -n "$(GOPATH)" ] && [ -f "$(GOPATH)/bin/$(BINARY_NAME)" ]; then \
		rm $(GOPATH)/bin/$(BINARY_NAME); \
		echo "✅ Removed from $(GOPATH)/bin/"; \
	elif [ -f "$(HOME)/go/bin/$(BINARY_NAME)" ]; then \
		rm $(HOME)/go/bin/$(BINARY_NAME); \
		echo "✅ Removed from $(HOME)/go/bin/"; \
	else \
		echo "❌ $(BINARY_NAME) not found in Go bin directories"; \
	fi

# Release management
release-prep: clean deps test build-all ## Prepare for release (build all platforms)
	@echo "Release preparation complete for version $(VERSION)"
	@echo "Built binaries:"
	@ls -la $(DIST_DIR)/

release: release-prep ## Create a release (requires git tag)
	@echo "Creating release for version $(VERSION)..."
	@if [ "$(VERSION)" = "dev" ]; then \
		echo "❌ Cannot release 'dev' version. Create a git tag first:"; \
		echo "   git tag v1.0.0"; \
		echo "   git push origin v1.0.0"; \
		exit 1; \
	fi
	@echo "✅ Release ready for $(VERSION)"
	@echo "Push the tag to trigger GitHub Actions:"
	@echo "   git push origin $(VERSION)"

# Cleanup
clean: ## Clean build artifacts
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)
	rm -f $(BINARY_WINDOWS)
	rm -rf $(DIST_DIR)
	rm -f coverage.out coverage.html

clean-logs: ## Clean log files
	rm -rf $(LOG_DIR)/*.log*
	@echo "✅ Log files cleaned"

clean-all: clean clean-logs ## Clean everything

# Docker support (future)
docker-build: ## Build Docker image
	@echo "Docker support coming soon..."

docker-run: ## Run in Docker container
	@echo "Docker support coming soon..."

# Development helpers
fmt: ## Format Go code
	$(GOCMD) fmt ./...

vet: ## Run go vet
	$(GOCMD) vet ./...

lint: ## Run golint (requires golint)
	@if command -v golint >/dev/null 2>&1; then \
		golint ./...; \
	else \
		echo "golint not installed. Install with: go install golang.org/x/lint/golint@latest"; \
	fi

check: fmt vet lint test ## Run all checks

# Information
info: ## Show build information
	@echo "Go1090 Build Information"
	@echo "======================="
	@echo "Version:     $(VERSION)"
	@echo "Build Time:  $(BUILD_TIME)"
	@echo "Git Commit:  $(GIT_COMMIT)"
	@echo "Go Version:  $(shell $(GOCMD) version)"
	@echo "Platform:    $(shell $(GOCMD) env GOOS)/$(shell $(GOCMD) env GOARCH)"

size: build ## Show binary size
	@echo "Binary size:"
	@ls -lh $(BINARY_NAME) | awk '{print $$5 "\t" $$9}'

# Quick development cycle
dev: clean deps test build run-version ## Quick development cycle 