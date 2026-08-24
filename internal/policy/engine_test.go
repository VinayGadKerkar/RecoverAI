package policy

import (
	"testing"
	"time"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// basePolicy returns a permissive MerchantPolicy that won't block unless we
// intentionally set a field in the test case.
func basePolicy() MerchantPolicy {
	return MerchantPolicy{
		AllowedActions:          []string{"retry", "payment_link", "notify", "escalate", "stop"},
		MaxRetryAmountPaise:     10_000_00, // ₹10,000
		RequireHumanAbovePaise:  50_000_00, // ₹50,000
		HighValueThresholdPaise: 15_000_00, // ₹15,000 (RBI)
		MaxRetries:              5,
	}
}

// safeInput returns a PolicyInput that passes all 10 rules when combined with
// basePolicy — individual tests override specific fields to isolate each rule.
func safeInput() PolicyInput {
	past := time.Now().Add(-2 * time.Hour)
	return PolicyInput{
		Action:             "RETRY_PAYMENT",
		UPIErrorCode:       "U30",  // retryable
		Amount:             100_00, // ₹100
		RetryCount:         0,
		ForcePaymentLink:   false,
		BankOutageDetected: false,
		IsMandatePayment:   false,
		CooldownUntil:      &past,
		MerchantPolicy:     basePolicy(),
	}
}

// ─── Rule 1: Non-retryable UPI error codes ────────────────────────────────────

func TestRule1_NonRetryableUPI(t *testing.T) {
	e := NewEngine()

	tests := []struct {
		name        string
		errorCode   string
		action      string
		wantAllowed bool
		wantRule    string
	}{
		// BLOCK: non-retryable codes + RETRY_PAYMENT
		{"Z9+RETRY blocked", "Z9", "RETRY_PAYMENT", false, "rule1_non_retryable_upi"},
		{"YG+RETRY blocked", "YG", "RETRY_PAYMENT", false, "rule1_non_retryable_upi"},
		{"Z8+RETRY blocked", "Z8", "RETRY_PAYMENT", false, "rule1_non_retryable_upi"},
		{"U68+RETRY blocked", "U68", "RETRY_PAYMENT", false, "rule1_non_retryable_upi"},

		// PASS: non-retryable code but different action
		{"Z9+PAYMENT_LINK pass", "Z9", "GENERATE_PAYMENT_LINK", true, "none"},
		{"YG+ESCALATE pass", "YG", "ESCALATE", true, "none"},

		// PASS: retryable codes
		{"U30+RETRY pass", "U30", "RETRY_PAYMENT", true, "none"},
		{"U28+RETRY pass", "U28", "RETRY_PAYMENT", true, "none"},
		{"RB+RETRY pass", "RB", "RETRY_PAYMENT", true, "none"},
		{"BT+RETRY pass", "BT", "RETRY_PAYMENT", true, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.Action = tt.action
			in.UPIErrorCode = tt.errorCode

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 2: Force payment link ───────────────────────────────────────────────

func TestRule2_ForcePaymentLink(t *testing.T) {
	e := NewEngine()

	tests := []struct {
		name        string
		force       bool
		action      string
		wantAllowed bool
		wantRule    string
	}{
		// BLOCK: force=true + RETRY
		{"force=true+RETRY blocked", true, "RETRY_PAYMENT", false, "rule2_force_payment_link"},

		// PASS: force=true but not a retry
		{"force=true+PAYMENT_LINK pass", true, "GENERATE_PAYMENT_LINK", true, "none"},
		{"force=true+ESCALATE pass", true, "ESCALATE", true, "none"},

		// PASS: force=false
		{"force=false+RETRY pass", false, "RETRY_PAYMENT", true, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.Action = tt.action
			in.ForcePaymentLink = tt.force

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 3: Bank outage active ───────────────────────────────────────────────

func TestRule3_BankOutage(t *testing.T) {
	e := NewEngine()

	tests := []struct {
		name        string
		outage      bool
		action      string
		wantAllowed bool
		wantRule    string
	}{
		// BLOCK: outage + RETRY
		{"outage+RETRY blocked", true, "RETRY_PAYMENT", false, "rule3_bank_outage"},

		// PASS: outage but not a retry
		{"outage+PAYMENT_LINK pass", true, "GENERATE_PAYMENT_LINK", true, "none"},
		{"outage+NOTIFY pass", true, "SEND_NOTIFICATION", true, "none"},

		// PASS: no outage
		{"no outage+RETRY pass", false, "RETRY_PAYMENT", true, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.Action = tt.action
			in.BankOutageDetected = tt.outage

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v", got.Allowed, tt.wantAllowed)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 4: RBI mandate 24h window ──────────────────────────────────────────

func TestRule4_RBIMandateWindow(t *testing.T) {
	e := NewEngine()
	future := time.Now().Add(2 * time.Hour)
	past := time.Now().Add(-25 * time.Hour)

	tests := []struct {
		name              string
		isMandate         bool
		rbiMinimumRetryAt *time.Time
		wantAllowed       bool
		wantRule          string
	}{
		// BLOCK: mandate + window not elapsed
		{"mandate+future window blocked", true, &future, false, "rule4_rbi_mandate_window"},

		// PASS: mandate + window elapsed
		{"mandate+past window pass", true, &past, true, "none"},

		// PASS: mandate + no window set
		{"mandate+nil window pass", true, nil, true, "none"},

		// PASS: not a mandate (window irrelevant)
		{"non-mandate+future window pass", false, &future, true, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.IsMandatePayment = tt.isMandate
			in.RBIMinimumRetryAt = tt.rbiMinimumRetryAt

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 5: High-value mandate requires human approval ──────────────────────
// Engine uses strict `>`: `input.Amount > HighValueThresholdPaise`
// So amount == threshold PASSES rule5 and falls through to later rules.

func TestRule5_RBIMandateHighValue(t *testing.T) {
	e := NewEngine()

	// Use a generous MaxRetryAmountPaise (20K) so rule6 doesn't interfere
	// with the small amounts we use to isolate rule5.
	policyForRule5 := func() MerchantPolicy {
		p := basePolicy()
		p.MaxRetryAmountPaise = 20_000_00     // ₹20K — above our test amounts
		p.RequireHumanAbovePaise = 50_000_00  // ₹50K — well above
		p.HighValueThresholdPaise = 15_000_00 // ₹15K
		return p
	}

	tests := []struct {
		name        string
		isMandate   bool
		amount      int64
		wantAllowed bool
		wantHuman   bool
		wantRule    string
	}{
		// BLOCK: mandate + amount strictly > threshold
		{
			name:      "mandate+₹16K blocked+human",
			isMandate: true, amount: 16_000_00,
			wantAllowed: false, wantHuman: true, wantRule: "rule5_rbi_mandate_high_value",
		},
		{
			name:      "mandate+₹15001 blocked+human",
			isMandate: true, amount: 15_000_00 + 100, // one paisa over threshold
			wantAllowed: false, wantHuman: true, wantRule: "rule5_rbi_mandate_high_value",
		},

		// BOUNDARY: amount == threshold → NOT strictly > → passes rule5
		{
			name:      "mandate+exactly ₹15K passes rule5 (strict >)",
			isMandate: true, amount: 15_000_00,
			wantAllowed: true, wantHuman: false, wantRule: "none",
		},

		// PASS: mandate + amount < threshold
		{
			name:      "mandate+₹1K passes rule5",
			isMandate: true, amount: 1_000_00,
			wantAllowed: true, wantHuman: false, wantRule: "none",
		},

		// PASS: non-mandate — rule5 short-circuits entirely
		{
			name:      "non-mandate+₹20K passes rule5",
			isMandate: false, amount: 19_000_00, // just below MaxRetryAmountPaise (20K)
			wantAllowed: true, wantHuman: false, wantRule: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.IsMandatePayment = tt.isMandate
			in.Amount = tt.amount
			in.MerchantPolicy = policyForRule5()

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RequiresHumanApproval != tt.wantHuman {
				t.Errorf("RequiresHumanApproval=%v want %v", got.RequiresHumanApproval, tt.wantHuman)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 6: Retry amount ceiling ────────────────────────────────────────────
// Engine uses strict `>`: `input.Amount > MaxRetryAmountPaise`
// So amount == ceiling PASSES rule6.

func TestRule6_RetryAmountCeiling(t *testing.T) {
	e := NewEngine()

	const ceiling = 10_000_00 // ₹10,000 in paise

	tests := []struct {
		name        string
		amount      int64
		action      string
		wantAllowed bool
		wantHuman   bool
		wantRule    string
	}{
		// BLOCK + human: amount strictly > ceiling
		{"₹11K+RETRY blocked+human", 11_000_00, "RETRY_PAYMENT", false, true, "rule6_retry_amount_ceiling"},
		{"₹10001+RETRY blocked", ceiling + 100, "RETRY_PAYMENT", false, true, "rule6_retry_amount_ceiling"},

		// BOUNDARY: amount == ceiling → NOT strictly > → passes rule6
		{"₹10K boundary passes rule6 (strict >)", ceiling, "RETRY_PAYMENT", true, false, "none"},

		// PASS: amount below ceiling
		{"₹9K+RETRY pass", 9_000_00, "RETRY_PAYMENT", true, false, "none"},

		// PASS: amount > ceiling but not a retry action
		{"₹11K+PAYMENT_LINK pass rule6", 11_000_00, "GENERATE_PAYMENT_LINK", true, false, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.Action = tt.action
			in.Amount = tt.amount
			in.MerchantPolicy = basePolicy()
			in.MerchantPolicy.MaxRetryAmountPaise = ceiling
			// Set RequireHumanAbovePaise well above so rule7 doesn't fire
			in.MerchantPolicy.RequireHumanAbovePaise = 50_000_00

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RequiresHumanApproval != tt.wantHuman {
				t.Errorf("RequiresHumanApproval=%v want %v", got.RequiresHumanApproval, tt.wantHuman)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 7: High value always requires human ─────────────────────────────────
// Engine uses strict `>`: `input.Amount > RequireHumanAbovePaise`
// So amount == limit PASSES rule7.

func TestRule7_RequireHumanHighValue(t *testing.T) {
	e := NewEngine()

	const universalLimit = 50_000_00 // ₹50,000 in paise

	tests := []struct {
		name        string
		amount      int64
		wantAllowed bool
		wantHuman   bool
		wantRule    string
	}{
		// BLOCK + human: amount strictly > universal limit
		{"₹51K blocked+human", 51_000_00, false, true, "rule7_require_human_high_value"},
		{"₹50001 blocked+human", universalLimit + 100, false, true, "rule7_require_human_high_value"},

		// BOUNDARY: amount == limit → NOT strictly > → passes rule7
		{"₹50K boundary passes rule7 (strict >)", universalLimit, true, false, "none"},

		// PASS: amount below limit
		{"₹49K pass", 49_000_00, true, false, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.Amount = tt.amount
			in.MerchantPolicy = basePolicy()
			// Set MaxRetryAmountPaise well above test amounts so rule6 doesn't fire
			in.MerchantPolicy.MaxRetryAmountPaise = 100_000_00 // ₹100K
			in.MerchantPolicy.RequireHumanAbovePaise = universalLimit

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RequiresHumanApproval != tt.wantHuman {
				t.Errorf("RequiresHumanApproval=%v want %v", got.RequiresHumanApproval, tt.wantHuman)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 8: Max retries reached ─────────────────────────────────────────────

func TestRule8_MaxRetries(t *testing.T) {
	e := NewEngine()

	tests := []struct {
		name        string
		retryCount  int
		maxRetries  int
		wantAllowed bool
		wantRule    string
	}{
		// BLOCK: count >= max
		{"count=max blocked", 2, 2, false, "rule8_max_retries"},
		{"count>max blocked", 3, 2, false, "rule8_max_retries"},

		// BOUNDARY: count = max-1 passes
		{"count=max-1 pass", 1, 2, true, "none"},

		// PASS: zero retries
		{"count=0 pass", 0, 2, true, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.RetryCount = tt.retryCount
			in.MerchantPolicy = basePolicy()
			in.MerchantPolicy.MaxRetries = tt.maxRetries

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 9: Cooldown active ──────────────────────────────────────────────────

func TestRule9_CooldownActive(t *testing.T) {
	e := NewEngine()

	future := time.Now().Add(30 * time.Minute)
	past := time.Now().Add(-30 * time.Minute)
	// just-expired: 1 nanosecond in the past — should pass
	justExpired := time.Now().Add(-1 * time.Nanosecond)

	tests := []struct {
		name          string
		cooldownUntil *time.Time
		wantAllowed   bool
		wantRule      string
	}{
		// BLOCK: cooldown in the future
		{"future cooldown blocked", &future, false, "rule9_cooldown_active"},

		// PASS: cooldown already expired
		{"past cooldown pass", &past, true, "none"},

		// PASS: nil cooldown
		{"nil cooldown pass", nil, true, "none"},

		// BOUNDARY: just expired (1 ns ago) — should pass
		{"just-expired cooldown pass", &justExpired, true, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.CooldownUntil = tt.cooldownUntil

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Rule 10: Action not in allowlist ────────────────────────────────────────

func TestRule10_ActionNotAllowed(t *testing.T) {
	e := NewEngine()

	tests := []struct {
		name           string
		action         string
		allowedActions []string
		wantAllowed    bool
		wantRule       string
	}{
		// BLOCK: action not in list
		{"RETRY not in list blocked", "RETRY_PAYMENT", []string{"payment_link", "notify"}, false, "rule10_action_not_allowed"},
		{"ESCALATE not in list blocked", "ESCALATE", []string{"retry"}, false, "rule10_action_not_allowed"},
		{"empty list blocked", "RETRY_PAYMENT", []string{}, false, "rule10_action_not_allowed"},

		// PASS: action in list
		{"RETRY in list pass", "RETRY_PAYMENT", []string{"retry", "payment_link"}, true, "none"},
		{"ESCALATE in list pass", "ESCALATE", []string{"escalate"}, true, "none"},
		{"STOP in list pass", "STOP", []string{"stop"}, true, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := safeInput()
			in.Action = tt.action
			in.MerchantPolicy = basePolicy()
			in.MerchantPolicy.AllowedActions = tt.allowedActions

			got := e.Evaluate(in)
			if got.Allowed != tt.wantAllowed {
				t.Errorf("Allowed=%v want %v (reason: %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, tt.wantRule)
			}
		})
	}
}

// ─── Integration: first match wins ───────────────────────────────────────────

func TestPolicyEngine_FirstMatchWins(t *testing.T) {
	e := NewEngine()

	future := time.Now().Add(2 * time.Hour)

	tests := []struct {
		name        string
		input       PolicyInput
		wantRule    string
		description string
	}{
		{
			name:        "Rule1 fires before Rule8",
			description: "Z9+RETRY with max retries also exceeded — rule1 wins",
			input: PolicyInput{
				Action:       "RETRY_PAYMENT",
				UPIErrorCode: "Z9", // triggers rule1
				RetryCount:   5,    // also triggers rule8
				Amount:       100_00,
				MerchantPolicy: func() MerchantPolicy {
					p := basePolicy()
					p.MaxRetries = 2
					return p
				}(),
			},
			wantRule: "rule1_non_retryable_upi",
		},
		{
			name:        "Rule2 fires before Rule3",
			description: "force_payment_link + bank outage — rule2 wins",
			input: PolicyInput{
				Action:             "RETRY_PAYMENT",
				ForcePaymentLink:   true, // rule2
				BankOutageDetected: true, // rule3
				Amount:             100_00,
				MerchantPolicy:     basePolicy(),
			},
			wantRule: "rule2_force_payment_link",
		},
		{
			name:        "Rule3 fires before Rule4",
			description: "bank outage + mandate window — rule3 wins",
			input: PolicyInput{
				Action:             "RETRY_PAYMENT",
				BankOutageDetected: true,    // rule3
				IsMandatePayment:   true,    // rule4
				RBIMinimumRetryAt:  &future, // rule4
				Amount:             100_00,
				MerchantPolicy:     basePolicy(),
			},
			wantRule: "rule3_bank_outage",
		},
		{
			name:        "Rule6 fires before Rule7",
			description: "exceeds retry ceiling AND universal limit — rule6 fires first",
			input: PolicyInput{
				Action: "RETRY_PAYMENT",
				Amount: 60_000_00, // ₹60K — over both rule6 and rule7
				MerchantPolicy: func() MerchantPolicy {
					p := basePolicy()
					p.MaxRetryAmountPaise = 10_000_00    // rule6: ₹10K
					p.RequireHumanAbovePaise = 50_000_00 // rule7: ₹50K
					return p
				}(),
			},
			wantRule: "rule6_retry_amount_ceiling",
		},
		{
			name:        "Rule8 fires before Rule9",
			description: "max retries exceeded + active cooldown — rule8 fires first",
			input: PolicyInput{
				Action:        "RETRY_PAYMENT",
				RetryCount:    3,       // rule8
				CooldownUntil: &future, // rule9
				Amount:        100_00,
				MerchantPolicy: func() MerchantPolicy {
					p := basePolicy()
					p.MaxRetries = 2
					return p
				}(),
			},
			wantRule: "rule8_max_retries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.Evaluate(tt.input)
			if got.Allowed {
				t.Errorf("expected blocked, got allowed (rule: %q)", got.RuleTriggered)
			}
			if got.RuleTriggered != tt.wantRule {
				t.Errorf("RuleTriggered=%q want %q (%s)", got.RuleTriggered, tt.wantRule, tt.description)
			}
		})
	}
}

// ─── All rules pass → ALLOWED ─────────────────────────────────────────────────

func TestPolicyEngine_AllRulesPass(t *testing.T) {
	e := NewEngine()
	got := e.Evaluate(safeInput())

	if !got.Allowed {
		t.Errorf("expected ALLOWED but blocked by %q: %s", got.RuleTriggered, got.Reason)
	}
	if got.RuleTriggered != "none" {
		t.Errorf("RuleTriggered=%q want %q", got.RuleTriggered, "none")
	}
	if got.RequiresHumanApproval {
		t.Error("RequiresHumanApproval should be false when all rules pass")
	}
}
