package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	kafkapkg "recoverai/internal/kafka"
	redisclient "recoverai/internal/redis"
	"recoverai/internal/services"
	"recoverai/internal/validator"
)

// ValidatorConsumer consumes from "payment.risk_scored", runs pre-recovery validation,
// calls AI service if validation passes, and publishes commands to "payment.ai_commands".
type ValidatorConsumer struct {
	db        *pgxpool.Pool
	redis     *redisclient.Client
	producer  *kafkapkg.Producer
	cfg       *config.Config
	validator *validator.Validator
	aiClient  *services.AIClient
}

func NewValidatorConsumer(db *pgxpool.Pool, redis *redisclient.Client, producer *kafkapkg.Producer, cfg *config.Config) *ValidatorConsumer {
	return &ValidatorConsumer{
		db:        db,
		redis:     redis,
		producer:  producer,
		cfg:       cfg,
		validator: validator.NewValidator(db, redis, cfg),
		aiClient:  services.NewAIClient(),
	}
}

// Run starts the Kafka consumer loop for "payment.risk_scored".
func (vc *ValidatorConsumer) Run(ctx context.Context) error {
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":    vc.cfg.KafkaBrokers,
		"group.id":             "validator-consumer-group",
		"auto.offset.reset":    "earliest",
		"enable.auto.commit":   false,
		"max.poll.interval.ms": 300000,
	})
	if err != nil {
		return fmt.Errorf("create kafka consumer: %w", err)
	}
	defer consumer.Close()

	if err := consumer.Subscribe(kafkapkg.TopicRiskScored, nil); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	slog.Info("validator consumer: started", "topic", kafkapkg.TopicRiskScored, "group", "validator-consumer-group")

	for {
		select {
		case <-ctx.Done():
			slog.Info("validator consumer: shutting down")
			return nil
		default:
		}

		msg, err := consumer.ReadMessage(100 * time.Millisecond)
		if err != nil {
			if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrTimedOut {
				continue
			}
			slog.Error("validator consumer: read error", "error", err)
			continue
		}

		if err := vc.processRiskEvent(ctx, msg.Value); err != nil {
			slog.Error("validator consumer: process failed", "error", err)
		}

		consumer.CommitMessage(msg)
	}
}

