# Integration Tests

Full five-stage pipeline tests using real PostgreSQL, Redis, and Kafka with an in-process mock AI server.

---

## What These Tests Do

Each test exercises the complete pipeline end-to-end:

```
POST /webhooks/razorpay
  → Kafka: payment.events
    → Risk Processor (Stage 2)
      → Kafka: revenue.risk
        → Pre-Recovery Validator (Stage 3)
          → Mock AI (Stage 4, in-process)
            → Policy Engine (Stage 5)
              → PostgreSQL recovery_cases / recovery_actions / audit_logs
```

No mocks for DB, Redis, or Kafka — those are real connections. Only the AI service is replaced by an in-process `httptest.Server` that returns deterministic responses identical in schema to the real Groq service.

---

## Prerequisites

```bash
# 1. Start infrastructure + API + worker
docker-compose up -d

# 2. Wait for all services to be healthy
docker-compose ps

# 3. Run migrations (first time only)
make migrate
```

Services that must be running:
- `postgres` (port 5432)
- `redis` (port 6379)
- `kafka` (port 9092)
- `api` (port 8080) ← receives webhooks
- `worker` ← processes Kafka events

The AI service (`ai-service` or `mock-ai`) does **not** need to be running — the integration tests start their own in-process mock AI server via `httptest.Server`.

---

## Running Tests

```bash
# Run all integration tests
make test-integration

# Run a single test
go test -tags integration -v -run TestFullPipeline_TransientFailure ./test/integration/...

# Run with custom timeouts
go test -tags integration -v -timeout 180s ./test/integration/...

# Run with custom env
DATABASE_URL=postgres://... API_URL=http://... \
  go test -tags integration -v ./test/integration/...
```

---

## Test Cases

### TestFullPipeline_TransientFailure
**Scenario:** U30 (transient bank timeout) payment failure  
**Asserts:**
- Webhook returns 200 in < 5 seconds
- `recovery_cases` row created
- Status advances to `in_progress`
- `ai_diagnosis` contains `_mock: true` (mock AI was called)
- `recovery_actions` has one row with `action_type = retry`
- `audit_logs` has entries from: `risk_engine`, `validator`, `ai_agent`, `policy_engine`

### TestFullPipeline_SelfRecovery
**Scenario:** Customer pays manually after `payment.failed` (out-of-order capture)  
**Asserts:**
- `payment.failed` creates recovery_case with `status = open`
- `payment.captured` (same payment_id) triggers self-recovery detection
- `recovery_cases.status` → `customer_self_recovered`
- `recovery_cases.amount_recovered` = captured amount
- `audit_logs` has entry with `actor = customer_self`

### TestFullPipeline_OutageDetection
**Scenario:** 12 U28 failures within 3 seconds triggers bank outage detection  
**Asserts:**
- Redis key `bank_outage:U28` is set
- `bank_outage_events` row has `failure_count >= 10`
- All cases have `bank_outage_detected = true` and `status = outage_batched`
- `ai_diagnosis` is NULL for all cases (AI not called during outage)

### TestFullPipeline_IdempotentWebhook
**Scenario:** Same webhook event ID sent twice  
**Asserts:**
- Only 1 `recovery_cases` row created
- Only 1 `webhook_events` row for that event_id
- Second webhook returns 200 (graceful duplicate handling)

### TestFullPipeline_NegativeROI
**Scenario:** Z9, ₹99, new customer (0 prior payments) — not worth recovering  
**Asserts:**
- `recovery_cases.status = not_worth_recovering`
- `validator_skip_reason` contains "ROI"
- `ai_diagnosis` is NULL (validator blocked before AI)

### TestPolicyEngine_BlocksNonRetryable
**Scenario:** Direct policy engine call with `upi_error_code = YG` + `RETRY_PAYMENT`  
**Asserts:**
- `decision.Allowed = false`
- `decision.RuleTriggered = rule1_non_retryable_upi`

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://recoverai:secret@localhost:5432/recoverai?sslmode=disable` | PostgreSQL DSN |
| `REDIS_URL` | `redis://localhost:6379` | Redis URL |
| `API_URL` | `http://localhost:8080` | Running API server base URL |
| `RAZORPAY_WEBHOOK_SECRET` | `test-webhook-secret` | Must match API server's `RAZORPAY_WEBHOOK_SECRET` |

