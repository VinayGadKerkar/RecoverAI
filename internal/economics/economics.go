// Package economics is the single source of truth for recovery cost/benefit.
//
// Every money value in this package is int64 PAISE. Nothing here returns rupees.
// Rupees exist only at the presentation boundary (log strings, API formatting).
//
// Two services depend on this package and MUST agree:
//   - internal/validator  → cheap pre-AI filter (is any action worth it at all?)
//   - internal/policy     → per-attempt gate at execution time (is THIS action worth it now?)
//
// It also owns the UPI error-code capability table, so the validator's
// force_payment_link flag and the policy engine's non-retryable rule can never
// drift apart again.
package economics

import (
	"fmt"
	"math"
)

// ─── Action names (match ExecutorCommand action strings) ─────────────────────

const (
	ActionRetry       = "RETRY_PAYMENT"
	ActionPaymentLink = "GENERATE_PAYMENT_LINK"
	ActionNotify      = "SEND_NOTIFICATION"
	ActionEscalate    = "ESCALATE"
	ActionStop        = "STOP"
)

// ─── Cost model (paise) ───────────────────────────────────────────────────────
//
// UPI MDR is zero for merchants in India, so a retry costs no gateway fee — but
// it is NOT free. A retry costs LLM inference + infra, and a *failed* retry
// degrades the merchant's success ratio with the PSP. The dominant real cost is
// human time, which is why escalation is ~1000x a retry.
const (
	CostRetryPaise       int64 = 5    // ₹0.05  — inference + infra per case
	CostNotifyPaise      int64 = 25   // ₹0.25  — SMS / WhatsApp delivery
	CostPaymentLinkPaise int64 = 30   // ₹0.30  — link generation + delivery
	CostEscalatePaise    int64 = 5000 // ₹50.00 — ~6 min of agent time, loaded
)

// DefaultContributionMargin is the share of the recovered amount that is real
// margin. 1.0 = treat the full amount as recovered value (correct when goods
// have not shipped yet). Lower it per merchant category if goods are already
// dispatched and only the margin is at stake.
const DefaultContributionMargin = 1.0

// MaxStorablePaise clamps values written to recovery_cases.recovery_roi, which
// is DECIMAL(10,2) and overflows above 99,999,999.99. Clamping keeps a
// pathological amount from turning an economics write into a 500.
const MaxStorablePaise int64 = 99_999_999

// ActionCostPaise returns the marginal cost of performing one action.
// Cost belongs to the ACTION, never to the probability.
func ActionCostPaise(action string) int64 {
	switch action {
	case ActionRetry:
		return CostRetryPaise
	case ActionPaymentLink:
		return CostPaymentLinkPaise
	case ActionNotify:
		return CostNotifyPaise
	case ActionEscalate:
		return CostEscalatePaise
	case ActionStop:
		return 0
	default:
		// Unknown action: price it as the most expensive path so unknowns
		// fail conservatively rather than looking free.
		return CostEscalatePaise
	}
}

// ─── UPI error-code capability table ─────────────────────────────────────────

// Capability describes what may be done about a given UPI error code, and how
// likely the customer is to fix it WITHOUT any intervention from us.
type Capability struct {
	Category string // "TD" (technical decline) | "BD" (business decline)

	// Retryable: a direct re-debit of the same instrument is permitted.
	Retryable bool

	// RequiresAlternateMethod: a direct retry is pointless or blocked; route to
	// a payment link so the customer can choose another instrument.
	RequiresAlternateMethod bool

	// EscalateOnly: risk/compliance block — neither retry nor link; a human
	// must look at it.
	EscalateOnly bool

	// SelfRecoveryBaseline is P(customer pays on their own with no intervention).
	// This is the counterfactual. Value we can claim is only the INCREMENT over
	// this baseline — otherwise we book revenue that would have arrived anyway.
	SelfRecoveryBaseline float64
}

var unknownCapability = Capability{
	Category:             "unknown",
	Retryable:            true,
	SelfRecoveryBaseline: 0.20,
}

// codeTable keeps the same retryable/non-retryable split the policy engine
// shipped with ({Z9, YG, Z8, U68} non-retryable) and adds the self-recovery
// baseline each code implies.
var codeTable = map[string]Capability{
	// ── Technical declines: bank-side, transient, customer can't fix it ──────
	"U30": {Category: "TD", Retryable: true, SelfRecoveryBaseline: 0.10}, // debit timeout
	"U28": {Category: "TD", Retryable: true, SelfRecoveryBaseline: 0.12}, // bank server down
	"RB":  {Category: "TD", Retryable: true, SelfRecoveryBaseline: 0.10}, // bank load block
	"BT":  {Category: "TD", Retryable: true, SelfRecoveryBaseline: 0.10}, // beneficiary timeout

	// ── Business declines: customer-side; many of these self-resolve ─────────
	"U16": {Category: "BD", Retryable: true, RequiresAlternateMethod: true, SelfRecoveryBaseline: 0.45},  // insufficient balance
	"Z7":  {Category: "BD", Retryable: true, SelfRecoveryBaseline: 0.30},                                 // velocity limit — retry after cooldown
	"Z9":  {Category: "BD", Retryable: false, RequiresAlternateMethod: true, SelfRecoveryBaseline: 0.40}, // insufficient funds
	"Z8":  {Category: "BD", Retryable: false, RequiresAlternateMethod: true, SelfRecoveryBaseline: 0.30}, // per-txn limit
	"U68": {Category: "BD", Retryable: false, RequiresAlternateMethod: true, SelfRecoveryBaseline: 0.15}, // txn not permitted
	"YG":  {Category: "BD", Retryable: false, EscalateOnly: true, SelfRecoveryBaseline: 0.05},            // NPCI risk block
}

