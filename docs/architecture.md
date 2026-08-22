# RecoverAI — Architecture Document
## Razorpay Build · Track 03: AI Revenue Recovery · Research-Enforced v2

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [The Five-Stage Pipeline](#2-the-five-stage-pipeline)
3. [Service Breakdown](#3-service-breakdown)
4. [Data Flow — Step by Step](#4-data-flow--step-by-step)
5. [Kafka Topics](#5-kafka-topics)
6. [Database Schema](#6-database-schema)
7. [Redis Key Design](#7-redis-key-design)
8. [AI Agent Architecture](#8-ai-agent-architecture)
9. [Policy Engine Rules](#9-policy-engine-rules)
10. [Pre-Recovery Validator](#10-pre-recovery-validator)
11. [Edge Case Handling](#11-edge-case-handling)
12. [API Routes](#12-api-routes)
13. [Directory Structure](#13-directory-structure)
14. [Environment Variables](#14-environment-variables)
15. [Docker Services](#15-docker-services)

---

## 1. System Overview

RecoverAI is an event-driven, AI-assisted payment recovery platform. When a Razorpay payment fails, the system detects it, diagnoses the root cause, selects the optimal recovery strategy, executes a bounded action, and measures the money recovered — all autonomously, with a complete audit trail.

```
The core loop:

  payment.failed
       ↓
  Understand failure (Risk Engine)
       ↓
  Validate recoverability (Pre-Recovery Validator)
       ↓
  Select strategy (AI Agents)
       ↓
  Gate the decision (Policy Engine)
       ↓
  Execute recovery action
       ↓
  Observe result
       ↓
  Measure recovered revenue
```

**Critical design constraint:** The AI never executes financial operations directly. Every AI decision passes through a deterministic Policy Engine before any action is taken. The AI produces structured JSON commands only.

---

## 2. The Five-Stage Pipeline

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        RECOVERAI PIPELINE                                │
└─────────────────────────────────────────────────────────────────────────┘

STAGE 1 — INGESTION
────────────────────
  Razorpay Webhooks
       │  HTTPS POST /webhooks/razorpay
       ▼
  Go API Gateway
  ├── HMAC-SHA256 signature verify (X-Razorpay-Signature)
  ├── Redis idempotency check (X-Razorpay-Event-Id, 24h TTL)
  ├── Write to webhook_events table (async)
  └── Publish to Kafka: payment.events
       │  Returns HTTP 200 in < 100ms always
       ▼

STAGE 2 — RISK ENGINE
──────────────────────
  Kafka Consumer: payment.events
       │  Consumer group: risk-processor-group
       ▼
  Risk Engine (Go Worker)
  ├── Classify UPI error code (11 codes, TD vs BD taxonomy)
  ├── Score risk: amount × customer_value × failure_type multipliers
  ├── Detect bank outage: Redis counter per error code per 5-min window
  ├── Create recovery_case in PostgreSQL
  └── Publish to Kafka: revenue.risk
       ▼

STAGE 3 — PRE-RECOVERY VALIDATOR  ← NEW COMPONENT (added from research)
──────────────────────────────────
  Kafka Consumer: revenue.risk
       │  Consumer group: validator-group
       ▼
  Pre-Recovery Validator (Go Worker)
  ├── Check 1: Is payment already captured? (Razorpay API call)
  ├── Check 2: Is bank outage active? (Redis flag)
  ├── Check 3: RBI mandate compliance? (24h window, ₹15K threshold)
  ├── Check 4: Is recovery ROI positive? (amount × probability - cost)
  ├── Check 5: Is error retryable? (inject force_payment_link flag)
  └── Check 6: Max retries reached?
       │
       ├── ANY CHECK FAILS → Kafka: recovery.blocked
       │                     (status: customer_self_recovered /
       │                      outage_batched / not_worth_recovering /
       │                      pending_human_approval / failed)
       │
       └── ALL PASS → Kafka: recovery.commands (with AI call)
       ▼

STAGE 4 — AI RECOVERY SERVICE
───────────────────────────────
  Go Worker calls FastAPI /analyze
       ▼
  Python FastAPI + LangGraph + Groq (llama-3.3-70b-versatile)
  ├── Agent 1: Risk Analyst
  │   └── Output: recovery_probability, failure_type, timing_penalty
  ├── Agent 2: Recovery Strategist
  │   └── Output: strategy, delay_minutes, confidence
  └── Agent 3: Executor Command Builder
      └── Output: structured JSON command (RETRY_PAYMENT / etc.)
       │  All outputs validated as strict JSON before returning
       ▼

STAGE 5 — POLICY ENGINE + EXECUTION
─────────────────────────────────────
  Policy Engine (Go) — 10 deterministic rules
       │
       ├── BLOCKED → human_approval queue or stop
       │
       └── APPROVED → Execution Worker
                      ├── RETRY_PAYMENT    → Razorpay API
                      ├── PAYMENT_LINK     → Razorpay API
                      ├── SEND_NOTIFICATION → notification.events
                      ├── ESCALATE         → merchant dashboard alert
                      └── STOP             → case closed
                           │
                           ▼
                      Result Processor
                      ├── Update recovery_cases (recovered / partial)
                      ├── Update customer lifetime_value
                      ├── Write audit_log entry
                      └── Publish to Kafka: recovery.results
```

---

## 3. Service Breakdown

### 3.1 Go API Gateway (`cmd/api`)

| Responsibility | Detail |
|----------------|--------|
| Webhook ingestion | POST /webhooks/razorpay — HMAC verify, idempotency, Kafka publish |
| REST API | JWT-authenticated endpoints for dashboard and merchants |
| Rate limiting | Redis sliding window per merchant |
| Auth | JWT issue and validation |

**Key behaviour:** The webhook handler never blocks on AI or heavy DB writes. It validates the signature, checks idempotency in Redis, publishes to Kafka, and returns 200 in under 100ms. Razorpay retries if it does not receive 200 within 5 seconds — this handler ensures it always does.

### 3.2 Go Worker (`cmd/worker`)

Runs five Kafka consumers as goroutines:

| Consumer | Input Topic | Output |
|----------|-------------|--------|
| Payment Processor | payment.events | Upserts payments table, updates customer stats |
| Risk Engine | payment.events | Creates recovery_cases, publishes revenue.risk |
| Pre-Recovery Validator | revenue.risk | Routes to recovery.commands or recovery.blocked |
| Execution Worker | recovery.commands | Runs Policy Engine, calls Razorpay API |
| Result Processor | recovery.results | Closes cases, updates audit trail |

### 3.3 AI Recovery Service (`ai-service/`)

| Component | Detail |
|-----------|--------|
| Framework | FastAPI + LangGraph |
| LLM Primary | Groq llama-3.3-70b-versatile (same API key as AI Career Copilot) |
| LLM Fallback | Gemini Flash — switch via `LLM_PROVIDER=gemini` env var |
| Agents | 3 sequential agents (Risk Analyst → Strategist → Executor) |
| Output | Structured JSON only — no free text in control path |
| Validation | Non-conforming agent output → retry once → fallback to STOP command |

### 3.4 Next.js Dashboard (`frontend/`)

| Feature | Detail |
|---------|--------|
| Polling | Overview API every 5 seconds — live metric cards |
| Screens | Overview · Recovery Cases · Case Detail (audit timeline) |
| Status badges | 9 statuses with distinct colours |
| Case Detail | Full audit timeline + AI decision breakdown + validator checks |
| Honest Exceptions | Dedicated view for unrecovered cases with reasons |

---

## 4. Data Flow — Step by Step

### Happy Path: Transient UPI Failure → Recovered

```
t=0s    Razorpay fires payment.failed (UPI error U30, ₹4,999, cus_abc)

t=0s    Go API receives POST /webhooks/razorpay
        → HMAC-SHA256 verified ✓
        → Redis SETNX webhook:idempotency:{event_id} — new key ✓
        → webhook_events row inserted
        → Kafka: payment.events published
        → HTTP 200 returned

t=0.1s  Payment Processor consumer reads payment.events
        → payments row upserted (status=failed, upi_error_code=U30)
        → customer.failed_payments++

t=0.2s  Risk Engine consumer reads payment.events
        → U30 classified: TD (Technical Decline), transient_bank_debit_fail
        → Bank outage check: bank_failures:U30:{window} = 1 (no outage)
        → Risk score = 4999/100 × 1.5 (7 prior successes) × 1.4 (TD) = 104.97
        → Priority = CRITICAL (score > 1.5 after normalisation)
        → recovery_cases row created (status=open)
        → Kafka: revenue.risk published

t=0.4s  Pre-Recovery Validator reads revenue.risk
        → Check 1: GET /v1/payments/pay_xxx → status=failed ✓ PASS
        → Check 2: GET bank_outage:U30 → nil ✓ PASS
        → Check 3: is_mandate_payment=false ✓ PASS
        → Check 4: ROI = (4999 × 0.82) - 0 = ₹40.99 > 0 ✓ PASS
        → Check 5: U30 not in [YG,Z8] ✓ PASS
        → Check 6: retry_count=0 < max_retries=2 ✓ PASS
        → All 6 passed → calls FastAPI /analyze

t=0.6s  AI Service receives /analyze request
        → Agent 1 (Risk Analyst):
            failure_category=TD, recovery_probability=0.82
            timing_penalty_applied=false (time is 2PM, not 7-10PM peak)
            priority=high
        → Agent 2 (Strategist):
            strategy=retry_payment, delay_minutes=10, confidence=0.91
            recovery_window_reason="TD failure outside peak hours"
        → Agent 3 (Executor):
            action=RETRY_PAYMENT, scheduled_at_minutes=10
        → Kafka: recovery.commands published
        → recovery_cases.ai_diagnosis, ai_strategy updated

t=0.7s  Execution Worker reads recovery.commands
        → Policy Engine runs 10 rules:
            R1: U30 not in [Z9,YG,Z8,U68] ✓ PASS
            R2: force_payment_link=false ✓ PASS
            R3: bank_outage_detected=false ✓ PASS
            R4: is_mandate_payment=false ✓ PASS
            R5: is_mandate_payment=false ✓ PASS
            R6: ₹4,999 < ₹10,000 ceiling ✓ PASS
            R7: ₹4,999 < ₹50,000 threshold ✓ PASS
            R8: retry_count=0 < 2 ✓ PASS
            R9: cooldown_until=nil ✓ PASS
            R10: RETRY_PAYMENT in allowed_actions ✓ PASS
        → Decision: APPROVED
        → Redis: recovery:cooldown:{case_id} EX 300 (5 min)
        → recovery_cases.retry_count = 1
        → recovery_actions row created (status=pending)

t=600s  (10 minutes later) Scheduled retry executes
        → POST /v1/payments/{id}/capture to Razorpay Test Mode API
        → Razorpay fires payment.captured webhook

t=600s  Payment Processor reads payment.captured
        → payments.status = captured
        → customer.successful_payments++, lifetime_value += 4999

t=600s  Result Processor reads recovery.results
        → amount_recovered = 499900 (paise)
        → recovery_probability == amount_recovered/revenue_at_risk → FULL
        → recovery_cases.status = recovered
        → recovery_cases.resolved_at = NOW()
        → audit_log: actor=execution_worker, action=payment_captured
        → Kafka: audit.events published

t=600s  Dashboard polls /api/v1/analytics/overview
        → recovered_revenue_paise += 499900
        → "Recovered Revenue" card ticks up: ₹5,68,001 → ₹5,73,000
```

### Edge Path: Customer Self-Recovery

```
t=0s    payment.failed received → recovery_case opened (status=open)

t=180s  Customer opens PhonePe, retries manually, payment succeeds

t=181s  Razorpay fires payment.captured for SAME payment_id

t=181s  Payment Processor reads payment.captured
        → Checks recovery_cases WHERE payment_id=X AND status IN
          [open, in_progress, outage_batched, pending_human_approval]
        → FOUND: open case exists
        → recovery_cases.status = customer_self_recovered
        → recovery_cases.amount_recovered = 499900
        → All pending recovery_actions CANCELLED
        → audit_log: actor=customer_self, action=self_recovered
        → If validator was in progress: abort (Redis cancellation flag)

Result: No system action taken. No double-billing. ₹4,999 marked recovered.
        Dashboard: customer_self_recovered_count++
```

### Edge Path: Bank Outage Cascade

```
t=0s    15 payment.failed events arrive, all error code U28, within 90 seconds

t=0.1s  Risk Engine processes first event:
        → INCR bank_failures:U28:{window} = 1
        → Below threshold (10), individual scoring proceeds

t=30s   Risk Engine processes 10th event:
        → INCR bank_failures:U28:{window} = 10
        → THRESHOLD REACHED
        → SET bank_outage:U28 "1" EX 3600
        → INSERT bank_outage_events (code=U28, count=10, window=5)
        → This case: bank_outage_detected=true, status=outage_batched
        → cooldown_until = NOW() + 60 min
        → Kafka: recovery.blocked (not recovery.commands)
        → audit_log: actor=risk_engine, action=outage_detected

t=30s+  All subsequent U28 failures:
        → Validator Check 2: GET bank_outage:U28 → "1" → SKIP
        → status = outage_batched immediately
        → Zero AI calls fired for outage failures
        → Zero Razorpay API calls fired

t=90m   bank_outage:U28 TTL expires → key gone
        → Scheduled batch job re-queues outage_batched cases
        → Individual recovery resumes with fresh timing
```

---

## 5. Kafka Topics

```
Topics (7 total):

  payment.events          partitions=3  retention=7d
  ├── Published by: Go API webhook handler
  └── Consumed by: Payment Processor, Risk Engine

  revenue.risk            partitions=3  retention=3d
  ├── Published by: Risk Engine
  └── Consumed by: Pre-Recovery Validator

  recovery.commands       partitions=2  retention=1d
  ├── Published by: Pre-Recovery Validator (after AI call)
  └── Consumed by: Execution Worker

  recovery.blocked        partitions=2  retention=3d  ← NEW
  ├── Published by: Pre-Recovery Validator (when any check fails)
  └── Consumed by: Result Processor (for audit + dashboard)

  recovery.results        partitions=2  retention=7d
  ├── Published by: Execution Worker
  └── Consumed by: Result Processor

  notification.events     partitions=2  retention=1d
  ├── Published by: Execution Worker (for SEND_NOTIFICATION action)
  └── Consumed by: Notification Worker (SMS/email dispatch)

  audit.events            partitions=1  retention=30d
  ├── Published by: Result Processor
  └── Consumed by: Audit Writer (PostgreSQL insert)
```

**Consumer groups:**

| Consumer | Group ID |
|----------|----------|
| Payment Processor | payment-processor-group |
| Risk Engine | risk-processor-group |
| Pre-Recovery Validator | validator-group |
| Execution Worker | execution-worker-group |
| Result Processor | result-processor-group |

All consumers use **manual offset commits** and implement **exponential backoff** with max 3 retries before dead-letter routing.

---

## 6. Database Schema

### Tables (9 total, up from original 8)

```sql
-- Core entities
merchants            -- Razorpay merchant accounts
customers            -- Customer profiles + payment history
payments             -- All payment attempts (captured + failed)

-- Recovery workflow
recovery_cases       -- One per failed payment needing recovery
recovery_actions     -- Individual actions taken per case
recovery_policies    -- Per-merchant rules (default + custom)

-- Infrastructure
webhook_events       -- Idempotency + event log
audit_logs           -- Complete immutable audit trail
bank_outage_events   -- NEW: outage detection events
```

### Key Columns Added by Research

**recovery_cases:**
```
status               VARCHAR(30)   -- 9 valid values (expanded from 5)
recovery_roi         DECIMAL(10,2) -- (amount × probability) - cost
upi_error_category   VARCHAR(20)   -- 'TD' or 'BD'
bank_outage_detected BOOLEAN       -- outage batch flag
is_mandate_payment   BOOLEAN       -- RBI compliance flag
rbi_minimum_retry_at TIMESTAMPTZ   -- 24h mandate rule enforcement
validator_skip_reason TEXT         -- why AI was not called
partial_recovery     BOOLEAN       -- partial payment detected
```

**recovery_cases status values:**
```
open                     -- just created, processing
in_progress              -- recovery action executing
recovered                -- full amount captured
partially_recovered      -- less than original amount captured
failed                   -- all retries exhausted
stopped                  -- merchant stopped recovery
customer_self_recovered  -- customer paid before system acted
outage_batched           -- bank outage — waiting
not_worth_recovering     -- negative ROI — system chose to stop
pending_human_approval   -- amount or mandate threshold exceeded
```

**recovery_policies (new columns):**
```
mandate_min_retry_hours      INT    DEFAULT 24     -- RBI compliance
high_value_threshold_paise   BIGINT DEFAULT 1500000 -- ₹15K RBI rule
min_recovery_roi             DECIMAL DEFAULT 0     -- economic gate
outage_detection_threshold   INT    DEFAULT 10     -- failures/5min
```

**audit_logs actor values:**
```
system | risk_engine | validator | ai_agent |
policy_engine | execution_worker | human | customer_self
```

---

## 7. Redis Key Design

```
# Webhook idempotency
webhook:idempotency:{razorpay_event_id}     TTL: 86400s (24h)
Value: "1"

# Rate limiting (sliding window)
ratelimit:{merchant_id}:{window_minute}     TTL: 120s
Value: count (integer)

# Bank outage detection counter
bank_failures:{upi_error_code}:{unix_ts/300}  TTL: 600s (10min)
Value: count (integer, incremented per failure)

# Bank outage active flag
bank_outage:{upi_error_code}               TTL: 3600s (1h)
Value: "1"

# Recovery cooldown per case
recovery:cooldown:{case_id}               TTL: cooldown_minutes × 60
Value: "1"

# Distributed lock for execution (prevent double-execution)
recovery:lock:{case_id}                   TTL: 30s
Value: worker_id

# Customer risk score cache
customer:risk:{customer_id}               TTL: 3600s
Value: JSON {risk_score, recovery_probability, last_updated}

# Validator cancellation signal (for outage → ongoing validation)
recovery:cancel:{case_id}                 TTL: 300s
Value: "1"
```

---

## 8. AI Agent Architecture

### Agent Flow (LangGraph Sequential Graph)

```
Input (from Go validator)
       │
       ▼
┌─────────────────────┐
│  Agent 1            │
│  Risk Analyst       │
│                     │
│  Input:             │
│  • payment_id       │
│  • amount_paise     │
│  • upi_error_code   │
│  • upi_error_cat    │
│  • failure_type     │
│  • time_of_failure  │  ← IST hour for timing penalty
│  • force_pay_link   │  ← injected by validator
│  • customer_history │
│  • risk_score       │
│                     │
│  Output JSON:       │
│  • recovery_prob    │
│  • failure_category │
│  • failure_type     │
│  • timing_penalty   │
│  • priority         │
│  • reasoning        │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│  Agent 2            │
│  Strategist         │
│                     │
│  Input:             │
│  • Agent 1 output   │
│  • merchant_policy  │
│  • force_pay_link   │
│                     │
│  Output JSON:       │
│  • strategy         │  ← 6 allowed values only
│  • confidence       │
│  • delay_minutes    │  ← highest-impact variable
│  • window_reason    │
│  • message_template │
│  • reasoning        │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│  Agent 3            │
│  Executor Cmd       │
│                     │
│  Input:             │
│  • Agent 2 output   │
│  • payment_id       │
│  • case_id          │
│                     │
│  Output JSON:       │
│  • action           │  ← 5 allowed values only
│  • payment_id       │
│  • case_id          │
│  • scheduled_at_min │
│  • parameters       │
└─────────┬───────────┘
          │
          ▼
   Strict JSON validation
   │
   ├── VALID → return command to Go validator
   └── INVALID → retry once → fallback: STOP command
```

### Strategy Allowed Values (Agent 2)

```
retry_payment
generate_payment_link
notify_customer
schedule_retry
escalate_to_merchant
stop_recovery
```

### Action Allowed Values (Agent 3)

```
RETRY_PAYMENT
GENERATE_PAYMENT_LINK
SEND_NOTIFICATION
ESCALATE
STOP
```

### Critical System Prompt Rules (baked into Agent 2)

```
- If failure_type is risk_blocked (YG): strategy MUST be escalate_to_merchant
- If failure_type is non_retryable_auto (Z9, Z8): strategy MUST NOT be retry_payment
- If force_payment_link is true: strategy MUST NOT be retry_payment
- If recovery_probability < 0.3 AND customer LTV < ₹5,000: strategy MUST be stop_recovery
- If time_of_failure_hour is 19–22: delay_minutes MUST be >= 480 (wait for morning)
- If failure is insufficient_funds (U16, Z9): delay_minutes MUST be >= 1440 (24h)
- If amount > ₹50,000: strategy MUST be escalate_to_merchant or generate_payment_link
```

---

## 9. Policy Engine Rules

Ten rules evaluated in order. First BLOCK wins. No randomness. No AI.

```
Rule 1 — Non-retryable UPI error codes
  IF action == RETRY_PAYMENT
  AND upi_error_code IN [Z9, YG, Z8, U68]
  → BLOCK: "UPI code {X} is non-retryable"

Rule 2 — Force payment link override
  IF force_payment_link == true
  AND action == RETRY_PAYMENT
  → BLOCK: "Validator flagged: retry not permitted for this error type"

Rule 3 — Bank outage active
  IF bank_outage_detected == true
  AND action == RETRY_PAYMENT
  → BLOCK: "Bank outage active — retry batched, do not execute now"

Rule 4 — RBI mandate minimum retry window
  IF is_mandate_payment == true
  AND rbi_minimum_retry_at != nil
  AND time.Now() < rbi_minimum_retry_at
  → BLOCK: "RBI mandate: minimum 24h between retries not elapsed"

Rule 5 — RBI high-value mandate
  IF is_mandate_payment == true
  AND amount > policy.HighValueThresholdPaise (₹15,000)
  → BLOCK + RequiresHumanApproval: "RBI: mandate >₹15K needs customer approval"

Rule 6 — Amount ceiling for auto-retry
  IF action == RETRY_PAYMENT
  AND amount > policy.MaxRetryAmountPaise (default ₹10,000)
  → BLOCK + RequiresHumanApproval: "Amount exceeds auto-retry ceiling"

Rule 7 — High value always requires human
  IF amount > policy.RequireHumanAbovePaise (default ₹50,000)
  → BLOCK + RequiresHumanApproval: "Amount >₹{X} requires human approval"

Rule 8 — Max retries reached
  IF retry_count >= policy.MaxRetries (default 2)
  → BLOCK: "Maximum retries ({N}) reached"

Rule 9 — Cooldown active
  IF cooldown_until != nil
  AND cooldown_until > time.Now()
  → BLOCK: "Cooldown active until {time}"

Rule 10 — Action not in allowlist
  IF action NOT IN policy.AllowedActions
  → BLOCK: "Action {X} not in merchant's allowed actions"

No rule fired → APPROVED
```

**Default policy values:**

| Policy | Default | Meaning |
|--------|---------|---------|
| max_retry_amount_paise | 1,000,000 | Auto-retry up to ₹10,000 |
| max_retries | 2 | Max 2 retry attempts per payment |
| retry_cooldown_minutes | 5 | Min 5 min between retries |
| require_human_above | 5,000,000 | Human approval for > ₹50,000 |
| high_value_threshold_paise | 1,500,000 | RBI mandate threshold ₹15,000 |
| mandate_min_retry_hours | 24 | RBI compliance minimum |
| outage_detection_threshold | 10 | Failures per 5-min window |
| min_recovery_roi | 0 | Stop if ROI is negative |

---

## 10. Pre-Recovery Validator

The validator is a pure Go function (`internal/validator/pre_recovery.go`) called by the AI Recovery Consumer before any HTTP call to the Python AI service.

```
func ValidateBeforeAI(ctx context.Context, case RecoveryCase) (ValidationResult, error)

type ValidationResult struct {
    ShouldProceed    bool
    SkipReason       string
    NewStatus        string
    ForcePaymentLink bool   // injected into AI request if Check 5 fires
}
```

### The Six Checks

```
CHECK 1 — Late Authorisation (payment already captured)
  → Razorpay API: GET /v1/payments/{razorpay_payment_id}
  → If response.status == "captured":
       NewStatus = "customer_self_recovered"
       ShouldProceed = false
       Reason = "Payment already captured — customer self-recovered"
  → Adds ~100-200ms latency. Always worth it to prevent double-billing.

CHECK 2 — Bank Outage Active
  → Redis: GET bank_outage:{upi_error_code}
  → If key exists:
       NewStatus = "outage_batched"
       ShouldProceed = false
       Set cooldown_until = NOW() + 60 minutes
       Reason = "Bank outage active for {code} — batched for {time}"

CHECK 3 — RBI Mandate Compliance
  → If payment.is_mandate_payment == true:
       If time.Now() < case.rbi_minimum_retry_at:
           NewStatus = "pending_human_approval" (time-based)
           ShouldProceed = false
           Reason = "RBI mandate: 24h minimum not elapsed"
       If amount > 1,500,000 paise (₹15,000):
           NewStatus = "pending_human_approval"
           ShouldProceed = false
           Reason = "RBI: mandate amounts >₹15,000 require customer approval"

CHECK 4 — Recovery ROI
  → roi = (amount × recovery_probability) - estimated_action_cost
  → Costs: RETRY=0, PAYMENT_LINK=0, SMS=50, HUMAN_ESCALATION=10000 (paise)
  → If roi < policy.MinRecoveryROI:
       NewStatus = "not_worth_recovering"
       ShouldProceed = false
       Reason = "Recovery ROI ₹{X} below threshold — not cost effective"

CHECK 5 — Error Retryability (does NOT stop pipeline)
  → If upi_error_code IN [YG, Z8]:
       ForcePaymentLink = true
       ShouldProceed = true  ← AI still runs
       Reason injected into AI request as constraint
  → Both Agent 2 system prompt AND Policy Engine Rule 1 enforce this

CHECK 6 — Max Retries Already Hit
  → If case.retry_count >= case.max_retries:
       NewStatus = "failed"
       ShouldProceed = false
       Reason = "Max retries ({N}) already reached"
```

---

## 11. Edge Case Handling

### EC-1: payment.failed → payment.captured (Same Payment ID)

**Root cause:** Customer retries manually in UPI app after system receives payment.failed.

**Detection:** `payment.captured` webhook handler checks for open recovery_case with same payment_id.

```go
// In Payment Processor, on payment.captured event:
openCase, err := db.FindOpenRecoveryCase(ctx, payment.RazorpayPaymentID)
if openCase != nil {
    db.UpdateRecoveryCaseStatus(ctx, openCase.ID, "customer_self_recovered")
    db.CancelPendingActions(ctx, openCase.ID)
    db.SetAmountRecovered(ctx, openCase.ID, payment.Amount)
    audit.Log(ctx, "customer_self", "self_recovered", openCase.ID)
    redis.Set(ctx, fmt.Sprintf("recovery:cancel:%s", openCase.ID), "1", 5*time.Minute)
}
```

### EC-2: Duplicate Webhook Storm

**Root cause:** Razorpay at-least-once delivery — retries if no 200 within 5 seconds.

**Two-layer protection:**
- Layer 1: Redis `SETNX webhook:idempotency:{event_id}` — catches exact duplicates
- Layer 2: PostgreSQL `UNIQUE(razorpay_payment_id)` on recovery_cases — catches race conditions

### EC-3: Out-of-Order Webhooks

**Root cause:** Network delays can deliver payment.captured before payment.failed.

**Protection in Payment Processor:**
```
On payment.failed:
  IF payments.status == 'captured' in DB:
    → Log "late_failure_webhook_for_captured_payment"
    → Skip recovery case creation entirely
    → Return (do not publish to revenue.risk)
```

### EC-4: Bank Outage Cascade

**Root cause:** NPCI or bank infrastructure failure affects many payments simultaneously.

**Detection via Redis counters:**
```
key = bank_failures:{error_code}:{unix_ts / 300}
INCR key; EXPIRE key 600

IF count > OutageDetectionThreshold (default 10):
    SET bank_outage:{error_code} "1" EX 3600
    INSERT bank_outage_events
```

**Real events this handles:** April 12, 2025 (IPL season), March 31, 2025 (FY-end)

### EC-5: Partial Payment on Recovery

**Root cause:** Customer pays different amount on generated payment link.

**Detection in Result Processor:**
```
if amount_recovered == revenue_at_risk:
    status = "recovered"
elif amount_recovered > 0:
    status = "partially_recovered"
    partial_recovery = true
    // Dashboard shows separately — honest reporting
```

### EC-6: Late Authorisation

**Root cause:** Net Banking responses can be delayed. Razorpay marks payment failed, then receives bank confirmation later.

**Protection in Validator Check 1:** Always call `GET /v1/payments/{id}` before proceeding. If status is already `captured`, abort recovery with `customer_self_recovered`.

### EC-7: RBI Compliance Violations

**Root cause:** Mandate/recurring payments have legally mandated retry rules.

**Rules enforced:**
- Minimum 24-hour gap between retry attempts (Validator Check 3 + Policy Rule 4)
- Transactions > ₹15,000 require explicit customer approval (Policy Rule 5)
- Logged in audit trail with compliance reason

---

## 12. API Routes

```
# Public (no auth)
POST   /webhooks/razorpay              Razorpay webhook receiver
GET    /health                         Service health check
GET    /ready                          Readiness probe

# Auth
POST   /api/v1/auth/login
POST   /api/v1/auth/refresh

# Recovery Cases (JWT required)
GET    /api/v1/recovery/cases          List with filters
GET    /api/v1/recovery/cases/:id      Case detail + full audit trail
POST   /api/v1/recovery/cases/:id/approve    Human approval
POST   /api/v1/recovery/cases/:id/stop       Stop recovery

# Analytics (JWT required)
GET    /api/v1/analytics/overview             Dashboard summary (polled every 5s)
GET    /api/v1/analytics/recovery-rate        By failure_type, method, upi_error_code
GET    /api/v1/analytics/revenue              Time-series (hour/day intervals)
GET    /api/v1/analytics/honest-exceptions    Cases that could not be recovered + why
GET    /api/v1/analytics/ai-performance       Agent accuracy vs actual outcomes

# Merchant Config (JWT required)
GET    /api/v1/merchants/:id/policy
PUT    /api/v1/merchants/:id/policy
```

---

## 13. Directory Structure

```
recoverai/
├── cmd/
│   ├── api/
│   │   └── main.go              HTTP server (Go API Gateway)
│   ├── worker/
│   │   └── main.go              Kafka consumer runner (all 5 consumers)
│   └── seed/
│       └── main.go              Demo data seeder
│
├── internal/
│   ├── config/                  Viper env config
│   ├── db/
│   │   ├── migrations/          golang-migrate SQL files (9 tables)
│   │   ├── queries/             sqlc .sql query files
│   │   └── sqlc/                Generated Go database code
│   ├── handlers/
│   │   ├── webhook.go           POST /webhooks/razorpay
│   │   ├── recovery.go          Recovery case REST endpoints
│   │   ├── merchant.go          Policy management endpoints
│   │   └── analytics.go         Dashboard + honest-exceptions endpoints
│   ├── middleware/
│   │   ├── auth.go              JWT validation
│   │   ├── ratelimit.go         Redis sliding window
│   │   └── idempotency.go       Webhook deduplication
│   ├── kafka/
│   │   ├── producer.go          Kafka message publishing
│   │   ├── consumer.go          Consumer group base implementation
│   │   └── topics.go            Topic name constants
│   ├── validator/
│   │   └── pre_recovery.go      ← NEW: 6-check gate before AI
│   ├── outage/
│   │   └── detector.go          ← NEW: Redis counter outage detection
│   ├── policy/
│   │   └── engine.go            10-rule deterministic policy engine
│   ├── redis/
│   │   └── client.go            Redis connection + key helpers
│   ├── services/
│   │   ├── risk.go              Risk scoring + UPI taxonomy
│   │   ├── recovery.go          Recovery orchestration
│   │   └── razorpay.go          Razorpay Test Mode API client
│   └── models/
│       └── types.go             Shared structs and constants
│
├── ai-service/
│   ├── main.py                  FastAPI app + /analyze endpoint
│   ├── llm.py                   Provider factory (Groq / Gemini)
│   ├── agents/
│   │   ├── risk_analyst.py      Agent 1: risk assessment
│   │   ├── strategist.py        Agent 2: recovery strategy
│   │   └── executor_cmd.py      Agent 3: structured command builder
│   ├── graph/
│   │   └── recovery_graph.py    LangGraph sequential workflow
│   ├── schemas/
│   │   ├── input.py             Pydantic input models
│   │   └── output.py            Pydantic output models (strict JSON)
│   ├── prompts/
│   │   ├── risk_analyst.txt     System prompt for Agent 1
│   │   └── strategist.txt       System prompt for Agent 2
│   ├── requirements.txt
│   └── Dockerfile
│
├── frontend/                    Next.js 14 + TypeScript + Tailwind
│   ├── app/
│   │   ├── dashboard/
│   │   │   ├── page.tsx         Overview (6 metrics, charts, live feed)
│   │   │   └── cases/
│   │   │       ├── page.tsx     Recovery cases table (filterable)
│   │   │       └── [id]/
│   │   │           └── page.tsx Case detail (audit timeline + AI panel)
│   │   └── layout.tsx
│   └── components/
│       ├── MetricCard.tsx
│       ├── AuditTimeline.tsx
│       ├── AIDecisionPanel.tsx
│       └── ValidatorChecks.tsx  ← Shows all 6 validator results
│
├── load-test/
│   └── payment_recovery.js      k6 script (4 scenarios, edge cases)
│
├── docker-compose.yml
├── .env.example
└── Makefile
```

---

## 14. Environment Variables

```bash
# Database
DATABASE_URL=postgres://recoverai:secret@localhost:5432/recoverai?sslmode=disable

# Redis
REDIS_URL=redis://localhost:6379

# Kafka
KAFKA_BROKERS=localhost:9092
KAFKA_TOPIC_PAYMENT_EVENTS=payment.events
KAFKA_TOPIC_REVENUE_RISK=revenue.risk
KAFKA_TOPIC_RECOVERY_COMMANDS=recovery.commands
KAFKA_TOPIC_RECOVERY_RESULTS=recovery.results
KAFKA_TOPIC_RECOVERY_BLOCKED=recovery.blocked
KAFKA_TOPIC_NOTIFICATION_EVENTS=notification.events
KAFKA_TOPIC_AUDIT_EVENTS=audit.events

# Razorpay (Test Mode)
RAZORPAY_KEY_ID=rzp_test_xxxxxxxxxxxx
RAZORPAY_KEY_SECRET=xxxxxxxxxxxxxxxxxxxx
RAZORPAY_WEBHOOK_SECRET=xxxxxxxxxxxxxxxxxxxx

# AI Service
LLM_PROVIDER=groq                           # or: gemini
GROQ_API_KEY=gsk_xxxxxxxxxxxxxxxxxxxx       # same key as AI Career Copilot
GEMINI_API_KEY=                             # optional fallback

# Auth
JWT_SECRET=change_this_to_a_random_32_char_string
JWT_EXPIRY_HOURS=24

# App
PORT=8080
AI_SERVICE_URL=http://localhost:8000
LOG_LEVEL=info
ENVIRONMENT=development

# Recovery Policy Defaults
DEFAULT_MAX_RETRY_AMOUNT_PAISE=1000000      # ₹10,000
DEFAULT_MAX_RETRIES=2
DEFAULT_RETRY_COOLDOWN_MINUTES=5
DEFAULT_REQUIRE_HUMAN_ABOVE_PAISE=5000000   # ₹50,000
DEFAULT_HIGH_VALUE_THRESHOLD_PAISE=1500000  # ₹15,000 (RBI mandate)
DEFAULT_OUTAGE_DETECTION_THRESHOLD=10       # failures per 5-min window
DEFAULT_MIN_RECOVERY_ROI=0                  # stop if ROI < 0
```

---

## 15. Docker Services

```yaml
services:
  postgres     # PostgreSQL 16-alpine — port 5432
  redis        # Redis 7-alpine — port 6379 — keyspace notifications enabled
  kafka        # Apache Kafka 3.7 KRaft (no ZooKeeper) — port 9092
  api          # Go API Gateway — port 8080
  worker       # Go Kafka Worker (all 5 consumers)
  ai-service   # Python FastAPI + LangGraph — port 8000
  frontend     # Next.js 14 — port 3000
```

Startup order enforced via health checks:
- `api` and `worker` wait for `postgres`, `redis`, `kafka` all healthy
- `ai-service` starts independently (no DB dependency at startup)
- `frontend` starts independently (calls api at runtime)

**Makefile targets:**
```
make dev            docker compose up -d
make migrate        golang-migrate up (all 9 migrations)
make kafka-topics   create all 7 topics with correct config
make seed           insert demo data (4 pre-built scenarios)
make test           go test ./...
make load-test      run k6 (4 scenarios, 10 VUs, 2 minutes)
make demo-scenarios trigger outage scenario (15 rapid U28 failures)
```

---

*RecoverAI — Built for Razorpay Build Hackathon, Track 03: AI Revenue Recovery*
*Stack: Go · Python · FastAPI · LangGraph · Groq · PostgreSQL · Redis · Kafka · Next.js*