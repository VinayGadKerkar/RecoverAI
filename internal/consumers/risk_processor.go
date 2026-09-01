package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	kafkapkg "recoverai/internal/kafka"
	redisclient "recoverai/internal/redis"
)

// ─── UPI Error Code Taxonomy ──────────────────────────────────────────────────

// ErrorCategory classifies UPI errors into Technical Decline vs Business Decline.
type ErrorCategory string

const (
	ErrorCategoryTD      ErrorCategory = "TD" // Technical Decline — infrastructure, highly retryable
	ErrorCategoryBD      ErrorCategory = "BD" // Business Decline — customer/merchant side, strategy-dependent
	ErrorCategoryUnknown ErrorCategory = "unknown"
)

// FailureType is the granular classification of the failure root cause.
type FailureType string

const (
	// Technical Decline (TD) — infrastructure failures, high retry success rate
	FailureTypeTransientBankDebitFail FailureType = "transient_bank_debit_fail" // U30
	FailureTypeBankServerDown         FailureType = "bank_server_down"          // U28
	FailureTypeBankLoadBlock          FailureType = "bank_load_block"           // RB
	FailureTypeBeneficiaryTimeout     FailureType = "beneficiary_timeout"       // BT

	// Business Decline (BD) — customer/merchant constraint, needs strategic handling
	FailureTypeInsufficientBalance    FailureType = "insufficient_balance"      // U16
	FailureTypeInsufficientFunds      FailureType = "insufficient_funds"        // Z9
	FailureTypePerTxLimitExceeded     FailureType = "per_tx_limit_exceeded"     // Z8
	FailureTypeVelocityLimit          FailureType = "velocity_limit"            // Z7
	FailureTypeTxNotPermitted         FailureType = "tx_not_permitted"          // U68
	FailureTypeRiskThresholdExceeded  FailureType = "risk_threshold_exceeded"   // YG
	FailureTypeCollectRequestExpired  FailureType = "collect_request_expired"   // U69

	FailureTypeUnknown FailureType = "unknown"
)

// classifyUPIError maps UPI error code to category + failure type.
func classifyUPIError(code string) (ErrorCategory, FailureType) {
	switch code {
	// Technical Decline (TD)
	case "U30":
		return ErrorCategoryTD, FailureTypeTransientBankDebitFail
	case "U28":
		return ErrorCategoryTD, FailureTypeBankServerDown
	case "RB":
		return ErrorCategoryTD, FailureTypeBankLoadBlock
	case "BT":
		return ErrorCategoryTD, FailureTypeBeneficiaryTimeout

	// Business Decline (BD)
	case "U16":
		return ErrorCategoryBD, FailureTypeInsufficientBalance
	case "Z9":
		return ErrorCategoryBD, FailureTypeInsufficientFunds
	case "Z8":
		return ErrorCategoryBD, FailureTypePerTxLimitExceeded
	case "Z7":
		return ErrorCategoryBD, FailureTypeVelocityLimit
	case "U68":
		return ErrorCategoryBD, FailureTypeTxNotPermitted
	case "YG":
		return ErrorCategoryBD, FailureTypeRiskThresholdExceeded
	case "U69":
		return ErrorCategoryBD, FailureTypeCollectRequestExpired

	default:
		return ErrorCategoryUnknown, FailureTypeUnknown
	}
}

// ─── Risk Scoring ─────────────────────────────────────────────────────────────

