/**
 * RecoverAI — k6 Load Test
 * ─────────────────────────────────────────────────────────────────────────────
 *
 * Simulates realistic UPI payment failure patterns across four scenario types,
 * with edge-case injection every Nth event. HMAC-SHA256 signed on every request.
 *
 * Run (mock AI — zero tokens):
 *   k6 run --env USE_MOCK_AI=true load-test/payment_recovery.js
 *
 * Run (real AI, 20-call cap):
 *   k6 run --env USE_MOCK_AI=false --env TEST_AI_LIMIT=20 load-test/payment_recovery.js
 *
 * Or via Makefile:
 *   make load-test-mock
 *   make load-test-real
 *
 * ─── Scenario mix ─────────────────────────────────────────────────────────────
 *
 *   Scenario A — 40%  Transient TD failures    (U30, U28, RB, BT)
 *                       → should enter recovery pipeline and retry
 *
 *   Scenario B — 30%  Business decline recoverable  (U16)
 *                       → high LTV customers → payment link strategy
 *
 *   Scenario C — 20%  Non-retryable  (Z9, YG, Z8)
 *                       → stopped by validator (policy Rule 1 or negative ROI)
 *
 *   Scenario D — 10%  Outage burst  (15 rapid U28 → 5 batched)
 *                       → triggers Redis outage flag, subsequent cases → outage_batched
 *
 * ─── Edge case injection ──────────────────────────────────────────────────────
 *
 *   Every  50th event:  same X-Razorpay-Event-Id as a previous event
 *                        → tests idempotency (Redis SETNX dedup)
 *
 *   Every 100th event:  payment.captured for a previously failed payment_id
 *                        → tests customer self-recovery detection
 *
 * ─── Expected results (USE_MOCK_AI=true) ──────────────────────────────────────
 *
 *   All ~1000 events complete in < 2 minutes
 *   webhook_200_rate = 100%
 *   p95 < 200ms (mock AI returns in ~50ms, Kafka publish < 50ms)
 *   Zero Groq API calls — dashboard shows mix of all case statuses
 */

import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

// ─── Crypto for HMAC-SHA256 ───────────────────────────────────────────────────
// k6 ships SubtleCrypto — use the synchronous crypto.subtle via encode/decode.
import { crypto } from "k6/experimental/webcrypto";

// ─── Custom metrics ───────────────────────────────────────────────────────────

const webhookDuration    = new Trend("webhook_duration_ms",   true); // percentiles
const webhookSuccessRate = new Rate("webhook_200_rate");              // must stay > 0.99
const errorRate          = new Rate("error_rate");                    // must stay < 0.01

// Per-scenario counters — visible in the k6 summary
const scenarioACount = new Counter("scenario_a_transient_td");
const scenarioBCount = new Counter("scenario_b_business_decline");
const scenarioCCount = new Counter("scenario_c_non_retryable");
const scenarioDCount = new Counter("scenario_d_outage_burst");
const idempotencyTests = new Counter("edge_idempotency_tests");
const selfRecoveryTests = new Counter("edge_self_recovery_tests");

// ─── Configuration ────────────────────────────────────────────────────────────

const BASE_URL       = __ENV.BASE_URL       || "http://localhost:8080";
const WEBHOOK_SECRET = __ENV.WEBHOOK_SECRET || ""; // leave empty to skip HMAC (dev)
const USE_MOCK_AI    = __ENV.USE_MOCK_AI    || "true";
const TEST_AI_LIMIT  = __ENV.TEST_AI_LIMIT  || "0";

// X-Test-Mode header — logged by the API for post-test analysis
const TEST_MODE_HEADER = USE_MOCK_AI === "true" ? "mock" : "real";

// ─── k6 test options ─────────────────────────────────────────────────────────

