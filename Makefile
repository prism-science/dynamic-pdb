GO ?= $(shell command -v go 2>/dev/null || echo /Users/Denis/sdk/go1.26.3/bin/go)
GO_BIN_DIR := $(dir $(GO))
GOCACHE ?= $(CURDIR)/.tmp/go-build-cache
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.tmp/golangci-lint-cache
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo $(shell $(GO) env GOPATH)/bin/golangci-lint)

# Modules in the go.work workspace. golangci-lint runs once per module because
# each is a separate Go module.
LINT_MODULES := backend cli integration

.PHONY: integration-test lint lint-fix install-lint-tools ensure-lint-tools

integration-test:
	@mkdir -p "$(GOCACHE)"
	DYNAMIC_PDB_RUN_INTEGRATION=1 GOCACHE="$(GOCACHE)" $(GO) test ./integration -count=1 -timeout=5m -v

install-lint-tools:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

ensure-lint-tools:
	@if ! command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1; then \
		echo "installing golangci-lint"; \
		$(MAKE) install-lint-tools; \
	fi

lint: ensure-lint-tools
	@mkdir -p "$(GOCACHE)" "$(GOLANGCI_LINT_CACHE)"
	@for module in $(LINT_MODULES); do \
		echo "==> golangci-lint: $$module"; \
		(cd $$module && PATH="$(GO_BIN_DIR):$$PATH" GOCACHE="$(GOCACHE)" GOLANGCI_LINT_CACHE="$(GOLANGCI_LINT_CACHE)" "$(GOLANGCI_LINT)" run ./...) || exit $$?; \
	done

lint-fix: ensure-lint-tools
	@mkdir -p "$(GOCACHE)" "$(GOLANGCI_LINT_CACHE)"
	@for module in $(LINT_MODULES); do \
		echo "==> golangci-lint --fix: $$module"; \
		(cd $$module && PATH="$(GO_BIN_DIR):$$PATH" GOCACHE="$(GOCACHE)" GOLANGCI_LINT_CACHE="$(GOLANGCI_LINT_CACHE)" "$(GOLANGCI_LINT)" run --fix ./...) || exit $$?; \
	done