// computeRiskScore calculates a composite risk score for recovery prioritization.
// Score = amount_multiplier × customer_value_multiplier × failure_type_multiplier
func computeRiskScore(amount int64, customerSuccessfulPayments int, category ErrorCategory, failureType FailureType) float64 {
	// Amount multiplier
	var amountMul float64
	switch {
	case amount > 5000000: // > ₹50K
		amountMul = 2.0
	case amount > 1000000: // > ₹10K
		amountMul = 1.5
	case amount > 50000: // > ₹500
		amountMul = 1.0
	default:
		amountMul = 0.5
	}

	// Customer value multiplier
	var customerMul float64
	switch {
	case customerSuccessfulPayments > 5:
		customerMul = 1.5
	case customerSuccessfulPayments > 2:
		customerMul = 1.2
	case customerSuccessfulPayments > 0:
		customerMul = 1.0
	default: // new customer
		customerMul = 0.7
	}

	// Failure type multiplier
	var failureMul float64
	if category == ErrorCategoryTD {
		failureMul = 1.4 // Technical failures have high recovery chance
	} else if category == ErrorCategoryBD {
		switch failureType {
		case FailureTypeInsufficientBalance, FailureTypeInsufficientFunds:
			failureMul = 0.9
		case FailureTypeVelocityLimit, FailureTypePerTxLimitExceeded:
			failureMul = 0.6
		case FailureTypeRiskThresholdExceeded:
			failureMul = 0.2 // YG almost never auto-recoverable
		default:
			failureMul = 0.8
		}
	} else {
		failureMul = 0.7 // unknown
	}

	return amountMul * customerMul * failureMul
}

// scoreToPriority converts a numeric score to a priority label.
func scoreToPriority(score float64) string {
	switch {
	case score > 1.5:
		return "critical"
	case score > 0.9:
		return "high"
	case score > 0.5:
		return "medium"
	default:
		return "low"
	}
}

// ─── Bank Outage Detection ────────────────────────────────────────────────────

// detectBankOutage increments the failure counter for this error code and checks if threshold is crossed.
// Returns true if an outage is active (either newly detected or already flagged).
func detectBankOutage(ctx context.Context, redis *redisclient.Client, db *pgxpool.Pool, upiErrorCode string, threshold int) (bool, error) {
	if upiErrorCode == "" {
		return false, nil
	}

	// 5-minute bucket key: "bank_failures:U30:12345" where 12345 = unix / 300
	bucket := time.Now().Unix() / 300
	countKey := fmt.Sprintf("bank_failures:%s:%d", upiErrorCode, bucket)

	// Increment and set expiry (10 minutes — covers 2 buckets for overlap)
	count, err := redis.Incr(ctx, countKey)
	if err != nil {
		return false, fmt.Errorf("redis incr: %w", err)
	}
	if count == 1 {
		redis.Expire(ctx, countKey, 10*time.Minute)
	}

	// Check if outage flag already exists (previous detection)
	outageKey := fmt.Sprintf("bank_outage:%s", upiErrorCode)
	exists, err := redis.Exists(ctx, outageKey)
	if err != nil {
		slog.Warn("risk: failed to check outage flag", "error", err)
	}
	if exists {
		return true, nil // outage already active
	}

	// Threshold crossed — flag a new outage
	if count >= int64(threshold) {
		slog.Warn("bank outage detected",
			"upi_error_code", upiErrorCode,
			"failure_count", count,
			"window_minutes", 5,
		)

		// Set outage flag with 1-hour TTL
		if err := redis.Set(ctx, outageKey, "1", 1*time.Hour); err != nil {
			slog.Error("risk: failed to set outage flag", "error", err)
		}

		// Persist to bank_outage_events table
		_, err := db.Exec(ctx, `
			INSERT INTO bank_outage_events (upi_error_code, detected_at, failure_count, window_minutes)
			VALUES ($1, NOW(), $2, 5)
		`, upiErrorCode, count)
		if err != nil {
			slog.Error("risk: failed to persist outage event", "error", err)
		}

		return true, nil
	}

	return false, nil
}

// ─── Kafka Event Structures ───────────────────────────────────────────────────

// KafkaPaymentEvent is consumed from "payment.events".
type KafkaPaymentEvent struct {
	EventID         string                 `json:"event_id"`
	RazorpayEventID string                 `json:"razorpay_event_id"`
	EventType       string                 `json:"event_type"`
	PaymentID       string                 `json:"payment_id"`
	OrderID         string                 `json:"order_id"`
	Amount          int64                  `json:"amount"`
	Currency        string                 `json:"currency"`
	Status          string                 `json:"status"`
	Method          string                 `json:"method"`
	ErrorCode       string                 `json:"error_code"`
	ErrorDescription string                `json:"error_description"`
	Bank            string                 `json:"bank"`
	VPA             string                 `json:"vpa"`
	Email           string                 `json:"email"`
	Contact         string                 `json:"contact"`
	RawPayload      map[string]interface{} `json:"raw_payload"`
	ReceivedAt      time.Time              `json:"received_at"`
}

