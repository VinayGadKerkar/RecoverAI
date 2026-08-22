package models

import (
	"time"
)

// ─── Payment & Webhook ───────────────────────────────────────────────────────

// PaymentStatus mirrors Razorpay's payment lifecycle states.
type PaymentStatus string

const (
	PaymentStatusCreated    PaymentStatus = "created"
	PaymentStatusAuthorized PaymentStatus = "authorized"
	PaymentStatusCaptured   PaymentStatus = "captured"
	PaymentStatusFailed     PaymentStatus = "failed"
	PaymentStatusRefunded   PaymentStatus = "refunded"
)

// PaymentMethod classifies the payment instrument.
type PaymentMethod string

const (
	PaymentMethodUPI     PaymentMethod = "upi"
	PaymentMethodCard    PaymentMethod = "card"
	PaymentMethodNetBank PaymentMethod = "netbanking"
	PaymentMethodWallet  PaymentMethod = "wallet"
)

// RazorpayWebhookPayload is the raw event received from Razorpay.
type RazorpayWebhookPayload struct {
	Entity    string         `json:"entity"`
	AccountID string         `json:"account_id"`
	Event     string         `json:"event"`
	Contains  []string       `json:"contains"`
	Payload   WebhookContent `json:"payload"`
	CreatedAt int64          `json:"created_at"`
}

type WebhookContent struct {
	Payment *PaymentEntity `json:"payment,omitempty"`
	Order   *OrderEntity   `json:"order,omitempty"`
}

type PaymentEntity struct {
	Entity struct {
		ID              string        `json:"id"`
		Amount          int64         `json:"amount"` // in paise
		Currency        string        `json:"currency"`
		Status          PaymentStatus `json:"status"`
		OrderID         string        `json:"order_id"`
		MerchantID      string        `json:"merchant_id"`
		Method          PaymentMethod `json:"method"`
		ErrorCode       string        `json:"error_code"`
		ErrorDescription string       `json:"error_description"`
		ErrorSource     string        `json:"error_source"`
		ErrorStep       string        `json:"error_step"`
		ErrorReason     string        `json:"error_reason"`
		Bank            string        `json:"bank"`
		VPA             string        `json:"vpa"` // UPI VPA
		CardID          string        `json:"card_id"`
		Description     string        `json:"description"`
		CreatedAt       int64         `json:"created_at"`
		Email           string        `json:"email"`
		Contact         string        `json:"contact"`
	} `json:"entity"`
}

