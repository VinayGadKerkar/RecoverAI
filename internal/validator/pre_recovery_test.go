package validator

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// contains is a helper for substring matching in reason strings.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// baseCase returns a RecoveryCaseInput that passes all pure-logic checks when
// used with the unexported check methods directly.
func baseCase() *RecoveryCaseInput {
	return &RecoveryCaseInput{
		Amount:              100_00, // ₹100 in paise
		RecoveryProbability: 0.80,
		RetryCount:          0,
		MaxRetries:          3,
		UPIErrorCode:        "U30",
		IsMandatePayment:    false,
		// CreatedAt 25 hours ago → 24h mandate window already elapsed
		CreatedAt: time.Now().Add(-25 * time.Hour),
	}
}

// ─── Check 1: Payment already captured ───────────────────────────────────────
// check1AlreadyCaptured makes an HTTP call to Razorpay, so we test only the
// pure decision that flows from it: the Validator's skip/proceed return values.
// We test this by exercising check2–check6 in isolation instead, and validate
// the check1 branching logic with a table of expected (skip, reason) pairs
// that mirror what the real HTTP response would produce.

// check1Logic mirrors the branch logic inside check1AlreadyCaptured so we can
// test it without an HTTP client.
func check1Logic(paymentStatus string, err error) (skip bool, reason string) {
	if err != nil {
		return false, "" // fail open
	}
	if paymentStatus == "captured" {
		return true, "Payment already captured — customer self-recovered"
	}
	return false, ""
}

func TestCheck1_AlreadyCaptured_Logic(t *testing.T) {
	tests := []struct {
		name          string
		paymentStatus string
		apiErr        error
		wantSkip      bool
		wantReason    string
	}{
		// BLOCK
		{"captured → skip", "captured", nil, true, "Payment already captured — customer self-recovered"},

		// PASS
		{"failed → proceed", "failed", nil, false, ""},
		{"authorized → proceed (not yet captured)", "authorized", nil, false, ""},

		// ERROR HANDLING: fail open
		{"razorpay API error → proceed (fail open)", "", fmt.Errorf("network error"), false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skip, reason := check1Logic(tt.paymentStatus, tt.apiErr)
			if skip != tt.wantSkip {
				t.Errorf("skip=%v want %v", skip, tt.wantSkip)
			}
			if reason != tt.wantReason {
				t.Errorf("reason=%q want %q", reason, tt.wantReason)
			}
		})
	}
}

// ─── Check 2: Bank outage ─────────────────────────────────────────────────────
// Same pattern: mirror the branch logic to avoid Redis dependency.

func check2Logic(exists bool, err error, errorCode string) (skip bool, reason string) {
	if errorCode == "" || err != nil {
		return false, ""
	}
	if exists {
		retryAt := time.Now().Add(60 * time.Minute)
		return true, fmt.Sprintf("Bank outage detected for %s — batched for retry at %s",
			errorCode, retryAt.Format(time.RFC3339))
	}
	return false, ""
}

func TestCheck2_BankOutage_Logic(t *testing.T) {
	tests := []struct {
		name      string
		exists    bool
		redisErr  error
		errorCode string
		wantSkip  bool
	}{
		// BLOCK
		{"outage key exists → skip", true, nil, "U30", true},

		// PASS
		{"no outage key → proceed", false, nil, "U30", false},
		{"empty error code → proceed", false, nil, "", false},

		// ERROR HANDLING: fail open
		{"redis error → proceed (fail open)", false, fmt.Errorf("conn refused"), "U30", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skip, _ := check2Logic(tt.exists, tt.redisErr, tt.errorCode)
			if skip != tt.wantSkip {
				t.Errorf("skip=%v want %v", skip, tt.wantSkip)
			}
		})
	}
}

// ─── Check 3: RBI mandate compliance ─────────────────────────────────────────
// check3RBIMandate is pure logic (no external deps beyond what's in the struct).

func TestCheck3_RBIMandate(t *testing.T) {
	v := &Validator{}

	now := time.Now()

	tests := []struct {
		name         string
		isMandate    bool
		amount       int64
		createdAt    time.Time
		wantSkip     bool
		wantContains string // substring the reason must contain
	}{
		// BLOCK: mandate + 24h window NOT elapsed
		{
			name:         "mandate created 1h ago → 24h not elapsed → blocked",
			isMandate:    true,
			amount:       100_00,
			createdAt:    now.Add(-1 * time.Hour),
			wantSkip:     true,
			wantContains: "RBI mandate rules: minimum 24h between retries",
		},
		// BLOCK: mandate + amount > ₹15,000 in paise (even after 24h)
		// Note: engine uses c.Amount > 1500000 (strict >)
		{
			name:         "mandate+₹16K → high value blocked",
			isMandate:    true,
			amount:       16_000_00, // ₹16,000 = 1600000 paise, strictly > 1500000
			createdAt:    now.Add(-25 * time.Hour),
			wantSkip:     true,
			wantContains: "RBI: amounts >₹15,000 require explicit customer approval",
		},
		// BOUNDARY: mandate + exactly ₹15,000 (=1500000, NOT > 1500000) → passes
		{
			name:         "mandate+exactly ₹15K passes (strict >)",
			isMandate:    true,
			amount:       15_000_00, // 1500000 paise — NOT strictly >
			createdAt:    now.Add(-25 * time.Hour),
			wantSkip:     false,
			wantContains: "",
		},
		// PASS: mandate + 24h elapsed + amount just below ₹15K
		{
			name:         "mandate+25h+₹1K → all RBI rules pass",
			isMandate:    true,
			amount:       1_000_00,
			createdAt:    now.Add(-25 * time.Hour),
			wantSkip:     false,
			wantContains: "",
		},
		// PASS: not a mandate (entire check short-circuits)
		{
			name:         "non-mandate → check skipped entirely",
			isMandate:    false,
			amount:       20_000_00,
			createdAt:    now, // would block if mandate
			wantSkip:     false,
			wantContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseCase()
			c.IsMandatePayment = tt.isMandate
			c.Amount = tt.amount
			c.CreatedAt = tt.createdAt

			skip, reason := v.check3RBIMandate(nil, c)

			if skip != tt.wantSkip {
				t.Errorf("skip=%v want %v (reason: %q)", skip, tt.wantSkip, reason)
			}

			if tt.wantContains != "" && !contains(reason, tt.wantContains) {
				t.Errorf("reason=%q does not contain %q", reason, tt.wantContains)
			}
		})
	}
}

