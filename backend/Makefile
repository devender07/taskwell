# Local backend: `make` (or `make dev`) starts Postgres, Mailpit, an S3 store (uploads) and Chrome, applies migrations, then runs the user
# API (:8090), AI tester (:3128 proxy, :3129 live view), admin API (:8080) and MCP server (:8095) with prefixed logs.
# Ctrl+C stops them all. The tester reads ANTHROPIC_API_KEY and TEST_SECRET_KEY from the git-ignored .env.

COMPOSE := docker compose -f ../docker-compose.yml
PKGS    := github.com/devender07/taskwell/backend/admin/... github.com/devender07/taskwell/backend/user/... \
           github.com/devender07/taskwell/backend/shared/... \
           github.com/devender07/taskwell/backend/mcp/...
# Loads .env into a recipe's environment when it exists.
ENV     := set -a; [ -f .env ] && . ./.env; set +a;
# run SERVICE, COMMAND: runs it with every log line prefixed by the service name.
run      = $(ENV) $(2) 2>&1 | sed -u 's/^/[$(1)] /'

# Test sites on localhost or the LAN are allowed in dev (the user API and the tester both read it).
export AI_TEST_ALLOW_PRIVATE ?= true

.PHONY: dev up down migrate user tester admin mcp build vet test admin-account admin-reset-mfa mfa-code

dev: up migrate
	@$(MAKE) --no-print-directory -j4 user tester admin mcp

up:
	$(COMPOSE) --profile ai up -d --wait postgres mailpit s3 chrome
	@echo 's3.bucket.create -name taskwell' | $(COMPOSE) exec -T s3 weed shell -master=localhost:9333 >/dev/null

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

# Admin accounts for the admin app (:5174), against the running stack:
#   make admin-account EMAIL=you@example.com NAME='Your name'   create one (or reset its password); prints the setup key
#   make admin-reset-mfa EMAIL=you@example.com                  new setup key and recovery codes; ends their sign-ins
#   make mfa-code EMAIL=you@example.com                         the admin's current 6-digit code, like their authenticator
#   make mfa-code KEY=SETUP_KEY                                 the same from a setup key (e.g. one printed on the server)
admin-account:
	@[ -n "$(EMAIL)" ] || { echo "usage: make admin-account EMAIL=you@example.com NAME='Your name'"; exit 1; }
	@../deploy/create-admin.sh "$(EMAIL)" "$(or $(NAME),Admin)"

admin-reset-mfa:
	@[ -n "$(EMAIL)" ] || { echo "usage: make admin-reset-mfa EMAIL=you@example.com"; exit 1; }
	@$(ENV) go run ./admin/cmd/admin -reset-mfa "$(EMAIL)"

mfa-code:
	@if [ -n "$(EMAIL)" ]; then $(ENV) go run ./admin/cmd/admin -mfa-code "$(EMAIL)" 2>&1 | grep -v ' level=WARN '; \
	elif [ -n "$(KEY)" ]; then ../deploy/mfa-code.sh "$(KEY)"; \
	else echo "usage: make mfa-code EMAIL=you@example.com (or KEY=SETUP_KEY)"; exit 1; fi

build:
	go build $(PKGS)

vet:
	go vet $(PKGS)

# The database tests need TEST_DATABASE_URL (a separate database); without it they skip.
test:
	go test $(PKGS)
