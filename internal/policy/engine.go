package policy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"recoverai/internal/models"
)

// Decision is the policy engine's verdict on an AI command.
type Decision string

const (
	DecisionApproved   Decision = "approved"   // Execute as-is
	DecisionOverridden Decision = "overridden" // Execute with a modified action
	DecisionRejected   Decision = "rejected"   // Block execution entirely
)

// PolicyResult is the output of the Policy Engine for a single AI command.
type PolicyResult struct {
	PaymentID      string                `json:"payment_id"`
	Decision       Decision              `json:"decision"`
	ApprovedAction models.RecoveryAction `json:"approved_action"`
	Reason         string                `json:"reason"`
	EvaluatedAt    time.Time             `json:"evaluated_at"`
}

// Engine evaluates AI commands against deterministic hard rules.
// The AI is NEVER executed directly — all commands must pass through here first.
type Engine struct {
	highValueThreshold int64 // in paise
	maxRetryAmount     int64 // maximum amount for auto-retry (in paise)
}

// NewEngine creates a policy engine with the given constraints.
func NewEngine(highValueThreshold int64) *Engine {
	return &Engine{
		highValueThreshold: highValueThreshold,
		maxRetryAmount:     1000000, // ₹10,000 in paise — auto-retry ceiling
	}
}

// Evaluate applies all policy rules to an AI command and returns a PolicyResult.
// Rules are evaluated in priority order; first matching rule wins.
func (e *Engine) Evaluate(ctx context.Context, cmd *models.AICommand, payment *PaymentContext) (*PolicyResult, error) {
	result := &PolicyResult{
		PaymentID:   cmd.PaymentID,
		EvaluatedAt: time.Now(),
	}

	// ─── Rule 1: High-value payments require human approval ──────────────────
	if payment.Amount >= e.highValueThreshold && !cmd.RequiresApproval {
		result.Decision = DecisionOverridden
		result.ApprovedAction = models.RecoveryActionEscalate
		result.Reason = fmt.Sprintf("high-value payment (₹%.2f ≥ ₹%.2f threshold) requires human approval",
			float64(payment.Amount)/100, float64(e.highValueThreshold)/100)
		slog.Info("policy: high-value override", "payment_id", cmd.PaymentID, "amount", payment.Amount)
		return result, nil
	}

	// ─── Rule 2: YG (risk threshold exceeded) error code must escalate ───────
	if payment.ErrorCode == string(models.UPIErrorYG) {
		result.Decision = DecisionOverridden
		result.ApprovedAction = models.RecoveryActionEscalate
		result.Reason = "UPI error YG (risk threshold exceeded) always requires human approval"
		return result, nil
	}

	// ─── Rule 3: Cap auto-retry amounts ──────────────────────────────────────
	if cmd.RecommendedAction == models.RecoveryActionRetry && payment.Amount > e.maxRetryAmount {
		result.Decision = DecisionOverridden
		result.ApprovedAction = models.RecoveryActionPaymentLink
		result.Reason = fmt.Sprintf("amount ₹%.2f exceeds auto-retry ceiling ₹%.2f; issuing payment link instead",
			float64(payment.Amount)/100, float64(e.maxRetryAmount)/100)
		return result, nil
	}

	// ─── Rule 4: RBI compliance — no retry after 24h ─────────────────────────
	if time.Since(payment.CreatedAt) > 24*time.Hour {
		result.Decision = DecisionRejected
		result.Reason = "payment older than 24 hours; RBI mandate prohibits automated recovery"
		slog.Info("policy: RBI compliance block", "payment_id", cmd.PaymentID)
		return result, nil
	}

	// ─── Rule 5: Low confidence AI commands should not auto-execute ──────────
	if cmd.Confidence < 0.5 && cmd.RecommendedAction == models.RecoveryActionRetry {
		result.Decision = DecisionOverridden
		result.ApprovedAction = models.RecoveryActionNotifyCustomer
		result.Reason = fmt.Sprintf("AI confidence %.2f < 0.5; downgrading retry to customer notification", cmd.Confidence)
		return result, nil
	}

	// ─── Rule 6: Abort is always approved ────────────────────────────────────
	if cmd.RecommendedAction == models.RecoveryActionAbort {
		result.Decision = DecisionApproved
		result.ApprovedAction = models.RecoveryActionAbort
		result.Reason = "abort command approved"
		return result, nil
	}

	// ─── Default: Approve the AI recommendation ──────────────────────────────
	result.Decision = DecisionApproved
	result.ApprovedAction = cmd.RecommendedAction
	result.Reason = "all policy checks passed"
	return result, nil
}

// PaymentContext provides the policy engine with payment details needed for rule evaluation.
type PaymentContext struct {
	PaymentID  string
	MerchantID string
	Amount     int64
	Currency   string
	ErrorCode  string
	CreatedAt  time.Time
	AttemptNum int
}
