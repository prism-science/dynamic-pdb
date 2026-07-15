DOCKER_COMPOSE ?= $(shell command -v docker-compose >/dev/null 2>&1 && echo docker-compose || echo "docker compose")
GO ?= $(shell command -v go 2>/dev/null || echo /Users/Denis/sdk/go1.26.3/bin/go)
GOCACHE ?= $(abspath ../.tmp/go-build-cache)

.PHONY: install-tools generate build test start-postgres destroy-postgres migrate-local

install-tools:
	$(MAKE) -C tools install-tools

generate:
	$(GO) generate ./...

build:
	@mkdir -p bin "$(GOCACHE)"
	GOCACHE="$(GOCACHE)" $(GO) build -o bin/server ./cmd/server

test:
	@mkdir -p "$(GOCACHE)"
	GOCACHE="$(GOCACHE)" $(GO) test ./...

start-postgres:
	$(DOCKER_COMPOSE) up -d postgres --wait

destroy-postgres:
	$(DOCKER_COMPOSE) rm -s -v -f postgres

migrate-local:
	$(MAKE) -C migrations migrate-local
