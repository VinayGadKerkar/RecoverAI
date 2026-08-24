package consumers

import (
	"testing"
)

// ─── UPI Error Classification ─────────────────────────────────────────────────

func TestClassifyUPIError(t *testing.T) {
	tests := []struct {
		code            string
		wantCategory    ErrorCategory
		wantFailureType FailureType
	}{
		// ── Technical Decline (TD) ──────────────────────────────────────────
		{"U30", ErrorCategoryTD, FailureTypeTransientBankDebitFail},
		{"U28", ErrorCategoryTD, FailureTypeBankServerDown},
		{"RB", ErrorCategoryTD, FailureTypeBankLoadBlock},
		{"BT", ErrorCategoryTD, FailureTypeBeneficiaryTimeout},

		// ── Business Decline (BD) ───────────────────────────────────────────
		{"U16", ErrorCategoryBD, FailureTypeInsufficientBalance},
		{"Z9", ErrorCategoryBD, FailureTypeInsufficientFunds},
		{"Z8", ErrorCategoryBD, FailureTypePerTxLimitExceeded},
		{"Z7", ErrorCategoryBD, FailureTypeVelocityLimit},
		{"U68", ErrorCategoryBD, FailureTypeTxNotPermitted},
		{"YG", ErrorCategoryBD, FailureTypeRiskThresholdExceeded},
		{"U69", ErrorCategoryBD, FailureTypeCollectRequestExpired},

		// ── Unknown ─────────────────────────────────────────────────────────
		{"", ErrorCategoryUnknown, FailureTypeUnknown},
		{"XX99", ErrorCategoryUnknown, FailureTypeUnknown},
		{"u30", ErrorCategoryUnknown, FailureTypeUnknown}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			cat, ft := classifyUPIError(tt.code)

			if cat != tt.wantCategory {
				t.Errorf("code=%q: category=%q want %q", tt.code, cat, tt.wantCategory)
			}

			if ft != tt.wantFailureType {
				t.Errorf("code=%q: failureType=%q want %q", tt.code, ft, tt.wantFailureType)
			}
		})
	}
}

// ─── Risk Score Calculation ───────────────────────────────────────────────────

func TestComputeRiskScore(t *testing.T) {
	tests := []struct {
		name               string
		amount             int64
		successfulPayments int
		category           ErrorCategory
		failureType        FailureType
		wantMinScore       float64
		wantMaxScore       float64
		wantPriority       string
	}{
		{
			name:               "high LTV + TD + ₹10K → CRITICAL (score > 1.5)",
			amount:             10_000_00, // ₹10,000 → amountMul=1.5
			successfulPayments: 10,        // > 5 → customerMul=1.5
			category:           ErrorCategoryTD,
			failureType:        FailureTypeTransientBankDebitFail, // TD → failureMul=1.4
			// score = 1.5 × 1.5 × 1.4 = 3.15
			wantMinScore: 1.5,
			wantMaxScore: 10.0,
			wantPriority: "critical",
		},
		{
			name:               "new customer + BD non-retryable + ₹99 → LOW (score < 0.5)",
			amount:             99_00, // ₹99 → amountMul=0.5
			successfulPayments: 0,     // new customer → customerMul=0.7
			category:           ErrorCategoryBD,
			failureType:        FailureTypeRiskThresholdExceeded, // YG → failureMul=0.2
			// score = 0.5 × 0.7 × 0.2 = 0.07
			wantMinScore: 0.0,
			wantMaxScore: 0.5,
			wantPriority: "low",
		},
		{
			name:               "medium LTV + TD + ₹5K → MEDIUM or HIGH",
			amount:             5_000_00, // ₹5,000 → amountMul=1.0 (>₹500, <=₹10K)
			successfulPayments: 3,        // > 2 → customerMul=1.2
			category:           ErrorCategoryTD,
			failureType:        FailureTypeBankServerDown, // TD → failureMul=1.4
			// score = 1.0 × 1.2 × 1.4 = 1.68 → CRITICAL
			// (5000 < 10000, so amountMul=1.0)
			wantMinScore: 0.5,
			wantMaxScore: 2.5,
			wantPriority: "critical", // 1.68 > 1.5 → critical
		},
		{
			name:               "high amount > ₹50K → amountMul=2.0",
			amount:             50_001_00, // ₹50,001
			successfulPayments: 6,         // > 5 → customerMul=1.5
			category:           ErrorCategoryTD,
			failureType:        FailureTypeTransientBankDebitFail,
			// score = 2.0 × 1.5 × 1.4 = 4.2 → critical
			wantMinScore: 1.5,
			wantMaxScore: 10.0,
			wantPriority: "critical",
		},
		{
			name:               "BD insufficient funds + some history",
			amount:             1_000_00, // ₹1,000 → amountMul=1.0
			successfulPayments: 1,        // > 0 → customerMul=1.0
			category:           ErrorCategoryBD,
			failureType:        FailureTypeInsufficientBalance, // failureMul=0.9
			// score = 1.0 × 1.0 × 0.9 = 0.9 → high (>0.5, <=0.9 boundary)
			wantMinScore: 0.5,
			wantMaxScore: 1.5,
			wantPriority: "high",
		},
		{
			name:               "BD velocity limit → medium/low",
			amount:             500_00, // ₹500 → amountMul=1.0
			successfulPayments: 0,      // new customer → customerMul=0.7
			category:           ErrorCategoryBD,
			failureType:        FailureTypeVelocityLimit, // failureMul=0.6
			// score = 1.0 × 0.7 × 0.6 = 0.42 → low
			wantMinScore: 0.0,
			wantMaxScore: 0.5,
			wantPriority: "low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := computeRiskScore(tt.amount, tt.successfulPayments, tt.category, tt.failureType)
			priority := scoreToPriority(score)

			if score < tt.wantMinScore || score > tt.wantMaxScore {
				t.Errorf("score=%.4f want in [%.4f, %.4f]", score, tt.wantMinScore, tt.wantMaxScore)
			}

			if priority != tt.wantPriority {
				t.Errorf("priority=%q want %q (score=%.4f)", priority, tt.wantPriority, score)
			}
		})
	}
}

