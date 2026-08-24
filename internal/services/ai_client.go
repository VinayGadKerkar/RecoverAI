package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// ─── Request/Response Schemas ─────────────────────────────────────────────────

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
	PaymentID         string          `json:"payment_id"`
	CaseID            string          `json:"case_id"`
	AmountPaise       int64           `json:"amount_paise"`
	UPIErrorCode      string          `json:"upi_error_code"`
	UPIErrorCategory  string          `json:"upi_error_category"`
	FailureType       string          `json:"failure_type"`
	FailureReason     string          `json:"failure_reason"`
	TimeOfFailureHour int             `json:"time_of_failure_hour"`
	ForcePaymentLink  bool            `json:"force_payment_link"`
	CustomerHistory   CustomerHistory `json:"customer_history"`
	RiskScore         float64         `json:"risk_score"`
	Priority          string          `json:"priority"`
	MerchantPolicy    MerchantPolicy  `json:"merchant_policy"`
}

type AnalyzeResponse struct {
	Action              string                 `json:"action"`
	PaymentID           string                 `json:"payment_id"`
	CaseID              string                 `json:"case_id"`
	ScheduledAtMinutes  int                    `json:"scheduled_at_minutes"`
	Parameters          map[string]interface{} `json:"parameters"`
	RiskAssessment      map[string]interface{} `json:"risk_assessment_summary,omitempty"`
	StrategyAssessment  map[string]interface{} `json:"strategy_summary,omitempty"`
	Mock                bool                   `json:"_mock,omitempty"`
}

// ─── AI Client ────────────────────────────────────────────────────────────────

// AIClient handles routing between real and mock AI services.
type AIClient struct {
	useMock          bool
	realURL          string
	mockURL          string
	httpClient       *http.Client
	testLimit        int32  // TEST_AI_LIMIT value (0 = unlimited)
	realCallCount    int32  // Atomic counter for real AI calls
	forceMockMode    int32  // Atomic flag: 1 = forced to mock due to limit
}

// NewAIClient creates a new AI client with environment-based configuration.
func NewAIClient() *AIClient {
	useMock := os.Getenv("USE_MOCK_AI") == "true"
	
	realURL := os.Getenv("AI_SERVICE_URL")
	if realURL == "" {
		realURL = "http://localhost:8000"
	}
	
	mockURL := os.Getenv("MOCK_AI_URL")
	if mockURL == "" {
		mockURL = "http://localhost:8001"
	}

	// Parse TEST_AI_LIMIT (default: 0 = unlimited)
	testLimit := int32(0)
	if limitStr := os.Getenv("TEST_AI_LIMIT"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			testLimit = int32(limit)
		}
	}

	client := &AIClient{
		useMock:       useMock,
		realURL:       realURL,
		mockURL:       mockURL,
		testLimit:     testLimit,
		realCallCount: 0,
		forceMockMode: 0,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Log startup mode
	if useMock {
		log.Printf("🤖 AI mode: MOCK (%s)", mockURL)
	} else {
		log.Printf("🧠 AI mode: REAL (%s)", realURL)
		if testLimit > 0 {
			log.Printf("   TEST_AI_LIMIT enabled: will switch to mock after %d real AI calls", testLimit)
		}
	}

	return client
}

