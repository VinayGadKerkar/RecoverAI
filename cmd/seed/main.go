// cmd/seed/main.go — Development data seeder for RecoverAI
//
// Idempotent: re-running clears existing demo data and re-seeds cleanly.
// Run with: go run ./cmd/seed/main.go
//        or: make seed

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func ago(d time.Duration) time.Time {
	return time.Now().UTC().Add(-d)
}

// ─── Data definitions ─────────────────────────────────────────────────────────

// Indian names for realistic demo data
var indianCustomers = []struct {
	name  string
	email string
	phone string
}{
	// High LTV (indices 0-4)
	{"Aarav Sharma", "aarav.sharma@gmail.com", "+919876543210"},
	{"Priya Patel", "priya.patel@outlook.com", "+918765432109"},
	{"Rohan Mehta", "rohan.mehta@yahoo.com", "+917654321098"},
	{"Ananya Iyer", "ananya.iyer@gmail.com", "+916543210987"},
	{"Vikram Nair", "vikram.nair@company.in", "+915432109876"},
	// Medium LTV (indices 5-14)
	{"Sneha Reddy", "sneha.reddy@gmail.com", "+914321098765"},
	{"Arjun Gupta", "arjun.gupta@outlook.com", "+913210987654"},
	{"Kavya Pillai", "kavya.pillai@gmail.com", "+912109876543"},
	{"Kunal Joshi", "kunal.joshi@hotmail.com", "+911098765432"},
	{"Meera Krishnan", "meera.k@gmail.com", "+910987654321"},
	{"Rahul Verma", "rahul.verma@gmail.com", "+919871234560"},
	{"Divya Bose", "divya.bose@gmail.com", "+918761234509"},
	{"Amit Choudhary", "amit.c@company.in", "+917651234508"},
	{"Pooja Malhotra", "pooja.m@outlook.com", "+916541234507"},
	{"Nikhil Singh", "nikhil.s@gmail.com", "+915431234506"},
	// New / Low LTV (indices 15-19)
	{"Aisha Khan", "aisha.khan@gmail.com", "+914321234505"},
	{"Dev Goyal", "dev.goyal@gmail.com", "+913211234504"},
	{"Simran Kaur", "simran.kaur@gmail.com", "+912101234503"},
	{"Ravi Teja", "ravi.teja@gmail.com", "+911091234502"},
	{"Nisha Dubey", "nisha.dubey@gmail.com", "+910981234501"},
}

// UPI error codes for the 10 failed payments
var failedPaymentScenarios = []struct {
	errorCode   string
	amount      int64 // paise
	description string
}{
	{"U30", 349900, "Debit timeout — retryable"},
	{"U28", 899900, "Bank server down — retryable after recovery"},
	{"RB", 149900, "Bank load block — transient"},
	{"U16", 249900, "Insufficient balance — needs payment link"},
	{"Z9", 9900, "Insufficient funds (low-value, new customer)"},
	{"Z8", 59900, "Per-transaction limit exceeded"},
	{"Z7", 199900, "Velocity limit reached"},
	{"U68", 499900, "Transaction not permitted"},
	{"YG", 749900, "Risk threshold exceeded — escalate only"},
	{"BT", 129900, "Beneficiary timeout — retryable"},
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	ctx := context.Background()

	dbURL := getEnv("DATABASE_URL",
		"postgres://recoverai:secret@localhost:5432/recoverai?sslmode=disable")

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping postgres: %v\n\nIs docker-compose running? Try: docker-compose up -d", err)
	}

	log.Println("Connected to PostgreSQL. Starting seed...")

	s := &seeder{db: pool, ctx: ctx}

	// ── Idempotent clean-up of any previous demo seed ────────────────────────
	s.cleanup()

	// ── 1. Merchant ──────────────────────────────────────────────────────────
	merchantID := s.seedMerchant()

	// ── 2. Recovery policy ───────────────────────────────────────────────────
	s.seedRecoveryPolicy(merchantID)

	// ── 3. Customers (20 total: 5 high + 10 medium + 5 new) ─────────────────
	customerIDs := s.seedCustomers(merchantID)

	// ── 4. 50 historical payments (40 captured + 10 failed) ──────────────────
	paymentIDs := s.seedHistoricalPayments(merchantID, customerIDs)

	// ── 5. Demo recovery cases A, B, C, D ────────────────────────────────────
	s.seedDemoCaseA(merchantID, customerIDs, paymentIDs)
	s.seedDemoCaseB(merchantID, customerIDs, paymentIDs)
	s.seedDemoCaseC(merchantID, customerIDs, paymentIDs)
	s.seedDemoCaseD(merchantID, customerIDs, paymentIDs)

	// ── Summary ───────────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════")
	fmt.Println("  RecoverAI Demo Seed Complete")
	fmt.Println("════════════════════════════════════════════════")
	fmt.Printf("  Seeded: 1 merchant, 20 customers, 50 payments, 4 demo cases\n")
	fmt.Println()
	fmt.Println("  Dashboard will show:")
	fmt.Println("    ✅  ₹4,999 recovered   (Case A — U30 transient → retry succeeded)")
	fmt.Println("    🚫  ₹99 blocked        (Case B — Z9 negative ROI → not worth recovering)")
	fmt.Println("    👤  ₹2,499 self-paid   (Case C — U16 → customer paid themselves)")
	fmt.Println("    🌊  ₹8,999 outage      (Case D — U28 bank down → outage batched)")
	fmt.Println()
	fmt.Println("  Open the dashboard: http://localhost:3000")
	fmt.Println("════════════════════════════════════════════════")
}

