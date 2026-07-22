# Common developer tasks. All commands assume you're in a `nix develop` shell
# or have the equivalent tools on $PATH.

.PHONY: help run watch test lint fmt build sqlc migrate-up migrate-down migrate-new openapi db-up db-down db-reset docker-up docker-down

# Local Nix-native Postgres. Data lives in a gitignored dir under the repo, so
# `make db-reset` is a safe throwaway and nothing touches system Postgres.
PGDATA := .pgdata
PGPORT := 5432

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Available targets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

run: ## Run the server locally (needs `make db-up` first)
	go run ./cmd/server

watch: ## Run the server with live reload, rebuilding on file change
	wgo run ./cmd/server

test: ## Run all tests with race detector
	go test -race -cover ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format all Go files
	gofmt -w .
	go mod tidy

build: ## Build the server binary
	go build -o bin/server ./cmd/server

sqlc: ## Regenerate sqlc code from queries.sql files
	sqlc generate

migrate-up: ## Apply all pending migrations
	goose -dir migrations postgres "$$DATABASE_URL" up

migrate-down: ## Roll back one migration
	goose -dir migrations postgres "$$DATABASE_URL" down

migrate-new: ## Create a new migration. Usage: make migrate-new name=add_sessions
	goose -dir migrations create $(name) sql

openapi: ## Generate Go server interfaces from openapi.yaml
	oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml

db-up: ## Start a local Nix-native Postgres (initialises $(PGDATA) on first run)
	@if [ ! -d "$(PGDATA)" ]; then \
		echo "Initialising Postgres cluster in $(PGDATA)..."; \
		initdb --username=typing --auth=trust --pgdata=$(PGDATA) >/dev/null; \
	fi
	@pg_ctl --pgdata=$(PGDATA) --log=$(PGDATA)/server.log \
		--options="-p $(PGPORT) -k /tmp" --wait start
	@createdb --host=localhost --port=$(PGPORT) --username=typing typing 2>/dev/null \
		&& echo "Created database 'typing'." || true
	@echo "Postgres up on localhost:$(PGPORT). Migrations apply on server start."

db-down: ## Stop the local Postgres
	@pg_ctl --pgdata=$(PGDATA) --mode=fast stop 2>/dev/null || echo "Postgres not running."

db-reset: db-down ## Stop Postgres and delete all local data
	@rm -rf $(PGDATA)
	@echo "Removed $(PGDATA). Next 'make db-up' starts fresh."

docker-up: ## Start the full dev stack in Docker (app + postgres) — for reviewers
	docker compose -f deploy/docker/compose.yaml up --build

docker-down: ## Stop and remove the dev stack
	docker compose -f deploy/docker/compose.yaml down