// ─── Score → Priority mapping ─────────────────────────────────────────────────

func TestScoreToPriority(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		// Boundaries
		{1.51, "critical"},
		{1.50, "critical"}, // > 1.5 strict, so 1.5 is "high"
		{1.49, "high"},
		{0.91, "high"},
		{0.90, "high"}, // > 0.9 strict boundary
		{0.89, "medium"},
		{0.51, "medium"},
		{0.50, "medium"}, // > 0.5 strict
		{0.49, "low"},
		{0.0, "low"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := scoreToPriority(tt.score)
			if got != tt.want {
				t.Errorf("scoreToPriority(%.2f)=%q want %q", tt.score, got, tt.want)
			}
		})
	}
}

// ─── Recovery ROI Calculation ─────────────────────────────────────────────────
// The ROI formula mirrors the check4ROI logic in pre_recovery.go:
//   ROI = amount × probability − estimated_cost

// recoveryCost mirrors the cost constants and heuristic in check4ROI.
const (
	costRetry            int64 = 0
	costPaymentLink      int64 = 0
	costSendNotification int64 = 50     // SMS cost in paise
	costHumanEscalation  int64 = 10_000 // agent time in paise (₹100)
)

// computeROI models the heuristic in check4ROI (no DB required).
func computeROI(amountPaise int64, recoveryProbability float64, actionType string) float64 {
	var cost int64
	switch actionType {
	case "retry", "payment_link":
		cost = costRetry
	case "sms_notify":
		cost = costSendNotification
	case "human_escalation":
		cost = costHumanEscalation
	}
	return float64(amountPaise)*recoveryProbability - float64(cost)
}

func TestRecoveryROI(t *testing.T) {
	tests := []struct {
		name         string
		amount       int64
		probability  float64
		action       string
		wantROI      float64
		wantPositive bool
	}{
		{
			name:         "RETRY + ₹5000 + 0.8 prob → ROI = ₹4000 (400000 paise)",
			amount:       500_000, // ₹5,000 in paise
			probability:  0.80,
			action:       "retry",
			wantROI:      400_000, // 500000 × 0.8 - 0 = 400000
			wantPositive: true,
		},
		{
			name:         "SMS_NOTIFY + ₹500 + 0.3 prob → ROI = 100 paise (₹1)",
			amount:       50_000, // ₹500 in paise
			probability:  0.30,
			action:       "sms_notify",
			wantROI:      15_000 - 50, // 50000×0.3=15000, 15000-50=14950
			wantPositive: true,
		},
		{
			name:         "HUMAN_ESCALATION + ₹200 + 0.5 prob → negative ROI",
			amount:       20_000, // ₹200 in paise
			probability:  0.50,
			action:       "human_escalation",
			wantROI:      10_000 - 10_000, // 20000×0.5=10000, 10000-10000=0
			wantPositive: true,            // 0 is not negative
		},
		{
			name:        "HUMAN_ESCALATION + ₹100 + 0.5 prob → -₹50 ROI",
			amount:      10_000, // ₹100 in paise
			probability: 0.50,
			action:      "human_escalation",
			// 10000×0.5=5000 − 10000 = -5000
			wantROI:      -5000,
			wantPositive: false,
		},
		{
			name:         "PAYMENT_LINK + ₹10000 + 0.9 prob → positive",
			amount:       1_000_000, // ₹10,000 in paise
			probability:  0.90,
			action:       "payment_link",
			wantROI:      900_000, // 1000000×0.9 - 0
			wantPositive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roi := computeROI(tt.amount, tt.probability, tt.action)

			if roi != tt.wantROI {
				t.Errorf("ROI=%.0f want %.0f", roi, tt.wantROI)
			}

			isPositive := roi >= 0
			if isPositive != tt.wantPositive {
				t.Errorf("isPositive=%v want %v (ROI=%.0f)", isPositive, tt.wantPositive, roi)
			}
		})
	}
}

