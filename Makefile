# ─── RecoverAI Makefile ───────────────────────────────────────────────────────
# Usage:
#   make dev              — start all services with mock AI (zero tokens)
#   make dev-real         — start all services with real Groq AI
#   make test             — run unit tests only (no docker required)
#   make test-integration — run full pipeline integration tests
#   make test-all         — unit + integration
#   make build            — build all Go binaries
#   make mock-ai          — start the mock AI server standalone
#   make logs             — tail all docker-compose logs
#   make clean            — stop and remove docker volumes
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: dev dev-real dev-stop test test-unit test-integration test-all \
        build build-api build-worker build-mock-ai \
        mock-ai logs clean migrate seed lint fmt vet

# ─── Environment ─────────────────────────────────────────────────────────────

# Default: mock AI, zero tokens
USE_MOCK_AI     ?= true
TEST_AI_LIMIT   ?= 0
MOCK_AI_DELAY   ?= 50
MOCK_AI_PORT    ?= 8001

# Integration test env
DATABASE_URL    ?= postgres://recoverai:secret@localhost:5432/recoverai?sslmode=disable
REDIS_URL       ?= redis://localhost:6379
API_URL         ?= http://localhost:8080

# ─── Development ─────────────────────────────────────────────────────────────

## Start all services in mock AI mode (zero Groq tokens)
dev:
	@echo "Starting RecoverAI with MOCK AI (zero tokens)..."
	USE_MOCK_AI=true MOCK_AI_DELAY_MS=$(MOCK_AI_DELAY) \
		docker-compose up -d
	@echo ""
	@echo "  API:       http://localhost:8080"
	@echo "  Mock AI:   http://localhost:$(MOCK_AI_PORT)"
	@echo "  Dashboard: http://localhost:3000"
	@echo ""
	@echo "  AI mode: MOCK (USE_MOCK_AI=true)"
	@echo "  Run 'make logs' to watch logs"

## Start all services with real Groq AI
dev-real:
	@echo "Starting RecoverAI with REAL AI (Groq)..."
	@if [ -z "$(GROQ_API_KEY)" ]; then echo "ERROR: GROQ_API_KEY is required"; exit 1; fi
	USE_MOCK_AI=false \
		docker-compose up -d
	@echo "  AI mode: REAL (Groq)"

## Start with TEST_AI_LIMIT=N: use real AI for N calls, then switch to mock
## Example: make dev-limit N=5
dev-limit:
	@echo "Starting RecoverAI with TEST_AI_LIMIT=$(N) (real AI for first $(N) calls)..."
	USE_MOCK_AI=false TEST_AI_LIMIT=$(N) \
		docker-compose up -d
	@echo "  AI mode: REAL for first $(N) calls, then MOCK"

dev-stop:
	docker-compose stop

logs:
	docker-compose logs -f

# ─── Build ────────────────────────────────────────────────────────────────────

build: build-api build-worker build-mock-ai
	@echo "All binaries built."

build-api:
	go build -o bin/api ./cmd/api

build-worker:
	go build -o bin/worker ./cmd/worker

build-mock-ai:
	go build -o bin/mock-ai ./cmd/mock-ai

## Run the mock AI server standalone (outside Docker)
mock-ai: build-mock-ai
	MOCK_AI_DELAY_MS=$(MOCK_AI_DELAY) PORT=$(MOCK_AI_PORT) ./bin/mock-ai

# ─── Tests ───────────────────────────────────────────────────────────────────

## Run all unit tests (no external deps required)
test: test-unit

test-unit:
	@echo "Running unit tests..."
	go test -v -count=1 ./internal/policy/... ./internal/services/...
	@echo "Unit tests complete."

## Run integration tests (requires docker-compose up)
##
## The integration tests:
##   - Connect to real PostgreSQL, Redis, Kafka
##   - Start an in-process mock AI server (httptest.Server)
##   - Seed test data before each test
##   - Clean up after each test
##   - Skip gracefully if infrastructure is unavailable
##
## Prerequisites:
##   docker-compose up -d
##   (wait for services to be healthy)
test-integration:
	@echo "Running integration tests (requires docker-compose up)..."
	@echo "  DATABASE_URL: $(DATABASE_URL)"
	@echo "  REDIS_URL:    $(REDIS_URL)"
	@echo "  API_URL:      $(API_URL)"
	@echo ""
	USE_MOCK_AI=true \
	DATABASE_URL=$(DATABASE_URL) \
	REDIS_URL=$(REDIS_URL) \
	API_URL=$(API_URL) \
	go test \
		-tags integration \
		-v \
		-count=1 \
		-timeout 120s \
		./test/integration/...

