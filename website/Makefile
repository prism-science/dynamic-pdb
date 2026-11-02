NPM ?= npm
DOCKER_COMPOSE ?= $(shell command -v docker-compose >/dev/null 2>&1 && echo docker-compose || echo "docker compose")

BROWSER_STACK_ENV := e2e/stack/stack.env
include $(BROWSER_STACK_ENV)
BROWSER_COMPOSE = $(DOCKER_COMPOSE) --env-file $(BROWSER_STACK_ENV) -f e2e/stack/compose.yml

.PHONY: test typecheck build dev browser-stack browser-stack-down

test:
	$(NPM) test

typecheck:
	$(NPM) run typecheck

build:
	$(NPM) run build

dev:
	$(NPM) run dev

# Prepares everything `npm run test:browser` runs against: Postgres and the S3
# stand-in, a migrated and seeded database, the backend binary and a production
# build of the website. Run it again after changing backend or website code.
browser-stack:
	$(BROWSER_COMPOSE) up -d --wait postgres s3
	$(BROWSER_COMPOSE) run --rm migrate
	$(BROWSER_COMPOSE) exec -T postgres psql -U $(BROWSER_DB_USER) -d $(BROWSER_DB_NAME) -v ON_ERROR_STOP=1 -q \
		-v submitter_id=$(BROWSER_SUBMITTER_ID) -v reviewer_id=$(BROWSER_REVIEWER_ID) < e2e/stack/seed.sql
	@for attempt in $$(seq 1 60); do \
		curl -sf -o /dev/null http://127.0.0.1:$(BROWSER_S3_PORT)/ && exit 0; \
		sleep 1; \
	done; \
	echo "the S3 stand-in did not answer on port $(BROWSER_S3_PORT)"; \
	exit 1
	$(MAKE) -C ../backend build
	NEXT_PUBLIC_API_BASE_URL=http://127.0.0.1:$(BROWSER_BACKEND_PORT) $(NPM) run build

browser-stack-down:
	$(BROWSER_COMPOSE) down -v
