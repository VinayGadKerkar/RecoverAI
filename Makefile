# ════════════════════════════════════════════════════════════════════════════════
# RecoverAI — Makefile
#
# Usage: make <target>
#        make help   — print this reference
#
# Quick start:
#   make dev          — start everything (mock AI, zero tokens)
#   make migrate      — run DB migrations
#   make seed         — seed demo data
#   open http://localhost:3000
# ════════════════════════════════════════════════════════════════════════════════

# ─── Configuration ────────────────────────────────────────────────────────────

# These can be overridden on the command line: make <target> VAR=value
MOCK_AI_PORT    ?= 8001
MOCK_AI_DELAY   ?= 50
DATABASE_URL    ?= postgres://recoverai:secret@localhost:5432/recoverai?sslmode=disable
REDIS_URL       ?= redis://localhost:6379
API_URL         ?= http://localhost:8080
KAFKA_BROKERS   ?= localhost:9092

KAFKA_BIN       := docker-compose exec kafka /opt/kafka/bin
MIGRATE         := migrate -path ./internal/db/migrations -database "$(DATABASE_URL)"

# Colours for terminal output
BOLD  := \033[1m
RESET := \033[0m
GREEN := \033[32m
CYAN  := \033[36m
YELLOW:= \033[33m

.PHONY: \
  dev dev-logs dev-stop \
  migrate migrate-down migrate-status migrate-force \
  seed seed-reset \
  kafka-topics kafka-ui \
  mock-ai mock-ai-status ai-status \
  test-unit test-integration test-real-ai test-all test-race test-coverage \
  load-test-mock load-test-real \
  demo-scenarios demo-reset demo-checklist \
  ai-toggle-mock ai-toggle-real \
  build build-api build-worker build-mock-ai \
  fmt vet lint \
  docker-build clean clean-db \
  help

# Default target
.DEFAULT_GOAL := help


# ════════════════════════════════════════════════════════════════════════════════
# ── Development ──────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## dev: Start all services with MOCK AI (zero Groq tokens)
##      API: :8080 | Mock AI: :8001 | Dashboard: :3000
dev:
	@echo "$(BOLD)Starting RecoverAI — MOCK AI mode (zero tokens)$(RESET)"
	@docker-compose up -d
	@echo ""
	@echo "  $(GREEN)API$(RESET)        http://localhost:8080"
	@echo "  $(GREEN)Mock AI$(RESET)    http://localhost:$(MOCK_AI_PORT)/health"
	@echo "  $(GREEN)Dashboard$(RESET)  http://localhost:3000"
	@echo ""
	@echo "  AI mode: $(YELLOW)MOCK$(RESET) (USE_MOCK_AI=true, zero tokens)"
	@echo "  Watch logs: make dev-logs"
	@echo "  Check mode: make ai-status"

## dev-logs: Tail live logs from API, worker, and AI service only
dev-logs:
	@docker-compose logs -f api worker ai-service

## dev-stop: Stop all containers (preserves volumes / data)
dev-stop:
	@docker-compose stop
	@echo "Services stopped. Data preserved. Use 'make dev' to restart."


# ════════════════════════════════════════════════════════════════════════════════
# ── Database ──────────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## migrate: Apply all pending migrations (golang-migrate)
##          Install: https://github.com/golang-migrate/migrate/releases
migrate:
	@echo "$(BOLD)Running database migrations...$(RESET)"
	@$(MIGRATE) up
	@echo "$(GREEN)Migrations complete.$(RESET)"

## migrate-down: Roll back the most recent migration (one step)
migrate-down:
	@echo "$(YELLOW)Rolling back one migration...$(RESET)"
	@$(MIGRATE) down 1

## migrate-status: Show current migration version
migrate-status:
	@$(MIGRATE) version

## migrate-force: Force migration version (use if migration left dirty state)
##               Usage: make migrate-force V=<version_number>
migrate-force:
	@if [ -z "$(V)" ]; then echo "Usage: make migrate-force V=<version>"; exit 1; fi
	@$(MIGRATE) force $(V)

## seed: Seed 1 merchant, 20 customers, 50 payments, 4 demo recovery cases
##       Safe to re-run — cleans up previous demo data before re-seeding
seed:
	@echo "$(BOLD)Seeding demo data...$(RESET)"
	@go run ./cmd/seed/main.go

## seed-reset: Alias for seed (idempotent — always cleans and re-seeds)
seed-reset: seed


# ════════════════════════════════════════════════════════════════════════════════
# ── Kafka ─────────────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## kafka-topics: Create all 7 required topics (idempotent — skips existing ones)
##               Partitions and retention are set per topic role.
kafka-topics:
	@echo "$(BOLD)Creating Kafka topics...$(RESET)"
	@$(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic payment.events \
		--partitions 6 \
		--replication-factor 1 \
		--config retention.ms=604800000  # 7 days
	@$(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic revenue.risk \
		--partitions 6 \
		--replication-factor 1 \
		--config retention.ms=259200000  # 3 days
	@$(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic recovery.commands \
		--partitions 6 \
		--replication-factor 1 \
		--config retention.ms=86400000   # 1 day
	@$(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic recovery.results \
		--partitions 6 \
		--replication-factor 1 \
		--config retention.ms=604800000  # 7 days
	@$(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic recovery.blocked \
		--partitions 6 \
		--replication-factor 1 \
		--config retention.ms=259200000  # 3 days
	@$(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic notification.events \
		--partitions 3 \
		--replication-factor 1 \
		--config retention.ms=86400000   # 1 day
	@$(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic audit.events \
		--partitions 3 \
		--replication-factor 1 \
		--config retention.ms=2592000000 # 30 days
	@echo ""
	@echo "$(GREEN)Topics ready. Listing:$(RESET)"
	@$(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 --list

## kafka-ui: Open Kafka UI in browser if running, else print topic summary
kafka-ui:
	@if curl -sf http://localhost:9080/api/clusters >/dev/null 2>&1; then \
		echo "Opening Kafka UI at http://localhost:9080"; \
		open http://localhost:9080 2>/dev/null || xdg-open http://localhost:9080 2>/dev/null || \
			echo "Open http://localhost:9080 in your browser"; \
	else \
		echo "$(YELLOW)Kafka UI not running. Printing topic list via kafka-topics.sh:$(RESET)"; \
		echo ""; \
		$(KAFKA_BIN)/kafka-topics.sh \
			--bootstrap-server localhost:9092 \
			--describe 2>/dev/null || \
			echo "Kafka not reachable at localhost:9092. Run: make dev"; \
	fi


# ════════════════════════════════════════════════════════════════════════════════
# ── Mock AI Server ────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## mock-ai: Start the mock AI server standalone (outside Docker)
##          Same port as Docker mock-ai container — stop Docker version first.
##          Reads: PORT (default 8001), MOCK_AI_DELAY_MS (default 50ms)
mock-ai:
	@echo "$(BOLD)Starting Mock AI server...$(RESET)"
	@echo "  Port:  $(MOCK_AI_PORT)"
	@echo "  Delay: $(MOCK_AI_DELAY)ms (simulated AI latency)"
	@echo "  Docs:  POST http://localhost:$(MOCK_AI_PORT)/analyze"
	@echo "  Zero tokens. Always HTTP 200. Deterministic by UPI error code."
	@echo ""
	@PORT=$(MOCK_AI_PORT) MOCK_AI_DELAY_MS=$(MOCK_AI_DELAY) go run ./cmd/mock-ai/main.go

## mock-ai-status: Check if mock AI server is running and healthy
mock-ai-status:
	@echo "$(BOLD)Mock AI server health:$(RESET)"
	@curl -sf http://localhost:$(MOCK_AI_PORT)/health 2>/dev/null | \
		(command -v jq >/dev/null 2>&1 && jq . || cat) || \
		echo "$(YELLOW)Mock AI not reachable at :$(MOCK_AI_PORT)$(RESET)"
	@echo ""
	@echo "Test POST /analyze with U30:"
	@curl -sf -X POST http://localhost:$(MOCK_AI_PORT)/analyze \
		-H "Content-Type: application/json" \
		-d '{"payment_id":"test","case_id":"test","amount_paise":499900,"upi_error_code":"U30","upi_error_category":"TD","failure_type":"transient","failure_reason":"test","time_of_failure_hour":14,"force_payment_link":false,"customer_history":{"successful_payments":5,"failed_payments":1,"lifetime_value_paise":500000},"risk_score":0.82,"priority":"high","merchant_policy":{"max_retry_amount_paise":1000000,"max_retries":3,"retry_cooldown_minutes":10,"require_human_above_paise":5000000,"allowed_actions":["retry","payment_link"]}}' \
		2>/dev/null | \
		(command -v jq >/dev/null 2>&1 && jq '{action,_mock,scheduled_at_minutes}' || cat) || \
		echo "$(YELLOW)POST failed — is the mock server running?$(RESET)"

## ai-status: Show current AI mode from the running API (mock vs real, active URL)
ai-status:
	@echo "$(BOLD)Current AI configuration:$(RESET)"
	@curl -sf $(API_URL)/api/v1/status 2>/dev/null | \
		(command -v jq >/dev/null 2>&1 && jq . || cat) || \
		echo "$(YELLOW)API not reachable at $(API_URL). Run: make dev$(RESET)"


# ════════════════════════════════════════════════════════════════════════════════
# ── Testing ───────────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## test-unit: Run all unit tests — zero external connections required
##            No DB, Redis, Kafka, AI, or Razorpay API calls.
##            47 policy engine tests + 35 risk processor tests.
test-unit:
	@echo "$(BOLD)Running unit tests (no external connections)...$(RESET)"
	@go test ./internal/... -v -count=1
	@echo ""
	@echo "$(GREEN)Unit tests passed.$(RESET)"

## test-integration: Run full pipeline integration tests with MOCK AI
##                   Requires: docker-compose up && make migrate
##                   Uses real PostgreSQL, Redis, Kafka.
##                   AI is replaced by in-process httptest.Server (zero tokens).
##                   Skips gracefully if infrastructure is unavailable.
test-integration:
	@echo "$(BOLD)Running integration tests (mock AI, real pipeline)...$(RESET)"
	@echo "  DATABASE_URL: $(DATABASE_URL)"
	@echo "  API_URL:      $(API_URL)"
	@echo "  AI mode:      MOCK (in-process httptest.Server)"
	@echo ""
	@USE_MOCK_AI=true \
	DATABASE_URL=$(DATABASE_URL) \
	REDIS_URL=$(REDIS_URL) \
	API_URL=$(API_URL) \
	go test \
		-tags integration \
		-v \
		-count=1 \
		-timeout 60s \
		./test/integration/...

## test-real-ai: Run integration tests with REAL Groq AI
##               Fires exactly 11 real Groq calls (one per UPI error code),
##               then auto-switches to mock for the rest. ~$0.001 cost.
##               Requires GROQ_API_KEY in environment.
test-real-ai:
	@if [ -z "$$GROQ_API_KEY" ]; then \
		echo "$(YELLOW)ERROR: GROQ_API_KEY not set.$(RESET)"; \
		echo "Export it: export GROQ_API_KEY=gsk_xxx"; \
		exit 1; \
	fi
	@echo "$(BOLD)Running integration tests with REAL Groq AI (TEST_AI_LIMIT=11)...$(RESET)"
	@echo "  This fires exactly 11 real Groq API calls (one per UPI error code)."
	@echo "  Estimated cost: <\$0.001"
	@echo ""
	@USE_MOCK_AI=false \
	TEST_AI_LIMIT=11 \
	DATABASE_URL=$(DATABASE_URL) \
	REDIS_URL=$(REDIS_URL) \
	API_URL=$(API_URL) \
	go test \
		-tags integration \
		-v \
		-count=1 \
		-timeout 120s \
		./test/integration/...

## test-all: Run unit tests then integration tests
test-all: test-unit test-integration

## test-race: Run unit tests with Go's race detector
test-race:
	@echo "$(BOLD)Running tests with race detector...$(RESET)"
	@go test -race -count=1 ./internal/...

## test-coverage: Generate HTML coverage report
##                Opens coverage.html after generation.
test-coverage:
	@go test -coverprofile=coverage.out ./internal/...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report: coverage.html$(RESET)"
	@open coverage.html 2>/dev/null || xdg-open coverage.html 2>/dev/null || true


# ════════════════════════════════════════════════════════════════════════════════
# ── Load Testing ─────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## load-test-mock: k6 load test with MOCK AI — zero Groq tokens
##                 1000 events/s sustained, 2 min test, no API cost.
##                 Safe to run unlimited times.
##                 Install k6: https://k6.io/docs/get-started/installation/
load-test-mock:
	@echo "$(BOLD)Load test: MOCK AI — zero tokens$(RESET)"
	@echo "  Sending 100 req/s for ~2 min via k6"
	@echo "  Mock AI expected throughput: ~12,000 req/s"
	@echo ""
	@which k6 >/dev/null 2>&1 || { \
		echo "$(YELLOW)k6 not found. Install: brew install k6 (macOS) or see https://k6.io/docs/get-started/installation/$(RESET)"; \
		exit 1; \
	}
	@USE_MOCK_AI=true BASE_URL=$(API_URL) \
		k6 run \
		--env USE_MOCK_AI=true \
		load-test/payment_recovery.js

## load-test-real: k6 load test with REAL AI, capped at 20 Groq calls
##                 Validates real AI latency under load.
##                 After 20 Groq calls, worker auto-switches to mock.
##                 Estimated cost: <$0.002
load-test-real:
	@if [ -z "$$GROQ_API_KEY" ]; then \
		echo "$(YELLOW)ERROR: GROQ_API_KEY not set.$(RESET)"; \
		exit 1; \
	fi
	@echo "$(BOLD)Load test: REAL AI (first 20 calls), then MOCK$(RESET)"
	@echo "  TEST_AI_LIMIT=20 — exactly 20 real Groq calls, rest use mock"
	@echo "  Estimated cost: <\$0.002"
	@echo ""
	@which k6 >/dev/null 2>&1 || { \
		echo "$(YELLOW)k6 not found. Install from https://k6.io/docs/get-started/installation/$(RESET)"; \
		exit 1; \
	}
	@USE_MOCK_AI=false TEST_AI_LIMIT=20 BASE_URL=$(API_URL) \
		k6 run \
		--env USE_MOCK_AI=false \
		--env TEST_AI_LIMIT=20 \
		load-test/payment_recovery.js


# ════════════════════════════════════════════════════════════════════════════════
# ── Demo Scenarios ────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## demo-scenarios: Fire webhook events to populate the dashboard for a live demo
##                 Step 1 (0-5s):   15 rapid U28 failures → triggers outage detection
##                 Step 2 (6-10s):  U30 failure → full recovery pipeline runs
##                 Step 3 (11-15s): Z9 + ₹99 → validator blocks at negative ROI
##                 Total runtime: ~90 seconds (waits for pipeline to process)
demo-scenarios:
	@echo "$(BOLD)Running demo scenarios...$(RESET)"
	@echo ""
	@echo "$(CYAN)Step 1/3: Firing 15 U28 failures to trigger bank outage detection$(RESET)"
	@echo "          Threshold: 10 failures/5min → outage flag will be set in Redis"
	@for i in $$(seq 1 15); do \
		EVENT_ID="evt_demo_outage_$$i_$$RANDOM"; \
		PAYMENT_ID="pay_demo_outage_$$i_$$RANDOM"; \
		BODY="{\"entity\":\"event\",\"account_id\":\"acc_demo\",\"event\":\"payment.failed\",\"contains\":[\"payment\"],\"payload\":{\"payment\":{\"entity\":{\"id\":\"$$PAYMENT_ID\",\"amount\":899900,\"currency\":\"INR\",\"status\":\"failed\",\"method\":\"upi\",\"error_code\":\"U28\",\"error_description\":\"Bank server down\",\"bank\":\"SBI\",\"vpa\":\"demo$$i@upi\",\"email\":\"demo@example.com\",\"contact\":\"+919999999999\",\"created_at\":$$(date +%s)}}},\"created_at\":$$(date +%s)}"; \
		curl -sf -X POST $(API_URL)/webhooks/razorpay \
			-H "Content-Type: application/json" \
			-H "X-Razorpay-Event-Id: $$EVENT_ID" \
			-d "$$BODY" >/dev/null 2>&1 && printf "." || printf "x"; \
		sleep 0.2; \
	done
	@echo ""
	@echo "  Waiting 5s for outage detection to propagate..."
	@sleep 5
	@echo ""
	@echo "$(CYAN)Step 2/3: Firing U30 failure (₹4,999 high-LTV customer) → recovery pipeline$(RESET)"
	@BODY="{\"entity\":\"event\",\"account_id\":\"acc_demo\",\"event\":\"payment.failed\",\"contains\":[\"payment\"],\"payload\":{\"payment\":{\"entity\":{\"id\":\"pay_demo_u30_$$RANDOM\",\"amount\":499900,\"currency\":\"INR\",\"status\":\"failed\",\"method\":\"upi\",\"error_code\":\"U30\",\"error_description\":\"Debit timeout\",\"bank\":\"HDFC\",\"vpa\":\"highltv@upi\",\"email\":\"aarav@gmail.com\",\"contact\":\"+919876543210\",\"created_at\":$$(date +%s)}}},\"created_at\":$$(date +%s)}"; \
	curl -sf -X POST $(API_URL)/webhooks/razorpay \
		-H "Content-Type: application/json" \
		-H "X-Razorpay-Event-Id: evt_demo_u30_$$RANDOM" \
		-d "$$BODY" >/dev/null 2>&1 && echo "  Sent ✓" || echo "  Failed to send"
	@echo ""
	@echo "$(CYAN)Step 3/3: Firing Z9 failure (₹99 new customer) → validator blocks (negative ROI)$(RESET)"
	@BODY="{\"entity\":\"event\",\"account_id\":\"acc_demo\",\"event\":\"payment.failed\",\"contains\":[\"payment\"],\"payload\":{\"payment\":{\"entity\":{\"id\":\"pay_demo_z9_$$RANDOM\",\"amount\":9900,\"currency\":\"INR\",\"status\":\"failed\",\"method\":\"upi\",\"error_code\":\"Z9\",\"error_description\":\"Insufficient funds\",\"bank\":\"AXIS\",\"vpa\":\"newcustomer@upi\",\"email\":\"nisha@gmail.com\",\"contact\":\"+910981234501\",\"created_at\":$$(date +%s)}}},\"created_at\":$$(date +%s)}"; \
	curl -sf -X POST $(API_URL)/webhooks/razorpay \
		-H "Content-Type: application/json" \
		-H "X-Razorpay-Event-Id: evt_demo_z9_$$RANDOM" \
		-d "$$BODY" >/dev/null 2>&1 && echo "  Sent ✓" || echo "  Failed to send"
	@echo ""
	@echo "  Waiting 30s for pipeline to process all events..."
	@sleep 30
	@echo ""
	@echo "$(GREEN)$(BOLD)Demo scenarios complete. Dashboard should now show:$(RESET)"
	@echo "  🌊  bank_outage:U28 active in Redis"
	@echo "  🔵  U30 case in in_progress or recovered"
	@echo "  🚫  Z9 ₹99 case as not_worth_recovering"
	@echo ""
	@echo "  Open dashboard: http://localhost:3000"

## demo-reset: Full environment reset — use before every live presentation
##             Clears DB demo data, re-seeds 4 demo cases, fires live events.
demo-reset: seed-reset demo-scenarios
	@echo ""
	@echo "$(GREEN)$(BOLD)Demo environment ready.$(RESET)"
	@echo "  Dashboard: http://localhost:3000"

## demo-checklist: Print the full pre-presentation checklist and open DEMO_SCRIPT.md
demo-checklist:
	@echo ""
	@echo "$(BOLD)RecoverAI — Pre-Demo Checklist$(RESET)"
	@echo "$(YELLOW)Run these steps in order before presenting:$(RESET)"
	@echo ""
	@echo "  $(BOLD)Step 1$(RESET)  Start services"
	@echo "    $$ make dev"
	@echo ""
	@echo "  $(BOLD)Step 2$(RESET)  Open 3 terminal windows"
	@echo "    Terminal 1 (logs):     make dev-logs"
	@echo "    Terminal 2 (triggers): ready for curl commands"
	@echo "    Terminal 3 (redis):    docker-compose exec redis redis-cli"
	@echo ""
	@echo "  $(BOLD)Step 3$(RESET)  Reset and seed demo data"
	@echo "    $$ make demo-reset"
	@echo "    → wait ~30s for pipeline to process seeded cases"
	@echo ""
	@echo "  $(BOLD)Step 4$(RESET)  Verify AI mode is MOCK"
	@echo "    $$ make ai-status"
	@printf "    → expect: $(GREEN)\"ai_mode\": \"mock\"$(RESET)\n"
	@echo ""
	@echo "  $(BOLD)Step 5$(RESET)  Open dashboard and verify pre-seeded data"
	@echo "    $$ open http://localhost:3000"
	@echo "    → verify these 4 cases are visible:"
	@echo "      $(GREEN)✅  Case A  ₹4,999 recovered$(RESET)         (U30 → retry succeeded)"
	@echo "      $(YELLOW)🚫  Case B  ₹99 not_worth_recovering$(RESET)  (Z9 → negative ROI)"
	@echo "      $(CYAN)👤  Case C  ₹2,499 customer_self_recovered$(RESET)"
	@echo "      🌊  Case D  ₹8,999 outage_batched             (U28 bank down)"
	@echo ""
	@echo "  $(BOLD)Step 6$(RESET)  Service health check"
	@make health 2>/dev/null || true
	@echo ""
	@echo "$(GREEN)$(BOLD)Ready to present!$(RESET)"
	@echo ""
	@echo "  Demo script: cat DEMO_SCRIPT.md"
	@echo "  Scenarios:   4 scenarios, ~3 minutes total"
	@echo "  Recovery:    make demo-reset  (if anything goes wrong)"
	@echo ""


# ════════════════════════════════════════════════════════════════════════════════
# ── Utilities ─────────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## ai-toggle-mock: Switch to mock AI and restart worker — no Groq calls
##                 Edits USE_MOCK_AI in .env then restarts worker container.
ai-toggle-mock:
	@if [ ! -f .env ]; then cp .env.example .env; echo "Created .env from .env.example"; fi
	@if grep -q "USE_MOCK_AI=false" .env; then \
		sed -i.bak 's/USE_MOCK_AI=false/USE_MOCK_AI=true/' .env && rm -f .env.bak; \
		echo "$(GREEN)Switched to MOCK AI in .env$(RESET)"; \
	elif grep -q "USE_MOCK_AI=true" .env; then \
		echo "Already set to MOCK AI in .env"; \
	else \
		echo "USE_MOCK_AI=true" >> .env; \
		echo "$(GREEN)Added USE_MOCK_AI=true to .env$(RESET)"; \
	fi
	@docker-compose restart worker 2>/dev/null || echo "Worker not running — changes will apply on next 'make dev'"
	@echo ""
	@echo "AI mode: $(YELLOW)MOCK$(RESET) — zero Groq tokens"
	@echo "Verify:  make ai-status"

## ai-toggle-real: Switch to real Groq AI and restart worker
##                 Requires GROQ_API_KEY in .env.
ai-toggle-real:
	@if [ ! -f .env ]; then cp .env.example .env; echo "Created .env from .env.example"; fi
	@if ! grep -q "^GROQ_API_KEY=gsk_" .env; then \
		echo "$(YELLOW)WARNING: GROQ_API_KEY not set or looks like a placeholder in .env$(RESET)"; \
		echo "         Set it before switching: GROQ_API_KEY=gsk_yourkey"; \
	fi
	@if grep -q "USE_MOCK_AI=true" .env; then \
		sed -i.bak 's/USE_MOCK_AI=true/USE_MOCK_AI=false/' .env && rm -f .env.bak; \
		echo "$(GREEN)Switched to REAL AI in .env$(RESET)"; \
	elif grep -q "USE_MOCK_AI=false" .env; then \
		echo "Already set to REAL AI in .env"; \
	else \
		echo "USE_MOCK_AI=false" >> .env; \
		echo "$(GREEN)Added USE_MOCK_AI=false to .env$(RESET)"; \
	fi
	@docker-compose restart worker 2>/dev/null || echo "Worker not running — changes will apply on next 'make dev'"
	@echo ""
	@echo "AI mode: $(GREEN)REAL$(RESET) (Groq) — tokens will be consumed"
	@echo "Verify:  make ai-status"


# ════════════════════════════════════════════════════════════════════════════════
# ── Build ─────────────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## build: Compile all Go binaries into ./bin/
build: build-api build-worker build-mock-ai
	@echo "$(GREEN)All binaries built in ./bin/$(RESET)"

build-api:
	@go build -o bin/api ./cmd/api

build-worker:
	@go build -o bin/worker ./cmd/worker

build-mock-ai:
	@go build -o bin/mock-ai ./cmd/mock-ai

## docker-build: Build all Docker images (api, worker, mock-ai, ai-service, frontend)
docker-build:
	@echo "$(BOLD)Building Docker images...$(RESET)"
	@docker build -f Dockerfile.go --target api     -t recoverai-api:latest     . --quiet
	@docker build -f Dockerfile.go --target worker  -t recoverai-worker:latest  . --quiet
	@docker build -f Dockerfile.go --target mock-ai -t recoverai-mock-ai:latest . --quiet
	@echo "$(GREEN)Docker images built.$(RESET)"


# ════════════════════════════════════════════════════════════════════════════════
# ── Code Quality ─────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## fmt: Format all Go source files with gofmt
fmt:
	@gofmt -w ./cmd ./internal ./test
	@echo "$(GREEN)gofmt complete.$(RESET)"

## vet: Run go vet across all packages
vet:
	@go vet ./...
	@echo "$(GREEN)go vet: no issues.$(RESET)"

## lint: Run golangci-lint (install: brew install golangci-lint)
lint:
	@which golangci-lint >/dev/null 2>&1 || { \
		echo "$(YELLOW)golangci-lint not installed.$(RESET)"; \
		echo "Install: brew install golangci-lint"; \
		exit 1; \
	}
	@golangci-lint run ./...


# ════════════════════════════════════════════════════════════════════════════════
# ── Maintenance ───────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## clean: DESTRUCTIVE — stop all containers and delete ALL volumes (data)
clean:
	@echo "$(YELLOW)$(BOLD)⚠  WARNING: This will stop all services and delete ALL data$(RESET)"
	@echo "   Volumes to be deleted: pgdata, redis-data, kafka-data"
	@read -p "   Type 'yes' to confirm: " ans && [ "$$ans" = "yes" ] || { echo "Aborted."; exit 1; }
	@docker-compose down -v
	@rm -f bin/api bin/worker bin/mock-ai coverage.out coverage.html
	@echo "$(GREEN)Clean complete.$(RESET)"

## clean-db: DESTRUCTIVE — reset the database schema only (re-runs all migrations)
clean-db:
	@echo "$(YELLOW)$(BOLD)⚠  WARNING: This will DELETE ALL DATABASE DATA$(RESET)"
	@read -p "   Type 'yes' to confirm: " ans && [ "$$ans" = "yes" ] || { echo "Aborted."; exit 1; }
	@docker-compose exec postgres psql -U recoverai -c \
		"DROP SCHEMA public CASCADE; CREATE SCHEMA public; GRANT ALL ON SCHEMA public TO recoverai;"
	@$(MAKE) migrate
	@echo "$(GREEN)Database reset and re-migrated.$(RESET)"


# ════════════════════════════════════════════════════════════════════════════════
# ── Help ──────────────────────────────────────────────────────────────────────
# ════════════════════════════════════════════════════════════════════════════════

## help: Print this command reference
help:
	@echo ""
	@echo "$(BOLD)RecoverAI — Makefile Reference$(RESET)"
	@echo ""
	@echo "$(CYAN)── Development ──────────────────────────────────────────────$(RESET)"
	@echo "  make dev                  Start all services (mock AI, zero tokens)"
	@echo "  make dev-logs             Tail API + worker + ai-service logs"
	@echo "  make dev-stop             Stop containers (preserves data)"
	@echo ""
	@echo "$(CYAN)── Database ─────────────────────────────────────────────────$(RESET)"
	@echo "  make migrate              Apply all pending migrations"
	@echo "  make migrate-down         Roll back one migration"
	@echo "  make migrate-status       Show current migration version"
	@echo "  make migrate-force V=N    Force migration to version N"
	@echo "  make seed                 Seed 1 merchant, 20 customers, 50 payments, 4 demo cases"
	@echo "  make seed-reset           Same as seed (cleans first, idempotent)"
	@echo ""
	@echo "$(CYAN)── Kafka ────────────────────────────────────────────────────$(RESET)"
	@echo "  make kafka-topics         Create all 7 topics with correct settings"
	@echo "  make kafka-ui             Open Kafka UI or print topic list"
	@echo ""
	@echo "$(CYAN)── Mock AI Server ───────────────────────────────────────────$(RESET)"
	@echo "  make mock-ai              Start mock AI server standalone (:8001)"
	@echo "  make mock-ai-status       Health check + test POST /analyze"
	@echo "  make ai-status            Show current AI mode from running API"
	@echo ""
	@echo "$(CYAN)── Testing ──────────────────────────────────────────────────$(RESET)"
	@echo "  make test-unit            Unit tests — no external connections"
	@echo "  make test-integration     Pipeline tests — real infra, mock AI"
	@echo "  make test-real-ai         Pipeline tests — 11 real Groq calls"
	@echo "  make test-all             Unit + integration"
	@echo "  make test-race            Unit tests with race detector"
	@echo "  make test-coverage        HTML coverage report"
	@echo ""
	@echo "$(CYAN)── Load Testing ─────────────────────────────────────────────$(RESET)"
	@echo "  make load-test-mock       k6 load test — mock AI, zero tokens"
	@echo "  make load-test-real       k6 load test — 20 real Groq calls, then mock"
	@echo ""
	@echo "$(CYAN)── Demo ─────────────────────────────────────────────────────$(RESET)"
	@echo "  make demo-checklist       Pre-presentation checklist + service health"
	@echo "  make demo-scenarios       Fire live webhooks: outage + recovery + blocked"
	@echo "  make demo-reset           Full reset: re-seed + fire demo events"
	@echo ""
	@echo "$(CYAN)── Utilities ────────────────────────────────────────────────$(RESET)"
	@echo "  make ai-toggle-mock       Switch .env to mock AI + restart worker"
	@echo "  make ai-toggle-real       Switch .env to real Groq + restart worker"
	@echo ""
	@echo "$(CYAN)── Build ────────────────────────────────────────────────────$(RESET)"
	@echo "  make build                Build all Go binaries into ./bin/"
	@echo "  make docker-build         Build all Docker images"
	@echo "  make fmt                  Run gofmt"
	@echo "  make vet                  Run go vet"
	@echo "  make lint                 Run golangci-lint"
	@echo ""
	@echo "$(CYAN)── Maintenance ──────────────────────────────────────────────$(RESET)"
	@echo "  make clean                ⚠  Stop + delete ALL volumes"
	@echo "  make clean-db             ⚠  Reset DB schema only"
	@echo ""
