/**
 * RecoverAI — k6 Load Test
 * Tests: webhook ingestion throughput + recovery pipeline latency
 *
 * Run: k6 run load-test/payment_recovery.js
 */

import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

// ─── Custom metrics ──────────────────────────────────────────────────────────
const webhookErrorRate = new Rate("webhook_errors");
const webhookDuration = new Trend("webhook_duration_ms");

// ─── Test config ──────────────────────────────────────────────────────────────
export const options = {
  scenarios: {
    // Ramp to 100 req/s over 30s, hold 1 min, ramp down
    webhook_ingestion: {
      executor: "ramping-arrival-rate",
      startRate: 10,
      timeUnit: "1s",
      preAllocatedVUs: 50,
      maxVUs: 200,
      stages: [
        { duration: "30s", target: 100 },
        { duration: "60s", target: 100 },
        { duration: "15s", target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_duration: ["p(95)<500"], // 95th percentile under 500ms
    webhook_errors: ["rate<0.01"],    // less than 1% errors
  },
};

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

// Sample UPI error codes from the taxonomy
const UPI_ERRORS = ["U16", "U30", "Z9", "U68", "RB", "YG"];
const BANKS = ["SBI", "HDFC", "ICICI", "AXIS", "KOTAK"];

function randomItem(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

function makeWebhookPayload() {
  const paymentId = `pay_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`;
  const errorCode = randomItem(UPI_ERRORS);
  const amount = Math.floor(Math.random() * 500000) + 10000; // ₹100 – ₹5,000

  return JSON.stringify({
    entity: "event",
    account_id: "acc_test01",
    event: "payment.failed",
    contains: ["payment"],
    payload: {
      payment: {
        entity: {
          id: paymentId,
          amount: amount,
          currency: "INR",
          status: "failed",
          order_id: `order_${Math.random().toString(36).slice(2, 10)}`,
          merchant_id: "merch_01",
          method: "upi",
          error_code: errorCode,
          error_description: `UPI error ${errorCode}`,
          error_source: "bank",
          error_step: "debit",
          error_reason: "payment_failed",
          bank: randomItem(BANKS),
          vpa: `customer${Math.floor(Math.random() * 1000)}@upi`,
          email: "customer@example.com",
          contact: "+919876543210",
          created_at: Math.floor(Date.now() / 1000),
        },
      },
    },
    created_at: Math.floor(Date.now() / 1000),
  });
}

// ─── Default scenario ─────────────────────────────────────────────────────────
export default function () {
  const payload = makeWebhookPayload();

  // Simulate a valid HMAC signature header (test mode — server skips if secret unset)
  const params = {
    headers: {
      "Content-Type": "application/json",
      "X-Razorpay-Signature": "test_signature",
      "X-Razorpay-Event-Id": `evt_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
    },
  };

  const start = Date.now();
  const res = http.post(`${BASE_URL}/webhooks/razorpay`, payload, params);
  webhookDuration.add(Date.now() - start);

  const ok = check(res, {
    "webhook accepted (200)": (r) => r.status === 200,
    "response time < 200ms": (r) => r.timings.duration < 200,
  });

  webhookErrorRate.add(!ok);
  sleep(0.01);
}
