package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	kafkapkg "recoverai/internal/kafka"
	"recoverai/internal/policy"
	redisclient "recoverai/internal/redis"
	"recoverai/internal/services"
)

// ─── Kafka Message Structures ─────────────────────────────────────────────────

// RecoveryCommandMessage is consumed from "recovery.commands" topic.
type RecoveryCommandMessage struct {
	Action              string    `json:"action"` // RETRY_PAYMENT | GENERATE_PAYMENT_LINK | SEND_NOTIFICATION | ESCALATE | STOP
	PaymentID           string    `json:"payment_id"`
	CaseID              string    `json:"case_id"`
	ScheduledAtMinutes  int       `json:"scheduled_at_minutes"`
	Parameters          map[string]interface{} `json:"parameters"`
	RiskAssessmentSummary map[string]interface{} `json:"risk_assessment_summary"`
	StrategySummary     map[string]interface{} `json:"strategy_summary"`
}

// RecoveryResultMessage is published to "recovery.results" topic.
type RecoveryResultMessage struct {
	CaseID            string                 `json:"case_id"`
	PaymentID         string                 `json:"payment_id"`
	Action            string                 `json:"action"`
	Status            string                 `json:"status"` // success | failed | blocked | requires_human
	PolicyDecision    policy.PolicyDecision  `json:"policy_decision"`
	ExecutionResult   map[string]interface{} `json:"execution_result,omitempty"`
	AmountRecovered   int64                  `json:"amount_recovered"` // paise
	PartialRecovery   bool                   `json:"partial_recovery"`
	ExecutedAt        time.Time              `json:"executed_at"`
}

// ─── Execution Worker ─────────────────────────────────────────────────────────

type ExecutionWorker struct {
	db           *pgxpool.Pool
	redis        *redisclient.Client
	producer     *kafkapkg.Producer
	cfg          *config.Config
	policyEngine *policy.Engine
	razorpay     *services.RazorpayService
}

func NewExecutionWorker(db *pgxpool.Pool, redis *redisclient.Client, producer *kafkapkg.Producer, cfg *config.Config) *ExecutionWorker {
	return &ExecutionWorker{
		db:           db,
		redis:        redis,
		producer:     producer,
		cfg:          cfg,
		policyEngine: policy.NewEngine(),
		razorpay:     services.NewRazorpayService(cfg),
	}
}

// Run starts the Kafka consumer loop for "recovery.commands".
func (ew *ExecutionWorker) Run(ctx context.Context) error {
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":    ew.cfg.KafkaBrokers,
		"group.id":             "execution-worker-group",
		"auto.offset.reset":    "earliest",
		"enable.auto.commit":   false,
		"max.poll.interval.ms": 300000,
	})
	if err != nil {
		return fmt.Errorf("create kafka consumer: %w", err)
	}
	defer consumer.Close()

	if err := consumer.Subscribe("recovery.commands", nil); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	slog.Info("execution worker: started", "topic", "recovery.commands")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msg, err := consumer.ReadMessage(100 * time.Millisecond)
		if err != nil {
			if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrTimedOut {
				continue
			}
			slog.Error("execution worker: read error", "error", err)
			continue
		}

		if err := ew.processCommand(ctx, msg.Value); err != nil {
			slog.Error("execution worker: process failed", "error", err)
		}

		consumer.CommitMessage(msg)
	}
}