// ─── Check 4: Recovery ROI ────────────────────────────────────────────────────
// check4ROI hits the DB for merchant policy. We test the pure ROI formula
// by extracting the calculation logic.

// roiCalculation mirrors the pure arithmetic in check4ROI so it's testable
// without a DB connection.
func roiCalculation(amount int64, probability float64, estimatedCost int64) float64 {
	return float64(amount)*probability - float64(estimatedCost)
}

func TestCheck4_ROI_Formula(t *testing.T) {
	tests := []struct {
		name        string
		amount      int64
		probability float64
		cost        int64
		wantROI     float64
		wantNeg     bool
	}{
		// BLOCK: negative ROI
		{
			name:        "₹200×0.5 prob - ₹200 cost = -₹100 → negative",
			amount:      200_00,
			probability: 0.50,
			cost:        200_00,
			wantROI:     -100_00,
			wantNeg:     true,
		},
		{
			name:        "ROI = -50 paise",
			amount:      10_00, // ₹10
			probability: 0.30,
			cost:        3_50,  // ₹3.50
			wantROI:     -50.0, // 10×0.3=3 → 3-3.5=-0.5 (=-50 paise)
			wantNeg:     true,
		},
		// PASS: positive ROI
		{
			name:        "₹5000×0.8 prob - 0 cost = +₹4000 → positive",
			amount:      5000_00, // ₹5,000
			probability: 0.80,
			cost:        0,
			wantROI:     4000_00,
			wantNeg:     false,
		},
		{
			name:        "ROI = +4000 paise",
			amount:      500_00, // ₹500
			probability: 0.90,
			cost:        50_00,  // ₹50
			wantROI:     400_00, // 500×0.9=450 → 450-50=400
			wantNeg:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roi := roiCalculation(tt.amount, tt.probability, tt.cost)

			if roi != tt.wantROI {
				t.Errorf("ROI=%.2f want %.2f", roi, tt.wantROI)
			}

			isNeg := roi < 0
			if isNeg != tt.wantNeg {
				t.Errorf("isNegative=%v want %v", isNeg, tt.wantNeg)
			}
		})
	}
}

// ─── Check 5: Non-retryable errors ───────────────────────────────────────────

func TestCheck5_NonRetryable(t *testing.T) {
	v := &Validator{}

	tests := []struct {
		name             string
		upiErrorCode     string
		wantForcePayLink bool
	}{
		// Non-retryable: should set force_payment_link = true
		{"YG → force_payment_link=true", "YG", true},
		{"Z8 → force_payment_link=true", "Z8", true},

		// Retryable: no flag
		{"U30 → no force flag", "U30", false},
		{"U28 → no force flag", "U28", false},
		{"RB → no force flag", "RB", false},
		{"BT → no force flag", "BT", false},
		{"U16 → no force flag", "U16", false},
		{"Z9 → no force flag (Z9 blocks in policy, not here)", "Z9", false},
		{"Z7 → no force flag", "Z7", false},
		{"U68 → no force flag", "U68", false},
		{"U69 → no force flag", "U69", false},
		{"empty → no force flag", "", false},
		{"unknown → no force flag", "XX99", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseCase()
			c.UPIErrorCode = tt.upiErrorCode

			forcePayLink := v.check5NonRetryable(c)

			if forcePayLink != tt.wantForcePayLink {
				t.Errorf("force_payment_link=%v want %v", forcePayLink, tt.wantForcePayLink)
			}
		})
	}
}

// ─── Check 6: Max retries ─────────────────────────────────────────────────────

func TestCheck6_MaxRetries(t *testing.T) {
	v := &Validator{}

	tests := []struct {
		name       string
		retryCount int
		maxRetries int
		wantSkip   bool
		wantReason string
	}{
		// BLOCK: count == max
		{"count=2,max=2 → blocked", 2, 2, true, "Max retries (2) already reached"},

		// BLOCK: count > max
		{"count=3,max=2 → blocked", 3, 2, true, "Max retries (2) already reached"},

		// BOUNDARY: count = max-1 → pass
		{"count=1,max=2 → pass", 1, 2, false, ""},

		// PASS: zero retries
		{"count=0,max=3 → pass", 0, 3, false, ""},

		// BOUNDARY: check uses `>=` — count=max is blocked, count=max-1 passes
		// max=0 edge: 0 >= 0 → always blocked
		{"count=0,max=0 → blocked (0>=0)", 0, 0, true, "Max retries (0) already reached"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseCase()
			c.RetryCount = tt.retryCount
			c.MaxRetries = tt.maxRetries

			skip, reason := v.check6MaxRetries(nil, c)

			if skip != tt.wantSkip {
				t.Errorf("skip=%v want %v", skip, tt.wantSkip)
			}

			if tt.wantReason != "" && reason != tt.wantReason {
				t.Errorf("reason=%q want %q", reason, tt.wantReason)
			}
		})
	}
}
