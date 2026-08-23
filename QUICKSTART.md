# RecoverAI — Quick Start Guide

Complete payment recovery platform with Go backend, Python AI service, and Next.js dashboard.

---

## Architecture Overview

```
┌─────────────────┐
│  Razorpay       │
│  Webhooks       │
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│                     Go API (Port 8080)                       │
│  • Webhook ingestion (HMAC verification)                    │
│  • REST API endpoints                                        │
│  • Kafka producer                                            │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│                   Kafka Topics                               │
│  payment.events → revenue.risk → recovery.commands →         │
│  recovery.results                                            │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌──────────────┬──────────────┬──────────────┬───────────────┐
│ Risk         │ Pre-Recovery │ AI Service   │ Policy +      │
│ Processor    │ Validator    │ (FastAPI)    │ Execution     │
│              │              │              │ Worker        │
│ • UPI error  │ • 6 checks   │ • 3 agents   │ • 10 rules    │
│ • Outage     │ • ROI calc   │ • Groq LLM   │ • Razorpay    │
│ • Risk score │ • RBI rules  │ • JSON cmds  │ • Cooldown    │
└──────────────┴──────────────┴──────────────┴───────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│              PostgreSQL + Redis + Kafka                      │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│           Next.js Dashboard (Port 3000)                      │
│  • Real-time metrics (5s polling)                           │
│  • Full audit timeline                                       │
│  • Validator + AI + Policy breakdown                         │
└─────────────────────────────────────────────────────────────┘
```

---

## Prerequisites

