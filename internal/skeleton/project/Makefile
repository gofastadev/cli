.PHONY: dev up down build wire generate test test-integration lint clean migrate-up migrate-down init

# ──────────────────────────────────────────────
# Development (pick one)
# ──────────────────────────────────────────────

# Run on host (requires Go + DB running)
dev:
	go run ./app/main dev

# Run in Docker (requires Docker only)
up:
	docker compose up --build

down:
	docker compose down

# ──────────────────────────────────────────────
# Build & Code Generation
# ──────────────────────────────────────────────

build:
	go build -o ./tmp/main ./app/main/

wire:
	go tool wire ./app/di/

generate:
	go generate ./...

gqlgen:
	go tool gqlgen generate

# ──────────────────────────────────────────────
# Testing
# ──────────────────────────────────────────────

test:
	go test ./... -short -v

test-integration:
	go test ./... -run Integration -v

# ──────────────────────────────────────────────
# Database
# ──────────────────────────────────────────────

migrate-up:
	go run ./app/main migrate up

migrate-down:
	go run ./app/main migrate down

# ──────────────────────────────────────────────
# Setup
# ──────────────────────────────────────────────

init:
	go run ./app/main init

# ──────────────────────────────────────────────
# Lint & Cleanup
# ──────────────────────────────────────────────

lint:
	golangci-lint run

clean:
	rm -rf ./tmp
	go clean
