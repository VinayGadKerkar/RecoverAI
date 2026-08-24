//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisclient "github.com/redis/go-redis/v9"

	"recoverai/internal/policy"
)

// ─── Test infrastructure ──────────────────────────────────────────────────────

// env returns the environment variable or its default.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// testEnv holds all shared infrastructure connections for the test suite.
type testEnv struct {
	db            *pgxpool.Pool
	redis         *redisclient.Client
	apiURL        string           // base URL of the running API server
	mockAI        *httptest.Server // in-process mock AI server
	webhookSecret string

	// Seeded IDs — reset per test
	merchantID string
	customerID string
}

// setup connects to live infrastructure and starts an in-process mock AI server.
func setup(t *testing.T) *testEnv {
	t.Helper()

	dbURL := env("DATABASE_URL", "postgres://recoverai:secret@localhost:5432/recoverai?sslmode=disable")
	redisURL := env("REDIS_URL", "redis://localhost:6379")
	apiURL := env("API_URL", "http://localhost:8080")
	webhookSecret := env("RAZORPAY_WEBHOOK_SECRET", "test-webhook-secret")

	// ─── PostgreSQL connection ────────────────────────────────────────────────
	dbPool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	if err := dbPool.Ping(context.Background()); err != nil {
		t.Fatalf("ping postgres: %v (is docker-compose running?)", err)
	}

	// ─── Redis connection ─────────────────────────────────────────────────────
	opt, err := redisclient.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redisclient.NewClient(opt)
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v (is docker-compose running?)", err)
	}

	// ─── In-process Mock AI server ────────────────────────────────────────────
	// Uses a random free port to avoid conflicts with docker mock-ai on 8001.
	mockAI := httptest.NewServer(mockAIHandler())

	// Point the running worker at our test mock AI.
	// NOTE: the worker reads AI_SERVICE_URL at startup; for integration tests
	// the worker must already be running with USE_MOCK_AI=true and MOCK_AI_URL
	// pointing at this test server. We document the requirement in the header.
	t.Setenv("USE_MOCK_AI", "true")
	t.Setenv("MOCK_AI_URL", mockAI.URL)

	te := &testEnv{
		db:            dbPool,
		redis:         rdb,
		apiURL:        apiURL,
		mockAI:        mockAI,
		webhookSecret: webhookSecret,
	}

	// Seed test merchant and customer
	te.seedMerchant(t)
	te.seedCustomer(t, 0)

	t.Cleanup(func() {
		te.cleanup(t)
		mockAI.Close()
		rdb.Close()
		dbPool.Close()
	})

	return te
}

