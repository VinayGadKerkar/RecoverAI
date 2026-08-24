package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ─── Request Schema (mirrors Python AnalyzeRequest) ──────────────────────────

type CustomerHistory struct {
	SuccessfulPayments      int     `json:"successful_payments"`
	FailedPayments          int     `json:"failed_payments"`
	LifetimeValuePaise      int64   `json:"lifetime_value_paise"`
	LastSuccessfulPaymentAt *string `json:"last_successful_payment_at"`
	DaysSinceLastSuccess    *int    `json:"days_since_last_success"`
}

type MerchantPolicy struct {
	MaxRetryAmountPaise    int64    `json:"max_retry_amount_paise"`
	MaxRetries             int      `json:"max_retries"`
	RetryCooldownMinutes   int      `json:"retry_cooldown_minutes"`
	RequireHumanAbovePaise int64    `json:"require_human_above_paise"`
	AllowedActions         []string `json:"allowed_actions"`
}

type AnalyzeRequest struct {
	PaymentID          string          `json:"payment_id"`
	CaseID             string          `json:"case_id"`
	AmountPaise        int64           `json:"amount_paise"`
	UPIErrorCode       string          `json:"upi_error_code"`
	UPIErrorCategory   string          `json:"upi_error_category"`
	FailureType        string          `json:"failure_type"`
	FailureReason      string          `json:"failure_reason"`
	TimeOfFailureHour  int             `json:"time_of_failure_hour"`
	ForcePaymentLink   bool            `json:"force_payment_link"`
	CustomerHistory    CustomerHistory `json:"customer_history"`
	RiskScore          float64         `json:"risk_score"`
	Priority           string          `json:"priority"`
	MerchantPolicy     MerchantPolicy  `json:"merchant_policy"`
}

// ─── Response Schema (mirrors Python ExecutorCommand) ────────────────────────

type ExecutorCommand struct {
	Action              string                 `json:"action"`
	PaymentID           string                 `json:"payment_id"`
	CaseID              string                 `json:"case_id"`
	ScheduledAtMinutes  int                    `json:"scheduled_at_minutes"`
	Parameters          map[string]interface{} `json:"parameters"`
	RiskAssessment      *map[string]interface{} `json:"risk_assessment_summary,omitempty"`
	StrategyAssessment  *map[string]interface{} `json:"strategy_summary,omitempty"`
	Mock                bool                   `json:"_mock"` // Extra field to identify mock responses
}

// ─── Mock AI Logic ────────────────────────────────────────────────────────────

type mockDecision struct {
	action         string
	strategy       string
	confidence     float64
	delayMinutes   int
	reasoning      string
}

func getMockDecision(upiErrorCode string) mockDecision {
	switch upiErrorCode {
	case "U30", "RB", "BT":
		// Transient TD failures — high confidence retry
		return mockDecision{
			action:       "RETRY_PAYMENT",
			strategy:     "retry_payment",
			confidence:   0.91,
			delayMinutes: 10,
			reasoning:    "Transient TD failure — mock: high confidence retry",
		}

	case "U28":
		// Bank server down — retry after recovery window
		return mockDecision{
			action:       "RETRY_PAYMENT",
			strategy:     "schedule_retry",
			confidence:   0.85,
			delayMinutes: 60,
			reasoning:    "Bank server down — mock: retry after bank recovery",
		}

	case "U16":
		// Insufficient balance — 24h delay, payment link
		return mockDecision{
			action:       "GENERATE_PAYMENT_LINK",
			strategy:     "generate_payment_link",
			confidence:   0.75,
			delayMinutes: 1440,
			reasoning:    "Insufficient balance — mock: 24h delay, payment link",
		}

	case "Z9", "Z8":
		// Non-retryable — payment link only
		return mockDecision{
			action:       "GENERATE_PAYMENT_LINK",
			strategy:     "generate_payment_link",
			confidence:   0.70,
			delayMinutes: 1440,
			reasoning:    "Non-retryable — mock: payment link only",
		}

	case "YG":
		// Risk blocked — escalate immediately
		return mockDecision{
			action:       "ESCALATE",
			strategy:     "escalate_to_merchant",
			confidence:   0.95,
			delayMinutes: 0,
			reasoning:    "Risk blocked — mock: escalate immediately",
		}

	case "U68", "Z7":
		// Account issue — notify customer
		return mockDecision{
			action:       "GENERATE_PAYMENT_LINK",
			strategy:     "notify_customer",
			confidence:   0.65,
			delayMinutes: 30,
			reasoning:    "Account issue — mock: notify customer",
		}

	default:
		// Unknown error — safe default
		return mockDecision{
			action:       "GENERATE_PAYMENT_LINK",
			strategy:     "generate_payment_link",
			confidence:   0.60,
			delayMinutes: 30,
			reasoning:    "Unknown error — mock: safe default",
		}
	}
}

