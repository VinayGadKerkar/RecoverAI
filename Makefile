.PHONY: dev build test migrate seed lint docker-up docker-down sqlc tidy

# ─── Local dev ────────────────────────────────────────────────────────────────
dev:
	docker compose up postgres redis kafka -d
	go run ./cmd/api & go run ./cmd/worker

# ─── Build ────────────────────────────────────────────────────────────────────
build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	go build -o bin/seed ./cmd/seed

# ─── Tests ────────────────────────────────────────────────────────────────────
test:
	go test ./... -v -race

# ─── Database ─────────────────────────────────────────────────────────────────
migrate:
	psql "$(DATABASE_URL)" -f internal/db/migrations/001_initial_schema.sql

seed:
	go run ./cmd/seed

# ─── Code generation ──────────────────────────────────────────────────────────
sqlc:
	cd internal/db/sqlc && sqlc generate

tidy:
	go mod tidy

# ─── Lint ─────────────────────────────────────────────────────────────────────
lint:
	golangci-lint run ./...

# ─── Docker ───────────────────────────────────────────────────────────────────
docker-up:
	docker compose up --build -d

docker-down:
	docker compose down -v
