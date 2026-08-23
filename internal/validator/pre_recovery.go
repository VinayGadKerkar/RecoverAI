package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	redisclient "recoverai/internal/redis"
)

// ─── Validation Result ────────────────────────────────────────────────────────

// ValidationResult is the output of the 6-check validator gate.
type ValidationResult struct {
	RecoveryCaseID uuid.UUID `json:"recovery_case_id"`
	PaymentID      string    `json:"payment_id"`
	Passed         bool      `json:"passed"`
	SkipReason     string    `json:"skip_reason,omitempty"`
	BlockedByCheck string    `json:"blocked_by_check,omitempty"`
	// Flags for special routing
	ForcePaymentLink bool `json:"force_payment_link"` // CHECK 5: non-retryable errors
	CheckedAt        time.Time `json:"checked_at"`
}

// ─── Recovery Case Input ──────────────────────────────────────────────────────

// RecoveryCaseInput is loaded from the database for validation.
type RecoveryCaseInput struct {
	ID                   uuid.UUID
	PaymentID            uuid.UUID
	RazorpayPaymentID    string
	MerchantID           uuid.UUID
	Amount               int64
	UPIErrorCode         string
	RecoveryProbability  float64
	RetryCount           int
	MaxRetries           int
	IsMandatePayment     bool
	CreatedAt            time.Time
	Status               string
}

// ─── Validator ────────────────────────────────────────────────────────────────

// Validator runs 6 checks before the AI service is invoked.
// If ANY check fails, the case is blocked and the AI is NOT called.
type Validator struct {
	db         *pgxpool.Pool
	redis      *redisclient.Client
	cfg        *config.Config
	httpClient *http.Client
}

