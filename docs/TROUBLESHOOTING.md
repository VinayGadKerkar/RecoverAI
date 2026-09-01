# RecoverAI — Troubleshooting Guide

**Comprehensive guide to errors encountered during development and their solutions.**

Last Updated: September 1, 2026

---

## 📑 Table of Contents

1. [AI Service Issues](#ai-service-issues)
2. [Frontend Issues](#frontend-issues)
3. [Docker & Infrastructure](#docker--infrastructure)
4. [Database Issues](#database-issues)
5. [Kafka Issues](#kafka-issues)
6. [API & Backend Issues](#api--backend-issues)
7. [Performance Issues](#performance-issues)

---

## 🤖 AI Service Issues

### Issue 1: AI Returning Identical Fallback Responses

**Problem:** AI service was returning the same generic strategy for all UPI error codes instead of varied, context-aware responses.

**Symptoms:**
```sql
razorpay_payment_id | upi_error_code | strategy              | confidence
pay_test_U16_34624  | U16            | generate_payment_link | 0.5
pay_test_Z9_25988   | Z9             | generate_payment_link | 0.5
pay_test_U30_55882  | U30            | generate_payment_link | 0.5
```

All cases showing identical strategy with 0.5 confidence and 0.3 recovery probability.

**Root Cause Analysis:**

1. **LangChain KeyError** - JSON examples in SYSTEM_PROMPT were treated as template variables
2. **Groq Model Deprecation** - Models `llama-3.3-70b-versatile` and `llama-3.1-70b-versatile` were decommissioned
3. **Low Temperature** - Temperature was set to 0.1, causing deterministic/repetitive outputs
4. **Docker Cache** - Code changes not being picked up due to cached Docker layers

**Error Logs:**
```python
KeyError: 'Input to ChatPromptTemplate is missing variables {'\n  "revenue_at_risk_paise"'}.
Expected: ['error_code', 'payment_method', 'payment_amount_inr', ...
```

```
Error code: 404 - {'error': {'message': 'The model `llama-3.3-70b-versatile` does not exist or you do not have access to it.', 'type': 'invalid_request_error', 'code': 'model_not_found'}}
```

```
Error code: 400 - {'error': {'message': 'The model `llama-3.1-70b-versatile` has been decommissioned and is no longer supported. Please refer to https://console.groq.com/docs/deprecations for a recommendation on which model to use instead.', 'type': 'invalid_request_error', 'code': 'model_decommissioned'}}
```

**Solution:**

**Step 1:** Escape JSON curly braces in prompts

File: `ai-service/agents/risk_analyst.py`
```python
# Before (lines 23-24):
"revenue_at_risk_paise": 499900,

# After:
"revenue_at_risk_paise": 499900,  # Note: Escaped with {{}} in actual SYSTEM_PROMPT
```

Changed all `{` to `{{` and `}` to `}}` in JSON examples within SYSTEM_PROMPT strings.

**Step 2:** Update to supported Groq model

File: `ai-service/llm.py`
```python
# Before:
model='llama-3.1-70b-versatile'

# After:
model='openai/gpt-oss-120b'  # Current production model as of 2026
```

**Step 3:** Increase temperature for variety

File: `ai-service/llm.py`
```python
# Before:
temperature=0.1

# After:
temperature=0.5  # Allows more variation in responses
```

**Step 4:** Add debug logging

File: `ai-service/agents/risk_analyst.py` and `strategist.py`
```python
import traceback

try:
    result = await chain.ainvoke(...)
except Exception as e:
    print(f"🚨 DEBUG {name} validation failed [Attempt {attempt}]: {e}")
    print(f"🔍 DEBUG Exception type: {type(e).__name__}")
    print(f"🔍 DEBUG Raw result causing error: {raw_result if 'raw_result' in locals() else 'No result'}")
    print(f"🔍 DEBUG Full traceback:")
    traceback.print_exc()
```

**Step 5:** Add unbuffered Python output

File: `ai-service/Dockerfile`
```dockerfile
# Add environment variable for immediate log output
ENV PYTHONUNBUFFERED=1
```

**Step 6:** Clean Docker build

```powershell
# Stop service
docker-compose stop ai-service

# Remove container and image
docker-compose rm -f ai-service
docker rmi recoverai-ai-service

# Rebuild without cache
docker-compose build --no-cache ai-service

# Recreate and start
docker-compose create ai-service
docker-compose start ai-service
```

**Step 7:** Remove hardcoded overrides

File: `docker-compose.yml`
```yaml
# Removed this line which was overriding .env:
# LLM_PROVIDER: groq  # REMOVED - now reads from .env
```

**Verification:**

After fixes, AI now returns varied responses:

```sql
razorpay_payment_id | upi_error_code | strategy        | confidence | recovery_prob | reasoning
pay_test_U30_88651  | U30           | retry_payment   | 0.92       | 0.85          | Transient TD error, high recovery probability
pay_test_Z9_12813   | Z9            | generate_link   | 0.2        | 0.2           | Bank system issue, low confidence
pay_test_U16_55978  | U16           | schedule_retry  | 0.8        | 0.4           | U16 indicates insufficient funds
```

**Files Changed:**
- `ai-service/agents/risk_analyst.py` (escaped curly braces, added debug logs)
- `ai-service/agents/strategist.py` (escaped curly braces, added debug logs)
- `ai-service/llm.py` (model update, temperature increase)
- `ai-service/Dockerfile` (added PYTHONUNBUFFERED)
- `docker-compose.yml` (removed LLM_PROVIDER override)
- `.env` (set LLM_PROVIDER=groq)

**Prevention:**
- Always escape curly braces in LangChain prompt templates: `{` → `{{`, `}` → `}}`
- Check Groq model availability at https://console.groq.com/docs/models
- Use `docker-compose build --no-cache` when Python code changes aren't reflected
- Test with multiple error codes to verify variety in responses

---

### Issue 2: Groq API Key Not Working

**Problem:** New Groq API key shows "0 API calls" in dashboard, but service returns errors.

**Symptoms:**
```
Error code: 401 - {'error': {'message': 'Invalid API Key', 'type': 'invalid_request_error'}}
```

**Root Causes:**
1. API key not properly set in `.env`
2. Docker container not restarted after `.env` change
3. Whitespace in API key value

**Solution:**

```bash
# 1. Verify API key in .env (no quotes, no whitespace)
GROQ_API_KEY=gsk_your_actual_key_here

# 2. Restart services to pick up new environment
docker-compose restart ai-service worker

# 3. Verify key is loaded
docker exec recoverai-ai-service-1 env | grep GROQ_API_KEY

# 4. Test AI service directly
curl -X POST http://localhost:8000/analyze \
  -H "Content-Type: application/json" \
  -d @test_request.json
```

**Prevention:**
- Always restart services after changing `.env`
- Use `docker-compose down && docker-compose up -d` for clean restart
- Check Groq dashboard for API usage after testing

---

### Issue 3: LangChain Pydantic Validation Errors

**Problem:** AI service returns truncated JSON causing validation errors.

**Symptoms:**
```python
pydantic_core._pydantic_core.ValidationError: 5 validation errors for RecoveryStrategy
  Input should be 'retry_payment', 'generate_payment_link', 'notify_customer', 'schedule_retry', 'escalate_to_merchant' or 'stop_recovery' [type=literal_error, input_value='stop_re', input_type=str]
```

**Root Cause:** `max_tokens=1024` in `llm.py` was too low, causing response truncation.

**Solution:**

File: `ai-service/llm.py`
```python
# Increase max_tokens
return ChatGroq(
    model='openai/gpt-oss-120b',
    api_key=os.getenv('GROQ_API_KEY'),
    temperature=0.5,
    max_tokens=2048,  # Increased from 1024
)
```

**Prevention:**
- Monitor logs for `"Raw result causing error"` messages
- Increase `max_tokens` if seeing truncated responses
- Consider using structured output format with Groq

---

## 🎨 Frontend Issues

### Issue 4: Frontend Shows "No cases found"

**Problem:** Dashboard showing empty state despite recovery cases existing in database.

**Symptoms:**
- Database query returns data
- Frontend displays "No cases found"
- Network tab shows 404 errors

**Root Cause:** API endpoint mismatch between backend and frontend.

**Error:**
```
GET http://localhost:8080/recovery/cases 404 (Not Found)
```

**Solution:**

**Backend Route (Correct):**
```go
// File: cmd/api/main.go
r.Route("/api/v1", func(r chi.Router) {
    handlers.RegisterRecoveryRoutes(r, dbPool, redisClient, cfg)
})

// File: internal/handlers/recovery.go
r.Get("/recovery-cases", handler.ListCases)
```

**Frontend API Client (Fixed):**

File: `frontend/src/lib/api.ts`
```typescript
// Before:
export const fetchRecoveryCases = async (): Promise<RecoveryCase[]> => {
  const response = await fetch(`${API_BASE_URL}/recovery/cases`);
  // ...
}

// After:
export const fetchRecoveryCases = async (): Promise<RecoveryCase[]> => {
  const response = await fetch(`${API_BASE_URL}/recovery-cases`);
  // ...
}
```

**Also Fixed:** Field name mismatches between backend JSON (snake_case) and TypeScript types.

File: `frontend/src/lib/types.ts`
```typescript
// Added proper field mappings
export interface RecoveryCase {
  id: string;
  payment_id: string;  // Backend uses snake_case
  razorpay_payment_id: string;
  // ... all fields now match backend
}
```

**Files Changed:**
- `frontend/src/lib/api.ts` (endpoint paths)
- `frontend/src/lib/types.ts` (field names)
- `frontend/src/app/dashboard/cases/page.tsx` (field access)

**Verification:**
```bash
# Test endpoint
curl http://localhost:8080/api/v1/recovery-cases | jq .

# Should return array of cases
```

**Prevention:**
- Use TypeScript types that match backend exactly
- Test API endpoints with curl before implementing frontend
- Consider generating TypeScript types from Go structs

---

### Issue 5: Next.js Build Errors

**Problem:** Frontend fails to build with module resolution errors.

**Symptoms:**
```
Module not found: Can't resolve '@/components/ui/badge'
```

**Root Cause:** Missing shadcn/ui components or incorrect import paths.

**Solution:**

```bash
# Install missing shadcn components
cd frontend
npx shadcn-ui@latest add badge
npx shadcn-ui@latest add button
npx shadcn-ui@latest add card

# Or install all at once
npx shadcn-ui@latest add badge button card table tabs dialog
```

**Prevention:**
- Keep `components.json` in sync
- Run `npm run build` locally before committing
- Use consistent import paths with `@/` alias

---

## 🐳 Docker & Infrastructure

### Issue 6: Services Won't Start

**Problem:** Docker services fail to start or are unhealthy.

**Symptoms:**
```bash
docker-compose ps
# Shows services as "unhealthy" or "restarting"
```

**Common Causes:**

**1. Port Conflicts**
```bash
# Check if ports are in use
netstat -an | findstr "5432 6379 9092 8080 8000 8001 3000"

# Solution: Stop conflicting services or change ports in docker-compose.yml
```

**2. Insufficient Resources**
```bash
# Check Docker Desktop resources
# Settings → Resources → Advanced
# Minimum: 4GB RAM, 2 CPUs
```

**3. Kafka Not Ready**
```bash
# Wait for Kafka to fully initialize (can take 30-60 seconds)
docker-compose logs kafka | findstr "started"

# If stuck, restart Kafka
docker-compose restart kafka
```

**4. Database Migration Issues**
```bash
# Check migration status
docker exec recoverai-postgres-1 psql -U recoverai -d recoverai -c "\dt"

# If migrations not applied
docker exec recoverai-api-1 /bin/api -migrate
```

**Complete Reset (Nuclear Option):**
```bash
# Stop all services and remove volumes
docker-compose down -v

# Remove all images
docker rmi $(docker images -q recoverai-*)

# Rebuild and start fresh
docker-compose build --no-cache
docker-compose up -d

# Wait 60 seconds, then check
docker-compose ps
```

**Prevention:**
- Always check `docker-compose ps` after starting
- Monitor logs during startup: `docker-compose logs -f`
- Give Kafka time to fully start before sending requests

---

### Issue 7: Docker Build Cache Issues

**Problem:** Code changes not reflected in running containers.

**Symptoms:**
- Changed Go/Python code, but old behavior persists
- New files not appearing in container
- Environment variables not updating

**Root Cause:** Docker layer caching + not rebuilding after changes.

**Solution:**

**For Python AI Service:**
```bash
# Full rebuild required for any .py file changes
docker-compose stop ai-service
docker-compose rm -f ai-service
docker-compose build --no-cache ai-service
docker-compose create ai-service
docker-compose start ai-service
```

**For Go Services (API/Worker):**
```bash
# Rebuild Go binary
docker-compose build --no-cache api worker

# Restart
docker-compose restart api worker
```

**For Environment Variable Changes:**
```bash
# Must recreate containers
docker-compose down
docker-compose up -d
```

**Quick Fix Script (PowerShell):**
```powershell
# File: rebuild_ai.ps1
docker-compose stop ai-service
docker-compose rm -f ai-service
docker-compose build --no-cache ai-service
docker-compose create ai-service
docker-compose start ai-service
Start-Sleep -Seconds 8
docker-compose ps ai-service
```

**Prevention:**
- Use `docker-compose build --no-cache` for critical changes
- Consider mounting code as volumes during development (requires setup)
- Always verify changes with `docker exec` or logs

---

## 💾 Database Issues

### Issue 8: Migration Failures

**Problem:** Database migrations fail to apply.

**Symptoms:**
```
ERROR: relation "recovery_cases" does not exist
```

**Solution:**

```bash
# 1. Check current migration version
docker exec recoverai-postgres-1 psql -U recoverai -d recoverai \
  -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;"

# 2. Check migration files exist
docker exec recoverai-api-1 ls -la /migrations

# 3. Manually run migrations
docker exec recoverai-api-1 /bin/api -migrate

# 4. Verify tables created
docker exec recoverai-postgres-1 psql -U recoverai -d recoverai -c "\dt"
```

**If Migrations Corrupted:**
```bash
# Nuclear option: Reset database
docker-compose stop postgres
docker volume rm recoverai_postgres_data
docker-compose up -d postgres

# Wait 10 seconds, then run migrations
docker exec recoverai-api-1 /bin/api -migrate
```

**Prevention:**
- Always run migrations before seeding
- Keep migration files in version control
- Test migrations on clean database before deploying

---

### Issue 9: Database Connection Refused

**Problem:** Services can't connect to PostgreSQL.

**Symptoms:**
```
connection refused: dial tcp 127.0.0.1:5432: connect: connection refused
```

**Root Cause:** Using `localhost` instead of Docker service name in `DATABASE_URL`.

**Solution:**

File: `.env`
```env
# Wrong:
DATABASE_URL=postgresql://recoverai:recoverai@localhost:5432/recoverai

# Correct:
DATABASE_URL=postgresql://recoverai:recoverai@postgres:5432/recoverai
```

**For External Access (from host machine):**
```env
# Use localhost when connecting from host
DATABASE_URL=postgresql://recoverai:recoverai@localhost:5432/recoverai
```

**Prevention:**
- Use Docker service names in `.env` for containers
- Use `localhost` only for host machine access
- Document both connection strings

---

## 📨 Kafka Issues

### Issue 10: Topics Not Created

**Problem:** Kafka consumers can't find topics.

**Symptoms:**
```
Unknown topic or partition: payment.events
```

**Root Cause:** kafka-init service failed or topics not created.

**Solution:**

```bash
# 1. Check if topics exist
docker exec recoverai-kafka-1 kafka-topics.sh \
  --list --bootstrap-server localhost:9092

# 2. Manually create missing topics
docker exec recoverai-kafka-1 kafka-topics.sh \
  --create --topic payment.events \
  --bootstrap-server localhost:9092 \
  --partitions 3 --replication-factor 1

# 3. Restart kafka-init service
docker-compose restart kafka-init

# 4. Verify all 6 topics exist:
# payment.events, revenue.risk, payment.validated_for_ai,
# payment.ai_commands, payment.execution_results, payment.deadletter
```

**Prevention:**
- Check kafka-init logs: `docker-compose logs kafka-init`
- Wait for Kafka to be ready before creating topics
- Use health checks in docker-compose.yml

---

### Issue 11: Consumer Group Lag

**Problem:** Messages piling up in Kafka, not being consumed.

**Symptoms:**
```bash
# High lag in consumer groups
docker exec recoverai-kafka-1 kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --describe --group recoverai-workers
# Shows LAG > 1000
```

**Root Causes:**
1. Worker process crashed
2. Slow AI service causing backlog
3. Database connection pool exhausted

**Solution:**

```bash
# 1. Check worker logs for errors
docker-compose logs worker --tail=100

# 2. Restart worker
docker-compose restart worker

# 3. Scale workers (if needed)
docker-compose up -d --scale worker=3

# 4. Check AI service health
curl http://localhost:8000/health
curl http://localhost:8001/health  # mock AI
```

**Prevention:**
- Monitor consumer lag with Kafka tools
- Use mock AI for development to avoid rate limits
- Implement circuit breaker for AI calls

---

## 🔧 API & Backend Issues

### Issue 12: Webhook Signature Verification Fails

**Problem:** Razorpay webhooks rejected with "invalid signature".

**Symptoms:**
```json
{"error":"invalid signature"}
```

**Root Cause:** Incorrect webhook secret or signature calculation.

**Solution:**

**1. Verify Secret in .env:**
```env
RAZORPAY_WEBHOOK_SECRET=your_actual_secret_from_razorpay_dashboard
```

**2. Test Signature Calculation:**
```powershell
# PowerShell script to compute HMAC
$secret = "your_webhook_secret"
$body = Get-Content test_request.json -Raw
$hmac = New-Object System.Security.Cryptography.HMACSHA256
$hmac.Key = [System.Text.Encoding]::UTF8.GetBytes($secret)
$hash = $hmac.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($body))
$signature = [System.BitConverter]::ToString($hash).Replace("-", "").ToLower()
Write-Host "Signature: $signature"
```

**3. Send Webhook with Correct Signature:**
```powershell
$signature = "computed_signature_from_above"
Invoke-RestMethod -Uri "http://localhost:8080/webhooks/razorpay" `
  -Method POST `
  -Body $body `
  -ContentType "application/json" `
  -Headers @{"X-Razorpay-Signature"=$signature}
```

**Prevention:**
- Store webhook secret securely
- Use test secret for development
- Log signature mismatches (but not the secret!)

---

### Issue 13: Out-of-Order Webhook Events

**Problem:** `payment.captured` arrives before `payment.failed`, causing incorrect state.

**Solution:** This is **by design** — Razorpay webhooks can arrive out of order. RecoverAI handles this:

**Handler Logic:**
```go
// File: internal/handlers/webhook.go
func (h *WebhookHandler) handleCustomerSelfRecovery(razorpayEventID string, payload RazorpayWebhookPayload) {
    // If payment.captured arrives before payment.failed:
    // 1. Marks case as customer_self_recovered
    // 2. Cancels pending actions
    // 3. Records recovered amount
}
```

**How to Test:**
```bash
# Send payment.captured first
curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "X-Razorpay-Signature: $sig1" \
  -d '{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_test_123"}}}}'

# Then send payment.failed
curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "X-Razorpay-Signature: $sig2" \
  -d '{"event":"payment.failed","payload":{"payment":{"entity":{"id":"pay_test_123"}}}}'

# Check case status - should be customer_self_recovered
```

**Prevention:**
- Never assume event order
- Use idempotency keys (Redis SETNX)
- Check current state before state transitions

---

## ⚡ Performance Issues

### Issue 14: Slow AI Response Times

**Problem:** AI service taking >5 seconds per request.

**Symptoms:**
- Validator stage completes in ms
- AI stage takes 5-15 seconds
- Pipeline stalls at Stage 4

**Root Causes:**
1. **Groq Rate Limits** — Free tier: 5 req/s
2. **Network Latency** — Container network overhead
3. **Large Prompts** — Too much context in SYSTEM_PROMPT

**Solutions:**

**1. Switch to Mock AI for Development:**
```env
USE_MOCK_AI=true
MOCK_AI_DELAY_MS=50
```
Results: 50ms average latency, 12,000 req/s throughput

**2. Upgrade Groq Plan:**
- Free tier: 5 req/s, 6,000 req/day
- Paid tier: 500 req/s, unlimited requests

**3. Optimize Prompts:**
```python
# File: ai-service/agents/risk_analyst.py
# Remove unnecessary examples, keep only critical ones
SYSTEM_PROMPT = """
You are a risk analyst. Analyze payments concisely.
[Keep only 2-3 examples instead of 10]
"""
```

**4. Use Caching:**
```python
# Cache identical requests (future enhancement)
from functools import lru_cache

@lru_cache(maxsize=1000)
def analyze_payment(payment_id, error_code, amount):
    # ...
```

**Prevention:**
- Use mock AI for development and load testing
- Monitor AI service metrics
- Set timeout on AI calls (current: 30s)

---

### Issue 15: High Database Connection Count

**Problem:** PostgreSQL max connections exceeded.

**Symptoms:**
```
FATAL: remaining connection slots are reserved for non-replication superuser connections
```

**Root Cause:** Too many services opening connections without pooling.

**Solution:**

File: `docker-compose.yml`
```yaml
postgres:
  command: postgres -c max_connections=200  # Default is 100
```

**Better Solution:** Connection pooling in application

File: `internal/db/db.go`
```go
config, _ := pgxpool.ParseConfig(databaseURL)
config.MaxConns = 10  // Per service instance
config.MinConns = 2
config.MaxConnIdleTime = 30 * time.Minute

pool, err := pgxpool.NewWithConfig(context.Background(), config)
```

**Prevention:**
- Use connection pooling (pgxpool)
- Close connections after use
- Monitor active connections: `SELECT count(*) FROM pg_stat_activity;`

---

## 🔍 Debugging Tips

### Enable Verbose Logging

**Go Services:**
```env
LOG_LEVEL=debug
```

**Python AI Service:**
```env
LOG_LEVEL=DEBUG
PYTHONUNBUFFERED=1
```

**View Logs:**
```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f ai-service

# Last 100 lines
docker-compose logs ai-service --tail=100

# With grep
docker-compose logs ai-service | Select-String "ERROR"
```

### Check Service Health

```bash
# API health
curl http://localhost:8080/health

# AI service health
curl http://localhost:8000/health

# Mock AI health
curl http://localhost:8001/health

# Check AI mode
curl http://localhost:8080/api/v1/status | jq .
```

### Database Queries

```bash
# Interactive psql
docker exec -it recoverai-postgres-1 psql -U recoverai -d recoverai

# Quick query
docker exec recoverai-postgres-1 psql -U recoverai -d recoverai -c "
  SELECT status, COUNT(*) 
  FROM recovery_cases 
  GROUP BY status;
"
```

### Redis Inspection

```bash
# Connect to Redis CLI
docker exec -it recoverai-redis-1 redis-cli

# Check keys
KEYS *

# Check outage flags
GET bank_outage:U30

# Check failure counters
KEYS bank_failures:*
```

---

## 📞 Getting Help

If you encounter an issue not covered here:

1. **Check Logs:** `docker-compose logs -f`
2. **Check Documentation:** See `docs/` folder
3. **Search Issues:** GitHub issue tracker
4. **Ask for Help:** Create new issue with:
   - Error message (full log)
   - Steps to reproduce
   - Environment (OS, Docker version)
   - Relevant configuration (.env values, minus secrets)

---

## 📚 Related Documentation

- [`README.md`](../README.md) — Project overview
- [`docs/QUICKSTART.md`](QUICKSTART.md) — Setup guide
- [`docs/architecture.md`](architecture.md) — System design
- [`docs/AI_TOGGLE_SYSTEM.md`](AI_TOGGLE_SYSTEM.md) — AI configuration

---

**Last Updated:** September 1, 2026  
**Maintained By:** RecoverAI Team