// processRiskEvent runs validation and AI analysis for a recovery case.
func (vc *ValidatorConsumer) processRiskEvent(ctx context.Context, payload []byte) error {
	var event RevenueRiskEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	slog.Info("validator consumer: processing case",
		"payment_id", event.PaymentID,
		"amount", event.Amount,
		"error_code", event.UPIErrorCode,
	)

	// Get recovery case ID from the payment
	var caseID uuid.UUID
	err := vc.db.QueryRow(ctx, `
		SELECT rc.id 
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		WHERE p.razorpay_payment_id = $1
		ORDER BY rc.created_at DESC
		LIMIT 1
	`, event.PaymentID).Scan(&caseID)
	if err != nil {
		return fmt.Errorf("lookup case ID: %w", err)
	}

	// Step 1: Run Pre-Recovery Validation (6 checks)
	validationResult, err := vc.validator.Validate(ctx, caseID)
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	// Audit validation result
	vc.auditLog(ctx, caseID.String(), "validation_completed", 
		fmt.Sprintf("passed=%v, reason=%s", validationResult.Passed, validationResult.SkipReason))

	if !validationResult.Passed {
		slog.Info("validator consumer: validation failed",
			"case_id", caseID,
			"reason", validationResult.SkipReason,
		)
		
		// Publish WebSocket event for validator blocked
		vc.publishWebSocketEvent(ctx, caseID.String(), event.PaymentID, "validator", "validator_blocked", map[string]interface{}{
			"reason": validationResult.SkipReason,
		})
		
		// Publish to recovery.blocked topic for analytics
		vc.publishBlocked(ctx, caseID.String(), event.PaymentID, validationResult.SkipReason)
		
		// Case already updated by validator
		return nil
	}

	// Publish WebSocket event for validator passed
	vc.publishWebSocketEvent(ctx, caseID.String(), event.PaymentID, "validator", "validator_passed", map[string]interface{}{})

	// Step 2: Call AI Service for strategy recommendation
	slog.Info("validator consumer: calling AI service", "case_id", caseID)

	aiRequest := services.AnalyzeRequest{
		PaymentID:         event.PaymentID,
		CaseID:            caseID.String(),
		AmountPaise:       event.Amount,
		UPIErrorCode:      event.UPIErrorCode,
		UPIErrorCategory:  event.UPIErrorCategory,
		FailureType:       event.FailureType,
		FailureReason:     "", // Not in RevenueRiskEvent
		TimeOfFailureHour: time.Now().Hour(),
		ForcePaymentLink:  validationResult.ForcePaymentLink,
		CustomerHistory: services.CustomerHistory{
			SuccessfulPayments:  event.CustomerHistory.SuccessfulPayments,
			FailedPayments:      event.CustomerHistory.FailedPayments,
			LifetimeValuePaise:  event.CustomerHistory.LifetimeValue,
		},
		RiskScore: event.RiskScore,
		Priority:  event.Priority,
		MerchantPolicy: services.MerchantPolicy{
			MaxRetryAmountPaise:    int64(vc.cfg.MaxRetryAttempts * 100000), // placeholder
			MaxRetries:             vc.cfg.MaxRetryAttempts,
			RetryCooldownMinutes:   vc.cfg.RetryWindowMinutes,
			RequireHumanAbovePaise: vc.cfg.HighValueThreshold,
			AllowedActions:         []string{"retry", "payment_link", "notify", "escalate"},
		},
	}

	aiResponse, err := vc.aiClient.Analyze(ctx, aiRequest)
	if err != nil {
		slog.Error("validator consumer: AI call failed", "error", err, "case_id", caseID)

		// Update case with error
		vc.db.Exec(ctx, `
			UPDATE recovery_cases
			SET status = 'failed',
			    updated_at = NOW()
			WHERE id = $1
		`, caseID)

		return fmt.Errorf("AI analyze: %w", err)
	}

	slog.Info("validator consumer: AI response received",
		"case_id", caseID,
		"action", aiResponse.Action,
		"confidence", getConfidence(aiResponse.StrategyAssessment),
	)

	// Publish WebSocket events for AI analysis
	vc.publishWebSocketEvent(ctx, caseID.String(), event.PaymentID, "ai_agent", "ai_analyzed", map[string]interface{}{
		"failure_type":         event.FailureType,
		"recovery_probability": getConfidence(aiResponse.StrategyAssessment),
	})
	
	vc.publishWebSocketEvent(ctx, caseID.String(), event.PaymentID, "ai_agent", "ai_strategy_selected", map[string]interface{}{
		"strategy":   aiResponse.Action,
		"confidence": getConfidence(aiResponse.StrategyAssessment),
	})

	// Step 3: Store AI strategy in database
	aiStrategyJSON, _ := json.Marshal(aiResponse.StrategyAssessment)
	aiDiagnosisJSON, _ := json.Marshal(aiResponse.RiskAssessment)

	_, err = vc.db.Exec(ctx, `
		UPDATE recovery_cases
		SET ai_strategy = $1,
		    ai_diagnosis = $2,
		    recovery_probability = $3,
		    status = 'in_progress',
		    updated_at = NOW()
		WHERE id = $4
	`, aiStrategyJSON, aiDiagnosisJSON, getConfidence(aiResponse.StrategyAssessment), caseID)
	if err != nil {
		return fmt.Errorf("store AI strategy: %w", err)
	}

	// Audit AI decision
	vc.auditLog(ctx, caseID.String(), "ai_strategy_generated", 
		fmt.Sprintf("action=%s, confidence=%.2f", aiResponse.Action, getConfidence(aiResponse.StrategyAssessment)))

	// Step 4: Build and publish recovery command
	command := RecoveryCommandMessage{
		Action:                 aiResponse.Action,
		PaymentID:              event.PaymentID,
		CaseID:                 caseID.String(),
		ScheduledAtMinutes:     aiResponse.ScheduledAtMinutes,
		Parameters:             aiResponse.Parameters,
		RiskAssessmentSummary:  aiResponse.RiskAssessment,
		StrategySummary:        aiResponse.StrategyAssessment,
	}

	// DEMO_MODE: Cap delays at 1 minute for smooth presentations
	if vc.cfg.DemoMode && command.ScheduledAtMinutes > 1 {
		slog.Info("DEMO_MODE: delay reduced to 1 minute",
			"case_id", caseID.String(),
			"original_delay", command.ScheduledAtMinutes,
		)
		command.ScheduledAtMinutes = 1
	}

	return vc.publishCommand(ctx, &command)
}

