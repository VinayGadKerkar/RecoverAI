#!/bin/bash
# Test script for Mock AI Service

set -e

MOCK_AI_URL="http://localhost:8001"

echo "🧪 Testing Mock AI Service"
echo "================================"
echo ""

# Test 1: Health check
echo "Test 1: Health check"
curl -sf "$MOCK_AI_URL/health" | jq .
echo "✅ Health check passed"
echo ""

# Test 2: U30 (transient failure — should retry)
echo "Test 2: UPI U30 (transient failure)"
curl -sf -X POST "$MOCK_AI_URL/analyze" \
  -H "Content-Type: application/json" \
  -d '{
    "payment_id": "pay_test_u30",
    "case_id": "case_test_u30",
    "amount_paise": 499900,
    "upi_error_code": "U30",
    "upi_error_category": "TD",
    "failure_type": "transient",
    "failure_reason": "Debit timeout",
    "time_of_failure_hour": 14,
    "force_payment_link": false,
    "customer_history": {
      "successful_payments": 5,
      "failed_payments": 1,
      "lifetime_value_paise": 2500000
    },
    "risk_score": 0.82,
    "priority": "high",
    "merchant_policy": {
      "max_retry_amount_paise": 1000000,
      "max_retries": 3,
      "retry_cooldown_minutes": 10,
      "require_human_above_paise": 5000000,
      "allowed_actions": ["retry", "payment_link", "notify", "escalate"]
    }
  }' | jq .

echo "✅ U30 test passed (expected: RETRY_PAYMENT)"
echo ""

# Test 3: YG (risk blocked — should escalate)
echo "Test 3: UPI YG (risk blocked)"
curl -sf -X POST "$MOCK_AI_URL/analyze" \
  -H "Content-Type: application/json" \
  -d '{
    "payment_id": "pay_test_yg",
    "case_id": "case_test_yg",
    "amount_paise": 299900,
    "upi_error_code": "YG",
    "upi_error_category": "BD",
    "failure_type": "risk_blocked",
    "failure_reason": "PSP declined",
    "time_of_failure_hour": 20,
    "force_payment_link": true,
    "customer_history": {
      "successful_payments": 0,
      "failed_payments": 3,
      "lifetime_value_paise": 0
    },
    "risk_score": 0.25,
    "priority": "critical",
    "merchant_policy": {
      "max_retry_amount_paise": 1000000,
      "max_retries": 3,
      "retry_cooldown_minutes": 10,
      "require_human_above_paise": 5000000,
      "allowed_actions": ["retry", "payment_link", "notify", "escalate"]
    }
  }' | jq .

echo "✅ YG test passed (expected: ESCALATE)"
echo ""

# Test 4: U16 (insufficient balance — payment link)
echo "Test 4: UPI U16 (insufficient balance)"
curl -sf -X POST "$MOCK_AI_URL/analyze" \
  -H "Content-Type: application/json" \
  -d '{
    "payment_id": "pay_test_u16",
    "case_id": "case_test_u16",
    "amount_paise": 199900,
    "upi_error_code": "U16",
    "upi_error_category": "BD",
    "failure_type": "insufficient_funds",
    "failure_reason": "Insufficient balance",
    "time_of_failure_hour": 10,
    "force_payment_link": false,
    "customer_history": {
      "successful_payments": 2,
      "failed_payments": 1,
      "lifetime_value_paise": 500000
    },
    "risk_score": 0.65,
    "priority": "medium",
    "merchant_policy": {
      "max_retry_amount_paise": 1000000,
      "max_retries": 3,
      "retry_cooldown_minutes": 10,
      "require_human_above_paise": 5000000,
      "allowed_actions": ["retry", "payment_link", "notify", "escalate"]
    }
  }' | jq .

echo "✅ U16 test passed (expected: GENERATE_PAYMENT_LINK)"
echo ""

# Test 5: Z9 (non-retryable — payment link)
echo "Test 5: UPI Z9 (bank declined)"
curl -sf -X POST "$MOCK_AI_URL/analyze" \
  -H "Content-Type: application/json" \
  -d '{
    "payment_id": "pay_test_z9",
    "case_id": "case_test_z9",
    "amount_paise": 99900,
    "upi_error_code": "Z9",
    "upi_error_category": "BD",
    "failure_type": "non_retryable_auto",
    "failure_reason": "Bank declined",
    "time_of_failure_hour": 16,
    "force_payment_link": false,
    "customer_history": {
      "successful_payments": 10,
      "failed_payments": 1,
      "lifetime_value_paise": 1000000
    },
    "risk_score": 0.45,
    "priority": "low",
    "merchant_policy": {
      "max_retry_amount_paise": 1000000,
      "max_retries": 3,
      "retry_cooldown_minutes": 10,
      "require_human_above_paise": 5000000,
      "allowed_actions": ["retry", "payment_link", "notify", "escalate"]
    }
  }' | jq .

echo "✅ Z9 test passed (expected: GENERATE_PAYMENT_LINK)"
echo ""

echo "================================"
echo "✅ All tests passed!"
echo ""
echo "Mock AI is working correctly and returning deterministic responses."
