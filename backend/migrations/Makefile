FLYWAY_IMAGE=flyway/flyway:11

.PHONY: build
build:
	@GOWORK=off go build -trimpath -v -o . ./...

.PHONY: add-migration
add-migration: build
	./migrations add-migration

.PHONY: template
template: build
	./migrations template

.PHONY: migrate-local
migrate-local: build
ifndef GITHUB_WORKSPACE
	$(eval DOCKER_INTERACTIVE_MODE='-it')
endif
	./migrations migrate-params local
	@docker run ${DOCKER_INTERACTIVE_MODE} --rm --network host \
		-v "${CURDIR}"/db:/flyway/sql/db \
		-v "${CURDIR}"/conf:/flyway/conf \
		--env-file flyway.env \
		${FLYWAY_IMAGE} -environment=local migrate