type OrderEntity struct {
	Entity struct {
		ID       string `json:"id"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Receipt  string `json:"receipt"`
	} `json:"entity"`
}

// ─── UPI Error Codes ──────────────────────────────────────────────────────────

// UPIErrorCode represents a classified UPI failure code.
type UPIErrorCode string

const (
	UPIErrorU16 UPIErrorCode = "U16" // Debit failed – insufficient balance → send payment link + notify (High)
	UPIErrorU30 UPIErrorCode = "U30" // Debit failed – payer account issue → retry after 10 min (Medium)
	UPIErrorZ9  UPIErrorCode = "Z9"  // Transaction declined → escalate to merchant (Low)
	UPIErrorU68 UPIErrorCode = "U68" // Transaction not permitted → alternate payment link (Medium)
	UPIErrorRB  UPIErrorCode = "RB"  // Request blocked by bank → retry after 30 min (High)
	UPIErrorYG  UPIErrorCode = "YG"  // Risk threshold exceeded → human approval required (Low)
)

// ─── Risk & Scoring ───────────────────────────────────────────────────────────

// RiskLevel classifies the risk tier of a payment failure.
type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelCritical RiskLevel = "critical"
)

// RiskScore is the output of Stage 2 (Risk Engine).
type RiskScore struct {
	PaymentID       string       `json:"payment_id"`
	Score           float64      `json:"score"` // 0.0 – 1.0
	Level           RiskLevel    `json:"level"`
	RecoveryChance  float64      `json:"recovery_chance"` // 0.0 – 1.0
	UPIErrorCode    UPIErrorCode `json:"upi_error_code,omitempty"`
	BankOutage      bool         `json:"bank_outage"`
	OutageBank      string       `json:"outage_bank,omitempty"`
	Factors         []string     `json:"factors"`
	ScoredAt        time.Time    `json:"scored_at"`
}

// ─── Recovery ─────────────────────────────────────────────────────────────────

// RecoveryStatus tracks the state of a recovery attempt.
type RecoveryStatus string

const (
	RecoveryStatusPending    RecoveryStatus = "pending"
	RecoveryStatusQueued     RecoveryStatus = "queued"
	RecoveryStatusInProgress RecoveryStatus = "in_progress"
	RecoveryStatusSucceeded  RecoveryStatus = "succeeded"
	RecoveryStatusFailed     RecoveryStatus = "failed"
	RecoveryStatusAborted    RecoveryStatus = "aborted"
	RecoveryStatusEscalated  RecoveryStatus = "escalated"
)

// RecoveryAction is the type of action the Policy Engine will execute.
type RecoveryAction string

const (
	RecoveryActionRetry          RecoveryAction = "retry"
	RecoveryActionPaymentLink    RecoveryAction = "payment_link"
	RecoveryActionNotifyCustomer RecoveryAction = "notify_customer"
	RecoveryActionEscalate       RecoveryAction = "escalate"
	RecoveryActionAbort          RecoveryAction = "abort"
	RecoveryActionWait           RecoveryAction = "wait"
)

// AICommand is the structured JSON produced by the AI service.
// The AI NEVER executes — it only produces this struct.
type AICommand struct {
	PaymentID        string         `json:"payment_id"`
	RecommendedAction RecoveryAction `json:"recommended_action"`
	WaitMinutes      int            `json:"wait_minutes,omitempty"`
	Rationale        string         `json:"rationale"`
	Confidence       float64        `json:"confidence"` // 0.0 – 1.0
	AlternateAction  RecoveryAction `json:"alternate_action,omitempty"`
	NotifyCustomer   bool           `json:"notify_customer"`
	MessageTemplate  string         `json:"message_template,omitempty"`
	RequiresApproval bool           `json:"requires_approval"`
	Diagnosis        string         `json:"diagnosis"`
	GeneratedAt      time.Time      `json:"generated_at"`
}

// ─── Pre-Recovery Validator ───────────────────────────────────────────────────

// ValidationResult is the output of the 6-check gate (Stage 3).
type ValidationResult struct {
	PaymentID string           `json:"payment_id"`
	Passed    bool             `json:"passed"`
	Checks    []ValidationCheck `json:"checks"`
	BlockedBy string           `json:"blocked_by,omitempty"`
	CheckedAt time.Time        `json:"checked_at"`
}

type ValidationCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Reason  string `json:"reason,omitempty"`
}

// ─── Kafka Events ─────────────────────────────────────────────────────────────

// KafkaPaymentEvent is published to TopicPaymentEvents after webhook ingestion.
type KafkaPaymentEvent struct {
	EventID    string                 `json:"event_id"`
	EventType  string                 `json:"event_type"`
	PaymentID  string                 `json:"payment_id"`
	MerchantID string                 `json:"merchant_id"`
	Amount     int64                  `json:"amount"`
	Currency   string                 `json:"currency"`
	Status     PaymentStatus          `json:"status"`
	Method     PaymentMethod          `json:"method"`
	ErrorCode  string                 `json:"error_code"`
	Bank       string                 `json:"bank"`
	VPA        string                 `json:"vpa"`
	RawPayload map[string]interface{} `json:"raw_payload"`
	ReceivedAt time.Time              `json:"received_at"`
}

// KafkaRiskScoredEvent is published after Stage 2 scores a payment.
type KafkaRiskScoredEvent struct {
	EventID    string    `json:"event_id"`
	PaymentID  string    `json:"payment_id"`
	MerchantID string    `json:"merchant_id"`
	RiskScore  RiskScore `json:"risk_score"`
	ScoredAt   time.Time `json:"scored_at"`
}

// ─── Merchant ─────────────────────────────────────────────────────────────────

type Merchant struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	RazorpayID  string    `json:"razorpay_id"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// ─── Audit ────────────────────────────────────────────────────────────────────

// AuditLogEntry records every decision and action in the recovery pipeline.
type AuditLogEntry struct {
	ID          string         `json:"id"`
	PaymentID   string         `json:"payment_id"`
	MerchantID  string         `json:"merchant_id"`
	Stage       string         `json:"stage"`
	Action      string         `json:"action"`
	Actor       string         `json:"actor"` // "system" | "ai" | "policy_engine" | "human"
	Decision    string         `json:"decision"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}
