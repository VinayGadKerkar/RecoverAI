package policy

import (
	"fmt"
	"time"
)

// ─── Input Structures ─────────────────────────────────────────────────────────

// PolicyInput is the complete context passed to the Policy Engine.
type PolicyInput struct {
	Action                    string
	PaymentID                 string
	CaseID                    string
	Amount                    int64 // paise
	RetryCount                int
	UPIErrorCode              string
	UPIErrorCategory          string // "TD" | "BD" | "unknown"
	IsMandatePayment          bool
	RBIMinimumRetryAt         *time.Time
	CooldownUntil             *time.Time
	BankOutageDetected        bool
	ForcePaymentLink          bool
	CustomerSuccessfulPayments int
	MerchantPolicy            MerchantPolicy
}

// MerchantPolicy holds the merchant's recovery configuration.
type MerchantPolicy struct {
	MaxRetryAmountPaise      int64
	MaxRetries               int
	RetryCooldownMinutes     int
	RequireHumanAbovePaise   int64
	AllowedActions           []string
	HighValueThresholdPaise  int64 // RBI: ₹15,000 for mandate payments
}

// ─── Output Structure ─────────────────────────────────────────────────────────

// PolicyDecision is the deterministic output of the Policy Engine.
type PolicyDecision struct {
	Allowed               bool
	Reason                string
	RequiresHumanApproval bool
	RuleTriggered         string
}

// ─── Policy Engine ────────────────────────────────────────────────────────────

// Engine evaluates policy rules. NO randomness. NO AI. Purely deterministic.
type Engine struct{}

// NewEngine creates a new Policy Engine.
func NewEngine() *Engine {
	return &Engine{}
}