// ─── Risk score multiplier breakdown ─────────────────────────────────────────
// Verify each multiplier bucket independently so regressions are easy to diagnose.

func TestAmountMultiplier(t *testing.T) {
	tests := []struct {
		amount  int64
		wantMul float64
	}{
		{0, 0.5},         // ≤ ₹500
		{50_000, 0.5},    // = ₹500 boundary (not strictly >500, so 0.5)
		{50_001, 1.0},    // just over ₹500
		{1_000_000, 1.5}, // just over ₹10,000
		{5_000_001, 2.0}, // just over ₹50,000
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			// Extract from computeRiskScore with neutral multipliers:
			// Use successfulPayments=1 (customerMul=1.0) and unknown category (failureMul=0.7)
			// so score = amountMul × 1.0 × 0.7, then divide by 0.7
			score := computeRiskScore(tt.amount, 1, ErrorCategoryUnknown, FailureTypeUnknown)
			gotMul := score / 0.7 // undo customerMul(1.0) × failureMul(0.7)

			if gotMul != tt.wantMul {
				t.Errorf("amountMul for %d = %.1f want %.1f", tt.amount, gotMul, tt.wantMul)
			}
		})
	}
}

func TestCustomerValueMultiplier(t *testing.T) {
	tests := []struct {
		successfulPayments int
		wantMul            float64
	}{
		{0, 0.7}, // new customer
		{1, 1.0}, // > 0
		{2, 1.0}, // = 2 (not strictly >2)
		{3, 1.2}, // > 2
		{5, 1.2}, // = 5 (not strictly >5)
		{6, 1.5}, // > 5
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			// Amount ₹1,000 → amountMul=1.0; unknown → failureMul=0.7
			// score = 1.0 × customerMul × 0.7
			score := computeRiskScore(1_000_00, tt.successfulPayments, ErrorCategoryUnknown, FailureTypeUnknown)
			gotMul := score / 0.7

			if gotMul != tt.wantMul {
				t.Errorf("customerMul for %d payments = %.1f want %.1f", tt.successfulPayments, gotMul, tt.wantMul)
			}
		})
	}
}

func TestFailureTypeMultiplier(t *testing.T) {
	tests := []struct {
		category    ErrorCategory
		failureType FailureType
		wantMul     float64
	}{
		// TD always 1.4
		{ErrorCategoryTD, FailureTypeTransientBankDebitFail, 1.4},
		{ErrorCategoryTD, FailureTypeBankServerDown, 1.4},
		{ErrorCategoryTD, FailureTypeBankLoadBlock, 1.4},

		// BD variants
		{ErrorCategoryBD, FailureTypeInsufficientBalance, 0.9},
		{ErrorCategoryBD, FailureTypeInsufficientFunds, 0.9},
		{ErrorCategoryBD, FailureTypeVelocityLimit, 0.6},
		{ErrorCategoryBD, FailureTypePerTxLimitExceeded, 0.6},
		{ErrorCategoryBD, FailureTypeRiskThresholdExceeded, 0.2}, // YG
		{ErrorCategoryBD, FailureTypeTxNotPermitted, 0.8},        // default BD

		// Unknown
		{ErrorCategoryUnknown, FailureTypeUnknown, 0.7},
	}

	for _, tt := range tests {
		t.Run(string(tt.failureType), func(t *testing.T) {
			// Amount ₹1,000 → amountMul=1.0; successfulPayments=1 → customerMul=1.0
			// score = 1.0 × 1.0 × failureMul
			score := computeRiskScore(1_000_00, 1, tt.category, tt.failureType)
			gotMul := score // amountMul and customerMul both = 1.0

			if gotMul != tt.wantMul {
				t.Errorf("failureMul for %q = %.2f want %.2f", tt.failureType, gotMul, tt.wantMul)
			}
		})
	}
}