// CapabilityFor returns the capability for a UPI error code. Unknown codes are
// treated as retryable with a moderate baseline.
func CapabilityFor(code string) Capability {
	if c, ok := codeTable[code]; ok {
		return c
	}
	return unknownCapability
}

// IsRetryable reports whether a direct re-debit is permitted for this code.
// internal/policy Rule 1 is defined by this function.
func IsRetryable(code string) bool { return CapabilityFor(code).Retryable }

// ForcePaymentLink reports whether the AI must be constrained to a payment-link
// strategy. internal/validator CHECK 5 is defined by this function.
func ForcePaymentLink(code string) bool {
	c := CapabilityFor(code)
	return c.RequiresAlternateMethod || (!c.Retryable && !c.EscalateOnly)
}

// EscalateOnly reports whether only a human may act on this code.
func EscalateOnly(code string) bool { return CapabilityFor(code).EscalateOnly }

// CheapestViableAction returns the lowest-cost action that could plausibly work
// for this error code. The validator uses it to price a case BEFORE the AI has
// picked a strategy, instead of inferring cost from probability.
func CheapestViableAction(code string) string {
	c := CapabilityFor(code)
	switch {
	case c.EscalateOnly:
		return ActionEscalate
	case !c.Retryable || c.RequiresAlternateMethod:
		return ActionPaymentLink
	default:
		return ActionRetry
	}
}

// ─── Attempt decay ────────────────────────────────────────────────────────────

// AttemptMultiplier scales the base success probability by attempt number.
// A second retry of the same instrument is worth far less than the first, at
// identical cost — which is exactly why the gate must run per attempt.
//
// attempt is zero-based and equals recovery_cases.retry_count.
func AttemptMultiplier(attempt int) float64 {
	switch {
	case attempt <= 0:
		return 1.00
	case attempt == 1:
		return 0.45
	case attempt == 2:
		return 0.20
	default:
		return 0.10
	}
}

// ─── Evaluation ───────────────────────────────────────────────────────────────

// Input is everything needed to price one action on one attempt.
type Input struct {
	AmountPaise int64 // recovery_cases.revenue_at_risk

	// Action being priced. Empty → CheapestViableAction(UPIErrorCode).
	Action string

	// Attempt is zero-based (recovery_cases.retry_count).
	Attempt int

	UPIErrorCode string

	// BaseProbability is the risk engine / AI estimate for attempt 1.
	// Zero or out-of-range falls back to the code's baseline + 0.2.
	BaseProbability float64

	// ContributionMargin: 0 → DefaultContributionMargin.
	ContributionMargin float64
}

// Result is the full, auditable breakdown. Persist NetEVPaise; log the rest.
type Result struct {
	Action               string  `json:"action"`
	Attempt              int     `json:"attempt"`
	Probability          float64 `json:"probability"`            // after attempt decay
	SelfRecoveryBaseline float64 `json:"self_recovery_baseline"` // counterfactual
	DeltaP               float64 `json:"delta_p"`                // incremental probability
	GrossPaise           int64   `json:"gross_paise"`
	CostPaise            int64   `json:"cost_paise"`
	NetEVPaise           int64   `json:"net_ev_paise"`
}

// Evaluate computes the incremental expected value of taking one action.
//
//	NetEV = Δp × amount × margin − cost(action)
//	Δp    = P(success | we intervene) − P(customer self-recovers anyway)
//
// Both terms matter. Using raw probability without the baseline books revenue
// that would have arrived without us; pricing every action the same blocks
// near-free retries because a hypothetical escalation is expensive.
func Evaluate(in Input) Result {
	capability := CapabilityFor(in.UPIErrorCode)

	action := in.Action
	if action == "" {
		action = CheapestViableAction(in.UPIErrorCode)
	}

	margin := in.ContributionMargin
	if margin <= 0 {
		margin = DefaultContributionMargin
	}

	base := in.BaseProbability
	if base <= 0 || base > 1 {
		base = math.Min(0.95, capability.SelfRecoveryBaseline+0.20)
	}

	p := clamp01(base * AttemptMultiplier(in.Attempt))
	deltaP := math.Max(0, p-capability.SelfRecoveryBaseline)

	gross := int64(float64(in.AmountPaise) * margin * deltaP)
	cost := ActionCostPaise(action)

	return Result{
		Action:               action,
		Attempt:              in.Attempt,
		Probability:          round4(p),
		SelfRecoveryBaseline: capability.SelfRecoveryBaseline,
		DeltaP:               round4(deltaP),
		GrossPaise:           gross,
		CostPaise:            cost,
		NetEVPaise:           gross - cost,
	}
}

// Explain renders a human-readable, unit-explicit sentence for
// validator_skip_reason and policy_reason. Rupees appear ONLY here.
func (r Result) Explain(thresholdPaise int64) string {
	return fmt.Sprintf(
		"Net EV %s (gross %s − cost %s; Δp=%.3f = p_intervene %.2f − p_self_recover %.2f) below threshold %s",
		FormatPaise(r.NetEVPaise),
		FormatPaise(r.GrossPaise),
		FormatPaise(r.CostPaise),
		r.DeltaP,
		r.Probability,
		r.SelfRecoveryBaseline,
		FormatPaise(thresholdPaise),
	)
}

// FormatPaise formats paise as rupees with proper currency symbol.
// This is the ONLY place rupees appear in the economics package.
func FormatPaise(paise int64) string {
	rupees := float64(paise) / 100.0
	if rupees < 0 {
		return fmt.Sprintf("−₹%.2f", -rupees)
	}
	return fmt.Sprintf("₹%.2f", rupees)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}