GO ?= $(shell command -v go 2>/dev/null || echo /Users/Denis/sdk/go1.26.3/bin/go)
GOCACHE ?= $(abspath ../.tmp/go-build-cache)
BIN_DIR := ./bin
BIN := $(BIN_DIR)/dynamic-pdb

.PHONY: build build-linux-amd64 build-linux-arm64 build-x86_64 test

build:
	@mkdir -p "$(BIN_DIR)" "$(GOCACHE)"
	GOCACHE="$(GOCACHE)" $(GO) build -o "$(BIN)" ./cmd/dynamic-pdb

build-linux-amd64:
	@mkdir -p "$(BIN_DIR)" "$(GOCACHE)"
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOCACHE="$(GOCACHE)" $(GO) build -o "$(BIN)" ./cmd/dynamic-pdb

build-linux-arm64:
	@mkdir -p "$(BIN_DIR)" "$(GOCACHE)"
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOCACHE="$(GOCACHE)" $(GO) build -o "$(BIN)" ./cmd/dynamic-pdb

build-x86_64: build-linux-amd64

test:
	@mkdir -p "$(GOCACHE)"
	GOCACHE="$(GOCACHE)" $(GO) test ./...