// mockAIHandler returns an http.Handler that serves the mock AI /analyze endpoint.
// Returns deterministic responses based on upi_error_code (mirrors cmd/mock-ai/main.go).
func mockAIHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"mock": true, "status": "ok"})
	})

	mux.HandleFunc("/analyze", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PaymentID    string `json:"payment_id"`
			CaseID       string `json:"case_id"`
			UPIErrorCode string `json:"upi_error_code"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)

		action, strategy, confidence, delayMin := mockDecision(req.UPIErrorCode)

		resp := map[string]interface{}{
			"action":               action,
			"payment_id":           req.PaymentID,
			"case_id":              req.CaseID,
			"scheduled_at_minutes": delayMin,
			"parameters":           map[string]string{},
			"risk_assessment_summary": map[string]interface{}{
				"recovery_probability": confidence,
				"reasoning":            "mock integration test",
			},
			"strategy_summary": map[string]interface{}{
				"strategy":      strategy,
				"confidence":    confidence,
				"delay_minutes": delayMin,
			},
			"_mock": true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	return mux
}

func mockDecision(code string) (action, strategy string, confidence float64, delayMin int) {
	switch code {
	case "U30", "RB", "BT":
		return "RETRY_PAYMENT", "retry_payment", 0.91, 10
	case "U28":
		return "RETRY_PAYMENT", "schedule_retry", 0.85, 60
	case "U16":
		return "GENERATE_PAYMENT_LINK", "generate_payment_link", 0.75, 1440
	case "Z9", "Z8":
		return "GENERATE_PAYMENT_LINK", "generate_payment_link", 0.70, 1440
	case "YG":
		return "ESCALATE", "escalate_to_merchant", 0.95, 0
	default:
		return "GENERATE_PAYMENT_LINK", "generate_payment_link", 0.60, 30
	}
}

// ─── Seeding helpers ──────────────────────────────────────────────────────────

func (te *testEnv) seedMerchant(t *testing.T) {
	t.Helper()
	err := te.db.QueryRow(context.Background(), `
		INSERT INTO merchants (razorpay_key_id, name, webhook_secret, settings)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`,
		fmt.Sprintf("rzp_test_%s", randHex(8)),
		"Integration Test Merchant",
		te.webhookSecret,
		`{"max_retries":3,"retry_cooldown_minutes":10,"max_retry_amount_paise":5000000,"require_human_above_paise":50000000,"allowed_actions":["retry","payment_link","notify","escalate"]}`,
	).Scan(&te.merchantID)
	if err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
}

func (te *testEnv) seedCustomer(t *testing.T, successfulPayments int) {
	t.Helper()
	err := te.db.QueryRow(context.Background(), `
		INSERT INTO customers (merchant_id, email, phone, successful_payments, failed_payments, lifetime_value)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`,
		te.merchantID,
		"test@integration.com",
		"+919999999999",
		successfulPayments,
		0,
		int64(successfulPayments)*50000, // ₹500 per payment
	).Scan(&te.customerID)
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
}

// seedPayment inserts a payments row and returns its UUID and the razorpay_payment_id.
func (te *testEnv) seedPayment(t *testing.T, razorpayID string, amount int64, status, upiErrorCode string) string {
	t.Helper()
	var paymentUUID string
	err := te.db.QueryRow(context.Background(), `
		INSERT INTO payments (merchant_id, customer_id, razorpay_payment_id, amount, currency, method, status, upi_error_code)
		VALUES ($1, $2, $3, $4, 'INR', 'upi', $5, $6)
		RETURNING id
	`,
		te.merchantID,
		te.customerID,
		razorpayID,
		amount,
		status,
		upiErrorCode,
	).Scan(&paymentUUID)
	if err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	return paymentUUID
}

func (te *testEnv) cleanup(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	// Clean up in reverse dependency order
	if te.merchantID != "" {
		te.db.Exec(ctx, `DELETE FROM audit_logs WHERE entity_id IN (SELECT id FROM recovery_cases WHERE merchant_id = $1)`, te.merchantID)
		te.db.Exec(ctx, `DELETE FROM recovery_actions WHERE case_id IN (SELECT id FROM recovery_cases WHERE merchant_id = $1)`, te.merchantID)
		te.db.Exec(ctx, `DELETE FROM recovery_cases WHERE merchant_id = $1`, te.merchantID)
		te.db.Exec(ctx, `DELETE FROM payments WHERE merchant_id = $1`, te.merchantID)
		te.db.Exec(ctx, `DELETE FROM customers WHERE merchant_id = $1`, te.merchantID)
		te.db.Exec(ctx, `DELETE FROM webhook_events WHERE event_type IN ('payment.failed', 'payment.captured') AND created_at > NOW() - INTERVAL '1 hour'`)
		te.db.Exec(ctx, `DELETE FROM merchants WHERE id = $1`, te.merchantID)

		// Clean Redis test keys
		te.redis.Del(ctx,
			fmt.Sprintf("bank_outage:U28"),
			fmt.Sprintf("bank_outage:U30"),
		)
	}
}

// ─── Webhook helpers ──────────────────────────────────────────────────────────

// postWebhook sends a Razorpay-style webhook to the API server.
// It computes the real HMAC signature from the provided secret.
func (te *testEnv) postWebhook(t *testing.T, eventID, eventType, paymentID string, amount int64, status, upiErrorCode string) int {
	t.Helper()

	var errCodeField string
	if upiErrorCode != "" {
		errCodeField = fmt.Sprintf(`,"error_code":%q`, upiErrorCode)
	}

	body := fmt.Sprintf(`{
		"entity":"event",
		"account_id":"acc_test",
		"event":%q,
		"contains":["payment"],
		"payload":{
			"payment":{
				"entity":{
					"id":%q,
					"amount":%d,
					"currency":"INR",
					"status":%q,
					"method":"upi",
					"vpa":"customer@upi",
					"bank":"HDFC",
					"email":"test@integration.com",
					"contact":"+919999999999"
					%s
				}
			}
		},
		"created_at":%d
	}`, eventType, paymentID, amount, status, errCodeField, time.Now().Unix())

	mac := hmac.New(sha256.New, []byte(te.webhookSecret))
	mac.Write([]byte(body))
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest("POST", te.apiURL+"/webhooks/razorpay", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Razorpay-Signature", sig)
	req.Header.Set("X-Razorpay-Event-Id", eventID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST webhook: %v (is API server running?)", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// ─── Poll helpers ─────────────────────────────────────────────────────────────

// pollUntil retries the condition function until it returns true or the deadline passes.
func pollUntil(t *testing.T, timeout, tick time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(tick)
	}
	return false
}

// waitForRecoveryCase polls until a recovery_case row exists for the given razorpay_payment_id.
// Returns the case UUID or fails the test.
func (te *testEnv) waitForRecoveryCase(t *testing.T, razorpayPaymentID string, timeout time.Duration) string {
	t.Helper()
	var caseID string
	ok := pollUntil(t, timeout, 300*time.Millisecond, func() bool {
		err := te.db.QueryRow(context.Background(), `
			SELECT rc.id
			FROM recovery_cases rc
			JOIN payments p ON p.id = rc.payment_id
			WHERE p.razorpay_payment_id = $1
			LIMIT 1
		`, razorpayPaymentID).Scan(&caseID)
		return err == nil && caseID != ""
	})
	if !ok {
		t.Fatalf("timed out waiting for recovery_case to be created for payment %s", razorpayPaymentID)
	}
	return caseID
}

// waitForCaseStatus polls until recovery_case.status matches wantStatus.
func (te *testEnv) waitForCaseStatus(t *testing.T, caseID, wantStatus string, timeout time.Duration) {
	t.Helper()
	var gotStatus string
	ok := pollUntil(t, timeout, 300*time.Millisecond, func() bool {
		te.db.QueryRow(context.Background(),
			`SELECT status FROM recovery_cases WHERE id = $1`, caseID,
		).Scan(&gotStatus)
		return gotStatus == wantStatus
	})
	if !ok {
		t.Errorf("timed out: recovery_case status=%q want %q", gotStatus, wantStatus)
	}
}

// ─── Utility ──────────────────────────────────────────────────────────────────

func randHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = "0123456789abcdef"[time.Now().UnixNano()%(16+int64(i)%7)]
	}
	return string(b)
}

func freePort() int {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST 1: Full pipeline for transient (TD) failure
// ─────────────────────────────────────────────────────────────────────────────

// TestFullPipeline_TransientFailure sends a U30 payment.failed webhook and
// asserts the full five-stage pipeline produces the correct DB state.
//
// Prerequisites: docker-compose up (postgres, redis, kafka, api, worker)
// Set USE_MOCK_AI=true, MOCK_AI_URL=<mock server> on the running worker.
func TestFullPipeline_TransientFailure(t *testing.T) {
	te := setup(t)

	const (
		paymentID = "pay_integ_u30_001"
		amount    = 499900 // ₹4,999 in paise
		eventID   = "evt_integ_u30_001"
	)

	// Step 1: Post payment.failed webhook
	status := te.postWebhook(t, eventID, "payment.failed", paymentID, amount, "failed", "U30")
	if status != http.StatusOK {
		t.Fatalf("webhook returned status %d, want 200", status)
	}

	// Step 2: Wait for recovery_case to appear (Stage 2: Risk Engine)
	caseID := te.waitForRecoveryCase(t, paymentID, 10*time.Second)
	t.Logf("recovery_case created: %s", caseID)

	// Step 3: Wait for status to advance to in_progress (Stage 4/5)
	te.waitForCaseStatus(t, caseID, "in_progress", 10*time.Second)

	ctx := context.Background()

	// ─── Assert: ai_diagnosis is populated with mock flag ────────────────────
	var aiDiagnosisRaw []byte
	te.db.QueryRow(ctx, `SELECT ai_diagnosis FROM recovery_cases WHERE id = $1`, caseID).Scan(&aiDiagnosisRaw)
	if len(aiDiagnosisRaw) == 0 {
		t.Error("ai_diagnosis is null — AI was not called")
	} else {
		var diag map[string]interface{}
		json.Unmarshal(aiDiagnosisRaw, &diag)
		if _, isMock := diag["_mock"]; !isMock {
			t.Error("ai_diagnosis does not have _mock field — real AI may have been called")
		}
	}

	// ─── Assert: recovery_actions has one row with action_type=retry ─────────
	var actionCount int
	var actionType string
	te.db.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MIN(action_type), '')
		FROM recovery_actions
		WHERE case_id = $1
	`, caseID).Scan(&actionCount, &actionType)

	if actionCount == 0 {
		t.Error("no recovery_actions created")
	} else if !strings.EqualFold(actionType, "retry") && !strings.EqualFold(actionType, "retry_payment") {
		t.Errorf("recovery_actions[0].action_type=%q want retry/retry_payment", actionType)
	}

	// ─── Assert: audit_logs has entries from all expected actors ─────────────
	rows, err := te.db.Query(ctx, `
		SELECT DISTINCT actor
		FROM audit_logs
		WHERE entity_id = $1
		ORDER BY actor
	`, caseID)
	if err != nil {
		t.Fatalf("query audit_logs: %v", err)
	}
	defer rows.Close()

	actors := map[string]bool{}
	for rows.Next() {
		var actor string
		rows.Scan(&actor)
		actors[actor] = true
	}

	requiredActors := []string{"risk_engine", "validator", "ai_agent", "policy_engine"}
	for _, a := range requiredActors {
		if !actors[a] {
			t.Errorf("audit_logs missing actor %q (got: %v)", a, actors)
		}
	}

	t.Logf("✅ TransientFailure pipeline: caseID=%s actors=%v", caseID, actors)
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST 2: Customer self-recovery detection
// ─────────────────────────────────────────────────────────────────────────────