export const options = {
  scenarios: {
    payment_failures: {
      executor:          "ramping-vus",
      startVUs:          0,
      stages: [
        { duration: "20s",  target: 10  }, // ramp up
        { duration: "100s", target: 10  }, // hold — steady 10 VUs for ~1000 events
        { duration: "10s",  target: 0   }, // ramp down
      ],
    },
  },

  thresholds: {
    // API must always return 200 for webhook ingestion
    "webhook_200_rate":   ["rate>0.99"],  // 99%+ success rate
    // Error rate must stay below 1%
    "error_rate":         ["rate<0.01"],
    // p95 < 500ms — even with Kafka publish overhead
    "http_req_duration":  ["p(95)<500", "p(99)<1000"],
    // Custom duration metric
    "webhook_duration_ms": ["p(95)<500"],
  },
};

// ─── UPI error code taxonomy ──────────────────────────────────────────────────

// Scenario A: Technical Decline (TD) — highly retryable
const SCENARIO_A_CODES = ["U30", "U28", "RB", "BT"];

// Scenario B: Business Decline — recoverable via payment link
const SCENARIO_B_CODES = ["U16"];

// Scenario C: Non-retryable — stopped at validator / policy
const SCENARIO_C_CODES = ["Z9", "YG", "Z8"];

// Scenario D: Outage burst code
const SCENARIO_D_CODE = "U28";

// ─── Realistic amounts by scenario ───────────────────────────────────────────

// Scenario A: mid-to-high value (high LTV customers)
const AMOUNTS_A = [149900, 299900, 499900, 749900, 999900];
// Scenario B: medium value (insufficient balance — realistic range)
const AMOUNTS_B = [49900, 99900, 149900, 249900];
// Scenario C: low-to-medium (non-retryable, often low-value new customers)
const AMOUNTS_C = [9900, 29900, 49900, 99900];
// Scenario D: outage — same medium amount (bulk batch scenario)
const AMOUNT_D  = 899900;

// ─── Realistic customer profiles ─────────────────────────────────────────────

const HIGH_LTV_CUSTOMERS = [
  { email: "aarav.sharma@gmail.com",  vpa: "aarav.sharma@okaxis",  contact: "+919876543210" },
  { email: "priya.patel@outlook.com", vpa: "priya.patel@okhdfcbank", contact: "+918765432109" },
  { email: "rohan.mehta@yahoo.com",   vpa: "rohan.mehta@oksbi",    contact: "+917654321098" },
  { email: "ananya.iyer@gmail.com",   vpa: "ananya.iyer@okicici",  contact: "+916543210987" },
  { email: "vikram.nair@gmail.com",   vpa: "vikram.nair@okaxis",   contact: "+915432109876" },
];

const NEW_CUSTOMERS = [
  { email: "aisha.khan@gmail.com",    vpa: "aisha.khan@oksbi",     contact: "+914321234505" },
  { email: "dev.goyal@gmail.com",     vpa: "dev.goyal@okhdfcbank", contact: "+913211234504" },
  { email: "nisha.dubey@gmail.com",   vpa: "nisha.dubey@okaxis",   contact: "+910981234501" },
];

const ALL_BANKS = ["SBI", "HDFC", "ICICI", "AXIS", "KOTAK", "PNB", "BOB"];

// ─── Shared state between iterations ─────────────────────────────────────────

// Tracks recently sent payment IDs for self-recovery edge case
const recentFailedPaymentIds = [];
// Tracks recently used event IDs for idempotency edge case
const recentEventIds = [];

let globalEventCounter = 0;

// ─── HMAC-SHA256 helper ───────────────────────────────────────────────────────
// k6 WebCrypto is async — we use the synchronous __VU / exec approach.
// For load tests where secret is set, this computes a real signature.
// If WEBHOOK_SECRET is empty, returns a sentinel that the server accepts in dev mode.