// ─── Seeder ───────────────────────────────────────────────────────────────────

type seeder struct {
	db  *pgxpool.Pool
	ctx context.Context
}

// cleanup removes any previously seeded demo data identified by the sentinel name.
func (s *seeder) cleanup() {
	log.Println("Cleaning up previous demo seed...")

	// Find and delete merchant + cascade
	var merchantID string
	s.db.QueryRow(s.ctx,
		`SELECT id FROM merchants WHERE name = 'Demo Store' LIMIT 1`,
	).Scan(&merchantID)

	if merchantID != "" {
		s.exec(`DELETE FROM audit_logs
				WHERE entity_id IN (
					SELECT id FROM recovery_cases WHERE merchant_id = $1
				)`, merchantID)
		s.exec(`DELETE FROM recovery_actions
				WHERE case_id IN (
					SELECT id FROM recovery_cases WHERE merchant_id = $1
				)`, merchantID)
		s.exec(`DELETE FROM bank_outage_events WHERE merchant_id = $1`, merchantID)
		s.exec(`DELETE FROM recovery_cases WHERE merchant_id = $1`, merchantID)
		s.exec(`DELETE FROM webhook_events WHERE merchant_id = $1`, merchantID)
		s.exec(`DELETE FROM payments WHERE merchant_id = $1`, merchantID)
		s.exec(`DELETE FROM recovery_policies WHERE merchant_id = $1`, merchantID)
		s.exec(`DELETE FROM customers WHERE merchant_id = $1`, merchantID)
		s.exec(`DELETE FROM merchants WHERE id = $1`, merchantID)
		log.Printf("  Removed previous demo merchant %s", merchantID)
	}
}

// ── 1. Merchant ───────────────────────────────────────────────────────────────

