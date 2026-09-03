package consumers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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

// RecoveryCommandMessage is consumed from "payment.ai_commands" topic.
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
	mockRetry    *services.MockRetrySimulator // For simulated flow
}

func NewExecutionWorker(db *pgxpool.Pool, redis *redisclient.Client, producer *kafkapkg.Producer, cfg *config.Config) *ExecutionWorker {
	return &ExecutionWorker{
		db:           db,
		redis:        redis,
		producer:     producer,
		cfg:          cfg,
		policyEngine: policy.NewEngine(),
		razorpay:     services.NewRazorpayService(cfg),
		mockRetry:    services.NewMockRetrySimulator(db),
	}
}

// Run starts the Kafka consumer loop for "payment.ai_commands".
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

	if err := consumer.Subscribe(kafkapkg.TopicAICommands, nil); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	slog.Info("execution worker: started", "topic", kafkapkg.TopicAICommands)

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
		
		// Publish WebSocket event for action executed
		ew.publishWebSocketEvent(ctx, cmd.CaseID, cmd.PaymentID, "execution_worker", "action_executed", map[string]interface{}{
			"action_type": cmd.Action,
		})
		
		// If payment was recovered, publish special event and trigger metric update
		if result.AmountRecovered > 0 {
			ew.publishWebSocketEvent(ctx, cmd.CaseID, cmd.PaymentID, "execution_worker", "payment_captured", map[string]interface{}{
				"amount_paise": result.AmountRecovered,
			})
			ew.publishMetricUpdate(ctx)
		}
	}

	// ─── Post-Execution: Set Cooldown + Increment Retry Count ────────────────
	if cmd.Action == "RETRY_PAYMENT" {
		cooldownKey := fmt.Sprintf("recovery:cooldown:%s", cmd.CaseID)
		cooldownDuration := time.Duration(policyInput.MerchantPolicy.RetryCooldownMinutes) * time.Minute
		
		// DEMO_MODE: Cap cooldown at 1 minute for smooth presentations
		if ew.cfg.DemoMode && cooldownDuration > time.Minute {
			slog.Info("DEMO_MODE: cooldown reduced to 1 minute",
				"case_id", cmd.CaseID,
				"original_cooldown_minutes", policyInput.MerchantPolicy.RetryCooldownMinutes,
			)
			cooldownDuration = time.Minute
		}
		
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
	slog.Info("executing retry", "payment_id", razorpayPaymentID)
	
	// Get case details for simulation
	var caseID, errorCode string
	var amount int64
	err := ew.db.QueryRow(ctx, `
		SELECT rc.id, COALESCE(rc.upi_error_code, 'UNKNOWN'), rc.revenue_at_risk
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		WHERE p.razorpay_payment_id = $1
		  AND rc.status = 'in_progress'
		LIMIT 1
	`, razorpayPaymentID).Scan(&caseID, &errorCode, &amount)
	
	if err != nil {
		return nil, fmt.Errorf("load case details: %w", err)
	}

	// Update retry count immediately
	if err := ew.mockRetry.UpdateRetryCount(ctx, caseID); err != nil {
		slog.Error("failed to update retry count", "case_id", caseID, "error", err)
	}

	// Simulate the retry with realistic latency and success rates
	result, err := ew.mockRetry.SimulateRetry(ctx, razorpayPaymentID, errorCode, amount)
	if err != nil {
		return nil, fmt.Errorf("mock retry failed: %w", err)
	}

	// If retry succeeded, publish payment.captured webhook
	if result.Success {
		slog.Info("retry succeeded - publishing payment.captured webhook",
			"payment_id", razorpayPaymentID,
			"case_id", caseID,
			"amount", amount,
		)
		
		// Publish webhook asynchronously (simulates Razorpay sending webhook)
		go ew.publishSuccessWebhook(razorpayPaymentID, amount, errorCode)
		
		return map[string]interface{}{
			"action":             "retry",
			"payment_id":         razorpayPaymentID,
			"amount_recovered":   amount,
			"partial_recovery":   false,
			"retry_duration_ms":  result.RetryDuration,
			"razorpay_response":  result.RazorpayResponse,
		}, nil
	}
	
	// Retry failed - log and return
	slog.Info("retry failed - payment still failed",
		"payment_id", razorpayPaymentID,
		"case_id", caseID,
		"error_code", errorCode,
	)
	
	return map[string]interface{}{
		"action":             "retry",
		"payment_id":         razorpayPaymentID,
		"amount_recovered":   int64(0),
		"partial_recovery":   false,
		"retry_duration_ms":  result.RetryDuration,
		"retry_failed":       true,
		"razorpay_response":  result.RazorpayResponse,
	}, nil
}

