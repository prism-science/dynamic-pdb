DOCKER_COMPOSE ?= $(shell command -v docker-compose >/dev/null 2>&1 && echo docker-compose || echo "docker compose")
GO ?= $(shell command -v go 2>/dev/null || echo /Users/Denis/sdk/go1.26.3/bin/go)
GOCACHE ?= $(abspath ../.tmp/go-build-cache)

LOADTEST_DB ?= dynamic_pdb_loadtest
LOADTEST_ENTRIES ?= 10000
LOADTEST_K6_IMAGE ?= grafana/k6:1.0.0
LOADTEST_PSQL = $(DOCKER_COMPOSE) exec -T postgres psql -U postgres -v ON_ERROR_STOP=1 -q

.PHONY: install-tools generate build test start-postgres destroy-postgres migrate-local loadtest-db loadtest-server loadtest

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

# Recreates LOADTEST_DB from the schema migrations and seeds it with synthetic data.
loadtest-db:
	$(LOADTEST_PSQL) -d postgres -c 'drop database if exists $(LOADTEST_DB) with (force)' -c 'create database $(LOADTEST_DB)'
	@for migration in migrations/db/public/structure/V*.sql; do \
		$(LOADTEST_PSQL) -d $(LOADTEST_DB) < $$migration || exit $$?; \
	done
	$(LOADTEST_PSQL) -d $(LOADTEST_DB) -v entries=$(LOADTEST_ENTRIES) < loadtest/seed.sql

# The per-IP rate limit is lifted because every load-test request comes from one IP.
loadtest-server: build
	DYNAMIC_PDB_ENV=local DYNAMIC_PDB_DB_NAME=$(LOADTEST_DB) DYNAMIC_PDB_SERVER_RATE_LIMIT_REQUESTS=1000000 ./bin/server

# Runs against a server started with loadtest-server. Override scenarios with
# ENDPOINTS, OFFSETS, CONCURRENCY, DURATION_SECONDS and P95_BUDGET_MS.
loadtest:
	docker run --rm -i --add-host=host.docker.internal:host-gateway \
		-e BASE_URL -e ENDPOINTS -e OFFSETS -e CONCURRENCY -e PAGE_LIMIT \
		-e DURATION_SECONDS -e P95_BUDGET_MS -e REQUEST_TIMEOUT \
		$(LOADTEST_K6_IMAGE) run --quiet - < loadtest/lists.js