func (vc *ValidatorConsumer) publishCommand(ctx context.Context, cmd *RecoveryCommandMessage) error {
	payload, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}

	if err := vc.producer.Publish(ctx, kafkapkg.TopicAICommands, cmd.CaseID, payload); err != nil {
		return fmt.Errorf("publish to %s: %w", kafkapkg.TopicAICommands, err)
	}

	slog.Info("validator consumer: command published",
		"case_id", cmd.CaseID,
		"action", cmd.Action,
		"topic", kafkapkg.TopicAICommands,
	)

	return nil
}

func (vc *ValidatorConsumer) publishBlocked(ctx context.Context, caseID, paymentID, reason string) {
	blockedEvent := map[string]interface{}{
		"event_id":   uuid.New().String(),
		"case_id":    caseID,
		"payment_id": paymentID,
		"reason":     reason,
		"blocked_at": time.Now(),
	}
	payload, _ := json.Marshal(blockedEvent)
	
	if err := vc.producer.Publish(ctx, kafkapkg.TopicRecoveryBlocked, caseID, payload); err != nil {
		slog.Error("validator consumer: failed to publish blocked event", "error", err, "case_id", caseID)
	} else {
		slog.Info("validator consumer: published to recovery.blocked", "case_id", caseID, "reason", reason)
	}
}

func (vc *ValidatorConsumer) auditLog(ctx context.Context, caseID, action, reason string) {
	metadata, _ := json.Marshal(map[string]interface{}{
		"reason":     reason,
		"action":     action,
		"timestamp":  time.Now().Format(time.RFC3339),
		"service":    "validator_consumer",
	})
	vc.db.Exec(ctx, `
		INSERT INTO audit_logs (entity_type, entity_id, actor, action, metadata)
		VALUES ('recovery_case', $1, 'validator_consumer', $2, $3)
	`, caseID, action, metadata)
}

// getConfidence extracts confidence value from strategy assessment map
func getConfidence(strategyAssessment map[string]interface{}) float64 {
	if strategyAssessment == nil {
		return 0.0
	}
	if conf, ok := strategyAssessment["confidence"].(float64); ok {
		return conf
	}
	return 0.0
}

// publishWebSocketEvent publishes an audit event to the WebSocket events topic
func (vc *ValidatorConsumer) publishWebSocketEvent(ctx context.Context, caseID, paymentID, actor, action string, metadata map[string]interface{}) {
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
		slog.Error("validator consumer: failed to marshal websocket event", "error", err)
		return
	}

	// Fire and forget - don't block on WebSocket publishing
	go func() {
		publishCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		if err := vc.producer.Publish(publishCtx, kafkapkg.TopicWebSocketEvents, caseID, payload); err != nil {
			slog.Error("validator consumer: failed to publish websocket event", "error", err)
		}
	}()
}
