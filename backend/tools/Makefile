GO ?= $(shell command -v go 2>/dev/null || echo /Users/Denis/sdk/go1.26.3/bin/go)

.PHONY: install-tools

install-tools:
	@GOWORK=off $(GO) install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
