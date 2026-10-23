FLYWAY_IMAGE=flyway/flyway:11
GO ?= $(shell command -v go 2>/dev/null || echo /Users/Denis/sdk/go1.26.3/bin/go)
GOCACHE ?= $(abspath ../../.tmp/go-build-cache)
DOCKER_INTERACTIVE_MODE ?= $(shell test -t 0 && echo -it)

.PHONY: build
build:
	@mkdir -p "$(GOCACHE)"
	@GOWORK=off GOCACHE="$(GOCACHE)" $(GO) build -trimpath -v -o . ./...

.PHONY: add-migration
add-migration: build
	./migrations add-migration

.PHONY: template
template: build
	./migrations template

.PHONY: migrate-local
migrate-local: build
	./migrations migrate-params local
	@docker run ${DOCKER_INTERACTIVE_MODE} --rm --network host \
		-v "${CURDIR}"/db:/flyway/sql/db \
		-v "${CURDIR}"/conf:/flyway/conf \
		--env-file flyway.env \
		${FLYWAY_IMAGE} -environment=local migrate

# Migration rehearsal on a temporary copy of production; see rehearsal/README.md.
# BRANCH is the git ref whose migrations to run; TARGET is aws (the copy of
# production) or local (the practice copy of the local dev database).
REHEARSAL := ./rehearsal/rehearsal.sh
TARGET ?= aws

.PHONY: rehearsal-up rehearsal-run rehearsal-down rehearsal-status rehearsal-local-up rehearsal-local-down
rehearsal-up:
	$(REHEARSAL) up

rehearsal-run:
	$(REHEARSAL) run --target "$(TARGET)" --branch "$(BRANCH)"

rehearsal-down:
	$(REHEARSAL) down

rehearsal-status:
	$(REHEARSAL) status

rehearsal-local-up:
	$(REHEARSAL) local-up

rehearsal-local-down:
	$(REHEARSAL) local-down