// RevenueRiskEvent is produced to "payment.risk_scored".
type RevenueRiskEvent struct {
	EventID         string    `json:"event_id"`
	PaymentID       string    `json:"payment_id"`
	MerchantID      string    `json:"merchant_id"`
	CustomerID      string    `json:"customer_id"`
	Amount          int64     `json:"amount"`
	Method          string    `json:"method"`
	UPIErrorCode    string    `json:"upi_error_code"`
	UPIErrorCategory string   `json:"upi_error_category"` // TD | BD | unknown
	FailureType     string    `json:"failure_type"`
	CustomerHistory struct {
		SuccessfulPayments int       `json:"successful_payments"`
		FailedPayments     int       `json:"failed_payments"`
		LifetimeValue      int64     `json:"lifetime_value"`
		LastPaymentAt      time.Time `json:"last_payment_at"`
	} `json:"customer_history"`
	RiskScore            float64    `json:"risk_score"`
	Priority             string     `json:"priority"`
	BankOutageDetected   bool       `json:"bank_outage_detected"`
	BatchRetryAt         *time.Time `json:"batch_retry_at,omitempty"`
	ProcessedAt          time.Time  `json:"processed_at"`
}

// ─── Risk Processor Consumer ──────────────────────────────────────────────────

type RiskProcessor struct {
	db       *pgxpool.Pool
	redis    *redisclient.Client
	producer *kafkapkg.Producer
	cfg      *config.Config
}

func NewRiskProcessor(db *pgxpool.Pool, redis *redisclient.Client, producer *kafkapkg.Producer, cfg *config.Config) *RiskProcessor {
	return &RiskProcessor{
		db:       db,
		redis:    redis,
		producer: producer,
		cfg:      cfg,
	}
}

// Run starts the Kafka consumer loop for the "payment.events" topic.
func (rp *RiskProcessor) Run(ctx context.Context) error {
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":    rp.cfg.KafkaBrokers,
		"group.id":             "risk-processor-group",
		"auto.offset.reset":    "earliest",
		"enable.auto.commit":   false, // manual offset management
		"max.poll.interval.ms": 300000,
		"session.timeout.ms":   30000,
	})
	if err != nil {
		return fmt.Errorf("create kafka consumer: %w", err)
	}
	defer consumer.Close()

	if err := consumer.Subscribe("payment.events", nil); err != nil {
		return fmt.Errorf("subscribe to payment.events: %w", err)
	}

	slog.Info("risk processor: consumer started", "topic", "payment.events", "group", "risk-processor-group")

	for {
		select {
		case <-ctx.Done():
			slog.Info("risk processor: shutting down")
			return nil
		default:
		}

		msg, err := consumer.ReadMessage(100 * time.Millisecond)
		if err != nil {
			if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrTimedOut {
				continue
			}
			slog.Error("risk processor: read error", "error", err)
			continue
		}

		// Process message with exponential backoff
		if err := rp.processMessageWithRetry(ctx, msg); err != nil {
			slog.Error("risk processor: message processing failed after retries",
				"error", err,
				"partition", msg.TopicPartition.Partition,
				"offset", msg.TopicPartition.Offset,
			)
			// TODO: publish to dead-letter queue
		}

		// Manual offset commit after successful processing
		if _, err := consumer.CommitMessage(msg); err != nil {
			slog.Error("risk processor: offset commit failed", "error", err)
		}
	}
}

// processMessageWithRetry attempts to process a message with exponential backoff (max 3 retries).
func (rp *RiskProcessor) processMessageWithRetry(ctx context.Context, msg *kafka.Message) error {
	var lastErr error
	backoff := 1 * time.Second

	for attempt := 1; attempt <= 3; attempt++ {
		if err := rp.processMessage(ctx, msg.Value); err != nil {
			lastErr = err
			slog.Warn("risk processor: retry attempt",
				"attempt", attempt,
				"error", err,
				"backoff_seconds", backoff.Seconds(),
			)
			time.Sleep(backoff)
			backoff *= 2 // exponential: 1s, 2s, 4s
		} else {
			return nil // success
		}
	}

	return fmt.Errorf("failed after 3 retries: %w", lastErr)
}