// Evaluate applies all 10 rules in order. First BLOCK wins.
func (e *Engine) Evaluate(input PolicyInput) PolicyDecision {
	// ─── Rule 1: Non-retryable UPI error codes ────────────────────────────────
	// Research-backed: Z9, YG, Z8, U68 should NEVER be retried directly.
	if input.Action == "RETRY_PAYMENT" {
		nonRetryable := []string{"Z9", "YG", "Z8", "U68"}
		for _, code := range nonRetryable {
			if input.UPIErrorCode == code {
				return PolicyDecision{
					Allowed:       false,
					Reason:        fmt.Sprintf("UPI code %s is non-retryable — no direct retry permitted", code),
					RuleTriggered: "rule1_non_retryable_upi",
				}
			}
		}
	}

	// ─── Rule 2: Force payment link override ──────────────────────────────────
	// Validator flagged this payment — respect the constraint.
	if input.ForcePaymentLink && input.Action == "RETRY_PAYMENT" {
		return PolicyDecision{
			Allowed:       false,
			Reason:        "Validator flagged: retry not permitted for this error type",
			RuleTriggered: "rule2_force_payment_link",
		}
	}

	// ─── Rule 3: Bank outage active ────────────────────────────────────────────
	// Payments during outage are batched — do not execute now.
	if input.BankOutageDetected && input.Action == "RETRY_PAYMENT" {
		return PolicyDecision{
			Allowed:       false,
			Reason:        "Bank outage active — retry batched, do not execute now",
			RuleTriggered: "rule3_bank_outage",
		}
	}

	// ─── Rule 4: RBI mandate minimum retry window ─────────────────────────────
	// RBI rule: 24 hours minimum between retries for recurring/mandate payments.
	if input.IsMandatePayment && input.RBIMinimumRetryAt != nil {
		if time.Now().Before(*input.RBIMinimumRetryAt) {
			return PolicyDecision{
				Allowed:       false,
				Reason:        fmt.Sprintf("RBI mandate rule: minimum 24h between retries not elapsed (retry allowed after %s)", input.RBIMinimumRetryAt.Format(time.RFC3339)),
				RuleTriggered: "rule4_rbi_mandate_window",
			}
		}
	}

	// ─── Rule 5: High-value mandate RBI approval ───────────────────────────────
	// RBI: mandate payments above ₹15,000 require explicit customer approval.
	if input.IsMandatePayment && input.Amount > input.MerchantPolicy.HighValueThresholdPaise {
		return PolicyDecision{
			Allowed:               false,
			Reason:                fmt.Sprintf("RBI: mandate amounts >₹%.2f require explicit customer approval", float64(input.MerchantPolicy.HighValueThresholdPaise)/100),
			RequiresHumanApproval: true,
			RuleTriggered:         "rule5_rbi_mandate_high_value",
		}
	}

	// ─── Rule 6: Amount ceiling for auto-retry ────────────────────────────────
	// Merchant-configurable limit — amounts above this need human review.
	if input.Action == "RETRY_PAYMENT" && input.Amount > input.MerchantPolicy.MaxRetryAmountPaise {
		return PolicyDecision{
			Allowed:               false,
			Reason:                fmt.Sprintf("Amount ₹%.2f exceeds auto-retry ceiling ₹%.2f — requires human approval", float64(input.Amount)/100, float64(input.MerchantPolicy.MaxRetryAmountPaise)/100),
			RequiresHumanApproval: true,
			RuleTriggered:         "rule6_retry_amount_ceiling",
		}
	}

	// ─── Rule 7: High value always needs human ────────────────────────────────
	// Universal rule: very high amounts always escalate.
	if input.Amount > input.MerchantPolicy.RequireHumanAbovePaise {
		return PolicyDecision{
			Allowed:               false,
			Reason:                fmt.Sprintf("Amount >₹%.2f requires human approval", float64(input.MerchantPolicy.RequireHumanAbovePaise)/100),
			RequiresHumanApproval: true,
			RuleTriggered:         "rule7_require_human_high_value",
		}
	}

	// ─── Rule 8: Max retries reached ───────────────────────────────────────────
	// Prevent infinite retry loops.
	if input.RetryCount >= input.MerchantPolicy.MaxRetries {
		return PolicyDecision{
			Allowed:       false,
			Reason:        fmt.Sprintf("Maximum retries (%d) reached", input.MerchantPolicy.MaxRetries),
			RuleTriggered: "rule8_max_retries",
		}
	}

	// ─── Rule 9: Cooldown active ───────────────────────────────────────────────
	// Rate-limiting per payment to avoid over-retrying.
	if input.CooldownUntil != nil && time.Now().Before(*input.CooldownUntil) {
		return PolicyDecision{
			Allowed:       false,
			Reason:        fmt.Sprintf("Cooldown active until %s", input.CooldownUntil.Format(time.RFC3339)),
			RuleTriggered: "rule9_cooldown_active",
		}
	}

	// ─── Rule 10: Action not in allowlist ──────────────────────────────────────
	// Merchant explicitly controls which actions are permitted.
	if !contains(input.MerchantPolicy.AllowedActions, actionToAllowlistName(input.Action)) {
		return PolicyDecision{
			Allowed:       false,
			Reason:        fmt.Sprintf("Action %s not in merchant's allowed actions", input.Action),
			RuleTriggered: "rule10_action_not_allowed",
		}
	}

	// ─── All rules passed → ALLOWED ────────────────────────────────────────────
	return PolicyDecision{
		Allowed:       true,
		Reason:        "All policy checks passed",
		RuleTriggered: "none",
	}
}

// actionToAllowlistName maps ExecutorCommand action names to merchant policy names.
func actionToAllowlistName(action string) string {
	switch action {
	case "RETRY_PAYMENT":
		return "retry"
	case "GENERATE_PAYMENT_LINK":
		return "payment_link"
	case "SEND_NOTIFICATION":
		return "notify"
	case "ESCALATE":
		return "escalate"
	case "STOP":
		return "stop"
	default:
		return ""
	}
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
