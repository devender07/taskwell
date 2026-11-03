# Local backend: `make` (or `make dev`) starts Postgres, Mailpit, an S3 store (uploads) and Chrome, applies migrations, then runs the user
# API (:8090), AI tester (:3128 proxy, :3129 live view), admin API (:8080) and MCP server (:8095) with prefixed logs.
# Ctrl+C stops them all. The tester reads ANTHROPIC_API_KEY and TEST_SECRET_KEY from the git-ignored .env.

COMPOSE := docker compose -f ../docker-compose.yml
PKGS    := github.com/devender07/tether/backend/admin/... github.com/devender07/tether/backend/user/... \
           github.com/devender07/tether/backend/shared/... \
           github.com/devender07/tether/backend/mcp/...
# Loads .env into a recipe's environment when it exists.
ENV     := set -a; [ -f .env ] && . ./.env; set +a;
# run SERVICE, COMMAND: runs it with every log line prefixed by the service name.
run      = $(ENV) $(2) 2>&1 | sed -u 's/^/[$(1)] /'

# Test sites on localhost or the LAN are allowed in dev (the user API and the tester both read it).
export AI_TEST_ALLOW_PRIVATE ?= true

.PHONY: dev up down migrate user tester admin mcp build vet test

dev: up migrate
	@$(MAKE) --no-print-directory -j4 user tester admin mcp

up:
	$(COMPOSE) --profile ai up -d --wait postgres mailpit s3 chrome
	@echo 's3.bucket.create -name tether' | $(COMPOSE) exec -T s3 weed shell -master=localhost:9333 >/dev/null

down:
	$(COMPOSE) --profile ai down

migrate:
	@$(call run,migrate,go run ./user/cmd/user -migrate)

user:
	@$(call run,user,go run ./user/cmd/user)

# Chrome runs in Docker and reaches the tester's egress proxy on the host (see docker-compose.yml).
tester:
	@$(call run,tester,CHROME_URL=http://localhost:9222 PROXY_URL=http://host.docker.internal:3128 go run ./user/cmd/tester)

admin:
	@$(call run,admin,go run ./admin/cmd/admin)

mcp:
	@$(call run,mcp,go run ./mcp/cmd/mcp)

build:
	go build $(PKGS)

vet:
	go vet $(PKGS)

# The database tests need TEST_DATABASE_URL (a separate database); without it they skip.
test:
	go test $(PKGS)