// processCommand handles a single recovery command.
func (ew *ExecutionWorker) processCommand(ctx context.Context, payload []byte) error {
	var cmd RecoveryCommandMessage
	if err := json.Unmarshal(payload, &cmd); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	slog.Info("execution worker: processing command",
		"case_id", cmd.CaseID,
		"action", cmd.Action,
	)

	// Load recovery case context for policy evaluation
	policyInput, err := ew.loadPolicyInput(ctx, cmd)
	if err != nil {
		return fmt.Errorf("load policy input: %w", err)
	}

	// ─── Policy Engine Evaluation ─────────────────────────────────────────────
	decision := ew.policyEngine.Evaluate(policyInput)

	result := RecoveryResultMessage{
		CaseID:         cmd.CaseID,
		PaymentID:      cmd.PaymentID,
		Action:         cmd.Action,
		PolicyDecision: decision,
		ExecutedAt:     time.Now(),
	}

	if !decision.Allowed {
		// Policy blocked execution
		result.Status = "blocked"
		if decision.RequiresHumanApproval {
			result.Status = "requires_human"
			ew.updateCaseStatus(ctx, cmd.CaseID, "pending_human_approval")
		}
		slog.Info("execution worker: policy blocked",
			"case_id", cmd.CaseID,
			"rule", decision.RuleTriggered,
			"reason", decision.Reason,
		)
		ew.auditLog(ctx, cmd.CaseID, "policy_blocked", decision.Reason)
		return ew.publishResult(ctx, &result)
	}

	// ─── Execute Action ───────────────────────────────────────────────────────
	execResult, err := ew.execute(ctx, cmd, policyInput)
	if err != nil {
		result.Status = "failed"
		result.ExecutionResult = map[string]interface{}{"error": err.Error()}
		slog.Error("execution worker: execution failed", "error", err, "case_id", cmd.CaseID)
	} else {
		result.Status = "success"
		result.ExecutionResult = execResult
		result.AmountRecovered = execResult["amount_recovered"].(int64)
		result.PartialRecovery = execResult["partial_recovery"].(bool)
	}

	// ─── Post-Execution: Set Cooldown + Increment Retry Count ────────────────
	if cmd.Action == "RETRY_PAYMENT" {
		cooldownKey := fmt.Sprintf("recovery:cooldown:%s", cmd.CaseID)
		cooldownDuration := time.Duration(policyInput.MerchantPolicy.RetryCooldownMinutes) * time.Minute
		ew.redis.Set(ctx, cooldownKey, "1", cooldownDuration)

		ew.db.Exec(ctx, `UPDATE recovery_cases SET retry_count = retry_count + 1 WHERE id = $1`, cmd.CaseID)
	}

	ew.auditLog(ctx, cmd.CaseID, "executed", fmt.Sprintf("action=%s, status=%s", cmd.Action, result.Status))
	return ew.publishResult(ctx, &result)
}

// execute performs the actual recovery action.
func (ew *ExecutionWorker) execute(ctx context.Context, cmd RecoveryCommandMessage, input policy.PolicyInput) (map[string]interface{}, error) {
	switch cmd.Action {
	case "RETRY_PAYMENT":
		return ew.executeRetry(ctx, input.PaymentID)
	case "GENERATE_PAYMENT_LINK":
		return ew.executePaymentLink(ctx, input.PaymentID, cmd.Parameters)
	case "SEND_NOTIFICATION":
		return ew.executeNotification(ctx, input.PaymentID, cmd.Parameters)
	case "ESCALATE":
		return ew.executeEscalation(ctx, cmd.CaseID, cmd.Parameters)
	case "STOP":
		return map[string]interface{}{"stopped": true}, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", cmd.Action)
	}
}

func (ew *ExecutionWorker) executeRetry(ctx context.Context, razorpayPaymentID string) (map[string]interface{}, error) {
	// Call Razorpay API: POST /v1/payments/{id}/retry (Test Mode)
	slog.Info("executing retry", "payment_id", razorpayPaymentID)
	// TODO: implement Razorpay retry API call
	return map[string]interface{}{
		"action":           "retry",
		"payment_id":       razorpayPaymentID,
		"amount_recovered": int64(0),
		"partial_recovery": false,
	}, nil
}

func (ew *ExecutionWorker) executePaymentLink(ctx context.Context, razorpayPaymentID string, params map[string]interface{}) (map[string]interface{}, error) {
	slog.Info("generating payment link", "payment_id", razorpayPaymentID)
	
	// Load payment details
	var amount int64
	var email, contact string
	ew.db.QueryRow(ctx, `
		SELECT p.amount, COALESCE(p.email, ''), COALESCE(p.contact, '')
		FROM payments p
		WHERE p.razorpay_payment_id = $1
	`, razorpayPaymentID).Scan(&amount, &email, &contact)

	link, err := ew.razorpay.CreatePaymentLink(ctx, razorpayPaymentID, amount, "INR", email, contact)
	if err != nil {
		return nil, fmt.Errorf("create payment link: %w", err)
	}

	return map[string]interface{}{
		"action":           "payment_link",
		"payment_link_url": link,
		"amount_recovered": int64(0),
		"partial_recovery": false,
	}, nil
}