// publishSuccessWebhook simulates Razorpay sending a payment.captured webhook
// This is called when our mock retry succeeds
func (ew *ExecutionWorker) publishSuccessWebhook(paymentID string, amount int64, originalErrorCode string) {
	// Small delay to simulate webhook delivery latency
	time.Sleep(1 * time.Second)
	
	// Compute HMAC signature for the webhook
	secret := ew.cfg.RazorpayWebhookSecret
	if secret == "" {
		secret = "recoverai_secret" // Fallback for dev
	}
	
	webhookPayload := fmt.Sprintf(`{
		"entity": "event",
		"account_id": "acc_test",
		"event": "payment.captured",
		"contains": ["payment"],
		"payload": {
			"payment": {
				"entity": {
					"id": "%s",
					"amount": %d,
					"currency": "INR",
					"status": "captured",
					"method": "upi",
					"description": "Recovered payment (originally failed with %s)",
					"email": "recovered@example.com",
					"contact": "+919876543210",
					"created_at": %d
				}
			}
		},
		"created_at": %d
	}`, paymentID, amount, originalErrorCode, time.Now().Unix(), time.Now().Unix())
	
	// Compute signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(webhookPayload))
	signature := hex.EncodeToString(mac.Sum(nil))
	
	// Send webhook to our own API (use Docker service name, not localhost)
	apiURL := "http://api:8080/webhooks/razorpay"
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer([]byte(webhookPayload)))
	if err != nil {
		slog.Error("failed to create webhook request", "error", err)
		return
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Razorpay-Signature", signature)
	req.Header.Set("X-Razorpay-Event-Id", fmt.Sprintf("evt_recovery_%s_%d", paymentID, time.Now().Unix()))
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("failed to send success webhook", "error", err, "payment_id", paymentID)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		slog.Error("success webhook returned non-200", "status", resp.StatusCode, "payment_id", paymentID)
		return
	}
	
	slog.Info("success webhook published", "payment_id", paymentID, "amount", amount)
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
	metadata, _ := json.Marshal(map[string]interface{}{
		"reason":     reason,
		"action":     action,
		"timestamp":  time.Now().Format(time.RFC3339),
		"service":    "execution_worker",
	})
	ew.db.Exec(ctx, `
		INSERT INTO audit_logs (entity_type, entity_id, actor, action, metadata)
		VALUES ('recovery_case', $1, 'execution_worker', $2, $3)
	`, caseID, action, metadata)
}

func (ew *ExecutionWorker) publishResult(ctx context.Context, result *RecoveryResultMessage) error {
	payload, _ := json.Marshal(result)
	return ew.producer.Publish(ctx, "recovery.results", result.CaseID, payload)
}

// publishWebSocketEvent publishes an audit event to the WebSocket events topic
func (ew *ExecutionWorker) publishWebSocketEvent(ctx context.Context, caseID, paymentID, actor, action string, metadata map[string]interface{}) {
	event := map[string]interface{}{
		"type":       "audit_event",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"case_id":    caseID,
		"payment_id": paymentID,
		"data": map[string]interface{}{
			"actor":    actor,
			"action":   action,
			"metadata": metadata,
		},
	}

	payload, err := json.Marshal(event)
	if err != nil {
		slog.Error("execution worker: failed to marshal websocket event", "error", err)
		return
	}

	// Fire and forget - don't block on WebSocket publishing
	go func() {
		publishCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		if err := ew.producer.Publish(publishCtx, kafkapkg.TopicWebSocketEvents, caseID, payload); err != nil {
			slog.Error("execution worker: failed to publish websocket event", "error", err)
		}
	}()
}

// publishMetricUpdate triggers a metric update broadcast
func (ew *ExecutionWorker) publishMetricUpdate(ctx context.Context) {
	event := map[string]interface{}{
		"type":      "metric_update",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	go func() {
		publishCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		ew.producer.Publish(publishCtx, kafkapkg.TopicWebSocketEvents, "metrics", payload)
	}()
}
