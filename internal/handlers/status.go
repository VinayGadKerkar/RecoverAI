package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"recoverai/internal/config"
	"recoverai/internal/services"
)

// StatusHandler handles the /api/v1/status endpoint.
type StatusHandler struct {
	aiClient *services.AIClient
	cfg      *config.Config
}

// NewStatusHandler creates a new status handler.
func NewStatusHandler(aiClient *services.AIClient, cfg *config.Config) *StatusHandler {
	return &StatusHandler{
		aiClient: aiClient,
		cfg:      cfg,
	}
}

// StatusResponse contains system status information.
type StatusResponse struct {
	AIMode           string `json:"ai_mode"`            // "mock" | "real"
	AIURL            string `json:"ai_url"`             // Current AI service URL
	MockAIAvailable  bool   `json:"mock_ai_available"`  // Health check result
	RealCallCount    int32  `json:"real_call_count"`    // Number of real AI calls made
	TestLimitEnabled bool   `json:"test_limit_enabled"` // Whether TEST_AI_LIMIT is active
	DemoMode         bool   `json:"demo_mode"`          // Whether DEMO_MODE is enabled (1-min delays)
}

// Handle returns the current AI configuration and status.
func (h *StatusHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Get current AI mode and URL
	aiMode := h.aiClient.GetMode()
	aiURL := h.aiClient.GetURL()
	
	// Check if mock AI is available (with timeout)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	mockAvailable := h.aiClient.IsMockAvailable(ctx)
	
	// Get real call count
	realCallCount := h.aiClient.GetRealCallCount()
	
	// Check if test limit is enabled
	testLimitEnabled := h.aiClient.GetTestLimit() > 0

	response := StatusResponse{
		AIMode:           aiMode,
		AIURL:            aiURL,
		MockAIAvailable:  mockAvailable,
		RealCallCount:    realCallCount,
		TestLimitEnabled: testLimitEnabled,
		DemoMode:         h.cfg.DemoMode,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
