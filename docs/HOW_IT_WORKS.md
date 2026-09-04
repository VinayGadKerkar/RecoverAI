# RecoverAI - Complete System Guide

## Table of Contents
1. [How the Application Works](#how-the-application-works)
2. [Razorpay Webhook Integration](#razorpay-webhook-integration)
3. [Generating Analytics Data](#generating-analytics-data)
4. [Makefile Commands Reference](#makefile-commands-reference)
5. [Testing the System](#testing-the-system)

---

## How the Application Works

### Architecture Overview

RecoverAI is a **5-stage autonomous payment recovery pipeline**:

```
┌──────────────────────────────────────────────────────────────────────┐
│                    STAGE 1: Webhook Ingestion                        │
│  Razorpay → API Server → HMAC Verify → Idempotency → Kafka Publish  │
└────────────────────────────┬─────────────────────────────────────────┘
                             │
                             ▼
┌──────────────────────────────────────────────────────────────────────┐
│                     STAGE 2: Risk Engine                             │
│  Kafka Consumer → Classify UPI Error → Score Risk → Detect Outages  │
└────────────────────────────┬─────────────────────────────────────────┘
                             │
                             ▼
┌──────────────────────────────────────────────────────────────────────┐
│                STAGE 3: Pre-Recovery Validator                       │
│  6 Safety Checks → Blocks ~30% of cases before AI call              │
│  ✓ Payment status  ✓ Bank outage  ✓ RBI compliance                  │
│  ✓ ROI calculation ✓ Retryability  ✓ Retry count                    │
└────────────────────────────┬─────────────────────────────────────────┘
                             │
                             ▼
┌──────────────────────────────────────────────────────────────────────┐
│                  STAGE 4: AI Recovery Service                        │
│  3 Sequential LLM Agents (LangGraph):                                │
│  1. Risk Analyst   → diagnoses root cause                            │
│  2. Strategist     → selects optimal strategy                        │
│  3. Executor       → builds structured JSON command                  │
│  Commands: RETRY_PAYMENT | GENERATE_PAYMENT_LINK | ESCALATE | STOP  │
└────────────────────────────┬─────────────────────────────────────────┘
                             │
                             ▼
┌──────────────────────────────────────────────────────────────────────┐
│            STAGE 5: Policy Engine + Execution                        │
│  10 Deterministic Rules → APPROVE | BLOCK                            │
│  If APPROVED → Execute via Razorpay API                              │
│  If BLOCKED  → Mark case + audit log                                 │
└──────────────────────────────────────────────────────────────────────┘
```

### Key Components

#### 1. **API Server** (`cmd/api`)
- **Port:** 8080
- **Responsibilities:**
  - Receive Razorpay webhooks at `/webhooks/razorpay`
  - HMAC-SHA256 signature verification
  - Idempotency checking (Redis SETNX)
  - REST API endpoints for dashboard
- **Tech:** Go, Chi router, pgx PostgreSQL driver

#### 2. **Worker** (`cmd/worker`)
- **Responsibilities:**
  - 4 Kafka consumers running in parallel:
    1. **Risk Consumer** - Scores risk, classifies errors
    2. **Validator Consumer** - Runs 6 safety checks
    3. **Execution Consumer** - Calls AI service
    4. **Result Consumer** - Processes execution results
- **Tech:** Go, segmentio/kafka-go

#### 3. **AI Service** (`ai-service/`)
- **Port:** 8000
- **Responsibilities:**
  - 3-agent LangGraph workflow
  - Structured JSON output (always valid schema)
- **Tech:** Python, FastAPI, LangGraph, Groq API
- **Model:** llama-3.3-70b-versatile

#### 4. **Frontend** (`frontend/`)
- **Port:** 3000
- **Pages:**
  - `/dashboard` - Overview with metrics
  - `/dashboard/cases` - Recovery cases table
  - `/dashboard/cases/[id]` - Case detail with full audit timeline
  - `/dashboard/analytics` - AI performance metrics
- **Real-time Features:**
  - WebSocket connection for live updates
  - Toast notifications for recovery events
  - Auto-refresh with SWR polling (30s intervals)
- **Tech:** Next.js 14, TypeScript, Tailwind CSS, SWR, Sonner (toast notifications)

#### 5. **Database Schema**

9 tables total:

```sql
merchants               -- Razorpay merchant accounts
customers               -- Customer profiles (LTV, success count)
payments                -- Every payment attempt
recovery_cases          -- One per failed payment
recovery_actions        -- Individual actions (retry, payment_link, etc)
recovery_policies       -- Per-merchant policy config
webhook_events          -- Idempotency log + raw webhook payloads
audit_logs              -- Immutable audit trail (8 actor types)
bank_outage_events      -- Outage detection history
```

**WebSocket Real-Time Updates:**

The backend publishes events to the `websocket.events` Kafka topic, which are consumed by the frontend via WebSocket:

```json
{
  "type": "audit_event",
  "action": "payment_captured",
  "case_id": "uuid",
  "amount": 49990,
  "timestamp": "2026-09-04T10:30:00Z"
}
```

These events trigger toast notifications in the bottom-right corner:
- 🟢 **payment_captured** → Green success toast
- 🔵 **self_recovered** → Blue info toast
- ⚪ **stopped** → Gray toast
- 🟠 **human_approval_required** → Orange warning toast
- 🟠 **bank_outage_detected** → Orange warning toast

---

## Razorpay Webhook Integration

### Webhook Endpoint

**URL:** `POST /webhooks/razorpay`

### Security & Reliability

The webhook handler implements **5 critical safeguards**:

#### 1. **HMAC-SHA256 Signature Verification**
```go
// Razorpay signs the raw body with your webhook secret
signature := r.Header.Get("X-Razorpay-Signature")
mac := hmac.New(sha256.New, []byte(webhookSecret))
mac.Write(body)
expected := hex.EncodeToString(mac.Sum(nil))
// Must match or webhook is rejected with 401
```

#### 2. **Idempotency (Redis SETNX)**
```
Razorpay sends: X-Razorpay-Event-Id: evt_abc123
RecoverAI stores: webhook:idempotency:evt_abc123 = 1 (24h TTL)
If key exists → 200 OK + "duplicate": true
```

**Why this matters:** Razorpay uses **at-least-once delivery**. Same event can arrive multiple times.

#### 3. **Out-of-Order Handling**
```
payment.captured can arrive BEFORE payment.failed
payment.failed then payment.captured is valid (manual retry by customer)
```

#### 4. **5-Second Response Timeout**
```go
// Razorpay retries exponentially if no 200 within 5s
publishCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
// Always return 200 OK within total 5s budget
```

#### 5. **Customer Self-Recovery Detection**
```go
// When payment.captured arrives for an open recovery case:
if payload.Event == "payment.captured" {
    go h.handleCustomerSelfRecovery(razorpayEventID, payload)
    // Marks case as customer_self_recovered
    // Cancels all pending actions
    // Avoids double-charging
}
```

### Supported Events

| Event | Action |
|-------|--------|
| `payment.failed` | Creates recovery case |
| `payment.captured` | Checks for customer self-recovery |
| `payment.authorized` | Logged but no action |
| `order.paid` | Logged but no action |

### Webhook Flow Diagram

```
1. Razorpay fires webhook
   POST https://your-domain.com/webhooks/razorpay
   Header: X-Razorpay-Signature: abc123...
   Header: X-Razorpay-Event-Id: evt_xyz...

2. RecoverAI receives
   ├─ Read raw body (preserve bytes for HMAC)
   ├─ Verify HMAC-SHA256 signature
   ├─ Check idempotency (Redis SETNX)
   ├─ Parse JSON payload
   └─ Filter: only payment.* events

3. Publish to Kafka
   Topic: payment.events
   Key: payment_id
   Payload: KafkaPaymentEvent struct

4. Async DB persist
   Table: webhook_events
   (non-blocking goroutine)

5. Return 200 OK
   {"status":"ok"}
   
Total time: < 500ms
```

### Testing Webhooks Locally

#### Option 1: Using ngrok (Recommended)

```bash
# Start ngrok
docker-compose up -d ngrok

# Get your public URL
curl http://localhost:4040/api/tunnels | jq -r '.tunnels[0].public_url'
# Output: https://abc123.ngrok.io

# Configure in Razorpay Dashboard:
# Settings → Webhooks → Add Webhook
# URL: https://abc123.ngrok.io/webhooks/razorpay
# Secret: (same as RAZORPAY_WEBHOOK_SECRET in .env)
# Events: payment.failed, payment.captured
```

#### Option 2: Using curl (Development)

```bash
# Fire a test payment.failed event
curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "Content-Type: application/json" \
  -H "X-Razorpay-Event-Id: evt_test_$(date +%s)" \
  -d '{
    "entity": "event",
    "account_id": "acc_demo",
    "event": "payment.failed",
    "contains": ["payment"],
    "payload": {
      "payment": {
        "entity": {
          "id": "pay_test_'$(date +%s)'",
          "amount": 499900,
          "currency": "INR",
          "status": "failed",
          "method": "upi",
          "error_code": "U30",
          "error_description": "Debit timeout",
          "bank": "HDFC",
          "vpa": "test@upi",
          "email": "test@example.com",
          "contact": "+919999999999",
          "created_at": '$(date +%s)'
        }
      }
    },
    "created_at": '$(date +%s)'
  }'
```

**Note:** If `RAZORPAY_WEBHOOK_SECRET` is not set, signature verification is skipped (dev mode).

---

## Generating Analytics Data

### Quick Start: Pre-Built Demo Data

```bash
# Clean slate + seed 4 demo cases
make demo-reset

# Wait 30 seconds for pipeline to process
# Then open: http://localhost:3000
```

This creates:
- ✅ **Case A:** ₹4,999 - recovered (U30 → retry succeeded)
- 🚫 **Case B:** ₹99 - not_worth_recovering (Z9 → negative ROI)
- 👤 **Case C:** ₹2,499 - customer_self_recovered
- 🌊 **Case D:** ₹8,999 - outage_batched (U28 bank down)

### Generating More Data

#### 1. **Using Demo Scenarios (Live Webhooks)**

```bash
# Fires 3 scenarios in 90 seconds:
# - 15× U28 failures → triggers outage detection
# - 1× U30 failure → full recovery pipeline
# - 1× Z9 failure → validator blocks (negative ROI)
make demo-scenarios
```

Watch logs in real-time:
```bash
make dev-logs
```

#### 2. **Manual Webhook Firing**

Create a test script `test_webhook.sh`:

```bash
#!/bin/bash
# Fire 100 random payment failures

for i in $(seq 1 100); do
  ERROR_CODES=("U30" "U28" "U16" "Z9" "RB" "BT")
  ERROR_CODE=${ERROR_CODES[$RANDOM % ${#ERROR_CODES[@]}]}
  
  AMOUNT=$((RANDOM % 50000 + 1000))00  # ₹100 to ₹50,000
  
  curl -s -X POST http://localhost:8080/webhooks/razorpay \
    -H "Content-Type: application/json" \
    -H "X-Razorpay-Event-Id: evt_test_${i}_${RANDOM}" \
    -d "{
      \"entity\": \"event\",
      \"event\": \"payment.failed\",
      \"payload\": {
        \"payment\": {
          \"entity\": {
            \"id\": \"pay_test_${i}_${RANDOM}\",
            \"amount\": ${AMOUNT},
            \"status\": \"failed\",
            \"method\": \"upi\",
            \"error_code\": \"${ERROR_CODE}\",
            \"bank\": \"HDFC\",
            \"vpa\": \"customer${i}@upi\",
            \"email\": \"customer${i}@test.com\",
            \"created_at\": $(date +%s)
          }
        }
      }
    }" > /dev/null
  
  printf "."
  sleep 0.2
done

echo "\n100 test payments fired!"
```

Run it:
```bash
chmod +x test_webhook.sh
./test_webhook.sh
```

#### 3. **Load Testing (k6)**

```bash
# Install k6
# macOS: brew install k6
# Windows: choco install k6
# Linux: snap install k6

# Run load test - 100 req/s for 2 minutes
make load-test-mock

# Or with real AI (first 20 calls only)
make load-test-real
```

#### 4. **Database Seeding**

```bash
# Seed fresh data (idempotent - cleans first)
make seed

# What it creates:
# - 1 merchant (acc_demo)
# - 20 customers with varied LTV
# - 50 payments (mix of captured + failed)
# - 4 demo recovery cases (one per scenario type)
```

### Verifying Analytics Data

```bash
# Check recovery cases count
docker-compose exec postgres psql -U recoverai -c \
  "SELECT status, COUNT(*) FROM recovery_cases GROUP BY status;"

# Check revenue metrics
docker-compose exec postgres psql -U recoverai -c \
  "SELECT 
     SUM(revenue_at_risk) as total_at_risk,
     SUM(amount_recovered) as total_recovered,
     COUNT(*) FILTER (WHERE status='recovered') as recovered_count
   FROM recovery_cases;"

# Check bank outage flags
docker-compose exec redis redis-cli KEYS "bank_outage:*"
```

---

## Makefile Commands Reference

### Development

```bash
# Start all services (mock AI - zero tokens)
make dev

# View logs (API + Worker + AI Service only)
make dev-logs

# Stop containers (preserves data)
make dev-stop

# Check AI mode (mock vs real)
make ai-status
```

### Database

```bash
# Run migrations
make migrate

# Seed demo data (1 merchant, 20 customers, 50 payments, 4 cases)
make seed

# Check migration status
make migrate-status

# Roll back one migration
make migrate-down

# Force migration version (if dirty state)
make migrate-force V=001
```

### Kafka

```bash
# Create all 7 topics
make kafka-topics

# List topics
docker-compose exec kafka /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --list

# View messages in a topic
docker-compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic payment.events \
  --from-beginning \
  --max-messages 10
```

### Testing

```bash
# Unit tests (no infrastructure required)
make test-unit

# Integration tests (requires docker-compose up)
make test-integration

# Real AI tests (exactly 11 Groq calls - one per UPI error code)
make test-real-ai

# All tests
make test-all

# Race detector
make test-race

# Coverage report
make test-coverage
```

### Load Testing

```bash
# k6 load test with mock AI (zero tokens)
make load-test-mock

# k6 with real AI (first 20 calls, then switches to mock)
make load-test-real
```

### Demo

```bash
# Full demo reset (seed + fire scenarios)
make demo-reset

# Fire live demo scenarios only
make demo-scenarios

# Pre-presentation checklist
make demo-checklist
```

### AI Mode Switching

```bash
# Switch to mock AI (edit .env + restart worker)
make ai-toggle-mock

# Switch to real AI (requires GROQ_API_KEY in .env)
make ai-toggle-real
```

### Build

```bash
# Build all Go binaries to ./bin/
make build

# Build Docker images
make docker-build

# Format code
make fmt

# Run go vet
make vet

# Run linter
make lint
```

### Maintenance

```bash
# DESTRUCTIVE: Stop + delete ALL volumes
make clean

# DESTRUCTIVE: Reset database schema only
make clean-db
```

---

## Testing the System

### 1. **End-to-End Workflow Test**

```bash
# Terminal 1: Start services
make dev

# Terminal 2: Watch logs
make dev-logs

# Terminal 3: Fire webhook
curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "Content-Type: application/json" \
  -H "X-Razorpay-Event-Id: evt_e2e_test" \
  -d '{
    "entity": "event",
    "event": "payment.failed",
    "payload": {
      "payment": {
        "entity": {
          "id": "pay_e2e_test",
          "amount": 499900,
          "status": "failed",
          "method": "upi",
          "error_code": "U30",
          "bank": "HDFC",
          "vpa": "test@upi",
          "email": "test@example.com",
          "created_at": '$(date +%s)'
        }
      }
    }
  }'

# Expected: Within 2 seconds, you should see in Terminal 2:
# 1. "webhook: event processed"
# 2. "risk_consumer: processed payment"
# 3. "validator_consumer: checks passed"
# 4. "execution_consumer: AI called"
# 5. "result_consumer: action executed"
```

### 2. **End-to-End Workflow Test**

```bash
# Terminal 1: Start services
make dev

# Terminal 2: Watch logs
make dev-logs

# Terminal 3: Fire webhook
curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "Content-Type: application/json" \
  -H "X-Razorpay-Event-Id: evt_e2e_test" \
  -d '{
    "entity": "event",
    "event": "payment.failed",
    "payload": {
      "payment": {
        "entity": {
          "id": "pay_e2e_test",
          "amount": 499900,
          "status": "failed",
          "method": "upi",
          "error_code": "U30",
          "bank": "HDFC",
          "vpa": "test@upi",
          "email": "test@example.com",
          "created_at": '$(date +%s)'
        }
      }
    }
  }'

# Expected: Within 2 seconds, you should see in Terminal 2:
# 1. "webhook: event processed"
# 2. "risk_consumer: processed payment"
# 3. "validator_consumer: checks passed"
# 4. "execution_consumer: AI called"
# 5. "result_consumer: action executed"

# Browser: Open http://localhost:3000/dashboard
# You should see a toast notification appear in bottom-right corner
```

---

### 3. **Toast Notification Test**

```powershell
# Windows PowerShell script to test toast notifications
# File: test_toast.ps1

# Open browser to http://localhost:3000/dashboard first
Write-Host "🧪 Testing Toast Notifications"
Write-Host "================================"
Write-Host "⚠️  IMPORTANT: Open http://localhost:3000/dashboard in your browser FIRST!"
Write-Host ""
Read-Host "Press Enter when browser is open and ready"

# Fire a U30 failure (should trigger success toast after recovery)
$response = Invoke-RestMethod -Uri "http://localhost:8080/webhooks/razorpay" `
  -Method POST `
  -ContentType "application/json" `
  -Body (@{
    entity = "event"
    event = "payment.failed"
    payload = @{
      payment = @{
        entity = @{
          id = "pay_test_toast_$([DateTimeOffset]::Now.ToUnixTimeSeconds())"
          amount = 499900
          status = "failed"
          method = "upi"
          error_code = "U30"
          bank = "HDFC"
          vpa = "test@upi"
          email = "test@example.com"
          created_at = [DateTimeOffset]::Now.ToUnixTimeSeconds()
        }
      }
    }
  } | ConvertTo-Json -Depth 10)

Write-Host "✅ Webhook sent successfully!"
Write-Host "⏳ Waiting 10 seconds for processing..."
Start-Sleep -Seconds 10
Write-Host ""
Write-Host "✨ CHECK YOUR BROWSER NOW!"
Write-Host "You should see a toast notification in the bottom-right corner:"
Write-Host "  • Green success toast if recovery succeeded"
Write-Host "  • Blue info toast if customer self-recovered"
Write-Host "  • Orange warning toast if needs approval"
```

Run it:
```powershell
.\test_toast.ps1
```

Expected toasts:
- 🟢 **Green success:** "Payment of ₹4,999.00 recovered successfully"
- 🔵 **Blue info:** "Customer self-recovered ₹2,499.00"
- ⚪ **Gray:** "Case stopped - not worth recovering"
- 🟠 **Orange warning:** "Human approval required for ₹8,999.00"

---

### 4. **Outage Detection Test**

```bash
# Fire 15 U28 failures rapidly
for i in $(seq 1 15); do
  curl -s -X POST http://localhost:8080/webhooks/razorpay \
    -H "X-Razorpay-Event-Id: evt_outage_$i" \
    -d '{
      "entity": "event",
      "event": "payment.failed",
      "payload": {
        "payment": {
          "entity": {
            "id": "pay_outage_'$i'",
            "amount": 100000,
            "status": "failed",
            "method": "upi",
            "error_code": "U28",
            "bank": "SBI",
            "vpa": "test'$i'@upi",
            "email": "test@example.com",
            "created_at": '$(date +%s)'
          }
        }
      }
    }' > /dev/null
  printf "."
  sleep 0.3
done

# Check Redis for outage flag
docker-compose exec redis redis-cli EXISTS bank_outage:U28
# Expected: (integer) 1

# Check TTL
docker-compose exec redis redis-cli TTL bank_outage:U28
# Expected: ~3500 seconds (1 hour)
```

### 5. **Customer Self-Recovery Test**

```bash
# Step 1: Fire payment.failed
curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "X-Razorpay-Event-Id: evt_self_fail" \
  -d '{
    "entity": "event",
    "event": "payment.failed",
    "payload": {
      "payment": {
        "entity": {
          "id": "pay_self_recovery",
          "amount": 250000,
          "status": "failed",
          "method": "upi",
          "error_code": "U16",
          "bank": "AXIS",
          "vpa": "test@upi",
          "email": "test@example.com",
          "created_at": '$(date +%s)'
        }
      }
    }
  }'

# Wait 5 seconds

# Step 2: Customer pays manually - fire payment.captured
curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "X-Razorpay-Event-Id: evt_self_cap" \
  -d '{
    "entity": "event",
    "event": "payment.captured",
    "payload": {
      "payment": {
        "entity": {
          "id": "pay_self_recovery",
          "amount": 250000,
          "status": "captured",
          "method": "upi",
          "bank": "AXIS",
          "vpa": "test@upi",
          "email": "test@example.com",
          "created_at": '$(date +%s)'
        }
      }
    }
  }'

# Check dashboard: case should show status "customer_self_recovered"
```

### 6. **Policy Engine Test**

```bash
# Fire a Z9 failure with tiny amount (₹99) from new customer
# Expected: Validator blocks with "not_worth_recovering"

curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "X-Razorpay-Event-Id: evt_roi_test" \
  -d '{
    "entity": "event",
    "event": "payment.failed",
    "payload": {
      "payment": {
        "entity": {
          "id": "pay_roi_test",
          "amount": 9900,
          "status": "failed",
          "method": "upi",
          "error_code": "Z9",
          "bank": "KOTAK",
          "vpa": "newcustomer@upi",
          "email": "newcustomer@example.com",
          "created_at": '$(date +%s)'
        }
      }
    }
  }'

# Check case detail:
# - Status: not_worth_recovering
# - Validator Check 4: ✗ FAIL
# - Skip reason: "Recovery ROI ₹-XX.XX below threshold"
# - AI diagnosis: null (never called)
```

---

## Troubleshooting

### Services Not Starting

```bash
# Check all containers
docker-compose ps

# View specific service logs
docker-compose logs api --tail=50
docker-compose logs worker --tail=50
docker-compose logs postgres --tail=50

# Restart specific service
docker-compose restart worker
```

### Webhook Not Working

```bash
# Check API logs
docker-compose logs api --tail=100 | grep webhook

# Test locally (no signature check if webhook secret not set)
curl -v -X POST http://localhost:8080/webhooks/razorpay \
  -H "Content-Type: application/json" \
  -d '{"entity":"event","event":"payment.failed","payload":{"payment":{"entity":{"id":"test","amount":100000,"status":"failed","method":"upi","error_code":"U30","created_at":'$(date +%s)'}}}}}'
```

### No Cases Appearing in Dashboard

```bash
# Check if webhook was received
docker-compose exec postgres psql -U recoverai -c \
  "SELECT COUNT(*) FROM webhook_events;"

# Check if payments were created
docker-compose exec postgres psql -U recoverai -c \
  "SELECT COUNT(*) FROM payments;"

# Check if recovery cases were created
docker-compose exec postgres psql -U recoverai -c \
  "SELECT COUNT(*) FROM recovery_cases;"

# Check Kafka messages
docker-compose exec kafka /opt/kafka/bin/kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --describe --all-groups
```

### Worker Not Processing

```bash
# Check worker status
docker-compose logs worker --tail=100

# Check Kafka lag
docker-compose exec kafka /opt/kafka/bin/kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --describe --group recovery-worker

# Restart worker
docker-compose restart worker
```

---

## Next Steps

1. **Set up Razorpay webhooks** using ngrok
2. **Generate test data** with `make demo-reset`
3. **Open dashboard** at http://localhost:3000
4. **Fire test webhooks** to see the pipeline in action
5. **Monitor logs** with `make dev-logs`

**Questions?** Check the `DEMO_SCRIPT.md` for live presentation examples!
