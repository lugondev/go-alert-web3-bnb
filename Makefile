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

build: ## Build the unified binary
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/app
	@echo "Build complete: $(BUILD_DIR)/$(APP_NAME)"

build-wstest: ## Build WebSocket test tool
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/wstest ./cmd/wstest

build-linux: ## Build for Linux (cross-compile)
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux ./cmd/app

## Run targets (local development)

run: ## Run with both server and UI enabled (default)
	$(GORUN) ./cmd/app -config configs/config.local.yaml

run-reset: ## Run with both server and UI enabled (default)
	$(GORUN) ./cmd/app -config configs/config.local.yaml -reset

run-server: ## Run server only (no UI)
	$(GORUN) ./cmd/app -config configs/config.yaml -server

run-ui: ## Run UI only (no server)
	$(GORUN) ./cmd/app -config configs/config.yaml -ui

run-local: ## Run with local config (both enabled)
	$(GORUN) ./cmd/app -config configs/config.local.yaml

run-local-reset: ## Run with local config and reset Redis settings
	$(GORUN) ./cmd/app -config configs/config.local.yaml -reset

run-wstest: ## Run WebSocket test tool
	$(GORUN) ./cmd/wstest

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
	@echo "Flags for the application:"
	@echo "  -server      Enable WebSocket alert server"
	@echo "  -ui          Enable settings web UI"
	@echo "  -port        Web UI port (default: 8080)"
	@echo "  -reset       Reset all settings in Redis"
	@echo "  -debug       Enable debug mode"
	@echo ""
	@echo "Note: If no -server or -ui flag is specified, both are enabled by default."
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "For Docker/deployment operations, use: ./deploy.sh help"
