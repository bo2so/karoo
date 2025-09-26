# Karoo - Stratum V1 Proxy (Go)
# Build automation for Stratum V1 proxy server

# Variables
GO		= go
BIN		= karoo
SRC		= ./cmd/karoo
BUILD_DIR	= ./bin
VERSION		= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_TIME	= $(shell date +%Y-%m-%dT%H:%M:%S%z)
LDFLAGS		= -s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

# Install paths
INSTALL_BIN_DIR	?= /usr/local/bin
INSTALL_CONFIG_DIR ?= /etc/karoo
CONFIG_SOURCE	?= config.example.json
CONFIG_DEST	= $(INSTALL_CONFIG_DIR)/config.json
DOCKER_IMAGE	= karoo:latest
SYSTEMD_PATH	= /etc/systemd/system/karoo.service

# Default target - show help
.DEFAULT_GOAL := help

.PHONY: help all build clean run test fmt vet lint docker install systemd deps mod-tidy info

help:	## Show this help
	@echo "Karoo - Stratum V1 Proxy (Go)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

all: clean fmt vet build	## Clean, format, vet and build

build:	## Build the project
	@echo "Building $(BIN)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -tags netgo -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BIN) $(SRC)

run: build	## Build and run with config.json
	@echo "Running $(BIN)..."
	@$(BUILD_DIR)/$(BIN) -config ./config.json

test:	## Run tests
	$(GO) test -v ./...

fmt:	## Format Go code
	$(GO) fmt ./...

vet:	## Run go vet
	$(GO) vet ./...

lint:	## Run golangci-lint (requires installation)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

deps:	## Download dependencies
	$(GO) mod download

mod-tidy:	## Tidy and verify dependencies
	$(GO) mod tidy
	$(GO) mod verify

clean:	## Remove build artifacts
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@$(GO) clean -cache -testcache -modcache 2>/dev/null || true

docker:	## Build Docker image
	@echo "Building Docker image $(DOCKER_IMAGE)..."
	@docker build -t $(DOCKER_IMAGE) .

install: build	## Install binary and config
	@echo "Installing binary to $(INSTALL_BIN_DIR)..."
	@install -d $(INSTALL_BIN_DIR)
	@install -m 755 $(BUILD_DIR)/$(BIN) $(INSTALL_BIN_DIR)/$(BIN)
	@echo "Installing configuration to $(INSTALL_CONFIG_DIR)..."
	@install -d $(INSTALL_CONFIG_DIR)
	@if [ -f $(CONFIG_DEST) ]; then \
		install -m 644 $(CONFIG_SOURCE) $(CONFIG_DEST).example; \
		echo "Config preserved; example at $(CONFIG_DEST).example"; \
	else \
		install -m 644 $(CONFIG_SOURCE) $(CONFIG_DEST); \
	fi

systemd: install	## Install systemd unit
	@echo "Installing systemd unit at $(SYSTEMD_PATH)..."
	@install -m 644 karoo.service $(SYSTEMD_PATH)
	@systemctl daemon-reload
	@systemctl enable karoo
	@echo "Use 'systemctl start karoo' to start the service"

info:	## Show project information
	@echo "Project: Karoo Stratum V1 Proxy"
	@echo "Binary: $(BIN)"
	@echo "Source: $(SRC)"
	@echo "Build: $(BUILD_DIR)"
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Go version: $$($(GO) version)"
