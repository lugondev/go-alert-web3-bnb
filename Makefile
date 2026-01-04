.PHONY: build run test clean lint fmt tidy help

# Application info
APP_NAME := go-alert-web3-bnb
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-w -s -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

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

# Default target
all: lint test build

## Build targets

build: ## Build all binaries
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/settings-ui ./cmd/settings
	@echo "Build complete: $(BUILD_DIR)/"

build-server: ## Build server only
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

build-settings: ## Build settings UI only
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/settings-ui ./cmd/settings

build-linux: ## Build for Linux (cross-compile)
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux ./cmd/server
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/settings-ui-linux ./cmd/settings

## Run targets (local development)

run: ## Run main server
	$(GORUN) ./cmd/server -config configs/config.yaml

run-ui: ## Run settings UI (port 8080)
	$(GORUN) ./cmd/settings -config configs/config.yaml

run-all: ## Run both server and UI in parallel (local)
	@echo "Starting both server and UI..."
	@trap 'kill 0' EXIT; \
	$(GORUN) ./cmd/settings -config configs/config.yaml & \
	$(GORUN) ./cmd/server -config configs/config.yaml & \
	wait

## Test targets

test: ## Run tests
	$(GOTEST) -v -race ./...

test-short: ## Run tests (short mode)
	$(GOTEST) -v -short ./...

test-cover: ## Run tests with coverage
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Code quality

lint: ## Run linter
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		golangci-lint run ./...; \
	fi

fmt: ## Format code
	$(GOFMT) -s -w .

vet: ## Run go vet
	$(GOCMD) vet ./...

## Dependencies

tidy: ## Tidy dependencies
	$(GOMOD) tidy

deps: ## Download dependencies
	$(GOMOD) download

verify: ## Verify dependencies
	$(GOMOD) verify

## Cleanup

clean: ## Clean build artifacts
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

## Help

help: ## Show this help
	@echo "$(APP_NAME) - Development Commands"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "For Docker/deployment operations, use: ./deploy.sh help"