// Analyze sends an analyze request to the appropriate AI service.
func (c *AIClient) Analyze(ctx context.Context, req AnalyzeRequest) (AnalyzeResponse, error) {
	// Determine which URL to use
	targetURL := c.getTargetURL()
	isMockCall := c.isMockCall()

	// Log the call type
	if isMockCall {
		log.Printf("[AI-CLIENT] Routing to MOCK AI: payment_id=%s upi_error=%s", 
			req.PaymentID, req.UPIErrorCode)
	} else {
		currentCount := atomic.LoadInt32(&c.realCallCount)
		log.Printf("[AI-CLIENT] Routing to REAL AI (%d/%d): payment_id=%s upi_error=%s", 
			currentCount+1, c.testLimit, req.PaymentID, req.UPIErrorCode)
	}

	// Make HTTP request
	response, err := c.makeRequest(ctx, targetURL, req)
	if err != nil {
		// If mock is unreachable, return safe default instead of crashing
		if isMockCall {
			log.Printf("[AI-CLIENT] WARNING: Mock AI unreachable (%v), returning safe STOP command", err)
			return c.createSafeStopCommand(req), nil
		}
		return AnalyzeResponse{}, fmt.Errorf("AI request failed: %w", err)
	}

	// Increment real AI counter if this was a real call
	if !isMockCall {
		newCount := atomic.AddInt32(&c.realCallCount, 1)
		
		// Check if we've hit the test limit
		if c.testLimit > 0 && newCount >= c.testLimit {
			if atomic.CompareAndSwapInt32(&c.forceMockMode, 0, 1) {
				log.Printf("⚠️  TEST_AI_LIMIT reached (%d/%d) — switching to mock mode for remaining cases", 
					newCount, c.testLimit)
			}
		}
	}

	return response, nil
}

// GetMode returns the current AI mode ("mock" or "real").
func (c *AIClient) GetMode() string {
	if c.isMockCall() {
		return "mock"
	}
	return "real"
}

// GetURL returns the current active AI service URL.
func (c *AIClient) GetURL() string {
	return c.getTargetURL()
}

// GetRealCallCount returns the number of real AI calls made.
func (c *AIClient) GetRealCallCount() int32 {
	return atomic.LoadInt32(&c.realCallCount)
}

// GetTestLimit returns the TEST_AI_LIMIT value.
func (c *AIClient) GetTestLimit() int32 {
	return c.testLimit
}

// IsMockAvailable checks if the mock AI service is reachable.
func (c *AIClient) IsMockAvailable(ctx context.Context) bool {
	healthURL := c.mockURL + "/health"
	
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}

// ─── Internal Methods ─────────────────────────────────────────────────────────

// isMockCall determines if this call should go to mock AI.
func (c *AIClient) isMockCall() bool {
	// If USE_MOCK_AI=true, always use mock
	if c.useMock {
		return true
	}
	
	// If TEST_AI_LIMIT is set and reached, force mock mode
	if c.testLimit > 0 && atomic.LoadInt32(&c.forceMockMode) == 1 {
		return true
	}
	
	return false
}

// getTargetURL returns the URL to send requests to.
func (c *AIClient) getTargetURL() string {
	if c.isMockCall() {
		return c.mockURL + "/analyze"
	}
	return c.realURL + "/analyze"
}

// makeRequest performs the HTTP POST request to the AI service.
func (c *AIClient) makeRequest(ctx context.Context, url string, req AnalyzeRequest) (AnalyzeResponse, error) {
	// Serialize request
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return AnalyzeResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return AnalyzeResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return AnalyzeResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return AnalyzeResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return AnalyzeResponse{}, fmt.Errorf("AI service returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var response AnalyzeResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return AnalyzeResponse{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return response, nil
}

// createSafeStopCommand creates a safe default STOP command when mock AI is unreachable.
func (c *AIClient) createSafeStopCommand(req AnalyzeRequest) AnalyzeResponse {
	return AnalyzeResponse{
		Action:             "STOP",
		PaymentID:          req.PaymentID,
		CaseID:             req.CaseID,
		ScheduledAtMinutes: 0,
		Parameters: map[string]interface{}{
			"reason": "Mock AI service unreachable — safe default applied",
		},
		RiskAssessment: map[string]interface{}{
			"recovery_probability": 0.0,
			"priority":             "low",
			"reasoning":            "Mock AI unreachable",
		},
		StrategyAssessment: map[string]interface{}{
			"strategy":      "stop_recovery",
			"confidence":    0.0,
			"delay_minutes": 0,
			"reasoning":     "Mock AI unreachable — cannot analyze",
		},
		Mock: true,
	}
}