func (ew *ExecutionWorker) executeNotification(ctx context.Context, razorpayPaymentID string, params map[string]interface{}) (map[string]interface{}, error) {
	slog.Info("sending notification", "payment_id", razorpayPaymentID)
	// TODO: integrate with SMS/email service
	return map[string]interface{}{
		"action":           "notify",
		"channel":          params["channel"],
		"amount_recovered": int64(0),
		"partial_recovery": false,
	}, nil
}

func (ew *ExecutionWorker) executeEscalation(ctx context.Context, caseID string, params map[string]interface{}) (map[string]interface{}, error) {
	slog.Info("escalating to merchant", "case_id", caseID)
	ew.updateCaseStatus(ctx, caseID, "pending_human_approval")
	return map[string]interface{}{
		"action":           "escalate",
		"reason":           params["reason"],
		"amount_recovered": int64(0),
		"partial_recovery": false,
	}, nil
}

func (ew *ExecutionWorker) loadPolicyInput(ctx context.Context, cmd RecoveryCommandMessage) (policy.PolicyInput, error) {
	var input policy.PolicyInput

	err := ew.db.QueryRow(ctx, `
		SELECT
			rc.id,
			p.razorpay_payment_id,
			rc.revenue_at_risk,
			rc.retry_count,
			COALESCE(rc.upi_error_code, ''),
			COALESCE(rc.upi_error_category, 'unknown'),
			COALESCE(p.is_mandate_payment, FALSE),
			rc.cooldown_until,
			rc.bank_outage_detected,
			COALESCE(c.successful_payments, 0),
			rp.max_retry_amount_paise,
			rp.max_retries,
			rp.retry_cooldown_minutes,
			rp.require_human_above,
			rp.allowed_actions,
			rp.high_value_threshold_paise
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		LEFT JOIN customers c ON c.id = rc.customer_id
		JOIN recovery_policies rp ON rp.merchant_id = rc.merchant_id
		WHERE rc.id = $1
	`, cmd.CaseID).Scan(
		&input.CaseID,
		&input.PaymentID,
		&input.Amount,
		&input.RetryCount,
		&input.UPIErrorCode,
		&input.UPIErrorCategory,
		&input.IsMandatePayment,
		&input.CooldownUntil,
		&input.BankOutageDetected,
		&input.CustomerSuccessfulPayments,
		&input.MerchantPolicy.MaxRetryAmountPaise,
		&input.MerchantPolicy.MaxRetries,
		&input.MerchantPolicy.RetryCooldownMinutes,
		&input.MerchantPolicy.RequireHumanAbovePaise,
		&input.MerchantPolicy.AllowedActions,
		&input.MerchantPolicy.HighValueThresholdPaise,
	)

	input.Action = cmd.Action
	return input, err
}

func (ew *ExecutionWorker) updateCaseStatus(ctx context.Context, caseID, status string) {
	ew.db.Exec(ctx, `UPDATE recovery_cases SET status = $1, updated_at = NOW() WHERE id = $2`, status, caseID)
}

func (ew *ExecutionWorker) auditLog(ctx context.Context, caseID, action, reason string) {
	metadata, _ := json.Marshal(map[string]string{"reason": reason})
	ew.db.Exec(ctx, `
		INSERT INTO audit_logs (entity_type, entity_id, actor, action, metadata)
		VALUES ('recovery_case', $1, 'execution_worker', $2, $3)
	`, caseID, action, metadata)
}

func (ew *ExecutionWorker) publishResult(ctx context.Context, result *RecoveryResultMessage) error {
	payload, _ := json.Marshal(result)
	return ew.producer.Publish(ctx, "recovery.results", result.CaseID, payload)
}