// processMessage processes a single payment event.
func (rp *RiskProcessor) processMessage(ctx context.Context, payload []byte) error {
	var event KafkaPaymentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	// Only process payment.failed events
	if event.EventType != "payment.failed" {
		return nil
	}

	slog.Info("risk processor: processing failed payment",
		"payment_id", event.PaymentID,
		"amount", event.Amount,
		"error_code", event.ErrorCode,
	)

	// Step 1: Classify UPI error
	category, failureType := classifyUPIError(event.ErrorCode)

	// Step 2: Load customer history (if customer exists), fallback to mock insert
	customerHistory, customerID, merchantID, err := rp.loadCustomerHistory(ctx, event)
	if err != nil {
		return fmt.Errorf("load customer history: %w", err)
	}

	// Step 3: Detect bank outage
	outageThreshold := rp.cfg.OutageDetectionThreshold
	if outageThreshold == 0 {
		outageThreshold = 10 // default
	}
	bankOutageDetected, err := detectBankOutage(ctx, rp.redis, rp.db, event.ErrorCode, outageThreshold)
	if err != nil {
		slog.Warn("risk processor: bank outage detection failed", "error", err)
	}

	// Step 4: Compute risk score (skip if outage — batch processing takes priority)
	var riskScore float64
	var priority string
	var batchRetryAt *time.Time

	if bankOutageDetected {
		// Outage mode: skip individual scoring, set batch retry time
		riskScore = 0
		priority = "low"
		retryTime := time.Now().Add(60 * time.Minute)
		batchRetryAt = &retryTime
	} else {
		riskScore = computeRiskScore(event.Amount, customerHistory.SuccessfulPayments, category, failureType)
		priority = scoreToPriority(riskScore)
	}

	// Step 5: Create recovery_case record
	caseID, err := rp.createRecoveryCase(ctx, event, merchantID, customerID, string(category), string(failureType), riskScore, priority, bankOutageDetected, batchRetryAt)
	if err != nil {
		return fmt.Errorf("create recovery case: %w", err)
	}

	// Step 6: Publish to payment.risk_scored topic
	riskEvent := RevenueRiskEvent{
		EventID:            uuid.New().String(),
		PaymentID:          event.PaymentID,
		MerchantID:         merchantID,
		CustomerID:         customerID,
		Amount:             event.Amount,
		Method:             event.Method,
		UPIErrorCode:       event.ErrorCode,
		UPIErrorCategory:   string(category),
		FailureType:        string(failureType),
		RiskScore:          riskScore,
		Priority:           priority,
		BankOutageDetected: bankOutageDetected,
		BatchRetryAt:       batchRetryAt,
		ProcessedAt:        time.Now(),
	}
	riskEvent.CustomerHistory.SuccessfulPayments = customerHistory.SuccessfulPayments
	riskEvent.CustomerHistory.FailedPayments = customerHistory.FailedPayments
	riskEvent.CustomerHistory.LifetimeValue = customerHistory.LifetimeValue
	riskEvent.CustomerHistory.LastPaymentAt = customerHistory.LastPaymentAt

	if err := rp.publishRevenueRisk(ctx, &riskEvent); err != nil {
		return fmt.Errorf("publish revenue risk: %w", err)
	}

	slog.Info("risk processor: event processed",
		"payment_id", event.PaymentID,
		"case_id", caseID,
		"risk_score", riskScore,
		"priority", priority,
		"bank_outage", bankOutageDetected,
	)

	return nil
}

