.PHONY: build run test clean lint fmt tidy docker-build docker-run

# Application info
APP_NAME := go-alert-web3-bnb
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Go commands
GOCMD := go
GOBUILD := $(GOCMD) build
GORUN := $(GOCMD) run
GOTEST := $(GOCMD) test
GOCLEAN := $(GOCMD) clean
GOMOD := $(GOCMD) mod
GOFMT := gofmt

# Build directory
BUILD_DIR := ./bin
MAIN_PATH := ./cmd/server

# Default target
all: lint test build

# Build the application
build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(APP_NAME)"

# Build for Linux (useful for Docker)
build-linux:
	@echo "Building $(APP_NAME) for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(APP_NAME)-linux"

# Run the application
run:
	@echo "Running $(APP_NAME)..."
	$(GORUN) $(MAIN_PATH)/main.go

# Run with custom config
run-config:
	@echo "Running $(APP_NAME) with config..."
	$(GORUN) $(MAIN_PATH)/main.go -config=$(CONFIG)

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -cover ./...

# Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	@echo "Clean complete"

# Run linter
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		golangci-lint run ./...; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .
	@echo "Formatting complete"

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy
	@echo "Tidy complete"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	@echo "Dependencies downloaded"

# Verify dependencies
verify:
	@echo "Verifying dependencies..."
	$(GOMOD) verify
	@echo "Dependencies verified"

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t $(APP_NAME):$(VERSION) .
	docker tag $(APP_NAME):$(VERSION) $(APP_NAME):latest
	@echo "Docker image built: $(APP_NAME):$(VERSION)"

# Docker run
docker-run:
	@echo "Running Docker container..."
	docker run --rm \
		--name $(APP_NAME) \
		--env-file .env \
		$(APP_NAME):latest

# Docker compose up
docker-up:
	docker-compose up -d

# Docker compose down
docker-down:
	docker-compose down

# Generate mocks (requires mockgen)
mocks:
	@echo "Generating mocks..."
	@if command -v mockgen >/dev/null 2>&1; then \
		go generate ./...; \
	else \
		echo "mockgen not installed. Installing..."; \
		go install github.com/golang/mock/mockgen@latest; \
		go generate ./...; \
	fi

# Show help
help:
	@echo "Available targets:"
	@echo "  build         - Build the application"
	@echo "  build-linux   - Build for Linux"
	@echo "  run           - Run the application"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  clean         - Clean build artifacts"
	@echo "  lint          - Run linter"
	@echo "  fmt           - Format code"
	@echo "  tidy          - Tidy dependencies"
	@echo "  deps          - Download dependencies"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run Docker container"
	@echo "  docker-up     - Start with docker-compose"
	@echo "  docker-down   - Stop docker-compose"
	@echo "  help          - Show this help"