// ─── HTTP Handlers ────────────────────────────────────────────────────────────

func handleAnalyze(mockDelayMs int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AnalyzeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[MOCK-AI] ERROR: Invalid request body: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Simulate realistic AI latency without calling Groq
		if mockDelayMs > 0 {
			time.Sleep(time.Duration(mockDelayMs) * time.Millisecond)
		}

		// Get deterministic decision based on UPI error code
		decision := getMockDecision(req.UPIErrorCode)

		// Build response matching real AI service schema
		response := ExecutorCommand{
			Action:             decision.action,
			PaymentID:          req.PaymentID,
			CaseID:             req.CaseID,
			ScheduledAtMinutes: decision.delayMinutes,
			Parameters:         make(map[string]interface{}),
			Mock:               true, // Identify mock responses in logs
		}

		// Add action-specific parameters
		switch decision.action {
		case "RETRY_PAYMENT":
			response.Parameters["retry_reason"] = decision.reasoning
		case "GENERATE_PAYMENT_LINK":
			response.Parameters["link_reason"] = decision.reasoning
			response.Parameters["expiry_hours"] = 48
		case "SEND_NOTIFICATION":
			response.Parameters["message"] = decision.reasoning
			response.Parameters["channel"] = "sms"
		case "ESCALATE":
			response.Parameters["escalation_reason"] = decision.reasoning
			response.Parameters["priority"] = "high"
		case "STOP":
			response.Parameters["stop_reason"] = decision.reasoning
		}

		// Add metadata summaries (optional, for audit trail)
		riskSummary := map[string]interface{}{
			"recovery_probability": decision.confidence,
			"priority":             req.Priority,
			"failure_category":     req.UPIErrorCategory,
			"reasoning":            decision.reasoning,
		}
		response.RiskAssessment = &riskSummary

		strategySummary := map[string]interface{}{
			"strategy":    decision.strategy,
			"confidence":  decision.confidence,
			"delay_minutes": decision.delayMinutes,
			"reasoning":   decision.reasoning,
		}
		response.StrategyAssessment = &strategySummary

		// Log request and decision
		log.Printf(
			"[MOCK-AI] ANALYZE: payment_id=%s upi_error=%s action=%s strategy=%s confidence=%.2f delay=%dm",
			req.PaymentID,
			req.UPIErrorCode,
			decision.action,
			decision.strategy,
			decision.confidence,
			decision.delayMinutes,
		)

		// Always return HTTP 200 (never fails — clean pipeline testing)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"mock":   true,
		"status": "ok",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	// Parse mock delay from environment (default 50ms)
	mockDelayMs := 50
	if delayStr := os.Getenv("MOCK_AI_DELAY_MS"); delayStr != "" {
		if delay, err := strconv.Atoi(delayStr); err == nil && delay >= 0 {
			mockDelayMs = delay
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Health endpoint
	r.Get("/health", handleHealth)

	// Main analyze endpoint (matches FastAPI path)
	r.Post("/analyze", handleAnalyze(mockDelayMs))

	log.Printf("🤖 Mock AI Server starting on port %s", port)
	log.Printf("   - Mock delay: %dms", mockDelayMs)
	log.Printf("   - Health: http://localhost:%s/health", port)
	log.Printf("   - Analyze: POST http://localhost:%s/analyze", port)
	log.Println("   - No LLM calls, zero tokens used, always returns HTTP 200")

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