## Run unit + integration tests
test-all: test-unit test-integration

## Run tests with race detector
test-race:
	go test -race -count=1 ./internal/policy/... ./internal/services/...

## Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./internal/policy/... ./internal/services/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# ─── Database ─────────────────────────────────────────────────────────────────

## Run database migrations
migrate:
	migrate \
		-path ./internal/db/migrations \
		-database "$(DATABASE_URL)" \
		up

## Roll back last migration
migrate-down:
	migrate \
		-path ./internal/db/migrations \
		-database "$(DATABASE_URL)" \
		down 1

## Seed test data
seed:
	go run ./cmd/seed/main.go

# ─── Code Quality ─────────────────────────────────────────────────────────────

## Run go vet
vet:
	go vet ./...

## Run gofmt
fmt:
	gofmt -w ./cmd ./internal ./test

## Run golangci-lint (install: brew install golangci-lint)
lint:
	golangci-lint run ./...

# ─── Docker ───────────────────────────────────────────────────────────────────

## Build all Docker images
docker-build:
	docker build -f Dockerfile.go --target api     -t recoverai-api:latest .
	docker build -f Dockerfile.go --target worker  -t recoverai-worker:latest .
	docker build -f Dockerfile.go --target mock-ai -t recoverai-mock-ai:latest .
	docker build -f ai-service/Dockerfile         -t recoverai-ai:latest ./ai-service
	docker build -f frontend/Dockerfile           -t recoverai-frontend:latest ./frontend

## Stop all services and remove volumes (DESTRUCTIVE)
clean:
	@echo "⚠️  This will stop all services and delete ALL data."
	@read -p "Are you sure? [y/N] " ans && [ "$$ans" = "y" ] || exit 1
	docker-compose down -v
	rm -f bin/api bin/worker bin/mock-ai

## Reset database only
clean-db:
	@echo "⚠️  This will delete ALL database data."
	@read -p "Are you sure? [y/N] " ans && [ "$$ans" = "y" ] || exit 1
	docker-compose exec postgres psql -U recoverai -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
	$(MAKE) migrate

# ─── Status ───────────────────────────────────────────────────────────────────

## Check current AI mode from the status endpoint
status:
	@curl -s http://localhost:8080/api/v1/status 2>/dev/null | \
		python3 -m json.tool 2>/dev/null || \
		echo "API not reachable. Run 'make dev' first."

## Check health of all services
health:
	@echo "--- API ---"
	@curl -s http://localhost:8080/health || echo "UNREACHABLE"
	@echo ""
	@echo "--- Mock AI ---"
	@curl -s http://localhost:$(MOCK_AI_PORT)/health || echo "UNREACHABLE"
	@echo ""
	@echo "--- AI Service ---"
	@curl -s http://localhost:8000/health || echo "UNREACHABLE"
	@echo ""

# ─── Help ─────────────────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "RecoverAI — Available Commands:"
	@echo ""
	@echo "  Development:"
	@echo "    make dev              Start all services (mock AI, zero tokens)"
	@echo "    make dev-real         Start all services (real Groq AI)"
	@echo "    make dev-limit N=10   Start with 10 real AI calls then mock"
	@echo "    make logs             Tail docker-compose logs"
	@echo "    make status           Check current AI mode"
	@echo "    make health           Check service health"
	@echo ""
	@echo "  Testing:"
	@echo "    make test             Run unit tests (no docker required)"
	@echo "    make test-integration Run pipeline integration tests"
	@echo "    make test-all         Run all tests"
	@echo "    make test-race        Run with race detector"
	@echo "    make test-coverage    Generate coverage report"
	@echo ""
	@echo "  Build:"
	@echo "    make build            Build all Go binaries"
	@echo "    make mock-ai          Start standalone mock AI server"
	@echo ""
	@echo "  Database:"
	@echo "    make migrate          Run pending migrations"
	@echo "    make migrate-down     Roll back last migration"
	@echo "    make seed             Seed test data"
	@echo ""
	@echo "  Maintenance:"
	@echo "    make fmt              Run gofmt"
	@echo "    make vet              Run go vet"
	@echo "    make lint             Run golangci-lint"
	@echo "    make clean            Stop services + delete volumes (DESTRUCTIVE)"
	@echo ""
