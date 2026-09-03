package policy

import (
	"fmt"
	"time"

	"recoverai/internal/economics"
)

// ─── Input Structures ─────────────────────────────────────────────────────────

// PolicyInput is the complete context passed to the Policy Engine.
type PolicyInput struct {
	Action                     string
	PaymentID                  string
	CaseID                     string
	Amount                     int64 // paise
	RetryCount                 int
	CaseMaxRetries             int // recovery_cases.max_retries; 0 → fall back to merchant policy
	UPIErrorCode               string
	UPIErrorCategory           string // "TD" | "BD" | "unknown"
	IsMandatePayment           bool
	RBIMinimumRetryAt          *time.Time
	CooldownUntil              *time.Time
	BankOutageDetected         bool
	ForcePaymentLink           bool
	RecoveryProbability        float64 // risk engine / AI estimate for attempt 1
	CustomerSuccessfulPayments int
	MerchantPolicy             MerchantPolicy
}

// MerchantPolicy holds the merchant's recovery configuration.
// Every money field is PAISE.
type MerchantPolicy struct {
	MaxRetryAmountPaise     int64
	MaxRetries              int
	RetryCooldownMinutes    int
	RequireHumanAbovePaise  int64
	AllowedActions          []string
	HighValueThresholdPaise int64 // RBI AFA limit for mandate payments (₹15,000 default)
	MinRecoveryROIPaise     int64 // recovery_policies.min_recovery_roi, in PAISE
}

// ─── Output Structure ─────────────────────────────────────────────────────────

// PolicyDecision is the deterministic output of the Policy Engine.
type PolicyDecision struct {
	Allowed               bool
	Reason                string
	RequiresHumanApproval bool
	RuleTriggered         string

	// Economics is the per-attempt cost/benefit breakdown for this action.
	// Always populated so the audit trail can show the number behind the call.
	Economics *economics.Result
}

// ─── Policy Engine ────────────────────────────────────────────────────────────

// Engine evaluates policy rules. NO randomness. NO AI. Purely deterministic.
type Engine struct {
	// Now is injectable for tests. Defaults to time.Now().UTC().
	Now func() time.Time
}

// NewEngine creates a new Policy Engine.
func NewEngine() *Engine {
	return &Engine{Now: func() time.Time { return time.Now().UTC() }}
}

func (e *Engine) now() time.Time {
	if e.Now == nil {
		return time.Now().UTC()
	}
	return e.Now()
}

// isMoneyMoving reports whether an action actually debits the customer.
// Rules about retry limits, cooldowns and approval ceilings apply ONLY to these.
// Applying them to ESCALATE or STOP deadlocks the case: a high-value failure
// would be blocked from escalating to the very human it needs, and blocked from
// being stopped.
func isMoneyMoving(action string) bool {
	return action == economics.ActionRetry
}

// isSafetyValve reports whether an action must always remain available.
// STOP is how a merchant halts recovery — no merchant configuration may
// disable it.
func isSafetyValve(action string) bool {
	return action == economics.ActionStop
}

