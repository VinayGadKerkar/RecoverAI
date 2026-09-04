# 🌐 Testing Complete Recovery Flow from Browser

## Quick Start

### 1. Open the Test Page

```bash
# Option A: Open directly in browser
start test-payment.html

# Option B: Serve it locally (if you have Python)
python -m http.server 8888
# Then open: http://localhost:8888/test-payment.html
```

### 2. Fill in the Form

The page is pre-filled with test data:
- **Amount**: ₹599 (you can change this)
- **Customer Name**: Test Customer
- **Email**: test@example.com
- **Phone**: 9999999999
- **UPI Error Code**: U30 (75% retry success rate)

### 3. Choose Your Test Scenario

#### ✅ Scenario A: Success Flow (No Recovery Needed)
1. Click **"✓ Pay & Succeed"**
2. When Razorpay opens, use: **Card: 4111 1111 1111 1111**
3. CVV: Any 3 digits (e.g., 123)
4. Expiry: Any future date (e.g., 12/25)
5. Click Pay
6. Payment succeeds immediately ✓

**Expected Result:**
- Payment status: `captured`
- No recovery case created (payment didn't fail)
- Shows in dashboard as successful payment

---

#### ❌ Scenario B: Failure Flow → AI Recovery (THIS IS WHAT YOU WANT!)

1. Click **"✗ Pay & Fail"**
2. When Razorpay opens, use the **FAILING CARD: 4000 0025 0000 3155**
3. CVV: Any 3 digits
4. Expiry: Any future date
5. Click Pay
6. Payment FAILS ❌

**Expected Result:**

📍 **Immediate:**
- Browser shows: "Payment failed! RecoverAI pipeline triggered!"

📍 **Within 5 seconds (Risk Processor):**
- System creates recovery case
- Assigns risk score based on error code

📍 **Within 10 seconds (AI Analysis):**
- AI Strategist analyzes the failure
- Recommends: **RETRY_PAYMENT** (U30 is retryable)
- Confidence: ~85-92%

📍 **Within 15 seconds (Execution):**
- Mock Retry Simulator executes
- **75% chance of SUCCESS** (for U30)
- If SUCCESS → publishes payment.captured webhook

📍 **Within 20 seconds (Final Status):**
- Database updated:
  - status = `recovered`
  - amount_recovered = 59900 (₹599)
- Dashboard shows recovered payment! 🎉

---

## 🔍 How to Verify the Recovery

### Method 1: Watch Logs in Real-Time

Open a terminal and watch the worker logs:

```powershell
docker logs recoverai-worker-1 -f
```

You'll see:
```
risk processor: processing failed payment
validator consumer: AI response received, action=RETRY_PAYMENT
executing retry, payment_id=pay_xxx
mock_retry: simulated SUCCESS ← KEY LINE!
retry succeeded - publishing payment.captured webhook
result processor: case finalized, final_status=recovered
```

### Method 2: Check Database

```powershell
# Find your payment (replace pay_xxx with actual payment ID from browser)
docker exec recoverai-postgres-1 psql -U recoverai -d recoverai -c "SELECT rc.status, rc.retry_count, rc.amount_recovered FROM recovery_cases rc JOIN payments p ON p.id = rc.payment_id WHERE p.razorpay_payment_id LIKE 'pay_%' ORDER BY rc.created_at DESC LIMIT 5;"
```

Look for:
- **status**: `recovered`
- **retry_count**: 1 or 2
- **amount_recovered**: > 0

### Method 3: Check Dashboard

```bash
# Open dashboard
start http://localhost:3000/dashboard

# Login if needed:
# Email: admin@demo.com
# Password: demo
```

Navigate to:
1. **Dashboard** → See "Revenue Recovered" metric increasing
2. **Cases** → Find your payment ID → Status should show "recovered"
3. **Analytics** → See recovery rate statistics

---

## 🎯 Different Error Codes to Test

Try different error codes to see different success rates:

| Error Code | Description | Mock Success Rate | Best For |
|------------|-------------|-------------------|----------|
| **U30** | Timeout/Transient | **75%** | High success demo |
| **U28** | Bank Server Down | **60%** | Moderate success |
| **U16** | Insufficient Funds | **30%** | Realistic scenario |
| **Z9** | Low Value Insufficient | **10%** | Failure testing |
| **U68** | Not Permitted | **20%** | Low success |

**Testing Tip:** Use U30 for demos (3 out of 4 attempts succeed)

---

## 🔄 Testing Multiple Attempts

To see both SUCCESS and FAILURE:

1. **Set Error Code to U30** (75% success)
2. Click **"✗ Pay & Fail"** → Use failing card
3. Wait 20 seconds
4. Repeat 3-4 times

**Expected Results:**
- ~75% will show `recovered` status
- ~25% will remain `failed` (realistic!)

Check all results:
```powershell
docker exec recoverai-postgres-1 psql -U recoverai -d recoverai -c "SELECT p.razorpay_payment_id, rc.status, rc.amount_recovered FROM recovery_cases rc JOIN payments p ON p.id = rc.payment_id WHERE rc.created_at > NOW() - INTERVAL '5 minutes' ORDER BY rc.created_at DESC;"
```

---

## 🐛 Troubleshooting

### Problem: "Failed to create order" error

**Solution:**
```powershell
# 1. Check if API is running
docker ps | Select-String api

# 2. Check API logs
docker logs recoverai-api-1 --tail 20

# 3. Test API directly
curl http://localhost:8080/health
```

### Problem: Payment fails but no recovery case created

**Solution:**
```powershell
# 1. Check if webhook was received
docker logs recoverai-api-1 | Select-String "webhook"

# 2. Check worker is running
docker ps | Select-String worker

# 3. Check Kafka is healthy
docker exec recoverai-kafka-1 kafka-topics.sh --list --bootstrap-server localhost:9092
```

### Problem: Mock retry doesn't execute

**Solution:**
```powershell
# Restart worker with latest code
docker-compose restart worker

# Wait 10 seconds for startup
Start-Sleep -Seconds 10

# Try payment again
```

### Problem: Browser says "CORS error"

**Solution:**
The test page must be opened from `http://localhost` or served via HTTP server, not `file://`

```powershell
# Serve via Python HTTP server
python -m http.server 8888
# Open: http://localhost:8888/test-payment.html
```

---

## 📊 What Success Looks Like

### Browser
```
✗ Payment failed! Error: BAD_REQUEST_ERROR
RecoverAI pipeline triggered! Check your dashboard.
```

### Worker Logs
```json
{"level":"INFO","msg":"mock_retry: simulated SUCCESS",
 "payment_id":"pay_xxxx","error_code":"U30","success_rate":0.75}

{"level":"INFO","msg":"result processor: case finalized",
 "final_status":"recovered","amount_recovered":59900}
```

### Database
```
 status    | recovered
 amount_recovered | 59900
```

### Dashboard
- Revenue Recovered: +₹599
- Recovery Rate: increased
- Case detail page shows: "Recovered ✓"

---

## 🎬 Full Demo Script

Perfect for showing to stakeholders:

```powershell
# Terminal 1: Watch logs
docker logs recoverai-worker-1 -f

# Terminal 2: Open dashboard
start http://localhost:3000/dashboard

# Browser: Open test page
start test-payment.html

# Browser: 
# 1. Set Amount: ₹999
# 2. Set Error: U30
# 3. Click "Pay & Fail"
# 4. Use card: 4000 0025 0000 3155
# 5. Watch Terminal 1 for "mock_retry: simulated SUCCESS"
# 6. Refresh dashboard → See +₹999 in Revenue Recovered
```

---

## 🔐 Test Card Reference

### Always SUCCEED:
- **4111 1111 1111 1111** (Visa)
- 5555 5555 5555 4444 (Mastercard)

### Always FAIL (Perfect for Recovery Testing):
- **4000 0025 0000 3155** ← USE THIS
- 4000 0000 0000 0002
- 4000 0000 0000 0101

### CVV & Expiry:
- CVV: Any 3 digits (123, 456, 789)
- Expiry: Any future date (12/25, 03/26, etc.)

---

## ✅ Success Checklist

After running a test, verify:

- [ ] Browser showed "Payment failed" message
- [ ] Worker logs show `mock_retry: simulated SUCCESS` (or FAILURE)
- [ ] Database has recovery case with correct status
- [ ] Dashboard shows increased Revenue Recovered (if success)
- [ ] Case detail page shows recovery attempt in timeline

---

## 💡 Pro Tips

1. **Best Error Code for Demos**: Use **U30** (75% success rate, looks good!)
2. **Test Both Outcomes**: Run 4 attempts with U30, expect 3 success + 1 failure
3. **Watch Logs Live**: Keep `docker logs -f` running for immediate feedback
4. **Check WebSocket**: Dashboard updates in real-time via WebSocket events
5. **Revenue Metrics**: Dashboard analytics auto-calculate from `amount_recovered`

---

## 🚀 Ready to Test!

1. ✅ Make sure all services are running: `docker-compose ps`
2. ✅ Open test page: `start test-payment.html`
3. ✅ Open logs: `docker logs recoverai-worker-1 -f`
4. ✅ Open dashboard: `start http://localhost:3000/dashboard`
5. ✅ Click **"Pay & Fail"**
6. ✅ Use failing card: **4000 0025 0000 3155**
7. ✅ Watch the magic happen! ✨

**The complete recovery flow from failed payment → recovered status with revenue tracking is now fully testable from your browser!** 🎉
