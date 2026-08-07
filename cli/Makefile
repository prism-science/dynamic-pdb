GO ?= $(shell command -v go 2>/dev/null || echo /Users/Denis/sdk/go1.26.3/bin/go)
GOCACHE ?= $(abspath ../.tmp/go-build-cache)
BIN_DIR := ./bin
BIN := $(BIN_DIR)/dynamic-pdb

.PHONY: build test

build:
	@mkdir -p "$(BIN_DIR)" "$(GOCACHE)"
	GOCACHE="$(GOCACHE)" $(GO) build -o "$(BIN)" ./cmd/dynamic-pdb

test:
	@mkdir -p "$(GOCACHE)"
	GOCACHE="$(GOCACHE)" $(GO) test ./...

