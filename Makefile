.PHONY: build run test clean install dev lint tidy

# Variables
BINARY_NAME=openbot
BUILD_DIR=./build
MAIN_PATH=./cmd/openbot
VERSION?=0.1.0
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

# Default target
all: tidy build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

# Run directly
run:
	go run $(MAIN_PATH) $(ARGS)

# Run in dev mode (chat)
dev:
	go run $(MAIN_PATH) chat

# Run tests
test:
	go test ./... -v -race -count=1

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	go clean -cache

# Install to $GOPATH/bin
install:
	go install $(LDFLAGS) $(MAIN_PATH)

# Tidy dependencies
tidy:
	go mod tidy

# Lint (requires golangci-lint)
lint:
	golangci-lint run ./...

# Cross-compile
build-linux:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)

build-darwin:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)

# Initialize config
init:
	go run $(MAIN_PATH) init

# E2E tests (requires running gateway on :8080)
e2e:
	cd e2e && npm test

# Docker
docker-build:
	docker build -t openbot:$(VERSION) .

docker-run:
	docker run --rm -p 8080:8080 -v $(PWD)/config.json:/home/openbot/.openbot/config.json:ro openbot:$(VERSION)

docker-compose:
	docker compose up -d

# Show help
help:
	@echo "OpenBot Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build          Build the binary"
	@echo "  make run ARGS='...' Run with arguments"
	@echo "  make dev            Run interactive chat"
	@echo "  make test           Run Go unit tests"
	@echo "  make e2e            Run Playwright E2E tests"
	@echo "  make clean          Clean build artifacts"
	@echo "  make install        Install to GOPATH/bin"
	@echo "  make tidy           Tidy Go modules"
	@echo "  make lint           Run linter"
	@echo "  make build-linux    Cross-compile for Linux"
	@echo "  make build-darwin   Cross-compile for macOS ARM64"
	@echo "  make docker-build   Build Docker image"
	@echo "  make docker-run     Run in Docker"
	@echo "  make init           Initialize config"