// Evaluate applies all 11 rules in order. First BLOCK wins.
func (e *Engine) Evaluate(input PolicyInput) PolicyDecision {
	// ─── Rule 1: Non-retryable UPI error codes ────────────────────────────────
	// Sourced from internal/economics so this can never drift from the
	// validator's force_payment_link flag. (Z9, YG, Z8, U68 are non-retryable.)
	if input.Action == economics.ActionRetry && !economics.IsRetryable(input.UPIErrorCode) {
		return PolicyDecision{
			Allowed:       false,
			Reason:        fmt.Sprintf("UPI code %s is non-retryable — no direct retry permitted", input.UPIErrorCode),
			RuleTriggered: "rule1_non_retryable_upi",
		}
	}

	// ─── Rule 2: Force payment link override ──────────────────────────────────
	if input.ForcePaymentLink && input.Action == economics.ActionRetry {
		return PolicyDecision{
			Allowed:       false,
			Reason:        "Validator flagged: retry not permitted for this error type",
			RuleTriggered: "rule2_force_payment_link",
		}
	}

	// ─── Rule 3: Bank outage active ───────────────────────────────────────────
	if input.BankOutageDetected && input.Action == economics.ActionRetry {
		return PolicyDecision{
			Allowed:       false,
			Reason:        "Bank outage active — retry batched, do not execute now",
			RuleTriggered: "rule3_bank_outage",
		}
	}

	// ─── Rule 4: RBI mandate minimum retry window ─────────────────────────────
	// FAIL CLOSED on a nil timestamp: for a mandate payment, "we don't know when
	// the window opens" must never mean "go ahead".
	if input.IsMandatePayment && isMoneyMoving(input.Action) {
		if input.RBIMinimumRetryAt == nil {
			return PolicyDecision{
				Allowed:               false,
				Reason:                "RBI mandate rule: minimum retry window unknown (rbi_minimum_retry_at not set) — cannot debit",
				RequiresHumanApproval: true,
				RuleTriggered:         "rule4_rbi_mandate_window_unknown",
			}
		}
		if e.now().Before(*input.RBIMinimumRetryAt) {
			return PolicyDecision{
				Allowed: false,
				Reason: fmt.Sprintf("RBI mandate rule: minimum retry window not elapsed (retry allowed after %s)",
					input.RBIMinimumRetryAt.Format(time.RFC3339)),
				RuleTriggered: "rule4_rbi_mandate_window",
			}
		}
	}

	// ─── Rule 5: High-value mandate RBI approval ──────────────────────────────
	// Applies to debits only — escalating or stopping a high-value mandate must
	// stay possible.
	if input.IsMandatePayment && isMoneyMoving(input.Action) &&
		input.Amount > input.MerchantPolicy.HighValueThresholdPaise {
		return PolicyDecision{
			Allowed: false,
			Reason: fmt.Sprintf("RBI: mandate amounts above %s require explicit customer approval",
				economics.FormatPaise(input.MerchantPolicy.HighValueThresholdPaise)),
			RequiresHumanApproval: true,
			RuleTriggered:         "rule5_rbi_mandate_high_value",
		}
	}

	// ─── Rule 6: Amount ceiling for auto-retry ────────────────────────────────
	if isMoneyMoving(input.Action) && input.Amount > input.MerchantPolicy.MaxRetryAmountPaise {
		return PolicyDecision{
			Allowed: false,
			Reason: fmt.Sprintf("Amount %s exceeds auto-retry ceiling %s — requires human approval",
				economics.FormatPaise(input.Amount),
				economics.FormatPaise(input.MerchantPolicy.MaxRetryAmountPaise)),
			RequiresHumanApproval: true,
			RuleTriggered:         "rule6_retry_amount_ceiling",
		}
	}

	// ─── Rule 7: High value always needs human ────────────────────────────────
	// Money-moving actions only. Previously this blocked ESCALATE and STOP on
	// exactly the cases that needed them, which deadlocked the case.
	if isMoneyMoving(input.Action) && input.Amount > input.MerchantPolicy.RequireHumanAbovePaise {
		return PolicyDecision{
			Allowed: false,
			Reason: fmt.Sprintf("Amount above %s requires human approval",
				economics.FormatPaise(input.MerchantPolicy.RequireHumanAbovePaise)),
			RequiresHumanApproval: true,
			RuleTriggered:         "rule7_require_human_high_value",
		}
	}

	// ─── Rule 8: Max retries reached ──────────────────────────────────────────
	// Retries only. A payment link or notification after two failed retries is
	// the fallback we want, not something to block.
	if isMoneyMoving(input.Action) {
		maxRetries := input.MerchantPolicy.MaxRetries
		if input.CaseMaxRetries > 0 {
			maxRetries = input.CaseMaxRetries // case column wins; matches validator CHECK 6
		}
		if input.RetryCount >= maxRetries {
			return PolicyDecision{
				Allowed:       false,
				Reason:        fmt.Sprintf("Maximum retries (%d) reached", maxRetries),
				RuleTriggered: "rule8_max_retries",
			}
		}
	}

	// ─── Rule 9: Cooldown active ──────────────────────────────────────────────
	// Retries only — cooldown rate-limits debits, not communication.
	if isMoneyMoving(input.Action) && input.CooldownUntil != nil && e.now().Before(*input.CooldownUntil) {
		return PolicyDecision{
			Allowed:       false,
			Reason:        fmt.Sprintf("Cooldown active until %s", input.CooldownUntil.Format(time.RFC3339)),
			RuleTriggered: "rule9_cooldown_active",
		}
	}

	// ─── Rule 10: Action not in allowlist ─────────────────────────────────────
	// Safety valves are exempt: merchant configuration may not remove the
	// ability to halt recovery.
	if !isSafetyValve(input.Action) {
		name := actionToAllowlistName(input.Action)
		if name == "" || !contains(input.MerchantPolicy.AllowedActions, name) {
			return PolicyDecision{
				Allowed:       false,
				Reason:        fmt.Sprintf("Action %s not in merchant's allowed actions", input.Action),
				RuleTriggered: "rule10_action_not_allowed",
			}
		}
	}

	// ─── Rule 11: Per-attempt economics ───────────────────────────────────────
	// The validator priced this case once, before the AI chose a strategy and
	// before any retry burned an attempt. This is the gate that runs on EVERY
	// attempt, with the actual action known — because attempt 2 costs the same
	// as attempt 1 while being worth far less.
	econ := economics.Evaluate(economics.Input{
		AmountPaise:     input.Amount,
		Action:          input.Action,
		Attempt:         input.RetryCount,
		UPIErrorCode:    input.UPIErrorCode,
		BaseProbability: input.RecoveryProbability,
	})

	if !isSafetyValve(input.Action) && econ.NetEVPaise < input.MerchantPolicy.MinRecoveryROIPaise {
		return PolicyDecision{
			Allowed:       false,
			Reason:        econ.Explain(input.MerchantPolicy.MinRecoveryROIPaise) + " — not cost effective",
			RuleTriggered: "rule11_negative_net_ev",
			Economics:     &econ,
		}
	}

	// ─── All rules passed → ALLOWED ───────────────────────────────────────────
	return PolicyDecision{
		Allowed:       true,
		Reason:        "All policy checks passed",
		RuleTriggered: "none",
		Economics:     &econ,
	}
}

// actionToAllowlistName maps ExecutorCommand action names to merchant policy names.
func actionToAllowlistName(action string) string {
	switch action {
	case economics.ActionRetry:
		return "retry"
	case economics.ActionPaymentLink:
		return "payment_link"
	case economics.ActionNotify:
		return "notify"
	case economics.ActionEscalate:
		return "escalate"
	case economics.ActionStop:
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