func TestFullPipeline_SelfRecovery(t *testing.T) {
	te := setup(t)

	const (
		paymentID      = "pay_integ_self_001"
		amount         = 249900 // ₹2,499 in paise
		eventIDFailed  = "evt_integ_self_fail_001"
		eventIDCapture = "evt_integ_self_cap_001"
	)

	// Step 1: payment.failed — creates recovery_case
	status := te.postWebhook(t, eventIDFailed, "payment.failed", paymentID, amount, "failed", "U16")
	if status != http.StatusOK {
		t.Fatalf("payment.failed webhook status=%d", status)
	}

	// Step 2: Wait for recovery_case to be created with status=open
	caseID := te.waitForRecoveryCase(t, paymentID, 10*time.Second)
	t.Logf("recovery_case created: %s", caseID)

	// Step 3: payment.captured — same payment_id (customer paid manually)
	status = te.postWebhook(t, eventIDCapture, "payment.captured", paymentID, amount, "captured", "")
	if status != http.StatusOK {
		t.Fatalf("payment.captured webhook status=%d", status)
	}

	// Step 4: Wait for case status to become customer_self_recovered
	te.waitForCaseStatus(t, caseID, "customer_self_recovered", 5*time.Second)

	ctx := context.Background()

	// ─── Assert: amount_recovered = the captured amount ───────────────────────
	var amountRecovered int64
	te.db.QueryRow(ctx, `SELECT amount_recovered FROM recovery_cases WHERE id = $1`, caseID).Scan(&amountRecovered)
	if amountRecovered != amount {
		t.Errorf("amount_recovered=%d want %d", amountRecovered, amount)
	}

	// ─── Assert: audit_log has entry with actor=customer_self ─────────────────
	var selfActorCount int
	te.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE entity_id = $1 AND actor = 'customer_self'
	`, caseID).Scan(&selfActorCount)
	if selfActorCount == 0 {
		t.Error("audit_logs missing entry with actor=customer_self")
	}

	t.Logf("✅ SelfRecovery: caseID=%s amount_recovered=%d", caseID, amountRecovered)
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST 3: Bank outage detection via Redis counter
// ─────────────────────────────────────────────────────────────────────────────

func TestFullPipeline_OutageDetection(t *testing.T) {
	te := setup(t)

	const (
		errorCode   = "U28"
		numPayments = 12
		amount      = 100000 // ₹1,000 in paise
	)

	// Clear any existing outage flag from previous runs
	te.redis.Del(context.Background(), fmt.Sprintf("bank_outage:%s", errorCode))

	paymentIDs := make([]string, numPayments)
	eventIDs := make([]string, numPayments)
	for i := range paymentIDs {
		paymentIDs[i] = fmt.Sprintf("pay_integ_outage_%03d", i)
		eventIDs[i] = fmt.Sprintf("evt_integ_outage_%03d", i)
	}

	// Step 1: Send 12 payment.failed webhooks for U28 within 3 seconds
	for i := 0; i < numPayments; i++ {
		status := te.postWebhook(t, eventIDs[i], "payment.failed", paymentIDs[i], amount, "failed", errorCode)
		if status != http.StatusOK {
			t.Fatalf("webhook[%d] status=%d", i, status)
		}
	}

	// Step 2: Wait 2 seconds for outage detection to trigger via Risk Engine
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	// ─── Assert: Redis outage key exists ─────────────────────────────────────
	outageKey := fmt.Sprintf("bank_outage:%s", errorCode)
	outageExists, err := te.redis.Exists(ctx, outageKey).Result()
	if err != nil {
		t.Fatalf("redis exists: %v", err)
	}
	if outageExists == 0 {
		t.Errorf("Redis key %q not set — outage not detected", outageKey)
	}

	// ─── Assert: bank_outage_events has a row with failure_count >= 10 ────────
	var failureCount int
	te.db.QueryRow(ctx, `
		SELECT failure_count
		FROM bank_outage_events
		WHERE upi_error_code = $1
		ORDER BY detected_at DESC
		LIMIT 1
	`, errorCode).Scan(&failureCount)
	if failureCount < 10 {
		t.Errorf("bank_outage_events failure_count=%d, want >= 10", failureCount)
	}

	// ─── Assert: recovery_cases have bank_outage_detected=true and status=outage_batched
	// Wait a bit longer for all 12 cases to be processed
	var batchedCount int
	ok := pollUntil(t, 8*time.Second, 500*time.Millisecond, func() bool {
		te.db.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM recovery_cases rc
			JOIN payments p ON p.id = rc.payment_id
			WHERE p.razorpay_payment_id = ANY($1)
			  AND bank_outage_detected = TRUE
			  AND status = 'outage_batched'
		`, paymentIDs).Scan(&batchedCount)
		return batchedCount >= 10 // threshold crosses at 10, some may arrive before outage detected
	})
	if !ok {
		t.Errorf("only %d/%d cases have bank_outage_detected=true + status=outage_batched", batchedCount, numPayments)
	}

	// ─── Assert: mock AI was NOT called for outage-batched cases ─────────────
	var aiCalledCount int
	te.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		WHERE p.razorpay_payment_id = ANY($1)
		  AND ai_diagnosis IS NOT NULL
	`, paymentIDs).Scan(&aiCalledCount)
	if aiCalledCount > 0 {
		t.Errorf("AI was called for %d outage-batched cases (expected 0)", aiCalledCount)
	}

	t.Logf("✅ OutageDetection: %d cases batched, failure_count=%d", batchedCount, failureCount)
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST 4: Idempotent webhook delivery
// ─────────────────────────────────────────────────────────────────────────────

func TestFullPipeline_IdempotentWebhook(t *testing.T) {
	te := setup(t)

	const (
		paymentID = "pay_integ_idem_001"
		amount    = 299900                // ₹2,999 in paise
		eventID   = "test-event-idem-001" // fixed event ID for dedup test
	)

	// Clear any leftover idempotency key from previous test runs
	te.redis.Del(context.Background(), "webhook:idempotency:"+eventID)

	// Step 1: Send the webhook twice with the SAME event ID
	s1 := te.postWebhook(t, eventID, "payment.failed", paymentID, amount, "failed", "U30")
	s2 := te.postWebhook(t, eventID, "payment.failed", paymentID, amount, "failed", "U30")

	if s1 != http.StatusOK {
		t.Errorf("first webhook status=%d want 200", s1)
	}
	if s2 != http.StatusOK {
		t.Errorf("second webhook status=%d want 200", s2)
	}

	// Step 2: Wait for processing
	time.Sleep(3 * time.Second)

	ctx := context.Background()

	// ─── Assert: only ONE recovery_case created ───────────────────────────────
	var caseCount int
	te.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		WHERE p.razorpay_payment_id = $1
	`, paymentID).Scan(&caseCount)
	if caseCount != 1 {
		t.Errorf("recovery_cases count=%d want 1 (idempotency failure)", caseCount)
	}

	// ─── Assert: webhook_events has only ONE row for this event_id ───────────
	var webhookCount int
	te.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM webhook_events
		WHERE razorpay_event_id = $1
	`, eventID).Scan(&webhookCount)
	if webhookCount != 1 {
		t.Errorf("webhook_events count=%d want 1 for event_id=%q", webhookCount, eventID)
	}

	t.Logf("✅ IdempotentWebhook: 2 webhooks → 1 recovery_case, 1 webhook_event")
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST 5: Negative ROI — validator blocks before AI
// ─────────────────────────────────────────────────────────────────────────────

func TestFullPipeline_NegativeROI(t *testing.T) {
	te := setup(t)

	// Re-seed customer with 0 prior payments (new customer = low LTV)
	te.db.Exec(context.Background(), `DELETE FROM customers WHERE id = $1`, te.customerID)
	te.seedCustomer(t, 0) // overwrite with fresh 0-payment customer

	const (
		paymentID = "pay_integ_roi_001"
		amount    = 9900 // ₹99 in paise — very low amount
		eventID   = "evt_integ_roi_001"
		errorCode = "Z9" // BD non-retryable → very low recovery probability
	)

	// Step 1: Post webhook for a tiny Z9 payment from a new customer
	status := te.postWebhook(t, eventID, "payment.failed", paymentID, amount, "failed", errorCode)
	if status != http.StatusOK {
		t.Fatalf("webhook status=%d", status)
	}

	// Step 2: Wait for recovery_case to be created
	caseID := te.waitForRecoveryCase(t, paymentID, 10*time.Second)
	t.Logf("recovery_case created: %s", caseID)

	// Step 3: Wait for validator to set status to not_worth_recovering
	te.waitForCaseStatus(t, caseID, "not_worth_recovering", 10*time.Second)

	ctx := context.Background()

	// ─── Assert: validator_skip_reason mentions ROI ───────────────────────────
	var skipReason string
	te.db.QueryRow(ctx, `SELECT COALESCE(validator_skip_reason,'') FROM recovery_cases WHERE id = $1`, caseID).Scan(&skipReason)
	if !strings.Contains(strings.ToLower(skipReason), "roi") {
		t.Errorf("validator_skip_reason=%q does not contain 'ROI'", skipReason)
	}

	// ─── Assert: AI was NOT called ────────────────────────────────────────────
	var aiDiag []byte
	te.db.QueryRow(ctx, `SELECT ai_diagnosis FROM recovery_cases WHERE id = $1`, caseID).Scan(&aiDiag)
	if len(aiDiag) > 0 {
		t.Error("ai_diagnosis is set — AI was called even though validator should have blocked")
	}

	t.Logf("✅ NegativeROI: caseID=%s skip_reason=%q", caseID, skipReason)
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST 6: Policy engine directly blocks YG + RETRY_PAYMENT (Rule 1)
// ─────────────────────────────────────────────────────────────────────────────

// TestPolicyEngine_BlocksNonRetryable tests the policy engine in-process
// against a manually created recovery_case with YG error code.
// This is a direct unit test of the policy engine (no Kafka/HTTP needed).
func TestPolicyEngine_BlocksNonRetryable(t *testing.T) {
	te := setup(t)

	ctx := context.Background()

	// Manually create a recovery_case with upi_error_code=YG
	var caseID string
	err := te.db.QueryRow(ctx, `
		INSERT INTO payments (merchant_id, customer_id, razorpay_payment_id, amount, currency, method, status, upi_error_code)
		VALUES ($1, $2, $3, 50000, 'INR', 'upi', 'failed', 'YG')
		RETURNING id
	`, te.merchantID, te.customerID, fmt.Sprintf("pay_integ_yg_%s", randHex(4))).Scan(new(string))
	_ = err // will be overwritten below

	// Get the payment UUID we just created
	var paymentUUID string
	te.db.QueryRow(ctx, `
		SELECT id FROM payments WHERE merchant_id = $1 AND upi_error_code = 'YG' ORDER BY created_at DESC LIMIT 1
	`, te.merchantID).Scan(&paymentUUID)

	// Create recovery case manually
	te.db.QueryRow(ctx, `
		INSERT INTO recovery_cases (merchant_id, payment_id, customer_id, status, revenue_at_risk, priority, upi_error_code, upi_error_category, retry_count, max_retries)
		VALUES ($1, $2, $3, 'open', 50000, 'critical', 'YG', 'BD', 0, 3)
		RETURNING id
	`, te.merchantID, paymentUUID, te.customerID).Scan(&caseID)

	if caseID == "" {
		t.Fatal("failed to create test recovery_case")
	}
	t.Logf("created test recovery_case: %s (YG error)", caseID)

	// Call Policy Engine directly — no Kafka, no AI, pure in-process
	engine := policy.NewEngine()
	decision := engine.Evaluate(policy.PolicyInput{
		Action:           "RETRY_PAYMENT",
		PaymentID:        "pay_integ_yg_direct",
		CaseID:           caseID,
		Amount:           50000,
		RetryCount:       0,
		UPIErrorCode:     "YG",
		UPIErrorCategory: "BD",
		MerchantPolicy: policy.MerchantPolicy{
			AllowedActions:          []string{"retry", "payment_link", "escalate"},
			MaxRetryAmountPaise:     5000000,
			RequireHumanAbovePaise:  50000000,
			HighValueThresholdPaise: 1500000,
			MaxRetries:              3,
		},
	})

	// ─── Assert: Allowed = false ──────────────────────────────────────────────
	if decision.Allowed {
		t.Errorf("policy.Allowed=true for YG+RETRY, want false")
	}

	// ─── Assert: RuleTriggered = rule1_non_retryable_upi ─────────────────────
	if decision.RuleTriggered != "rule1_non_retryable_upi" {
		t.Errorf("RuleTriggered=%q want %q", decision.RuleTriggered, "rule1_non_retryable_upi")
	}

	t.Logf("✅ PolicyEngine_BlocksNonRetryable: allowed=%v rule=%q reason=%q",
		decision.Allowed, decision.RuleTriggered, decision.Reason)
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST MAIN: Skip all if infrastructure is unavailable
// ─────────────────────────────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	// Quick check: if the API server isn't reachable, skip all integration tests.
	apiURL := env("API_URL", "http://localhost:8080")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(apiURL + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Printf("SKIP: API server not reachable at %s (run docker-compose up first)\n", apiURL)
		os.Exit(0)
	}

	// Also check DB
	dbURL := env("DATABASE_URL", "postgres://recoverai:secret@localhost:5432/recoverai?sslmode=disable")
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil || pool.Ping(context.Background()) != nil {
		fmt.Printf("SKIP: PostgreSQL not reachable at %s\n", dbURL)
		os.Exit(0)
	}
	pool.Close()

	os.Exit(m.Run())
}

// ─── Inline JSON body builder ─────────────────────────────────────────────────

func jsonBody(v interface{}) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}
