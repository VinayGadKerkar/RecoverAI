package validator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/models"
	redisclient "recoverai/internal/redis"
)

const (
	// Maximum number of recovery attempts allowed per payment.
	maxRecoveryAttempts = 3

	// Maximum age of a payment eligible for recovery (RBI mandate compliance).
	maxPaymentAgeHours = 24

	// Minimum amount eligible for automated recovery (in paise = ₹10).
	minRecoverableAmount int64 = 1000

	// Daily retry cap per merchant (prevents over-retrying and merchant flagging).
	maxDailyMerchantRetries = 50
)

// Validator runs the 6 hard checks that MUST ALL PASS before the AI is ever called.
// The AI is NEVER invoked unless this validator returns Passed=true.
type Validator struct {
	db    *pgxpool.Pool
	redis *redisclient.Client
}

// NewValidator creates a new pre-recovery validator.
func NewValidator(db *pgxpool.Pool, r *redisclient.Client) *Validator {
	return &Validator{db: db, redis: r}
}

// Validate runs all 6 checks and returns a ValidationResult.
// Short-circuits on the first failure — remaining checks are marked as skipped.
func (v *Validator) Validate(ctx context.Context, event *models.KafkaRiskScoredEvent) (*models.ValidationResult, error) {
	result := &models.ValidationResult{
		PaymentID: event.PaymentID,
		CheckedAt: time.Now(),
	}

	checks := []struct {
		name string
		fn   func() (bool, string, error)
	}{
		{"payment_status_is_failed", func() (bool, string, error) {
			return v.checkPaymentStatusFailed(ctx, event)
		}},
		{"not_already_recovering", func() (bool, string, error) {
			return v.checkNotAlreadyRecovering(ctx, event)
		}},
		{"retry_attempts_under_limit", func() (bool, string, error) {
			return v.checkRetryAttemptsUnderLimit(ctx, event)
		}},
		{"payment_age_within_window", func() (bool, string, error) {
			return v.checkPaymentAgeWithinWindow(ctx, event)
		}},
		{"amount_above_minimum", func() (bool, string, error) {
			return v.checkAmountAboveMinimum(ctx, event)
		}},
		{"merchant_daily_limit_not_exceeded", func() (bool, string, error) {
			return v.checkMerchantDailyLimit(ctx, event)
		}},
	}

	for _, check := range checks {
		passed, reason, err := check.fn()
		if err != nil {
			slog.Error("validator: check error", "check", check.name, "payment_id", event.PaymentID, "error", err)
			// Count as failed on error — do not call AI if we cannot verify
			result.Checks = append(result.Checks, models.ValidationCheck{
				Name:   check.name,
				Passed: false,
				Reason: fmt.Sprintf("check error: %v", err),
			})
			result.Passed = false
			result.BlockedBy = check.name
			return result, nil
		}

		result.Checks = append(result.Checks, models.ValidationCheck{
			Name:   check.name,
			Passed: passed,
			Reason: reason,
		})

		if !passed {
			result.Passed = false
			result.BlockedBy = check.name
			slog.Info("validator: payment blocked", "check", check.name, "payment_id", event.PaymentID, "reason", reason)
			return result, nil
		}
	}

	result.Passed = true
	return result, nil
}

// ─── Check 1: Payment must be in 'failed' state ───────────────────────────────
func (v *Validator) checkPaymentStatusFailed(ctx context.Context, event *models.KafkaRiskScoredEvent) (bool, string, error) {
	var status string
	err := v.db.QueryRow(ctx, "SELECT status FROM payments WHERE id = $1", event.PaymentID).Scan(&status)
	if err != nil {
		return false, "", fmt.Errorf("query payment status: %w", err)
	}
	if status == string(models.PaymentStatusFailed) {
		return true, "", nil
	}
	return false, fmt.Sprintf("payment status is '%s', not 'failed'", status), nil
}

// ─── Check 2: No active recovery already in progress ────────────────────────
func (v *Validator) checkNotAlreadyRecovering(ctx context.Context, event *models.KafkaRiskScoredEvent) (bool, string, error) {
	lockKey := fmt.Sprintf("recovery:lock:%s", event.PaymentID)
	exists, err := v.redis.Exists(ctx, lockKey)
	if err != nil {
		return false, "", fmt.Errorf("check recovery lock: %w", err)
	}
	if exists {
		return false, "recovery already in progress for this payment", nil
	}
	return true, "", nil
}

// ─── Check 3: Retry attempts under the per-payment limit ─────────────────────
func (v *Validator) checkRetryAttemptsUnderLimit(ctx context.Context, event *models.KafkaRiskScoredEvent) (bool, string, error) {
	var count int
	err := v.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM recovery_attempts WHERE payment_id = $1",
		event.PaymentID,
	).Scan(&count)
	if err != nil {
		return false, "", fmt.Errorf("count recovery attempts: %w", err)
	}
	if count >= maxRecoveryAttempts {
		return false, fmt.Sprintf("payment has already had %d/%d recovery attempts", count, maxRecoveryAttempts), nil
	}
	return true, "", nil
}

// ─── Check 4: Payment is within the recovery time window ──────────────────────
func (v *Validator) checkPaymentAgeWithinWindow(ctx context.Context, event *models.KafkaRiskScoredEvent) (bool, string, error) {
	var createdAt time.Time
	err := v.db.QueryRow(ctx,
		"SELECT COALESCE(razorpay_created_at, created_at) FROM payments WHERE id = $1",
		event.PaymentID,
	).Scan(&createdAt)
	if err != nil {
		return false, "", fmt.Errorf("query payment created_at: %w", err)
	}
	age := time.Since(createdAt)
	if age > time.Duration(maxPaymentAgeHours)*time.Hour {
		return false, fmt.Sprintf("payment is %.1f hours old, exceeds %dh window", age.Hours(), maxPaymentAgeHours), nil
	}
	return true, "", nil
}

// ─── Check 5: Amount is above the minimum recoverable threshold ───────────────
func (v *Validator) checkAmountAboveMinimum(ctx context.Context, event *models.KafkaRiskScoredEvent) (bool, string, error) {
	var amount int64
	err := v.db.QueryRow(ctx, "SELECT amount FROM payments WHERE id = $1", event.PaymentID).Scan(&amount)
	if err != nil {
		return false, "", fmt.Errorf("query payment amount: %w", err)
	}
	if amount < minRecoverableAmount {
		return false, fmt.Sprintf("amount ₹%.2f is below minimum ₹%.2f", float64(amount)/100, float64(minRecoverableAmount)/100), nil
	}
	return true, "", nil
}

// ─── Check 6: Merchant has not exceeded the daily retry cap ──────────────────
func (v *Validator) checkMerchantDailyLimit(ctx context.Context, event *models.KafkaRiskScoredEvent) (bool, string, error) {
	key := fmt.Sprintf("merchant:daily_retries:%s:%s",
		event.MerchantID,
		time.Now().UTC().Format("2006-01-02"),
	)
	countStr, err := v.redis.Get(ctx, key)
	if err != nil {
		// Key doesn't exist yet — count is 0
		return true, "", nil
	}
	var count int64
	fmt.Sscanf(countStr, "%d", &count)
	if count >= maxDailyMerchantRetries {
		return false, fmt.Sprintf("merchant has hit daily retry cap (%d/%d)", count, maxDailyMerchantRetries), nil
	}
	return true, "", nil
}