async function hmacSHA256(secret, body) {
  if (!secret) {
    // No secret configured — server skips verification when secret is unset
    return "dev_no_secret";
  }

  const encoder = new TextEncoder();
  const keyData = encoder.encode(secret);
  const msgData = encoder.encode(body);

  const key = await crypto.subtle.importKey(
    "raw",
    keyData,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"]
  );

  const sigBuffer = await crypto.subtle.sign("HMAC", key, msgData);
  const sigBytes  = new Uint8Array(sigBuffer);

  // Convert to hex string
  return Array.from(sigBytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

// ─── Payload builders ─────────────────────────────────────────────────────────

function randomItem(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

function shortId() {
  return Math.random().toString(36).slice(2, 10);
}

function nowUnix() {
  return Math.floor(Date.now() / 1000);
}

/**
 * Build a Razorpay payment.failed webhook payload.
 */
function buildFailedPayload(paymentId, amount, upiErrorCode, customer, bank) {
  const errDescriptions = {
    U30:  "Debit timeout — bank did not respond",
    U28:  "Bank server down or unresponsive",
    RB:   "Bank blocked transaction due to load",
    BT:   "Beneficiary bank timeout",
    U16:  "Customer account has insufficient balance",
    Z9:   "Bank declined — insufficient funds in account",
    YG:   "Transaction blocked by risk engine",
    Z8:   "Amount exceeds per-transaction limit",
    U68:  "Transaction type not permitted for this account",
    Z7:   "Velocity limit exceeded — too many transactions",
    U69:  "Collect request expired before customer approved",
  };

  return JSON.stringify({
    entity:     "event",
    account_id: "acc_loadtest",
    event:      "payment.failed",
    contains:   ["payment"],
    payload: {
      payment: {
        entity: {
          id:                paymentId,
          amount:            amount,
          currency:          "INR",
          status:            "failed",
          order_id:          `order_${shortId()}`,
          method:            "upi",
          error_code:        upiErrorCode,
          error_description: errDescriptions[upiErrorCode] || `UPI error ${upiErrorCode}`,
          error_source:      "bank",
          error_step:        "debit",
          error_reason:      "payment_failed",
          bank:              bank,
          vpa:               customer.vpa,
          email:             customer.email,
          contact:           customer.contact,
          created_at:        nowUnix(),
        },
      },
    },
    created_at: nowUnix(),
  });
}

/**
 * Build a Razorpay payment.captured webhook payload.
 * Used for self-recovery edge case — same payment_id as a prior failure.
 */
function buildCapturedPayload(paymentId, amount, customer) {
  return JSON.stringify({
    entity:     "event",
    account_id: "acc_loadtest",
    event:      "payment.captured",
    contains:   ["payment"],
    payload: {
      payment: {
        entity: {
          id:         paymentId,
          amount:     amount,
          currency:   "INR",
          status:     "captured",
          order_id:   `order_${shortId()}`,
          method:     "upi",
          bank:       "HDFC",
          vpa:        customer.vpa,
          email:      customer.email,
          contact:    customer.contact,
          created_at: nowUnix(),
        },
      },
    },
    created_at: nowUnix(),
  });
}

// ─── Request sender ───────────────────────────────────────────────────────────

async function sendWebhook(body, eventId) {
  const sig = await hmacSHA256(WEBHOOK_SECRET, body);

  const params = {
    headers: {
      "Content-Type":          "application/json",
      "X-Razorpay-Signature":  sig,
      "X-Razorpay-Event-Id":   eventId,
      // Custom header so API logs know which AI mode was active during this test
      "X-Test-Mode":           TEST_MODE_HEADER,
      // Pass AI limit for API-side logging/tracing
      "X-Test-AI-Limit":       TEST_AI_LIMIT,
    },
  };

  const start = Date.now();
  const res   = http.post(`${BASE_URL}/webhooks/razorpay`, body, params);
  const dur   = Date.now() - start;

  webhookDuration.add(dur);

  const is200 = res.status === 200;
  webhookSuccessRate.add(is200);
  errorRate.add(!is200);

  check(res, {
    "status 200":           (r) => r.status === 200,
    "response time < 500ms": (r) => r.timings.duration < 500,
  });

  return { is200, status: res.status, body: res.body };
}

// ─── Scenario implementations ─────────────────────────────────────────────────

/**
 * Scenario A (40%): Transient TD failure
 * High LTV customer, mid-to-high amount, retryable UPI code.
 * Expected pipeline outcome: risk_scored → validator PASS → AI → retry action
 */
async function runScenarioA() {
  const code      = randomItem(SCENARIO_A_CODES);
  const customer  = randomItem(HIGH_LTV_CUSTOMERS);
  const amount    = randomItem(AMOUNTS_A);
  const bank      = randomItem(ALL_BANKS);
  const paymentId = `pay_lt_a_${shortId()}`;
  const eventId   = `evt_lt_a_${shortId()}`;

  const body = buildFailedPayload(paymentId, amount, code, customer, bank);
  const result = await sendWebhook(body, eventId);

  if (result.is200) {
    // Track for self-recovery edge case (every 100th event)
    if (recentFailedPaymentIds.length < 50) {
      recentFailedPaymentIds.push({ paymentId, amount, customer });
    }
    recentEventIds.push(eventId);
    scenarioACount.add(1);
  }
}

/**
 * Scenario B (30%): Business decline — recoverable
 * U16 (insufficient balance), medium amount.
 * Expected pipeline outcome: validator PASS → AI → payment_link strategy
 */
async function runScenarioB() {
  const code      = randomItem(SCENARIO_B_CODES);
  const customer  = randomItem(HIGH_LTV_CUSTOMERS);
  const amount    = randomItem(AMOUNTS_B);
  const bank      = randomItem(["SBI", "HDFC", "ICICI"]);
  const paymentId = `pay_lt_b_${shortId()}`;
  const eventId   = `evt_lt_b_${shortId()}`;

  const body = buildFailedPayload(paymentId, amount, code, customer, bank);
  await sendWebhook(body, eventId);
  scenarioBCount.add(1);
}

/**
 * Scenario C (20%): Non-retryable
 * Z9/YG/Z8 codes or tiny amount + new customer → validator blocks (negative ROI or Policy Rule 1).
 * Expected pipeline outcome: validator BLOCKED → not_worth_recovering | stopped
 */
async function runScenarioC() {
  const code      = randomItem(SCENARIO_C_CODES);
  const customer  = randomItem(NEW_CUSTOMERS);
  const amount    = randomItem(AMOUNTS_C);
  const bank      = randomItem(ALL_BANKS);
  const paymentId = `pay_lt_c_${shortId()}`;
  const eventId   = `evt_lt_c_${shortId()}`;

  const body = buildFailedPayload(paymentId, amount, code, customer, bank);
  await sendWebhook(body, eventId);
  scenarioCCount.add(1);
}

/**
 * Scenario D (10%): Bank outage burst
 * Sends 15 rapid U28 failures to cross the 10/5min outage detection threshold,
 * then 5 more that should all be immediately batched (outage_batched status).
 *
 * NOTE: This is intentionally bursty — all 20 requests fire with minimal sleep.
 * The Risk Engine's Redis counter will hit 10 and set bank_outage:U28 during
 * the first 15, so the last 5 should skip the full pipeline.
 */
async function runScenarioD() {
  // Phase 1: 15 rapid failures to trigger detection (threshold = 10)
  for (let i = 0; i < 15; i++) {
    const paymentId = `pay_lt_d1_${shortId()}`;
    const eventId   = `evt_lt_d1_${shortId()}`;
    const customer  = randomItem(HIGH_LTV_CUSTOMERS);
    const body = buildFailedPayload(paymentId, AMOUNT_D, SCENARIO_D_CODE, customer, "SBI");

    await sendWebhook(body, eventId);
    sleep(0.05); // 50ms between burst events — rapid but not instantaneous
  }

  // Brief pause — outage flag should now be set in Redis
  sleep(0.5);

  // Phase 2: 5 more — these should be batched immediately
  for (let i = 0; i < 5; i++) {
    const paymentId = `pay_lt_d2_${shortId()}`;
    const eventId   = `evt_lt_d2_${shortId()}`;
    const customer  = randomItem(HIGH_LTV_CUSTOMERS);
    const body = buildFailedPayload(paymentId, AMOUNT_D, SCENARIO_D_CODE, customer, "SBI");

    await sendWebhook(body, eventId);
    sleep(0.1);
  }

  scenarioDCount.add(1); // counts one "burst" event
}

// ─── Edge case: idempotency test (every 50th event) ──────────────────────────

/**
 * Resend a recently used event ID — the server should deduplicate via Redis SETNX
 * and return 200 {"status":"ok","duplicate":true} without creating a new recovery case.
 */
async function runIdempotencyTest() {
  if (recentEventIds.length === 0) return;

  const dupEventId = recentEventIds[Math.floor(Math.random() * recentEventIds.length)];
  const paymentId  = `pay_lt_dup_${shortId()}`;
  const body = buildFailedPayload(paymentId, 99900, "U30", HIGH_LTV_CUSTOMERS[0], "HDFC");

  const result = await sendWebhook(body, dupEventId);

  // 200 is expected even for duplicates — server returns {"duplicate":true}
  if (result.is200) {
    idempotencyTests.add(1);
  }
}

// ─── Edge case: customer self-recovery (every 100th event) ───────────────────

/**
 * Send payment.captured for a payment_id that previously had a payment.failed.
 * The webhook handler should detect the open recovery case and mark it as
 * customer_self_recovered.
 */
async function runSelfRecoveryTest() {
  if (recentFailedPaymentIds.length === 0) return;

  const entry    = recentFailedPaymentIds[
    Math.floor(Math.random() * recentFailedPaymentIds.length)
  ];
  const eventId  = `evt_lt_cap_${shortId()}`;
  const body     = buildCapturedPayload(entry.paymentId, entry.amount, entry.customer);

  const result   = await sendWebhook(body, eventId);
  if (result.is200) {
    selfRecoveryTests.add(1);
  }
}

// ─── Main scenario selector ────────────────────────────────────────────────────

/**
 * Weighted random scenario picker.
 *
 *  0.00 – 0.40 → A (40%)  transient TD
 *  0.40 – 0.70 → B (30%)  business decline recoverable
 *  0.70 – 0.90 → C (20%)  non-retryable
 *  0.90 – 1.00 → D (10%)  outage burst
 */
function pickScenario() {
  const r = Math.random();
  if (r < 0.40) return "A";
  if (r < 0.70) return "B";
  if (r < 0.90) return "C";
  return "D";
}

// ─── Default function (k6 VU loop) ───────────────────────────────────────────

export default async function () {
  globalEventCounter++;

  // Edge case injection: idempotency test every 50th event
  if (globalEventCounter % 50 === 0) {
    await runIdempotencyTest();
    sleep(0.1);
    return;
  }

  // Edge case injection: customer self-recovery test every 100th event
  if (globalEventCounter % 100 === 0) {
    await runSelfRecoveryTest();
    sleep(0.1);
    return;
  }

  // Standard scenario (weighted distribution)
  const scenario = pickScenario();
  switch (scenario) {
    case "A": await runScenarioA(); break;
    case "B": await runScenarioB(); break;
    case "C": await runScenarioC(); break;
    case "D": await runScenarioD(); break;
  }

  // Short sleep to avoid saturating localhost in dev
  // At 10 VUs × 1000ms/loop = ~10 req/s baseline.
  // Scenario D sends 20 requests per iteration, so its effective rate is higher.
  sleep(0.1);
}

// ─── Setup and teardown ───────────────────────────────────────────────────────

export function setup() {
  console.log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━");
  console.log("  RecoverAI Load Test");
  console.log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━");
  console.log(`  Target:      ${BASE_URL}`);
  console.log(`  AI mode:     ${TEST_MODE_HEADER.toUpperCase()} (X-Test-Mode: ${TEST_MODE_HEADER})`);
  console.log(`  HMAC secret: ${WEBHOOK_SECRET ? "SET (real signatures)" : "NOT SET (dev mode)"}`);
  console.log(`  Scenario mix: A=40% B=30% C=20% D=10%`);
  console.log(`  Edge cases:   idempotency every 50th, self-recovery every 100th`);
  console.log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━");

  // Verify API is reachable before starting
  const res = http.get(`${BASE_URL}/health`);
  if (res.status !== 200) {
    throw new Error(`API not reachable at ${BASE_URL}/health (status ${res.status}). Run: make dev`);
  }

  console.log("  API health: OK ✓");
  console.log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━");
}

export function teardown() {
  console.log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━");
  console.log("  Load test complete.");
  console.log("");
  console.log(`  AI mode was: ${TEST_MODE_HEADER.toUpperCase()}`);
  if (TEST_MODE_HEADER === "mock") {
    console.log("  Zero Groq tokens consumed.");
    console.log("  Dashboard should show a mix of all case statuses.");
  } else {
    console.log(`  Real AI calls capped at: ${TEST_AI_LIMIT || "unlimited"}`);
  }
  console.log("");
  console.log("  Check dashboard:  http://localhost:3000");
  console.log("  Check AI mode:    make ai-status");
  console.log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━");
}

// ─── Custom summary output ────────────────────────────────────────────────────

export function handleSummary(data) {
  const metrics = data.metrics;

  const p95 = metrics["http_req_duration"]
    ? metrics["http_req_duration"].values["p(95)"].toFixed(0)
    : "N/A";
  const p99 = metrics["http_req_duration"]
    ? metrics["http_req_duration"].values["p(99)"].toFixed(0)
    : "N/A";
  const successRate = metrics["webhook_200_rate"]
    ? (metrics["webhook_200_rate"].values["rate"] * 100).toFixed(2)
    : "N/A";
  const errRate = metrics["error_rate"]
    ? (metrics["error_rate"].values["rate"] * 100).toFixed(2)
    : "N/A";
  const reqsPerSec = metrics["http_reqs"]
    ? metrics["http_reqs"].values["rate"].toFixed(1)
    : "N/A";

  const scA = metrics["scenario_a_transient_td"]
    ? metrics["scenario_a_transient_td"].values["count"]
    : 0;
  const scB = metrics["scenario_b_business_decline"]
    ? metrics["scenario_b_business_decline"].values["count"]
    : 0;
  const scC = metrics["scenario_c_non_retryable"]
    ? metrics["scenario_c_non_retryable"].values["count"]
    : 0;
  const scD = metrics["scenario_d_outage_burst"]
    ? metrics["scenario_d_outage_burst"].values["count"]
    : 0;
  const idem = metrics["edge_idempotency_tests"]
    ? metrics["edge_idempotency_tests"].values["count"]
    : 0;
  const selfRec = metrics["edge_self_recovery_tests"]
    ? metrics["edge_self_recovery_tests"].values["count"]
    : 0;

  const passed  = data.state.testRunDurationMs < data.options?.thresholds ? "✅ PASSED" : "";
  const summary = `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  RecoverAI Load Test Summary  [AI mode: ${TEST_MODE_HEADER.toUpperCase()}]
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Performance
  ──────────────────────────────────────────
  Requests/sec         ${reqsPerSec} req/s
  p95 latency          ${p95} ms          (threshold: < 500ms)
  p99 latency          ${p99} ms
  Webhook 200 rate     ${successRate}%    (threshold: > 99%)
  Error rate           ${errRate}%        (threshold: < 1%)

  Scenario distribution
  ──────────────────────────────────────────
  Scenario A  Transient TD (U30/U28/RB/BT)      ${scA} events  (target ~40%)
  Scenario B  Business decline recoverable (U16) ${scB} events  (target ~30%)
  Scenario C  Non-retryable (Z9/YG/Z8)           ${scC} events  (target ~20%)
  Scenario D  Outage burst (15+5 U28)            ${scD} bursts  (target ~10%)

  Edge cases injected
  ──────────────────────────────────────────
  Idempotency tests    ${idem}  (every 50th event — duplicate event ID)
  Self-recovery tests  ${selfRec}  (every 100th event — payment.captured for failed ID)

  Dashboard
  ──────────────────────────────────────────
  Open: http://localhost:3000
  Expected case statuses:
    recovered / in_progress    ← Scenario A
    not_worth_recovering       ← Scenario C (low-value + new customer)
    stopped                    ← Scenario C (YG → policy Rule 1)
    outage_batched             ← Scenario D (post-threshold U28)
    customer_self_recovered    ← Edge case self-recovery injections

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
`;

  return {
    stdout: summary,
  };
}