// loadCustomerHistory fetches customer metadata from the payments table.
func (rp *RiskProcessor) loadCustomerHistory(ctx context.Context, event KafkaPaymentEvent) (struct {
	SuccessfulPayments int
	FailedPayments     int
	LifetimeValue      int64
	LastPaymentAt      time.Time
}, string, string, error) {
	var result struct {
		SuccessfulPayments int
		FailedPayments     int
		LifetimeValue      int64
		LastPaymentAt      time.Time
	}
	var customerID, merchantID string

	// Load payment and customer data
	err := rp.db.QueryRow(ctx, `
		SELECT 
			p.merchant_id,
			COALESCE(p.customer_id::text, ''),
			COALESCE(c.successful_payments, 0),
			COALESCE(c.failed_payments, 0),
			COALESCE(c.lifetime_value, 0),
			COALESCE(p.created_at, NOW())
		FROM payments p
		LEFT JOIN customers c ON c.id = p.customer_id
		WHERE p.razorpay_payment_id = $1
	`, event.PaymentID).Scan(
		&merchantID,
		&customerID,
		&result.SuccessfulPayments,
		&result.FailedPayments,
		&result.LifetimeValue,
		&result.LastPaymentAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			slog.Info("risk processor: payment not found, inserting mock payment (Payment Processor fallback)", "payment_id", event.PaymentID)
			accountID, _ := event.RawPayload["account_id"].(string)
			if accountID == "" {
				accountID = "acc_demo_merchant" // fallback for older test events
			}
			
			err2 := rp.db.QueryRow(ctx, `SELECT id FROM merchants WHERE razorpay_key_id = $1 LIMIT 1`, accountID).Scan(&merchantID)
			if err2 != nil {
				return result, "", "", fmt.Errorf("query merchant by account_id %s: %w", accountID, err2)
			}
			
			_, err3 := rp.db.Exec(ctx, `
				INSERT INTO payments (id, razorpay_payment_id, merchant_id, amount, status)
				VALUES (gen_random_uuid(), $1, $2, $3, 'failed')
				ON CONFLICT (razorpay_payment_id) DO NOTHING
			`, event.PaymentID, merchantID, event.Amount)
			
			if err3 != nil {
				return result, "", "", fmt.Errorf("insert mock payment: %w", err3)
			}
			
			// Mock customer history
			return result, "", merchantID, nil
		}
		return result, "", "", fmt.Errorf("query payment: %w", err)
	}

	return result, customerID, merchantID, nil
}

// createRecoveryCase inserts a new recovery case record.
func (rp *RiskProcessor) createRecoveryCase(
	ctx context.Context,
	event KafkaPaymentEvent,
	merchantID, customerID string,
	category, failureType string,
	riskScore float64,
	priority string,
	bankOutageDetected bool,
	batchRetryAt *time.Time,
) (string, error) {
	caseID := uuid.New().String()

	status := "open"
	if bankOutageDetected {
		status = "outage_batched"
	}

	var customerUUID *uuid.UUID
	if customerID != "" {
		parsed, err := uuid.Parse(customerID)
		if err == nil {
			customerUUID = &parsed
		}
	}

	merchantUUID, _ := uuid.Parse(merchantID)

	// Lookup payment UUID from razorpay_payment_id
	var paymentUUID uuid.UUID
	err := rp.db.QueryRow(ctx, `SELECT id FROM payments WHERE razorpay_payment_id = $1`, event.PaymentID).Scan(&paymentUUID)
	if err != nil {
		return "", fmt.Errorf("lookup payment uuid: %w", err)
	}

	_, err = rp.db.Exec(ctx, `
		INSERT INTO recovery_cases (
			id, merchant_id, payment_id, customer_id, status,
			revenue_at_risk, recovery_probability, recovery_roi, priority,
			failure_type, upi_error_code, upi_error_category,
			bank_outage_detected, cooldown_until
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`,
		caseID,
		merchantUUID,
		paymentUUID,
		customerUUID,
		status,
		event.Amount,
		riskScore,
		0, // ROI computed later by AI
		priority,
		failureType,
		event.ErrorCode,
		category,
		bankOutageDetected,
		batchRetryAt,
	)

	return caseID, err
}

// publishRevenueRisk publishes a RevenueRiskEvent to the "payment.risk_scored" topic.
func (rp *RiskProcessor) publishRevenueRisk(ctx context.Context, event *RevenueRiskEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	return rp.producer.Publish(ctx, kafkapkg.TopicRiskScored, event.PaymentID, payload)
}