- **Docker & Docker Compose**
- **Go 1.21+** (for local development)
- **Python 3.11+** with `uv` (for AI service)
- **Node.js 18+** (for dashboard)
- **Groq API Key** (free at https://console.groq.com)

---

## 1. Clone & Setup Environment

```bash
# Clone repository
git clone <your-repo-url>
cd RecoverAi

# Copy environment variables
cp .env.example .env

# Edit .env and add your keys
nano .env
```

### Required Environment Variables

```env
# Razorpay
RAZORPAY_KEY_ID=rzp_test_xxxxx
RAZORPAY_KEY_SECRET=xxxxx
RAZORPAY_WEBHOOK_SECRET=xxxxx

# Groq AI
GROQ_API_KEY=gsk_xxxxx

# PostgreSQL
POSTGRES_USER=recoverai
POSTGRES_PASSWORD=recoverai_password
POSTGRES_DB=recoverai

# Redis
REDIS_PASSWORD=redis_password

# Kafka (no auth for local dev)
```

---

## 2. Start All Services (Docker)

```bash
# Start infrastructure + all services
docker-compose up -d

# Check logs
docker-compose logs -f

# Services will be available at:
# - Go API: http://localhost:8080
# - AI Service: http://localhost:8000
# - Frontend: http://localhost:3000
# - PostgreSQL: localhost:5432
# - Redis: localhost:6379
# - Kafka: localhost:9092
```

### Verify Services

```bash
# Check Go API health
curl http://localhost:8080/health

# Check AI service health
curl http://localhost:8000/health

# Check frontend
open http://localhost:3000
```

---

## 3. Run Database Migrations

```bash
# Install golang-migrate (if not installed)
# macOS
brew install golang-migrate

# Linux
curl -L https://github.com/golang-migrate/migrate/releases/download/v4.16.2/migrate.linux-amd64.tar.gz | tar xvz
sudo mv migrate /usr/local/bin/

# Run migrations
migrate -path internal/db/migrations -database "postgresql://recoverai:recoverai_password@localhost:5432/recoverai?sslmode=disable" up

# Verify
docker-compose exec postgres psql -U recoverai -d recoverai -c "\dt"
```

---

## 4. Seed Test Data (Optional)

```bash
# Run seeder
docker-compose exec api /app/seed

# Or locally
go run cmd/seed/main.go
```

This creates:
- 1 test merchant
- 100 test customers
- 50 failed payments with various UPI error codes

---

## 5. Test Webhook Ingestion

```bash
# Generate HMAC signature
WEBHOOK_SECRET="your_webhook_secret_here"
PAYLOAD='{"event":"payment.failed","payload":{"payment":{"entity":{"id":"pay_test123","amount":499900,"currency":"INR","status":"failed","method":"upi","error_code":"U30","error_description":"Debit timeout"}},"account_id":"acc_test"}}'

SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | awk '{print $2}')

# Send webhook
curl -X POST http://localhost:8080/webhooks/razorpay \
  -H "Content-Type: application/json" \
  -H "X-Razorpay-Signature: $SIGNATURE" \
  -H "X-Razorpay-Event-Id: evt_$(date +%s)" \
  -d "$PAYLOAD"
```

---

## 6. Monitor Recovery Pipeline

### Check Kafka Topics

```bash
# List topics
docker-compose exec kafka kafka-topics --bootstrap-server localhost:9092 --list

# Consume payment.events
docker-compose exec kafka kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic payment.events \
  --from-beginning

# Consume revenue.risk
docker-compose exec kafka kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic revenue.risk \
  --from-beginning
```

### Check Redis

```bash
# Connect to Redis
docker-compose exec redis redis-cli -a redis_password

# Check idempotency keys
KEYS webhook:idempotency:*

# Check bank outage counters
KEYS bank:outage:*

# Check cooldowns
KEYS recovery:cooldown:*
```

### Check PostgreSQL

```bash
# Connect to DB
docker-compose exec postgres psql -U recoverai -d recoverai

# Check recovery cases
SELECT id, status, revenue_at_risk, amount_recovered, upi_error_code 
FROM recovery_cases 
ORDER BY created_at DESC 
LIMIT 10;

# Check audit logs
SELECT actor, action, created_at 
FROM audit_logs 
WHERE case_id = '<case_id_here>' 
ORDER BY created_at ASC;
```

---

## 7. Access Dashboard

Open http://localhost:3000 and you'll see:

### Page 1: Overview (`/dashboard`)
- 6 metric cards (revenue at risk, recovered, rates, etc.)
- Line chart: 24h revenue trend
- Bar chart: Recovery rate by failure type
- Live feed: Recent cases (updates every 5s)

### Page 2: Cases List (`/dashboard/cases`)
- Filterable table (status, priority, error code, outage)
- Validator decision column
- Click row to view details

### Page 3: Case Detail (`/dashboard/cases/[id]`)
- **Left:** Full audit timeline showing ALL actors:
  - WEBHOOK → RISK → VALIDATOR (6 checks) → AI (3 agents) → POLICY → ACTION → RESULT
- **Right:** Breakdown panels:
  - Case summary
  - Why at risk
  - Validator checks (all 6 with pass/fail)
  - AI decision (strategy + confidence)
  - Policy rules
  - Result

---

## 8. Development Workflow

### Backend Changes

```bash
# Run Go API locally
cd cmd/api
go run main.go

# Run worker locally
cd cmd/worker
go run main.go

# Run tests (when implemented)
go test ./...
```

### AI Service Changes

```bash
# Run AI service locally
cd ai-service
uv venv
source .venv/bin/activate  # Windows: .venv\Scripts\activate
uv pip install -r requirements.txt
uvicorn main:app --reload --port 8000

# Test AI endpoint
curl -X POST http://localhost:8000/analyze \
  -H "Content-Type: application/json" \
  -d @test_request.json
```

### Frontend Changes

```bash
# Run Next.js dev server
cd frontend
npm install
npm run dev

# Build for production
npm run build
npm start
```

---

## 9. Debugging Tips

### Webhook Not Received?
1. Check HMAC signature calculation
2. Verify `X-Razorpay-Event-Id` is unique
3. Check webhook handler logs: `docker-compose logs api | grep webhook`

### Risk Processor Not Running?
1. Check Kafka connection: `docker-compose logs api | grep kafka`
2. Verify topic exists: `docker-compose exec kafka kafka-topics --list --bootstrap-server localhost:9092`
3. Check consumer group: `docker-compose exec kafka kafka-consumer-groups --bootstrap-server localhost:9092 --group risk-processor --describe`

### AI Service Errors?
1. Check Groq API key: `docker-compose logs ai-service | grep GROQ`
2. Verify LLM initialization: `curl http://localhost:8000/health`
3. Check prompt templates: `cat ai-service/prompts/*.txt`

### Policy Engine Blocking Everything?
1. Check merchant policy in database:
   ```sql
   SELECT * FROM merchants WHERE id = '<merchant_id>';
   ```
2. Verify `allowed_actions` includes: `["retry", "payment_link", "notify", "escalate"]`
3. Check `max_retry_amount_paise` and `require_human_above_paise`

### Dashboard Not Loading Data?
1. Check API URL: `cat frontend/.env.local`
2. Verify backend endpoints: `curl http://localhost:8080/api/v1/analytics/overview`
3. Check browser console for CORS errors
4. Verify SWR polling: Open React DevTools

---

## 10. Production Deployment

### Backend (Go API + Worker)

```bash
# Build Docker images
docker build -f Dockerfile.go -t recoverai-api:latest .

# Deploy to cloud (example: AWS ECS)
# Update docker-compose.prod.yml with:
# - External PostgreSQL (RDS)
# - External Redis (ElastiCache)
# - External Kafka (MSK)
# - Secrets from AWS Secrets Manager
```

### AI Service

```bash
# Build Docker image
docker build -f ai-service/Dockerfile -t recoverai-ai:latest ./ai-service

# Deploy to cloud
# - Use managed Python runtime (AWS Lambda, Cloud Run, etc.)
# - Set GROQ_API_KEY in environment
# - Enable auto-scaling based on Kafka lag
```

### Frontend

```bash
# Build static site
cd frontend
npm run build

# Deploy to Vercel/Netlify/CloudFront
vercel --prod

# Or use Docker
docker build -t recoverai-frontend:latest ./frontend
```

### Environment Variables (Production)

```env
# Use production Razorpay keys
RAZORPAY_KEY_ID=rzp_live_xxxxx
RAZORPAY_KEY_SECRET=xxxxx
RAZORPAY_WEBHOOK_SECRET=xxxxx

# Use production Groq key (or self-hosted LLM)
GROQ_API_KEY=gsk_live_xxxxx

# Use managed databases
DATABASE_URL=postgresql://user:pass@prod-db.aws.com:5432/recoverai?sslmode=require
REDIS_URL=rediss://:pass@prod-redis.aws.com:6380

# Use managed Kafka
KAFKA_BROKERS=b-1.prod.kafka.aws.com:9092,b-2.prod.kafka.aws.com:9092

# Enable TLS
USE_TLS=true
```

---

## 11. Monitoring & Observability

### Logs

```bash
# Structured logging already implemented in Go
docker-compose logs -f api worker

# Python logging in AI service
docker-compose logs -f ai-service
```

### Metrics (To Implement)

- Use Prometheus + Grafana
- Key metrics:
  - Recovery rate %
  - AI confidence distribution
  - Policy rule triggers
  - Average recovery time
  - Revenue recovered per day

### Alerts (To Implement)

- Bank outage detected → Slack/PagerDuty
- Recovery rate drops below 60% → Alert
- Policy engine blocking >50% → Alert
- AI service down → Critical alert

---

## 12. Troubleshooting Common Issues

### Issue: "Kafka connection refused"
**Solution:** Wait for Kafka to fully start (health check passes)
```bash
docker-compose ps kafka
docker-compose logs kafka | grep "started"
```

### Issue: "Redis NOAUTH error"
**Solution:** Check REDIS_PASSWORD matches in docker-compose.yml and .env

### Issue: "Migration version conflict"
**Solution:** Reset migrations
```bash
migrate -path internal/db/migrations -database $DATABASE_URL force <version>
migrate -path internal/db/migrations -database $DATABASE_URL up
```

### Issue: "AI service returns STOP command"
**Solution:** Check Groq API quota
```bash
curl -H "Authorization: Bearer $GROQ_API_KEY" https://api.groq.com/openai/v1/models
```

### Issue: "Dashboard shows no data"
**Solution:** Backend endpoints not implemented yet. See `DASHBOARD_IMPLEMENTATION.md` section "Required Backend Endpoints"

---

## Summary

✅ **You now have:**
- Complete 5-stage recovery pipeline
- Dark-mode dashboard with real-time updates
- 11 UPI error code taxonomy
- Bank outage detection
- RBI compliance rules
- 3 AI agents (Risk Analyst, Strategist, Executor)
- 10 deterministic policy rules
- Full audit trail with 10 actors

🚀 **Next steps:**
1. Implement 3 missing backend endpoints for dashboard
2. Add authentication/authorization
3. Set up monitoring and alerts
4. Load test with k6 (see `load-test/payment_recovery.js`)
5. Deploy to production

---

**Need help?** Check:
- `docs/architecture.md` - System design
- `DASHBOARD_IMPLEMENTATION.md` - Frontend details
- `internal/policy/engine.go` - Policy rules
- `ai-service/agents/` - AI agent logic
