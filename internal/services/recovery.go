package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	"recoverai/internal/models"
	"recoverai/internal/policy"
	redisclient "recoverai/internal/redis"
	"recoverai/internal/validator"
)

// RecoveryService orchestrates Stages 3, 4, and 5 for a risk-scored payment.
type RecoveryService struct {
	db        *pgxpool.Pool
	redis     *redisclient.Client
	cfg       *config.Config
	validator *validator.Validator
	policy    *policy.Engine
	httpClient *http.Client
}

func NewRecoveryService(db *pgxpool.Pool, r *redisclient.Client, cfg *config.Config) *RecoveryService {
	return &RecoveryService{
		db:        db,
		redis:     r,
		cfg:       cfg,
		validator: validator.NewValidator(db, r),
		policy:    policy.NewEngine(cfg.HighValueThreshold),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Process drives a risk-scored payment through Stages 3 → 4 → 5.
func (s *RecoveryService) Process(ctx context.Context, event *models.KafkaRiskScoredEvent) error {
	slog.Info("recovery: processing payment", "payment_id", event.PaymentID)

	// ─── Stage 3: Pre-Recovery Validator ─────────────────────────────────────
	validationResult, err := s.validator.Validate(ctx, event)
	if err != nil {
		return fmt.Errorf("validator: %w", err)
	}

	s.persistValidationResult(ctx, validationResult)

	if !validationResult.Passed {
		slog.Info("recovery: payment blocked by validator",
			"payment_id", event.PaymentID,
			"blocked_by", validationResult.BlockedBy,
		)
		s.auditLog(ctx, event.PaymentID, event.MerchantID, "validator", "blocked", "system",
			validationResult.BlockedBy, nil)
		return nil // Not an error — legitimately blocked
	}

	// Acquire recovery lock to prevent concurrent processing
	lockKey := fmt.Sprintf("recovery:lock:%s", event.PaymentID)
	locked, err := s.redis.SetNXWithTTL(ctx, lockKey, "1", 10*time.Minute)
	if err != nil || locked {
		return fmt.Errorf("failed to acquire recovery lock for %s", event.PaymentID)
	}
	defer s.redis.Del(ctx, lockKey)

	// Create recovery attempt record
	attemptNum, err := s.createRecoveryAttempt(ctx, event)
	if err != nil {
		return fmt.Errorf("create recovery attempt: %w", err)
	}

	// ─── Stage 4: AI Recovery Service ────────────────────────────────────────
	aiCmd, err := s.callAIService(ctx, event)
	if err != nil {
		slog.Error("recovery: AI service error", "payment_id", event.PaymentID, "error", err)
		// Fallback: safe default command
		aiCmd = s.safeDefaultCommand(event)
	}

	s.auditLog(ctx, event.PaymentID, event.MerchantID, "ai_service", "command_generated", "ai",
		string(aiCmd.RecommendedAction), map[string]any{
			"confidence": aiCmd.Confidence,
			"rationale":  aiCmd.Rationale,
		})

	// ─── Stage 5: Policy Engine ───────────────────────────────────────────────
	paymentCtx, err := s.loadPaymentContext(ctx, event)
	if err != nil {
		return fmt.Errorf("load payment context: %w", err)
	}

	policyResult, err := s.policy.Evaluate(ctx, aiCmd, paymentCtx)
	if err != nil {
		return fmt.Errorf("policy engine: %w", err)
	}

	slog.Info("policy decision",
		"payment_id", event.PaymentID,
		"decision", policyResult.Decision,
		"approved_action", policyResult.ApprovedAction,
		"reason", policyResult.Reason,
	)

	s.auditLog(ctx, event.PaymentID, event.MerchantID, "policy_engine", "evaluated", "policy_engine",
		string(policyResult.Decision), map[string]any{
			"approved_action": policyResult.ApprovedAction,
			"reason":          policyResult.Reason,
		})

	if policyResult.Decision == policy.DecisionRejected {
		s.updateAttemptStatus(ctx, event.PaymentID, attemptNum, string(models.RecoveryStatusAborted),
			string(policyResult.ApprovedAction), aiCmd, string(policyResult.Decision), policyResult.Reason)
		return nil
	}

	// Execute the policy-approved action
	result, err := s.execute(ctx, event, policyResult.ApprovedAction)
	if err != nil {
		slog.Error("recovery: execution failed", "payment_id", event.PaymentID, "error", err)
		s.updateAttemptStatus(ctx, event.PaymentID, attemptNum, string(models.RecoveryStatusFailed),
			string(policyResult.ApprovedAction), aiCmd, string(policyResult.Decision), policyResult.Reason)
		return err
	}

	s.updateAttemptStatus(ctx, event.PaymentID, attemptNum, string(models.RecoveryStatusSucceeded),
		string(policyResult.ApprovedAction), aiCmd, string(policyResult.Decision), policyResult.Reason)

	// Increment merchant daily retry counter
	s.incrementMerchantDailyRetries(ctx, event.MerchantID)

	slog.Info("recovery: action executed",
		"payment_id", event.PaymentID,
		"action", policyResult.ApprovedAction,
		"result", result,
	)
	return nil
}

// callAIService sends a risk-scored event to the Python AI service and returns an AICommand.
// This is the ONLY place in Go that calls the AI — and it never executes the result directly.
func (s *RecoveryService) callAIService(ctx context.Context, event *models.KafkaRiskScoredEvent) (*models.AICommand, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal AI request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.AIServiceURL+"/api/v1/recover", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create AI request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AI service request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI service returned status %d", resp.StatusCode)
	}

	var cmd models.AICommand
	if err := json.NewDecoder(resp.Body).Decode(&cmd); err != nil {
		return nil, fmt.Errorf("decode AI response: %w", err)
	}
	return &cmd, nil
}

// execute carries out the policy-approved recovery action.
func (s *RecoveryService) execute(ctx context.Context, event *models.KafkaRiskScoredEvent, action models.RecoveryAction) (map[string]any, error) {
	switch action {
	case models.RecoveryActionRetry:
		return s.executeRetry(ctx, event)
	case models.RecoveryActionPaymentLink:
		return s.executePaymentLink(ctx, event)
	case models.RecoveryActionNotifyCustomer:
		return s.executeNotification(ctx, event)
	case models.RecoveryActionEscalate:
		return s.executeEscalation(ctx, event)
	case models.RecoveryActionAbort:
		return map[string]any{"aborted": true}, nil
	default:
		return nil, fmt.Errorf("unknown recovery action: %s", action)
	}
}

func (s *RecoveryService) executeRetry(ctx context.Context, event *models.KafkaRiskScoredEvent) (map[string]any, error) {
	slog.Info("executing retry", "payment_id", event.PaymentID)
	// TODO: call Razorpay API via razorpay.go service
	return map[string]any{"action": "retry", "status": "queued"}, nil
}

func (s *RecoveryService) executePaymentLink(ctx context.Context, event *models.KafkaRiskScoredEvent) (map[string]any, error) {
	slog.Info("creating payment link", "payment_id", event.PaymentID)
	// TODO: call Razorpay Payment Links API
	return map[string]any{"action": "payment_link", "status": "created"}, nil
}

func (s *RecoveryService) executeNotification(ctx context.Context, event *models.KafkaRiskScoredEvent) (map[string]any, error) {
	slog.Info("sending customer notification", "payment_id", event.PaymentID)
	// TODO: send via Razorpay or email/SMS
	return map[string]any{"action": "notify_customer", "status": "sent"}, nil
}

func (s *RecoveryService) executeEscalation(ctx context.Context, event *models.KafkaRiskScoredEvent) (map[string]any, error) {
	slog.Info("escalating to merchant", "payment_id", event.PaymentID)
	// TODO: notify merchant dashboard + create alert
	return map[string]any{"action": "escalate", "status": "escalated"}, nil
}

// safeDefaultCommand returns a conservative fallback when the AI service is unavailable.
func (s *RecoveryService) safeDefaultCommand(event *models.KafkaRiskScoredEvent) *models.AICommand {
	return &models.AICommand{
		PaymentID:         event.PaymentID,
		RecommendedAction: models.RecoveryActionNotifyCustomer,
		Rationale:         "AI service unavailable — using safe default (notify customer)",
		Confidence:        0.3,
		NotifyCustomer:    true,
		RequiresApproval:  false,
		Diagnosis:         "fallback",
		GeneratedAt:       time.Now(),
	}
}

func (s *RecoveryService) loadPaymentContext(ctx context.Context, event *models.KafkaRiskScoredEvent) (*policy.PaymentContext, error) {
	var pc policy.PaymentContext
	err := s.db.QueryRow(ctx,
		"SELECT id, merchant_id, amount, currency, COALESCE(error_code,''), COALESCE(razorpay_created_at, created_at) FROM payments WHERE id = $1",
		event.PaymentID,
	).Scan(&pc.PaymentID, &pc.MerchantID, &pc.Amount, &pc.Currency, &pc.ErrorCode, &pc.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("load payment: %w", err)
	}
	return &pc, nil
}

func (s *RecoveryService) createRecoveryAttempt(ctx context.Context, event *models.KafkaRiskScoredEvent) (int, error) {
	var count int
	s.db.QueryRow(ctx, "SELECT COUNT(*) FROM recovery_attempts WHERE payment_id = $1", event.PaymentID).Scan(&count)
	attemptNum := count + 1

	_, err := s.db.Exec(ctx,
		"INSERT INTO recovery_attempts (payment_id, merchant_id, attempt_number, status) VALUES ($1, $2, $3, 'pending')",
		event.PaymentID, event.MerchantID, attemptNum)
	return attemptNum, err
}

func (s *RecoveryService) updateAttemptStatus(ctx context.Context, paymentID string, attemptNum int, status, action string, cmd *models.AICommand, policyDecision, policyReason string) {
	cmdJSON, _ := json.Marshal(cmd)
	now := time.Now()
	s.db.Exec(ctx, `
		UPDATE recovery_attempts
		SET status=$1, action=$2, ai_command=$3, policy_decision=$4, policy_reason=$5, executed_at=$6, updated_at=NOW()
		WHERE payment_id=$7 AND attempt_number=$8
	`, status, action, cmdJSON, policyDecision, policyReason, now, paymentID, attemptNum)
}

func (s *RecoveryService) persistValidationResult(ctx context.Context, result *models.ValidationResult) {
	checksJSON, _ := json.Marshal(result.Checks)
	s.db.Exec(ctx,
		"INSERT INTO validation_results (payment_id, passed, checks, blocked_by) VALUES ($1, $2, $3, $4)",
		result.PaymentID, result.Passed, checksJSON, result.BlockedBy)
}

func (s *RecoveryService) auditLog(ctx context.Context, paymentID, merchantID, stage, action, actor, decision string, metadata map[string]any) {
	metaJSON, _ := json.Marshal(metadata)
	s.db.Exec(ctx,
		"INSERT INTO audit_log (payment_id, merchant_id, stage, action, actor, decision, metadata) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		paymentID, merchantID, stage, action, actor, decision, metaJSON)
}

func (s *RecoveryService) incrementMerchantDailyRetries(ctx context.Context, merchantID string) {
	key := fmt.Sprintf("merchant:daily_retries:%s:%s", merchantID, time.Now().UTC().Format("2006-01-02"))
	s.redis.Incr(ctx, key)
	s.redis.Expire(ctx, key, 25*time.Hour)
}
