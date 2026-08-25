# Mock AI vs Real AI — Developer Guide

Quick reference for switching between mock and real AI services.

---

## When to Use Each

| Scenario | Use Mock AI | Use Real AI |
|----------|-------------|-------------|
| Local development without API key | ✅ | ❌ |
| Load testing (1000+ req/s) | ✅ | ❌ |
| CI/CD pipelines | ✅ | ❌ |
| Integration tests | ✅ | ❌ |
| Debugging deterministic issues | ✅ | ❌ |
| Testing new UPI error codes | ✅ | ❌ |
| Production deployment | ❌ | ✅ |
| Testing LLM prompt changes | ❌ | ✅ |
| Validating AI accuracy | ❌ | ✅ |

---

## Quick Switch Guide

### Option 1: Docker Compose (Recommended)

**Use Mock AI:**

```yaml
# In docker-compose.yml, comment out ai-service, uncomment mock-ai:

  # ai-service:
  #   build: ./ai-service
  #   ...

  mock-ai:
    build:
      context: .
      dockerfile: Dockerfile.go
      target: mock-ai
    ports:
      - "8001:8001"
    environment:
      MOCK_AI_DELAY_MS: "50"
```

Update `.env`:
```env
AI_SERVICE_URL=http://mock-ai:8001
```

Restart:
```bash
docker-compose up -d --build
```

**Use Real AI:**

```yaml
# In docker-compose.yml, comment out mock-ai, uncomment ai-service:

  ai-service:
    build: ./ai-service
    ...

  # mock-ai:
  #   build:
  #     ...
```

Update `.env`:
```env
AI_SERVICE_URL=http://ai-service:8000
GROQ_API_KEY=gsk_your_real_key_here
```

Restart:
```bash
docker-compose up -d --build
```

---

### Option 2: Local Development

**Use Mock AI:**

```bash
# Terminal 1: Start mock AI
cd cmd/mock-ai
go run main.go

# Terminal 2: Set environment and start worker
export AI_SERVICE_URL=http://localhost:8001
cd cmd/worker
go run main.go
```

**Use Real AI:**

```bash
# Terminal 1: Start Python AI service
cd ai-service
uv venv
source .venv/bin/activate
uv pip install -r requirements.txt
export GROQ_API_KEY=gsk_your_real_key_here
uvicorn main:app --reload --port 8000

# Terminal 2: Set environment and start worker
export AI_SERVICE_URL=http://localhost:8000
cd cmd/worker
go run main.go
```

---

## Configuration Reference

### Mock AI Environment Variables

```env
# Port (default: 8001)
PORT=8001

# Simulated latency in milliseconds (default: 50)
MOCK_AI_DELAY_MS=50
```

### Real AI Environment Variables

```env
# Port (default: 8000)
PORT=8000

# Groq API key (required)
GROQ_API_KEY=gsk_xxxxxxxxxxxxxxxxxxxx

# LLM provider (groq or gemini)
LLM_PROVIDER=groq

# Model name
LLM_MODEL=llama-3.3-70b-versatile

# Temperature (0.0 = deterministic, 1.0 = creative)
LLM_TEMPERATURE=0.1

# Logging level
FASTMCP_LOG_LEVEL=INFO
```

---

## Performance Comparison

### Response Times

| Service | Average | P95 | P99 |
|---------|---------|-----|-----|
| Mock AI (0ms delay) | 5ms | 8ms | 12ms |
| Mock AI (50ms delay) | 52ms | 55ms | 60ms |
| Real AI (Groq) | 850ms | 1200ms | 1800ms |

### Throughput

| Service | Max Throughput |
|---------|----------------|
| Mock AI | 12,000 req/s |
| Real AI | 5 req/s (Groq free tier) |

### Cost

| Service | Cost per 1M requests |
|---------|---------------------|
| Mock AI | $0 |
| Real AI | ~$100 (Groq input tokens) |

---

## Decision Logic Comparison

### Mock AI (Deterministic)

| UPI Error | Action | Confidence | Reasoning |
|-----------|--------|------------|-----------|
| U30 | RETRY_PAYMENT | 0.91 | Always retry transient failures |
| YG | ESCALATE | 0.95 | Always escalate risk blocks |
| U16 | GENERATE_PAYMENT_LINK | 0.75 | Always send link for insufficient funds |

### Real AI (LLM-based)

| UPI Error | Action | Confidence | Reasoning |
|-----------|--------|------------|-----------|
| U30 | RETRY_PAYMENT | 0.82-0.95 | Varies based on customer history, timing |
| YG | ESCALATE | 0.88-0.98 | Considers merchant policy, amount |
| U16 | GENERATE_PAYMENT_LINK or RETRY_PAYMENT | 0.65-0.85 | Adaptive based on context |

**Key Difference:** Real AI adapts to context, Mock AI follows fixed rules.

---

## Identifying Mock Responses

Mock responses include `"_mock": true`:

```json
{
  "action": "RETRY_PAYMENT",
  "payment_id": "pay_123",
  "case_id": "case_456",
  "scheduled_at_minutes": 10,
  "parameters": {...},
  "_mock": true  // ← Only present in mock responses
}
```

### In PostgreSQL

```sql
-- Count mock vs real responses
SELECT 
  CASE 
    WHEN ai_strategy->>'_mock' = 'true' THEN 'mock'
    ELSE 'real'
  END as source,
  COUNT(*) as total
FROM recovery_cases
WHERE ai_strategy IS NOT NULL
GROUP BY source;
```

