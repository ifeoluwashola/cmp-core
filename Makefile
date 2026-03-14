include .env
export

# ─── Variables ──────────────────────────────────────────────────────────────────
BINARY_DIR     := bin
MIGRATION_DIR  := migrations
DB_DSN         := postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable

# ─── Go ─────────────────────────────────────────────────────────────────────────
.PHONY: tidy build lint test

tidy:
	go mod tidy

build:
	@echo "Building all service binaries..."
	@for dir in cmd/*/; do \
		svc=$$(basename $$dir); \
		echo "  -> $$svc"; \
		go build -o $(BINARY_DIR)/$$svc ./$$dir; \
	done

lint:
	golangci-lint run ./...

test:
	go test -race -cover ./...

# ─── Database / Migrations ───────────────────────────────────────────────────────
.PHONY: migrate-up migrate-down migrate-create db-create

## Run all pending migrations
migrate-up:
	migrate -path $(MIGRATION_DIR) -database "$(DB_DSN)" up

## Roll back the last migration
migrate-down:
	migrate -path $(MIGRATION_DIR) -database "$(DB_DSN)" down 1

## Create a new migration: make migrate-create name=create_foo_table
migrate-create:
	@[ "$(name)" ] || { echo "Usage: make migrate-create name=<migration_name>"; exit 1; }
	migrate create -ext sql -dir $(MIGRATION_DIR) -seq $(name)

## Create the database (run once on a fresh postgres instance)
db-create:
	psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -c "CREATE DATABASE \"$(DB_NAME)\";" postgres

# ─── Docker ─────────────────────────────────────────────────────────────────────
.PHONY: up down

up:
	docker compose up -d

down:
	docker compose down

# ─── Help ───────────────────────────────────────────────────────────────────────
.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
