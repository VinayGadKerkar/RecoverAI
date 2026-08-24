# AI Toggle System — Quick Start

**Switch between real and mock AI in 30 seconds.**

---

## 🚀 Three Modes

### Mode 1: Mock Only (Zero Tokens)

```bash
# Edit .env
USE_MOCK_AI=true
TEST_AI_LIMIT=0

# Restart
docker-compose restart worker

# ✅ All calls → mock AI → $0 cost
```

### Mode 2: Real AI Only

```bash
# Edit .env
USE_MOCK_AI=false
TEST_AI_LIMIT=0
GROQ_API_KEY=gsk_your_key_here

# Restart
docker-compose restart worker

# ✅ All calls → real Groq → normal cost
```

### Mode 3: Real AI + Auto-Switch (RECOMMENDED)

```bash
# Edit .env
USE_MOCK_AI=false
TEST_AI_LIMIT=10
GROQ_API_KEY=gsk_your_key_here

# Restart
docker-compose restart worker

# ✅ First 10 calls → real Groq
# ✅ Calls 11+ → mock AI automatically
# ✅ Total cost: ~10 requests worth
```

---

## 🔍 Check Current Mode

```bash
# Check via API
curl http://localhost:8080/api/v1/status | jq .

# Response:
{
  "ai_mode": "mock",              # "mock" or "real"
  "ai_url": "http://...",         # Current URL
  "mock_ai_available": true,      # Health check
  "real_call_count": 3,           # Calls made
  "test_limit_enabled": true      # Limit active?
}
```

---

## 📊 Monitor Call Count

```bash
# Watch real-time
watch -n 1 'curl -s http://localhost:8080/api/v1/status | jq .real_call_count'

# Or check logs
docker-compose logs worker | grep "AI-CLIENT"
```

---

## 🧪 Test It Works

```bash
# 1. Start in real mode with limit
USE_MOCK_AI=false
TEST_AI_LIMIT=2
docker-compose restart worker

# 2. Trigger 3 payments
curl -X POST http://localhost:8080/webhooks/razorpay ...
curl -X POST http://localhost:8080/webhooks/razorpay ...
curl -X POST http://localhost:8080/webhooks/razorpay ...

# 3. Check logs - should see:
# [AI-CLIENT] Routing to REAL AI (1/2): ...
# [AI-CLIENT] Routing to REAL AI (2/2): ...
# ⚠️ TEST_AI_LIMIT reached (2/2) — switching to mock mode
# [AI-CLIENT] Routing to MOCK AI: ...

# 4. Check status
curl http://localhost:8080/api/v1/status | jq .
# {
#   "ai_mode": "mock",  ← Auto-switched!
#   "real_call_count": 2
# }
```

---

## 💡 Common Use Cases

### Local Development (No API Key)

```env
USE_MOCK_AI=true
TEST_AI_LIMIT=0
GROQ_API_KEY=  # Can be empty
```

### Daily Development (Budget-Friendly)

```env
USE_MOCK_AI=false
TEST_AI_LIMIT=10
GROQ_API_KEY=gsk_your_key
```

**Why 10?** Enough to validate real AI behavior, cheap enough for daily use.

### Load Testing

```env
USE_MOCK_AI=true
TEST_AI_LIMIT=0
```

**Result:** 10,000 requests × $0 = $0 total cost

### Production

```env
USE_MOCK_AI=false
TEST_AI_LIMIT=0
GROQ_API_KEY=gsk_production_key
```

---

## 🐛 Troubleshooting

### Worker uses wrong mode

```bash
# Check env
docker-compose exec worker env | grep USE_MOCK_AI

# Fix
vim .env  # Edit USE_MOCK_AI
docker-compose restart worker
```

### TEST_AI_LIMIT not working

```bash
# Must be numeric
TEST_AI_LIMIT=10  # ✅ Correct
TEST_AI_LIMIT="10"  # ❌ Wrong (quoted)
TEST_AI_LIMIT=ten  # ❌ Wrong (text)
```

### Mock AI unreachable

```bash
# Check mock AI status
curl http://localhost:8001/health

# Restart if needed
docker-compose restart mock-ai
```

---

## 📚 Full Documentation

See `AI_TOGGLE_SYSTEM.md` for complete details.

---

## ✅ Summary

**Three environment variables control everything:**

```env
USE_MOCK_AI=false     # true = mock, false = real
TEST_AI_LIMIT=10      # Auto-switch after N calls (0 = unlimited)
GROQ_API_KEY=gsk_xxx  # Only needed if using real AI
```

**No code changes needed. Just edit .env and restart worker.**