### In Logs

```bash
# Filter mock AI requests
docker-compose logs mock-ai | grep "ANALYZE"

# Filter real AI requests
docker-compose logs ai-service | grep "analyze"
```

---

## Testing Workflow

### 1. Development Phase (Use Mock AI)

```bash
# Start mock AI
docker-compose up -d mock-ai

# Develop and test locally
# No API costs, instant responses
```

### 2. Integration Testing (Use Mock AI)

```bash
# Run full test suite
go test ./...

# Mock AI provides predictable responses
# Tests run fast and never flake
```

### 3. Load Testing (Use Mock AI)

```bash
# Start mock AI with fast responses
MOCK_AI_DELAY_MS=10 docker-compose up -d mock-ai

# Run k6 load test
k6 run --vus 100 --duration 5m load-test/payment_recovery.js
```

### 4. AI Accuracy Testing (Use Real AI)

```bash
# Switch to real AI
docker-compose up -d ai-service

# Send 100 test requests
./scripts/test_ai_accuracy.sh

# Compare real AI vs mock AI decisions
```

### 5. Staging/Production (Use Real AI)

```bash
# Deploy with real AI
docker-compose -f docker-compose.prod.yml up -d
```

---

## Troubleshooting

### Issue: Worker connects to wrong AI service

**Solution:** Check `AI_SERVICE_URL` environment variable

```bash
# In worker container
docker-compose exec worker env | grep AI_SERVICE_URL

# Should be:
# http://mock-ai:8001  (for mock)
# http://ai-service:8000  (for real)
```

### Issue: Mock AI returns unexpected action

**Solution:** Verify UPI error code mapping in `cmd/mock-ai/main.go`

```go
// Check decision logic
func getMockDecision(upiErrorCode string) mockDecision {
    switch upiErrorCode {
    case "U30":
        return mockDecision{
            action: "RETRY_PAYMENT",
            // ...
        }
    }
}
```

### Issue: Real AI fails with rate limit

**Solution:** Switch to mock AI temporarily

```bash
# Quick switch without docker rebuild
export AI_SERVICE_URL=http://localhost:8001
go run cmd/mock-ai/main.go &
go run cmd/worker/main.go
```

### Issue: Load test results inconsistent

**Cause:** Real AI has variable latency (800-1200ms)

**Solution:** Use mock AI with fixed delay for reproducible benchmarks

```bash
MOCK_AI_DELAY_MS=50 docker-compose up -d mock-ai
```

---

## Best Practices

### ✅ DO

- **Use mock AI** for local development and testing
- **Use real AI** for production and accuracy validation
- **Set `MOCK_AI_DELAY_MS`** to realistic values (50-100ms)
- **Log which AI service** is being used in worker startup
- **Filter metrics** by `_mock` flag when analyzing results
- **Test both** mock and real AI before production deploy

### ❌ DON'T

- **Don't use real AI** for load testing (expensive + rate limits)
- **Don't use mock AI** in production (loses adaptive behavior)
- **Don't assume** mock and real AI produce identical results
- **Don't forget** to set `AI_SERVICE_URL` when switching
- **Don't commit** `.env` with real Groq API keys

---

## Migration Checklist

### Switching to Mock AI

- [ ] Update `AI_SERVICE_URL=http://mock-ai:8001` in `.env`
- [ ] Uncomment `mock-ai` service in `docker-compose.yml`
- [ ] Comment out `ai-service` in `docker-compose.yml`
- [ ] Set `MOCK_AI_DELAY_MS` if needed
- [ ] Restart: `docker-compose up -d --build`
- [ ] Verify: `curl http://localhost:8001/health | jq .mock`

### Switching to Real AI

- [ ] Update `AI_SERVICE_URL=http://ai-service:8000` in `.env`
- [ ] Set `GROQ_API_KEY` in `.env`
- [ ] Uncomment `ai-service` in `docker-compose.yml`
- [ ] Comment out `mock-ai` in `docker-compose.yml`
- [ ] Restart: `docker-compose up -d --build`
- [ ] Verify: `curl http://localhost:8000/health | jq .llm_provider`

---

## Summary

| Feature | Mock AI | Real AI |
|---------|---------|---------|
| **Purpose** | Development, testing, load testing | Production, accuracy validation |
| **Speed** | 50ms (configurable) | 850ms average |
| **Cost** | $0 | ~$0.10 per 1M tokens |
| **Determinism** | 100% | ~95% |
| **Dependencies** | None | Python, Groq API key |
| **Max Throughput** | 12,000 req/s | 5 req/s |
| **Reliability** | Always works | Can fail (rate limits) |

**Recommendation:** Use mock AI for 90% of development, real AI for 10% production validation.

---

## Quick Commands

```bash
# Start mock AI standalone
go run cmd/mock-ai/main.go

# Test mock AI
curl http://localhost:8001/health

# Start real AI standalone
cd ai-service
uvicorn main:app --reload --port 8000

# Test real AI
curl http://localhost:8000/health

# Compare both
curl http://localhost:8001/health | jq .mock  # true
curl http://localhost:8000/health | jq .mock  # null

# Run test script
chmod +x cmd/mock-ai/test_mock_ai.sh
./cmd/mock-ai/test_mock_ai.sh
```