func (s *seeder) seedMerchant() string {
	keyID := getEnv("RAZORPAY_KEY_ID", "rzp_test_demo00000000")
	webhookSecret := getEnv("RAZORPAY_WEBHOOK_SECRET", "demo-webhook-secret")

	settings := map[string]any{
		"business_type":   "ecommerce",
		"category":        "fashion",
		"monthly_volume":  "₹5L-₹20L",
		"primary_method":  "upi",
	}

	var id string
	err := s.db.QueryRow(s.ctx, `
		INSERT INTO merchants (razorpay_key_id, name, webhook_secret, settings)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, keyID, "Demo Store", webhookSecret, mustJSON(settings)).Scan(&id)
	if err != nil {
		log.Fatalf("seed merchant: %v", err)
	}

	log.Printf("  Merchant: Demo Store (%s)", id)
	return id
}

// ── 2. Recovery policy ────────────────────────────────────────────────────────

func (s *seeder) seedRecoveryPolicy(merchantID string) {
	s.exec(`
		INSERT INTO recovery_policies (
			merchant_id,
			max_retry_amount_paise,
			max_retries,
			retry_cooldown_minutes,
			require_human_above,
			allowed_actions,
			mandate_min_retry_hours,
			high_value_threshold_paise,
			min_recovery_roi,
			outage_detection_threshold
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		merchantID,
		1_000_000,                                             // ₹10,000 — auto-retry ceiling
		2,                                                     // max 2 retries
		5,                                                     // 5 min cooldown
		5_000_000,                                             // ₹50,000 — human approval threshold
		[]string{"retry", "payment_link", "notify", "escalate"}, // allowed actions
		24,                                                    // RBI: 24h mandate window
		1_500_000,                                             // ₹15,000 — RBI high-value threshold
		0,                                                     // min ROI: break-even
		10,                                                    // outage: 10 failures/5min
	)
	log.Println("  Recovery policy: max_retry=₹10K, max_retries=2, cooldown=5min")
}

// ── 3. Customers ──────────────────────────────────────────────────────────────

func (s *seeder) seedCustomers(merchantID string) []string {
	ids := make([]string, 0, 20)

	for i, c := range indianCustomers {
		var (
			ltv                int64
			successfulPayments int
			failedPayments     int
		)

		switch {
		case i < 5: // High LTV
			ltv = int64(3_000_000 + rand.Intn(5_000_000)) // ₹30K–₹80K
			successfulPayments = 8 + rand.Intn(8)          // 8–15
			failedPayments = rand.Intn(3)

		case i < 15: // Medium LTV
			ltv = int64(500_000 + rand.Intn(2_400_000)) // ₹5K–₹29K
			successfulPayments = 2 + rand.Intn(6)        // 2–7
			failedPayments = rand.Intn(2)

		default: // New / Low LTV
			ltv = int64(rand.Intn(400_000)) // ₹0–₹4K
			successfulPayments = rand.Intn(2)
			failedPayments = rand.Intn(3)
		}

		var id string
		err := s.db.QueryRow(s.ctx, `
			INSERT INTO customers (
				merchant_id, email, phone,
				lifetime_value, successful_payments, failed_payments,
				risk_score
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id
		`,
			merchantID,
			c.email,
			c.phone,
			ltv,
			successfulPayments,
			failedPayments,
			riskScore(successfulPayments, failedPayments),
		).Scan(&id)
		if err != nil {
			log.Fatalf("seed customer %s: %v", c.name, err)
		}

		ids = append(ids, id)
	}

	log.Printf("  Customers: 20 seeded (5 high-LTV, 10 medium, 5 new)")
	return ids
}

func riskScore(successful, failed int) float64 {
	if successful+failed == 0 {
		return 0.50
	}
	failRate := float64(failed) / float64(successful+failed)
	return min64(0.99, max64(0.01, failRate))
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ── 4. Historical payments ────────────────────────────────────────────────────

// paymentSet is returned by seedHistoricalPayments for use in demo cases.
type paymentSet struct {
	// Captured payments by customer bucket index
	captured []string
	// Failed payments indexed by UPI error code scenario
	failed map[string]string // error_code → payment UUID
}

func (s *seeder) seedHistoricalPayments(merchantID string, customerIDs []string) paymentSet {
	capturedUUIDs := make([]string, 0, 40)
	failedByCode := make(map[string]string)

	// ── 40 captured payments spread across customers ──────────────────────────
	amounts := []int64{
		49900, 99900, 149900, 199900, 249900, 299900, 349900, 399900,
		449900, 499900, 549900, 599900, 649900, 699900, 749900, 799900,
		849900, 899900, 949900, 999900, 49900, 99900, 149900, 199900,
		249900, 299900, 349900, 399900, 449900, 499900, 549900, 599900,
		649900, 699900, 749900, 799900, 849900, 899900, 949900, 999900,
	}

	for i, amt := range amounts {
		custIdx := i % len(customerIDs)
		rzpID := fmt.Sprintf("pay_hist_cap_%02d_%s", i, shortHex())

		var id string
		err := s.db.QueryRow(s.ctx, `
			INSERT INTO payments (
				merchant_id, customer_id, razorpay_payment_id,
				amount, currency, method, status, created_at
			) VALUES ($1, $2, $3, $4, 'INR', 'upi', 'captured', $5)
			RETURNING id
		`,
			merchantID,
			customerIDs[custIdx],
			rzpID,
			amt,
			ago(time.Duration(30+i)*24*time.Hour), // spread over past 30-70 days
		).Scan(&id)
		if err != nil {
			log.Fatalf("seed captured payment %d: %v", i, err)
		}
		capturedUUIDs = append(capturedUUIDs, id)
	}

	// ── 10 failed payments (one per UPI error scenario) ───────────────────────
	for i, scenario := range failedPaymentScenarios {
		custIdx := (i + 5) % len(customerIDs) // use medium/new customers for failures
		rzpID := fmt.Sprintf("pay_hist_fail_%s_%s", scenario.errorCode, shortHex())

		var id string
		err := s.db.QueryRow(s.ctx, `
			INSERT INTO payments (
				merchant_id, customer_id, razorpay_payment_id,
				amount, currency, method, status, upi_error_code, failure_reason,
				created_at
			) VALUES ($1, $2, $3, $4, 'INR', 'upi', 'failed', $5, $6, $7)
			RETURNING id
		`,
			merchantID,
			customerIDs[custIdx],
			rzpID,
			scenario.amount,
			scenario.errorCode,
			scenario.description,
			ago(time.Duration(1+i)*6*time.Hour),
		).Scan(&id)
		if err != nil {
			log.Fatalf("seed failed payment %s: %v", scenario.errorCode, err)
		}
		failedByCode[scenario.errorCode] = id
	}

	log.Printf("  Payments: 40 captured + 10 failed = 50 total")
	return paymentSet{captured: capturedUUIDs, failed: failedByCode}
}

// ─────────────────────────────────────────────────────────────────────────────
// Demo Case A — Fully recovered (U30 transient → retry succeeded)
// ─────────────────────────────────────────────────────────────────────────────
//
//   Status:     recovered
//   Amount:     ₹4,999 (499900 paise)
//   Error:      U30 (Technical Decline — transient bank debit fail)
//   Customer:   High LTV (Aarav Sharma, 12 prior payments)
//   AI note:    ai_diagnosis._mock=false to simulate real AI ran this case
//   Timeline:   webhook → risk → 6x validator PASS → AI → policy → retry → captured

func (s *seeder) seedDemoCaseA(merchantID string, customerIDs []string, ps paymentSet) {
	const amount int64 = 499_900 // ₹4,999

	// Create a dedicated payment for this demo case
	rzpID := fmt.Sprintf("pay_demo_a_%s", shortHex())
	var paymentUUID string
	err := s.db.QueryRow(s.ctx, `
		INSERT INTO payments (
			merchant_id, customer_id, razorpay_payment_id,
			amount, currency, method, status, upi_error_code, failure_reason, created_at
		) VALUES ($1, $2, $3, $4, 'INR', 'upi', 'captured', 'U30', 'Debit timeout', $5)
		RETURNING id
	`, merchantID, customerIDs[0], rzpID, amount, ago(2*time.Hour)).Scan(&paymentUUID)
	if err != nil {
		log.Fatalf("seed demo case A payment: %v", err)
	}

	// Recovery case
	var caseID string
	aiDiagnosis := mustJSON(map[string]any{
		"recovery_probability": 0.82,
		"failure_category":     "TD",
		"failure_type":         "transient_bank_debit_fail",
		"timing_penalty":       false,
		"priority":             "high",
		"reasoning":            "U30 outside 7-10PM peak — high retry confidence",
		"_mock":                false, // simulates real Groq ran this
	})
	aiStrategy := mustJSON(map[string]any{
		"strategy":      "retry_payment",
		"confidence":    0.91,
		"delay_minutes": 10,
		"reasoning":     "TD failure with high LTV customer — retry in 10 min",
	})

	err = s.db.QueryRow(s.ctx, `
		INSERT INTO recovery_cases (
			merchant_id, payment_id, customer_id,
			status, revenue_at_risk, amount_recovered, recovery_probability,
			recovery_roi, priority, failure_type, upi_error_code, upi_error_category,
			bank_outage_detected, is_mandate_payment,
			ai_diagnosis, ai_strategy,
			retry_count, max_retries, partial_recovery,
			created_at, resolved_at, updated_at
		) VALUES (
			$1, $2, $3,
			'recovered', $4, $4, 0.82,
			40.99, 'high', 'transient_bank_debit_fail', 'U30', 'TD',
			FALSE, FALSE,
			$5, $6,
			1, 2, FALSE,
			$7, $8, $8
		)
		RETURNING id
	`,
		merchantID, paymentUUID, customerIDs[0],
		amount, aiDiagnosis, aiStrategy,
		ago(2*time.Hour), ago(8*time.Minute),
	).Scan(&caseID)
	if err != nil {
		log.Fatalf("seed demo case A recovery_case: %v", err)
	}

	// Recovery action (retry — succeeded)
	s.exec(`
		INSERT INTO recovery_actions (
			case_id, action_type, status,
			ai_confidence, policy_approved, policy_rule_triggered, policy_reason,
			payload, result,
			executed_by, executed_at, created_at
		) VALUES (
			$1, 'retry', 'succeeded',
			0.91, TRUE, 'none', 'All 10 policy rules passed',
			$2, $3,
			'execution_worker', $4, $5
		)
	`,
		caseID,
		mustJSON(map[string]any{"payment_id": rzpID, "scheduled_at_minutes": 10}),
		mustJSON(map[string]any{"razorpay_status": "captured", "amount_captured": amount}),
		ago(8*time.Minute),
		ago(18*time.Minute),
	)

	// Audit trail — all actors
	s.seedAuditTrail(caseID, []auditEntry{
		{actor: "system", action: "webhook_received",
			meta: map[string]any{"event": "payment.failed", "upi_error_code": "U30", "razorpay_event_id": "evt_demo_a_001"},
			at:   ago(2 * time.Hour)},
		{actor: "risk_engine", action: "risk_scored",
			meta: map[string]any{"risk_score": 1.68, "priority": "high", "bank_outage_detected": false},
			at:   ago(119 * time.Minute)},
		{actor: "validator", action: "check1_pass",
			meta: map[string]any{"check": "payment_not_captured", "razorpay_status": "failed"},
			at:   ago(118 * time.Minute)},
		{actor: "validator", action: "check2_pass",
			meta: map[string]any{"check": "no_bank_outage", "redis_key": "bank_outage:U30", "exists": false},
			at:   ago(118 * time.Minute)},
		{actor: "validator", action: "check3_pass",
			meta: map[string]any{"check": "rbi_compliance", "is_mandate_payment": false},
			at:   ago(118 * time.Minute)},
		{actor: "validator", action: "check4_pass",
			meta: map[string]any{"check": "positive_roi", "roi_paise": 40.99, "threshold": 0},
			at:   ago(118 * time.Minute)},
		{actor: "validator", action: "check5_pass",
			meta: map[string]any{"check": "error_retryability", "upi_error_code": "U30", "force_payment_link": false},
			at:   ago(117 * time.Minute)},
		{actor: "validator", action: "check6_pass",
			meta: map[string]any{"check": "max_retries", "retry_count": 0, "max_retries": 2},
			at:   ago(117 * time.Minute)},
		{actor: "ai_agent", action: "risk_analyst_complete",
			meta: map[string]any{"recovery_probability": 0.82, "failure_category": "TD", "timing_penalty": false},
			at:   ago(116 * time.Minute)},
		{actor: "ai_agent", action: "strategist_complete",
			meta: map[string]any{"strategy": "retry_payment", "confidence": 0.91, "delay_minutes": 10},
			at:   ago(116 * time.Minute)},
		{actor: "ai_agent", action: "executor_command_built",
			meta: map[string]any{"action": "RETRY_PAYMENT", "scheduled_at_minutes": 10},
			at:   ago(115 * time.Minute)},
		{actor: "policy_engine", action: "all_rules_passed",
			meta: map[string]any{"rules_evaluated": 10, "decision": "APPROVED", "rule_triggered": "none"},
			at:   ago(115 * time.Minute)},
		{actor: "execution_worker", action: "retry_executed",
			meta: map[string]any{"razorpay_api": "POST /v1/payments/pay_demo_a/capture"},
			at:   ago(18 * time.Minute)},
		{actor: "execution_worker", action: "payment_captured",
			meta: map[string]any{"amount_captured": amount, "razorpay_status": "captured"},
			at:   ago(8 * time.Minute)},
	})

	log.Printf("  Case A: recovered   ₹4,999 (U30 → retry succeeded)  [%s]", caseID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Demo Case B — Not worth recovering (Z9 + ₹99 + new customer → negative ROI)
// ─────────────────────────────────────────────────────────────────────────────
//
//   Status:     not_worth_recovering
//   Amount:     ₹99 (9900 paise)
//   Error:      Z9 (Business Decline — insufficient funds)
//   Customer:   New (Nisha Dubey, 0 prior payments)
//   Blocked at: Validator CHECK 4 (negative ROI)
//   AI note:    ai_diagnosis is NULL (validator blocked before AI)

func (s *seeder) seedDemoCaseB(merchantID string, customerIDs []string, ps paymentSet) {
	const amount int64 = 9_900 // ₹99

	rzpID := fmt.Sprintf("pay_demo_b_%s", shortHex())
	var paymentUUID string
	err := s.db.QueryRow(s.ctx, `
		INSERT INTO payments (
			merchant_id, customer_id, razorpay_payment_id,
			amount, currency, method, status, upi_error_code, failure_reason, created_at
		) VALUES ($1, $2, $3, $4, 'INR', 'upi', 'failed', 'Z9', 'Insufficient funds', $5)
		RETURNING id
	`, merchantID, customerIDs[19], rzpID, amount, ago(3*time.Hour)).Scan(&paymentUUID)
	if err != nil {
		log.Fatalf("seed demo case B payment: %v", err)
	}

	var caseID string
	err = s.db.QueryRow(s.ctx, `
		INSERT INTO recovery_cases (
			merchant_id, payment_id, customer_id,
			status, revenue_at_risk, amount_recovered, recovery_probability,
			recovery_roi, priority, failure_type, upi_error_code, upi_error_category,
			bank_outage_detected, is_mandate_payment,
			validator_skip_reason,
			retry_count, max_retries,
			created_at, resolved_at, updated_at
		) VALUES (
			$1, $2, $3,
			'not_worth_recovering', $4, 0, 0.15,
			-47.0, 'low', 'insufficient_funds', 'Z9', 'BD',
			FALSE, FALSE,
			'Recovery ROI ₹-47.00 below threshold — not cost effective',
			0, 2,
			$5, $6, $6
		)
		RETURNING id
	`,
		merchantID, paymentUUID, customerIDs[19],
		amount, ago(3*time.Hour), ago(178*time.Minute),
	).Scan(&caseID)
	if err != nil {
		log.Fatalf("seed demo case B recovery_case: %v", err)
	}

	// Audit trail — stops at CHECK 4
	s.seedAuditTrail(caseID, []auditEntry{
		{actor: "system", action: "webhook_received",
			meta: map[string]any{"event": "payment.failed", "upi_error_code": "Z9"},
			at:   ago(3 * time.Hour)},
		{actor: "risk_engine", action: "risk_scored",
			meta: map[string]any{"risk_score": 0.07, "priority": "low", "failure_type": "insufficient_funds"},
			at:   ago(179 * time.Minute)},
		{actor: "validator", action: "check1_pass",
			meta: map[string]any{"check": "payment_not_captured", "razorpay_status": "failed"},
			at:   ago(179 * time.Minute)},
		{actor: "validator", action: "check2_pass",
			meta: map[string]any{"check": "no_bank_outage"},
			at:   ago(179 * time.Minute)},
		{actor: "validator", action: "check3_pass",
			meta: map[string]any{"check": "rbi_compliance", "is_mandate_payment": false},
			at:   ago(178 * time.Minute)},
		{actor: "validator", action: "check4_blocked",
			meta: map[string]any{
				"check":              "recovery_roi",
				"roi_paise":          -47.0,
				"threshold":          0,
				"reason":             "Recovery ROI ₹-47.00 below threshold — not cost effective",
				"new_status":         "not_worth_recovering",
				"calculation":        "amount=9900 × probability=0.15 - escalation_cost=10000 = -8515 paise",
			},
			at: ago(178 * time.Minute)},
	})

	log.Printf("  Case B: not_worth_recovering  ₹99 (Z9 + new customer → negative ROI blocked)  [%s]", caseID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Demo Case C — Customer self-recovered (U16 → customer paid themselves)
// ─────────────────────────────────────────────────────────────────────────────
//
//   Status:     customer_self_recovered
//   Amount:     ₹2,499 (249900 paise)
//   Error:      U16 (insufficient balance — sent payment link)
//   Customer:   Medium LTV (Sneha Reddy)
//   Timeline:   recovery opened → customer paid → system detected → closed

func (s *seeder) seedDemoCaseC(merchantID string, customerIDs []string, ps paymentSet) {
	const amount int64 = 249_900 // ₹2,499

	rzpID := fmt.Sprintf("pay_demo_c_%s", shortHex())
	var paymentUUID string
	err := s.db.QueryRow(s.ctx, `
		INSERT INTO payments (
			merchant_id, customer_id, razorpay_payment_id,
			amount, currency, method, status, upi_error_code, failure_reason, created_at
		) VALUES ($1, $2, $3, $4, 'INR', 'upi', 'captured', 'U16', 'Insufficient balance', $5)
		RETURNING id
	`, merchantID, customerIDs[5], rzpID, amount, ago(5*time.Hour)).Scan(&paymentUUID)
	if err != nil {
		log.Fatalf("seed demo case C payment: %v", err)
	}

	aiStrategy := mustJSON(map[string]any{
		"strategy":      "generate_payment_link",
		"confidence":    0.75,
		"delay_minutes": 1440,
		"reasoning":     "U16 insufficient balance — 24h payment link preferred",
	})

	var caseID string
	err = s.db.QueryRow(s.ctx, `
		INSERT INTO recovery_cases (
			merchant_id, payment_id, customer_id,
			status, revenue_at_risk, amount_recovered, recovery_probability,
			recovery_roi, priority, failure_type, upi_error_code, upi_error_category,
			bank_outage_detected, is_mandate_payment,
			ai_strategy,
			retry_count, max_retries,
			created_at, resolved_at, updated_at
		) VALUES (
			$1, $2, $3,
			'customer_self_recovered', $4, $4, 0.75,
			18742.50, 'medium', 'insufficient_balance', 'U16', 'BD',
			FALSE, FALSE,
			$5,
			0, 2,
			$6, $7, $7
		)
		RETURNING id
	`,
		merchantID, paymentUUID, customerIDs[5],
		amount, aiStrategy,
		ago(5*time.Hour), ago(4*time.Hour),
	).Scan(&caseID)
	if err != nil {
		log.Fatalf("seed demo case C recovery_case: %v", err)
	}

	// Audit trail — recovery opened, AI ran, then customer paid themselves
	s.seedAuditTrail(caseID, []auditEntry{
		{actor: "system", action: "webhook_received",
			meta: map[string]any{"event": "payment.failed", "upi_error_code": "U16"},
			at:   ago(5 * time.Hour)},
		{actor: "risk_engine", action: "risk_scored",
			meta: map[string]any{"risk_score": 0.90, "priority": "medium"},
			at:   ago(299 * time.Minute)},
		{actor: "validator", action: "all_checks_passed",
			meta: map[string]any{"checks": 6, "force_payment_link": false},
			at:   ago(298 * time.Minute)},
		{actor: "ai_agent", action: "strategist_complete",
			meta: map[string]any{"strategy": "generate_payment_link", "delay_minutes": 1440},
			at:   ago(297 * time.Minute)},
		{actor: "customer_self", action: "self_recovered",
			meta: map[string]any{
				"razorpay_event_id": "evt_demo_c_capture",
				"captured_amount":   amount,
				"previous_status":   "open",
				"message":           "Customer retried payment manually via UPI app",
			},
			at: ago(4 * time.Hour)},
	})

	log.Printf("  Case C: customer_self_recovered  ₹2,499 (U16 → customer paid themselves)  [%s]", caseID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Demo Case D — Bank outage batched (U28 — 15 failures triggered outage mode)
// ─────────────────────────────────────────────────────────────────────────────
//
//   Status:     outage_batched
//   Amount:     ₹8,999 (899900 paise)
//   Error:      U28 (bank server down)
//   Customer:   High LTV (Vikram Nair)
//   Timeline:   15 failures detected → outage flagged → batch scheduled

func (s *seeder) seedDemoCaseD(merchantID string, customerIDs []string, ps paymentSet) {
	const amount int64 = 899_900 // ₹8,999

	batchRetryAt := time.Now().UTC().Add(45 * time.Minute) // retry in ~45 min

	rzpID := fmt.Sprintf("pay_demo_d_%s", shortHex())
	var paymentUUID string
	err := s.db.QueryRow(s.ctx, `
		INSERT INTO payments (
			merchant_id, customer_id, razorpay_payment_id,
			amount, currency, method, status, upi_error_code, failure_reason, created_at
		) VALUES ($1, $2, $3, $4, 'INR', 'upi', 'failed', 'U28', 'Bank server down', $5)
		RETURNING id
	`, merchantID, customerIDs[4], rzpID, amount, ago(30*time.Minute)).Scan(&paymentUUID)
	if err != nil {
		log.Fatalf("seed demo case D payment: %v", err)
	}

	var caseID string
	err = s.db.QueryRow(s.ctx, `
		INSERT INTO recovery_cases (
			merchant_id, payment_id, customer_id,
			status, revenue_at_risk, amount_recovered, recovery_probability,
			priority, failure_type, upi_error_code, upi_error_category,
			bank_outage_detected, is_mandate_payment,
			validator_skip_reason,
			cooldown_until,
			retry_count, max_retries,
			created_at, updated_at
		) VALUES (
			$1, $2, $3,
			'outage_batched', $4, 0, 0.0,
			'outage_batched', 'bank_server_down', 'U28', 'TD',
			TRUE, FALSE,
			'Bank outage detected for U28 — batched for retry at batch window',
			$5,
			0, 2,
			$6, $6
		)
		RETURNING id
	`,
		merchantID, paymentUUID, customerIDs[4],
		amount, batchRetryAt,
		ago(30*time.Minute),
	).Scan(&caseID)
	if err != nil {
		log.Fatalf("seed demo case D recovery_case: %v", err)
	}

	// Bank outage event (simulating 15 failures triggered this)
	s.exec(`
		INSERT INTO bank_outage_events (
			upi_error_code, merchant_id, detected_at,
			failure_count, window_minutes, affected_case_ids
		) VALUES ($1, $2, $3, $4, $5, $6)
	`,
		"U28", merchantID, ago(25*time.Minute),
		15, 5, []string{caseID},
	)

	// Audit trail — outage detection cascade
	s.seedAuditTrail(caseID, []auditEntry{
		{actor: "system", action: "webhook_received",
			meta: map[string]any{"event": "payment.failed", "upi_error_code": "U28"},
			at:   ago(30 * time.Minute)},
		{actor: "risk_engine", action: "bank_outage_detected",
			meta: map[string]any{
				"upi_error_code":  "U28",
				"failure_count":   15,
				"window_minutes":  5,
				"threshold":       10,
				"outage_key":      "bank_outage:U28",
				"outage_ttl_secs": 3600,
				"message":         "15 U28 failures in 5 minutes — bank outage detected, batching all cases",
			},
			at: ago(25 * time.Minute)},
		{actor: "risk_engine", action: "case_outage_batched",
			meta: map[string]any{
				"new_status":    "outage_batched",
				"cooldown_until": batchRetryAt.Format(time.RFC3339),
				"reason":        "Bank outage active — retry batched, do not execute now",
			},
			at: ago(25 * time.Minute)},
	})

	log.Printf("  Case D: outage_batched        ₹8,999 (U28 bank down → 15 failures → outage batch)  [%s]", caseID)
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

type auditEntry struct {
	actor  string
	action string
	meta   map[string]any
	at     time.Time
}

func (s *seeder) seedAuditTrail(caseID string, entries []auditEntry) {
	for _, e := range entries {
		s.exec(`
			INSERT INTO audit_logs (entity_type, entity_id, actor, action, metadata, created_at)
			VALUES ('recovery_case', $1, $2, $3, $4, $5)
		`, caseID, e.actor, e.action, mustJSON(e.meta), e.at)
	}
}

func (s *seeder) exec(query string, args ...any) {
	_, err := s.db.Exec(s.ctx, query, args...)
	if err != nil {
		log.Fatalf("exec error\nquery: %s\nerror: %v", query, err)
	}
}

// shortHex returns a short random hex string for readable demo IDs.
func shortHex() string {
	return fmt.Sprintf("%08x", rand.Int31())
}