---

## How Test Isolation Works

Each test calls `setup(t)` which:

1. Connects to real PostgreSQL, Redis
2. Starts a fresh `httptest.Server` as mock AI on a random port
3. Seeds one `merchants` row and one `customers` row
4. Registers `t.Cleanup()` to DELETE all seeded rows after the test

The cleanup runs in reverse dependency order:
```
audit_logs → recovery_actions → recovery_cases → payments → customers → merchants
```

Redis keys used by the test (outage keys, idempotency keys) are also cleaned up.

---

## Timeouts

| Operation | Timeout | Reason |
|-----------|---------|--------|
| Recovery case creation | 10s | Kafka lag on CI |
| Status advance to in_progress | 10s | Multiple Kafka hops |
| Self-recovery detection | 5s | Single Kafka hop |
| Outage detection | 2s wait + 8s poll | Redis counter + case processing |
| Idempotency check | 3s fixed wait | Processing time |
| Negative ROI detection | 10s | Validator processing |

---

## Graceful Skip

`TestMain` checks if the API server and PostgreSQL are reachable before running any tests. If not:

```
SKIP: API server not reachable at http://localhost:8080 (run docker-compose up first)
```

Exit code 0 — CI does not fail, tests are simply skipped.

---

## Build Tag

The `//go:build integration` tag ensures these tests never run with `go test ./...` (unit tests only). They only run when explicitly requested:

```bash
# Unit tests only (fast, no docker)
go test ./...
go test ./internal/...

# Integration tests only (requires docker)
go test -tags integration ./test/integration/...

# Both
go test ./... && go test -tags integration ./test/integration/...
```

---

## Troubleshooting

### Test times out on "waiting for recovery_case"

The Kafka consumer may not be running. Check:
```bash
docker-compose ps worker
docker-compose logs worker | tail -20
```

### Test fails with "invalid signature"

The `RAZORPAY_WEBHOOK_SECRET` in the test must match the running API server. Either:
```bash
# Set same secret in .env
RAZORPAY_WEBHOOK_SECRET=test-webhook-secret

# Or pass to test
RAZORPAY_WEBHOOK_SECRET=mysecret make test-integration
```

### Outage detection test is flaky

Redis failure counters use 5-minute buckets. If the bucket is already populated from a previous test run, the outage may trigger after fewer than 10 new events. The test clears `bank_outage:U28` in setup but not the counter bucket keys (`bank_failures:U28:*`). 

To fully reset:
```bash
docker-compose exec redis redis-cli FLUSHDB
```

### "recovery_case not found" after webhook

Ensure Kafka topics were created:
```bash
docker-compose exec kafka kafka-topics.sh --list --bootstrap-server localhost:9092
# Should include: payment.events, revenue.risk, recovery.commands, recovery.results
```

If missing:
```bash
docker-compose restart kafka-init
```

---

## Adding New Tests

Follow this pattern:

```go
func TestFullPipeline_MyScenario(t *testing.T) {
    te := setup(t)  // connects, seeds, registers cleanup

    // 1. Post a webhook
    status := te.postWebhook(t, "evt_my_001", "payment.failed", "pay_my_001", 100000, "failed", "U30")
    if status != http.StatusOK {
        t.Fatalf("webhook status=%d", status)
    }

    // 2. Wait for case
    caseID := te.waitForRecoveryCase(t, "pay_my_001", 10*time.Second)

    // 3. Wait for specific status
    te.waitForCaseStatus(t, caseID, "in_progress", 10*time.Second)

    // 4. Assert DB state
    var got string
    te.db.QueryRow(context.Background(),
        "SELECT status FROM recovery_cases WHERE id = $1", caseID,
    ).Scan(&got)

    if got != "in_progress" {
        t.Errorf("status=%q want in_progress", got)
    }
}
```