// NewValidator creates a new pre-recovery validator.
func NewValidator(db *pgxpool.Pool, redis *redisclient.Client, cfg *config.Config) *Validator {
	return &Validator{
		db:         db,
		redis:      redis,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// Validate runs all 6 checks in order. Short-circuits on first failure.
func (v *Validator) Validate(ctx context.Context, caseID uuid.UUID) (*ValidationResult, error) {
	result := &ValidationResult{
		RecoveryCaseID: caseID,
		CheckedAt:      time.Now(),
	}

	// Load recovery case from DB
	caseInput, err := v.loadRecoveryCase(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("load recovery case: %w", err)
	}
	result.PaymentID = caseInput.RazorpayPaymentID

	// ─── CHECK 1: Is payment already captured? ────────────────────────────────
	if skip, reason := v.check1AlreadyCaptured(ctx, caseInput); skip {
		result.Passed = false
		result.SkipReason = reason
		result.BlockedByCheck = "check1_already_captured"
		v.updateCaseSkipped(ctx, caseInput, "customer_self_recovered", reason)
		v.auditLog(ctx, caseInput, "skip_already_captured", reason)
		return result, nil
	}

	// ─── CHECK 2: Is this part of an active bank outage? ─────────────────────
	if skip, reason := v.check2BankOutage(ctx, caseInput); skip {
		result.Passed = false
		result.SkipReason = reason
		result.BlockedByCheck = "check2_bank_outage"
		v.updateCaseOutageBatched(ctx, caseInput, reason)
		v.auditLog(ctx, caseInput, "skip_bank_outage", reason)
		return result, nil
	}

	// ─── CHECK 3: RBI mandate compliance ──────────────────────────────────────
	if skip, reason := v.check3RBIMandate(ctx, caseInput); skip {
		result.Passed = false
		result.SkipReason = reason
		result.BlockedByCheck = "check3_rbi_mandate"
		v.updateCaseSkipped(ctx, caseInput, "pending_human_approval", reason)
		v.auditLog(ctx, caseInput, "skip_rbi_mandate", reason)
		return result, nil
	}

	// ─── CHECK 4: Is recovery economically worth it? ──────────────────────────
	if skip, reason := v.check4ROI(ctx, caseInput); skip {
		result.Passed = false
		result.SkipReason = reason
		result.BlockedByCheck = "check4_negative_roi"
		v.updateCaseSkipped(ctx, caseInput, "not_worth_recovering", reason)
		v.auditLog(ctx, caseInput, "skip_negative_roi", reason)
		return result, nil
	}

	// ─── CHECK 5: Non-retryable errors (flag for AI, do not skip) ────────────
	forcePaymentLink := v.check5NonRetryable(caseInput)
	if forcePaymentLink {
		result.ForcePaymentLink = true
		slog.Info("validator: non-retryable error code, forcing payment_link strategy",
			"case_id", caseID,
			"upi_error_code", caseInput.UPIErrorCode,
		)
	}

	// ─── CHECK 6: Max retries already hit? ────────────────────────────────────
	if skip, reason := v.check6MaxRetries(ctx, caseInput); skip {
		result.Passed = false
		result.SkipReason = reason
		result.BlockedByCheck = "check6_max_retries"
		v.updateCaseSkipped(ctx, caseInput, "failed", reason)
		v.auditLog(ctx, caseInput, "skip_max_retries", reason)
		return result, nil
	}

	// All checks passed → proceed to AI
	result.Passed = true
	slog.Info("validator: all checks passed, proceeding to AI",
		"case_id", caseID,
		"payment_id", caseInput.RazorpayPaymentID,
	)
	return result, nil
}

// ─── CHECK 1: Payment already captured (late authorisation edge case) ─────────

func (v *Validator) check1AlreadyCaptured(ctx context.Context, c *RecoveryCaseInput) (skip bool, reason string) {
	// Call Razorpay API: GET /v1/payments/{id}
	url := fmt.Sprintf("https://api.razorpay.com/v1/payments/%s", c.RazorpayPaymentID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.SetBasicAuth(v.cfg.RazorpayKeyID, v.cfg.RazorpayKeySecret)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		slog.Warn("validator: failed to call Razorpay API for payment status", "error", err)
		return false, "" // fail open — proceed to AI if API is down
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("validator: Razorpay API returned non-200", "status", resp.StatusCode)
		return false, ""
	}

	var payment struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payment); err != nil {
		slog.Warn("validator: failed to decode Razorpay payment response", "error", err)
		return false, ""
	}

	if payment.Status == "captured" {
		return true, "Payment already captured — customer self-recovered"
	}
	return false, ""
}

// ─── CHECK 2: Bank outage detected ────────────────────────────────────────────

func (v *Validator) check2BankOutage(ctx context.Context, c *RecoveryCaseInput) (skip bool, reason string) {
	if c.UPIErrorCode == "" {
		return false, ""
	}

	outageKey := fmt.Sprintf("bank_outage:%s", c.UPIErrorCode)
	exists, err := v.redis.Exists(ctx, outageKey)
	if err != nil {
		slog.Warn("validator: failed to check bank outage flag", "error", err)
		return false, ""
	}

	if exists {
		retryAt := time.Now().Add(60 * time.Minute)
		reason = fmt.Sprintf("Bank outage detected for %s — batched for retry at %s",
			c.UPIErrorCode, retryAt.Format(time.RFC3339))
		return true, reason
	}
	return false, ""
}

// ─── CHECK 3: RBI mandate compliance ──────────────────────────────────────────

func (v *Validator) check3RBIMandate(ctx context.Context, c *RecoveryCaseInput) (skip bool, reason string) {
	if !c.IsMandatePayment {
		return false, ""
	}

	// RBI rule: minimum 24 hours between retries for mandate payments
	rbiMinRetryAt := c.CreatedAt.Add(24 * time.Hour)
	if time.Now().Before(rbiMinRetryAt) {
		return true, fmt.Sprintf("RBI mandate rules: minimum 24h between retries (retry allowed after %s)",
			rbiMinRetryAt.Format(time.RFC3339))
	}

	// RBI rule: amounts > ₹15,000 require explicit customer approval
	if c.Amount > 1500000 { // ₹15,000 in paise
		return true, "RBI: amounts >₹15,000 require explicit customer approval"
	}

	return false, ""
}

// ─── CHECK 4: Recovery ROI ────────────────────────────────────────────────────

func (v *Validator) check4ROI(ctx context.Context, c *RecoveryCaseInput) (skip bool, reason string) {
	// Estimated cost by action type (in paise)
	const (
		costRetry           = 0     // free
		costPaymentLink     = 0     // free
		costSendNotification = 50    // SMS cost
		costHumanEscalation = 10000 // agent time
	)

	// Simple heuristic: assume action will be retry or payment_link (zero cost)
	estimatedCost := int64(0)
	if c.RecoveryProbability < 0.3 {
		// Low probability likely triggers escalation
		estimatedCost = costHumanEscalation
	}

	recoveryROI := float64(c.Amount)*c.RecoveryProbability - float64(estimatedCost)

	// Load merchant's MinRecoveryROI policy (default 0)
	var minROI float64
	err := v.db.QueryRow(ctx, `
		SELECT COALESCE(min_recovery_roi, 0)
		FROM recovery_policies
		WHERE merchant_id = $1
	`, c.MerchantID).Scan(&minROI)
	if err != nil {
		slog.Warn("validator: failed to load min_recovery_roi policy, using default 0", "error", err)
		minROI = 0
	}

	if recoveryROI < minROI {
		return true, fmt.Sprintf("Recovery ROI ₹%.2f below threshold ₹%.2f — not cost effective",
			recoveryROI/100, minROI/100)
	}

	// Update recovery_roi in DB
	v.db.Exec(ctx, `UPDATE recovery_cases SET recovery_roi = $1 WHERE id = $2`, recoveryROI/100, c.ID)

	return false, ""
}

// ─── CHECK 5: Non-retryable error codes ───────────────────────────────────────

// check5NonRetryable flags error codes that should NEVER be retried, only payment_link or escalate.
// Returns true if force_payment_link should be set.
func (v *Validator) check5NonRetryable(c *RecoveryCaseInput) bool {
	// YG = risk threshold exceeded (NPCI block)
	// Z8 = per-transaction limit exceeded
	// Both need alternate payment method, NOT a direct retry
	switch c.UPIErrorCode {
	case "YG", "Z8":
		return true
	default:
		return false
	}
}

// ─── CHECK 6: Max retries hit ─────────────────────────────────────────────────

func (v *Validator) check6MaxRetries(ctx context.Context, c *RecoveryCaseInput) (skip bool, reason string) {
	if c.RetryCount >= c.MaxRetries {
		return true, fmt.Sprintf("Max retries (%d) already reached", c.MaxRetries)
	}
	return false, ""
}

// ─── Database helpers ─────────────────────────────────────────────────────────

func (v *Validator) loadRecoveryCase(ctx context.Context, caseID uuid.UUID) (*RecoveryCaseInput, error) {
	var c RecoveryCaseInput
	err := v.db.QueryRow(ctx, `
		SELECT
			rc.id,
			rc.payment_id,
			p.razorpay_payment_id,
			rc.merchant_id,
			rc.revenue_at_risk,
			COALESCE(rc.upi_error_code, ''),
			COALESCE(rc.recovery_probability, 0.5),
			rc.retry_count,
			rc.max_retries,
			COALESCE(p.is_mandate_payment, FALSE),
			rc.created_at,
			rc.status
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		WHERE rc.id = $1
	`, caseID).Scan(
		&c.ID,
		&c.PaymentID,
		&c.RazorpayPaymentID,
		&c.MerchantID,
		&c.Amount,
		&c.UPIErrorCode,
		&c.RecoveryProbability,
		&c.RetryCount,
		&c.MaxRetries,
		&c.IsMandatePayment,
		&c.CreatedAt,
		&c.Status,
	)
	return &c, err
}

func (v *Validator) updateCaseSkipped(ctx context.Context, c *RecoveryCaseInput, status, reason string) {
	_, err := v.db.Exec(ctx, `
		UPDATE recovery_cases
		SET status = $1, validator_skip_reason = $2, updated_at = NOW()
		WHERE id = $3
	`, status, reason, c.ID)
	if err != nil {
		slog.Error("validator: failed to update case status", "error", err, "case_id", c.ID)
	}
}

func (v *Validator) updateCaseOutageBatched(ctx context.Context, c *RecoveryCaseInput, reason string) {
	cooldownUntil := time.Now().Add(60 * time.Minute)
	_, err := v.db.Exec(ctx, `
		UPDATE recovery_cases
		SET status = 'outage_batched',
		    bank_outage_detected = TRUE,
		    cooldown_until = $1,
		    validator_skip_reason = $2,
		    updated_at = NOW()
		WHERE id = $3
	`, cooldownUntil, reason, c.ID)
	if err != nil {
		slog.Error("validator: failed to update outage batch status", "error", err, "case_id", c.ID)
	}
}

func (v *Validator) auditLog(ctx context.Context, c *RecoveryCaseInput, action, reason string) {
	metadata := map[string]string{"reason": reason}
	metaJSON, _ := json.Marshal(metadata)

	_, err := v.db.Exec(ctx, `
		INSERT INTO audit_logs (entity_type, entity_id, actor, action, metadata)
		VALUES ('recovery_case', $1, 'validator', $2, $3)
	`, c.ID, action, metaJSON)
	if err != nil {
		slog.Error("validator: failed to write audit log", "error", err)
	}
}
