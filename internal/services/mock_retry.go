package services

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MockRetrySimulator simulates Razorpay payment retry for demo purposes.
// In production, this would be replaced with actual Razorpay API calls.
type MockRetrySimulator struct {
	db *pgxpool.Pool
}

// RetryResult represents the outcome of a retry attempt.
type RetryResult struct {
	Success          bool   `json:"success"`
	NewStatus        string `json:"new_status"`        // "captured" or "failed"
	RazorpayResponse string `json:"razorpay_response"` // Simulated API response
	RetryDuration    int    `json:"retry_duration_ms"` // Simulated processing time
}

func NewMockRetrySimulator(db *pgxpool.Pool) *MockRetrySimulator {
	return &MockRetrySimulator{db: db}
}

// SimulateRetry simulates a Razorpay payment retry with realistic success rates.
// Success probability is based on the original error code:
//   - U30 (timeout): 75% success rate
//   - U28 (bank down): 60% success rate
//   - U16 (insufficient funds): 30% success rate
//   - Z9 (low value insufficient funds): 10% success rate
//   - Others: 50% success rate
func (m *MockRetrySimulator) SimulateRetry(ctx context.Context, paymentID, errorCode string, amount int64) (*RetryResult, error) {
	// Simulate realistic API latency (1-4 seconds)
	processingTime := time.Duration(1000+rand.Intn(3000)) * time.Millisecond
	time.Sleep(processingTime)

	// Determine success probability based on error type
	successRate := m.getSuccessRate(errorCode)
	
	// Roll the dice
	success := rand.Float64() < successRate

	result := &RetryResult{
		Success:       success,
		RetryDuration: int(processingTime.Milliseconds()),
	}

	if success {
		result.NewStatus = "captured"
		result.RazorpayResponse = fmt.Sprintf(
			`{"id":"%s","status":"captured","amount":%d,"method":"upi","captured":true}`,
			paymentID,
			amount,
		)
		slog.Info("mock_retry: simulated SUCCESS",
			"payment_id", paymentID,
			"error_code", errorCode,
			"amount", amount,
			"success_rate", successRate,
			"duration_ms", result.RetryDuration,
		)
	} else {
		result.NewStatus = "failed"
		result.RazorpayResponse = fmt.Sprintf(
			`{"id":"%s","status":"failed","amount":%d,"method":"upi","error_code":"%s","error_description":"Payment failed again"}`,
			paymentID,
			amount,
			errorCode,
		)
		slog.Info("mock_retry: simulated FAILURE",
			"payment_id", paymentID,
			"error_code", errorCode,
			"amount", amount,
			"success_rate", successRate,
			"duration_ms", result.RetryDuration,
		)
	}

	return result, nil
}

// getSuccessRate returns the probability of retry success based on error code.
func (m *MockRetrySimulator) getSuccessRate(errorCode string) float64 {
	switch errorCode {
	case "U30": // Payment timed out (transient)
		return 0.75 // 75% success rate
	case "U28": // Bank server down (transient)
		return 0.60 // 60% success rate
	case "U16": // Insufficient funds (customer issue)
		return 0.30 // 30% success rate
	case "Z9": // Low value insufficient funds
		return 0.10 // 10% success rate
	case "U68": // Transaction not permitted
		return 0.20 // 20% success rate
	case "RB": // Risk threshold by bank
		return 0.15 // 15% success rate
	default:
		return 0.50 // 50% default
	}
}

// UpdateRetryCount increments the retry_count for a recovery case.
func (m *MockRetrySimulator) UpdateRetryCount(ctx context.Context, caseID string) error {
	_, err := m.db.Exec(ctx, `
		UPDATE recovery_cases
		SET retry_count = retry_count + 1,
		    updated_at = NOW()
		WHERE id = $1
	`, caseID)
	
	if err != nil {
		slog.Error("mock_retry: failed to update retry count", "case_id", caseID, "error", err)
		return err
	}
	
	return nil
}

// GetPaymentDetails retrieves payment information needed for retry.
func (m *MockRetrySimulator) GetPaymentDetails(ctx context.Context, caseID string) (paymentID string, errorCode string, amount int64, err error) {
	err = m.db.QueryRow(ctx, `
		SELECT 
			p.razorpay_payment_id,
			COALESCE(rc.upi_error_code, ''),
			rc.revenue_at_risk
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		WHERE rc.id = $1
	`, caseID).Scan(&paymentID, &errorCode, &amount)
	
	if err != nil {
		slog.Error("mock_retry: failed to get payment details", "case_id", caseID, "error", err)
		return "", "", 0, err
	}
	
	return paymentID, errorCode, amount, nil
}
